package interpreter

import (
	"context"
	"io"
	"net"
	"net/http"
	"time"

	"grab/internal/ast"
	"grab/internal/contracts"
	"grab/internal/lexer"
	"grab/internal/platform"
	"grab/internal/realtime"
	rt "grab/internal/runtime"
)

func init() {
	stdlibModules["sockets"] = map[string]stdlibFunc{"publish": {arity: 3, fn: stdSocketsPublish}}
}

type socketRuntime struct {
	program  *PreparedProgram
	channels map[string]*realtime.Channel
}
type socketIdentity struct {
	channel, audience string
	principal         rt.Value
}

// ValidateSockets performs callback, contract, collision and literal checks offline.
func ValidateSockets(program *ast.Program, cfg *platform.Config, d *contracts.Document) error {
	functions, _, routes, e := validateProgram(program)
	if e != nil {
		return e
	}
	for _, v := range cfg.Channels() {
		for _, name := range []string{v.Authorize, v.Receive, v.PublishAuthorize} {
			if name == "" {
				continue
			}
			f := functions[name]
			if f == nil || len(f.Parameters) != 1 {
				return platform.ErrInvalid
			}
		}
		if d == nil {
			return platform.ErrInvalid
		}
		if _, ok := d.Types[v.ClientType]; !ok {
			return platform.ErrInvalid
		}
		if _, ok := d.Types[v.ServerType]; !ok {
			return platform.ErrInvalid
		}
		for _, r := range routes {
			if _, ok := r.Match(v.Path); ok {
				return platform.ErrInvalid
			}
		}
	}
	return walkCalls(program, func(module, name string, args []ast.Expression, s lexer.Span) error {
		if module != "sockets" {
			return nil
		}
		if len(args) != 3 {
			return platform.ErrInvalid
		}
		alias, ok := literalArg(args[0])
		if !ok || !cfg.HasChannel(alias) {
			return &Error{Kind: StandardLibraryError, CustomCode: "GRAB-E210", Message: "channel must be a configured direct text literal", Span: s}
		}
		return nil
	})
}
func (p *PreparedProgram) prepareSockets(ctx context.Context) error {
	if e := ValidateSockets(p.program, p.opts.PlatformConfig, p.opts.APIContract); e != nil {
		return e
	}
	p.opts.Sockets = &socketRuntime{program: p, channels: map[string]*realtime.Channel{}}
	for alias, d := range p.opts.PlatformConfig.Channels() {
		if p.opts.PlatformGrants.Channel(alias, "connect") != nil && p.opts.PlatformGrants.Channel(alias, "publish") != nil {
			continue
		}
		op, cancel := context.WithTimeout(ctx, time.Duration(d.Broker.TimeoutMS)*time.Millisecond)
		b, e := realtime.NewBroker(op, alias, d.Broker)
		cancel()
		if e != nil {
			return e
		}
		p.opts.Sockets.channels[alias] = &realtime.Channel{Metrics: p.opts.Metrics, Config: d, Contracts: p.opts.APIContract, Broker: b}
	}
	return nil
}
func (p *PreparedProgram) Channel(path string) (string, bool) {
	for alias, d := range p.opts.PlatformConfig.Channels() {
		if d.Path == path {
			return alias, true
		}
	}
	return "", false
}
func (p *PreparedProgram) socketCall(ctx context.Context, name string, arg rt.Value, id *socketIdentity) (rt.Value, error) {
	f := p.functions[name]
	if f == nil || len(f.Parameters) != 1 {
		return rt.Value{}, realtime.ErrPolicy
	}
	c := newExecContext(ctx, io.Discard, p.functions, p.opts)
	c.vmProg = p.vm
	defer c.data.Close()
	c.socketIdentity = id
	c.socketCallback = id == nil
	env := newScope(nil)
	env.defineLocal(f.Parameters[0].Name, arg)
	v, returned, e := c.execBlock(f.Body, env)
	if e != nil || !returned {
		return rt.Value{}, realtime.ErrPolicy
	}
	return v, nil
}
func (p *PreparedProgram) ServeChannel(w http.ResponseWriter, r *http.Request, alias, scheme string, request rt.Value, authorized func()) {
	d := p.opts.PlatformConfig.Channels()[alias]
	ch := p.opts.Sockets.channels[alias]
	if ch == nil || p.opts.PlatformGrants.Channel(alias, "connect") != nil {
		http.Error(w, "channel unavailable", 503)
		return
	}
	peer, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(peer)
	if !ch.HandshakeAllowed(r) {
		http.Error(w, "channel policy rejected", 403)
		return
	}
	if scheme != "https" && !(d.Development && ip != nil && ip.IsLoopback()) {
		http.Error(w, "secure channel required", 403)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(d.HandlerTimeoutMS)*time.Millisecond)
	v, e := p.socketCall(ctx, d.Authorize, request, nil)
	cancel()
	allowed, _ := v.LookupEntry("allowed")
	principal, pok := v.LookupEntry("principal")
	audiences, _ := v.LookupEntry("audiences")
	expiry, _ := v.LookupEntry("expires_at")
	expires, te := time.Parse(time.RFC3339Nano, expiry.TextValue())
	if e != nil || v.Kind() != rt.RecordKind || len(v.Entries()) != 4 || allowed.Kind() != rt.BooleanKind || !allowed.BooleanValue() || !pok || principal.Kind() == rt.NothingKind || principal.ContainsResource() || audiences.Kind() != rt.ListKind || expiry.Kind() != rt.TextKind || te != nil {
		http.Error(w, "channel authorization rejected", 403)
		return
	}
	var as []string
	for _, a := range audiences.ListValues() {
		if a.Kind() != rt.TextKind {
			http.Error(w, "channel authorization rejected", 403)
			return
		}
		as = append(as, a.TextValue())
	}
	// Release the HTTP admission slot after authorization; established channels
	// have their own independent connection and queue limits.
	authorized()
	ch.Serve(w, r, realtime.Authorization{Principal: principal, Audiences: as, Expires: expires}, func(ctx context.Context, principal rt.Value, audience string, value rt.Value) error {
		message := rt.Record([]rt.Entry{{Name: "principal", Value: principal}, {Name: "audience", Value: rt.Text(audience)}, {Name: "value", Value: value}})
		_, e := p.socketCall(ctx, d.Receive, message, &socketIdentity{alias, audience, principal})
		return e
	})
}
func stdSocketsPublish(c *execContext, s lexer.Span, a []rt.Value) (rt.Value, error) {
	denied := func(e error) (rt.Value, error) { return rt.Value{}, integrationError(s, "GRAB-E318", e) }
	if c.sockets == nil || c.socketCallback || c.eventWork != nil {
		return denied(platform.ErrDenied)
	}
	alias, audience := a[0].TextValue(), a[1].TextValue()
	if a[1].Kind() != rt.TextKind || !realtime.Audience(audience) {
		return denied(realtime.ErrPolicy)
	}
	p := c.sockets.program
	ch := c.sockets.channels[alias]
	if ch == nil || p.opts.PlatformGrants.Channel(alias, "publish") != nil {
		return denied(platform.ErrDenied)
	}
	if id := c.socketIdentity; id != nil {
		if id.channel != alias || id.audience != audience {
			return denied(platform.ErrDenied)
		}
	} else {
		if ch.Config.PublishAuthorize == "" || c.trustedContext.Kind() == rt.NothingKind {
			return denied(platform.ErrDenied)
		}
		arg := rt.Record([]rt.Entry{{Name: "context", Value: c.trustedContext}, {Name: "channel", Value: rt.Text(alias)}, {Name: "audience", Value: rt.Text(audience)}})
		op, cancel := context.WithTimeout(c.context(), time.Duration(ch.Config.HandlerTimeoutMS)*time.Millisecond)
		v, e := p.socketCall(op, ch.Config.PublishAuthorize, arg, nil)
		cancel()
		if e != nil || v.Kind() != rt.BooleanKind || !v.BooleanValue() {
			return denied(platform.ErrDenied)
		}
	}
	op, cancel := context.WithTimeout(c.context(), time.Duration(ch.Config.Broker.TimeoutMS)*time.Millisecond)
	defer cancel()
	accepted, e := ch.Publish(op, audience, a[2])
	if e != nil {
		return denied(e)
	}
	return rt.Record([]rt.Entry{{Name: "accepted", Value: rt.Boolean(accepted)}}), nil
}
func (p *PreparedProgram) StopSockets() {
	if p.opts.Sockets != nil {
		for _, c := range p.opts.Sockets.channels {
			c.Broker.Close()
		}
	}
}

// Ready propagates store/broker outages into HTTP and supervisor readiness.
func (p *PreparedProgram) Ready(ctx context.Context) bool {
	pools := p.opts.CacheManager.PoolUse() + p.opts.DataManager.PoolUse()
	if p.opts.Sockets != nil {
		for _, c := range p.opts.Sockets.channels {
			pools += c.Broker.PoolUse()
		}
	}
	p.opts.Metrics.Set("pool_used", pools)
	if !p.opts.CacheManager.Ready(ctx) || !p.opts.DataManager.Ready(ctx) {
		return false
	}
	if p.opts.Sockets != nil {
		for _, c := range p.opts.Sockets.channels {
			if !c.Broker.Ready() {
				return false
			}
		}
	}
	return true
}

func (p *PreparedProgram) PlatformConfiguration() *platform.Config { return p.opts.PlatformConfig }

func (p *PreparedProgram) PlatformCapabilities() *platform.Grants { return p.opts.PlatformGrants }
