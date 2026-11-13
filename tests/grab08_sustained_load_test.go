package tests

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"grab/internal/connect"
	"grab/internal/data"
	"grab/internal/events"
	"grab/internal/platform"
)

// PhaseMetrics captures the measured performance and stability metrics for one worker configuration.
type PhaseMetrics struct {
	Workers          int
	Duration         time.Duration
	TotalRequests    int64
	SuccessRequests  int64
	DeduplicatedReqs int64
	FailedRequests   int64
	ThroughputRPS    float64
	P50LatencyMs     float64
	P95LatencyMs     float64
	P99LatencyMs     float64
	MaxLatencyMs     float64
	InitialHeapMB    float64
	FinalHeapMB      float64
	PeakHeapMB       float64
	NumGC            uint32
	WorkerRestarts   int
	FaultsInjected   int
	FaultsRecovered  int
	AcknowledgedRows int64
	CommittedRows    int64
}

// TestGRAB08SustainedFaultAndLoad executes the reproducible 30-minute 1/2/4-worker
// sustained fault and load test required by GRAB 0.8 Section 12 and GRAB 1.0 (spec 100).
func TestGRAB08SustainedFaultAndLoad(t *testing.T) {
	if os.Getenv("GRAB_TEST_SUSTAINED_LOAD") != "1" {
		t.Skip("skipping 30-minute sustained fault/load test; set GRAB_TEST_SUSTAINED_LOAD=1 to run")
	}
	if os.Getenv("GRAB_TEST_DATABASES") != "1" || os.Getenv("GRAB_TEST_REDIS_PORT") == "" {
		t.Skip("requires disposable PostgreSQL/MariaDB and Redis")
	}

	phaseDuration := 10 * time.Minute
	if rawDur := os.Getenv("GRAB_LOAD_PHASE_DURATION"); rawDur != "" {
		if d, err := time.ParseDuration(rawDur); err == nil && d > 0 {
			phaseDuration = d
		}
	}

	tool, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}

	binDir := t.TempDir()
	binary := filepath.Join(binDir, "grab.exe")
	buildCmd := exec.Command(tool, "build", "-trimpath", "-ldflags=-s -w", "-o", binary, "./cmd/grab")
	buildCmd.Dir = ".."
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %s: %v", out, err)
	}

	workerCounts := []int{1, 2, 4}
	var results []PhaseMetrics

	t.Logf("=== Starting GRAB 1.0 Sustained Fault/Load Test ===")
	t.Logf("Total Phases: %d (Workers: %v), Duration per phase: %v, Total expected: %v",
		len(workerCounts), workerCounts, phaseDuration, time.Duration(len(workerCounts))*phaseDuration)

	for _, w := range workerCounts {
		t.Logf(">>> Beginning Phase: %d worker(s) for %v", w, phaseDuration)
		metrics := runSustainedLoadPhase(t, binary, w, phaseDuration)
		results = append(results, metrics)
		t.Logf("<<< Completed Phase: %d worker(s) - RPS: %.2f, p50: %.2fms, p95: %.2fms, p99: %.2fms, Acked: %d, Committed: %d",
			w, metrics.ThroughputRPS, metrics.P50LatencyMs, metrics.P95LatencyMs, metrics.P99LatencyMs,
			metrics.AcknowledgedRows, metrics.CommittedRows)
	}

	printSummaryReport(t, results)
}

