package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"grab/internal/connect"
	"grab/internal/contracts"
	"grab/internal/data"
	"grab/internal/events"
	"grab/internal/interpreter"
	"grab/internal/platform"
	rt "grab/internal/runtime"
	"grab/internal/server"
)

// This permanent fixture joins real GRAB handlers, signed ingestion, atomic
// business/outbox work, authorized publication and typed authoritative refetch.
func TestGRAB08DurableWorkflow(t *testing.T) { runDurableWorkflow(t, "sqlite", 0) }
func TestGRAB08DurableRemoteWorkers(t *testing.T) {
	if os.Getenv("GRAB_TEST_DATABASES") != "1" || os.Getenv("GRAB_TEST_REDIS_PORT") == "" {
		t.Skip("requires disposable MariaDB/PostgreSQL and Redis")
	}
	for _, engine := range []string{"mariadb", "postgres"} {
		t.Run(engine, func(t *testing.T) { runDurableWorkflow(t, engine, 2) })
	}
}
func runDurableWorkflow(t *testing.T, engine string, workers int) {
	root := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	t.Setenv("GRAB_FLOW_KEY", "local-demo")
	t.Setenv("GRAB_FLOW_SIGNING", "public-disposable-signing-fixture")
	dbRaw := []byte(`{"connections":{"shop":{"driver":"sqlite","path":"workflow.sqlite"}}}`)
	if engine != "sqlite" {
		port := 55432
		if engine == "mariadb" {
			port = 53306
		}
		dbRaw = []byte(fmt.Sprintf(`{"connections":{"shop":{"driver":%q,"host":"127.0.0.1","port":%d,"database":"grab_test","user":"grab","password_env":"GRAB_TEST_DB_PASSWORD","tls":"disabled","pool":{"max_open":4}}}}`, engine, port))
	}
	dbPath := filepath.Join(root, "grab.data.json")
	if e := os.WriteFile(dbPath, dbRaw, 0600); e != nil {
		t.Fatal(e)
	}
	database, e := data.ParseConfig(dbRaw, dbPath)
	if e != nil {
		t.Fatal(e)
	}
	mg, _ := database.Grant([]string{"shop:migrate"}, false)
	manager := data.NewManager(mg, false)
	scope := manager.Scope()
	pool, e := scope.Open(ctx, "shop")
	if e != nil {
		t.Fatal(e)
	}
	db, e := scope.HostMaintenanceDatabase(pool)
	if e != nil {
		t.Fatal(e)
	}
	if engine == "sqlite" {
		versions, e := data.MigrationManifest(rt.List([]rt.Value{rt.Record([]rt.Entry{{Name: "version", Value: rt.IntegerFromInt64(1)}, {Name: "name", Value: rt.Text("workflow")}, {Name: "steps", Value: rt.List([]rt.Value{rt.Text("CREATE TABLE notifications (id TEXT PRIMARY KEY, message TEXT NOT NULL)")})}})}), "sqlite")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = scope.Migrate(ctx, pool, versions); e != nil {
			t.Fatal(e)
		}
	} else {
		// Explicit maintenance of disposable fixture tables, before any worker starts.
		if _, e = db.ExecContext(ctx, "DROP TABLE IF EXISTS notifications"); e != nil {
			t.Fatal(e)
		}
		if _, e = db.ExecContext(ctx, "CREATE TABLE notifications (id VARCHAR(128) PRIMARY KEY, message TEXT NOT NULL)"); e != nil {
			t.Fatal(e)
		}
	}
	if e = scope.MigrateEvents(ctx, pool, events.MigrationSteps(engine)); e != nil {
		t.Fatal(e)
	}
	namespace := "workflow" + events.ID()[:8]
	store := &events.Store{DB: db, Driver: engine, Config: platform.EventStore{Namespace: namespace}}
	if e = store.InitNamespace(ctx); e != nil {
		t.Fatal(e)
	}
	scope.Close()
	manager.Close()
	var appURL atomic.Value
	var providerMu sync.Mutex
	delivered := map[string]bool{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		key := r.Header.Get("Idempotency-Key")
		if r.Header.Get("Authorization") != "Bearer local-demo" || key == "" {
			http.Error(w, "denied", 403)
			return
		}
		providerMu.Lock()
		defer providerMu.Unlock()
		if !delivered[key] {
			request, _ := http.NewRequestWithContext(r.Context(), "POST", appURL.Load().(string)+"/notify", nil)
			request.Header.Set("Authorization", "Bearer local-demo")
			response, e := http.DefaultClient.Do(request)
			if e != nil {
				http.Error(w, "notification unavailable", 503)
				return
			}
			response.Body.Close()
			if response.StatusCode != 202 {
				http.Error(w, "notification rejected", 503)
				return
			}
			delivered[key] = true
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(raw)
	}))
	defer provider.Close()
	pkg, e := os.ReadFile("../examples/connectors/reference/connector.json")
	if e != nil {
		t.Fatal(e)
	}
	pkg = bytes.ReplaceAll(pkg, []byte("https://reference.example.invalid"), []byte(provider.URL))
	os.Mkdir(filepath.Join(root, "reference"), 0700)
	os.WriteFile(filepath.Join(root, "reference", "connector.json"), pkg, 0600)
	raw := fmt.Sprintf(`{"version":1,"caches":{"catalog":{"driver":"memory","namespace":"flow"}},"connectors":{"reference":{"package":"reference","version":"1.0.0","development":true,"secrets":{"api_key":"GRAB_FLOW_KEY"}}},"events":{"jobs":{"database":"shop","namespace":"workflow","concurrency":1}},"webhooks":{"inbound":{"path":"/webhook","connector":"reference","store":"jobs","tenant":"demo","handler":"ingest","keys":["GRAB_FLOW_SIGNING"],"type":"notification"}},"channels":{"updates":{"path":"/live","authorize":"authorize","receive":"receive","publish_authorize":"publish_authorize","client_type":"Notice","server_type":"Notice","origins":["http://127.0.0.1:8080"],"development":true,"broker":{"driver":"memory"}}}}`)
	raw = strings.ReplaceAll(raw, `"namespace":"workflow"`, `"namespace":"`+namespace+`"`)
	if workers > 0 {
		redisPort, _ := strconv.Atoi(os.Getenv("GRAB_TEST_REDIS_PORT"))
		var manifest map[string]any
		if e = json.Unmarshal([]byte(raw), &manifest); e != nil {
			t.Fatal(e)
		}
		redis := map[string]any{"driver": "redis", "namespace": namespace, "host": "127.0.0.1", "port": redisPort, "tls": "disabled", "development": true, "pool_size": 4}
		manifest["caches"].(map[string]any)["catalog"] = redis
		manifest["channels"].(map[string]any)["updates"].(map[string]any)["broker"] = redis
		manifest["workers"] = map[string]any{"max_db_connections": 32, "max_redis_connections": 64, "db_headroom": 4, "redis_headroom": 9}
		b, e := json.Marshal(manifest)
		if e != nil {
			t.Fatal(e)
		}
		raw = string(b)
	}
	platformPath := filepath.Join(root, "grab.platform.json")
	if e = os.WriteFile(platformPath, []byte(raw), 0600); e != nil {
		t.Fatal(e)
	}
	cfg, e := platform.Parse([]byte(raw), filepath.Join(root, "grab.platform.json"))
	if e != nil {
		t.Fatal(e)
	}
	lock, e := connect.BuildLock(cfg)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(root, "grab.connect.lock"), lock, 0600)
	grants, e := cfg.Grant(map[string][]string{"cache": {"catalog:read-write"}, "events": {"jobs:accept", "jobs:work"}, "connector": {"reference:send"}, "channel": {"updates:connect", "updates:publish"}})
	if e != nil {
		t.Fatal(e)
	}
	source, e := os.ReadFile("../examples/realtime_app.grab")
	if e != nil {
		t.Fatal(e)
	}
	source = append(source, []byte(`
use data
use events
use cache
use api
set catalog_cache to cache.open using "catalog"
set shop to data.open using "shop"
set rows to data.prepare using shop, "SELECT id, message FROM notifications ORDER BY id", record {params: record {}, columns: record {id: "text", message: "text"}}
make ingest using event
  set db to data.open using "shop"
  set insert_row to data.prepare using db, "INSERT INTO notifications (id, message) VALUES (:id, :message)", record {params: record {id: "text", message: "text"}, columns: record {}}
  call data.execute using insert_row, record {id: event.id, message: event.payload.message}, event.transaction
  call events.enqueue using event.transaction, "jobs", "reference", "send", event.payload, event.id
  call events.complete using event.transaction, event
  give back nothing
end
route "GET" "/catalog" using request
  set hit to cache.get using catalog_cache, "rows"
  if hit.found
    give back api.respond using "catalog", 200, hit.value
  end
  set values to data.query using rows, record {}
  call cache.set using catalog_cache, "rows", values, 1000
  give back api.respond using "catalog", 200, values
end
`)...)
	// Explicit cache invalidation occurs after durable outbox delivery, before the
	// transient notification. Reconnect still uses authoritative HTTP data.
	source = bytes.Replace(source, []byte(`  call sockets.publish using "updates", "demo", "Catalog changed; refetch /"`), []byte("  call cache.delete using catalog_cache, \"rows\"\n  call sockets.publish using \"updates\", \"demo\", \"Catalog changed; refetch /\""), 1)
	// Imports must precede executable declarations.
	source = bytes.Replace(source, []byte("use server\n"), []byte("use server\nuse data\nuse events\nuse cache\nuse api\n"), 1)
	for _, module := range []string{"data", "events", "cache", "api"} {
		needle := []byte("\nuse " + module + "\n")
		first := bytes.Index(source, needle)
		if first >= 0 {
			tail := bytes.ReplaceAll(source[first+len(needle):], needle, []byte("\n"))
			source = append(source[:first+len(needle)], tail...)
		}
	}
	program := filepath.Join(root, "app.grab")
	os.WriteFile(program, source, 0600)
	contractRaw := []byte(`{"version":1,"types":{"Notice":{"type":"text","max_length":2048},"Row":{"type":"record","fields":[{"name":"id","schema":{"type":"text","max_length":128}},{"name":"message","schema":{"type":"text","max_length":1000}}]}},"operations":[{"id":"catalog","method":"GET","path":"/catalog","success":[{"status":200,"schema":{"type":"list","items":{"type":"ref","ref":"Row"},"max_count":100}}]}]}`)
	contract, e := contracts.Parse(contractRaw)
	if e != nil {
		t.Fatal(e)
	}
	contractPath := filepath.Join(root, "grab.api.json")
	if e = os.WriteFile(contractPath, contractRaw, 0600); e != nil {
		t.Fatal(e)
	}
	var target, socketTarget string
	if workers == 0 {
		dg, _ := database.Grant([]string{"shop:read-write"}, true)
		app, e := server.NewApplication(program, "", interpreter.Options{PlatformConfig: cfg, PlatformGrants: grants, DataGrants: dg, APIContract: contract}, server.DefaultConfig(), io.Discard)
		if e != nil {
			t.Fatal(e)
		}
		defer app.Close()
		srv := httptest.NewServer(app)
		defer srv.Close()
		target, socketTarget = srv.URL, srv.URL
	} else {
		binary := filepath.Join(root, "grab.exe")
		build := exec.Command("go", "build", "-o", binary, "./cmd/grab")
		build.Dir = ".."
		if out, e := build.CombinedOutput(); e != nil {
			t.Fatalf("%s: %v", out, e)
		}
		base := workerPorts(t, workers)
		args := []string{"serve", "--workers", strconv.Itoa(workers), "--worker-base-port", strconv.Itoa(base), "--platform-config", platformPath, "--api-contract", contractPath, "--db-config", dbPath, "--allow-db", "shop:read-write", "--allow-cache", "catalog:read-write", "--allow-events", "jobs:accept", "--allow-events", "jobs:work", "--allow-connector", "reference:send", "--allow-channel", "updates:connect", "--allow-channel", "updates:publish", program}
		cmd := exec.Command(binary, args...)
		log := &workerLog{}
		cmd.Stdout = log
		cmd.Stderr = log
		if e = cmd.Start(); e != nil {
			t.Fatal(e)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		defer func() {
			cmd.Process.Kill()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("supervisor cleanup timeout")
			}
			time.Sleep(6 * time.Second)
		}()
		target = fmt.Sprintf("http://127.0.0.1:%d", base)
		socketTarget = fmt.Sprintf("http://127.0.0.1:%d", base+workers-1)
		deadline := time.Now().Add(30 * time.Second)
		ready := false
		client := &http.Client{Timeout: time.Second}
		for time.Now().Before(deadline) {
			resp, e := client.Get(socketTarget + "/_ready")
			if e == nil {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					ready = true
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
		if !ready {
			t.Fatal("remote workers not ready", log.String())
		}
	}
	appURL.Store(target)
	ws, _, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(socketTarget, "http")+"/live", &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"http://127.0.0.1:8080"}, "Authorization": {"Bearer local-demo"}}})
	if e != nil {
		t.Fatal(e)
	}
	defer ws.CloseNow()
	body := []byte(`{"message":"exactly one durable row"}`)
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	send := func() int {
		request, _ := http.NewRequestWithContext(ctx, "POST", target+"/webhook", bytes.NewReader(body))
		request.Header.Set("Grab-Event-Id", "evt_workflow")
		request.Header.Set("Grab-Event-Time", stamp)
		request.Header.Set("Grab-Signature", connect.Sign("evt_workflow", stamp, body, []byte(os.Getenv("GRAB_FLOW_SIGNING"))))
		response, e := http.DefaultClient.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		response.Body.Close()
		return response.StatusCode
	}
	if status := send(); status != 202 {
		t.Fatal(status)
	}
	if status := send(); status != 200 {
		t.Fatal("duplicate not deduplicated", status)
	}
	_, notice, e := ws.Read(ctx)
	if e != nil || !bytes.Contains(notice, []byte("Catalog changed")) {
		t.Fatal(string(notice), e)
	}
	response, e := http.Get(target + "/catalog")
	if e != nil {
		t.Fatal(e)
	}
	wire, _ := io.ReadAll(response.Body)
	response.Body.Close()
	var rows []map[string]string
	if response.StatusCode != 200 || json.Unmarshal(wire, &rows) != nil || len(rows) != 1 || rows[0]["message"] != "exactly one durable row" {
		t.Fatal(response.Status, string(wire))
	}
}
