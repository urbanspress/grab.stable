package platform

import (
	"encoding/json"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// GRAB function and contract type names share the ASCII identifier shape.
var symbolPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

// ChannelBroker selects transient pub/sub transport for one channel. Memory is
// one-process only; Redis is standalone with verified TLS and pinned DNS.
type ChannelBroker struct {
	Driver       string   `json:"driver"`
	Namespace    string   `json:"namespace"`
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	TLS          string   `json:"tls"`
	Development  bool     `json:"development"`
	UsernameEnv  string   `json:"username_env"`
	PasswordEnv  string   `json:"password_env"`
	AllowedCIDRs []string `json:"allowed_cidrs"`
	PoolSize     int      `json:"pool_size"`
	TimeoutMS    int      `json:"operation_timeout_ms"`
}

// Channel declares one reserved WebSocket upgrade path and its bounds.
type Channel struct {
	Path               string        `json:"path"`
	Authorize          string        `json:"authorize"`
	Receive            string        `json:"receive"`
	PublishAuthorize   string        `json:"publish_authorize"`
	ClientType         string        `json:"client_type"`
	ServerType         string        `json:"server_type"`
	Origins            []string      `json:"origins"`
	AllowMissingOrigin bool          `json:"allow_missing_origin"`
	Development        bool          `json:"development"`
	Broker             ChannelBroker `json:"broker"`
	MaxMessageBytes    int           `json:"max_message_bytes"`
	MaxConnections     int           `json:"max_connections"`
	MaxAudiences       int           `json:"max_audiences"`
	MessagesPerSecond  int           `json:"messages_per_second"`
	OutgoingMessages   int           `json:"outgoing_messages"`
	OutgoingBytes      int           `json:"outgoing_bytes"`
	IncomingMessages   int           `json:"incoming_messages"`
	HandlerTimeoutMS   int           `json:"handler_timeout_ms"`
	PingIntervalMS     int           `json:"ping_interval_ms"`
	PongTimeoutMS      int           `json:"pong_timeout_ms"`
}

// DefaultChannel returns the specification defaults (Section 8).
func DefaultChannel() Channel {
	return Channel{MaxMessageBytes: 64 << 10, MaxConnections: 1000, MaxAudiences: 16, MessagesPerSecond: 100,
		OutgoingMessages: 64, OutgoingBytes: 1 << 20, IncomingMessages: 16, HandlerTimeoutMS: 5000,
		PingIntervalMS: 20000, PongTimeoutMS: 10000}
}

// CanonicalOrigin returns scheme://host[:port] for an exact allowlist entry.
func CanonicalOrigin(raw string) (string, bool) {
	u, e := url.Parse(raw)
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.HasSuffix(raw, "?") || strings.HasSuffix(raw, "#") {
		return "", false
	}
	host := u.Hostname()
	if !validHost(host) && !(strings.HasPrefix(u.Host, "[") && net.ParseIP(host) != nil) {
		return "", false
	}
	port := u.Port()
	if port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return "", false
		}
	}
	if port == "" && strings.HasSuffix(u.Host, ":") || (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		return "", false
	}
	canonical := strings.ToLower(u.Scheme + "://" + u.Host)
	return canonical, canonical == raw
}

