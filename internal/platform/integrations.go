package platform

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
)

type Connector struct {
	Package      string            `json:"package"`
	Version      string            `json:"version"`
	Secrets      map[string]string `json:"secrets"`
	Development  bool              `json:"development,omitempty"`
	AllowedCIDRs []string          `json:"allowed_cidrs,omitempty"`
}
type EventStore struct {
	Database    string `json:"database"`
	Namespace   string `json:"namespace"`
	MaxPending  int    `json:"max_pending"`
	MaxBytes    int    `json:"max_bytes"`
	Concurrency int    `json:"concurrency"`
	MaxAttempts int    `json:"max_attempts"`
}
type Webhook struct {
	Path      string   `json:"path"`
	Connector string   `json:"connector"`
	Store     string   `json:"store"`
	Tenant    string   `json:"tenant"`
	Handler   string   `json:"handler"`
	Keys      []string `json:"keys"`
	Type      string   `json:"type"`
}

func (c *Config) parseIntegrations(connectors, events, webhooks map[string]json.RawMessage) error {
	c.connectors = map[string]Connector{}
	c.events = map[string]EventStore{}
	c.webhooks = map[string]Webhook{}
	for alias, b := range connectors {
		var v Connector
		if !Identifier(alias) || StrictJSON(b, &v) != nil || v.Package == "" || filepath.IsAbs(v.Package) || v.Version == "" || len(v.Version) > 64 {
			return ErrInvalid
		}
		clean := filepath.Clean(v.Package)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || strings.ContainsAny(v.Package, "\x00:") {
			return ErrInvalid
		}
		for k, n := range v.Secrets {
			if !Identifier(k) || !envPattern.MatchString(n) {
				return ErrInvalid
			}
		}
		for _, cidr := range v.AllowedCIDRs {
			if _, _, e := net.ParseCIDR(cidr); e != nil {
				return ErrInvalid
			}
		}
		c.connectors[alias] = v
	}
	spaces := map[string]bool{}
	for alias, b := range events {
		v := EventStore{MaxPending: 100000, MaxBytes: 256 << 20, Concurrency: 4, MaxAttempts: 8}
		if !Identifier(alias) || StrictJSON(b, &v) != nil || !Identifier(v.Database) || !Identifier(v.Namespace) || v.MaxPending < 1 || v.MaxPending > 1000000 || v.MaxBytes < 1 || v.MaxBytes > 1<<30 || v.Concurrency < 1 || v.Concurrency > 64 || v.MaxAttempts < 1 || v.MaxAttempts > 20 {
			return ErrInvalid
		}
		key := v.Database + ":" + v.Namespace
		if spaces[key] {
			return ErrInvalid
		}
		spaces[key] = true
		c.events[alias] = v
	}
	paths := map[string]bool{}
	for alias, b := range webhooks {
		var v Webhook
		if !Identifier(alias) || StrictJSON(b, &v) != nil || !strings.HasPrefix(v.Path, "/") || strings.ContainsAny(v.Path, "{}?#\\\x00\r\n ") || paths[v.Path] || !Identifier(v.Tenant) || !Identifier(v.Handler) || !Identifier(v.Type) || len(v.Keys) < 1 || len(v.Keys) > 2 {
			return ErrInvalid
		}
		if _, ok := c.connectors[v.Connector]; !ok {
			return ErrInvalid
		}
		if _, ok := c.events[v.Store]; !ok {
			return ErrInvalid
		}
		for _, n := range v.Keys {
			if !envPattern.MatchString(n) {
				return ErrInvalid
			}
		}
		for _, segment := range strings.Split(v.Path, "/") {
			if segment == "." || segment == ".." {
				return ErrInvalid
			}
		}
		for _, old := range c.webhooks {
			if old.Store == v.Store && old.Connector == v.Connector && old.Tenant == v.Tenant && old.Type == v.Type && old.Handler != v.Handler {
				return ErrInvalid
			}
		}
		paths[v.Path] = true
		c.webhooks[alias] = v
	}
	return nil
}
func (c *Config) Root() string {
	if c == nil {
		return ""
	}
	return c.root
}
func (c *Config) Connectors() map[string]Connector {
	out := map[string]Connector{}
	if c != nil {
		for k, v := range c.connectors {
			v.Secrets = clone(v.Secrets)
			v.AllowedCIDRs = append([]string(nil), v.AllowedCIDRs...)
			out[k] = v
		}
	}
	return out
}
func clone(m map[string]string) map[string]string {
	n := map[string]string{}
	for k, v := range m {
		n[k] = v
	}
	return n
}
func (c *Config) Events() map[string]EventStore {
	out := map[string]EventStore{}
	if c != nil {
		for k, v := range c.events {
			out[k] = v
		}
	}
	return out
}
func (c *Config) Webhooks() map[string]Webhook {
	out := map[string]Webhook{}
	if c != nil {
		for k, v := range c.webhooks {
			v.Keys = append([]string(nil), v.Keys...)
			out[k] = v
		}
	}
	return out
}
func (g *Grants) Connector(alias, operation string) error {
	if g == nil || !g.other["connector"][alias+":"+operation] {
		return ErrDenied
	}
	return nil
}
func (g *Grants) Events(alias, mode string) error {
	if g == nil || !g.other["events"][alias+":"+mode] {
		return ErrDenied
	}
	return nil
}
func (c *Config) HasConnector(alias string) bool {
	if c == nil {
		return false
	}
	_, ok := c.connectors[alias]
	return ok
}
func (c *Config) HasEvents(alias string) bool {
	if c == nil {
		return false
	}
	_, ok := c.events[alias]
	return ok
}

func (g *Grants) ValidateConnectorOperations(ids []string) error {
	if g == nil {
		return nil
	}
	allowed := map[string]bool{}
	for _, id := range ids {
		allowed[id] = true
	}
	for id := range g.other["connector"] {
		if !allowed[id] {
			return ErrDenied
		}
	}
	return nil
}

// ConfinedFile reads a regular project file without following directory symlinks.
func ConfinedFile(root, name string) ([]byte, error) {
	if name == "" || filepath.IsAbs(name) || strings.ContainsAny(name, "\x00:") || strings.Contains(name, "\\") {
		return nil, ErrInvalid
	}
	p := filepath.Clean(filepath.FromSlash(name))
	if p == ".." || strings.HasPrefix(p, ".."+string(filepath.Separator)) {
		return nil, ErrInvalid
	}
	for dir := p; dir != "."; dir = filepath.Dir(dir) {
		st, e := os.Lstat(filepath.Join(root, dir))
		if e != nil || st.Mode()&os.ModeSymlink != 0 {
			return nil, ErrInvalid
		}
	}
	r, e := os.OpenRoot(root)
	if e != nil {
		return nil, ErrInvalid
	}
	defer r.Close()
	st, e := r.Stat(p)
	if e != nil || !st.Mode().IsRegular() || st.Size() > 1<<20 {
		return nil, ErrInvalid
	}
	return r.ReadFile(p)
}
