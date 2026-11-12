package interpreter

import (
	"context"
	"crypto/rand"

	"grab/internal/connect"
	"grab/internal/events"
	"grab/internal/lexer"
	"grab/internal/platform"
	rt "grab/internal/runtime"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

func (p *PreparedProgram) Webhook(path string) (string, bool) {
	for alias, w := range p.opts.PlatformConfig.Webhooks() {
		if w.Path == path {
			return alias, true
		}
	}
	return "", false
}
func (p *PreparedProgram) AcceptWebhook(ctx context.Context, alias string, h http.Header, raw []byte) (int, []byte) {
	w, ok := p.opts.PlatformConfig.Webhooks()[alias]
	if !ok {
		return 404, nil
	}
	c := newExecContext(ctx, io.Discard, p.functions, p.opts)
	defer c.data.Close()
	store, e := c.eventStore(ctx, w.Store, "accept")
	if e != nil {
		return 503, []byte("webhook unavailable")
	}
	pkg := c.integrations.connect.Packages[w.Connector]
	if pkg == nil || pkg.Manifest.Verifier != "grab-hmac-sha256-v1" {
		return 503, []byte("webhook unavailable")
	}
	keys := p.webhookKeys[alias]
	id, e := connect.Verify(h, raw, keys, time.Now())
	if e != nil {
		return 401, []byte("webhook verification failed")
	}
	schema, ok := pkg.Manifest.Types[w.Type]
	if !ok {
		return 503, []byte("webhook unavailable")
	}
	v, e := pkg.Contracts.Decode(schema, raw)
	if e != nil {
		return 400, []byte("invalid webhook payload")
	}
	payload, e := pkg.Contracts.Encode(schema, v)
	if e != nil {
		return 400, []byte("invalid webhook payload")
	}
	// Preserve raw-byte digest for identity mismatch detection even when canonical
	// payloads are equivalent. The store receives both separately.
	_, duplicate, e := store.AcceptRaw(ctx, events.Work{Kind: "inbox", Connector: w.Connector, Tenant: w.Tenant, EventID: id, Type: w.Type, Payload: payload}, raw)
	switch e {
	case nil:
		if duplicate {
			return 200, []byte("accepted")
		}
		return 202, []byte("accepted")
	case events.ErrConflict:
		return 409, []byte("event conflict")
	default:
		return 503, []byte("webhook storage unavailable")
	}
}
func (p *PreparedProgram) prepareEvents(ctx context.Context) error {
	p.webhookKeys = map[string][][]byte{}
	c := newExecContext(ctx, io.Discard, p.functions, p.opts)
	defer c.data.Close()
	for alias, w := range p.opts.PlatformConfig.Webhooks() {
		for _, r := range p.routes {
			if _, ok := r.Match(w.Path); ok {
				return events.ErrStore
			}
		}
		fn := p.functions[w.Handler]
		if fn == nil || len(fn.Parameters) != 1 {
			return events.ErrStore
		}
		pkg := p.opts.Integrations.connect.Packages[w.Connector]
		if pkg == nil || pkg.Manifest.Verifier != "grab-hmac-sha256-v1" {
			return events.ErrStore
		}
		if _, ok := pkg.Manifest.Types[w.Type]; !ok {
			return events.ErrStore
		}
		s, e := c.eventStore(ctx, w.Store, "accept")
		if e != nil {
			return e
		}
		if e = s.Ready(ctx); e != nil {
			return e
		}
		for _, name := range w.Keys {
			secret, e := platform.Secret(name)
			if e != nil || len(secret) < 16 {
				return events.ErrStore
			}
			p.webhookKeys[alias] = append(p.webhookKeys[alias], []byte(secret))
		}
	}
	for alias := range p.opts.PlatformConfig.Events() {
		if p.opts.PlatformGrants.Events(alias, "work") == nil {
			if p.opts.PlatformConfig.Events()[alias].Concurrency > p.opts.DataGrants.PoolLimit(p.opts.PlatformConfig.Events()[alias].Database) {
				return platform.ErrInvalid
			}
			s, e := c.eventStore(ctx, alias, "work")
			if e != nil {
				return e
			}
			if e = s.Ready(ctx); e != nil {
				return e
			}
		}
	}
	return nil
}

// StartWorkers is called only after the complete generation has validated.
func (p *PreparedProgram) StartWorkers() {
	ctx, cancel := context.WithCancel(context.Background())
	p.workerCancel = cancel
	enabled := false
	for alias := range p.opts.PlatformConfig.Telemetry() {
		if p.opts.PlatformGrants.Telemetry(alias) == nil {
			enabled = true
		}
	}
	if enabled {
		p.workerWG.Add(1)
		go func() { defer p.workerWG.Done(); p.observeEvents(ctx) }()
	}
	for alias, def := range p.opts.PlatformConfig.Events() {
		if p.opts.PlatformGrants.Events(alias, "work") != nil {
			continue
		}
		for i := 0; i < def.Concurrency; i++ {
			p.workerWG.Add(1)
			go func(alias string) { defer p.workerWG.Done(); defer func() { _ = recover() }(); p.workLoop(ctx, alias) }(alias)
		}
	}
}
func (p *PreparedProgram) workLoop(ctx context.Context, alias string) {
	owner := events.ID()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		c := newExecContext(ctx, io.Discard, p.functions, p.opts)
		s, e := c.eventStore(ctx, alias, "work")
		if e != nil {
			c.data.Close()
			continue
		}
		w, e := s.Claim(ctx, owner)
		c.data.Close()
		if e != nil || w.ID == "" {
			continue
		}
		func() { defer func() { _ = recover() }(); p.processWork(ctx, alias, s, w) }()
	}
}
func backoff(attempt int) time.Duration {
	if attempt > 11 {
		attempt = 11
	}
	max := int64(time.Second) << uint(attempt-1)
	if max > int64(15*time.Minute) {
		max = int64(15 * time.Minute)
	}
	n, e := rand.Int(rand.Reader, big.NewInt(max))
	if e != nil {
		return time.Second
	}
	return time.Duration(n.Int64())
}

