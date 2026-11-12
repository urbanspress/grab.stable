// Package platform owns private, versioned host configuration and capabilities.
// Parsing and granting never perform network I/O or resolve host secrets.
package platform

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var ErrInvalid = errors.New("GRAB-E210: invalid platform configuration")
var ErrDenied = errors.New("GRAB-E403: platform capability denied")
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var envPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

func Identifier(s string) bool { return namePattern.MatchString(s) }

func validHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	if len(host) > 253 || host == "" {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// Cache is copied out of an immutable Config. Zero omitted budgets get defaults;
// explicit zero limits are rejected by Parse.
type Cache struct {
	Driver       string   `json:"driver"`
	Namespace    string   `json:"namespace"`
	MaxKeys      int      `json:"max_keys"`
	MaxBytes     int      `json:"max_bytes"`
	PoolSize     int      `json:"pool_size"`
	TimeoutMS    int      `json:"operation_timeout_ms"`
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	TLS          string   `json:"tls"`
	Development  bool     `json:"development"`
	UsernameEnv  string   `json:"username_env"`
	PasswordEnv  string   `json:"password_env"`
	AllowedCIDRs []string `json:"allowed_cidrs"`
}

type Config struct {
	caches     map[string]Cache
	root       string
	connectors map[string]Connector
	events     map[string]EventStore
	webhooks   map[string]Webhook
}

func (c *Config) HasCache(alias string) bool {
	if c == nil {
		return false
	}
	_, ok := c.caches[alias]
	return ok
}

// StrictJSON rejects duplicates at every depth, null configuration values,
// trailing input and excessive nesting before decoding the closed schema.
func StrictJSON(raw []byte, target any) error { return strictJSON(raw, target, false) }

// StrictValueJSON additionally permits JSON null for typed wire values.
func StrictValueJSON(raw []byte, target any) error { return strictJSON(raw, target, true) }
func strictJSON(raw []byte, target any, allowNull bool) error {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 32 {
			return ErrInvalid
		}
		t, e := d.Token()
		if e != nil || t == nil && !allowNull {
			return ErrInvalid
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					if e != nil {
						return ErrInvalid
					}
					s, ok := k.(string)
					if !ok || seen[s] {
						return ErrInvalid
					}
					seen[s] = true
					if e = walk(depth + 1); e != nil {
						return e
					}
				}
			case '[':
				for d.More() {
					if e = walk(depth + 1); e != nil {
						return e
					}
				}
			default:
				return ErrInvalid
			}
			if _, e = d.Token(); e != nil {
				return ErrInvalid
			}
		}
		return nil
	}
	if e := walk(0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(target); e != nil {
		return ErrInvalid
	}
	return nil
}

