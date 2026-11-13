package data

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
)

type Mode string

const (
	Read      Mode = "read"
	ReadWrite Mode = "read-write"
	Migrate   Mode = "migrate"
)

// Config keeps host definitions private. Parsing configuration never opens a
// database, resolves a hostname, or reads a password environment variable.
type Config struct{ connections map[string]definition }
type definition struct {
	Driver      string     `json:"driver"`
	Path        string     `json:"path,omitempty"`
	Host        string     `json:"host,omitempty"`
	Port        int        `json:"port,omitempty"`
	Database    string     `json:"database,omitempty"`
	User        string     `json:"user,omitempty"`
	PasswordEnv string     `json:"password_env,omitempty"`
	TLS         string     `json:"tls,omitempty"`
	TLSCA       string     `json:"tls_ca,omitempty"`
	Pool        poolInput  `json:"pool,omitempty"`
	Limits      limitInput `json:"limits,omitempty"`
}
type poolInput struct {
	MaxOpen       *int `json:"max_open,omitempty"`
	MaxIdle       *int `json:"max_idle,omitempty"`
	MaxLifetimeMS *int `json:"max_lifetime_ms,omitempty"`
	IdleTimeoutMS *int `json:"idle_timeout_ms,omitempty"`
}
type limitInput struct {
	OperationTimeoutMS   *int `json:"operation_timeout_ms,omitempty"`
	MaxRows              *int `json:"max_rows,omitempty"`
	MaxResultBytes       *int `json:"max_result_bytes,omitempty"`
	MaxValueBytes        *int `json:"max_value_bytes,omitempty"`
	MaxStatements        *int `json:"max_statements,omitempty"`
	TransactionTimeoutMS *int `json:"transaction_timeout_ms,omitempty"`
}

// Grants is tied to a particular immutable Config, not a mutable alias lookup.
type Grants struct {
	config *Config
	modes  map[string]Mode
}

