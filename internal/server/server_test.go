package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateLoopbackAddr(t *testing.T) {
	cases := []struct {
		addr    string
		wantErr bool
	}{
		{"127.0.0.1:8080", false},
		{"127.0.0.1:0", false},
		{"localhost:8080", false},
		{"LOCALHOST:3000", false},
		{"[::1]:8080", false},
		{"0.0.0.0:8080", true},
		{":8080", true},
		{"[::]:8080", true},
		{"192.168.1.10:8080", true},
		{"example.com:8080", true},
		{"127.0.0.1:99999", true},
		{"127.0.0.1:-1", true},
		{"127.0.0.1", true},
	}

	for _, tc := range cases {
		t.Run(tc.addr, func(t *testing.T) {
			err := ValidateLoopbackAddr(tc.addr)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateLoopbackAddr(%q) error = %v, wantErr %v", tc.addr, err, tc.wantErr)
			}
		})
	}
}

func TestResolveAndRenderFiles(t *testing.T) {
	dir := t.TempDir()

	grabA := filepath.Join(dir, "page.grab")
	tmplDefault := filepath.Join(dir, "page.grab.html")
	grabB := filepath.Join(dir, "second.grab")
	sharedTmpl := filepath.Join(dir, "shared.grab.html")

	if err := os.WriteFile(grabA, []byte("set title to \"First <Page>\"\nshow \"console log\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmplDefault, []byte("<h1>{title}</h1>\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(grabB, []byte("set title to \"Second & Co\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sharedTmpl, []byte("<article>{title}</article>\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("default companion template lookup and stdout separation", func(t *testing.T) {
		var console bytes.Buffer
		html, err := RenderFiles(grabA, "", &console)
		if err != nil {
			t.Fatalf("RenderFiles() error: %v", err)
		}
		if html != "<h1>First &lt;Page&gt;</h1>\n" {
			t.Errorf("html = %q, want %q", html, "<h1>First &lt;Page&gt;</h1>\n")
		}
		if console.String() != "console log\n" {
			t.Errorf("console = %q, want %q", console.String(), "console log\n")
		}
	})

	t.Run("reusable template across multiple grab programs", func(t *testing.T) {
		htmlA, err := RenderFiles(grabA, sharedTmpl, io.Discard)
		if err != nil {
			t.Fatalf("RenderFiles(grabA, sharedTmpl) error: %v", err)
		}
		htmlB, err := RenderFiles(grabB, sharedTmpl, io.Discard)
		if err != nil {
			t.Fatalf("RenderFiles(grabB, sharedTmpl) error: %v", err)
		}
		if htmlA != "<article>First &lt;Page&gt;</article>\n" {
			t.Errorf("htmlA = %q", htmlA)
		}
		if htmlB != "<article>Second &amp; Co</article>\n" {
			t.Errorf("htmlB = %q", htmlB)
		}
	})

	t.Run("rejects wrong extensions and traversal outside workspace", func(t *testing.T) {
		if _, _, err := ResolveFiles("bad.txt", ""); err == nil {
			t.Error("expected error for non-.grab file")
		}
		if _, _, err := ResolveFiles(grabA, "bad.html"); err == nil {
			t.Error("expected error for non-.grab.html file")
		}
		if _, _, err := ResolveFiles(grabA, "../escape.grab.html"); err == nil {
			t.Error("expected error for parent directory traversal")
		}
		otherDir := t.TempDir()
		outsideTmpl := filepath.Join(otherDir, "outside.grab.html")
		_ = os.WriteFile(outsideTmpl, []byte("<h1>{title}</h1>"), 0o600)
		if _, _, err := ResolveFiles(grabA, outsideTmpl); err == nil {
			t.Error("expected error for template outside program and working directory")
		}
	})
}

func TestNewHandler_HTTPBehaviorAndSecurityHeaders(t *testing.T) {
	dir := t.TempDir()
	grabFile := filepath.Join(dir, "app.grab")
	tmplFile := filepath.Join(dir, "app.grab.html")

	if err := os.WriteFile(grabFile, []byte("set heading to \"Hello <GRAB 0.2>\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmplFile, []byte("<h1>{heading}</h1>\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	handler := NewHandler(grabFile, tmplFile, io.Discard)

	checkSecurityHeaders := func(t *testing.T, h http.Header) {
		t.Helper()
		if got := h.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
		}
		if got := h.Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("X-Frame-Options = %q, want DENY", got)
		}
		wantCSP := "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'none'; object-src 'none'"
		if got := h.Get("Content-Security-Policy"); got != wantCSP {
			t.Errorf("Content-Security-Policy = %q, want %q", got, wantCSP)
		}
		if got := h.Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", got)
		}
	}

	t.Run("GET / returns 200 and escaped HTML", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		checkSecurityHeaders(t, rec.Header())
		if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
			t.Errorf("Content-Type = %q, want text/html; charset=utf-8", got)
		}
		if rec.Body.String() != "<h1>Hello &lt;GRAB 0.2&gt;</h1>\n" {
			t.Errorf("body = %q", rec.Body.String())
		}
	})

	t.Run("HEAD / returns 200 with empty body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodHead, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		checkSecurityHeaders(t, rec.Header())
		if rec.Body.Len() != 0 {
			t.Errorf("expected empty body for HEAD, got %q", rec.Body.String())
		}
	})

	t.Run("non-root path returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/other", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
		checkSecurityHeaders(t, rec.Header())
	})

	t.Run("POST / returns 405 with Allow header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
		checkSecurityHeaders(t, rec.Header())
		if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
			t.Errorf("Allow = %q, want GET, HEAD", got)
		}
	})

	t.Run("runtime error returns 500 with diagnostic and no partial HTML", func(t *testing.T) {
		errGrab := filepath.Join(dir, "err.grab")
		errTmpl := filepath.Join(dir, "err.grab.html")
		_ = os.WriteFile(errGrab, []byte("set bad to \"javascript:alert(1)\"\n"), 0o600)
		_ = os.WriteFile(errTmpl, []byte("<a href=\"{bad}\">Click</a>\n"), 0o600)

		errHandler := NewHandler(errGrab, errTmpl, io.Discard)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		errHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
		checkSecurityHeaders(t, rec.Header())
		if !strings.Contains(rec.Body.String(), "unsafe URL scheme") {
			t.Errorf("body = %q, want unsafe URL scheme diagnostic", rec.Body.String())
		}
	})
}

type pipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
}

func newPipeListener() *pipeListener {
	return &pipeListener{
		conns:  make(chan net.Conn),
		closed: make(chan struct{}),
	}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return nil
}

func (l *pipeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
}

func (l *pipeListener) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	client, srv := net.Pipe()
	select {
	case l.conns <- srv:
		return client, nil
	case <-l.closed:
		client.Close()
		srv.Close()
		return nil, net.ErrClosed
	case <-ctx.Done():
		client.Close()
		srv.Close()
		return nil, ctx.Err()
	}
}

func TestServe_LifecycleAndGracefulShutdown(t *testing.T) {
	dir := t.TempDir()
	grabFile := filepath.Join(dir, "live.grab")
	tmplFile := filepath.Join(dir, "live.grab.html")

	if err := os.WriteFile(grabFile, []byte("set msg to \"Live on Localhost\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmplFile, []byte("<p>{msg}</p>\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("real loopback TCP listener bind and shutdown", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var stdout bytes.Buffer
		ready := make(chan string, 1)
		errCh := make(chan error, 1)

		go func() {
			errCh <- Serve(ctx, "127.0.0.1:0", grabFile, "", &stdout, ready)
		}()

		var boundAddr string
		select {
		case boundAddr = <-ready:
		case err := <-errCh:
			if errors.Is(err, os.ErrPermission) {
				t.Skipf("TCP bind blocked by OS sandbox: %v", err)
			}
			t.Fatalf("Serve failed early: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for server to bind")
		}

		if !strings.Contains(stdout.String(), "http://"+boundAddr+"/") {
			t.Errorf("stdout announcement = %q, want URL", stdout.String())
		}

		cancel()
		if err := <-errCh; err != nil {
			t.Errorf("Serve shutdown returned error: %v", err)
		}
	})

	t.Run("full HTTP wire round-trip and graceful shutdown", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		pl := newPipeListener()
		var stdout bytes.Buffer
		ready := make(chan string, 1)
		errCh := make(chan error, 1)

		go func() {
			errCh <- ServeListener(ctx, pl, grabFile, "", &stdout, ready)
		}()

		select {
		case <-ready:
		case err := <-errCh:
			t.Fatalf("ServeListener failed early: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for server readiness")
		}

		client := &http.Client{
			Transport: &http.Transport{
				DialContext: pl.DialContext,
			},
		}

		resp, err := client.Get("http://127.0.0.1:8080/")
		if err != nil {
			t.Fatalf("client.Get: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
		if string(body) != "<p>Live on Localhost</p>\n" {
			t.Errorf("body = %q, want %q", string(body), "<p>Live on Localhost</p>\n")
		}

		cancel()
		if err := <-errCh; err != nil {
			t.Errorf("ServeListener shutdown returned error: %v", err)
		}
	})
}
