package realtime

import (
	"context"
	"encoding/json"

	"net/http"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
	"grab/internal/contracts"
	"grab/internal/platform"
	rt "grab/internal/runtime"
	"grab/internal/telemetry"
)

type Authorization struct {
	Principal rt.Value
	Audiences []string
	Expires   time.Time
}
type Receiver func(context.Context, rt.Value, string, rt.Value) error
type Channel struct {
	Metrics   *telemetry.Registry
	Config    platform.Channel
	Contracts *contracts.Document
	Broker    *Broker
}
type connection struct {
	ws        *websocket.Conn
	audiences map[string]bool
	out       chan []byte
	mu        sync.Mutex
	bytes     int
	maxBytes  int
	closeOnce sync.Once
	cancel    context.CancelFunc
	code      chan websocket.StatusCode
}

func (c *connection) stop(code websocket.StatusCode) {
	c.closeOnce.Do(func() { c.code <- code })
}
func (c *connection) enqueue(raw []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bytes+len(raw) > c.maxBytes {
		c.stop(1013)
		return
	}
	select {
	case c.out <- raw:
		c.bytes += len(raw)
	default:
		c.stop(1013)
	}
}
func (c *Channel) Publish(ctx context.Context, audience string, v rt.Value) (bool, error) {
	raw, e := c.Contracts.Encode(contracts.Schema{Type: "ref", Ref: c.Config.ServerType}, v)
	if e != nil || len(raw) > c.Config.MaxMessageBytes {
		return false, ErrPolicy
	}
	envelope, _ := json.Marshal(publication{audience, raw})
	if len(envelope) > c.Config.MaxMessageBytes {
		return false, ErrPolicy
	}
	return c.Broker.Publish(ctx, audience, raw)
}

// Serve assumes application authorization and TLS/proxy verification already ran.
func (c *Channel) Serve(w http.ResponseWriter, r *http.Request, auth Authorization, receive Receiver) {
	d := c.Config
	if r.Method != "GET" || r.URL.RawQuery != "" || len(r.Header.Values("Origin")) > 1 {
		http.Error(w, "channel policy rejected", 403)
		return
	}
	origin := r.Header.Get("Origin")
	allowed := origin == "" && d.AllowMissingOrigin
	for _, o := range d.Origins {
		if origin == o {
			allowed = true
		}
	}
	if !allowed || r.Header.Get("Sec-WebSocket-Extensions") != "" {
		http.Error(w, "channel policy rejected", 403)
		return
	}
	if auth.Expires.IsZero() || !auth.Expires.After(time.Now()) || len(auth.Audiences) == 0 || len(auth.Audiences) > d.MaxAudiences {
		http.Error(w, "channel authorization rejected", 403)
		return
	}
	audiences := map[string]bool{}
	for _, a := range auth.Audiences {
		if !Audience(a) || audiences[a] {
			http.Error(w, "channel authorization rejected", 403)
			return
		}
		audiences[a] = true
	}
	ctx, cancel := context.WithDeadline(r.Context(), auth.Expires)
	defer cancel()
	cl := &connection{audiences: audiences, out: make(chan []byte, d.OutgoingMessages), maxBytes: d.OutgoingBytes, cancel: cancel, code: make(chan websocket.StatusCode, 1)}
	b := c.Broker
	b.mu.Lock()
	if b.closed || len(b.clients) >= d.MaxConnections {
		b.mu.Unlock()
		http.Error(w, "channel unavailable", 503)
		return
	}
	// Reserve capacity before upgrading; publish may safely enqueue during handshake.
	b.clients[cl] = true
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.clients, cl); b.mu.Unlock() }()
	ws, e := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if e != nil {
		return
	}
	cl.ws = ws
	c.Metrics.Add("connections_active", 1)
	defer c.Metrics.Add("connections_active", -1)
	defer ws.CloseNow()
	ws.SetReadLimit(int64(d.MaxMessageBytes))
	incoming := make(chan publication, d.IncomingMessages)
	var wg sync.WaitGroup
	readCtx, readCancel := context.WithCancel(context.Background())
	defer readCancel()
	wg.Add(4)
	go func() {
		defer wg.Done()
		code := websocket.StatusPolicyViolation
		select {
		case code = <-cl.code:
		case <-ctx.Done():
		}
		_ = ws.Close(code, "reconnect and refetch authoritative HTTP data")
		cancel()
		readCancel()
	}()
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Duration(d.PingIntervalMS) * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				op, stop := context.WithTimeout(ctx, time.Duration(d.PongTimeoutMS)*time.Millisecond)
				e := ws.Ping(op)
				stop()
				if e != nil {
					cl.stop(1008)
					return
				}
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case raw := <-cl.out:
				cl.mu.Lock()
				cl.bytes -= len(raw)
				cl.mu.Unlock()
				// Revalidate broker input against this generation's contract. Another
				// process (or an older deployment) cannot bypass the wire boundary.
				var message publication
				if len(raw) > d.MaxMessageBytes || platform.StrictValueJSON(raw, &message) != nil {
					cl.stop(1007)
					return
				}
				if _, e := c.Contracts.Decode(contracts.Schema{Type: "ref", Ref: d.ServerType}, message.Value); e != nil {
					cl.stop(1007)
					return
				}
				op, stop := context.WithTimeout(ctx, time.Duration(d.PongTimeoutMS)*time.Millisecond)
				e := ws.Write(op, websocket.MessageText, raw)
				stop()
				if e != nil {
					cl.stop(1013)
					return
				}
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case p := <-incoming:
				v, e := c.Contracts.Decode(contracts.Schema{Type: "ref", Ref: d.ClientType}, p.Value)
				if e != nil {
					cl.stop(1007)
					return
				}
				op, stop := context.WithTimeout(ctx, time.Duration(d.HandlerTimeoutMS)*time.Millisecond)
				e = receive(op, auth.Principal, p.Audience, v)
				stop()
				if e != nil {
					cl.stop(1008)
					return
				}
			}
		}
	}()
	window := time.Now()
	count := 0
	for ctx.Err() == nil {
		typ, raw, e := ws.Read(readCtx)
		if e != nil {
			break
		}
		if time.Since(window) >= time.Second {
			window = time.Now()
			count = 0
		}
		count++
		if count > d.MessagesPerSecond {
			cl.stop(1008)
			break
		}
		if typ != websocket.MessageText || !utf8.Valid(raw) {
			cl.stop(1007)
			break
		}
		var p publication
		if platform.StrictValueJSON(raw, &p) != nil || p.Value == nil {
			cl.stop(1007)
			break
		}
		if !audiences[p.Audience] {
			cl.stop(1008)
			break
		}
		select {
		case incoming <- p:
		default:
			cl.stop(1013)
		}
	}
	cl.stop(websocket.StatusPolicyViolation)
	wg.Wait()
}

func (c *Channel) HandshakeAllowed(r *http.Request) bool {
	if r.Method != "GET" || r.URL.RawQuery != "" || len(r.Header.Values("Origin")) > 1 || r.Header.Get("Sec-WebSocket-Extensions") != "" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return c.Config.AllowMissingOrigin
	}
	for _, o := range c.Config.Origins {
		if origin == o {
			return true
		}
	}
	return false
}
