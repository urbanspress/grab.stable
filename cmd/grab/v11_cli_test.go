package main

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIAllowEnvRunAndDenial(t *testing.T) {
	dir := t.TempDir()
	progPath := filepath.Join(dir, "env_check.grab")
	src := `use env
use crypto
show env.has using "CLI_ALLOWED_ONE"
show env.get using "CLI_ALLOWED_ONE"
show env.get using "CLI_ALLOWED_TWO"
set h to crypto.sha256_base64url using (env.get using "CLI_ALLOWED_ONE")
show h
`
	if err := os.WriteFile(progPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLI_ALLOWED_ONE", "alpha-token")
	t.Setenv("CLI_ALLOWED_TWO", "beta-token")
	t.Setenv("CLI_SECRET_DENIED", "must-not-read")

	// 1. Default-deny without --allow-env.
	for _, engine := range []string{"ast", "vm", "auto"} {
		var stdout, stderr bytes.Buffer
		code := run([]string{"run", "--engine", engine, progPath}, &stdout, &stderr)
		if code != 1 || !strings.Contains(stderr.String(), "GRAB-E401") {
			t.Fatalf("engine=%s expected exit 1 with GRAB-E401, got code=%d stderr=%q", engine, code, stderr.String())
		}
		if strings.Contains(stderr.String(), "alpha-token") || strings.Contains(stderr.String(), "must-not-read") {
			t.Fatalf("engine=%s leaked secret env value in stderr: %q", engine, stderr.String())
		}
	}

	// 2. Granting via comma-separated and repeated --allow-env works across engines.
	for _, engine := range []string{"ast", "vm", "auto"} {
		var stdout, stderr bytes.Buffer
		code := run([]string{"run", "--engine", engine, "--allow-env", "CLI_ALLOWED_ONE,OTHER", "--allow-env=CLI_ALLOWED_TWO", progPath}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("engine=%s exit code = %d, want 0; stderr=%q", engine, code, stderr.String())
		}
		lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
		if len(lines) != 4 || lines[0] != "yes" || lines[1] != "alpha-token" || lines[2] != "beta-token" {
			t.Fatalf("engine=%s unexpected output: %q", engine, stdout.String())
		}
	}

	// 3. Invalid --allow-env syntax fails with usage exit code 2.
	for _, badArgs := range [][]string{
		{"run", "--allow-env"},
		{"run", "--allow-env="},
		{"run", "--allow-env", "*", progPath},
		{"run", "--allow-env", "A=1", progPath},
		{"run", "--allow-env", "1BAD", progPath},
		{"run", "--allow-env", "A,,B", progPath},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(badArgs, &stdout, &stderr); code != 2 {
			t.Fatalf("args=%v exit code = %d, want 2", badArgs, code)
		}
	}
}

func TestCLIAllowScriptsFlagValidationAndServe(t *testing.T) {
	dir := t.TempDir()
	staticDir := filepath.Join(dir, "assets")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "grex.js"), []byte("window.grex = true;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.grab.html"), []byte("<!doctype html><html><body><h1>{title}</h1></body></html>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	appPath := filepath.Join(dir, "app.grab")
	appSrc := `use server
use env
route "GET" "/"
	give back server.html using "index.grab.html", record {title: env.get using "SITE_TITLE"}
end
`
	if err := os.WriteFile(appPath, []byte(appSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SITE_TITLE", "GrabLab Admin")

	// --allow-scripts must be rejected by non-serve commands.
	for _, badCmd := range [][]string{
		{"run", "--allow-scripts", appPath},
		{"check", "--allow-scripts", appPath},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(badCmd, &stdout, &stderr); code != 2 {
			t.Fatalf("cmd=%v exit code = %d, want 2", badCmd, code)
		}
	}

	// Verify live HTTP CSP headers without vs with --allow-scripts.
	for _, tc := range []struct {
		name         string
		allowScripts bool
		wantCSP      string
	}{
		{
			name:         "default_strict",
			allowScripts: false,
			wantCSP:      "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'none'; object-src 'none'",
		},
		{
			name:         "allow_scripts",
			allowScripts: true,
			wantCSP:      "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; object-src 'none'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ready := make(chan string, 1)
			done := make(chan int, 1)
			var stdout, stderr bytes.Buffer
			args := []string{"serve", "--addr", "127.0.0.1:0", "--allow-env", "SITE_TITLE", "--static", "/assets/=" + staticDir}
			if tc.allowScripts {
				args = append(args, "--allow-scripts")
			}
			args = append(args, appPath)
			go func() {
				done <- runContext(ctx, args, &stdout, &stderr, ready)
			}()

			addr := <-ready
			resp, err := http.Get("http://" + addr + "/")
			if err != nil {
				t.Fatalf("GET / failed: %v (stderr: %s)", err, stderr.String())
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET / status = %d, want 200", resp.StatusCode)
			}
			if got := resp.Header.Get("Content-Security-Policy"); got != tc.wantCSP {
				t.Fatalf("GET / CSP = %q, want %q", got, tc.wantCSP)
			}

			jsResp, err := http.Get("http://" + addr + "/assets/grex.js")
			if err != nil {
				t.Fatalf("GET /assets/grex.js failed: %v", err)
			}
			jsResp.Body.Close()
			if jsResp.StatusCode != http.StatusOK {
				t.Fatalf("GET /assets/grex.js status = %d, want 200", jsResp.StatusCode)
			}
			if got := jsResp.Header.Get("Content-Security-Policy"); got != tc.wantCSP {
				t.Fatalf("GET /assets/grex.js CSP = %q, want %q", got, tc.wantCSP)
			}

			cancel()
			if code := <-done; code != 0 {
				t.Fatalf("serve exit code = %d, want 0 (stderr: %s)", code, stderr.String())
			}
		})
	}
}
