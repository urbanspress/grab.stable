package platform

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestChannelConfiguration(t *testing.T) {
	base := DefaultChannel()
	base.Path = "/live"
	base.Authorize = "authorize"
	base.Receive = "receive"
	base.ClientType = "Message"
	base.ServerType = "Message"
	base.Origins = []string{"https://shop.example"}
	base.Broker.Driver = "memory"
	for _, tt := range []struct {
		name  string
		edit  func(*Channel)
		valid bool
	}{
		{"valid", func(*Channel) {}, true}, {"wildcard origin", func(c *Channel) { c.Origins = []string{"*"} }, false}, {"origin path", func(c *Channel) { c.Origins = []string{"https://shop.example/"} }, false},
		{"missing auth", func(c *Channel) { c.Authorize = "" }, false}, {"unknown broker", func(c *Channel) { c.Broker.Driver = "cluster" }, false}, {"zero queue", func(c *Channel) { c.IncomingMessages = 0 }, false}, {"overflow", func(c *Channel) { c.MaxMessageBytes = 2 << 20 }, false}, {"unsafe path", func(c *Channel) { c.Path = "/a/%2f" }, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			v := base
			tt.edit(&v)
			raw, _ := json.Marshal(map[string]any{"version": 1, "channels": map[string]any{"shop": v}})
			raw = bytes.ReplaceAll(raw, []byte(":null"), []byte(":[]"))
			c, e := Parse(raw, filepath.Join(t.TempDir(), "grab.platform.json"))
			if (e == nil) != tt.valid {
				t.Fatalf("valid=%v error=%v", tt.valid, e)
			}
			if e == nil {
				g, e := c.Grant(map[string][]string{"channel": {"shop:connect"}})
				if e != nil || g.Channel("shop", "connect") != nil || g.Channel("shop", "publish") == nil {
					t.Fatal("independent channel grants")
				}
				copy := c.Channels()
				v := copy["shop"]
				v.Origins[0] = "changed"
				if c.Channels()["shop"].Origins[0] == "changed" {
					t.Fatal("mutable config")
				}
			}
		})
	}
}
func TestScalingBudgetsAndTelemetry(t *testing.T) {
	for _, tt := range []struct {
		raw   string
		n, db int
		valid bool
	}{
		{`{"version":1}`, 2, 0, false},
		{`{"version":1,"workers":{"max_db_connections":24,"db_headroom":4}}`, 2, 10, true},
		{`{"version":1,"workers":{"max_db_connections":23,"db_headroom":4}}`, 2, 10, false},
		{`{"version":1,"workers":{"unknown":1}}`, 2, 0, false},
		{`{"version":1,"workers":{"max_redis_connections":16},"caches":{"x":{"driver":"redis","namespace":"x","host":"127.0.0.1","port":6379,"tls":"disabled","development":true}}}`, 2, 0, true},
		{`{"version":1,"telemetry":{"metrics":{"driver":"openmetrics","port":9091,"token_env":"GRAB_METRICS_TOKEN"}}}`, 1, 0, true},
		{`{"version":1,"telemetry":{"trace":{"driver":"otlp","endpoint":"http://collector.example/v1/traces","token_env":"TOKEN"}}}`, 1, 0, false},
		{`{"version":1,"telemetry":{"trace":{"driver":"otlp","endpoint":"https://collector.example/v1/traces","token_env":"TOKEN","sample_rate":1.1}}}`, 1, 0, false},
	} {
		c, e := Parse([]byte(tt.raw), filepath.Join(t.TempDir(), "platform.json"))
		if e == nil {
			e = c.PoolBudget(tt.n, tt.db)
		}
		if (e == nil) != tt.valid {
			t.Fatalf("%s: %v", tt.raw, e)
		}
	}
}
