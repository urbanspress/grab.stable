package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"grab/internal/contracts"
	"grab/internal/interpreter"
	grabrt "grab/internal/runtime"
	"grab/internal/telemetry"
)

// Config controls resource limits and deployment policy. Use DefaultConfig.
type Config struct {
	Workers, WorkerBasePort                       int
	WorkerArguments, WorkerFiles                  []string
	AllowPublic, Reload                           bool
	TrustedProxy, Static, LogFormat               string
	MaxConcurrency, MaxQueue                      int
	QueueTimeout, RequestTimeout, ShutdownTimeout time.Duration
	MaxBodyBytes                                  int64
	Diagnostics                                   interpreter.DiagnosticMode
}

func DefaultConfig() Config {
	return Config{Workers: 1, MaxConcurrency: 64, MaxQueue: 128, QueueTimeout: time.Second, RequestTimeout: 10 * time.Second, ShutdownTimeout: 10 * time.Second, MaxBodyBytes: 1048576, LogFormat: "text", Diagnostics: interpreter.ModeDeveloper}
}
func (c Config) Validate() error {
	if c.Workers < 1 || c.Workers > 32 {
		return fmt.Errorf("workers must be 1 through 32")
	}
	if c.MaxConcurrency < 1 || c.MaxQueue < 0 || c.QueueTimeout <= 0 || c.RequestTimeout <= 0 || c.ShutdownTimeout <= 0 || c.MaxBodyBytes < 1 {
		return fmt.Errorf("server limits must be positive (queue may be zero)")
	}
	if c.LogFormat != "text" && c.LogFormat != "json" {
		return fmt.Errorf("log format must be text or json")
	}
	if c.Static != "" {
		prefix, dir, ok := strings.Cut(c.Static, "=")
		if !ok {
			dir, prefix = prefix, "/static/"
		}
		if dir == "" || !strings.HasPrefix(prefix, "/") || !strings.HasSuffix(prefix, "/") || strings.ContainsAny(prefix, "?#\\\x00") || strings.Contains(prefix, "..") {
			return fmt.Errorf("static requires [url_prefix=]directory, with a /prefix/")
		}
	}
	_, e := parseProxies(c.TrustedProxy)
	return e
}
func ValidateAddr(addr string, allowPublic bool) error {
	if !allowPublic {
		return ValidateLoopbackAddr(addr)
	}
	_, port, e := net.SplitHostPort(addr)
	if e != nil {
		return e
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 0 || n > 65535 {
		return fmt.Errorf("invalid TCP port")
	}
	return nil
}
func parseProxies(raw string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	if raw == "" {
		return out, nil
	}
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		ip := net.ParseIP(s)
		if ip != nil {
			bits := 128
			if ip.To4() != nil {
				bits = 32
			}
			out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			continue
		}
		_, n, e := net.ParseCIDR(s)
		if e != nil {
			return nil, fmt.Errorf("invalid trusted proxy %q", s)
		}
		out = append(out, n)
	}
	return out, nil
}

// Application supervises atomic generations and a bounded execution gate.
type Application struct {
	telemetry                *telemetry.Manager
	active                   atomic.Pointer[PreparedApp]
	draining                 atomic.Bool
	ids                      atomic.Uint64
	slots, queue             chan struct{}
	cfg                      Config
	proxies                  []*net.IPNet
	grabPath, templatePath   string
	opts                     interpreter.Options
	output                   io.Writer
	logMu, reloadMu          sync.Mutex
	staticRoot, staticPrefix string
}

// Close retires the active resource generation after its final request drains.
func (a *Application) Close() {
	a.draining.Store(true)
	a.telemetry.Close()
	a.reloadMu.Lock()
	p := a.active.Swap(nil)
	a.reloadMu.Unlock()
	if p != nil {
		p.retire()
	}
}