func runSustainedLoadPhase(t *testing.T, binary string, workers int, duration time.Duration) PhaseMetrics {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	redisPort, _ := strconv.Atoi(os.Getenv("GRAB_TEST_REDIS_PORT"))
	dbPort := 55432 // PostgreSQL
	dbEngine := "postgres"
	signingSecret := "sustained-load-signing-secret"
	flowKey := "sustained-load-key"

	t.Setenv("GRAB_FLOW_KEY", flowKey)
	t.Setenv("GRAB_FLOW_SIGNING", signingSecret)

	dbRaw := []byte(fmt.Sprintf(`{"connections":{"shop":{"driver":%q,"host":"127.0.0.1","port":%d,"database":"grab_test","user":"grab","password_env":"GRAB_TEST_DB_PASSWORD","tls":"disabled","pool":{"max_open":8}}}}`, dbEngine, dbPort))
	dbPath := filepath.Join(root, "grab.data.json")
	if err := os.WriteFile(dbPath, dbRaw, 0600); err != nil {
		t.Fatal(err)
	}

	database, err := data.ParseConfig(dbRaw, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	mg, _ := database.Grant([]string{"shop:migrate"}, false)
	manager := data.NewManager(mg, false)
	scope := manager.Scope()
	pool, err := scope.Open(ctx, "shop")
	if err != nil {
		t.Fatal(err)
	}
	db, err := scope.HostMaintenanceDatabase(pool)
	if err != nil {
		t.Fatal(err)
	}

	tblName := fmt.Sprintf("load_notifs_%d_%d", workers, time.Now().UnixNano()%100000)
	if _, err = db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tblName)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, fmt.Sprintf("CREATE TABLE %s (id VARCHAR(128) PRIMARY KEY, message TEXT NOT NULL, tenant VARCHAR(64) NOT NULL)", tblName)); err != nil {
		t.Fatal(err)
	}

	if err = scope.MigrateEvents(ctx, pool, events.MigrationSteps(dbEngine)); err != nil {
		t.Fatal(err)
	}
	namespace := fmt.Sprintf("ld%dw%d", workers, time.Now().UnixNano()%10000)
	store := &events.Store{DB: db, Driver: dbEngine, Config: platform.EventStore{Namespace: namespace}}
	if err = store.InitNamespace(ctx); err != nil {
		t.Fatal(err)
	}
	scope.Close()
	manager.Close()

	var deliveredCount int64
	var providerMu sync.Mutex
	deliveredMap := make(map[string]bool)

	var appBaseURL atomic.Value
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		key := r.Header.Get("Idempotency-Key")
		if r.Header.Get("Authorization") != "Bearer "+flowKey || key == "" {
			http.Error(w, "denied", 403)
			return
		}
		providerMu.Lock()
		already := deliveredMap[key]
		if !already {
			deliveredMap[key] = true
			atomic.AddInt64(&deliveredCount, 1)
		}
		providerMu.Unlock()

		if !already {
			u, _ := appBaseURL.Load().(string)
			if u != "" {
				notifyClient := &http.Client{Timeout: 2 * time.Second}
				req, _ := http.NewRequestWithContext(context.Background(), "POST", u+"/notify", nil)
				req.Header.Set("Authorization", "Bearer "+flowKey)
				if resp, err := notifyClient.Do(req); err == nil {
					resp.Body.Close()
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(raw)
	}))
	defer provider.Close()

	pkg, err := os.ReadFile("../examples/connectors/reference/connector.json")
	if err != nil {
		t.Fatal(err)
	}
	pkg = bytes.ReplaceAll(pkg, []byte("https://reference.example.invalid"), []byte(provider.URL))
	os.Mkdir(filepath.Join(root, "reference"), 0700)
	os.WriteFile(filepath.Join(root, "reference", "connector.json"), pkg, 0600)

	platRaw := fmt.Sprintf(`{
		"version": 1,
		"caches": {
			"catalog": {
				"driver": "redis",
				"namespace": "%s",
				"host": "127.0.0.1",
				"port": %d,
				"tls": "disabled",
				"development": true,
				"pool_size": 4
			}
		},
		"connectors": {
			"reference": {
				"package": "reference",
				"version": "1.0.0",
				"development": true,
				"secrets": {"api_key": "GRAB_FLOW_KEY"}
			}
		},
		"events": {
			"jobs": {
				"database": "shop",
				"namespace": "%s",
				"concurrency": 4
			}
		},
		"webhooks": {
			"inbound": {
				"path": "/webhook",
				"connector": "reference",
				"store": "jobs",
				"tenant": "demo",
				"handler": "ingest",
				"keys": ["GRAB_FLOW_SIGNING"],
				"type": "notification"
			}
		},
		"channels": {
			"updates": {
				"path": "/live",
				"authorize": "authorize",
				"receive": "receive",
				"publish_authorize": "publish_authorize",
				"client_type": "Notice",
				"server_type": "Notice",
				"origins": ["http://127.0.0.1:8080"],
				"development": true,
				"broker": {
					"driver": "redis",
					"namespace": "%s",
					"host": "127.0.0.1",
					"port": %d,
					"tls": "disabled",
					"development": true,
					"pool_size": 4
				}
			}
		},
		"workers": {
			"max_db_connections": 256,
			"max_redis_connections": 256,
			"db_headroom": 8,
			"redis_headroom": 8
		}
	}`, namespace, redisPort, namespace, namespace, redisPort)

	platformPath := filepath.Join(root, "grab.platform.json")
	if err := os.WriteFile(platformPath, []byte(platRaw), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := platform.Parse([]byte(platRaw), platformPath)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := connect.BuildLock(cfg)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "grab.connect.lock"), lock, 0600)

	appSource := fmt.Sprintf(`
use server
use data
use events
use cache
use api
use sockets

set catalog_cache to cache.open using "catalog"
set shop to data.open using "shop"
set rows to data.prepare using shop, "SELECT id, message, tenant FROM %s ORDER BY id LIMIT 50", record {params: record {}, columns: record {id: "text", message: "text", tenant: "text"}}

make authorize using req
  give back record {allowed: yes, principal: "demo_user", audiences: ["demo"], expires_at: "2099-01-01T00:00:00Z"}
end

make receive using msg
  give back nothing
end

make publish_authorize using ctx
  give back yes
end

make ingest using event
  set db to data.open using "shop"
  set insert_row to data.prepare using db, "INSERT INTO %s (id, message, tenant) VALUES (:id, :message, :tenant)", record {params: record {id: "text", message: "text", tenant: "text"}, columns: record {}}
  call data.execute using insert_row, record {id: event.id, message: event.payload.message, tenant: event.tenant}, event.transaction
  call events.enqueue using event.transaction, "jobs", "reference", "send", event.payload, event.id
  call events.complete using event.transaction, event
  give back nothing
end

route "POST" "/notify" using request
  call cache.delete using catalog_cache, "rows"
  call sockets.publish using "updates", "demo", "Catalog changed"
  give back server.json using 202, record {status: "notified"}
end

route "GET" "/catalog" using request
  set hit to cache.get using catalog_cache, "rows"
  if hit.found
    give back api.respond using "catalog", 200, hit.value
  end
  set values to data.query using rows, record {}
  call cache.set using catalog_cache, "rows", values, 5000
  give back api.respond using "catalog", 200, values
end

route "GET" "/_health"
  give back server.json using 200, record {status: "ok"}
end
`, tblName, tblName)

	programPath := filepath.Join(root, "app.grab")
	if err := os.WriteFile(programPath, []byte(appSource), 0600); err != nil {
		t.Fatal(err)
	}

	contractRaw := []byte(`{
		"version": 1,
		"types": {
			"Notice": {"type": "text", "max_length": 2048},
			"Row": {
				"type": "record",
				"fields": [
					{"name": "id", "schema": {"type": "text", "max_length": 128}},
					{"name": "message", "schema": {"type": "text", "max_length": 1000}},
					{"name": "tenant", "schema": {"type": "text", "max_length": 64}}
				]
			}
		},
		"operations": [
			{
				"id": "catalog",
				"method": "GET",
				"path": "/catalog",
				"success": [
					{"status": 200, "schema": {"type": "list", "items": {"type": "ref", "ref": "Row"}, "max_count": 500}}
				]
			}
		]
	}`)
	contractPath := filepath.Join(root, "grab.api.json")
	if err := os.WriteFile(contractPath, contractRaw, 0600); err != nil {
		t.Fatal(err)
	}

	basePort := workerPorts(t, workers)
	args := []string{
		"serve",
		"--addr", fmt.Sprintf("127.0.0.1:%d", basePort),
		"--workers", strconv.Itoa(workers),
		"--worker-base-port", strconv.Itoa(basePort),
		"--platform-config", platformPath,
		"--api-contract", contractPath,
		"--db-config", dbPath,
		"--allow-db", "shop:read-write",
		"--allow-cache", "catalog:read-write",
		"--allow-events", "jobs:accept",
		"--allow-events", "jobs:work",
		"--allow-connector", "reference:send",
		"--allow-channel", "updates:connect",
		"--allow-channel", "updates:publish",
		programPath,
	}

	supervisorCmd := exec.Command(binary, args...)
	supLog := &workerLog{}
	supervisorCmd.Stdout = supLog
	supervisorCmd.Stderr = supLog

	if err := supervisorCmd.Start(); err != nil {
		t.Fatalf("failed to start supervisor: %v", err)
	}
	supDone := make(chan error, 1)
	go func() { supDone <- supervisorCmd.Wait() }()

	defer func() {
		_ = supervisorCmd.Process.Kill()
		matches := regexp.MustCompile(`worker [0-9]+ pid=([0-9]+)`).FindAllStringSubmatch(supLog.String(), -1)
		for _, m := range matches {
			if len(m) > 1 {
				if pid, err := strconv.Atoi(m[1]); err == nil {
					if p, err := os.FindProcess(pid); err == nil {
						_ = p.Kill()
					}
				}
			}
		}
		select {
		case <-supDone:
		case <-time.After(5 * time.Second):
		}
		time.Sleep(2 * time.Second)
	}()

	targetBaseURL := fmt.Sprintf("http://127.0.0.1:%d", basePort)
	lastWorkerURL := fmt.Sprintf("http://127.0.0.1:%d", basePort+workers-1)
	appBaseURL.Store(targetBaseURL)

	// Wait for workers to become ready
	readyDeadline := time.Now().Add(30 * time.Second)
	ready := false
	httpClient := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(readyDeadline) {
		resp, err := httpClient.Get(lastWorkerURL + "/_ready")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("workers did not become ready in 30s: %s", supLog.String())
	}

	// Connect WebSocket client
	wsCtx, wsCancel := context.WithCancel(ctx)
	defer wsCancel()
	ws, _, err := websocket.Dial(wsCtx, "ws"+strings.TrimPrefix(targetBaseURL, "http")+"/live", &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Origin":        {"http://127.0.0.1:8080"},
			"Authorization": {"Bearer " + flowKey},
		},
	})
	if err != nil {
		t.Fatalf("failed to connect websocket: %v", err)
	}
	defer ws.CloseNow()

	var wsNoticesCount int64
	go func() {
		for {
			_, _, err := ws.Read(wsCtx)
			if err != nil {
				return
			}
			atomic.AddInt64(&wsNoticesCount, 1)
		}
	}()

	// Metric counters
	var totalReqs, successReqs, dedupReqs, failedReqs int64
	var latenciesMu sync.Mutex
	var latenciesMs []float64

	var initialHeap, finalHeap, peakHeap float64
	var m runtimeMemStats
	readMem(&m)
	initialHeap = m.HeapAllocMB
	peakHeap = m.HeapAllocMB

	// Shared keep-alive client to prevent Windows ephemeral port exhaustion under sustained load
	sharedTransport := &http.Transport{
		MaxIdleConns:        256,
		MaxIdleConnsPerHost: 64,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false,
	}
	sharedClient := &http.Client{
		Transport: sharedTransport,
		Timeout:   5 * time.Second,
	}
	defer sharedTransport.CloseIdleConnections()

	// Warmup
	warmupDuration := 30 * time.Second
	if duration <= 30*time.Second {
		warmupDuration = 2 * time.Second
	}
	t.Logf("[%d-worker] Starting %v warmup...", workers, warmupDuration)
	warmupDeadline := time.Now().Add(warmupDuration)
	runTraffic(ctx, warmupDeadline, sharedClient, targetBaseURL, signingSecret, workers, basePort, 1, nil, nil, nil, nil, nil)
	t.Logf("[%d-worker] Warmup finished. Starting measured sustained load for %v...", workers, duration)

	var faultsInjected, faultsRecovered int
	startTime := time.Now()
	endTime := startTime.Add(duration)

	faultInjected := false
	halfwayTime := startTime.Add(duration / 2)

	// Concurrency workers
	clientConcurrency := 4
	var wg sync.WaitGroup
	wg.Add(clientConcurrency)

	for c := 0; c < clientConcurrency; c++ {
		go func(clientID int) {
			defer wg.Done()
			runTraffic(ctx, endTime, sharedClient, targetBaseURL, signingSecret, workers, basePort, clientID,
				&totalReqs, &successReqs, &dedupReqs, &failedReqs, func(dur time.Duration) {
					ms := float64(dur.Microseconds()) / 1000.0
					latenciesMu.Lock()
					if len(latenciesMs) < 1000000 {
						latenciesMs = append(latenciesMs, ms)
					}
					latenciesMu.Unlock()
				})
		}(c)
	}

	// Status and fault monitor ticker
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

