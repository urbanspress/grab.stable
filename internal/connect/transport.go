package connect

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"grab/internal/platform"
	rt "grab/internal/runtime"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Result struct {
	Value      rt.Value
	Status     int
	RetryAfter time.Duration
	Uncertain  bool
}
type Manager struct {
	Packages    map[string]*Package
	Grants      *platform.Grants
	DialContext func(context.Context, string, string) (net.Conn, error)
}

func literalLoopback(host string) bool { ip := net.ParseIP(host); return ip != nil && ip.IsLoopback() }
func blocked(ip net.IP, b platform.Connector) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if v := ip.To4(); v != nil && (v[0] == 0 || v[0] >= 224) {
		return true
	}
	if ip.IsLoopback() {
		return !b.Development
	}
	if ip.IsPrivate() {
		for _, s := range b.AllowedCIDRs {
			_, n, e := net.ParseCIDR(s)
			if e == nil && n.Contains(ip) {
				return false
			}
		}
		return true
	}
	return false
}
func (m *Manager) Validate(alias, operation string, input rt.Value) error {
	if m == nil || m.Grants.Connector(alias, operation) != nil {
		return platform.ErrDenied
	}
	p := m.Packages[alias]
	if p == nil {
		return ErrInvalid
	}
	o, ok := p.Manifest.Operations[operation]
	if !ok {
		return ErrInvalid
	}
	_, e := p.Contracts.Encode(o.Input, input)
	return e
}
func (m *Manager) Call(ctx context.Context, alias, operation string, input rt.Value, key string) (Result, error) {
	if e := m.Validate(alias, operation, input); e != nil {
		return Result{}, e
	}
	p := m.Packages[alias]
	o := p.Manifest.Operations[operation]
	timeout, cancel := context.WithTimeout(ctx, time.Duration(o.TimeoutMS)*time.Millisecond)
	defer cancel()
	raw, e := p.Contracts.Encode(o.Input, input)
	if e != nil {
		return Result{}, ErrOperation
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return Result{}, ErrOperation
	}
	scalar := func(name string) (string, error) {
		v, ok := input.LookupEntry(name)
		if !ok {
			return "", ErrOperation
		}
		switch v.Kind() {
		case rt.TextKind:
			return v.TextValue(), nil
		case rt.IntegerKind, rt.DecimalKind, rt.BooleanKind, rt.FloatKind:
			return v.Display(), nil
		}
		return "", ErrOperation
	}
	path := o.Path
	for n, f := range o.PathFields {
		v, e := scalar(f)
		if e != nil || v == "." || v == ".." {
			return Result{}, ErrOperation
		}
		path = strings.ReplaceAll(path, "{"+n+"}", url.PathEscape(v))
	}
	parsed, e := url.Parse(p.Manifest.Origin + path)
	if e != nil {
		return Result{}, ErrOperation
	}
	q := url.Values{}
	for n, f := range o.QueryFields {
		v, e := scalar(f)
		if e != nil {
			return Result{}, e
		}
		q.Set(n, v)
	}
	parsed.RawQuery = q.Encode()
	var body []byte
	if o.BodyFields != nil {
		mapped := map[string]json.RawMessage{}
		for k, v := range o.BodyFields {
			if v.From != "" {
				b, ok := fields[v.From]
				if !ok {
					return Result{}, ErrOperation
				}
				mapped[k] = b
			} else {
				mapped[k] = v.Literal
			}
		}
		body, e = json.Marshal(mapped)
		if e != nil {
			return Result{}, ErrOperation
		}
	} else if o.Method != "GET" && o.Method != "HEAD" {
		body = raw
	}
	if len(body) > o.MaxBody {
		return Result{}, ErrOperation
	}
	req, e := http.NewRequestWithContext(timeout, o.Method, parsed.String(), bytes.NewReader(body))
	if e != nil {
		return Result{}, ErrOperation
	}
	req.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range o.Headers {
		req.Header.Set(k, v)
	}
	if o.Classification == "idempotent" {
		if key == "" || len(key) > 256 || strings.ContainsAny(key, "\r\n\x00") {
			return Result{}, ErrOperation
		}
		req.Header.Set(o.IdempotencyHeader, key)
	}
	if o.Auth != nil {
		a := o.Auth
		secret, e := platform.Secret(p.Binding.Secrets[a.Secret])
		if e != nil {
			return Result{}, ErrOperation
		}
		switch a.Type {
		case "api-key":
			req.Header.Set(a.Header, secret)
		case "bearer":
			req.Header.Set("Authorization", "Bearer "+secret)
		case "basic":
			user, e := platform.Secret(p.Binding.Secrets[a.Username])
			if e != nil {
				return Result{}, ErrOperation
			}
			req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+secret)))
		}
	}
	target := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = "443"
		if parsed.Scheme == "http" {
			port = "80"
		}
	}
	tr := &http.Transport{DisableKeepAlives: true, DisableCompression: true, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: target}, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		if network != "tcp" || addr != net.JoinHostPort(target, port) {
			return nil, platform.ErrDenied
		}
		ips := []net.IP{net.ParseIP(target)}
		if ips[0] == nil {
			resolved, e := net.DefaultResolver.LookupIPAddr(ctx, target)
			if e != nil {
				return nil, ErrOperation
			}
			ips = nil
			for _, ip := range resolved {
				ips = append(ips, ip.IP)
			}
		}
		if len(ips) == 0 {
			return nil, ErrOperation
		}
		for _, ip := range ips {
			if blocked(ip, p.Binding) {
				return nil, platform.ErrDenied
			}
		}
		address := net.JoinHostPort(ips[0].String(), port)
		if m.DialContext != nil {
			return m.DialContext(ctx, "tcp", address)
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", address)
	}}
	defer tr.CloseIdleConnections()
	client := http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect denied") }}
	// DisableKeepAlives and a single Do prevent replay of a request body by the
	// transport. All durable retry decisions belong to the outbox.
	response, e := client.Do(req)
	if e != nil {
		return Result{Uncertain: o.Classification == "unsafe"}, ErrUncertain
	}
	defer response.Body.Close()
	r := Result{Status: response.StatusCode}
	if v := response.Header.Get("Retry-After"); v != "" {
		if seconds, e := strconv.ParseInt(v, 10, 32); e == nil && seconds >= 0 {
			r.RetryAfter = time.Duration(seconds) * time.Second
		} else if at, e := http.ParseTime(v); e == nil {
			r.RetryAfter = time.Until(at)
			if r.RetryAfter < 0 {
				r.RetryAfter = 0
			}
		}
	}
	b, e := io.ReadAll(io.LimitReader(response.Body, int64(o.MaxBody)+1))
	if e != nil || len(b) > o.MaxBody || response.Header.Get("Content-Encoding") != "" {
		return r, ErrOperation
	}
	if r.Status < 200 || r.Status > 299 {
		return r, ErrOperation
	}
	if len(o.ResponseFields) > 0 {
		var source map[string]json.RawMessage
		if platform.StrictValueJSON(b, &source) != nil {
			return r, ErrOperation
		}
		mapped := map[string]json.RawMessage{}
		for name, field := range o.ResponseFields {
			value, ok := source[field]
			if !ok {
				return r, ErrOperation
			}
			mapped[name] = value
		}
		b, _ = json.Marshal(mapped)
	}
	r.Value, e = p.Contracts.Decode(o.Output, b)
	if e != nil {
		return r, ErrOperation
	}
	return r, nil
}
