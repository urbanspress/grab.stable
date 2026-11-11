package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"grab/internal/interpreter"
	"grab/internal/parser"
	"grab/internal/template"
)

// DefaultAddr is the default loopback address for grab serve.
const DefaultAddr = "127.0.0.1:8080"

// ValidateLoopbackAddr ensures addr binds exclusively to a loopback interface
// (127.0.0.1, ::1, or localhost) with a valid TCP port (0..65535).
func ValidateLoopbackAddr(addr string) error {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid address %q: %w", addr, err)
	}
	if host == "" {
		return fmt.Errorf("non-loopback address %q is not allowed in GRAB 0.2 (use 127.0.0.1 or localhost)", addr)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > 65535 {
		return fmt.Errorf("invalid port in address %q", addr)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("non-loopback address %q is not allowed in GRAB 0.2 (use 127.0.0.1 or localhost)", addr)
	}
	return nil
}

// ResolveFiles resolves and validates the .grab program path and .grab.html
// template path, enforcing file extensions, regular-file checks, and directory
// confinement.
func ResolveFiles(grabPath, templatePath string) (string, string, error) {
	if strings.ContainsRune(grabPath, '\x00') || strings.ContainsRune(templatePath, '\x00') {
		return "", "", fmt.Errorf("invalid NUL byte in file path")
	}
	if !strings.HasSuffix(grabPath, ".grab") {
		return "", "", fmt.Errorf("program file %q must have a .grab extension", grabPath)
	}
	if templatePath == "" {
		templatePath = strings.TrimSuffix(grabPath, ".grab") + ".grab.html"
	}
	if !strings.HasSuffix(templatePath, ".grab.html") {
		return "", "", fmt.Errorf("template file %q must have a .grab.html extension", templatePath)
	}

	// Reject explicit parent-directory traversal in relative paths.
	if !filepath.IsAbs(grabPath) && escapesRelativeRoot(grabPath) {
		return "", "", fmt.Errorf("program path %q escapes the working directory", grabPath)
	}
	if !filepath.IsAbs(templatePath) && escapesRelativeRoot(templatePath) {
		return "", "", fmt.Errorf("template path %q escapes the allowed directory", templatePath)
	}

	absGrab, err := filepath.Abs(grabPath)
	if err != nil {
		return "", "", err
	}
	absTmpl, err := filepath.Abs(templatePath)
	if err != nil {
		return "", "", err
	}

	grabInfo, err := os.Stat(absGrab)
	if err != nil {
		return "", "", err
	}
	if !grabInfo.Mode().IsRegular() {
		return "", "", fmt.Errorf("program path %q is not a regular file", grabPath)
	}

	tmplInfo, err := os.Stat(absTmpl)
	if err != nil {
		return "", "", err
	}
	if !tmplInfo.Mode().IsRegular() {
		return "", "", fmt.Errorf("template path %q is not a regular file", templatePath)
	}

	realGrab, err := filepath.EvalSymlinks(absGrab)
	if err != nil {
		return "", "", err
	}
	realTmpl, err := filepath.EvalSymlinks(absTmpl)
	if err != nil {
		return "", "", err
	}

	grabDir := filepath.Dir(realGrab)
	var workDir string
	if wd, err := os.Getwd(); err == nil {
		if realWD, err := filepath.EvalSymlinks(wd); err == nil {
			workDir = realWD
		} else {
			workDir = wd
		}
	}

	if !isWithinDir(realTmpl, grabDir) && (workDir == "" || !isWithinDir(realTmpl, workDir)) {
		return "", "", fmt.Errorf("template path %q is outside the allowed workspace directory", templatePath)
	}

	return grabPath, templatePath, nil
}

func escapesRelativeRoot(p string) bool {
	cleaned := filepath.Clean(p)
	return cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator))
}

