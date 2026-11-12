// Package telemetry exports only fixed, bounded operational attributes. It never
// receives application bodies, identities, URLs, headers, database values or keys.
package telemetry

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"grab/internal/platform"
)

var metricNames = []string{"requests_admitted", "requests_rejected", "requests_completed", "request_duration_nanoseconds", "connections_active", "cache_hits", "cache_errors", "rate_denials", "event_attempts", "event_dead", "lease_recovery", "worker_ready", "worker_restarts", "queue_used", "pool_used", "trace_drops", "event_pending", "inbox_lag_milliseconds", "outbox_lag_milliseconds"}

type Registry struct {
	tracer atomic.Pointer[Manager]
	values [19]atomic.Int64
	mu     sync.Mutex
	routes map[string]*routeMetrics
}

func (r *Registry) Add(name string, n int64) {
	if r == nil {
		return
	}
	for i, k := range metricNames {
		if k == name {
			r.values[i].Add(n)
			return
		}
	}
}
func (r *Registry) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	w.Header().Set("Content-Type", "application/openmetrics-text; version=1.0.0; charset=utf-8")
	for i, k := range metricNames {
		fmt.Fprintf(w, "# TYPE grab_%s gauge\ngrab_%s %d\n", k, k, r.values[i].Load())
	}
	r.writeRoutes(w)
	fmt.Fprintln(w, "# EOF")
}

type Span struct {
	TraceID      string `json:"traceId"`
	SpanID       string `json:"spanId"`
	ParentSpanID string `json:"parentSpanId,omitempty"`
	Name         string `json:"name"`
	Start        string `json:"startTimeUnixNano"`
	End          string `json:"endTimeUnixNano"`
	Kind         int    `json:"kind"`
	Links        []Link `json:"links,omitempty"`
}
type Link struct {
	TraceID string `json:"traceId"`
	SpanID  string `json:"spanId"`
}

// Work records only opaque, host-generated work identity hashes. Attempts share
// a trace and link to the previous attempt across processes and lease recovery;
// provider IDs, tenant IDs and payloads never become attributes.
func (r *Registry) Work(kind, identity string, attempt int, start time.Time) {
	if r == nil {
		return
	}
	if m := r.tracer.Load(); m != nil {
		m.recordWork(kind, identity, attempt, start)
	}
}

// TraceParent accepts version 00 only, canonical lowercase hex, nonzero IDs and
// known flags. Baggage/tracestate and the remote sampling bit are never retained.
func TraceParent(raw string) (string, string, bool) {
	if len(raw) != 55 || raw[:3] != "00-" || raw[35] != '-' || raw[52] != '-' {
		return "", "", false
	}
	if raw[53:] != "00" && raw[53:] != "01" {
		return "", "", false
	}
	for _, s := range []string{raw[3:35], raw[36:52]} {
		if strings.Trim(s, "0") == "" {
			return "", "", false
		}
		for _, c := range s {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return "", "", false
			}
		}
	}
	return raw[3:35], raw[36:52], true
}

type exporter struct {
	pending      *atomic.Int64
	tokenPointer *atomic.Pointer[string]
	config       platform.Telemetry
	token        string
	client       *http.Client
	queue        chan Span
	rate         float64
	registry     *Registry
}
type Manager struct {
	pending   atomic.Int64
	closed    atomic.Bool
	config    map[string]platform.Telemetry
	tokens    map[string]*atomic.Pointer[string]
	Registry  *Registry
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	servers   []*http.Server
	exporters []*exporter
	once      sync.Once
}

func New(ctx context.Context, cfg *platform.Config, grants *platform.Grants, offset int, registries ...*Registry) (*Manager, error) {
	ctx, cancel := context.WithCancel(ctx)
	m := &Manager{Registry: &Registry{}, cancel: cancel, config: cfg.Telemetry(), tokens: map[string]*atomic.Pointer[string]{}}
	if len(registries) > 0 && registries[0] != nil {
		m.Registry = registries[0]
	}
	success := false
	defer func() {
		if !success {
			m.Close()
		}
	}()
	for alias, d := range cfg.Telemetry() {
		if grants.Telemetry(alias) != nil {
			continue
		}
		token, e := platform.Secret(d.TokenEnv)
		if e != nil || len(token) < 32 {
			return nil, platform.ErrInvalid
		}
		pointer := &atomic.Pointer[string]{}
		pointer.Store(&token)
		m.tokens[alias] = pointer
		if d.Driver == "openmetrics" {
			if d.Port+offset > 65535 {
				return nil, platform.ErrInvalid
			}
			l, e := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", d.Port+offset))
			if e != nil {
				return nil, platform.ErrInvalid
			}
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/metrics" {
					http.NotFound(w, r)
					return
				}
				if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+tokenValue(pointer, ""))) != 1 {
					http.Error(w, "unauthorized", 401)
					return
				}
				m.Registry.ServeHTTP(w, r)
			})
			srv := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, MaxHeaderBytes: 8192}
			m.servers = append(m.servers, srv)
			m.wg.Add(1)
			go func() { defer m.wg.Done(); _ = srv.Serve(l) }()
		} else {
			client, e := collectorClient(ctx, d)
			if e != nil {
				return nil, e
			}
			rate := 0.01
			if d.SampleRate != nil {
				rate = *d.SampleRate
			}
			x := &exporter{pending: &m.pending, tokenPointer: pointer, config: d, token: token, client: client, queue: make(chan Span, 2048), rate: rate, registry: m.Registry}
			m.exporters = append(m.exporters, x)
			m.wg.Add(1)
			go func() { defer m.wg.Done(); x.run(ctx) }()
		}
	}
	success = true
	m.Registry.tracer.Store(m)
	return m, nil
}
func collectorClient(ctx context.Context, d platform.Telemetry) (*http.Client, error) {
	u, e := url.Parse(d.Endpoint)
	if e != nil {
		return nil, platform.ErrInvalid
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "443"
	}
	op, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ips, e := net.DefaultResolver.LookupIPAddr(op, host)
	if e != nil || len(ips) == 0 {
		return nil, platform.ErrInvalid
	}
	for _, ip := range ips {
		blocked := ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast()
		if ip.IP.IsUnspecified() || ip.IP.IsMulticast() || ip.IP.IsLinkLocalMulticast() || ip.IP.IsLinkLocalUnicast() {
			return nil, platform.ErrInvalid
		}
		if blocked {
			allowed := false
			for _, raw := range d.AllowedCIDRs {
				_, cidr, _ := net.ParseCIDR(raw)
				if cidr != nil && cidr.Contains(ip.IP) {
					allowed = true
				}
			}
			if !allowed {
				return nil, platform.ErrInvalid
			}
		}
	}
	address := net.JoinHostPort(ips[0].IP.String(), port)
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: host}, MaxConnsPerHost: 1, MaxIdleConns: 1, ResponseHeaderTimeout: 5 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", address)
		}}
	return &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

