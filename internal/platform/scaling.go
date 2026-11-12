package platform

import (
	"encoding/json"
	"net"
	"net/url"
)

// Workers bounds total application pools, including reserved maintenance capacity.
type Workers struct {
	TokenEnv       string `json:"token_env"`
	MaxDB          int    `json:"max_db_connections"`
	MaxRedis       int    `json:"max_redis_connections"`
	DBHeadroom     int    `json:"db_headroom"`
	RedisHeadroom  int    `json:"redis_headroom"`
	ManagementPort int    `json:"management_port"`
}
type Telemetry struct {
	Driver       string   `json:"driver"`
	Port         int      `json:"port"`
	TokenEnv     string   `json:"token_env"`
	Endpoint     string   `json:"endpoint"`
	AllowedCIDRs []string `json:"allowed_cidrs"`
	SampleRate   *float64 `json:"sample_rate,omitempty"`
}

func (c *Config) parseScaling(workers map[string]json.RawMessage, telemetry map[string]json.RawMessage) error {
	raw, e := json.Marshal(workers)
	if e != nil {
		return ErrInvalid
	}
	if len(workers) > 0 {
		var w Workers
		if StrictJSON(raw, &w) != nil || w.MaxDB < 0 || w.MaxDB > 100000 || w.MaxRedis < 0 || w.MaxRedis > 100000 || w.DBHeadroom < 0 || w.RedisHeadroom < 0 || !within(w.ManagementPort, 0, 65535) {
			return ErrInvalid
		}
		if w.TokenEnv != "" && !envPattern.MatchString(w.TokenEnv) || w.ManagementPort > 0 && w.TokenEnv == "" {
			return ErrInvalid
		}
		c.workers = &w
	}
	c.telemetry = map[string]Telemetry{}
	ports := map[int]bool{}
	for alias, raw := range telemetry {
		t := Telemetry{}
		if !Identifier(alias) || StrictJSON(raw, &t) != nil || !envPattern.MatchString(t.TokenEnv) {
			return ErrInvalid
		}
		switch t.Driver {
		case "openmetrics":
			if !within(t.Port, 1, 65535) || ports[t.Port] || t.Endpoint != "" || len(t.AllowedCIDRs) > 0 || t.SampleRate != nil {
				return ErrInvalid
			}
			ports[t.Port] = true
		case "otlp":
			u, e := url.Parse(t.Endpoint)
			if e != nil || u.Scheme != "https" || !validHost(u.Hostname()) || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/v1/traces" || t.Port != 0 {
				return ErrInvalid
			}
			if t.SampleRate != nil && (*t.SampleRate < 0 || *t.SampleRate > 1) {
				return ErrInvalid
			}
			for _, s := range t.AllowedCIDRs {
				if _, _, e := net.ParseCIDR(s); e != nil {
					return ErrInvalid
				}
			}
		default:
			return ErrInvalid
		}
		c.telemetry[alias] = t
	}
	return nil
}
func (c *Config) Workers() (Workers, bool) {
	if c == nil || c.workers == nil {
		return Workers{}, false
	}
	return *c.workers, true
}
func (c *Config) Telemetry() map[string]Telemetry {
	out := map[string]Telemetry{}
	if c != nil {
		for k, v := range c.telemetry {
			v.AllowedCIDRs = append([]string(nil), v.AllowedCIDRs...)
			if v.SampleRate != nil {
				r := *v.SampleRate
				v.SampleRate = &r
			}
			out[k] = v
		}
	}
	return out
}
func (g *Grants) Telemetry(alias string) error {
	if g == nil || !g.other["telemetry"][alias] {
		return ErrDenied
	}
	return nil
}

// PoolBudget is deliberately conservative: all declared resources count, even
// when a particular code path might not open them during one test run.
func (c *Config) PoolBudget(n, db int) error {
	if n < 1 || n > 32 {
		return ErrInvalid
	}
	if n == 1 {
		return nil
	}
	w, ok := c.Workers()
	if !ok {
		return ErrInvalid
	}
	for _, channel := range c.channels {
		if channel.Broker.Driver != "redis" {
			return ErrInvalid
		}
	}
	redis := c.RedisPoolBudget()
	if n*db+w.DBHeadroom > w.MaxDB || n*redis+w.RedisHeadroom > w.MaxRedis {
		return ErrInvalid
	}
	return nil
}
func (c *Config) RedisPoolBudget() int {
	if c == nil {
		return 0
	}
	redis := 0
	for _, v := range c.caches {
		if v.Driver == "redis" {
			redis += v.PoolSize
		}
	}
	for _, v := range c.channels {
		if v.Broker.Driver == "redis" {
			redis += v.Broker.PoolSize + 1
		}
	}
	return redis
}

// SecretNames exposes only environment references for the restricted child environment.
func (c *Config) SecretNames() []string {
	var names []string
	if c == nil {
		return names
	}
	if c.workers != nil {
		names = append(names, c.workers.TokenEnv)
	}
	for _, v := range c.caches {
		names = append(names, v.UsernameEnv, v.PasswordEnv)
	}
	for _, v := range c.channels {
		names = append(names, v.Broker.UsernameEnv, v.Broker.PasswordEnv)
	}
	for _, v := range c.telemetry {
		names = append(names, v.TokenEnv)
	}
	for _, v := range c.webhooks {
		names = append(names, v.Keys...)
	}
	for _, v := range c.connectors {
		for _, name := range v.Secrets {
			names = append(names, name)
		}
	}
	return names
}
