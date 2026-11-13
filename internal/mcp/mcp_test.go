package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func callRPC(t *testing.T, s *Server, req string) rpcResponse {
	t.Helper()
	var out, errBuf bytes.Buffer
	if err := s.ServeStdio(context.Background(), strings.NewReader(req+"\n"), &out, &errBuf); err != nil {
		t.Fatalf("ServeStdio error: %v (stderr=%s)", err, errBuf.String())
	}
	var resp rpcResponse
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("invalid JSON response %q: %v", out.String(), err)
	}
	return resp
}

func callTool(t *testing.T, s *Server, id int, name string, args any) (map[string]any, bool) {
	t.Helper()
	reqBytes, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := callRPC(t, s, string(reqBytes))
	if resp.Error != nil {
		t.Fatalf("unexpected RPC error for %s: %+v", name, resp.Error)
	}
	resultMap, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", resp.Result)
	}
	isErr, _ := resultMap["isError"].(bool)
	content, _ := resultMap["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("unexpected content length: %d", len(content))
	}
	item := content[0].(map[string]any)
	text := item["text"].(string)
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("invalid tool JSON payload %q: %v", text, err)
	}
	return payload, isErr
}

func TestMCPProtocolLifecycleAndErrors(t *testing.T) {
	ws := t.TempDir()
	s, err := NewServer(Config{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"unknown/method"}`,
		`{not-json`,
		`{"jsonrpc":"1.0","id":5,"method":"ping"}`,
	}, "\n") + "\n"

	var out, errBuf bytes.Buffer
	if err := s.ServeStdio(context.Background(), strings.NewReader(input), &out, &errBuf); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 6 {
		t.Fatalf("expected 6 responses (notification produces none), got %d:\n%s", len(lines), out.String())
	}

	var r1, r3, r4, r5, r6 rpcResponse
	_ = json.Unmarshal([]byte(lines[0]), &r1)
	_ = json.Unmarshal([]byte(lines[2]), &r3)
	_ = json.Unmarshal([]byte(lines[3]), &r4)
	_ = json.Unmarshal([]byte(lines[4]), &r5)
	_ = json.Unmarshal([]byte(lines[5]), &r6)

	initRes := r1.Result.(map[string]any)
	if initRes["protocolVersion"] != "2024-11-05" {
		t.Fatalf("unexpected protocolVersion: %v", initRes)
	}
	toolsRes := r3.Result.(map[string]any)
	tools := toolsRes["tools"].([]any)
	if len(tools) != 5 {
		t.Fatalf("expected 5 MCP tools, got %d", len(tools))
	}
	if r4.Error == nil || r4.Error.Code != -32601 {
		t.Fatalf("expected -32601 for unknown method, got %+v", r4.Error)
	}
	if r5.Error == nil || r5.Error.Code != -32700 {
		t.Fatalf("expected -32700 for parse error, got %+v", r5.Error)
	}
	if r6.Error == nil || r6.Error.Code != -32600 {
		t.Fatalf("expected -32600 for invalid request, got %+v", r6.Error)
	}

	// Oversized line (> 4 MiB) must return -32600 without breaking framing.
	oversized := `{"jsonrpc":"2.0","id":9,"method":"ping","params":"` + strings.Repeat("a", maxRPCMessageBytes+10) + `"}`
	respOver := callRPC(t, s, oversized)
	if respOver.Error == nil || respOver.Error.Code != -32600 {
		t.Fatalf("expected -32600 for oversized frame, got %+v", respOver.Error)
	}
}

func TestMCPConfinementAndDefaultDenyWrite(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()
	outsideFile := filepath.Join(outside, "outside.grab")
	if err := os.WriteFile(outsideFile, []byte("set x to 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "app.grab"), []byte("set x to 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".env"), []byte("SECRET=supersecret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(ws, ".git"), 0o700)
	_ = os.WriteFile(filepath.Join(ws, ".git", "config.grab"), []byte("set x to 1\n"), 0o600)
	hasSymlink := os.Symlink(outsideFile, filepath.Join(ws, "link.grab")) == nil

	// Default server starts read-only.
	s, err := NewServer(Config{Workspace: ws})
	if err != nil {
		t.Fatal(err)
	}

	badPaths := []string{
		"../outside.grab",
		outsideFile,
		".env",
		".git/config.grab",
		".ssh/id_ed25519.grab",
		"server.key",
		"bad\x00.grab",
	}
	if hasSymlink {
		badPaths = append(badPaths, "link.grab")
	}
	for _, badPath := range badPaths {
		payload, isErr := callTool(t, s, 1, "grab_check", map[string]any{"file": badPath})
		if !isErr {
			t.Fatalf("expected tool error for %q, got %v", badPath, payload)
		}
		errObj := payload["error"].(map[string]any)
		if errObj["code"] != "GRAB-E401" {
			t.Fatalf("expected GRAB-E401 for %q, got %v", badPath, errObj)
		}
	}

	// Default-deny write enforcement.
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"grab_format", map[string]any{"file": "app.grab", "write": true}},
		{"grab_scaffold", map[string]any{"kind": "app", "target": "new.grab"}},
	} {
		payload, isErr := callTool(t, s, 2, tc.tool, tc.args)
		if !isErr {
			t.Fatalf("expected write denial for %s, got %v", tc.tool, payload)
		}
		errObj := payload["error"].(map[string]any)
		if errObj["code"] != "GRAB-E401" {
			t.Fatalf("expected GRAB-E401 for %s, got %v", tc.tool, errObj)
		}
	}
}

func TestMCPToolsEndToEnd(t *testing.T) {
	ws := t.TempDir()
	t.Setenv("MCP_TEST_DB_SECRET", "DO_NOT_LEAK_THIS_SECRET")
	t.Setenv("MCP_TEST_REDIS_SECRET", "DO_NOT_LEAK_REDIS_SECRET")

	if err := os.WriteFile(filepath.Join(ws, "home.grab.html"), []byte("<h1>{title}</h1>\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "app.grab"), []byte("use server\n\nroute \"GET\" \"/users/{id}\" using req\n  give back server.json using 200, record {id: req.params.id}\nend\n\nroute \"GET\" \"/users/me\"\n  give back server.html using \"home.grab.html\", record {title: \"Me\"}\nend\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "bad.grab"), []byte("set x to\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "grab.data.json"), []byte(`{"connections":{"main":{"driver":"postgres","host":"db.example.com","port":5432,"database":"app","user":"app","password_env":"MCP_TEST_DB_SECRET"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "grab.api.json"), []byte(`{"version":1,"types":{},"operations":[{"id":"get_home","method":"GET","path":"/","success":[{"status":200,"schema":{"type":"record","fields":[{"name":"ok","schema":{"type":"boolean"}}]}}]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "grab.platform.json"), []byte(`{"version":1,"caches":{"sessions":{"driver":"redis","namespace":"sess","host":"127.0.0.1","port":6379,"tls":"disabled","development":true,"password_env":"MCP_TEST_REDIS_SECRET"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := NewServer(Config{Workspace: ws, AllowWrite: true})
	if err != nil {
		t.Fatal(err)
	}

	// 1. grab_check valid + invalid
	okRes, isErr := callTool(t, s, 1, "grab_check", map[string]any{"file": "app.grab"})
	if isErr || okRes["ok"] != true {
		t.Fatalf("grab_check valid failed: isErr=%v %v", isErr, okRes)
	}
	badRes, isErr := callTool(t, s, 2, "grab_check", map[string]any{"file": "bad.grab", "mode": "beginner"})
	if isErr || badRes["ok"] != false {
		t.Fatalf("grab_check invalid unexpected: isErr=%v %v", isErr, badRes)
	}
	diags := badRes["diagnostics"].([]any)
	if len(diags) != 1 || diags[0].(map[string]any)["code"] == "" {
		t.Fatalf("expected structured diagnostic, got %v", diags)
	}

	// 2. grab_format inline and write-back
	fmtRes, isErr := callTool(t, s, 3, "grab_format", map[string]any{"source": "if yes\nshow \"hi\"\nend\n"})
	if isErr || fmtRes["changed"] != true {
		t.Fatalf("grab_format inline failed: %v", fmtRes)
	}
	if !strings.Contains(fmtRes["formatted"].(string), "  show \"hi\"") {
		t.Fatalf("unexpected formatted output: %q", fmtRes["formatted"])
	}
	if err := os.WriteFile(filepath.Join(ws, "messy.grab"), []byte("if yes\nshow \"ok\"\nend\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fmtWriteRes, isErr := callTool(t, s, 4, "grab_format", map[string]any{"file": "messy.grab", "write": true})
	if isErr || fmtWriteRes["written"] != true || fmtWriteRes["changed"] != true {
		t.Fatalf("grab_format write failed: %v", fmtWriteRes)
	}

	// 3. grab_inspect_routes
	routesRes, isErr := callTool(t, s, 5, "grab_inspect_routes", map[string]any{"file": "app.grab"})
	if isErr {
		t.Fatalf("grab_inspect_routes failed: %v", routesRes)
	}
	routes := routesRes["routes"].([]any)
	if len(routes) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(routes))
	}
	// Static /users/me sorts before parameterized /users/{id}.
	if routes[0].(map[string]any)["path"] != "/users/me" {
		t.Fatalf("expected static route first, got %v", routes)
	}

	// 4. grab_inspect_schema (must never leak env secret values!)
	schemaRes, isErr := callTool(t, s, 6, "grab_inspect_schema", map[string]any{})
	if isErr {
		t.Fatalf("grab_inspect_schema failed: %v", schemaRes)
	}
	rawSchema, _ := json.Marshal(schemaRes)
	if strings.Contains(string(rawSchema), "DO_NOT_LEAK") {
		t.Fatalf("secret leaked in grab_inspect_schema output: %s", rawSchema)
	}
	if !strings.Contains(string(rawSchema), "MCP_TEST_DB_SECRET") || !strings.Contains(string(rawSchema), "get_home") {
		t.Fatalf("expected schema metadata in output: %s", rawSchema)
	}

	// 5. grab_scaffold (dry_run and actual write + check)
	scafDry, isErr := callTool(t, s, 7, "grab_scaffold", map[string]any{
		"kind":    "template",
		"target":  "card.grab.html",
		"fields":  []map[string]string{{"name": "heading"}, {"name": "summary"}},
		"dry_run": true,
	})
	if isErr || scafDry["written"] != false {
		t.Fatalf("grab_scaffold dry_run failed: %v", scafDry)
	}
	scafWrite, isErr := callTool(t, s, 8, "grab_scaffold", map[string]any{
		"kind":   "app",
		"target": "scaffolded/server.grab",
		"name":   "Hello MCP",
	})
	if isErr || scafWrite["written"] != true {
		t.Fatalf("grab_scaffold write failed: %v", scafWrite)
	}
	checkScaf, isErr := callTool(t, s, 9, "grab_check", map[string]any{"file": "scaffolded/server.grab"})
	if isErr || checkScaf["ok"] != true {
		t.Fatalf("scaffolded app did not pass grab_check: %v", checkScaf)
	}
}