func NewApplication(grabPath, templatePath string, opts interpreter.Options, cfg Config, output io.Writer) (*Application, error) {
	if e := cfg.Validate(); e != nil {
		return nil, e
	}
	if output == nil {
		output = io.Discard
	}
	if opts.Metrics == nil {
		opts.Metrics = &telemetry.Registry{}
	}
	a := &Application{cfg: cfg, slots: make(chan struct{}, cfg.MaxConcurrency), queue: make(chan struct{}, cfg.MaxQueue), grabPath: grabPath, templatePath: templatePath, opts: opts, output: output}
	a.proxies, _ = parseProxies(cfg.TrustedProxy)
	if cfg.Static != "" {
		prefix, dir, ok := strings.Cut(cfg.Static, "=")
		if !ok {
			dir = prefix
			prefix = "/static/"
		}
		if !strings.HasPrefix(prefix, "/") || strings.ContainsAny(prefix, "?#\\") || strings.Contains(prefix, "..") || !strings.HasSuffix(prefix, "/") {
			return nil, fmt.Errorf("static prefix must start and end with /")
		}
		root, e := filepath.Abs(dir)
		if e != nil {
			return nil, e
		}
		root, e = filepath.EvalSymlinks(root)
		if e != nil {
			return nil, e
		}
		st, e := os.Stat(root)
		if e != nil || !st.IsDir() {
			return nil, fmt.Errorf("static root must be a directory")
		}
		a.staticRoot, a.staticPrefix = root, prefix
	}
	if e := a.Reload(); e != nil {
		return nil, e
	}
	return a, nil
}
func (a *Application) Reload() error {
	a.reloadMu.Lock()
	defer a.reloadMu.Unlock()
	if a.draining.Load() {
		return fmt.Errorf("application is draining")
	}
	// Initialization output is separate from request logs and serialized.
	var startup bytes.Buffer
	p, e := Load(a.grabPath, a.templatePath, a.opts, &startup)
	if e != nil {
		return e
	}
	for _, d := range p.code.PlatformConfiguration().Channels() {
		if a.staticPrefix != "" && strings.HasPrefix(d.Path, a.staticPrefix) {
			p.retire()
			return fmt.Errorf("channel collides with static path")
		}
	}
	commitTelemetry, e := a.telemetry.PrepareReload(context.Background(), p.code.PlatformConfiguration(), p.code.PlatformCapabilities())
	if e != nil {
		p.retire()
		return e
	}
	var routes []string
	for _, r := range p.code.Routes() {
		routes = append(routes, r.Definition.Path)
	}
	a.opts.Metrics.ConfigureRoutes(routes)
	old := a.active.Swap(p)
	commitTelemetry()
	if old != nil {
		old.retire()
	}
	a.logMu.Lock()
	_, _ = a.output.Write(startup.Bytes())
	a.logMu.Unlock()
	return nil
}
func (a *Application) changed() bool {
	p := a.active.Load()
	if p == nil {
		return false
	}
	for f, t := range p.files {
		st, e := os.Stat(f)
		if e != nil || !st.ModTime().Equal(t) {
			return true
		}
	}
	return false
}
func (a *Application) watch(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if a.changed() {
				if e := a.Reload(); e != nil {
					a.logMu.Lock()
					fmt.Fprintln(a.output, "grab: reload rejected; continuing last-known-good application")
					a.logMu.Unlock()
				}
			}
		}
	}
}
func (a *Application) trusted(ip net.IP) bool {
	for _, n := range a.proxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
func (a *Application) metadata(r *http.Request) (ip, scheme, host string) {
	ip, _, _ = net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}
	scheme = "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host = r.Host
	if !a.trusted(net.ParseIP(ip)) {
		return
	}
	chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	valid := true
	for _, s := range chain {
		if net.ParseIP(strings.TrimSpace(s)) == nil {
			valid = false
		}
	}
	if valid {
		for i := len(chain) - 1; i >= 0; i-- {
			if !a.trusted(net.ParseIP(ip)) {
				break
			}
			ip = strings.TrimSpace(chain[i])
		}
	}
	if s := r.Header.Get("X-Forwarded-Proto"); s == "http" || s == "https" {
		scheme = s
	}
	if h := r.Header.Get("X-Forwarded-Host"); h != "" && validHost(h) {
		host = h
	}
	return
}
func validHost(h string) bool {
	if strings.ContainsAny(h, "/\\?#@, \t\r\n\x00") {
		return false
	}
	u, e := url.Parse("http://" + h)
	if e != nil || u.Host != h || u.Hostname() == "" {
		return false
	}
	for _, c := range u.Hostname() {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune(".-:", c)) {
			return false
		}
	}
	return true
}
func mapStrings(m map[string][]string, lower bool) grabrt.Value {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var entries []grabrt.Entry
	for _, k := range keys {
		if len(m[k]) == 0 {
			continue
		}
		name := k
		if lower {
			name = strings.ToLower(k)
		}
		value := m[k][0]
		if lower {
			value = strings.TrimSpace(value)
		}
		entries = append(entries, grabrt.Entry{Name: name, Value: grabrt.Text(value)})
	}
	return grabrt.Map(entries)
}
func (a *Application) acquire(ctx context.Context) bool {
	select {
	case a.slots <- struct{}{}:
		return true
	default:
	}
	select {
	case a.queue <- struct{}{}:
	default:
		return false
	}
	defer func() { <-a.queue }()
	timer := time.NewTimer(a.cfg.QueueTimeout)
	defer timer.Stop()
	select {
	case a.slots <- struct{}{}:
		return true
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
}

type requestLog struct {
	Timestamp  string  `json:"timestamp"`
	Level      string  `json:"level"`
	RequestID  string  `json:"request_id"`
	Method     string  `json:"method"`
	Path       string  `json:"path"`
	Route      string  `json:"route"`
	Status     int     `json:"status"`
	DurationMS float64 `json:"duration_ms"`
	ClientIP   string  `json:"client_ip"`
	ErrorCode  string  `json:"error_code,omitempty"`
	Error      string  `json:"error,omitempty"`
}

func redact(message string, r *http.Request, requestBody, responseBody []byte) string {
	for _, key := range []string{"Authorization", "Proxy-Authorization", "Cookie", "Set-Cookie", "X-API-Key"} {
		for _, value := range r.Header.Values(key) {
			if value != "" {
				message = strings.ReplaceAll(message, value, "[redacted]")
			}
		}
	}
	for _, value := range [][]byte{requestBody, responseBody} {
		if len(value) > 0 {
			message = strings.ReplaceAll(message, string(value), "[redacted body]")
		}
	}
	return message
}

func (a *Application) console(id, message string) {
	if message == "" {
		return
	}
	a.logMu.Lock()
	defer a.logMu.Unlock()
	if a.cfg.LogFormat == "json" {
		_ = json.NewEncoder(a.output).Encode(map[string]string{"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "level": "info", "request_id": id, "event": "console", "message": message})
	} else {
		fmt.Fprintf(a.output, "grab: console request_id=%s message=%q\n", id, message)
	}
}

func (a *Application) log(entry requestLog) {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	if a.cfg.LogFormat == "json" {
		_ = json.NewEncoder(a.output).Encode(entry)
	} else {
		fmt.Fprintf(a.output, "%s %s request_id=%s method=%q path=%q route=%q status=%d duration_ms=%.3f client_ip=%q error_code=%s\n", entry.Timestamp, entry.Level, entry.RequestID, entry.Method, entry.Path, entry.Route, entry.Status, entry.DurationMS, entry.ClientIP, entry.ErrorCode)
	}
}
func (a *Application) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if a.telemetry != nil {
		a.telemetry.Registry.Add("requests_admitted", 1)
	}
	id := fmt.Sprintf("req_%x_%x", start.UnixNano(), a.ids.Add(1))
	ip, scheme, host := a.metadata(r)
	entry := requestLog{Timestamp: start.UTC().Format(time.RFC3339Nano), Level: "info", RequestID: id, Method: r.Method, Path: r.URL.Path, ClientIP: ip, Status: 500}
	setSecurityHeaders(w.Header())
	w.Header().Set("X-Request-ID", id)
	// All application writes are buffered; panic recovery can always emit a clean error.
	status := 500
	headers := http.Header{}
	var body []byte
	var requestBody []byte
	contracted := false
	upgraded := false
	fail := func(n int, message string) {
		status = n
		headers.Set("Content-Type", "text/plain; charset=utf-8")
		body = []byte(message + "\n")
	}
	defer func() {
		if recover() != nil {
			entry.ErrorCode = "GRAB-E301"
			fail(500, "grab: internal server error; request_id="+id)
		}
		if upgraded {
			return
		}
		if contracted && status >= 400 && !strings.HasPrefix(headers.Get("Content-Type"), "application/json") {
			headers.Set("Content-Type", "application/json; charset=utf-8")
			body = contracts.ErrorBody("GRAB-E315", http.StatusText(status), id)
		}
		entry.Error = redact(entry.Error, r, requestBody, body)
		for k, v := range headers {
			w.Header()[k] = v
		}
		setSecurityHeaders(w.Header())
		entry.Status = status
		entry.DurationMS = float64(time.Since(start).Microseconds()) / 1000
		if status >= 500 {
			entry.Level = "error"
		}
		a.log(entry)
		if a.telemetry != nil {
			a.telemetry.Registry.Observe(entry.Route, status, time.Since(start))
			a.telemetry.Registry.Set("queue_used", int64(len(a.queue)))
			a.telemetry.Registry.Add("requests_completed", 1)
			a.telemetry.Registry.Add("request_duration_nanoseconds", time.Since(start).Nanoseconds())
			if status == 503 {
				a.telemetry.Registry.Add("requests_rejected", 1)
			}
			a.telemetry.Record(entry.Route, r.Header.Get("Traceparent"), start)
		}
		w.WriteHeader(status)
		if r.Method != "HEAD" && status != 204 && status != 304 && status >= 200 {
			_, _ = w.Write(body)
		}
	}()
	if (r.URL.Path == "/_health" || r.URL.Path == "/_ready") && (r.Method == "GET" || r.Method == "HEAD") {
		status = 200
		headers.Set("Content-Type", "application/json")
		body = []byte(`{"status":"ok"}`)
		if r.URL.Path == "/_ready" {
			if !a.isReady() || a.draining.Load() || len(a.slots) == cap(a.slots) && len(a.queue) == cap(a.queue) {
				status = 503
				body = []byte(`{"status":"draining"}`)
			} else {
				body = []byte(fmt.Sprintf(`{"status":"ready","in_flight":%d,"max_concurrency":%d}`, len(a.slots), cap(a.slots)))
			}
		}
		return
	}
	if a.draining.Load() {
		headers.Set("Retry-After", "1")
		fail(503, "service unavailable")
		return
	}
	if a.staticRoot != "" && strings.HasPrefix(r.URL.Path, a.staticPrefix) {
		status, headers, body = a.static(r)
		return
	}
	var p *PreparedApp
	for {
		p = a.active.Load()
		if p == nil {
			if a.draining.Load() {
				fail(503, "service unavailable")
				return
			}
			panic("missing active application")
		}
		if p.acquire() {
			break
		}
	}
	defer p.release()
	if alias, ok := p.code.Channel(r.URL.Path); ok {
		if !a.acquire(r.Context()) {
			fail(503, "service unavailable")
			return
		}
		var released bool
		release := func() {
			if !released {
				released = true
				<-a.slots
			}
		}
		defer release()
		upgraded = true
		request := grabrt.Record([]grabrt.Entry{{Name: "method", Value: grabrt.Text(r.Method)}, {Name: "path", Value: grabrt.Text(r.URL.Path)}, {Name: "headers", Value: mapStrings(r.Header, true)}, {Name: "scheme", Value: grabrt.Text(scheme)}, {Name: "client_ip", Value: grabrt.Text(ip)}})
		p.code.ServeChannel(w, r, alias, scheme, request, release)
		return
	}
	if hook, ok := p.code.Webhook(r.URL.Path); ok {
		if r.Method != "POST" {
			headers.Set("Allow", "POST")
			fail(405, "method not allowed")
			return
		}
		if !a.acquire(r.Context()) {
			fail(503, "service unavailable")
			return
		}
		defer func() { <-a.slots }()
		raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if e != nil {
			fail(413, "payload too large")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), a.cfg.RequestTimeout)
		defer cancel()
		status, body = p.code.AcceptWebhook(ctx, hook, r.Header, raw)
		headers.Set("Content-Type", "text/plain; charset=utf-8")
		return
	}
	var route *interpreter.Route
	params := grabrt.Record(nil)
	allowed := map[string]bool{}
	escaped := r.URL.EscapedPath()
	for _, candidate := range p.code.Routes() {
		values, ok := candidate.Match(escaped)
		if !ok {
			continue
		}
		m := candidate.Definition.Method
		allowed[m] = true
		if m == "GET" {
			allowed["HEAD"] = true
		}
		if route == nil && (m == r.Method || r.Method == "HEAD" && m == "GET") {
			route = candidate
			params = values
		}
		if r.Method == "HEAD" && m == "HEAD" && (route == nil || route.Definition.Method != "HEAD") {
			route = candidate
			params = values
		}
	}
	if p.legacy != nil && r.URL.Path == "/" {
		allowed["GET"], allowed["HEAD"] = true, true
	}
	if route == nil && !(p.legacy != nil && r.URL.Path == "/" && (r.Method == "GET" || r.Method == "HEAD")) {
		if len(allowed) > 0 {
			var methods []string
			for m := range allowed {
				methods = append(methods, m)
			}
			sort.Strings(methods)
			headers.Set("Allow", strings.Join(methods, ", "))
			fail(405, "405 method not allowed")
		} else {
			fail(404, "404 page not found")
			if strings.Contains(r.Header.Get("Accept"), "application/json") {
				headers.Set("Content-Type", "application/json")
				body = []byte(`{"error":"not found"}`)
			}
		}
		return
	}
	if route != nil {
		_, contracted = p.code.ContractOperation(route)
		entry.Route = route.Definition.Path
	} else {
		entry.Route = "/"
	}
	if !a.acquire(r.Context()) {
		headers.Set("Retry-After", "1")
		fail(503, "service unavailable")
		return
	}
	defer func() { <-a.slots }()
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, a.cfg.MaxBodyBytes))
	requestBody = raw
	if e != nil {
		var max *http.MaxBytesError
		if errors.As(e, &max) {
			fail(413, "payload too large")
		} else {
			fail(400, "bad request body")
		}
		return
	}
	if !utf8.Valid(raw) {
		fail(400, "body must be UTF-8")
		return
	}
	form := url.Values{}
	if media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); media == "application/x-www-form-urlencoded" {
		form, e = url.ParseQuery(string(raw))
		if e != nil {
			fail(400, "invalid form body")
			return
		}
	}
	queryValue := mapStrings(r.URL.Query(), false)
	if contracted {
		values, e := url.ParseQuery(r.URL.RawQuery)
		if e != nil {
			fail(400, "invalid query")
			return
		}
		var es []grabrt.Entry
		for key, vs := range values {
			var list []grabrt.Value
			for _, v := range vs {
				list = append(list, grabrt.Text(v))
			}
			es = append(es, grabrt.Entry{Name: key, Value: grabrt.List(list)})
		}
		queryValue = grabrt.Map(es)
	}
	req := grabrt.Record([]grabrt.Entry{
		{Name: "method", Value: grabrt.Text(r.Method)}, {Name: "path", Value: grabrt.Text(r.URL.Path)}, {Name: "params", Value: params},
		{Name: "query", Value: queryValue}, {Name: "headers", Value: mapStrings(r.Header, true)},
		{Name: "body", Value: grabrt.Text(string(raw))}, {Name: "client_ip", Value: grabrt.Text(ip)}, {Name: "scheme", Value: grabrt.Text(scheme)},
		{Name: "form", Value: mapStrings(form, false)},
		{Name: "host", Value: grabrt.Text(host)}, {Name: "request_id", Value: grabrt.Text(id)},
	})
	ctx, cancel := context.WithTimeout(r.Context(), a.cfg.RequestTimeout)
	defer cancel()
	var console bytes.Buffer

	if contracted {
		apiFail := func(n int, message string) {
			fail(n, message)
			headers.Set("Content-Type", "application/json; charset=utf-8")
			body = contracts.ErrorBody("GRAB-E315", message, id)
		}
		if len(raw) > 1<<20 {
			apiFail(413, "payload too large")
			return
		}
		if len(raw) > 0 {
			media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || media != "application/json" {
				apiFail(400, "expected JSON content type")
				return
			}
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("Cookie") != "" {
			origin := r.Header.Values("Origin")
			if len(origin) != 1 || origin[0] != scheme+"://"+host || r.Header.Get("X-CSRF-Token") == "" {
				apiFail(403, "mutation origin and CSRF token required")
				return
			}
		}
		o, _ := p.code.ContractOperation(route)
		if _, err := p.code.APIInput(o, req); err != nil {
			apiFail(400, "invalid API input")
			return
		}
	}
	res, e := p.execute(ctx, route, req, &console)
	for _, value := range res.Headers.Values("Set-Cookie") {
		if value != "" {
			consoleText := strings.ReplaceAll(console.String(), value, "[redacted]")
			console.Reset()
			console.WriteString(consoleText)
		}
	}
	a.console(id, redact(console.String(), r, raw, res.Body))
	if ctx.Err() != nil {
		entry.ErrorCode = "GRAB-E311"
		fail(504, "request timed out; request_id="+id)
		return
	}
	if e != nil {
		if contracted {
			fail(500, "API response failed")
			headers.Set("Content-Type", "application/json; charset=utf-8")
			body = contracts.ErrorBody("GRAB-E315", "API response failed", id)
			return
		}
		entry.ErrorCode, _, _, _ = interpreter.ExtractErrorMeta(e)
		entry.Error = interpreter.FormatDiagnostic("", e, interpreter.ModeDeveloper)
		n := 500
		if strings.Contains(e.Error(), "bad request payload:") {
			n = 400
		}
		message := "grab: internal server error; request_id=" + id
		if a.cfg.Diagnostics != interpreter.ModeProduction {
			message = fmt.Sprintf("grab: [%s] %v; request_id=%s", entry.ErrorCode, e, id)
		}
		fail(n, message)
		return
	}
	status, headers, body = res.Status, res.Headers, res.Body
}

