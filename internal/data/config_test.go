package data

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigValidation(t *testing.T) {
	for _, tt := range []struct {
		name, raw string
		valid     bool
	}{
		{"memory", `{"connections":{"shop":{"driver":"sqlite","path":":memory:"}}}`, true},
		{"file", `{"connections":{"shop":{"driver":"sqlite","path":"storage/shop.db"}}}`, true},
		{"unknown option", `{"connections":{"shop":{"driver":"sqlite","path":":memory:","dsn":"secret"}}}`, false},
		{"duplicate", `{"connections":{"shop":{"driver":"sqlite","path":":memory:","path":"elsewhere"}}}`, false},
		{"null", `{"connections":{"shop":{"driver":"sqlite","path":":memory:","pool":{"max_idle":null}}}}`, false},
		{"uri", `{"connections":{"shop":{"driver":"sqlite","path":"file:escape?vfs=other"}}}`, false},
		{"memory idle zero", `{"connections":{"shop":{"driver":"sqlite","path":":memory:","pool":{"max_idle":0}}}}`, false},
		{"memory eviction", `{"connections":{"shop":{"driver":"sqlite","path":":memory:","pool":{"idle_timeout_ms":1000}}}}`, false},
		{"memory lifetime", `{"connections":{"shop":{"driver":"sqlite","path":":memory:","pool":{"max_lifetime_ms":1000}}}}`, false},
		{"sqlite parallel pool", `{"connections":{"shop":{"driver":"sqlite","path":":memory:","pool":{"max_open":2}}}}`, false},
		{"operation limit", `{"connections":{"shop":{"driver":"sqlite","path":":memory:","limits":{"operation_timeout_ms":60001}}}}`, false},
		{"remote verified", `{"connections":{"shop":{"driver":"postgres","host":"db.example.test","port":5432,"database":"test","user":"grab","password_env":"GRAB_TEST_PASSWORD"}}}`, true},
		{"loopback plaintext", `{"connections":{"shop":{"driver":"mariadb","host":"127.0.0.1","port":3306,"database":"test","user":"grab","password_env":"GRAB_TEST_PASSWORD","tls":"disabled"}}}`, true},
		{"hostname plaintext", `{"connections":{"shop":{"driver":"postgres","host":"localhost","port":5432,"database":"test","user":"grab","password_env":"GRAB_TEST_PASSWORD","tls":"disabled"}}}`, false},
		{"metadata denied", `{"connections":{"shop":{"driver":"postgres","host":"169.254.169.254","port":5432,"database":"test","user":"grab","password_env":"GRAB_TEST_PASSWORD"}}}`, false},
		{"literal password", `{"connections":{"shop":{"driver":"postgres","host":"127.0.0.1","port":5432,"database":"test","user":"grab","password":"secret"}}}`, false},
		{"trailing JSON", `{"connections":{}} {}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, e := ParseConfig([]byte(tt.raw), filepath.Join(t.TempDir(), "grab.data.json"))
			if (e == nil) != tt.valid {
				t.Fatalf("valid=%v, err=%v", tt.valid, e)
			}
			if e != nil && strings.Contains(e.Error(), "secret") {
				t.Fatal("diagnostic leaked config value")
			}
		})
	}
}

func TestGrants(t *testing.T) {
	c, e := ParseConfig([]byte(`{"connections":{"shop":{"driver":"sqlite","path":":memory:"}}}`), filepath.Join(t.TempDir(), "grab.data.json"))
	if e != nil {
		t.Fatal(e)
	}
	for _, tt := range []struct {
		name          string
		entries       []string
		server, valid bool
	}{
		{"deny by default", nil, false, true}, {"read", []string{"shop:read"}, false, true}, {"write", []string{"shop:read-write"}, false, true}, {"migrate", []string{"shop:migrate"}, false, true}, {"server migrate", []string{"shop:migrate"}, true, false}, {"wildcard", []string{"*:read"}, false, false}, {"unknown alias", []string{"other:read"}, false, false}, {"conflict", []string{"shop:read", "shop:read-write"}, false, false}, {"unknown mode", []string{"shop:admin"}, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g, e := c.Grant(tt.entries, tt.server)
			if (e == nil) != tt.valid {
				t.Fatalf("valid=%v, err=%v", tt.valid, e)
			}
			if tt.name == "deny by default" {
				if _, _, e = g.definition("shop"); e == nil {
					t.Fatal("ungranted alias authorized")
				}
			}
		})
	}
	var g *Grants
	if _, _, e = g.definition("shop"); e == nil {
		t.Fatal("nil grant authorized")
	}
}
