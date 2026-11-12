package tests

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"grab/internal/interpreter"
	"grab/internal/parser"
	"grab/internal/server"
)

// Exercise the public server entry point over HTTP, including startup, routes,
// templates, exact JSON, body handling, probes, and graceful shutdown.
func TestGRAB06HTTPConformance(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.grab")
	source := `use server
set store to "Northbound & Co"
route "GET" "/" using req
give back server.html using "home.grab.html", record {title: store}
end
route "GET" "/products/{id}" using req
give back server.json using 200, record {sku: req.params.id, price: 19.95}
end
route "POST" "/orders" using req
try
set order to server.parse_json using req
give back server.json using 201, order
catch err
give back server.json using 400, record {code: err.code}
end
end
`
	for name, data := range map[string]string{path: source, filepath.Join(dir, "home.grab.html"): "<h1>{title}</h1>"} {
		if e := os.WriteFile(name, []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan string, 1)
	done := make(chan error, 1)
	listener := newGRAB05PipeListener()
	go func() {
		done <- server.ServeListener(ctx, listener, path, "", io.Discard, ready)
	}()
	var addr string
	select {
	case addr = <-ready:
	case e := <-done:
		t.Fatal(e)
	case <-time.After(5 * time.Second):
		t.Fatal("startup timeout")
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DialContext: listener.DialContext}}
	defer client.CloseIdleConnections()
	for _, tc := range []struct {
		method, path, body string
		status             int
		want               string
	}{
		{"GET", "/", "", 200, "<h1>Northbound &amp; Co</h1>"},
		{"HEAD", "/", "", 200, ""},
		{"GET", "/products/A-17", "", 200, `{"sku":"A-17","price":19.95}`},
		{"POST", "/orders", `{"n":123456789012345678901234567890}`, 201, `{"n":123456789012345678901234567890}`},
		{"POST", "/orders", "bad", 400, `{"code":"GRAB-E312"}`},
		{"PUT", "/orders", "", 405, "405 method not allowed\n"},
		{"GET", "/missing", "", 404, "404 page not found\n"},
		{"GET", "/_health", "", 200, `{"status":"ok"}`},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			r, e := http.NewRequest(tc.method, "http://"+addr+tc.path, strings.NewReader(tc.body))
			if e != nil {
				t.Fatal(e)
			}
			res, e := client.Do(r)
			if e != nil {
				t.Fatal(e)
			}
			b, e := io.ReadAll(res.Body)
			res.Body.Close()
			if e != nil || res.StatusCode != tc.status || string(b) != tc.want {
				t.Fatalf("%d %q %v", res.StatusCode, b, e)
			}
		})
	}
	cancel()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown timeout")
	}
}

func TestGRAB06CheckedInExample(t *testing.T) {
	path, e := filepath.Abs(filepath.Join("..", "examples", "server_app.grab"))
	if e != nil {
		t.Fatal(e)
	}
	source, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	formatted, e := parser.Format(path, string(source))
	if e != nil || formatted != string(source) {
		t.Fatalf("example needs canonical formatting: %v", e)
	}
	a, e := server.NewApplication(path, "", interpreter.Options{}, server.DefaultConfig(), io.Discard)
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		method, path, body, contentType string
		status                          int
		contains                        string
	}{
		{"GET", "/", "", "", 200, "Northbound Goods"},
		{"GET", "/api/products/A-17", "", "", 200, `"price_with_tax":21.55`},
		{"POST", "/api/orders", `{"sku":"A-17"}`, "application/json", 201, `"accepted":true`},
		{"POST", "/greet", "name=Alice+%26+Bob", "application/x-www-form-urlencoded", 200, "Alice &amp; Bob"},
		{"GET", "/start", "", "", 302, ""},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			w := httptest.NewRecorder()
			a.ServeHTTP(w, r)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
		})
	}
}
