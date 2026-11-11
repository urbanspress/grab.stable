package template

import (
	"errors"
	"testing"

	"grab/internal/interpreter"
	"grab/internal/parser"
	grabrt "grab/internal/runtime"
)

func evalGrab(t *testing.T, src string) map[string]grabrt.Value {
	t.Helper()
	prog, err := parser.Parse("test.grab", src)
	if err != nil {
		t.Fatalf("parser.Parse: %v", err)
	}
	var discard nopWriter
	env, err := interpreter.RunWithEnvironment(prog, &discard)
	if err != nil {
		t.Fatalf("interpreter.RunWithEnvironment: %v", err)
	}
	return env
}

type nopWriter struct{}

func (*nopWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestParseAndRender_Success(t *testing.T) {
	cases := []struct {
		name     string
		grabSrc  string
		tmplSrc  string
		wantHTML string
	}{
		{
			name: "spec case 1 basic interpolation and escaping",
			grabSrc: `set title to "GRAB <0.2> & Friends"
set link to "https://example.com/docs?v=0.2&lang=grab"
set summary to "Safe \"quoted\" preview"`,
			tmplSrc: "<!doctype html>\n<html>\n  <head><title>{title}</title></head>\n  <body>\n    <h1 title=\"{summary}\">{title}</h1>\n    <a href=\"{link}\">Documentation {{v0.2}}</a>\n  </body>\n</html>\n",
			wantHTML: "<!doctype html>\n<html>\n  <head><title>GRAB &lt;0.2&gt; &amp; Friends</title></head>\n  <body>\n    <h1 title=\"Safe &#34;quoted&#34; preview\">GRAB &lt;0.2&gt; &amp; Friends</h1>\n    <a href=\"https://example.com/docs?v=0.2&amp;lang=grab\">Documentation {v0.2}</a>\n  </body>\n</html>\n",
		},
		{
			name: "whitespace inside tag and CRLF normalization",
			grabSrc: `set name to "Ada"`,
			tmplSrc: "<p>{  name\t }</p>\r\n<div>{{literal}}</div>\r\n",
			wantHTML: "<p>Ada</p>\n<div>{literal}</div>\n",
		},
		{
			name: "all portable value kinds formatted canonically and escaped",
			grabSrc: `set flag to yes
set empty to nothing
set count to 42
set price to 19.95
set ratio to 1.25e-2
set raw to b"\x00\xff"
set items to ["<a&b>", 2]
set meta to {"k": "<v>"}
set rec to record {sku: "A-17", active: yes}`,
			tmplSrc: `<ul><li>{flag}</li><li>{empty}</li><li>{count}</li><li>{price}</li><li>{ratio}</li><li>{raw}</li><li>{items}</li><li>{meta}</li><li>{rec}</li></ul>`,
			wantHTML: `<ul><li>yes</li><li>nothing</li><li>42</li><li>19.95</li><li>0.0125</li><li>b&#34;\x00\xff&#34;</li><li>[&#34;&lt;a&amp;b&gt;&#34;, 2]</li><li>{&#34;k&#34;: &#34;&lt;v&gt;&#34;}</li><li>record {sku: &#34;A-17&#34;, active: yes}</li></ul>`,
		},
		{
			name: "safe URL schemes and relative URLs",
			grabSrc: `set u1 to "http://localhost:8080/page"
set u2 to "https://example.com/path"
set u3 to "mailto:user@example.com"
set u4 to "/local/path?a=1&b=2"
set u5 to "./relative"
set u6 to "#anchor"`,
			tmplSrc: `<a href="{u1}">1</a><a href='{u2}'>2</a><a href="{u3}">3</a><img src="{u4}"><form action="{u5}"><button formaction="{u6}">Go</button></form>`,
			wantHTML: `<a href="http://localhost:8080/page">1</a><a href='https://example.com/path'>2</a><a href="mailto:user@example.com">3</a><img src="/local/path?a=1&amp;b=2"><form action="./relative"><button formaction="#anchor">Go</button></form>`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := evalGrab(t, tc.grabSrc)
			tmpl, err := Parse("test.grab.html", tc.tmplSrc)
			if err != nil {
				t.Fatalf("Parse() unexpected error: %v", err)
			}
			got, err := Render(tmpl, env)
			if err != nil {
				t.Fatalf("Render() unexpected error: %v", err)
			}
			if got != tc.wantHTML {
				t.Errorf("Render() =\n%q\nwant:\n%q", got, tc.wantHTML)
			}
		})
	}
}