MonitorLoop:
	for {
		select {
		case <-ctx.Done():
			break MonitorLoop
		case now := <-ticker.C:
			if now.After(endTime) {
				break MonitorLoop
			}
			elapsed := now.Sub(startTime)
			readMem(&m)
			if m.HeapAllocMB > peakHeap {
				peakHeap = m.HeapAllocMB
			}
			curTot := atomic.LoadInt64(&totalReqs)
			curSucc := atomic.LoadInt64(&successReqs)
			curFail := atomic.LoadInt64(&failedReqs)
			curWS := atomic.LoadInt64(&wsNoticesCount)

			t.Logf("[%d-worker] Elapsed: %v/%v | Reqs: %d (Succ: %d, Fail: %d) | WS notices: %d | Heap: %.1f MB (Sys: %.1f MB) | GC: %d",
				workers, elapsed.Round(time.Second), duration, curTot, curSucc, curFail, curWS, m.HeapAllocMB, m.SysMB, m.NumGC)

			// Fault injection at halfway mark
			if !faultInjected && now.After(halfwayTime) && workers > 1 {
				faultInjected = true
				faultsInjected++
				t.Logf("[%d-worker] === INJECTING FAULT: Killing worker 0 process under load ===", workers)
				matches := regexp.MustCompile(`worker 0 pid=([0-9]+)`).FindAllStringSubmatch(supLog.String(), -1)
				if len(matches) > 0 {
					pid, _ := strconv.Atoi(matches[len(matches)-1][1])
					if child, err := os.FindProcess(pid); err == nil {
						_ = child.Kill()
						t.Logf("[%d-worker] Sent SIGKILL to worker 0 (pid=%d). Observing supervisor auto-restart...", workers, pid)
						// Verify worker 0 recovers within 15 seconds
						recovered := false
						recDeadline := time.Now().Add(15 * time.Second)
						for time.Now().Before(recDeadline) {
							resp, err := httpClient.Get(fmt.Sprintf("http://127.0.0.1:%d/_health", basePort))
							if err == nil {
								resp.Body.Close()
								if resp.StatusCode == 200 {
									recovered = true
									break
								}
							}
							time.Sleep(200 * time.Millisecond)
						}
						if recovered {
							faultsRecovered++
							t.Logf("[%d-worker] === FAULT RECOVERED: Supervisor restarted worker 0 cleanly ===", workers)
						} else {
							t.Errorf("[%d-worker] Worker 0 failed to recover within 15s", workers)
						}
					}
				}
			}
		}
	}

	wg.Wait()
	actualDuration := time.Since(startTime)

	readMem(&m)
	finalHeap = m.HeapAllocMB

	// Reconcile database state
	dg, _ := database.Grant([]string{"shop:migrate"}, false)
	mgr := data.NewManager(dg, false)
	sc := mgr.Scope()
	p, err := sc.Open(context.Background(), "shop")
	if err != nil {
		t.Fatalf("failed to open database for reconciliation: %v", err)
	}
	dbCheck, err := sc.HostMaintenanceDatabase(p)
	if err != nil {
		t.Fatalf("failed to get maintenance database: %v", err)
	}
	var committedCount int64
	row := dbCheck.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tblName))
	if err := row.Scan(&committedCount); err != nil {
		t.Fatalf("failed to count committed rows: %v", err)
	}
	sc.Close()
	mgr.Close()

	// Calculate latency percentiles
	latenciesMu.Lock()
	sort.Float64s(latenciesMs)
	p50, p95, p99, maxLat := 0.0, 0.0, 0.0, 0.0
	if n := len(latenciesMs); n > 0 {
		p50 = latenciesMs[int(math.Floor(0.50*float64(n-1)))]
		p95 = latenciesMs[int(math.Floor(0.95*float64(n-1)))]
		p99 = latenciesMs[int(math.Floor(0.99*float64(n-1)))]
		maxLat = latenciesMs[n-1]
	}
	latenciesMu.Unlock()

	rps := float64(totalReqs) / actualDuration.Seconds()

	// Assertions for Section 12 gate
	if failedReqs > (totalReqs / 50) { // >= 98% success rate required even under active process SIGKILL fault injection
		t.Errorf("[%d-worker] Excessive failed requests: %d / %d", workers, failedReqs, totalReqs)
	}
	if deliveredCount == 0 {
		t.Errorf("[%d-worker] Zero webhook rows delivered", workers)
	}
	if committedCount < deliveredCount {
		t.Errorf("[%d-worker] DATA LOSS: committed count %d < delivered count %d", workers, committedCount, deliveredCount)
	}

	return PhaseMetrics{
		Workers:          workers,
		Duration:         actualDuration,
		TotalRequests:    totalReqs,
		SuccessRequests:  successReqs,
		DeduplicatedReqs: dedupReqs,
		FailedRequests:   failedReqs,
		ThroughputRPS:    rps,
		P50LatencyMs:     p50,
		P95LatencyMs:     p95,
		P99LatencyMs:     p99,
		MaxLatencyMs:     maxLat,
		InitialHeapMB:    initialHeap,
		FinalHeapMB:      finalHeap,
		PeakHeapMB:       peakHeap,
		NumGC:            m.NumGC,
		FaultsInjected:   faultsInjected,
		FaultsRecovered:  faultsRecovered,
		AcknowledgedRows: deliveredCount,
		CommittedRows:    committedCount,
	}
}

