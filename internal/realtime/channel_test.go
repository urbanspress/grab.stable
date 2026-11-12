package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"grab/internal/contracts"
	"grab/internal/platform"
	rt "grab/internal/runtime"
)

func fixture(t *testing.T) (*Channel, *httptest.Server, *atomic.Int64) {
	t.Helper()
	d := platform.DefaultChannel()
	d.Origins = []string{"https://shop.example"}
	d.ClientType = "Message"
	d.ServerType = "Message"
	d.Broker.Driver = "memory"
	d.PingIntervalMS = 100
	d.PongTimeoutMS = 100
	doc, e := contracts.Parse([]byte(`{"version":1,"types":{"Message":{"type":"text","max_length":2048}}}`))
	if e != nil {
		t.Fatal(e)
	}
	broker, e := NewBroker(context.Background(), "shop", d.Broker)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(broker.Close)
	ch := &Channel{Config: d, Contracts: doc, Broker: broker}
	count := &atomic.Int64{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ch.Serve(w, r, Authorization{rt.Text("principal"), []string{"tenant-a"}, time.Now().Add(time.Hour)}, func(ctx context.Context, p rt.Value, a string, v rt.Value) error {
			count.Add(1)
			_, e := ch.Publish(ctx, a, v)
			return e
		})
	}))
	t.Cleanup(srv.Close)
	return ch, srv, count
}

func TestBrokerCannotBypassReceivingContract(t *testing.T) {
	channel, srv, _ := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ws, _, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"https://shop.example"}}})
	if e != nil {
		t.Fatal(e)
	}
	defer ws.CloseNow()
	channel.Broker.deliver("tenant-a", []byte(`{"audience":"tenant-a","value":123}`))
	_, _, e = ws.Read(ctx)
	if websocket.CloseStatus(e) != websocket.StatusInvalidFramePayloadData {
		t.Fatal(e)
	}
}
func dial(t *testing.T, srv *httptest.Server, headers http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), &websocket.DialOptions{HTTPHeader: headers})
}
func TestHandshakePolicy(t *testing.T) {
	_, srv, _ := fixture(t)
	for _, tt := range []struct {
		name, origin, extensions string
		ok                       bool
	}{
		{"allowed", "https://shop.example", "", true}, {"cross origin", "https://evil.example", "", false}, {"missing", "", "", false}, {"compression", "https://shop.example", "permessage-deflate", false}, {"prefix", "https://shop.example.evil", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			if tt.origin != "" {
				h.Set("Origin", tt.origin)
			}
			if tt.extensions != "" {
				h.Set("Sec-WebSocket-Extensions", tt.extensions)
			}
			c, r, e := dial(t, srv, h)
			if c != nil {
				c.CloseNow()
			}
			if r != nil && r.Body != nil {
				r.Body.Close()
			}
			if (e == nil) != tt.ok {
				t.Fatalf("%v", e)
			}
		})
	}
}
func TestContractAudienceAndBinary(t *testing.T) {
	for _, tt := range []struct {
		name, raw string
		typ       websocket.MessageType
		code      websocket.StatusCode
	}{
		{"valid", `{"audience":"tenant-a","value":"hello"}`, websocket.MessageText, 0},
		{"cross tenant", `{"audience":"tenant-b","value":"hello"}`, websocket.MessageText, 1008},
		{"contract", `{"audience":"tenant-a","value":42}`, websocket.MessageText, 1007},
		{"duplicate", `{"audience":"tenant-a","audience":"tenant-b","value":"hello"}`, websocket.MessageText, 1007},
		{"binary", `x`, websocket.MessageBinary, 1007},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, srv, count := fixture(t)
			c, _, e := dial(t, srv, http.Header{"Origin": {"https://shop.example"}})
			if e != nil {
				t.Fatal(e)
			}
			defer c.CloseNow()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if e = c.Write(ctx, tt.typ, []byte(tt.raw)); e != nil {
				t.Fatal(e)
			}
			_, raw, e := c.Read(ctx)
			if tt.code == 0 {
				if e != nil || !strings.Contains(string(raw), `"hello"`) || count.Load() != 1 {
					t.Fatal(string(raw), e, count.Load())
				}
			} else if websocket.CloseStatus(e) != tt.code {
				t.Fatalf("close %v want %d", e, tt.code)
			}
		})
	}
}
func TestQueueAdmissionAndAudienceIDs(t *testing.T) {
	for _, a := range []string{"", "a b", "a*", "a/b", strings.Repeat("a", 129)} {
		if Audience(a) {
			t.Fatal(a)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &connection{out: make(chan []byte, 1), maxBytes: 8, code: make(chan websocket.StatusCode, 1), cancel: cancel}
	c.enqueue([]byte("12345"))
	c.enqueue([]byte("6789"))
	select {
	case code := <-c.code:
		if code != 1013 {
			t.Fatal(code)
		}
	default:
		t.Fatal("queue overflow accepted")
	}
	_ = ctx
}
func TestRedisTwoBrokersAndOutage(t *testing.T) {
	port, _ := strconv.Atoi(os.Getenv("GRAB_TEST_REDIS_PORT"))
	if port == 0 {
		t.Skip("real Redis: set GRAB_TEST_REDIS_PORT")
	}
	d := platform.ChannelBroker{Driver: "redis", Namespace: fmt.Sprintf("test-%d", time.Now().UnixNano()), Host: "127.0.0.1", Port: port, TLS: "disabled", Development: true, PoolSize: 2, TimeoutMS: 1000}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a, e := NewBroker(ctx, "shop", d)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, e := NewBroker(ctx, "shop", d)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	c := &connection{audiences: map[string]bool{"tenant-a": true}, out: make(chan []byte, 2), maxBytes: 1024, code: make(chan websocket.StatusCode, 1), cancel: func() {}}
	b.mu.Lock()
	b.clients[c] = true
	b.mu.Unlock()
	if ok, e := a.Publish(ctx, "tenant-a", []byte(`"notice"`)); e != nil || !ok {
		t.Fatal(e)
	}
	select {
	case raw := <-c.out:
		var p publication
		if json.Unmarshal(raw, &p) != nil || p.Audience != "tenant-a" {
			t.Fatal(string(raw))
		}
	case <-ctx.Done():
		t.Fatal("no cross-worker publication")
	}
	b.sub.Close()
	select {
	case code := <-c.code:
		if code != 1013 {
			t.Fatal(code)
		}
	case <-ctx.Done():
		t.Fatal("outage not closed")
	}
	if _, e = b.Publish(ctx, "tenant-a", []byte(`"notice"`)); e == nil {
		t.Fatal("outage accepted")
	}
}