func sensitive(path string) bool {
	for _, s := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		s = strings.ToLower(s)
		if s == ".env" || strings.HasPrefix(s, ".env.") || s == ".git" || s == ".ssh" || s == "id_rsa" || s == "id_ed25519" || strings.HasSuffix(s, ".pem") || strings.HasSuffix(s, ".key") {
			return true
		}
	}
	return false
}
func (a *Application) static(r *http.Request) (int, http.Header, []byte) {
	h := http.Header{"Content-Type": []string{"text/plain; charset=utf-8"}}
	fail := func() (int, http.Header, []byte) { return 404, h, []byte("404 page not found\n") }
	if r.Method != "GET" && r.Method != "HEAD" {
		h.Set("Allow", "GET, HEAD")
		return 405, h, nil
	}
	rel := strings.TrimPrefix(r.URL.Path, a.staticPrefix)
	if strings.ContainsAny(rel, "\x00\\:") || sensitive(rel) {
		return fail()
	}
	for _, s := range strings.Split(rel, "/") {
		if s == ".." {
			return fail()
		}
	}
	path, e := filepath.EvalSymlinks(filepath.Join(a.staticRoot, filepath.FromSlash(rel)))
	if e != nil || !isWithinDir(path, a.staticRoot) || sensitive(path) {
		return fail()
	}
	f, e := os.Open(path)
	if e != nil {
		return fail()
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() {
		return fail()
	}
	b, e := io.ReadAll(f)
	if e != nil {
		return fail()
	}
	sum := sha256.Sum256(b)
	tag := fmt.Sprintf(`"%x"`, sum)
	h.Set("ETag", tag)
	contentType := mime.TypeByExtension(filepath.Ext(path))
	if contentType == "" {
		contentType = http.DetectContentType(b)
	}
	h.Set("Content-Type", contentType)
	for _, t := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		if strings.TrimPrefix(strings.TrimSpace(t), "W/") == tag || strings.TrimSpace(t) == "*" {
			return 304, h, nil
		}
	}
	return 200, h, b
}

func (a *Application) isReady() bool {
	if a.draining.Load() {
		return false
	}
	p := a.active.Load()
	if p == nil || !p.acquire() {
		return false
	}
	defer p.release()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return p.code.Ready(ctx)
}