func Parse(raw []byte, manifestPath string) (*Config, error) {
	if !filepath.IsAbs(manifestPath) {
		return nil, ErrInvalid
	}
	var in struct {
		Version    int                        `json:"version"`
		Caches     map[string]json.RawMessage `json:"caches"`
		Connectors map[string]json.RawMessage `json:"connectors"`
		Webhooks   map[string]json.RawMessage `json:"webhooks"`
		Channels   map[string]json.RawMessage `json:"channels"`
		Events     map[string]json.RawMessage `json:"events"`
		Workers    map[string]json.RawMessage `json:"workers"`
		Telemetry  map[string]json.RawMessage `json:"telemetry"`
	}
	if StrictJSON(raw, &in) != nil || in.Version != 1 {
		return nil, ErrInvalid
	}
	// Until an adapter is implemented its declarations must fail closed, never
	// be accepted as working configuration.
	if len(in.Channels)+len(in.Workers)+len(in.Telemetry) != 0 {
		return nil, ErrInvalid
	}
	c := &Config{caches: map[string]Cache{}, root: filepath.Dir(manifestPath)}
	if e := c.parseIntegrations(in.Connectors, in.Events, in.Webhooks); e != nil {
		return nil, e
	}
	spaces := map[string]bool{}
	for alias, rawDef := range in.Caches {
		def := Cache{MaxKeys: 10000, MaxBytes: 16 << 20, PoolSize: 8, TimeoutMS: 1000}
		if !Identifier(alias) || StrictJSON(rawDef, &def) != nil {
			return nil, ErrInvalid
		}
		if !Identifier(def.Namespace) || def.MaxKeys < 1 || def.MaxKeys > 1000000 || def.MaxBytes < 256 || def.MaxBytes > 1<<30 || def.PoolSize < 1 || def.PoolSize > 64 || def.TimeoutMS < 1 || def.TimeoutMS > 10000 {
			return nil, ErrInvalid
		}
		// An alias is included in the physical prefix as well as the namespace.
		space := def.Namespace + ":" + alias
		if spaces[space] {
			return nil, ErrInvalid
		}
		spaces[space] = true
		switch def.Driver {
		case "memory":
			if def.Host != "" || def.Port != 0 || def.TLS != "" || def.Development || def.UsernameEnv != "" || def.PasswordEnv != "" || len(def.AllowedCIDRs) != 0 {
				return nil, ErrInvalid
			}
		case "redis":
			if !validHost(def.Host) || def.Port < 1 || def.Port > 65535 {
				return nil, ErrInvalid
			}
			if def.TLS != "verify" && !(def.TLS == "disabled" && def.Development && net.ParseIP(def.Host) != nil && net.ParseIP(def.Host).IsLoopback()) {
				return nil, ErrInvalid
			}
			for _, s := range []string{def.UsernameEnv, def.PasswordEnv} {
				if s != "" && !envPattern.MatchString(s) {
					return nil, ErrInvalid
				}
			}
			for _, s := range def.AllowedCIDRs {
				if _, _, e := net.ParseCIDR(s); e != nil {
					return nil, ErrInvalid
				}
			}
		default:
			return nil, ErrInvalid
		}
		c.caches[alias] = def
	}
	return c, nil
}

// Grants binds capabilities to one immutable configuration. No wildcard or
// filesystem/network/database capability implies platform authority.
type Grants struct {
	config *Config
	cache  map[string]map[string]bool
	other  map[string]map[string]bool
}

func (c *Config) Grant(flags map[string][]string) (*Grants, error) {
	g := &Grants{config: c, cache: map[string]map[string]bool{}, other: map[string]map[string]bool{"connector": {}, "events": {}}}
	for subsystem, values := range flags {
		for _, v := range values {
			alias, mode, ok := strings.Cut(v, ":")
			if subsystem == "connector" || subsystem == "events" {
				if !ok || !Identifier(alias) || !Identifier(mode) || c == nil {
					return nil, ErrDenied
				}
				if subsystem == "connector" && !c.HasConnector(alias) || subsystem == "events" && (!c.HasEvents(alias) || (mode != "accept" && mode != "work")) {
					return nil, ErrDenied
				}
				g.other[subsystem][v] = true
				continue
			}
			if subsystem != "cache" || !ok || !Identifier(alias) || (mode != "read" && mode != "read-write" && mode != "limit") || c == nil {
				return nil, ErrDenied
			}
			if _, ok := c.caches[alias]; !ok {
				return nil, ErrDenied
			}
			if g.cache[alias] == nil {
				g.cache[alias] = map[string]bool{}
			}
			g.cache[alias][mode] = true
		}
	}
	return g, nil
}
func (g *Grants) Cache(alias, operation string) (Cache, error) {
	if g == nil || g.config == nil {
		return Cache{}, ErrDenied
	}
	modes := g.cache[alias]
	allowed := modes[operation] || operation == "read" && modes["read-write"]
	if operation == "open" {
		allowed = len(modes) > 0
	}
	if !allowed {
		return Cache{}, ErrDenied
	}
	d := g.config.caches[alias]
	d.AllowedCIDRs = append([]string(nil), d.AllowedCIDRs...)
	return d, nil
}

// Secret is used only while preparing an authorized adapter, never by check.
// Its errors intentionally contain neither variable names nor values.
func Secret(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	if !envPattern.MatchString(name) {
		return "", ErrInvalid
	}
	v, ok := os.LookupEnv(name)
	if !ok || v == "" || len(v) > 8192 || strings.ContainsAny(v, "\x00\r\n") {
		return "", errors.New("GRAB-E210: platform credential unavailable")
	}
	return v, nil
}