func runTraffic(ctx context.Context, until time.Time, client *http.Client, baseURL, signingSecret string, workers, basePort, clientID int,
	total, success, dedup, failed *int64, recordLat func(time.Duration)) {

	counter := 0
	for time.Now().Before(until) && ctx.Err() == nil {
		counter++
		port := basePort + (counter % workers)
		target := fmt.Sprintf("http://127.0.0.1:%d", port)

		start := time.Now()
		var err error
		var status int

		if clientID == 0 {
			// Ingest pipeline: streams durable webhook events and test deduplication
			isDup := (counter % 5 == 0)
			var evtID string
			if isDup {
				evtID = fmt.Sprintf("evt_dup_%d", counter%3)
			} else {
				evtID = fmt.Sprintf("evt_item_%d_%d", counter, time.Now().UnixNano())
			}

			// Webhooks retry transient 503 up to 3 times
			for attempt := 0; attempt < 3; attempt++ {
				status, err = sendWebhook(client, target, signingSecret, evtID, "durable row payload", "demo")
				if err == nil && (status == 200 || status == 202) {
					break
				}
				time.Sleep(15 * time.Millisecond)
			}

			if err == nil && (status == 200 || status == 202) {
				if status == 200 && dedup != nil {
					atomic.AddInt64(dedup, 1)
				}
				if success != nil {
					atomic.AddInt64(success, 1)
				}
			} else if failed != nil {
				atomic.AddInt64(failed, 1)
			}
			time.Sleep(30 * time.Millisecond) // Paced event ingestion
		} else {
			// Query pipeline: concurrent cached catalog reads
			req, _ := http.NewRequestWithContext(ctx, "GET", target+"/catalog", nil)
			resp, rerr := client.Do(req)
			if rerr == nil {
				status = resp.StatusCode
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if status == 200 {
					if success != nil {
						atomic.AddInt64(success, 1)
					}
				} else if failed != nil {
					atomic.AddInt64(failed, 1)
				}
			} else {
				err = rerr
				if failed != nil {
					atomic.AddInt64(failed, 1)
				}
			}
			time.Sleep(15 * time.Millisecond) // Smooth concurrency pacing without socket starvation
		}

		dur := time.Since(start)
		if recordLat != nil {
			recordLat(dur)
		}
		if total != nil {
			atomic.AddInt64(total, 1)
		}
	}
}

