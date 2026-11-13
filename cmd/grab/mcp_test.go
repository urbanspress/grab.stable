package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPCLI_FlagsAndStdioRoundTrip(t *testing.T) {
	// Unknown flag returns usage exit code 2.
	var out, errBuf bytes.Buffer
	if code := run([]string{"mcp", "--unknown-flag"}, &out, &errBuf); code != 2 {
		t.Fatalf("expected exit 2 on unknown flag, got %d (%s)", code, errBuf.String())
	}

	// Non-existent workspace fails closed with exit code 1.
	out.Reset()
	errBuf.Reset()
	if code := runMCP(context.Background(), []string{"--workspace", filepath.Join(t.TempDir(), "missing")}, strings.NewReader(""), &out, &errBuf); code != 1 {
		t.Fatalf("expected exit 1 on missing workspace, got %d", code)
	}

	// Valid workspace stdio round-trip: initialize -> tools/list -> grab_check.
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "hello.grab"), []byte("set x to 42\nshow x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	req := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"grab_check","arguments":{"file":"hello.grab"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"grab_scaffold","arguments":{"kind":"app","target":"new.grab"}}}`,
	}, "\n") + "\n"

	out.Reset()
	errBuf.Reset()
	if code := runMCP(context.Background(), []string{"--workspace", ws}, strings.NewReader(req), &out, &errBuf); code != 0 {
		t.Fatalf("runMCP failed (%d): %s", code, errBuf.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 JSON-RPC responses, got %d:\n%s", len(lines), out.String())
	}
	var resp2, resp3 map[string]any
	_ = json.Unmarshal([]byte(lines[1]), &resp2)
	_ = json.Unmarshal([]byte(lines[2]), &resp3)
	text2 := resp2["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text2, `"ok":true`) {
		t.Fatalf("unexpected grab_check result: %s", text2)
	}
	// Default read-only mode denies grab_scaffold write.
	res3 := resp3["result"].(map[string]any)
	if res3["isError"] != true {
		t.Fatalf("expected write denial in read-only mode, got %v", res3)
	}
}
