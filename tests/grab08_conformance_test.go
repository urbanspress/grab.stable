package tests

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"grab/internal/interpreter"
	"grab/internal/parser"
	"grab/internal/platform"
	rt "grab/internal/runtime"
	"grab/internal/server"
)

func grab08Options(t *testing.T, grants ...string) interpreter.Options {
	t.Helper()
	c, e := platform.Parse([]byte(`{"version":1,"caches":{"shop":{"driver":"memory","namespace":"catalog"}}}`), filepath.Join(t.TempDir(), "grab.platform.json"))
	if e != nil {
		t.Fatal(e)
	}
	g, e := c.Grant(map[string][]string{"cache": grants})
	if e != nil {
		t.Fatal(e)
	}
	return interpreter.Options{PlatformConfig: c, PlatformGrants: g}
}

// This is the initial cache/configuration slice of specification 008. The other
// release gates remain pending; these tests do not claim milestone acceptance.
func TestGRAB08CachePrograms(t *testing.T) {
	for _, tt := range []struct{ name, source, want, code string }{
		{"exact values", `use cache
set store to cache.open using "shop"
call cache.set using store, "price", 123456789012345678901234567890.00100, 10000
set hit to cache.get using store, "price"
show hit.found
show hit.value
call cache.set using store, "null", nothing, 10000
set present to cache.get using store, "null"
set absent to cache.get using store, "absent"
show present.found
show absent.found
show cache.delete using store, "price"
`, "yes\n123456789012345678901234567890.00100\nyes\nno\nyes\n", ""},
		{"fail closed denial", `use cache
set store to cache.open using "shop"
set first to cache.limit using store, "principal", 1, 10000
set second to cache.limit using store, "principal", 1, 10000
show first.allowed
show second.allowed
show second.count
show second.remaining
`, "yes\nno\n2\n0\n", ""},
		{"recoverable error", `use cache
set store to cache.open using "shop"
try
  call cache.set using store, "key", "value", 0
catch problem
  show problem.code
end
`, "GRAB-E314\n", ""},
		{"dynamic alias", `use cache
show "must not execute"
set alias to "shop"
set store to cache.open using alias
`, "", "GRAB-E210"},
		{"unknown alias in dead branch", `use cache
show "must not execute"
if no
  set store to cache.open using "unknown"
end
`, "", "GRAB-E210"},
		{"opaque display", `use cache
set store to cache.open using "shop"
show store
`, "", "GRAB-E301"},
		{"opaque cache value", `use cache
set store to cache.open using "shop"
call cache.set using store, "key", record {handle: store}, 100
`, "", "GRAB-E314"},
		{"task snapshot boundary", `use cache
set store to cache.open using "shop"
do together
  set item to wait for (cache.get using store, "key")
end
`, "", "GRAB-E314"},
		{"task result boundary", `use cache
do together
  set store to wait for (cache.open using "shop")
end
`, "", "GRAB-E314"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, e := parser.Parse("test.grab", tt.source)
			if e != nil {
				t.Fatal(e)
			}
			var out bytes.Buffer
			e = interpreter.RunWithOptions(p, &out, grab08Options(t, "shop:read-write", "shop:limit"))
			if tt.code != "" {
				var ie *interpreter.Error
				if !errors.As(e, &ie) || ie.Code() != tt.code {
					t.Fatalf("want %s got %v", tt.code, e)
				}
			} else if e != nil {
				t.Fatal(e)
			}
			if out.String() != tt.want {
				t.Fatalf("output %q want %q", out.String(), tt.want)
			}
		})
	}
}
func TestGRAB08DefaultDeny(t *testing.T) {
	p, e := parser.Parse("deny.grab", "use cache\nset store to cache.open using \"shop\"\n")
	if e != nil {
		t.Fatal(e)
	}
	e = interpreter.RunWithOptions(p, io.Discard, grab08Options(t))
	var ie *interpreter.Error
	if !errors.As(e, &ie) || ie.Code() != "GRAB-E403" {
		t.Fatal(e)
	}
}
func TestGRAB08GenerationIsolation(t *testing.T) {
	source := `use cache
use server
set store to cache.open using "shop"
route "GET" "/" using request
  set rate to cache.limit using store, "principal", 1, 10000
  give back server.text using 200, rate.count
end
`
	// Use text.from to respect existing server.text's text-only contract.
	source = strings.Replace(source, "use server", "use server\nuse text", 1)
	source = strings.Replace(source, "server.text using 200, rate.count", "server.text using 200, (text.from using rate.count)", 1)
	p, e := parser.Parse("app.grab", source)
	if e != nil {
		t.Fatal(e)
	}
	prepare := func() *interpreter.PreparedProgram {
		p, e := interpreter.Prepare(p, grab08Options(t, "shop:limit"), io.Discard)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = p.Close() })
		return p
	}
	first, second := prepare(), prepare()
	for _, tt := range []struct {
		p    *interpreter.PreparedProgram
		want string
	}{{first, "1"}, {first, "2"}, {second, "1"}} {
		v, _, e := tt.p.Execute(context.Background(), tt.p.Routes()[0], rt.Record(nil), io.Discard)
		if e != nil {
			t.Fatal(e)
		}
		body, ok := v.LookupEntry("body")
		if !ok || body.TextValue() != tt.want {
			t.Fatalf("response=%s err=%v", v.Display(), e)
		}
	}
	_ = first.Close()
	if _, _, e := first.Execute(context.Background(), first.Routes()[0], rt.Record(nil), io.Discard); e == nil {
		t.Fatal("closed generation still active")
	}
}

