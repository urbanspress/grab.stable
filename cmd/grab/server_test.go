package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServerCLIFlagValidation(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"--max-concurrency", "0"}, {"--max-concurrency", "bad"}, {"--max-queue", "-1"}, {"--max-body-bytes", "0"},
		{"--queue-timeout", "-1s"}, {"--request-timeout", "0s"}, {"--shutdown-timeout", "bad"}, {"--log-format", "csv"}, {"--trusted-proxy", "not-an-ip"},
	} {
		t.Run(tc.name+tc.value, func(t *testing.T) {
			var out, err bytes.Buffer
			if code := run([]string{"serve", tc.name, tc.value, "missing.grab"}, &out, &err); code != 2 || !strings.Contains(err.String(), "usage:") {
				t.Fatalf("code=%d stderr=%s", code, &err)
			}
		})
	}
	cfg, files, ok := parseCommonFlags([]string{"--allow-public", "--reload", "--trusted-proxy=127.0.0.1,::1", "--static=/assets/=public", "--max-concurrency=3", "--max-queue=0", "--queue-timeout=2s", "--request-timeout=4s", "--max-body-bytes=42", "--shutdown-timeout=5s", "--log-format=json", "app.grab"}, true)
	if !ok || len(files) != 1 || !cfg.server.AllowPublic || !cfg.server.Reload || cfg.server.MaxConcurrency != 3 || cfg.server.RequestTimeout != 4*time.Second || cfg.server.MaxBodyBytes != 42 || cfg.server.LogFormat != "json" {
		t.Fatalf("%+v %v %v", cfg, files, ok)
	}
	for _, flag := range []string{"--allow-public", "--reload", "--static=x", "--request-timeout=1s"} {
		if _, _, ok := parseCommonFlags([]string{flag, "app.grab"}, false); ok {
			t.Fatalf("serve-only flag accepted by other command: %s", flag)
		}
	}
}

func TestServerCLIPreflightAndLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.grab")
	source := `use server
route "GET" "/"
give back server.text using 200, "ok"
end
`
	if e := os.WriteFile(path, []byte(source), 0600); e != nil {
		t.Fatal(e)
	}
	var out, err bytes.Buffer
	if code := run([]string{"check", path}, &out, &err); code != 0 {
		t.Fatalf("check=%d %s", code, &err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan string, 1)
	done := make(chan int, 1)
	go func() {
		done <- runContext(ctx, []string{"serve", "--addr=127.0.0.1:0", "--log-format=json", path}, io.Discard, io.Discard, ready)
	}()
	select {
	case <-ready:
	case code := <-done:
		t.Fatalf("serve failed %d", code)
	case <-time.After(5 * time.Second):
		t.Fatal("startup timed out")
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatal(code)
	}
	source = `use server
route "GET" "/"
give back server.html using "missing.grab.html", record {}
end
`
	if e := os.WriteFile(path, []byte(source), 0600); e != nil {
		t.Fatal(e)
	}
	for _, command := range []string{"check", "serve"} {
		out.Reset()
		err.Reset()
		if code := run([]string{command, path}, &out, &err); code != 1 || !strings.Contains(err.String(), "GRAB-E209") {
			t.Fatalf("%s: %d %s", command, code, &err)
		}
	}
}
