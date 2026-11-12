package realtime

import (
	"context"
	"net/http"

	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	rt "grab/internal/runtime"
)

func TestExpiryShutdownSizeRateAndCaps(t *testing.T) {
	for _, tt := range []struct {
		name string
		code websocket.StatusCode
	}{{"expiry", 1008}, {"shutdown", 1012}, {"oversize", 1009}, {"rate", 1008}} {
		t.Run(tt.name, func(t *testing.T) {
			ch, srv, _ := fixture(t)
			expires := time.Now().Add(time.Hour)
			if tt.name == "expiry" {
				expires = time.Now().Add(150 * time.Millisecond)
			}
			ch.Config.PingIntervalMS = 2000
			if tt.name == "oversize" {
				ch.Config.MaxMessageBytes = 128
			}
			if tt.name == "rate" {
				ch.Config.MessagesPerSecond = 1
			}
			srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ch.Serve(w, r, Authorization{rt.Text("principal"), []string{"tenant-a"}, expires}, func(context.Context, rt.Value, string, rt.Value) error { return nil })
			})
			c, _, e := dial(t, srv, http.Header{"Origin": {"https://shop.example"}})
			if e != nil {
				t.Fatal(e)
			}
			defer c.CloseNow()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			switch tt.name {
			case "shutdown":
				ch.Broker.Close()
			case "oversize":
				_ = c.Write(ctx, websocket.MessageText, []byte(strings.Repeat("x", 256)))
			case "rate":
				_ = c.Write(ctx, websocket.MessageText, []byte(`{"audience":"tenant-a","value":"one"}`))
				_ = c.Write(ctx, websocket.MessageText, []byte(`{"audience":"tenant-a","value":"two"}`))
			}
			_, _, e = c.Read(ctx)
			if websocket.CloseStatus(e) != tt.code {
				t.Fatalf("%v want close %d", e, tt.code)
			}
		})
	}
}
func TestSerializedHandlersAndIncomingOverload(t *testing.T) {
	ch, srv, _ := fixture(t)
	ch.Config.IncomingMessages = 1
	ch.Config.PingIntervalMS = 2000
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ch.Serve(w, r, Authorization{rt.Text("p"), []string{"tenant-a"}, time.Now().Add(time.Hour)}, func(ctx context.Context, _ rt.Value, _ string, _ rt.Value) error {
			select {
			case <-started:
			default:
				close(started)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
				return nil
			}
		})
	})
	c, _, e := dial(t, srv, http.Header{"Origin": {"https://shop.example"}})
	if e != nil {
		t.Fatal(e)
	}
	defer c.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	raw := []byte(`{"audience":"tenant-a","value":"one"}`)
	_ = c.Write(ctx, websocket.MessageText, raw)
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("no handler")
	}
	_ = c.Write(ctx, websocket.MessageText, raw)
	_ = c.Write(ctx, websocket.MessageText, raw)
	_, _, e = c.Read(ctx)
	if websocket.CloseStatus(e) != 1013 {
		t.Fatal(e)
	}
}
func TestFragmentedMessage(t *testing.T) {
	_, srv, _ := fixture(t)
	c, _, e := dial(t, srv, http.Header{"Origin": {"https://shop.example"}})
	if e != nil {
		t.Fatal(e)
	}
	defer c.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	writer, e := c.Writer(ctx, websocket.MessageText)
	if e != nil {
		t.Fatal(e)
	}
	for _, part := range []string{`{"audience":`, `"tenant-a",`, `"value":"fragmented"}`} {
		if _, e = writer.Write([]byte(part)); e != nil {
			t.Fatal(e)
		}
	}
	if e = writer.Close(); e != nil {
		t.Fatal(e)
	}
	_, raw, e := c.Read(ctx)
	if e != nil || !strings.Contains(string(raw), "fragmented") {
		t.Fatal(string(raw), e)
	}
}
