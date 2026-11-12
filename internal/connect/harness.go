package connect

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// DialContext runs the reference provider over an in-memory HTTP connection.
// It performs no TCP/DNS I/O and obeys the calling operation's cancellation.
func (h *Harness) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		r, e := http.ReadRequest(bufio.NewReader(server))
		if e != nil {
			return
		}
		defer r.Body.Close()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		res := w.Result()
		defer res.Body.Close()
		_ = res.Write(server)
	}()
	go func() {
		select {
		case <-ctx.Done():
			client.Close()
			server.Close()
		case <-done:
		}
	}()
	return client, nil
}

// Harness is the provider-neutral SDK fake. It has no external side effects.
// Each credential has an independent idempotency namespace.
type Harness struct {
	mu      sync.Mutex
	results map[string][]byte
	digests map[string]string
	Calls   int
}

func (h *Harness) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || r.Header.Get("Authorization") == "Bearer " {
		http.Error(w, "unauthorized", 401)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/messages/") {
		io.WriteString(w, `{"message":"reference"}`)
		return
	}
	if r.Method != "POST" || r.URL.Path != "/messages" {
		http.NotFound(w, r)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		http.Error(w, "key required", 400)
		return
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if e != nil || len(b) > 1<<20 {
		http.Error(w, "body limit", 413)
		return
	}
	sum := sha256.Sum256(b)
	digest := hex.EncodeToString(sum[:])
	identity := r.Header.Get("Authorization") + ":" + key
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.results == nil {
		h.results = map[string][]byte{}
		h.digests = map[string]string{}
	}
	if old, ok := h.results[identity]; ok {
		if h.digests[identity] != digest {
			http.Error(w, "key conflict", 409)
			return
		}
		w.Write(old)
		return
	}
	h.Calls++
	h.results[identity] = append([]byte(nil), b...)
	h.digests[identity] = digest
	w.Write(b)
}
