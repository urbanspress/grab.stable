package interpreter

import (
	"context"
	"errors"
	"grab/internal/ast"
	"grab/internal/connect"
	"grab/internal/events"
	"grab/internal/lexer"
	"grab/internal/platform"
	rt "grab/internal/runtime"
	"strconv"
)

var connectFunctions = map[string]stdlibFunc{"call": {arity: 3, fn: stdConnectCall}}
var eventFunctions = map[string]stdlibFunc{"enqueue": {arity: 6, fn: stdEventsEnqueue}, "status": {arity: 2, fn: stdEventsStatus}, "complete": {arity: 2, fn: stdEventsComplete}}

type integrationRuntime struct {
	connect *connect.Manager
	config  *platform.Config
	grants  *platform.Grants
}

func integrationError(s lexer.Span, code string, e error) error {
	if e == nil {
		return nil
	}
	if errors.Is(e, platform.ErrDenied) {
		code = "GRAB-E403"
	}
	return &Error{Kind: StandardLibraryError, CustomCode: code, Message: e.Error(), Span: s, Err: e}
}
func literalArg(a ast.Expression) (string, bool) {
	l, ok := a.(*ast.LiteralExpr)
	if !ok || l.Kind != ast.TextLiteral {
		return "", false
	}
	v, e := strconv.Unquote(l.Lexeme)
	return v, e == nil && platform.Identifier(v)
}
func ValidateIntegrations(p *ast.Program, cfg *platform.Config) (map[string]*connect.Package, error) {
	packages, e := connect.Load(cfg)
	if e != nil {
		return nil, e
	}
	e = walkCalls(p, func(module, name string, args []ast.Expression, s lexer.Span) error {
		switch module {
		case "connect":
			if len(args) < 2 {
				return connect.ErrInvalid
			}
			alias, ok := literalArg(args[0])
			operation, opOK := literalArg(args[1])
			if !ok || !opOK || packages[alias] == nil {
				return connect.ErrInvalid
			}
			if _, ok := packages[alias].Manifest.Operations[operation]; !ok {
				return connect.ErrInvalid
			}
		case "events":
			indices := []int{}
			if name == "enqueue" {
				indices = []int{1, 2, 3}
			} else if name == "status" {
				indices = []int{0}
			}
			for _, i := range indices {
				if len(args) <= i {
					return events.ErrStore
				}
				alias, ok := literalArg(args[i])
				if !ok {
					return events.ErrStore
				}
				if name == "status" || i == 1 {
					if !cfg.HasEvents(alias) {
						return events.ErrStore
					}
				} else if i == 2 && !cfg.HasConnector(alias) {
					return events.ErrStore
				}
			}
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	return packages, nil
}
func prepareIntegrations(p *ast.Program, opts *Options) error {
	packages, e := ValidateIntegrations(p, opts.PlatformConfig)
	if e != nil {
		return e
	}
	if e = opts.PlatformGrants.ValidateConnectorOperations(connect.IDs(packages)); e != nil {
		return e
	}
	opts.Integrations = &integrationRuntime{connect: &connect.Manager{Packages: packages, Grants: opts.PlatformGrants, DialContext: opts.DialContext}, config: opts.PlatformConfig, grants: opts.PlatformGrants}
	return nil
}
func stdConnectCall(c *execContext, s lexer.Span, a []rt.Value) (rt.Value, error) {
	if c.eventWork != nil {
		return rt.Value{}, integrationError(s, "GRAB-E316", platform.ErrDenied)
	}
	if c.integrations == nil {
		return rt.Value{}, integrationError(s, "GRAB-E316", platform.ErrDenied)
	}
	r, e := c.integrations.connect.Call(c.context(), a[0].TextValue(), a[1].TextValue(), a[2], "")
	return r.Value, integrationError(s, "GRAB-E316", e)
}
func (c *execContext) eventStore(ctx context.Context, alias, mode string) (*events.Store, error) {
	if c.integrations == nil || c.integrations.grants.Events(alias, mode) != nil {
		return nil, platform.ErrDenied
	}
	def, ok := c.integrations.config.Events()[alias]
	if !ok {
		return nil, events.ErrStore
	}
	p, e := c.data.Open(ctx, def.Database)
	if e != nil {
		return nil, e
	}
	db, e := c.data.HostDatabase(p)
	if e != nil {
		return nil, e
	}
	return &events.Store{DB: db, Driver: p.Driver(), Config: def}, nil
}
func stdEventsEnqueue(c *execContext, s lexer.Span, a []rt.Value) (rt.Value, error) {
	store, e := c.eventStore(c.context(), a[1].TextValue(), "work")
	if e != nil {
		return rt.Value{}, integrationError(s, "GRAB-E317", e)
	}
	alias, operation := a[2].TextValue(), a[3].TextValue()
	if e = c.integrations.connect.Validate(alias, operation, a[4]); e != nil {
		return rt.Value{}, integrationError(s, "GRAB-E317", e)
	}
	p, e := c.data.Open(c.context(), store.Config.Database)
	if e != nil {
		return rt.Value{}, integrationError(s, "GRAB-E317", e)
	}
	t, ok := transactionArg(a[0])
	if !ok {
		return rt.Value{}, integrationError(s, "GRAB-E317", events.ErrStore)
	}
	tx, e := c.data.HostTransaction(t, p)
	if e != nil {
		return rt.Value{}, integrationError(s, "GRAB-E317", e)
	}
	pkg := c.integrations.connect.Packages[alias]
	op := pkg.Manifest.Operations[operation]
	payload, e := pkg.Contracts.Encode(op.Input, a[4])
	if e != nil {
		return rt.Value{}, integrationError(s, "GRAB-E317", e)
	}
	if a[5].Kind() != rt.TextKind || a[5].TextValue() == "" {
		return rt.Value{}, integrationError(s, "GRAB-E317", events.ErrStore)
	}
	id, e := store.Enqueue(c.context(), tx, events.Work{Kind: "outbox", Connector: alias, Operation: operation, Payload: payload, Key: a[5].TextValue(), Classification: op.Classification})
	if e != nil {
		c.data.PoisonTransaction(t)
	}
	return rt.Text(id), integrationError(s, "GRAB-E317", e)
}
func stdEventsStatus(c *execContext, s lexer.Span, a []rt.Value) (rt.Value, error) {
	store, e := c.eventStore(c.context(), a[0].TextValue(), "work")
	if e != nil {
		return rt.Value{}, integrationError(s, "GRAB-E317", e)
	}
	v, e := store.Inspect(c.context(), a[1].TextValue())
	return rt.Record([]rt.Entry{{Name: "id", Value: rt.Text(v.ID)}, {Name: "state", Value: rt.Text(v.State)}, {Name: "attempts", Value: rt.IntegerFromInt64(int64(v.Attempts))}, {Name: "next_attempt_at", Value: rt.IntegerFromInt64(v.Next)}}), integrationError(s, "GRAB-E317", e)
}
func stdEventsComplete(c *execContext, s lexer.Span, a []rt.Value) (rt.Value, error) {
	t, ok := transactionArg(a[0])
	if !ok || c.eventWork == nil || t != c.data.ActiveTransaction() || !sameEvent(a[1], c.eventValue) || c.eventCompleted {
		return rt.Value{}, integrationError(s, "GRAB-E317", events.ErrFence)
	}
	c.eventCompleted = true
	return rt.Nothing(), nil
}

func sameEvent(a, b rt.Value) bool {
	if a.Kind() != rt.RecordKind || len(a.Entries()) != 6 {
		return false
	}
	for _, e := range b.Entries() {
		v, ok := a.LookupEntry(e.Name)
		if !ok {
			return false
		}
		if e.Name == "transaction" {
			if v.ResourceValue() != e.Value.ResourceValue() {
				return false
			}
		} else if !v.Equal(e.Value) {
			return false
		}
	}
	return true
}
