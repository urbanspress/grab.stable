package server

import (
	"context"
	"fmt"
	"grab/internal/connect"
	"grab/internal/data"
	"grab/internal/events"
	"grab/internal/interpreter"
	"grab/internal/platform"
	rt "grab/internal/runtime"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func eventApp(t *testing.T, work bool) (*Application, *events.Store, *connect.Harness) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "reference"), 0700)
	for _, name := range []string{"connector.json", "README", "vectors.json"} {
		b, e := os.ReadFile(filepath.Join("../../examples/connectors/reference", name))
		if e != nil {
			t.Fatal(e)
		}
		if name == "connector.json" {
			b = []byte(strings.ReplaceAll(string(b), "https://reference.example.invalid", "http://127.0.0.1"))
		}
		os.WriteFile(filepath.Join(dir, "reference", name), b, 0600)
	}
	raw := `{"version":1,"connectors":{"reference":{"package":"reference","version":"1.0.0","development":true,"secrets":{"api_key":"GRAB_E2E_API_KEY"}}},"events":{"queue":{"database":"shop","namespace":"e2e","max_pending":2,"concurrency":1}},"webhooks":{"a":{"path":"/hooks/a","connector":"reference","store":"queue","tenant":"a","handler":"receive","keys":["GRAB_E2E_HMAC"],"type":"notification"},"b":{"path":"/hooks/b","connector":"reference","store":"queue","tenant":"b","handler":"receive","keys":["GRAB_E2E_HMAC"],"type":"notification"}}}`
	if work {
		raw = strings.ReplaceAll(raw, `"max_pending":2`, `"max_pending":10`)
	}
	cfg, e := platform.Parse([]byte(raw), filepath.Join(dir, "grab.platform.json"))
	if e != nil {
		t.Fatal(e)
	}
	lock, e := connect.BuildLock(cfg)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(dir, "grab.connect.lock"), lock, 0600)
	dbcfg, e := data.ParseConfig([]byte(`{"connections":{"shop":{"driver":"sqlite","path":"events.db"}}}`), filepath.Join(dir, "grab.data.json"))
	if e != nil {
		t.Fatal(e)
	}
	mg, e := dbcfg.Grant([]string{"shop:migrate"}, false)
	if e != nil {
		t.Fatal(e)
	}
	m := data.NewManager(mg, false)
	sc := m.Scope()
	pool, e := sc.Open(ctx, "shop")
	if e != nil {
		t.Fatal(e)
	}
	if e = sc.MigrateEvents(ctx, pool, events.MigrationSteps("sqlite")); e != nil {
		t.Fatal(e)
	}
	versions, e := data.MigrationManifest(rt.List([]rt.Value{rt.Record([]rt.Entry{{Name: "version", Value: rt.IntegerFromInt64(1)}, {Name: "name", Value: rt.Text("business")}, {Name: "steps", Value: rt.List([]rt.Value{rt.Text("CREATE TABLE effects (id TEXT PRIMARY KEY, tenant TEXT NOT NULL)")})}})}), "sqlite")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = sc.Migrate(ctx, pool, versions); e != nil {
		t.Fatal(e)
	}
	db, e := sc.HostMaintenanceDatabase(pool)
	if e != nil {
		t.Fatal(e)
	}
	store := &events.Store{DB: db, Driver: "sqlite", Config: cfg.Events()["queue"]}
	if e = store.InitNamespace(ctx); e != nil {
		t.Fatal(e)
	}
	sc.Close()
	m.Close()
	dg, e := dbcfg.Grant([]string{"shop:read-write"}, true)
	if e != nil {
		t.Fatal(e)
	}
	flags := map[string][]string{"connector": {"reference:send"}, "events": {"queue:accept"}}
	if work {
		flags["events"] = append(flags["events"], "queue:work")
	}
	pg, e := cfg.Grant(flags)
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("GRAB_E2E_API_KEY", "reference-tenant-credential")
	t.Setenv("GRAB_E2E_HMAC", "reference-event-signing-key")
	h := &connect.Harness{}
	program := filepath.Join(dir, "app.grab")
	os.WriteFile(program, []byte(`use data
use events
make receive using event
 set db to data.open using "shop"
 set insert to data.prepare using db, "INSERT INTO effects(id,tenant) VALUES(:id,:tenant)", record {params: record {id: "text", tenant: "text"}, columns: record {}}
 call data.execute using insert, record {id: event.id, tenant: event.tenant}, event.transaction
 set outgoing to events.enqueue using event.transaction, "queue", "reference", "send", event.payload, event.id
 call events.complete using event.transaction, event
end
route "GET" "/"
 give back "ready"
end
`), 0600)
	app, e := NewApplication(program, "", interpreter.Options{PlatformConfig: cfg, PlatformGrants: pg, DataGrants: dg, DialContext: h.DialContext}, DefaultConfig(), io.Discard)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(app.Close)
	observer := data.NewManager(dg, false)
	scope := observer.Scope()
	p, e := scope.Open(ctx, "shop")
	if e != nil {
		t.Fatal(e)
	}
	store.DB, e = scope.HostDatabase(p)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { scope.Close(); observer.Close() })
	return app, store, h
}
func signed(a *Application, path, id string, body []byte) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(string(body)))
	r.Header.Set("Content-Type", "application/json")
	stamp := fmt.Sprint(time.Now().Unix())
	r.Header.Set("Grab-Event-Id", id)
	r.Header.Set("Grab-Event-Time", stamp)
	r.Header.Set("Grab-Signature", connect.Sign(id, stamp, body, []byte("reference-event-signing-key")))
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w
}
func TestVerifiedWebhookBusinessOutboxEndToEnd(t *testing.T) {
	a, store, _ := eventApp(t, true)
	body := []byte(`{"message":"hello"}`)
	for _, path := range []string{"/hooks/a", "/hooks/b"} {
		w := signed(a, path, "evt_shared", body)
		if w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
		w = signed(a, path, "evt_shared", body)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := signed(a, "/hooks/a", "evt_shared", []byte(`{ "message":"hello" }`))
	if w.Code != 409 {
		t.Fatal("raw body identity mismatch", w.Code)
	}
	deadline := time.Now().Add(8 * time.Second)
	for {
		var effects, completed int
		store.DB.QueryRow("SELECT COUNT(*) FROM effects").Scan(&effects)
		store.DB.QueryRow("SELECT COUNT(*) FROM grab_events_work WHERE state='completed'").Scan(&completed)
		if effects == 2 && completed == 4 {
			break
		}
		if time.Now().After(deadline) {
			var states string
			rows, _ := store.DB.Query("SELECT kind,state,attempts FROM grab_events_work")
			if rows != nil {
				for rows.Next() {
					var k, s string
					var n int
					rows.Scan(&k, &s, &n)
					states += fmt.Sprint(k, s, n, " ")
				}
				rows.Close()
			}
			t.Fatalf("effects=%d completed=%d states=%s", effects, completed, states)
		}
		time.Sleep(20 * time.Millisecond)
	}
	r := httptest.NewRequest("POST", "/hooks/a", strings.NewReader(string(body)))
	r.Header.Set("Grab-Signature", "v1=bad")
	response := httptest.NewRecorder()
	a.ServeHTTP(response, r)
	if response.Code != http.StatusUnauthorized {
		t.Fatal(response.Code)
	}
}
func TestWebhookAcknowledgmentRequiresDurableAdmission(t *testing.T) {
	a, store, _ := eventApp(t, false)
	for i := 0; i < 3; i++ {
		w := signed(a, "/hooks/a", fmt.Sprint("evt_", i), []byte(`{"message":"hello"}`))
		want := 202
		if i == 2 {
			want = 503
		}
		if w.Code != want {
			t.Fatal(i, w.Code, w.Body.String())
		}
	}
	var count int
	store.DB.QueryRow("SELECT COUNT(*) FROM grab_events_work").Scan(&count)
	if count != 2 {
		t.Fatal(count)
	}
}