func isWithinDir(target, rootDir string) bool {
	rel, err := filepath.Rel(rootDir, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Preflight validates both the .grab program and .grab.html template (including
// lexical, syntax, name-resolution, HTML context, and template variable
// binding checks) without starting a server or executing side effects.
func Preflight(grabPath, templatePath string) (string, string, error) {
	resolvedGrab, resolvedTmpl, err := ResolveFiles(grabPath, templatePath)
	if err != nil {
		return "", "", err
	}

	grabSource, err := os.ReadFile(resolvedGrab)
	if err != nil {
		return "", "", err
	}
	program, err := parser.Parse(resolvedGrab, string(grabSource))
	if err != nil {
		return "", "", err
	}
	bound, err := interpreter.Validate(program)
	if err != nil {
		return "", "", err
	}

	tmplSource, err := os.ReadFile(resolvedTmpl)
	if err != nil {
		return "", "", err
	}
	tmpl, err := template.Parse(resolvedTmpl, string(tmplSource))
	if err != nil {
		return "", "", err
	}
	if err := template.ValidateBindings(tmpl, bound); err != nil {
		return "", "", err
	}

	return resolvedGrab, resolvedTmpl, nil
}

// RenderFiles executes grabPath in a fresh isolated environment and renders
// templatePath with the resulting bound variables.
func RenderFiles(grabPath, templatePath string, consoleOut io.Writer) (string, error) {
	return RenderFilesWithOptions(grabPath, templatePath, interpreter.Options{}, consoleOut)
}

// RenderFilesWithOptions executes grabPath with the supplied interpreter
// options in a fresh isolated environment and renders templatePath with the
// resulting bound variables.
func RenderFilesWithOptions(grabPath, templatePath string, opts interpreter.Options, consoleOut io.Writer) (string, error) {
	resolvedGrab, resolvedTmpl, err := ResolveFiles(grabPath, templatePath)
	if err != nil {
		return "", err
	}

	grabSource, err := os.ReadFile(resolvedGrab)
	if err != nil {
		return "", err
	}
	program, err := parser.Parse(resolvedGrab, string(grabSource))
	if err != nil {
		return "", err
	}

	tmplSource, err := os.ReadFile(resolvedTmpl)
	if err != nil {
		return "", err
	}
	tmpl, err := template.Parse(resolvedTmpl, string(tmplSource))
	if err != nil {
		return "", err
	}

	if consoleOut == nil {
		consoleOut = io.Discard
	}
	env, err := interpreter.RunWithEnvironmentAndOptions(program, consoleOut, opts)
	if err != nil {
		return "", err
	}

	return template.Render(tmpl, env)
}

// NewHandler creates the HTTP handler for grab serve with mandatory security
// headers, GET/HEAD '/' routing, per-request isolated execution, and a 5-second
// execution timeout.
func NewHandler(grabPath, templatePath string, consoleOut io.Writer) http.Handler {
	return NewHandlerWithOptions(grabPath, templatePath, interpreter.Options{}, consoleOut)
}

// NewHandlerWithOptions creates the HTTP handler for grab serve with the
// supplied interpreter capability options.
func NewHandlerWithOptions(grabPath, templatePath string, opts interpreter.Options, consoleOut io.Writer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w.Header())

		if r.URL.Path != "/" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, "404 page not found\n")
			return
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = io.WriteString(w, "405 method not allowed\n")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		type renderResult struct {
			html string
			err  error
		}
		done := make(chan renderResult, 1)
		go func() {
			html, err := RenderFilesWithOptions(grabPath, templatePath, opts, consoleOut)
			done <- renderResult{html: html, err: err}
		}()

		select {
		case <-ctx.Done():
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "grab: request execution timed out\n")
		case res := <-done:
			if res.err != nil {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = fmt.Fprintf(w, "grab: %v\n", res.err)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			if r.Method == http.MethodGet {
				_, _ = io.WriteString(w, res.html)
			}
		}
	})
}

func setSecurityHeaders(h http.Header) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", "default-src 'self'; script-src 'none'; object-src 'none'")
	h.Set("Cache-Control", "no-store")
}

// Serve validates loopback binding, runs startup preflight on the .grab and
// .grab.html files, binds the TCP listener, announces the URL to stdout, and
// serves HTTP requests until ctx is cancelled.
func Serve(ctx context.Context, addr, grabPath, templatePath string, stdout io.Writer, ready chan<- string) error {
	return ServeWithOptions(ctx, addr, grabPath, templatePath, interpreter.Options{}, stdout, ready)
}

// ServeWithOptions validates loopback binding, runs startup preflight on the
// .grab and .grab.html files, and serves HTTP requests with the supplied
// interpreter capability options until ctx is cancelled.
func ServeWithOptions(ctx context.Context, addr, grabPath, templatePath string, opts interpreter.Options, stdout io.Writer, ready chan<- string) error {
	if addr == "" {
		addr = DefaultAddr
	}
	if err := ValidateLoopbackAddr(addr); err != nil {
		return err
	}
	resolvedGrab, resolvedTmpl, err := Preflight(grabPath, templatePath)
	if err != nil {
		return err
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return serveWithListener(ctx, listener, resolvedGrab, resolvedTmpl, opts, stdout, ready)
}

// ServeListener validates loopback address and startup preflight, then runs the
// HTTP server on the supplied net.Listener until ctx is cancelled.
func ServeListener(ctx context.Context, listener net.Listener, grabPath, templatePath string, stdout io.Writer, ready chan<- string) error {
	if err := ValidateLoopbackAddr(listener.Addr().String()); err != nil {
		return err
	}
	resolvedGrab, resolvedTmpl, err := Preflight(grabPath, templatePath)
	if err != nil {
		return err
	}
	return serveWithListener(ctx, listener, resolvedGrab, resolvedTmpl, interpreter.Options{}, stdout, ready)
}

func serveWithListener(ctx context.Context, listener net.Listener, resolvedGrab, resolvedTmpl string, opts interpreter.Options, stdout io.Writer, ready chan<- string) error {
	defer listener.Close()

	boundAddr := listener.Addr().String()
	if stdout != nil {
		_, _ = fmt.Fprintf(stdout, "grab: serving %s with %s at http://%s/\n", resolvedGrab, resolvedTmpl, boundAddr)
	}
	if ready != nil {
		ready <- boundAddr
		close(ready)
	}

	srv := &http.Server{
		Handler:           NewHandlerWithOptions(resolvedGrab, resolvedTmpl, opts, stdout),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    65536,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return <-errCh
	case err := <-errCh:
		return err
	}
}