func validChannelPath(p string) bool {
	if p == "/_health" || p == "/_ready" {
		return false
	}
	if !strings.HasPrefix(p, "/") || len(p) > 256 || strings.ContainsAny(p, "{}?#%\\\x00\r\n\t ") {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func within(v, lo, hi int) bool { return v >= lo && v <= hi }

func (c *Config) parseChannels(raw map[string]json.RawMessage) error {
	c.channels = map[string]Channel{}
	paths := map[string]bool{}
	for _, w := range c.webhooks {
		paths[w.Path] = true
	}
	spaces := map[string]bool{}
	for alias, b := range raw {
		v := DefaultChannel()
		v.Broker.PoolSize, v.Broker.TimeoutMS = 8, 1000
		if !Identifier(alias) || StrictJSON(b, &v) != nil || !validChannelPath(v.Path) || paths[v.Path] {
			return ErrInvalid
		}
		paths[v.Path] = true
		for _, s := range []string{v.Authorize, v.Receive, v.ClientType, v.ServerType} {
			if !symbolPattern.MatchString(s) {
				return ErrInvalid
			}
		}
		if v.PublishAuthorize != "" && !symbolPattern.MatchString(v.PublishAuthorize) {
			return ErrInvalid
		}
		if len(v.Origins) > 32 || len(v.Origins) == 0 && !v.AllowMissingOrigin {
			return ErrInvalid
		}
		seen := map[string]bool{}
		for _, o := range v.Origins {
			if _, ok := CanonicalOrigin(o); !ok || seen[o] {
				return ErrInvalid
			}
			seen[o] = true
		}
		if !within(v.MaxMessageBytes, 128, 1<<20) || !within(v.MaxConnections, 1, 10000) || !within(v.MaxAudiences, 1, 64) ||
			!within(v.MessagesPerSecond, 1, 1000) || !within(v.OutgoingMessages, 1, 1024) || !within(v.OutgoingBytes, v.MaxMessageBytes, 16<<20) ||
			!within(v.IncomingMessages, 1, 256) || !within(v.HandlerTimeoutMS, 10, 30000) || !within(v.PingIntervalMS, 50, 120000) || !within(v.PongTimeoutMS, 50, 60000) {
			return ErrInvalid
		}
		br := v.Broker
		switch br.Driver {
		case "memory":
			if br.Namespace != "" || br.Host != "" || br.Port != 0 || br.TLS != "" || br.Development || br.UsernameEnv != "" || br.PasswordEnv != "" || len(br.AllowedCIDRs) != 0 {
				return ErrInvalid
			}
		case "redis":
			if !Identifier(br.Namespace) || !validHost(br.Host) || !within(br.Port, 1, 65535) || !within(br.PoolSize, 1, 64) || !within(br.TimeoutMS, 1, 10000) {
				return ErrInvalid
			}
			ip := net.ParseIP(br.Host)
			if br.TLS != "verify" && !(br.TLS == "disabled" && br.Development && ip != nil && ip.IsLoopback()) {
				return ErrInvalid
			}
			for _, s := range []string{br.UsernameEnv, br.PasswordEnv} {
				if s != "" && !envPattern.MatchString(s) {
					return ErrInvalid
				}
			}
			for _, s := range br.AllowedCIDRs {
				if _, _, e := net.ParseCIDR(s); e != nil {
					return ErrInvalid
				}
			}
			space := br.Namespace + ":" + alias
			if spaces[space] {
				return ErrInvalid
			}
			spaces[space] = true
		default:
			return ErrInvalid
		}
		c.channels[alias] = v
	}
	return nil
}

func (c *Config) HasChannel(alias string) bool {
	if c == nil {
		return false
	}
	_, ok := c.channels[alias]
	return ok
}

// Channels returns deep copies; the immutable configuration is never shared.
func (c *Config) Channels() map[string]Channel {
	out := map[string]Channel{}
	if c != nil {
		for k, v := range c.channels {
			v.Origins = append([]string(nil), v.Origins...)
			v.Broker.AllowedCIDRs = append([]string(nil), v.Broker.AllowedCIDRs...)
			out[k] = v
		}
	}
	return out
}

// RedisCache converts a Redis channel broker into the shared verified-client
// configuration. Channel transport never shares cache grants or namespaces.
func (b ChannelBroker) RedisCache() Cache {
	return Cache{Driver: "redis", Namespace: b.Namespace, Host: b.Host, Port: b.Port, TLS: b.TLS, Development: b.Development,
		UsernameEnv: b.UsernameEnv, PasswordEnv: b.PasswordEnv, AllowedCIDRs: append([]string(nil), b.AllowedCIDRs...),
		PoolSize: b.PoolSize, TimeoutMS: b.TimeoutMS, MaxKeys: 1, MaxBytes: 256}
}

// Channel reports whether a connect or publish grant exists for alias.
func (g *Grants) Channel(alias, mode string) error {
	if g == nil || !g.other["channel"][alias+":"+mode] {
		return ErrDenied
	}
	return nil
}