func ParseConfig(raw []byte, manifestPath string) (*Config, error) {
	bad := func() (*Config, error) { return nil, fail(Invalid, "invalid database configuration") }
	if len(raw) > 1<<20 || len(raw) == 0 || !filepath.IsAbs(manifestPath) {
		return bad()
	}
	if err := uniqueJSON(raw); err != nil {
		return bad()
	}
	var input struct {
		Connections map[string]definition `json:"connections"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&input); err != nil || input.Connections == nil {
		return bad()
	}
	for alias, def := range input.Connections {
		if !Identifier(alias) {
			return bad()
		}
		if err := def.validate(filepath.Dir(manifestPath)); err != nil {
			return bad()
		}
		input.Connections[alias] = def
	}
	return &Config{connections: input.Connections}, nil
}

// uniqueJSON rejects duplicate object keys and null option values, which the
// standard struct decoder alone would silently accept as omitted settings.
func uniqueJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 32 {
			return fail(Invalid, "configuration nesting exceeded")
		}
		t, e := d.Token()
		if e != nil {
			return e
		}
		if t == nil {
			return fail(Invalid, "null configuration option")
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					if e != nil {
						return e
					}
					key, ok := k.(string)
					if !ok || seen[key] {
						return fail(Invalid, "duplicate configuration key")
					}
					seen[key] = true
					if e = value(depth + 1); e != nil {
						return e
					}
				}
			case '[':
				for d.More() {
					if e = value(depth + 1); e != nil {
						return e
					}
				}
			default:
				return fail(Invalid, "invalid configuration structure")
			}
			_, e = d.Token()
			return e
		}
		return nil
	}
	if e := value(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return fail(Invalid, "trailing configuration data")
	}
	return nil
}

func inRange(n *int, def, min, max int) (int, bool) {
	if n == nil {
		return def, true
	}
	return *n, *n >= min && *n <= max
}

func (d *definition) validate(base string) error {
	bad := func() error { return fail(Invalid, "invalid database configuration") }
	sqlite := d.Driver == "sqlite"
	if !sqlite && d.Driver != "mariadb" && d.Driver != "postgres" {
		return bad()
	}
	if sqlite {
		if d.Path == "" || d.Host != "" || d.Port != 0 || d.Database != "" || d.User != "" || d.PasswordEnv != "" || d.TLS != "" || d.TLSCA != "" {
			return bad()
		}
		if d.Path != ":memory:" {
			if strings.ContainsAny(d.Path, "?\x00") || strings.HasPrefix(strings.ToLower(d.Path), "file:") || strings.HasPrefix(d.Path, "\\\\") || strings.HasPrefix(d.Path, "//") {
				return bad()
			}
			if !filepath.IsAbs(d.Path) {
				d.Path = filepath.Join(base, d.Path)
			}
			d.Path = filepath.Clean(d.Path)
		}
	} else {
		if d.Path != "" || d.Host == "" || d.Database == "" || d.User == "" || !Identifier(d.PasswordEnv) || strings.ContainsAny(d.Host, "/@\\?#\x00 \t\r\n") || strings.ContainsAny(d.Database, "\x00\r\n") || strings.ContainsAny(d.User, "\x00\r\n") {
			return bad()
		}
		if d.Port < 1 || d.Port > 65535 {
			return bad()
		}
		if d.TLS == "" {
			d.TLS = "verify"
		}
		if d.TLS != "verify" && d.TLS != "disabled" {
			return bad()
		}
		if d.TLSCA != "" {
			if d.TLS != "verify" || strings.ContainsAny(d.TLSCA, "\x00") || strings.HasPrefix(d.TLSCA, `\\`) || strings.HasPrefix(d.TLSCA, "//") {
				return bad()
			}
			if !filepath.IsAbs(d.TLSCA) {
				d.TLSCA = filepath.Join(base, d.TLSCA)
			}
			d.TLSCA = filepath.Clean(d.TLSCA)
		}
		ip := net.ParseIP(d.Host)
		if d.TLS == "disabled" && (ip == nil || !ip.IsLoopback()) {
			return bad()
		}
		if ip != nil && blockedDatabaseIP(ip) {
			return bad()
		}
	}
	maxOpenDefault, maxIdleDefault, lifetimeDefault := 10, 5, 1800000
	if sqlite {
		maxOpenDefault, maxIdleDefault, lifetimeDefault = 1, 1, 0
	}
	open, ok := inRange(d.Pool.MaxOpen, maxOpenDefault, 1, 100)
	if !ok || sqlite && open != 1 {
		return bad()
	}
	idle, ok := inRange(d.Pool.MaxIdle, maxIdleDefault, 0, open)
	if !ok {
		return bad()
	}
	lifetime, ok := inRange(d.Pool.MaxLifetimeMS, lifetimeDefault, 0, 3600000)
	if !ok || lifetime != 0 && lifetime < 1000 {
		return bad()
	}
	_, ok = inRange(d.Pool.IdleTimeoutMS, 300000, 1000, 3600000)
	if !ok {
		return bad()
	}
	if sqlite && d.Path == ":memory:" {
		if idle != 1 || lifetime != 0 || d.Pool.IdleTimeoutMS != nil {
			return bad()
		}
		// Host pool construction must disable idle eviction for memory pools.
	}
	for _, limit := range []struct {
		n             *int
		def, min, max int
	}{
		{d.Limits.OperationTimeoutMS, 5000, 1, 60000}, {d.Limits.MaxRows, 10000, 1, 100000}, {d.Limits.MaxResultBytes, 10 << 20, 1, 100 << 20}, {d.Limits.MaxValueBytes, 1 << 20, 1, 10 << 20}, {d.Limits.MaxStatements, 128, 1, 1024}, {d.Limits.TransactionTimeoutMS, 10000, 1, 60000},
	} {
		if _, ok := inRange(limit.n, limit.def, limit.min, limit.max); !ok {
			return bad()
		}
	}
	return nil
}

func (c *Config) Grant(entries []string, server bool) (*Grants, error) {
	g := &Grants{config: c, modes: map[string]Mode{}}
	for _, entry := range entries {
		parts := strings.Split(entry, ":")
		if len(parts) != 2 || c == nil {
			return nil, fail(Denied, "invalid database grant")
		}
		alias, mode := parts[0], Mode(parts[1])
		if _, ok := c.connections[alias]; !ok {
			return nil, fail(Denied, "database alias is not configured")
		}
		if mode != Read && mode != ReadWrite && mode != Migrate || server && mode == Migrate {
			return nil, fail(Denied, "database grant is not allowed in this host")
		}
		if existing, ok := g.modes[alias]; ok && existing != mode {
			return nil, fail(Denied, "conflicting database grants")
		}
		g.modes[alias] = mode
	}
	return g, nil
}

func (g *Grants) definition(alias string) (definition, Mode, error) {
	if g == nil || g.config == nil {
		return definition{}, "", fail(Denied, "database capability denied")
	}
	mode, ok := g.modes[alias]
	if !ok {
		return definition{}, "", fail(Denied, "database capability denied")
	}
	d, ok := g.config.connections[alias]
	if !ok {
		return definition{}, "", fail(Denied, "database capability denied")
	}
	return d, mode, nil
}

// Endpoint is for host dialers only. Password resolution belongs after Grant.
func (d definition) endpoint() string { return net.JoinHostPort(d.Host, strconv.Itoa(d.Port)) }

// PoolBudget returns a conservative sum of configured maximum open connections.
func (g *Grants) PoolBudget() int {
	n := 0
	if g != nil && g.config != nil {
		for alias := range g.modes {
			d := g.config.connections[alias]
			fallback := 10
			if d.Driver == "sqlite" {
				fallback = 1
			}
			v, ok := inRange(d.Pool.MaxOpen, fallback, 1, 100)
			if ok {
				n += v
			}
		}
	}
	return n
}

func (g *Grants) SecretNames() []string {
	var names []string
	if g != nil && g.config != nil {
		for alias := range g.modes {
			names = append(names, g.config.connections[alias].PasswordEnv)
		}
	}
	return names
}

func (g *Grants) PoolLimit(alias string) int {
	if g == nil || g.config == nil {
		return 0
	}
	if _, ok := g.modes[alias]; !ok {
		return 0
	}
	d := g.config.connections[alias]
	fallback := 10
	if d.Driver == "sqlite" {
		fallback = 1
	}
	n, _ := inRange(d.Pool.MaxOpen, fallback, 1, 100)
	return n
}

// ConnectionSummary describes a configured database connection without resolving secrets.
type ConnectionSummary struct {
	Driver      string `json:"driver"`
	Path        string `json:"path,omitempty"`
	Host        string `json:"host,omitempty"`
	Port        int    `json:"port,omitempty"`
	Database    string `json:"database,omitempty"`
	User        string `json:"user,omitempty"`
	PasswordEnv string `json:"password_env,omitempty"`
	TLS         string `json:"tls,omitempty"`
}

// Summaries returns sanitized connection metadata for read-only inspection.
func (c *Config) Summaries() map[string]ConnectionSummary {
	out := map[string]ConnectionSummary{}
	if c == nil {
		return out
	}
	for alias, d := range c.connections {
		out[alias] = ConnectionSummary{
			Driver:      d.Driver,
			Path:        d.Path,
			Host:        d.Host,
			Port:        d.Port,
			Database:    d.Database,
			User:        d.User,
			PasswordEnv: d.PasswordEnv,
			TLS:         d.TLS,
		}
	}
	return out
}
