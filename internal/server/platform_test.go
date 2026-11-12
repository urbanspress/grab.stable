package server

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"grab/internal/interpreter"
	"grab/internal/platform"
)

func TestPlatformReloadKeepsLastGoodGeneration(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "grab.platform.json")
	write := func(raw string) {
		t.Helper()
		if e := os.WriteFile(configPath, []byte(raw), 0600); e != nil {
			t.Fatal(e)
		}
	}
	valid := `{"version":1,"caches":{"shop":{"driver":"memory","namespace":"reload"}}}`
	write(valid)
	loader := func() (*platform.Config, *platform.Grants, error) {
		raw, e := os.ReadFile(configPath)
		if e != nil {
			return nil, nil, e
		}
		c, e := platform.Parse(raw, configPath)
		if e != nil {
			return nil, nil, e
		}
		g, e := c.Grant(map[string][]string{"cache": {"shop:limit"}})
		return c, g, e
	}
	path := appFile(t, `use cache
use server
set store to cache.open using "shop"
route "GET" "/"
  set admission to cache.limit using store, "trusted-demo", 1, 60000
  if admission.allowed
    give back server.text using 200, "allowed"
  end
  give back server.text using 429, "limited"
end
`)
	a, e := NewApplication(path, "", interpreter.Options{PlatformLoader: loader, PlatformPath: configPath}, DefaultConfig(), io.Discard)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	if w := request(a, "GET", "/", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(a, "GET", "/", ""); w.Code != 429 {
		t.Fatal(w.Code, w.Body.String())
	}
	write(`{"version":1,"unexpected":"private"}`)
	if e = a.Reload(); e == nil || strings.Contains(e.Error(), "private") {
		t.Fatal("bad reload", e)
	}
	if w := request(a, "GET", "/", ""); w.Code != 429 {
		t.Fatal("last good state lost", w.Code)
	}
	write(valid)
	if e = a.Reload(); e != nil {
		t.Fatal(e)
	}
	if w := request(a, "GET", "/", ""); w.Code != 200 {
		t.Fatal("new memory generation was not isolated", w.Code)
	}
}
func TestPlatformReadinessRequiresGrantInRoute(t *testing.T) {
	c, e := platform.Parse([]byte(`{"version":1,"caches":{"shop":{"driver":"memory","namespace":"ready"}}}`), filepath.Join(t.TempDir(), "grab.platform.json"))
	if e != nil {
		t.Fatal(e)
	}
	path := appFile(t, `use cache
route "GET" "/"
  set store to cache.open using "shop"
  give back "ok"
end
`)
	a, e := NewApplication(path, "", interpreter.Options{PlatformConfig: c}, DefaultConfig(), io.Discard)
	if e == nil {
		a.Close()
		t.Fatal("ungranted route became ready")
	}
}
