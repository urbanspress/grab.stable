package telemetry

import (
	"context"
	"grab/internal/platform"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTracePrivacyAndValidation(t *testing.T) {
	good := "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01"
	for _, tt := range []struct {
		raw string
		ok  bool
	}{{good, true}, {strings.ToUpper(good), false}, {"00-00000000000000000000000000000000-0123456789abcdef-01", false}, {good + " baggage=secret", false}, {good[:53] + "ff", false}} {
		_, _, ok := TraceParent(tt.raw)
		if ok != tt.ok {
			t.Fatal(tt)
		}
	}
	r := &Registry{}
	x := &exporter{rate: 0, queue: make(chan Span, 2048), registry: r}
	m := &Manager{Registry: r, exporters: []*exporter{x}}
	m.Record("/catalog/{id}", good, time.Now())
	if len(x.queue) != 0 {
		t.Fatal("remote sampler overrode local policy")
	}
	x.rate = 1
	for i := 0; i < 2050; i++ {
		m.Record("/catalog/{id}", good, time.Now())
	}
	if len(x.queue) != 2048 {
		t.Fatal("unbounded buffer")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if !strings.Contains(w.Body.String(), "grab_trace_drops 2") || !strings.HasSuffix(w.Body.String(), "# EOF\n") {
		t.Fatal(w.Body.String())
	}
	span := <-x.queue
	if span.ParentSpanID != "0123456789abcdef" || span.Name != "/catalog/{id}" {
		t.Fatal(span)
	}
}
func TestDefaultDenyAndCollectorSSRF(t *testing.T) {
	c, e := platform.Parse([]byte(`{"version":1,"telemetry":{"trace":{"driver":"otlp","endpoint":"https://127.0.0.1:4318/v1/traces","token_env":"DO_NOT_READ"}}}`), filepath.Join(t.TempDir(), "platform.json"))
	if e != nil {
		t.Fatal(e)
	}
	g, _ := c.Grant(nil)
	m, e := New(context.Background(), c, g, 0)
	if e != nil {
		t.Fatal("ungranted exporter resolved secret", e)
	}
	m.Close()
	if _, e = collectorClient(context.Background(), c.Telemetry()["trace"]); e == nil {
		t.Fatal("private collector without CIDR accepted")
	}
}
func TestExporterOutageDrops(t *testing.T) {
	r := &Registry{}
	ctx, cancel := context.WithCancel(context.Background())
	x := &exporter{client: &http.Client{}, queue: make(chan Span, 2048), registry: r}
	x.queue <- Span{Name: "/safe"}
	cancel()
	x.run(ctx)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(w.Body.String(), "grab_trace_drops 1") {
		t.Fatal(w.Body.String())
	}
}

func TestDurableAttemptLinksAreStableAndPrivate(t *testing.T) {
	r := &Registry{}
	x := &exporter{rate: 1, queue: make(chan Span, 2048), registry: r}
	m := &Manager{Registry: r, exporters: []*exporter{x}}
	r.tracer.Store(m)
	r.Work("inbox", "private-namespace:host-generated-random-id", 1, time.Now())
	first := <-x.queue
	r.Work("inbox", "private-namespace:host-generated-random-id", 2, time.Now())
	second := <-x.queue
	if first.TraceID != second.TraceID || first.SpanID == second.SpanID || len(first.Links) != 0 || len(second.Links) != 1 || second.Links[0].TraceID != first.TraceID || second.Links[0].SpanID != first.SpanID || second.Name != "inbox" || second.Kind != 5 {
		t.Fatal(first, second)
	}
	// A fresh process reconstructs the link using only the persisted random work
	// identity and attempt number. No provider identity or payload is exported.
	r.Work("inbox", "different-namespace:host-generated-random-id", 2, time.Now())
	third := <-x.queue
	if third.TraceID == first.TraceID {
		t.Fatal("namespace isolation")
	}
	r.Work("arbitrary-user-label", "secret", 2, time.Now())
	if len(x.queue) != 0 {
		t.Fatal("unbounded work label")
	}
}
