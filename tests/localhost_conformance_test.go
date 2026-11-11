package tests

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"grab/internal/server"
	"grab/internal/template"
)

func TestLocalhostSpecificationConformance(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "grab")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	if runtime.GOOS == "windows" {
		goTool += ".exe"
	}
	build := exec.Command(goTool, "build", "-o", binary, "./cmd/grab")
	build.Dir = ".."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}

	t.Run("spec case 1 basic template interpolation and HTML escaping", func(t *testing.T) {
		dir := t.TempDir()
		grabFile := filepath.Join(dir, "hello.grab")
		tmplFile := filepath.Join(dir, "hello.grab.html")

		grabSrc := "set title to \"GRAB <0.2> & Friends\"\n" +
			"set link to \"https://example.com/docs?v=0.2&lang=grab\"\n" +
			"set summary to \"Safe \\\"quoted\\\" preview\"\n"
		tmplSrc := "<!doctype html>\n<html>\n  <head><title>{title}</title></head>\n  <body>\n    <h1 title=\"{summary}\">{title}</h1>\n    <a href=\"{link}\">Documentation {{v0.2}}</a>\n  </body>\n</html>\n"
		wantHTML := "<!doctype html>\n<html>\n  <head><title>GRAB &lt;0.2&gt; &amp; Friends</title></head>\n  <body>\n    <h1 title=\"Safe &#34;quoted&#34; preview\">GRAB &lt;0.2&gt; &amp; Friends</h1>\n    <a href=\"https://example.com/docs?v=0.2&amp;lang=grab\">Documentation {v0.2}</a>\n  </body>\n</html>\n"

		if err := os.WriteFile(grabFile, []byte(grabSrc), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tmplFile, []byte(tmplSrc), 0o600); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		server.NewHandler(grabFile, tmplFile, nil).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if rec.Body.String() != wantHTML {
			t.Errorf("rendered HTML =\n%q\nwant:\n%q", rec.Body.String(), wantHTML)
		}
	})

	t.Run("spec case 2 unbound template variable rejected at preflight and render", func(t *testing.T) {
		dir := t.TempDir()
		grabFile := filepath.Join(dir, "missing.grab")
		tmplFile := filepath.Join(dir, "missing.grab.html")

		_ = os.WriteFile(grabFile, []byte("set title to \"Hello\"\n"), 0o600)
		_ = os.WriteFile(tmplFile, []byte("<h1>{title}</h1>\n<p>{author}</p>\n"), 0o600)

		var stdout, stderr bytes.Buffer
		cmd := exec.Command(binary, "serve", grabFile, tmplFile)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		if err == nil {
			t.Fatal("expected CLI serve preflight failure for unbound template variable")
		}
		if !strings.Contains(stderr.String(), "unbound template variable \"author\" at line 2, column 5") {
			t.Errorf("stderr = %q, want unbound template variable at line 2, column 5", stderr.String())
		}

		html, renderErr := server.RenderFiles(grabFile, tmplFile, nil)
		if html != "" {
			t.Errorf("expected empty HTML output, got %q", html)
		}
		var tmplErr *template.Error
		if !errors.As(renderErr, &tmplErr) || tmplErr.Kind != template.UnboundVariable {
			t.Fatalf("RenderFiles error = %v, want *template.Error(UnboundVariable)", renderErr)
		}
	})

	t.Run("spec case 3 disallowed context onclick rejected at preflight", func(t *testing.T) {
		dir := t.TempDir()
		grabFile := filepath.Join(dir, "bad.grab")
		tmplFile := filepath.Join(dir, "bad.grab.html")

		_ = os.WriteFile(grabFile, []byte("set action to \"run()\"\n"), 0o600)
		_ = os.WriteFile(tmplFile, []byte("<button onclick=\"{action}\">Click</button>\n"), 0o600)

		var stdout, stderr bytes.Buffer
		cmd := exec.Command(binary, "serve", grabFile, tmplFile)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err == nil {
			t.Fatal("expected CLI serve preflight failure for disallowed context")
		}
		if !strings.Contains(stderr.String(), "disallowed interpolation context at line 1, column 18") {
			t.Errorf("stderr = %q, want disallowed interpolation context at line 1, column 18", stderr.String())
		}
	})

	t.Run("spec case 4 unsafe URL scheme rejected at render time", func(t *testing.T) {
		dir := t.TempDir()
		grabFile := filepath.Join(dir, "xss.grab")
		tmplFile := filepath.Join(dir, "xss.grab.html")

		_ = os.WriteFile(grabFile, []byte("set bad_url to \"javascript:alert(1)\"\n"), 0o600)
		_ = os.WriteFile(tmplFile, []byte("<a href=\"{bad_url}\">Click</a>\n"), 0o600)

		html, err := server.RenderFiles(grabFile, tmplFile, nil)
		if html != "" {
			t.Errorf("expected no partial HTML output, got %q", html)
		}
		var tmplErr *template.Error
		if !errors.As(err, &tmplErr) || tmplErr.Kind != template.UnsafeURLScheme {
			t.Fatalf("RenderFiles error = %v, want UnsafeURLScheme", err)
		}
	})

	t.Run("checked-in examples render cleanly", func(t *testing.T) {
		helloGrab, err := filepath.Abs(filepath.Join("..", "examples", "hello.grab"))
		if err != nil {
			t.Fatal(err)
		}
		helloTmpl, err := filepath.Abs(filepath.Join("..", "examples", "hello.grab.html"))
		if err != nil {
			t.Fatal(err)
		}
		helloHTML, err := server.RenderFiles(helloGrab, helloTmpl, nil)
		if err != nil {
			t.Fatalf("RenderFiles(examples/hello): %v", err)
		}
		if !strings.Contains(helloHTML, "Welcome to GRAB") || !strings.Contains(helloHTML, "Hello Allen! This page was rendered by {GRAB 0.2}.") {
			t.Errorf("unexpected helloHTML:\n%s", helloHTML)
		}

		productGrab, err := filepath.Abs(filepath.Join("..", "examples", "product.grab"))
		if err != nil {
			t.Fatal(err)
		}
		productTmpl, err := filepath.Abs(filepath.Join("..", "examples", "product.grab.html"))
		if err != nil {
			t.Fatal(err)
		}
		productHTML, err := server.RenderFiles(productGrab, productTmpl, nil)
		if err != nil {
			t.Fatalf("RenderFiles(examples/product): %v", err)
		}
		if !strings.Contains(productHTML, "record {sku: &#34;A-17&#34;, active: yes") {
			t.Errorf("unexpected productHTML:\n%s", productHTML)
		}
	})
}
