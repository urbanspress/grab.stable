package telemetry

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"grab/internal/platform"
)

func TestVerifiedCollectorWireAndFailure(t *testing.T) {
	received := make(chan string, 1)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer host-test-token" || r.URL.Path != "/v1/traces" {
			http.Error(w, "bad authentication", 401)
			return
		}
		received <- string(raw)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	registry := &Registry{}
	x := &exporter{rate: 1, queue: make(chan Span, 2048), client: srv.Client(), registry: registry, token: "host-test-token", config: platform.Telemetry{Endpoint: srv.URL + "/v1/traces"}}
	m := &Manager{Registry: registry, exporters: []*exporter{x}}
	m.Record("/catalog/{id}", "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01", time.Now())
	x.flush(context.Background())
	select {
	case raw := <-received:
		if !strings.Contains(raw, `"resourceSpans"`) || !strings.Contains(raw, `"name":"/catalog/{id}"`) || strings.Contains(raw, "host-test-token") || strings.Contains(raw, "baggage") {
			t.Fatal(raw)
		}
	default:
		t.Fatal("no OTLP/HTTP payload")
	}
	// The verified transport refuses the fixture's untrusted certificate.
	d := platform.Telemetry{Endpoint: srv.URL + "/v1/traces", AllowedCIDRs: []string{"127.0.0.0/8"}}
	client, e := collectorClient(context.Background(), d)
	if e != nil {
		t.Fatal(e)
	}
	defer client.CloseIdleConnections()
	if resp, e := client.Get(d.Endpoint); e == nil {
		resp.Body.Close()
		t.Fatal("untrusted TLS collector accepted")
	}
	srv.Close()
	m.Record("/catalog/{id}", "", time.Now())
	x.flush(context.Background())
	if registry.values[15].Load() != 1 {
		t.Fatal("collector outage not counted", registry.values[15].Load())
	}
}
func TestMetricsAuthenticationAndSecretReload(t *testing.T) {
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	t.Setenv("GRAB_METRICS_TOKEN", strings.Repeat("a", 32))
	raw := fmt.Sprintf(`{"version":1,"telemetry":{"metrics":{"driver":"openmetrics","port":%d,"token_env":"GRAB_METRICS_TOKEN"}}}`, port)
	cfg, e := platform.Parse([]byte(raw), filepath.Join(t.TempDir(), "platform.json"))
	if e != nil {
		t.Fatal(e)
	}
	g, e := cfg.Grant(map[string][]string{"telemetry": {"metrics"}})
	if e != nil {
		t.Fatal(e)
	}
	m, e := New(context.Background(), cfg, g, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer m.Close()
	m.Registry.ConfigureRoutes([]string{"/catalog/{id}"})
	m.Registry.Observe("/catalog/private-user-id", 200, time.Millisecond)
	check := func(token string, want int) {
		t.Helper()
		request, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/metrics", port), nil)
		request.Header.Set("Authorization", "Bearer "+token)
		client := &http.Client{Timeout: time.Second}
		defer client.CloseIdleConnections()
		response, e := client.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != want || strings.Contains(string(body), "private-user-id") {
			t.Fatal(response.Status, string(body))
		}
	}
	check("", 401)
	check(strings.Repeat("a", 32), 200)
	t.Setenv("GRAB_METRICS_TOKEN", strings.Repeat("b", 32))
	commit, e := m.PrepareReload(context.Background(), cfg, g)
	if e != nil {
		t.Fatal(e)
	}
	check(strings.Repeat("a", 32), 200)
	commit()
	check(strings.Repeat("a", 32), 401)
	check(strings.Repeat("b", 32), 200)
	t.Setenv("GRAB_METRICS_TOKEN", "short")
	if _, e = m.PrepareReload(context.Background(), cfg, g); e == nil {
		t.Fatal("invalid secret reload accepted")
	}
	check(strings.Repeat("b", 32), 200)
}
func TestTraceBudgetSharedAcrossExporters(t *testing.T) {
	m := &Manager{Registry: &Registry{}}
	for i := 0; i < 2; i++ {
		m.exporters = append(m.exporters, &exporter{rate: 1, queue: make(chan Span, 2048)})
	}
	for i := 0; i < 1500; i++ {
		m.Record("/fixed", "", time.Now())
	}
	if total := len(m.exporters[0].queue) + len(m.exporters[1].queue); total != 2048 {
		t.Fatal(total)
	}
	// All tokens are atomically replaced without exposing their values in spans.
	pointer := &atomic.Pointer[string]{}
	token := "secret"
	pointer.Store(&token)
	if tokenValue(pointer, "") != "secret" {
		t.Fatal("token rotation")
	}
}
