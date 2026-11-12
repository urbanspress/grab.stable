package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"grab/internal/interpreter"
)

func appFile(t testing.TB, source string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "app.grab")
	if e := os.WriteFile(p, []byte(source), 0600); e != nil {
		t.Fatal(e)
	}
	return p
}
func application(t testing.TB, path string, c Config, output io.Writer) *Application {
	t.Helper()
	a, e := NewApplication(path, "", interpreter.Options{}, c, output)
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func request(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestApplicationRoutes(t *testing.T) {
	path := appFile(t, `use server
set count to 0
route "GET" "/items/{id}" using req
set count to count plus 1
give back server.json using 200, record {id: req.params.id, count: count, query: req.query at "q", header: req.headers at "x-test"}
end
route "GET" "/items/featured"
give back "featured"
end
route "POST" "/orders" using req
try
set order to server.parse_json using req
give back server.json using 201, order
catch err
give back server.json using 400, record {code: err.code}
end
end
route "POST" "/form" using req
give back server.json using 200, req.form
end
route "GET" "/go"
give back server.redirect using "/items/featured"
end
route "GET" "/empty"
end
route "GET" "/head"
give back server.text using 200, "get"
end
route "HEAD" "/head"
give back server.text using 202, "explicit"
end
`)
	a := application(t, path, DefaultConfig(), nil)
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
		want, allow              string
	}{
		{"priority", "GET", "/items/featured", "", 200, "featured", ""},
		{"HEAD fallback", "HEAD", "/items/featured", "", 200, "", ""},
		{"HEAD explicit", "HEAD", "/head", "", 202, "", ""},
		{"not found", "GET", "/missing", "", 404, "404 page not found\n", ""},
		{"method", "DELETE", "/items/featured", "", 405, "405 method not allowed\n", "GET, HEAD"},
		{"JSON exact", "POST", "/orders", `{"id":123456789012345678901234567890,"price":19.95}`, 201, `{"id":123456789012345678901234567890,"price":19.95}`, ""},
		{"bad JSON caught", "POST", "/orders", `{`, 400, `{"code":"GRAB-E312"}`, ""},
		{"invalid return", "GET", "/empty", "", 500, "GRAB-E312", ""},
		{"redirect", "GET", "/go", "", 302, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(a, tc.method, tc.path, tc.body)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.want) || w.Header().Get("Allow") != tc.allow {
				t.Fatalf("%d %v %q", w.Code, w.Header(), w.Body.String())
			}
			if tc.method == "HEAD" && w.Body.Len() != 0 {
				t.Fatal("HEAD body")
			}
			if w.Header().Get("X-Frame-Options") != "DENY" || w.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing safety headers")
			}
		})
	}
	for i := 0; i < 3; i++ {
		r := httptest.NewRequest("GET", "/items/caf%C3%A9?q=%20hello%20&q=second", nil)
		r.Header.Set("X-Test", " trimmed ")
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Body.String() != `{"id":"café","count":1,"query":" hello ","header":"trimmed"}` {
			t.Fatal(w.Body.String())
		}
	}
	r := httptest.NewRequest("POST", "/form", strings.NewReader("name=Alice+Smith&name=second"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Body.String() != `{"name":"Alice Smith"}` {
		t.Fatal(w.Body.String())
	}
}

func TestApplicationLoadOnceTemplatesAndReload(t *testing.T) {
	path := appFile(t, `use server
show "startup"
route "GET" "/" using req
give back server.html using "view.grab.html", record {title: req.query at "title"}
end
`)
	tp := filepath.Join(filepath.Dir(path), "view.grab.html")
	if e := os.WriteFile(tp, []byte("<h1>{title}</h1>"), 0600); e != nil {
		t.Fatal(e)
	}
	var logs bytes.Buffer
	a := application(t, path, DefaultConfig(), &logs)
	old := a.active.Load()
	if e := os.WriteFile(tp, []byte("<script>{title}</script>"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := a.Reload(); e == nil {
		t.Fatal("invalid template reload accepted")
	}
	if a.active.Load() != old {
		t.Fatal("lost last-known-good generation")
	}
	w := request(a, "GET", "/?title=%3Cscript%3E", "")
	if w.Code != 200 || w.Body.String() != "<h1>&lt;script&gt;</h1>" {
		t.Fatal(w.Body.String())
	}
	if strings.Count(logs.String(), "startup\n") != 1 {
		t.Fatal("startup reran on request or failed template reload")
	}
	if e := os.WriteFile(tp, []byte("<p>{title}</p>"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := a.Reload(); e != nil {
		t.Fatal(e)
	}
	if a.active.Load() == old {
		t.Fatal("generation not swapped")
	}
	if e := os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if e := os.Remove(tp); e != nil {
		t.Fatal(e)
	}
	w = request(a, "GET", "/?title=new", "")
	if w.Body.String() != "<p>new</p>" {
		t.Fatal(w.Body.String())
	}
}

func TestApplicationPreflightFailures(t *testing.T) {
	for _, tc := range []struct{ name, source, template string }{
		{"missing template", `use server
route "GET" "/"
give back server.html using "missing.grab.html", record {}
end`, ""},
		{"unsafe template", `use server
route "GET" "/"
give back server.html using "view.grab.html", record {x: "x"}
end`, "<a onclick=\"{x}\">x</a>"},
		{"startup failure", `set x to 1 divided by 0
route "GET" "/"
give back "ok"
end`, ""},
		{"path escape", `use server
route "GET" "/"
give back server.html using "../../escape.grab.html", record {}
end`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := appFile(t, tc.source+"\n")
			if tc.template != "" {
				if e := os.WriteFile(filepath.Join(filepath.Dir(p), "view.grab.html"), []byte(tc.template), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := NewApplication(p, "", interpreter.Options{}, DefaultConfig(), nil); e == nil {
				t.Fatal("preflight accepted invalid app")
			}
		})
	}
}

func TestApplicationLimitsProbesAndFaults(t *testing.T) {
	path := appFile(t, `route "POST" "/"
give back "ok"
end
route "GET" "/"
repeat 1000000 times
set x to 1
end
give back "done"
end
`)
	c := DefaultConfig()
	c.MaxConcurrency = 1
	c.MaxQueue = 0
	c.MaxBodyBytes = 3
	c.RequestTimeout = time.Nanosecond
	a := application(t, path, c, nil)
	for _, tc := range []struct {
		name, method, path, body string
		status                   int
	}{
		{"body limit", "POST", "/", "four", 413},
		{"invalid utf8", "POST", "/", string([]byte{255}), 400},
		{"deadline", "GET", "/", "", 504},
		{"liveness", "GET", "/_health", "", 200},
		{"head health", "HEAD", "/_health", "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(a, tc.method, tc.path, tc.body)
			if w.Code != tc.status {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
		})
	}
	a.slots <- struct{}{}
	if w := request(a, "POST", "/", ""); w.Code != 503 || w.Header().Get("Retry-After") != "1" {
		t.Fatal(w)
	}
	if w := request(a, "GET", "/_health", ""); w.Code != 200 {
		t.Fatal(w)
	}
	if w := request(a, "GET", "/_ready", ""); w.Code != 503 {
		t.Fatal(w)
	}
	<-a.slots
	a.draining.Store(true)
	if w := request(a, "GET", "/_ready", ""); w.Code != 503 {
		t.Fatal(w)
	}
	a.draining.Store(false)
	old := a.active.Swap(nil)
	w := request(a, "POST", "/", "")
	a.active.Store(old)
	if w.Code != 500 || !strings.Contains(w.Body.String(), "request_id=") {
		t.Fatal(w)
	}
	if w := request(a, "GET", "/_health", ""); w.Code != 200 {
		t.Fatal("panic crashed server")
	}
}

func TestBoundedQueue(t *testing.T) {
	c := DefaultConfig()
	c.MaxConcurrency = 1
	c.MaxQueue = 1
	c.QueueTimeout = 20 * time.Millisecond
	a := application(t, appFile(t, "route \"GET\" \"/\"\ngive back \"ok\"\nend\n"), c, nil)
	a.slots <- struct{}{}
	a.queue <- struct{}{}
	if a.acquire(context.Background()) {
		t.Fatal("overfull gate accepted request")
	}
	<-a.queue
	start := time.Now()
	if a.acquire(context.Background()) {
		t.Fatal("queue timeout accepted request")
	}
	if time.Since(start) < c.QueueTimeout {
		t.Fatal("did not wait")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if a.acquire(ctx) {
		t.Fatal("canceled waiter accepted")
	}
	<-a.slots
	if !a.acquire(context.Background()) {
		t.Fatal("available slot rejected")
	}
	<-a.slots
}

func TestTrustedProxyMetadata(t *testing.T) {
	path := appFile(t, `use server
route "GET" "/" using req
give back server.json using 200, record {ip: req.client_ip, scheme: req.scheme, host: req.host}
end
`)
	for _, tc := range []struct{ name, trusted, peer, xff, proto, host, wantIP, wantScheme, wantHost string }{
		{"untrusted", "", "127.0.0.1:8", "198.51.100.1", "https", "example.test", "127.0.0.1", "http", "example.com"},
		{"trusted chain", "127.0.0.0/8,10.0.0.0/8", "127.0.0.1:8", "spoofed, 198.51.100.1, 10.0.0.1", "https", "example.test", "127.0.0.1", "https", "example.test"},
		{"valid chain", "127.0.0.0/8,10.0.0.0/8", "127.0.0.1:8", "192.0.2.8, 198.51.100.1, 10.0.0.1", "https", "example.test", "198.51.100.1", "https", "example.test"},
		{"nonproxy peer", "127.0.0.1", "192.0.2.2:8", "198.51.100.1", "https", "example.test", "192.0.2.2", "http", "example.com"},
		{"invalid metadata", "127.0.0.1", "127.0.0.1:8", "garbage", "javascript", "bad/host", "127.0.0.1", "http", "example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := DefaultConfig()
			c.TrustedProxy = tc.trusted
			a := application(t, path, c, nil)
			r := httptest.NewRequest("GET", "http://example.com/", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set("X-Forwarded-For", tc.xff)
			r.Header.Set("X-Forwarded-Proto", tc.proto)
			r.Header.Set("X-Forwarded-Host", tc.host)
			w := httptest.NewRecorder()
			a.ServeHTTP(w, r)
			var v map[string]string
			if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil {
				t.Fatal(e)
			}
			if v["ip"] != tc.wantIP || v["scheme"] != tc.wantScheme || v["host"] != tc.wantHost {
				t.Fatal(v)
			}
		})
	}
}

func TestStaticConfinementAndConditionalRequests(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{"style.css": "body { color: red; }", ".env": "secret", "server.key": "secret"} {
		if e := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); e != nil {
			t.Fatal(e)
		}
	}
	c := DefaultConfig()
	c.Static = dir
	a := application(t, appFile(t, "route \"GET\" \"/\"\ngive back \"ok\"\nend\n"), c, nil)
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"GET", "/static/style.css", 200}, {"HEAD", "/static/style.css", 200}, {"POST", "/static/style.css", 405}, {"GET", "/static/.env", 404}, {"GET", "/static/server.key", 404}, {"GET", "/static/../style.css", 404}, {"GET", "/static/", 404}, {"GET", "/static/missing", 404}} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := request(a, tc.method, tc.path, "")
			if w.Code != tc.status {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if tc.method == "HEAD" && w.Body.Len() != 0 {
				t.Fatal("HEAD body")
			}
		})
	}
	w := request(a, "GET", "/static/style.css", "")
	if !strings.Contains(w.Header().Get("Content-Type"), "text/css") {
		t.Fatal(w.Header())
	}
	r := httptest.NewRequest("GET", "/static/style.css", nil)
	r.Header.Set("If-None-Match", w.Header().Get("ETag"))
	rec := httptest.NewRecorder()
	a.ServeHTTP(rec, r)
	if rec.Code != 304 || rec.Body.Len() != 0 {
		t.Fatal(rec)
	}
	outside := appFile(t, "secret")
	if e := os.Symlink(outside, filepath.Join(dir, "escape.txt")); e == nil {
		if w := request(a, "GET", "/static/escape.txt", ""); w.Code != 404 {
			t.Fatal("served escaping symlink")
		}
	}
}

func TestConcurrentIsolationAndSecretSafeLogs(t *testing.T) {
	path := appFile(t, `use server
set count to 0
route "GET" "/"
set count to count plus 1
give back server.json using 200, count
end
`)
	c := DefaultConfig()
	c.LogFormat = "json"
	var logs bytes.Buffer
	a := application(t, path, c, &logs)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest("GET", "/?token=SECRET_QUERY", strings.NewReader("SECRET_BODY"))
			for _, k := range []string{"Authorization", "Proxy-Authorization", "Cookie", "Set-Cookie", "X-API-Key"} {
				r.Header.Set(k, "SECRET_HEADER")
			}
			w := httptest.NewRecorder()
			a.ServeHTTP(w, r)
			if w.Code != 200 || w.Body.String() != "1" {
				t.Errorf("%d %q", w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()
	if strings.Contains(logs.String(), "SECRET_") {
		t.Fatal("secret leaked to log")
	}
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry requestLog
		if e := json.Unmarshal([]byte(line), &entry); e != nil {
			t.Fatal(e)
		}
		if entry.RequestID == "" || entry.Status != 200 || entry.Route != "/" {
			t.Fatal(entry)
		}
	}
}

func TestProductionErrorsAndConsoleRedaction(t *testing.T) {
	path := appFile(t, `route "POST" "/" using req
show req.body
show req.headers at "authorization"
raise req.body
end
`)
	c := DefaultConfig()
	c.Diagnostics = interpreter.ModeProduction
	c.LogFormat = "json"
	var logs bytes.Buffer
	a := application(t, path, c, &logs)
	r := httptest.NewRequest("POST", "/", strings.NewReader("SECRET_PAYLOAD"))
	r.Header.Set("Authorization", "SECRET_TOKEN")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 500 || strings.Contains(w.Body.String(), "SECRET_") || strings.Contains(w.Body.String(), "line") || !strings.Contains(w.Body.String(), "request_id=") {
		t.Fatal(w.Body.String())
	}
	if strings.Contains(logs.String(), "SECRET_") || !strings.Contains(logs.String(), "GRAB-E308") || !strings.Contains(logs.String(), "redacted") {
		t.Fatal(logs.String())
	}
}

func TestRequestDeadlineCancelsOutboundHTTP(t *testing.T) {
	path := appFile(t, `use http
route "GET" "/"
set result to http.get using "http://127.0.0.1:12345/"
give back "finished"
end
`)
	c := DefaultConfig()
	c.RequestTimeout = 30 * time.Millisecond
	dialStopped := make(chan struct{}, 1)
	opts := interpreter.Options{Permissions: interpreter.Permissions{AllowNet: []string{"127.0.0.1:12345"}}, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		<-ctx.Done()
		dialStopped <- struct{}{}
		return nil, ctx.Err()
	}}
	a, e := NewApplication(path, "", opts, c, nil)
	if e != nil {
		t.Fatal(e)
	}
	w := request(a, "GET", "/", "")
	if w.Code != 504 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	select {
	case <-dialStopped:
	case <-time.After(time.Second):
		t.Fatal("outbound operation kept running after request deadline")
	}
}

func TestAutomaticReloadAndLegacyPreparedMode(t *testing.T) {
	path := appFile(t, "set title to \"before\"\n")
	tp := strings.TrimSuffix(path, ".grab") + ".grab.html"
	if e := os.WriteFile(tp, []byte("<h1>{title}</h1>"), 0600); e != nil {
		t.Fatal(e)
	}
	a := application(t, path, DefaultConfig(), nil)
	if e := os.WriteFile(path, []byte("set title to \"after\"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	// No reload means the original source remains prepared in memory.
	if w := request(a, "GET", "/", ""); w.Body.String() != "<h1>before</h1>" {
		t.Fatal(w.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.watch(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for {
		w := request(a, "GET", "/", "")
		if w.Body.String() == "<h1>after</h1>" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("watcher failed to reload source")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if e := os.WriteFile(tp, []byte("<p>{title}</p>"), 0600); e != nil {
		t.Fatal(e)
	}
	deadline = time.Now().Add(2 * time.Second)
	for {
		w := request(a, "GET", "/", "")
		if w.Body.String() == "<p>after</p>" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("watcher failed to reload template")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