// Record accepts a prevalidated route template, never a raw URL. It exports no
// caller-supplied attributes and makes no synchronous collector calls.
func (m *Manager) Record(route, parent string, start time.Time) {
	m.record(route, parent, start, nil)
}
func (m *Manager) recordWork(kind, identity string, attempt int, start time.Time) {
	if kind != "inbox" && kind != "outbox" || attempt < 1 {
		return
	}
	trace := sha256.Sum256([]byte("grab-work-v1:" + identity))
	span := sha256.Sum256([]byte("grab-attempt-v1:" + identity + ":" + strconv.Itoa(attempt)))
	value := Span{TraceID: hex.EncodeToString(trace[:16]), SpanID: hex.EncodeToString(span[:8]), Kind: 5}
	if attempt > 1 {
		prior := sha256.Sum256([]byte("grab-attempt-v1:" + identity + ":" + strconv.Itoa(attempt-1)))
		value.Links = []Link{{value.TraceID, hex.EncodeToString(prior[:8])}}
	}
	m.record(kind, "", start, &value)
}
func (m *Manager) record(route, parent string, start time.Time, work *Span) {
	if m == nil || m.closed.Load() {
		return
	}
	if len(route) > 256 {
		route = "unmatched"
	}
	for _, x := range m.exporters {
		var sample [8]byte
		if _, e := rand.Read(sample[:]); e != nil {
			continue
		}
		n := uint64(0)
		for _, b := range sample {
			n = n<<8 | uint64(b)
		}
		if float64(n)/float64(^uint64(0)) >= x.rate {
			continue
		}
		traceID, parentID, valid := TraceParent(parent)
		if !valid {
			b := make([]byte, 16)
			if _, e := rand.Read(b); e != nil {
				continue
			}
			traceID = hex.EncodeToString(b)
		}
		id := make([]byte, 8)
		if _, e := rand.Read(id); e != nil {
			continue
		}
		if m.pending.Add(1) > 2048 {
			m.pending.Add(-1)
			m.Registry.Add("trace_drops", 1)
			continue
		}
		span := Span{TraceID: traceID, SpanID: hex.EncodeToString(id), ParentSpanID: parentID, Name: route, Start: strconv.FormatInt(start.UnixNano(), 10), End: strconv.FormatInt(time.Now().UnixNano(), 10), Kind: 2}
		if work != nil {
			span.TraceID, span.SpanID, span.Kind, span.Links = work.TraceID, work.SpanID, work.Kind, work.Links
		}
		select {
		case x.queue <- span:
		default:
			m.pending.Add(-1)
			m.Registry.Add("trace_drops", 1)
		}
	}
}
func (x *exporter) run(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	defer x.client.CloseIdleConnections()
	for {
		select {
		case <-ctx.Done():
			x.registry.Add("trace_drops", int64(len(x.queue)))
			if x.pending != nil {
				x.pending.Add(-int64(len(x.queue)))
			}
			return
		case <-tick.C:
		}
		x.flush(ctx)
	}
}
func (x *exporter) flush(ctx context.Context) {
	spans := make([]Span, 0, 512)
	for len(spans) < 512 {
		select {
		case s := <-x.queue:
			spans = append(spans, s)
		default:
			goto send
		}
	}
send:
	if len(spans) == 0 {
		return
	}
	payload := map[string]any{"resourceSpans": []any{map[string]any{"resource": map[string]any{"attributes": []any{map[string]any{"key": "service.name", "value": map[string]string{"stringValue": "grab"}}}}, "scopeSpans": []any{map[string]any{"scope": map[string]string{"name": "grab"}, "spans": spans}}}}}
	raw, _ := json.Marshal(payload)
	op, cancel := context.WithTimeout(ctx, 5*time.Second)
	req, _ := http.NewRequestWithContext(op, "POST", x.config.Endpoint, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokenValue(x.tokenPointer, x.token))
	response, e := x.client.Do(req)
	failed := e != nil
	if response != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 8192))
		response.Body.Close()
		failed = failed || response.StatusCode < 200 || response.StatusCode >= 300
	}
	cancel()
	if x.pending != nil {
		x.pending.Add(-int64(len(spans)))
	}
	if failed {
		x.registry.Add("trace_drops", int64(len(spans)))
	}
}

func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.once.Do(func() {
		m.closed.Store(true)
		m.cancel()
		for _, srv := range m.servers {
			srv.Close()
		}
		m.wg.Wait()
	})
}