func TestGRAB08CacheExampleHTTP(t *testing.T) {
	c, e := platform.Parse([]byte(`{"version":1,"caches":{"catalog":{"driver":"memory","namespace":"example"}}}`), filepath.Join(t.TempDir(), "grab.platform.json"))
	if e != nil {
		t.Fatal(e)
	}
	g, e := c.Grant(map[string][]string{"cache": {"catalog:read-write", "catalog:limit"}})
	if e != nil {
		t.Fatal(e)
	}
	path, e := filepath.Abs(filepath.Join("..", "examples", "cache_app.grab"))
	if e != nil {
		t.Fatal(e)
	}
	a, e := server.NewApplication(path, "", interpreter.Options{PlatformConfig: c, PlatformGrants: g}, server.DefaultConfig(), io.Discard)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	for i := 0; i < 4; i++ {
		w := httptest.NewRecorder()
		a.ServeHTTP(w, httptest.NewRequest("GET", "/catalog", nil))
		want := 200
		if i == 3 {
			want = 429
		}
		if w.Code != want {
			t.Fatal(w.Code, w.Body.String())
		}
		if i == 0 && !strings.Contains(w.Body.String(), `"price":"12345678901234567890.00100"`) {
			t.Fatal("example lost precision", w.Body.String())
		}
		if i == 3 && w.Header().Get("Retry-After") == "" {
			t.Fatal("no retry header")
		}
	}
	// A full store cannot admit a fresh rate bucket; the example must fail
	// closed with 503 instead of allowing the request or pretending denial.
	c, e = platform.Parse([]byte(`{"version":1,"caches":{"catalog":{"driver":"memory","namespace":"example-full","max_keys":1}}}`), filepath.Join(t.TempDir(), "grab.platform.json"))
	if e != nil {
		t.Fatal(e)
	}
	g, e = c.Grant(map[string][]string{"cache": {"catalog:read-write", "catalog:limit"}})
	if e != nil {
		t.Fatal(e)
	}
	full, e := server.NewApplication(path, "", interpreter.Options{PlatformConfig: c, PlatformGrants: g}, server.DefaultConfig(), io.Discard)
	if e != nil {
		t.Fatal(e)
	}
	defer full.Close()
	w := httptest.NewRecorder()
	full.ServeHTTP(w, httptest.NewRequest("GET", "/catalog", nil))
	if w.Code != 503 {
		t.Fatal("full store failed open", w.Code, w.Body.String())
	}
}