func (p *PreparedProgram) processInbox(ctx context.Context, alias string, s *events.Store, w events.Work) bool {
	c := newExecContext(ctx, io.Discard, p.functions, p.opts)
	defer c.data.Close()
	pool, e := c.data.Open(ctx, s.Config.Database)
	if e != nil {
		return false
	}
	t, e := c.data.Begin(ctx, pool, false, "serializable")
	if e != nil {
		return false
	}
	if e = c.data.HostOwnTransaction(t); e != nil {
		return false
	}
	tx, e := c.data.HostTransaction(t, pool)
	if e != nil {
		return false
	}
	if e = s.Fence(ctx, tx, w); e != nil {
		return false
	}
	handler := ""
	for _, hook := range p.opts.PlatformConfig.Webhooks() {
		if hook.Store == alias && hook.Connector == w.Connector && hook.Tenant == w.Tenant && hook.Type == w.Type {
			handler = hook.Handler
			break
		}
	}
	pkg := p.opts.Integrations.connect.Packages[w.Connector]
	fn := p.functions[handler]
	if pkg == nil || fn == nil {
		return false
	}
	payload, e := pkg.Contracts.Decode(pkg.Manifest.Types[w.Type], w.Payload)
	if e != nil {
		return false
	}
	event := rt.Record([]rt.Entry{{Name: "id", Value: rt.Text(w.ID)}, {Name: "tenant", Value: rt.Text(w.Tenant)}, {Name: "type", Value: rt.Text(w.Type)}, {Name: "payload", Value: payload}, {Name: "attempt", Value: rt.IntegerFromInt64(int64(w.Attempts))}, {Name: "transaction", Value: rt.Resource(t)}})
	c.eventDatabase = s.Config.Database
	c.eventWork = &w
	c.eventValue = event
	env := newScope(nil)
	env.defineLocal(fn.Parameters[0].Name, event)
	v, _, e := c.execBlock(fn.Body, env)
	if e != nil || !c.eventCompleted || v.ContainsResource() {

		return false
	}
	if e = s.Complete(ctx, tx, w); e != nil {

		return false
	}
	return c.data.Commit(t) == nil
}
func (p *PreparedProgram) processWork(parent context.Context, alias string, s *events.Store, w events.Work) {
	start := time.Now()
	defer func() { p.opts.Metrics.Work(w.Kind, s.Config.Namespace+":"+w.ID, w.Attempts, start) }()
	p.opts.Metrics.Add("event_attempts", 1)
	if w.Recovered {
		p.opts.Metrics.Add("lease_recovery", 1)
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	if w.Kind == "inbox" {
		if p.processInbox(ctx, alias, s, w) {
			return
		}
		cleanup, c := context.WithTimeout(context.Background(), time.Second)
		defer c()
		_ = s.Fail(cleanup, w, true, false, backoff(w.Attempts))
		return
	}
	done := make(chan struct{})
	var renewal sync.WaitGroup
	renewal.Add(1)
	go func() {
		defer renewal.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if s.Renew(ctx, w) != nil {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { close(done); cancel(); renewal.Wait() }()
	pkg := p.opts.Integrations.connect.Packages[w.Connector]
	retry, uncertain := false, false
	delay := backoff(w.Attempts)
	if pkg != nil {
		if op, ok := pkg.Manifest.Operations[w.Operation]; ok {
			if v, e := pkg.Contracts.Decode(op.Input, w.Payload); e == nil {
				r, e := p.opts.Integrations.connect.Call(ctx, w.Connector, w.Operation, v, w.Key)
				uncertain = r.Uncertain && op.Classification == "unsafe"
				retry = op.Classification != "unsafe" && (r.Status == 0 || r.Status == 408 || r.Status == 429)
				for _, status := range op.TransientStatuses {
					retry = retry || op.Classification != "unsafe" && r.Status == status
				}
				if r.RetryAfter > delay {
					delay = r.RetryAfter
				}
				if e == nil {
					tx, err := s.DB.BeginTx(ctx, nil)
					if err == nil {
						err = s.Complete(ctx, tx, w)
						if err == nil {
							err = tx.Commit()
						}
						_ = tx.Rollback()
						if err == nil {
							return
						}
					}
					retry = op.Classification != "unsafe"
					uncertain = op.Classification == "unsafe"
				}
			}
		}
	}
	cleanup, c := context.WithTimeout(context.Background(), time.Second)
	defer c()
	_ = s.Fail(cleanup, w, retry, uncertain, delay)
}

// Keep the worker's refusal source-located like other module errors.
func workerDenied(s lexer.Span) error { return integrationError(s, "GRAB-E317", events.ErrFence) }

func (p *PreparedProgram) StopWorkers() {
	if p.workerCancel != nil {
		p.workerCancel()
	}
}

func (p *PreparedProgram) observeEvents(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		op, cancel := context.WithTimeout(ctx, time.Second)
		c := newExecContext(op, io.Discard, p.functions, p.opts)
		var sum events.Metrics
		for alias := range p.opts.PlatformConfig.Events() {
			mode := "work"
			if p.opts.PlatformGrants.Events(alias, mode) != nil {
				mode = "accept"
			}
			store, e := c.eventStore(op, alias, mode)
			if e != nil {
				continue
			}
			m, e := store.Metrics(op)
			if e != nil {
				continue
			}
			sum.Pending += m.Pending
			sum.Dead += m.Dead
			if m.InboxLagMS > sum.InboxLagMS {
				sum.InboxLagMS = m.InboxLagMS
			}
			if m.OutboxLagMS > sum.OutboxLagMS {
				sum.OutboxLagMS = m.OutboxLagMS
			}
		}
		c.data.Close()
		cancel()
		p.opts.Metrics.Set("event_pending", sum.Pending)
		p.opts.Metrics.Set("event_dead", sum.Dead)
		p.opts.Metrics.Set("inbox_lag_milliseconds", sum.InboxLagMS)
		p.opts.Metrics.Set("outbox_lag_milliseconds", sum.OutboxLagMS)
	}
}
