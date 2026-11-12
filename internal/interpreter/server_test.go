package interpreter

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"grab/internal/parser"
	grabrt "grab/internal/runtime"
)

func TestRouteValidation(t *testing.T) {
	for _, tc := range []struct{ name, source, code string }{
		{"valid", `route "GET" "/items/{id}" using req
give back req.params.id
end`, ""},
		{"lowercase", `route "get" "/"
give back "ok"
end`, "GRAB-E209"},
		{"method", `route "OPTIONS" "/"
end`, "GRAB-E209"},
		{"method separator", `route "GET|HEAD" "/"
end`, "GRAB-E209"},
		{"relative", `route "GET" "items"
end`, "GRAB-E209"},
		{"duplicate parameter", `route "GET" "/{id}/{id}"
end`, "GRAB-E209"},
		{"bad braces", `route "GET" "/item{x}"
end`, "GRAB-E209"},
		{"equivalent", `route "GET" "/{a}"
end
route "GET" "/{b}"
end`, "GRAB-E209"},
		{"unknown binding", `route "GET" "/"
give back missing
end`, "GRAB-E201"},
		{"return outside route", `give back "no"`, "GRAB-E205"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, e := parser.Parse("app.grab", tc.source+"\n")
			if e == nil {
				_, e = Validate(p)
			}
			if tc.code == "" {
				if e != nil {
					t.Fatal(e)
				}
				return
			}
			code, _, _, _ := ExtractErrorMeta(e)
			if e == nil || code != tc.code {
				t.Fatalf("%v, code %s; want %s", e, code, tc.code)
			}
		})
	}
}

func TestServerHelpers(t *testing.T) {
	for _, tc := range []struct {
		name, expr string
		status     int
		body, code string
	}{
		{"text", `server.text using 201, "created"`, 201, "created", ""},
		{"json precision", `server.json using 200, record {n: 123456789012345678901234567890, p: 19.95}`, 200, `{"n":123456789012345678901234567890,"p":19.95}`, ""},
		{"redirect", `server.redirect using "/next", 303`, 303, "", ""},
		{"bad redirect", `server.redirect using "javascript:alert(1)"`, 0, "", "GRAB-E312"},
		{"network redirect", `server.redirect using "//evil.test"`, 0, "", "GRAB-E312"},
		{"triple slash redirect", `server.redirect using "///evil.test"`, 0, "", "GRAB-E312"},
		{"bad status", `server.text using 999, "x"`, 0, "", "GRAB-E312"},
		{"bad JSON", `server.parse_json using record {body: ""}`, 0, "", "GRAB-E312"},
		{"custom bytes", `server.response using record {status: 202, body: b"abc"}`, 202, "abc", ""},
		{"header injection", `server.response using record {status: 200, headers: {"X-Test": "x\r\ny"}, body: "x"}`, 0, "", "GRAB-E312"},
		{"bad header name", `server.response using record {status: 200, headers: {"Bad Header": "x"}, body: "x"}`, 0, "", "GRAB-E312"},
		{"nothing", `nothing`, 0, "", "GRAB-E312"},
		{"number", `123`, 0, "", "GRAB-E312"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, e := parser.Parse("app.grab", "use server\nroute \"GET\" \"/\"\ngive back "+tc.expr+"\nend\n")
			if e != nil {
				t.Fatal(e)
			}
			prep, e := Prepare(p, Options{}, io.Discard)
			if e != nil {
				t.Fatal(e)
			}
			v, _, e := prep.Execute(context.Background(), prep.Routes()[0], grabrt.Nothing(), nil)
			var res Response
			if e == nil {
				res, e = DecodeResponse(v)
			}
			if tc.code != "" {
				code, _, _, _ := ExtractErrorMeta(e)
				if e == nil || code != tc.code {
					t.Fatalf("error=%v code=%s", e, code)
				}
				return
			}
			if e != nil || res.Status != tc.status || string(res.Body) != tc.body {
				t.Fatalf("response=%+v error=%v", res, e)
			}
		})
	}
}

func TestPreparedIsolationAndCancellation(t *testing.T) {
	p, e := parser.Parse("app.grab", `set count to 0
route "GET" "/" using req
set count to count plus 1
give back "ok"
end
`)
	if e != nil {
		t.Fatal(e)
	}
	prep, e := Prepare(p, Options{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		_, env, e := prep.Execute(context.Background(), prep.Routes()[0], grabrt.Nothing(), nil)
		if e != nil || env["count"].Display() != "1" {
			t.Fatalf("environment=%v error=%v", env, e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, e = prep.Execute(ctx, prep.Routes()[0], grabrt.Nothing(), nil)
	if !errors.Is(e, context.Canceled) && !strings.Contains(e.Error(), "canceled") {
		t.Fatal(e)
	}
}

func TestRouteMatchingPriority(t *testing.T) {
	p, e := parser.Parse("app.grab", `route "GET" "/items/{id}"
give back "parameter"
end
route "GET" "/items/featured"
give back "static"
end
`)
	if e != nil {
		t.Fatal(e)
	}
	prep, e := Prepare(p, Options{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if prep.Routes()[0].Definition.Path != "/items/featured" {
		t.Fatal("static route must sort first")
	}
	params, ok := prep.Routes()[1].Match("/items/caf%C3%A9")
	v, _ := params.LookupEntry("id")
	if !ok || v.TextValue() != "café" {
		t.Fatal(params)
	}
	for _, path := range []string{"/items/", "/items/a/b", "/items/%ff", "/items/%zz"} {
		if _, ok := prep.Routes()[1].Match(path); ok {
			t.Fatalf("matched %s", path)
		}
	}
}

func TestConcurrentTaskPanicIsolated(t *testing.T) {
	p, e := parser.Parse("app.grab", `use server
route "GET" "/"
do together
set a to server.html using "panic.grab.html", record {}
set b to "other task"
end
give back a
end
`)
	if e != nil {
		t.Fatal(e)
	}
	prep, e := Prepare(p, Options{TemplateLoader: func(string, map[string]grabrt.Value) (string, error) { panic("unexpected template failure") }}, nil)
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = prep.Execute(context.Background(), prep.Routes()[0], grabrt.Nothing(), nil)
	if e == nil || !strings.Contains(e.Error(), "internal task failure") {
		t.Fatalf("task panic was not contained: %v", e)
	}
}

func TestPriorityAcrossInterspersedRouteLengths(t *testing.T) {
	p, e := parser.Parse("app.grab", `route "GET" "/a/{id}"
give back "parameter"
end
route "GET" "/"
give back "home"
end
route "GET" "/a/featured"
give back "static"
end
route "GET" "/{first}/x"
give back "later static"
end
route "GET" "/a/{last}"
give back "duplicate"
end
`)
	if e != nil {
		t.Fatal(e)
	}
	// The final declaration is structurally equivalent to the first.
	p.Statements = p.Statements[:len(p.Statements)-1]
	prep, e := Prepare(p, Options{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ path, want string }{{"/a/featured", "/a/featured"}, {"/a/x", "/a/{id}"}, {"/b/x", "/{first}/x"}} {
		found := ""
		for _, r := range prep.Routes() {
			if _, ok := r.Match(tc.path); ok {
				found = r.Definition.Path
				break
			}
		}
		if found != tc.want {
			t.Fatalf("%s matched %s; want %s", tc.path, found, tc.want)
		}
	}
}
