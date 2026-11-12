package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"grab/internal/interpreter"
)

func setSecurityHeaders(h http.Header) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Security-Policy", "default-src 'self'; script-src 'none'; object-src 'none'")
	h.Set("Cache-Control", "no-store")
}
func NewHandler(grabPath, templatePath string, output io.Writer) http.Handler {
	return NewHandlerWithOptions(grabPath, templatePath, interpreter.Options{}, output)
}
func NewHandlerWithOptions(grabPath, templatePath string, opts interpreter.Options, output io.Writer) http.Handler {
	a, e := NewApplication(grabPath, templatePath, opts, DefaultConfig(), output)
	if e != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			setSecurityHeaders(w.Header())
			http.Error(w, "grab: "+e.Error(), 500)
		})
	}
	return a
}
func Serve(ctx context.Context, addr, grabPath, templatePath string, output io.Writer, ready chan<- string) error {
	return ServeWithOptions(ctx, addr, grabPath, templatePath, interpreter.Options{}, output, ready)
}
func ServeWithOptions(ctx context.Context, addr, grabPath, templatePath string, opts interpreter.Options, output io.Writer, ready chan<- string) error {
	return ServeWithConfig(ctx, addr, grabPath, templatePath, opts, DefaultConfig(), output, ready)
}
func ServeWithConfig(ctx context.Context, addr, grabPath, templatePath string, opts interpreter.Options, cfg Config, output io.Writer, ready chan<- string) error {
	if addr == "" {
		addr = DefaultAddr
	}
	if e := ValidateAddr(addr, cfg.AllowPublic); e != nil {
		return e
	}
	a, e := NewApplication(grabPath, templatePath, opts, cfg, output)
	if e != nil {
		return e
	}
	defer a.Close()
	l, e := net.Listen("tcp", addr)
	if e != nil {
		return e
	}
	if cfg.AllowPublic && ValidateLoopbackAddr(addr) != nil && cfg.TrustedProxy == "" && output != nil {
		fmt.Fprintln(output, "grab: public HTTP listener: use TLS termination at Nginx or a reverse proxy")
	}
	return serveApplication(ctx, l, a, output, ready)
}
func ServeListener(ctx context.Context, l net.Listener, grabPath, templatePath string, output io.Writer, ready chan<- string) error {
	if e := ValidateLoopbackAddr(l.Addr().String()); e != nil {
		return e
	}
	a, e := NewApplication(grabPath, templatePath, interpreter.Options{}, DefaultConfig(), output)
	if e != nil {
		return e
	}
	return serveApplication(ctx, l, a, output, ready)
}
func serveApplication(ctx context.Context, l net.Listener, a *Application, output io.Writer, ready chan<- string) error {
	defer a.Close()
	defer l.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	srv := &http.Server{Handler: a, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 65536}
	if output != nil {
		fmt.Fprintf(output, "grab: serving %s at http://%s/\n", a.grabPath, l.Addr())
	}
	if ready != nil {
		ready <- l.Addr().String()
		close(ready)
	}
	if a.cfg.Reload {
		go a.watch(ctx)
	}
	hup := make(chan os.Signal, 1)
	signals := reloadSignals()
	if len(signals) > 0 {
		signal.Notify(hup, signals...)
		defer signal.Stop(hup)
	}
	done := make(chan error, 1)
	go func() {
		e := srv.Serve(l)
		if errors.Is(e, http.ErrServerClosed) {
			e = nil
		}
		done <- e
	}()
	for {
		select {
		case <-hup:
			if e := a.Reload(); e != nil {
				a.logMu.Lock()
				fmt.Fprintln(a.output, "grab: reload rejected; continuing last-known-good application")
				a.logMu.Unlock()
			}
		case <-ctx.Done():
			a.draining.Store(true)
			shutdown, c := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
			e := srv.Shutdown(shutdown)
			c()
			if e != nil {
				_ = srv.Close()
			}
			return <-done
		case e := <-done:
			return e
		}
	}
}
