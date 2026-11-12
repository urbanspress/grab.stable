package cache

import (
	"context"
	"math"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"grab/internal/platform"
	rt "grab/internal/runtime"
)

type Result struct {
	Found bool
	Value rt.Value
	TTLMS int64
}
type LimitResult struct {
	Allowed                        bool
	Count, Remaining, RetryAfterMS int64
}
type entry struct {
	raw                    []byte
	expires                time.Time
	count, maximum, window int64
	size                   int
}
type store struct {
	mu      sync.Mutex
	def     platform.Cache
	entries map[string]entry
	bytes   int
	now     func() time.Time
	remote  *redisStore
}

// Manager and handles are generation-owned. Closing a generation invalidates
// every handle, including handles retained by an embedding host.
type Manager struct {
	mu     sync.RWMutex
	grants *platform.Grants
	stores map[string]*store
	closed bool
	now    func() time.Time
}
type Handle struct {
	owner *Manager
	alias string
	store *store
}

func NewManager(g *platform.Grants, now func() time.Time) *Manager {
	if now == nil {
		now = time.Now
	}
	return &Manager{grants: g, stores: map[string]*store{}, now: now}
}
func (m *Manager) Open(ctx context.Context, alias string) (*Handle, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	d, e := m.grants.Cache(alias, "open")
	if e != nil {
		return nil, e
	}
	s := m.stores[alias]
	if s == nil {
		s = &store{def: d, entries: map[string]entry{}, now: m.now}
		if d.Driver == "redis" {
			s.remote, e = newRedis(ctx, d, alias)
			if e != nil {
				return nil, e
			}
		}
		m.stores[alias] = s
	}
	return &Handle{m, alias, s}, nil
}
func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	for _, s := range m.stores {
		if s.remote != nil {
			_ = s.remote.close()
		}
		s.entries = nil
		s.bytes = 0
	}
	m.stores = nil
	return nil
}
func validKey(k string) bool {
	if len(k) < 1 || len(k) > 256 || !utf8.ValidString(k) {
		return false
	}
	return !strings.ContainsFunc(k, unicode.IsControl)
}

// begin holds generation ownership until the operation finishes. Deadline and
// cancellation are applied by both adapters, including waits for capacity.
func (m *Manager) begin(ctx context.Context, h *Handle, key, mode string) (context.Context, func(), error) {
	m.mu.RLock()
	if m.closed {
		m.mu.RUnlock()
		return nil, nil, ErrClosed
	}
	if h == nil || h.owner != m || h.store == nil {
		m.mu.RUnlock()
		return nil, nil, ErrValue
	}
	if _, e := m.grants.Cache(h.alias, mode); e != nil {
		m.mu.RUnlock()
		return nil, nil, e
	}
	if !validKey(key) {
		m.mu.RUnlock()
		return nil, nil, ErrValue
	}
	op, cancel := context.WithTimeout(ctx, time.Duration(h.store.def.TimeoutMS)*time.Millisecond)
	if e := op.Err(); e != nil {
		cancel()
		m.mu.RUnlock()
		return nil, nil, e
	}
	return op, func() { cancel(); m.mu.RUnlock() }, nil
}
func (s *store) reclaim(now time.Time) {
	for k, e := range s.entries {
		if !now.Before(e.expires) {
			delete(s.entries, k)
			s.bytes -= e.size
		}
	}
}
func (s *store) put(k string, e entry, now time.Time) error {
	s.reclaim(now)
	old := s.entries[k]
	if (old.size == 0 && len(s.entries) >= s.def.MaxKeys) || s.bytes-old.size+e.size > s.def.MaxBytes {
		return ErrFull
	}
	s.entries[k] = e
	s.bytes += e.size - old.size
	return nil
}
func (m *Manager) Get(ctx context.Context, h *Handle, key string) (Result, error) {
	op, end, e := m.begin(ctx, h, key, "read")
	if e != nil {
		return Result{}, e
	}
	defer end()
	s := h.store
	if s.remote != nil {
		return s.remote.get(op, key)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e = op.Err(); e != nil {
		return Result{}, e
	}
	now := s.now()
	s.reclaim(now)
	v, ok := s.entries["value:"+key]
	if !ok {
		return Result{Value: rt.Nothing()}, nil
	}
	x, e := Decode(v.raw)
	if e != nil {
		return Result{}, ErrValue
	}
	ttl := v.expires.Sub(now).Milliseconds()
	if ttl < 1 {
		ttl = 1
	}
	return Result{true, x, ttl}, nil
}
func (m *Manager) Set(ctx context.Context, h *Handle, key string, v rt.Value, ttl int64) error {
	op, end, e := m.begin(ctx, h, key, "read-write")
	if e != nil {
		return e
	}
	defer end()
	if ttl < 1 || ttl > MaxTTL {
		return ErrValue
	}
	raw, e := Encode(v)
	if e != nil {
		return e
	}
	s := h.store
	if s.remote != nil {
		return s.remote.set(op, key, raw, ttl)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e = op.Err(); e != nil {
		return e
	}
	now := s.now()
	return s.put("value:"+key, entry{raw: raw, expires: now.Add(time.Duration(ttl) * time.Millisecond), size: len(key) + len(raw) + 128}, now)
}
func (m *Manager) Delete(ctx context.Context, h *Handle, key string) (bool, error) {
	op, end, e := m.begin(ctx, h, key, "read-write")
	if e != nil {
		return false, e
	}
	defer end()
	s := h.store
	if s.remote != nil {
		return s.remote.delete(op, key)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e = op.Err(); e != nil {
		return false, e
	}
	s.reclaim(s.now())
	k := "value:" + key
	old, ok := s.entries[k]
	delete(s.entries, k)
	s.bytes -= old.size
	return ok, nil
}
func (m *Manager) Limit(ctx context.Context, h *Handle, key string, maximum, window int64) (LimitResult, error) {
	op, end, e := m.begin(ctx, h, key, "limit")
	if e != nil {
		return LimitResult{}, e
	}
	defer end()
	if maximum < 1 || maximum > 1000000 || window < 1 || window > 86400000 {
		return LimitResult{}, ErrValue
	}
	s := h.store
	if s.remote != nil {
		return s.remote.limit(op, key, maximum, window)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e = op.Err(); e != nil {
		return LimitResult{}, e
	}
	now := s.now()
	s.reclaim(now)
	k := "limit:" + key
	v, ok := s.entries[k]
	if ok && (v.maximum != maximum || v.window != window) {
		return LimitResult{}, ErrValue
	}
	if !ok {
		v = entry{expires: now.Add(time.Duration(window) * time.Millisecond), maximum: maximum, window: window, size: len(key) + 128}
	}
	if v.count < math.MaxInt64 {
		v.count++
	}
	if e = s.put(k, v, now); e != nil {
		return LimitResult{}, e
	}
	return limitResult(v.count, maximum, v.expires.Sub(now).Milliseconds()), nil
}
func limitResult(count, maximum, ttl int64) LimitResult {
	r := LimitResult{Allowed: count <= maximum, Count: count}
	if r.Allowed {
		r.Remaining = maximum - count
	} else {
		if ttl < 1 {
			ttl = 1
		}
		r.RetryAfterMS = ttl
	}
	return r
}
