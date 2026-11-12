package platform

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrictConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name, raw string
		valid     bool
	}{
		{"empty", `{"version":1}`, true},
		{"memory", `{"version":1,"caches":{"shop":{"driver":"memory","namespace":"shop-dev"}}}`, true},
		{"unknown", `{"version":1,"secret":"private"}`, false},
		{"duplicate", `{"version":1,"version":1}`, false},
		{"nested duplicate", `{"version":1,"caches":{"shop":{"driver":"memory","driver":"redis","namespace":"x"}}}`, false},
		{"null", `{"version":1,"caches":null}`, false},
		{"version", `{"version":2}`, false},
		{"trailing", `{"version":1} {}`, false},
		{"wildcard", `{"version":1,"caches":{"*":{"driver":"memory","namespace":"x"}}}`, false},
		{"missing namespace", `{"version":1,"caches":{"shop":{"driver":"memory"}}}`, false},
		{"zero limit", `{"version":1,"caches":{"shop":{"driver":"memory","namespace":"x","max_keys":0}}}`, false},
		{"zero pool", `{"version":1,"caches":{"shop":{"driver":"memory","namespace":"x","pool_size":0}}}`, false},
		{"wrong driver", `{"version":1,"caches":{"shop":{"driver":"auto","namespace":"x"}}}`, false},
		{"unused sections", `{"version":1,"connectors":{},"channels":{},"events":{},"telemetry":{}}`, true},
		{"unimplemented fail closed", `{"version":1,"channels":{"chat":{}}}`, false},
		{"redis tls", `{"version":1,"caches":{"shop":{"driver":"redis","namespace":"x","host":"redis.example.com","port":6379,"tls":"verify","password_env":"GRAB_CACHE_TEST_SECRET"}}}`, true},
		{"no plaintext dns", `{"version":1,"caches":{"shop":{"driver":"redis","namespace":"x","host":"localhost","port":6379,"tls":"disabled","development":true}}}`, false},
		{"no plaintext remote", `{"version":1,"caches":{"shop":{"driver":"redis","namespace":"x","host":"192.0.2.1","port":6379,"tls":"disabled","development":true}}}`, false},
		{"no redis creds memory", `{"version":1,"caches":{"shop":{"driver":"memory","namespace":"x","password_env":"SECRET"}}}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, e := Parse([]byte(tt.raw), filepath.Join(t.TempDir(), "grab.platform.json"))
			if (e == nil) != tt.valid {
				t.Fatalf("valid=%v err=%v", tt.valid, e)
			}
			if e != nil && strings.Contains(e.Error(), "private") {
				t.Fatal("value leaked")
			}
		})
	}
}
func TestIndependentGrants(t *testing.T) {
	c, e := Parse([]byte(`{"version":1,"caches":{"shop":{"driver":"memory","namespace":"dev"}}}`), filepath.Join(t.TempDir(), "grab.platform.json"))
	if e != nil {
		t.Fatal(e)
	}
	for _, tt := range []struct {
		flags     []string
		operation string
		allowed   bool
	}{
		{nil, "open", false}, {[]string{"shop:read"}, "read", true}, {[]string{"shop:read"}, "read-write", false}, {[]string{"shop:read-write"}, "read", true}, {[]string{"shop:read-write"}, "limit", false}, {[]string{"shop:limit"}, "read", false}, {[]string{"shop:limit"}, "limit", true}, {[]string{"shop:read", "shop:limit"}, "limit", true},
	} {
		g, e := c.Grant(map[string][]string{"cache": tt.flags})
		if e != nil {
			t.Fatal(e)
		}
		_, e = g.Cache("shop", tt.operation)
		if (e == nil) != tt.allowed {
			t.Fatalf("%v %s: %v", tt.flags, tt.operation, e)
		}
	}
	for _, s := range []string{"*:read", "missing:read", "shop:*", "shop:write", "shop:read:limit", "shop"} {
		if _, e = c.Grant(map[string][]string{"cache": {s}}); !errors.Is(e, ErrDenied) {
			t.Fatal(s, e)
		}
	}
	for _, system := range []string{"connector", "events", "channel", "telemetry", "net"} {
		if _, e = c.Grant(map[string][]string{system: {"shop:read"}}); !errors.Is(e, ErrDenied) {
			t.Fatal(system, e)
		}
	}
}
func TestSecretLookupRedaction(t *testing.T) {
	const key = "GRAB_PLATFORM_TEST_SECRET"
	t.Setenv(key, "a-private-value")
	got, e := Secret(key)
	if e != nil || got != "a-private-value" {
		t.Fatal("lookup failed")
	}
	for _, s := range []string{"", "bad\nsecret"} {
		t.Setenv(key, s)
		_, e := Secret(key)
		if e == nil || strings.Contains(e.Error(), key) || strings.Contains(e.Error(), "bad") {
			t.Fatal("credential error leaked detail")
		}
	}
}

func TestHostSyntax(t *testing.T) {
	for _, tt := range []struct {
		host  string
		valid bool
	}{{"127.0.0.1", true}, {"::1", true}, {"redis.example.com", true}, {"redis..example", false}, {"redis.example/path", false}, {"[::1]", false}, {"x%zone", false}, {"-bad.example", false}, {"bad_.example", false}} {
		if got := validHost(tt.host); got != tt.valid {
			t.Fatal(tt.host, got)
		}
	}
}
