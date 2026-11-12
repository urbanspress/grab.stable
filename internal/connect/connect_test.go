package connect

import (
	"context"
	"encoding/json"
	"errors"
	"grab/internal/platform"
	rt "grab/internal/runtime"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type oneListener struct {
	conn net.Conn
	done chan struct{}
}

func (l *oneListener) Accept() (net.Conn, error) {
	if l.conn != nil {
		c := l.conn
		l.conn = nil
		return c, nil
	}
	<-l.done
	return nil, net.ErrClosed
}
func (l *oneListener) Close() error {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	return nil
}
func (l *oneListener) Addr() net.Addr { return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 80} }
func PipeDial(handler http.Handler) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		client, server := net.Pipe()
		listener := &oneListener{conn: server, done: make(chan struct{})}
		srv := &http.Server{Handler: handler}
		go func() { srv.Serve(listener) }()
		go func() {
			select {
			case <-ctx.Done():
				srv.Close()
				listener.Close()
			case <-listener.done:
			}
		}()
		return client, nil
	}
}
func TestReferenceReuseIntegrityAndTransport(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "reference"), 0700)
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
	raw := `{"version":1,"connectors":{"app-a":{"package":"reference","version":"1.0.0","development":true,"secrets":{"api_key":"GRAB_REF_A"}},"app-b":{"package":"reference","version":"1.0.0","development":true,"secrets":{"api_key":"GRAB_REF_B"}}}}`
	cfg, e := platform.Parse([]byte(raw), filepath.Join(dir, "grab.platform.json"))
	if e != nil {
		t.Fatal(e)
	}
	lock, e := BuildLock(cfg)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(dir, "grab.connect.lock"), lock, 0600)
	packages, e := Load(cfg)
	if e != nil {
		t.Fatal(e)
	}
	g, e := cfg.Grant(map[string][]string{"connector": {"app-a:send", "app-b:send", "app-a:lookup"}})
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("GRAB_REF_A", "tenant-a-secret")
	t.Setenv("GRAB_REF_B", "tenant-b-secret")
	h := &Harness{}
	m := &Manager{Packages: packages, Grants: g, DialContext: PipeDial(h)}
	input := rt.Record([]rt.Entry{{Name: "message", Value: rt.Text("hello")}})
	for _, alias := range []string{"app-a", "app-a", "app-b"} {
		r, e := m.Call(context.Background(), alias, "send", input, "same-key")
		if e != nil || !r.Value.Equal(input) {
			t.Fatal(r, e)
		}
	}
	if h.Calls != 2 {
		t.Fatal("tenant idempotency isolation", h.Calls)
	}
	if _, e = m.Call(context.Background(), "app-b", "lookup", rt.Record([]rt.Entry{{Name: "id", Value: rt.Text("x")}}), ""); !errors.Is(e, platform.ErrDenied) {
		t.Fatal(e)
	}
	for _, bad := range []string{".", ".."} {
		if _, e = m.Call(context.Background(), "app-a", "lookup", rt.Record([]rt.Entry{{Name: "id", Value: rt.Text(bad)}}), ""); e == nil {
			t.Fatal("dot path accepted")
		}
	}
	os.WriteFile(filepath.Join(dir, "reference", "README"), []byte("tampered"), 0600)
	if _, e = Load(cfg); e == nil {
		t.Fatal("integrity bypass")
	}
}
func TestWebhookProfile(t *testing.T) {
	body := []byte("{ \"message\":\"hello\" }\n")
	key := []byte("reference-rotation-key-1")
	now := time.Unix(1804953600, 0)
	stamp := strconv.FormatInt(now.Unix(), 10)
	headers := http.Header{}
	headers.Set("Grab-Event-Id", "evt_1")
	headers.Set("Grab-Event-Time", stamp)
	headers.Set("Grab-Signature", Sign("evt_1", stamp, body, key))
	for _, tc := range []struct {
		name   string
		change func(http.Header)
		body   []byte
		at     time.Time
		valid  bool
	}{
		{"raw", func(http.Header) {}, body, now, true}, {"rotation", func(http.Header) {}, body, now, true}, {"tamper", func(http.Header) {}, []byte(`{"message":"hello"}`), now, false}, {"replay", func(http.Header) {}, body, now.Add(301 * time.Second), false}, {"future", func(http.Header) {}, body, now.Add(-301 * time.Second), false}, {"duplicate", func(h http.Header) { h.Add("Grab-Signature", h.Get("Grab-Signature")) }, body, now, false}, {"encoding", func(h http.Header) { h.Set("Content-Encoding", "gzip") }, body, now, false}, {"bad ID", func(h http.Header) { h.Set("Grab-Event-Id", "../evt") }, body, now, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := headers.Clone()
			tc.change(h)
			keys := [][]byte{key}
			if tc.name == "rotation" {
				keys = [][]byte{[]byte("reference-rotation-key-0"), key}
			}
			_, e := Verify(h, tc.body, keys, tc.at)
			if (e == nil) != tc.valid {
				t.Fatal(e)
			}
		})
	}
}
func TestManifestRejectsUnsafeDeclarations(t *testing.T) {
	b, _ := os.ReadFile("../../examples/connectors/reference/connector.json")
	for _, mutate := range []func(map[string]any){func(m map[string]any) { m["script"] = "run" }, func(m map[string]any) { m["origin"] = "http://example.com" }, func(m map[string]any) {
		m["operations"].(map[string]any)["send"].(map[string]any)["headers"] = map[string]string{"Host": "evil"}
	}, func(m map[string]any) {
		m["operations"].(map[string]any)["send"].(map[string]any)["path"] = "/../escape"
	}} {
		var m map[string]any
		json.Unmarshal(b, &m)
		mutate(m)
		raw, _ := json.Marshal(m)
		if _, e := Parse(raw, platform.Connector{Version: "1.0.0", Secrets: map[string]string{"api_key": "TEST_KEY"}}); e == nil {
			t.Fatal(string(raw))
		}
	}
	for _, address := range []string{"169.254.169.254", "127.0.0.1", "10.0.0.1", "0.0.0.0"} {
		if !blocked(net.ParseIP(address), platform.Connector{}) {
			t.Fatal("SSRF allowed", address)
		}
	}
}

var _ io.Reader