func sendWebhook(client *http.Client, target, secret, evtID, message, tenant string) (int, error) {
	body := []byte(fmt.Sprintf(`{"message":%q}`, message))
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	sig := connect.Sign(evtID, stamp, body, []byte(secret))

	req, err := http.NewRequest("POST", target+"/webhook", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Grab-Event-Id", evtID)
	req.Header.Set("Grab-Event-Time", stamp)
	req.Header.Set("Grab-Signature", sig)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

type runtimeMemStats struct {
	HeapAllocMB float64
	SysMB       float64
	NumGC       uint32
}

func readMem(out *runtimeMemStats) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	out.HeapAllocMB = float64(m.HeapAlloc) / 1024.0 / 1024.0
	out.SysMB = float64(m.Sys) / 1024.0 / 1024.0
	out.NumGC = m.NumGC
}

func printSummaryReport(t *testing.T, results []PhaseMetrics) {
	t.Logf("\n=================== 30-MINUTE SUSTAINED LOAD & FAULT SCORECARD ===================")
	t.Logf("| Workers | Duration | Total Reqs | Success | Deduplicated | Failed | RPS    | p50 (ms) | p95 (ms) | p99 (ms) | Max (ms) | Acked Rows | Committed | Fault Recovery |")
	t.Logf("|---------|----------|------------|---------|--------------|--------|--------|----------|----------|----------|----------|------------|-----------|----------------|")
	for _, r := range results {
		faultStr := fmt.Sprintf("%d/%d", r.FaultsRecovered, r.FaultsInjected)
		if r.FaultsInjected == 0 {
			faultStr = "N/A"
		}
		t.Logf("| %-7d | %-8v | %-10d | %-7d | %-12d | %-6d | %-6.1f | %-8.2f | %-8.2f | %-8.2f | %-8.2f | %-10d | %-9d | %-14s |",
			r.Workers, r.Duration.Round(time.Second), r.TotalRequests, r.SuccessRequests, r.DeduplicatedReqs,
			r.FailedRequests, r.ThroughputRPS, r.P50LatencyMs, r.P95LatencyMs, r.P99LatencyMs, r.MaxLatencyMs,
			r.AcknowledgedRows, r.CommittedRows, faultStr)
	}
	t.Logf("===================================================================================\n")
}
