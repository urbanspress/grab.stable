package telemetry

import (
	"context"
	"grab/internal/platform"
	"reflect"
	"sync/atomic"
)

// Listener topology changes need a stopped restart. Secret rotation is prepared
// for every active destination before any token is replaced.
func (m *Manager) PrepareReload(ctx context.Context, cfg *platform.Config, g *platform.Grants) (func(), error) {
	if m == nil {
		return func() {}, nil
	}
	defs := cfg.Telemetry()
	if !reflect.DeepEqual(defs, m.config) {
		return nil, platform.ErrInvalid
	}
	tokens := map[string]string{}
	for alias, d := range defs {
		_, active := m.tokens[alias]
		allowed := g.Telemetry(alias) == nil
		if active != allowed {
			return nil, platform.ErrDenied
		}
		if !active {
			continue
		}
		token, e := platform.Secret(d.TokenEnv)
		if e != nil || len(token) < 32 {
			return nil, platform.ErrInvalid
		}
		tokens[alias] = token
	}
	return func() {
		for alias, token := range tokens {
			copy := token
			m.tokens[alias].Store(&copy)
		}
	}, nil
}
func tokenValue(pointer *atomic.Pointer[string], fallback string) string {
	if pointer != nil {
		if v := pointer.Load(); v != nil {
			return *v
		}
	}
	return fallback
}