func TestParse_DiagnosticErrors(t *testing.T) {
	cases := []struct {
		name     string
		tmplSrc  string
		wantKind ErrorKind
		wantLine int
		wantCol  int
	}{
		{
			name:     "invalid UTF-8 encoding",
			tmplSrc:  "<h1>\xff</h1>",
			wantKind: InvalidEncoding,
			wantLine: 1,
			wantCol:  5,
		},
		{
			name:     "unclosed brace at EOF",
			tmplSrc:  "<h1>{title",
			wantKind: MalformedInterpolation,
			wantLine: 1,
			wantCol:  5,
		},
		{
			name:     "newline inside interpolation",
			tmplSrc:  "<h1>{title\n}</h1>",
			wantKind: MalformedInterpolation,
			wantLine: 1,
			wantCol:  5,
		},
		{
			name:     "unmatched closing brace",
			tmplSrc:  "<h1>title}</h1>",
			wantKind: MalformedInterpolation,
			wantLine: 1,
			wantCol:  10,
		},
		{
			name:     "empty interpolation braces",
			tmplSrc:  "<h1>{}</h1>",
			wantKind: MalformedInterpolation,
			wantLine: 1,
			wantCol:  5,
		},
		{
			name:     "whitespace only interpolation braces",
			tmplSrc:  "<h1>{   }</h1>",
			wantKind: MalformedInterpolation,
			wantLine: 1,
			wantCol:  5,
		},
		{
			name:     "reserved word show inside braces",
			tmplSrc:  "<h1>{show title}</h1>",
			wantKind: MalformedInterpolation,
			wantLine: 1,
			wantCol:  6,
		},
		{
			name:     "reserved word yes inside braces",
			tmplSrc:  "<h1>{yes}</h1>",
			wantKind: MalformedInterpolation,
			wantLine: 1,
			wantCol:  6,
		},
		{
			name:     "literal string inside braces",
			tmplSrc:  "<h1>{\"hello\"}</h1>",
			wantKind: MalformedInterpolation,
			wantLine: 1,
			wantCol:  6,
		},
		{
			name:     "multiple identifiers inside braces",
			tmplSrc:  "<h1>{first second}</h1>",
			wantKind: MalformedInterpolation,
			wantLine: 1,
			wantCol:  12,
		},
		{
			name:     "disallowed context onclick attribute",
			tmplSrc:  `<button onclick="{action}">Click</button>`,
			wantKind: DisallowedContext,
			wantLine: 1,
			wantCol:  18,
		},
		{
			name:     "disallowed context unquoted attribute",
			tmplSrc:  `<div class={cls}></div>`,
			wantKind: DisallowedContext,
			wantLine: 1,
			wantCol:  12,
		},
		{
			name:     "disallowed context script element",
			tmplSrc:  "<script>\nvar x = {val};\n</script>",
			wantKind: DisallowedContext,
			wantLine: 2,
			wantCol:  9,
		},
		{
			name:     "disallowed context style element",
			tmplSrc:  "<style>body {{ color: red; }} p {{ width: {w}; }}</style>",
			wantKind: DisallowedContext,
			wantLine: 1,
			wantCol:  43,
		},
		{
			name:     "disallowed context HTML comment",
			tmplSrc:  "<!-- {secret} -->",
			wantKind: DisallowedContext,
			wantLine: 1,
			wantCol:  6,
		},
		{
			name:     "disallowed context tag name",
			tmplSrc:  "<{tag}>hello</{tag}>",
			wantKind: DisallowedContext,
			wantLine: 1,
			wantCol:  2,
		},
		{
			name:     "disallowed context attribute name",
			tmplSrc:  `<div {attr}="1"></div>`,
			wantKind: DisallowedContext,
			wantLine: 1,
			wantCol:  6,
		},
		{
			name:     "disallowed context unclosed tag",
			tmplSrc:  `<div title="{tip}"`,
			wantKind: DisallowedContext,
			wantLine: 1,
			wantCol:  13,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse("bad.grab.html", tc.tmplSrc)
			if err == nil {
				t.Fatalf("Parse() = nil, want %s error", tc.wantKind)
			}
			var tmplErr *Error
			if !errors.As(err, &tmplErr) {
				t.Fatalf("Parse() error type = %T, want *Error", err)
			}
			if tmplErr.Kind != tc.wantKind {
				t.Errorf("Error.Kind = %s, want %s", tmplErr.Kind, tc.wantKind)
			}
			if tmplErr.Span.Start.Line != tc.wantLine || tmplErr.Span.Start.Column != tc.wantCol {
				t.Errorf("Error.Span.Start = line %d, col %d; want line %d, col %d",
					tmplErr.Span.Start.Line, tmplErr.Span.Start.Column, tc.wantLine, tc.wantCol)
			}
		})
	}
}

func TestRender_DiagnosticErrors(t *testing.T) {
	t.Run("spec case 2 unbound template variable", func(t *testing.T) {
		env := evalGrab(t, `set title to "Hello"`)
		tmpl, err := Parse("missing.grab.html", "<h1>{title}</h1>\n<p>{author}</p>\n")
		if err != nil {
			t.Fatalf("Parse() error: %v", err)
		}
		out, err := Render(tmpl, env)
		if out != "" {
			t.Errorf("expected empty output on error, got %q", out)
		}
		var tmplErr *Error
		if !errors.As(err, &tmplErr) {
			t.Fatalf("Render() error = %v, want *Error", err)
		}
		if tmplErr.Kind != UnboundVariable || tmplErr.Name != "author" {
			t.Errorf("got kind=%s name=%q, want UnboundVariable \"author\"", tmplErr.Kind, tmplErr.Name)
		}
		if tmplErr.Span.Start.Line != 2 || tmplErr.Span.Start.Column != 5 {
			t.Errorf("span start = line %d col %d, want line 2 col 5",
				tmplErr.Span.Start.Line, tmplErr.Span.Start.Column)
		}
	})

	unsafeURLs := []string{
		"javascript:alert(1)",
		"JAVASCRIPT:alert(1)",
		"   javascript:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"vbscript:msgbox(1)",
		"file:///etc/passwd",
		"http:alert(1)",
	}
	for _, bad := range unsafeURLs {
		t.Run("unsafe URL "+bad, func(t *testing.T) {
			env := map[string]grabrt.Value{
				"bad_url": grabrt.Text(bad),
			}
			tmpl, err := Parse("xss.grab.html", `<a href="{bad_url}">Click</a>`)
			if err != nil {
				t.Fatalf("Parse() error: %v", err)
			}
			out, err := Render(tmpl, env)
			if out != "" {
				t.Errorf("expected empty output on unsafe URL error, got %q", out)
			}
			var tmplErr *Error
			if !errors.As(err, &tmplErr) || tmplErr.Kind != UnsafeURLScheme {
				t.Fatalf("Render() error = %v, want UnsafeURLScheme", err)
			}
		})
	}
}
