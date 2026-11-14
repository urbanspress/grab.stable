package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"grab/internal/interpreter"
)

func TestAllowScriptsCSPHeaders(t *testing.T) {
	dir := t.TempDir()
	staticDir := filepath.Join(dir, "public")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "grex.js"), []byte("console.log('grex');\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "home.grab.html"), []byte("<!doctype html><html><body><main data-token=\"{token}\"></main><script src=\"/static/grex.js\"></script></body></html>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	appPath := filepath.Join(dir, "app.grab")
	appSrc := `use server
use env
use crypto
route "GET" "/"
	set msg to env.get using "APP_GREETING"
	set token to crypto.sha256_hex using msg
	give back server.html using "home.grab.html", record {token: token}
end
route "GET" "/custom"
	give back server.response using record {status: 200, headers: {"Content-Type": "text/html; charset=utf-8", "Content-Security-Policy": "script-src 'unsafe-eval'"}, body: "<h1>Admin</h1>"}
end
`
	if err := os.WriteFile(appPath, []byte(appSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APP_GREETING", "hello-grab-11")

	wantDefaultCSP := "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'none'; object-src 'none'"
	wantScriptsCSP := "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; object-src 'none'"

	for _, tc := range []struct {
		name         string
		allowScripts bool
		wantCSP      string
	}{
		{name: "strict_default", allowScripts: false, wantCSP: wantDefaultCSP},
		{name: "allow_scripts", allowScripts: true, wantCSP: wantScriptsCSP},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Static = "/static/=" + staticDir
			cfg.AllowScripts = tc.allowScripts
			opts := interpreter.Options{
				Engine: interpreter.EngineAuto,
				Permissions: interpreter.Permissions{
					AllowEnv: []string{"APP_GREETING"},
				},
			}
			var logBuf bytes.Buffer
			app, err := NewApplication(appPath, "", opts, cfg, &logBuf)
			if err != nil {
				t.Fatalf("NewApplication failed: %v", err)
			}
			defer app.Close()

			for _, path := range []string{"/", "/custom", "/static/grex.js", "/missing"} {
				req := httptest.NewRequest(http.MethodGet, path, nil)
				rec := httptest.NewRecorder()
				app.ServeHTTP(rec, req)
				gotCSP := rec.Header().Get("Content-Security-Policy")
				if gotCSP != tc.wantCSP {
					t.Fatalf("path %s: CSP = %q, want %q", path, gotCSP, tc.wantCSP)
				}
				if strings.Contains(gotCSP, "unsafe-eval") {
					t.Fatalf("path %s: CSP must never include unsafe-eval: %q", path, gotCSP)
				}
				if !strings.Contains(gotCSP, "object-src 'none'") {
					t.Fatalf("path %s: CSP must preserve object-src 'none': %q", path, gotCSP)
				}
				if path == "/" && rec.Code != http.StatusOK {
					t.Fatalf("path %s: status = %d, want 200; body=%s", path, rec.Code, rec.Body.String())
				}
				if path == "/static/grex.js" && rec.Code != http.StatusOK {
					t.Fatalf("path %s: status = %d, want 200", path, rec.Code)
				}
				if path == "/missing" && rec.Code != http.StatusNotFound {
					t.Fatalf("path %s: status = %d, want 404", path, rec.Code)
				}
			}
		})
	}
}

func TestRestrictedWorkerEnvForwardsAllowEnv(t *testing.T) {
	t.Setenv("GRAB_WORKER_SECRET", "worker-allowed-val")
	t.Setenv("GRAB_UNGRANTED_SECRET", "worker-denied-val")

	opts := interpreter.Options{
		Permissions: interpreter.Permissions{
			AllowEnv: []string{"GRAB_WORKER_SECRET"},
		},
	}
	env := restrictedWorkerEnv(opts, DefaultConfig())
	if !slices.Contains(env, "GRAB_WORKER_SECRET=worker-allowed-val") {
		t.Fatalf("expected allowed env var forwarded to worker env, got %v", env)
	}
	if slices.Contains(env, "GRAB_UNGRANTED_SECRET=worker-denied-val") {
		t.Fatalf("ungranted env var must not be forwarded to worker env, got %v", env)
	}
}
