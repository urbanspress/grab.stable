package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grab/internal/interpreter"
	"grab/internal/mcp"
	"grab/internal/server"
)

func TestGRAB10MCPConformanceAndConfinement(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.grab"), []byte("set x to 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".env"), []byte("API_KEY=topsecret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "grab.data.json"), []byte(`{"connections":{"db":{"driver":"sqlite","path":"app.db"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Read-only server rejects disk scaffolds/writes with GRAB-E401.
	roSrv, err := mcp.NewServer(mcp.Config{Workspace: ws, AllowWrite: false})
	if err != nil {
		t.Fatal(err)
	}
	roInput := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"grab_scaffold","arguments":{"kind":"app","target":"app.grab"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"grab_check","arguments":{"file":"../secret.grab"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"grab_check","arguments":{"file":".env"}}}`,
	}, "\n") + "\n"
	var roOut bytes.Buffer
	if err := roSrv.ServeStdio(context.Background(), strings.NewReader(roInput), &roOut, io.Discard); err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(strings.TrimSpace(roOut.String()), "\n") {
		if i == 0 {
			continue
		}
		if !strings.Contains(line, "GRAB-E401") {
			t.Fatalf("line %d expected GRAB-E401 denial, got %s", i+1, line)
		}
	}

	// Write-enabled server scaffolds template + app + contract, formats, checks, and inspects schema/routes.
	rwSrv, err := mcp.NewServer(mcp.Config{Workspace: ws, AllowWrite: true})
	if err != nil {
		t.Fatal(err)
	}
	rwInput := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"grab_scaffold","arguments":{"kind":"template","target":"home.grab.html","fields":[{"name":"title"}]}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"grab_scaffold","arguments":{"kind":"app","target":"app.grab","name":"GRAB 1.0","routes":[{"method":"GET","path":"/","template":"home.grab.html"},{"method":"GET","path":"/items/{id}"}]}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"grab_check","arguments":{"file":"app.grab"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"grab_inspect_routes","arguments":{"file":"app.grab"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"grab_inspect_schema","arguments":{}}}`,
	}, "\n") + "\n"
	var rwOut bytes.Buffer
	if err := rwSrv.ServeStdio(context.Background(), strings.NewReader(rwInput), &rwOut, io.Discard); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(rwOut.String()), "\n")
	if len(lines) != 6 {
		t.Fatalf("expected 6 JSON-RPC responses, got %d:\n%s", len(lines), rwOut.String())
	}

	parseToolText := func(line string) map[string]any {
		var env map[string]any
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			t.Fatal(err)
		}
		res := env["result"].(map[string]any)
		if res["isError"] == true {
			t.Fatalf("unexpected tool error: %s", line)
		}
		text := res["content"].([]any)[0].(map[string]any)["text"].(string)
		var payload map[string]any
		if err := json.Unmarshal([]byte(text), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}

	checkPayload := parseToolText(lines[3])
	if checkPayload["ok"] != true {
		t.Fatalf("scaffolded app failed grab_check: %v", checkPayload)
	}
	routesPayload := parseToolText(lines[4])
	if len(routesPayload["routes"].([]any)) != 2 {
		t.Fatalf("expected 2 inspected routes, got %v", routesPayload)
	}
	schemaPayload := parseToolText(lines[5])
	if schemaPayload["database"] == nil {
		t.Fatalf("expected database schema metadata, got %v", schemaPayload)
	}
}

func TestGRAB10ServerAssetAndEngineParity(t *testing.T) {
	dir := t.TempDir()
	staticDir := filepath.Join(dir, "public")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cssPath := filepath.Join(staticDir, "app.css")
	if err := os.WriteFile(cssPath, []byte("body{margin:0}"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmplPath := filepath.Join(dir, "page.grab.html")
	if err := os.WriteFile(tmplPath, []byte(`<link rel="stylesheet" href="{css}"><h1>{title}</h1>`), 0o600); err != nil {
		t.Fatal(err)
	}
	appPath := filepath.Join(dir, "app.grab")
	appSrc := `use server

route "GET" "/"
  set css_url to server.asset using "/static/app.css"
  give back server.html using "page.grab.html", record {css: css_url, title: "GRAB 1.0"}
end
`
	if err := os.WriteFile(appPath, []byte(appSrc), 0o600); err != nil {
		t.Fatal(err)
	}

	var expectedBody string
	for _, mode := range []interpreter.EngineMode{interpreter.EngineAST, interpreter.EngineVM, interpreter.EngineAuto} {
		t.Run(string(mode), func(t *testing.T) {
			cfg := server.DefaultConfig()
			cfg.Static = staticDir
			opts := interpreter.Options{Engine: mode, HotRouteThreshold: 2}
			app, err := server.NewApplication(appPath, "", opts, cfg, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			defer app.Close()

			for hit := 0; hit < 4; hit++ {
				rec := httptest.NewRecorder()
				app.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
				if rec.Code != 200 {
					t.Fatalf("hit %d status=%d body=%s", hit, rec.Code, rec.Body.String())
				}
				if expectedBody == "" {
					expectedBody = rec.Body.String()
				} else if rec.Body.String() != expectedBody {
					t.Fatalf("parity mismatch (%s hit %d): got %q, want %q", mode, hit, rec.Body.String(), expectedBody)
				}
			}

			// Extract fingerprinted URL from rendered HTML href="..."
			start := strings.Index(expectedBody, `href="`) + len(`href="`)
			end := strings.Index(expectedBody[start:], `"`) + start
			assetURL := expectedBody[start:end]

			// Matching hash -> immutable Cache-Control.
			recAsset := httptest.NewRecorder()
			app.ServeHTTP(recAsset, httptest.NewRequest("GET", assetURL, nil))
			if recAsset.Code != 200 || recAsset.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
				t.Fatalf("asset match failed: %d %v", recAsset.Code, recAsset.Header())
			}

			// Mismatch or missing ?v= -> no-cache fallback.
			recMismatch := httptest.NewRecorder()
			app.ServeHTTP(recMismatch, httptest.NewRequest("GET", "/static/app.css?v=stalehash000", nil))
			if recMismatch.Code != 200 || recMismatch.Header().Get("Cache-Control") != "no-cache" {
				t.Fatalf("asset mismatch failed: %d %v", recMismatch.Code, recMismatch.Header())
			}
		})
	}
}
