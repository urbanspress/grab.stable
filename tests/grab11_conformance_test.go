package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grab/internal/interpreter"
	"grab/internal/mcp"
	"grab/internal/server"
)

func TestGRAB11ASTAndVMDifferentialParity(t *testing.T) {
	t.Setenv("GRAB11_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("GRAB11_FLAG", "enabled")
	opts := interpreter.Options{
		Permissions: interpreter.Permissions{
			AllowEnv: []string{"GRAB11_KEY", "GRAB11_FLAG", "GRAB11_UNSET"},
		},
	}

	program := `use crypto
use env
use text

make pkce_pair using verifier
	set challenge to crypto.sha256_base64url using verifier
	give back challenge
end

set has_key to env.has using "GRAB11_KEY"
set has_unset to env.has using "GRAB11_UNSET"
set key to env.get using "GRAB11_KEY"
set flag to env.get using "GRAB11_FLAG"
set missing to env.get using "GRAB11_UNSET"

set verifier to "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
set challenge to call pkce_pair using verifier
set digest to crypto.sha256_hex using "session-token-abc"

set sealed to crypto.encrypt_aead using "oauth_refresh=rt_987", key
set opened to crypto.decrypt_aead using sealed, key

set pw_hash to crypto.hash_password using "correct-horse-battery"
set ok_yes to crypto.verify_password using "correct-horse-battery", pw_hash
set ok_no to crypto.verify_password using "wrong-horse", pw_hash

set tok to crypto.random_urlsafe using 24
set hex_tok to crypto.random_hex using 16

show has_key
show has_unset
show flag
show missing is nothing
show challenge
show digest
show opened
show ok_yes
show ok_no
show text.length using tok
show text.length using hex_tok

do together
	set c1 to crypto.decrypt_aead using sealed, key
	set c2 to env.get using "GRAB11_FLAG"
end
show c1 joined with ":" joined with c2
`
	assertASTAndVMParity(t, "crypto_and_env_parity", program, opts)
}

func TestGRAB11AuthPKCESessionAppAndMCP(t *testing.T) {
	ws := t.TempDir()
	staticDir := filepath.Join(ws, "public")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "grex.bundle.js"), []byte("export const init = () => 'grex-v1';\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "admin.grab.html"), []byte("<!doctype html><html><head><script type=\"module\" src=\"{js}\"></script></head><body><h1>{title}</h1><p data-pkce=\"{pkce}\">{token}</p></body></html>\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	appSrc := `use crypto
use env
use server
use text

route "GET" "/admin"
	set bundle to server.asset using "/static/grex.bundle.js"
	set secret_key to env.get using "GRAB_AEAD_KEY"
	set pw_hash to crypto.hash_password using "admin-pass-123"
	set verified to crypto.verify_password using "admin-pass-123", pw_hash
	set raw_session to crypto.random_urlsafe using 32
	set session_hash to crypto.sha256_hex using raw_session
	set pkce to crypto.sha256_base64url using raw_session
	set encrypted_tok to crypto.encrypt_aead using "oauth-secret", secret_key
	set decrypted_tok to crypto.decrypt_aead using encrypted_tok, secret_key
	if not verified or decrypted_tok is not "oauth-secret"
		give back server.text using 500, "auth mismatch"
	end
	give back server.html using "admin.grab.html", record {title: "GrabLab Admin", js: bundle, pkce: pkce, token: session_hash}
end

route "GET" "/leak-check"
	set secret to env.get using "UNGRANTED_AWS_SECRET"
	give back server.text using 200, secret
end
`
	if err := os.WriteFile(filepath.Join(ws, "app.grab"), []byte(appSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	// Verify MCP grab_check validates use crypto & use env offline without evaluating secrets.
	srv, err := mcp.NewServer(mcp.Config{Workspace: ws, AllowWrite: false})
	if err != nil {
		t.Fatal(err)
	}
	mcpIn := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"grab_check","arguments":{"file":"app.grab"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"grab_format","arguments":{"file":"app.grab"}}}`,
	}, "\n") + "\n"
	var mcpOut bytes.Buffer
	if err := srv.ServeStdio(context.Background(), strings.NewReader(mcpIn), &mcpOut, io.Discard); err != nil {
		t.Fatal(err)
	}
	mcpLines := strings.Split(strings.TrimSpace(mcpOut.String()), "\n")
	if len(mcpLines) != 3 {
		t.Fatalf("expected 3 MCP responses, got %d:\n%s", len(mcpLines), mcpOut.String())
	}
	for _, line := range mcpLines[1:] {
		var env map[string]any
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			t.Fatal(err)
		}
		res := env["result"].(map[string]any)
		if res["isError"] == true {
			t.Fatalf("MCP tool failed unexpectedly: %s", line)
		}
	}

	t.Setenv("GRAB_AEAD_KEY", "abcdefghijklmnopqrstuvwxyz012345")
	t.Setenv("UNGRANTED_AWS_SECRET", "aws-secret-must-never-appear")

	for _, tc := range []struct {
		name         string
		engine       interpreter.EngineMode
		allowScripts bool
		wantCSP      string
	}{
		{
			name:         "vm_strict_default",
			engine:       interpreter.EngineVM,
			allowScripts: false,
			wantCSP:      "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'none'; object-src 'none'",
		},
		{
			name:         "ast_allow_scripts",
			engine:       interpreter.EngineAST,
			allowScripts: true,
			wantCSP:      "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; object-src 'none'",
		},
		{
			name:         "auto_allow_scripts",
			engine:       interpreter.EngineAuto,
			allowScripts: true,
			wantCSP:      "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; object-src 'none'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := server.DefaultConfig()
			cfg.Static = "/static/=" + staticDir
			cfg.AllowScripts = tc.allowScripts
			opts := interpreter.Options{
				Engine: tc.engine,
				Permissions: interpreter.Permissions{
					AllowEnv: []string{"GRAB_AEAD_KEY"},
				},
			}
			var logBuf bytes.Buffer
			app, err := server.NewApplication(filepath.Join(ws, "app.grab"), "", opts, cfg, &logBuf)
			if err != nil {
				t.Fatalf("NewApplication failed: %v", err)
			}
			defer app.Close()

			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /admin status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Security-Policy"); got != tc.wantCSP {
				t.Fatalf("CSP = %q, want %q", got, tc.wantCSP)
			}
			body := rec.Body.String()
			if !strings.Contains(body, "/static/grex.bundle.js?v=") || !strings.Contains(body, "GrabLab Admin") {
				t.Fatalf("unexpected rendered HTML body: %s", body)
			}

			// Unauthorized env variable access fails closed with 500 & GRAB-E401, never exposing secret value.
			denyRec := httptest.NewRecorder()
			app.ServeHTTP(denyRec, httptest.NewRequest(http.MethodGet, "/leak-check", nil))
			if denyRec.Code != http.StatusInternalServerError {
				t.Fatalf("GET /leak-check status = %d, want 500", denyRec.Code)
			}
			if strings.Contains(denyRec.Body.String(), "aws-secret-must-never-appear") || strings.Contains(logBuf.String(), "aws-secret-must-never-appear") {
				t.Fatalf("leaked ungranted env secret in HTTP response or logs")
			}
			if !strings.Contains(logBuf.String(), "GRAB-E401") {
				t.Fatalf("expected GRAB-E401 in server log, got: %s", logBuf.String())
			}
		})
	}
}
