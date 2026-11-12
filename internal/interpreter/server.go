package interpreter

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"grab/internal/ast"
	"grab/internal/lexer"
	grabrt "grab/internal/runtime"
)

// Route is an immutable compiled route declaration.
type Route struct {
	Definition       *ast.RouteDef
	segments         []string
	identifierParams bool
}

func routeError(d *ast.RouteDef, message string) error {
	return &Error{Kind: InvalidRouteDefinition, Span: d.Span, Err: fmt.Errorf("%s", message)}
}

func compileRoutes(defs []*ast.RouteDef) ([]*Route, error) {
	var routes []*Route
	seen := map[string]bool{}
	for _, d := range defs {
		switch d.Method {
		case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE":
		default:
			return nil, routeError(d, "use an uppercase supported HTTP method")
		}
		if !strings.HasPrefix(d.Path, "/") || strings.ContainsAny(d.Path, "?#\\\x00\r\n\t ") {
			return nil, routeError(d, "route paths must be absolute URL paths")
		}
		segs := strings.Split(d.Path, "/")
		names := map[string]bool{}
		shape := append([]string(nil), segs...)
		identifierParams := true
		for i, s := range segs {
			if strings.ContainsAny(s, "{}") {
				if len(s) < 3 || s[0] != '{' || s[len(s)-1] != '}' || strings.ContainsAny(s[1:len(s)-1], "{}") {
					return nil, routeError(d, "malformed path parameter")
				}
				name := s[1 : len(s)-1]
				identifierParams = identifierParams && isValidGRABIdentifier(name)
				if names[name] {
					return nil, routeError(d, "duplicate path parameter")
				}
				names[name] = true
				shape[i] = "{}"
			}
			if s == "." || s == ".." {
				return nil, routeError(d, "invalid path segment")
			}
		}
		key := d.Method + " " + strings.Join(shape, "/")
		if seen[key] {
			return nil, routeError(d, "duplicate route pattern")
		}
		seen[key] = true
		routes = append(routes, &Route{Definition: d, segments: segs, identifierParams: identifierParams})
	}
	sort.SliceStable(routes, func(i, j int) bool {
		a, b := routes[i].segments, routes[j].segments
		// Group lengths first so shorter non-matching patterns cannot interrupt
		// the ordering of routes that compete for the same request path.
		if len(a) != len(b) {
			return len(a) < len(b)
		}
		for k := 0; k < len(a) && k < len(b); k++ {
			ap, bp := strings.HasPrefix(a[k], "{"), strings.HasPrefix(b[k], "{")
			if ap != bp {
				return !ap
			}
		}
		return false
	})
	return routes, nil
}

// Match returns decoded path parameters without normalizing or merging segments.
func (r *Route) Match(path string) (grabrt.Value, bool) {
	parts := strings.Split(path, "/")
	if len(parts) != len(r.segments) {
		return grabrt.Value{}, false
	}
	var entries []grabrt.Entry
	for i, s := range r.segments {
		p, err := url.PathUnescape(parts[i])
		if err != nil || !utf8.ValidString(p) {
			return grabrt.Value{}, false
		}
		if strings.HasPrefix(s, "{") {
			if p == "" {
				return grabrt.Value{}, false
			}
			name := s[1 : len(s)-1]
			entries = append(entries, grabrt.Entry{Name: name, Value: grabrt.Text(p)})
		} else if s != p {
			return grabrt.Value{}, false
		}
	}
	if !r.identifierParams {
		return grabrt.Map(entries), true
	}
	return grabrt.Record(entries), true
}

// PreparedProgram holds validated code and an immutable startup environment.
// Values are immutable, so each request only needs a fresh binding map.
type PreparedProgram struct {
	program   *ast.Program
	functions map[string]*ast.FunctionDef
	routes    []*Route
	base      map[string]grabrt.Value
	opts      Options
}

func Prepare(program *ast.Program, opts Options, output io.Writer) (*PreparedProgram, error) {
	f, _, routes, err := validateProgram(program)
	if err != nil {
		return nil, err
	}
	if output == nil {
		output = io.Discard
	}
	p := &PreparedProgram{program: program, functions: f, routes: routes, opts: opts}
	if len(routes) > 0 {
		env := newScope(nil)
		c := newExecContext(context.Background(), output, f, opts)
		if _, _, err = c.execBlock(program.Statements, env); err != nil {
			return nil, err
		}
		p.base = env.snapshot()
	}
	return p, nil
}

func (p *PreparedProgram) Routes() []*Route { return append([]*Route(nil), p.routes...) }
func (p *PreparedProgram) Execute(ctx context.Context, route *Route, req grabrt.Value, output io.Writer) (grabrt.Value, map[string]grabrt.Value, error) {
	if output == nil {
		output = io.Discard
	}
	env := newScope(nil)
	for k, v := range p.base {
		env.defineLocal(k, v)
	}
	body := p.program.Statements
	if route != nil {
		body = route.Definition.Body
		if route.Definition.RequestName != "" {
			env.defineLocal(route.Definition.RequestName, req)
		}
	}
	c := newExecContext(ctx, output, p.functions, p.opts)
	v, _, err := c.execBlock(body, env)
	return v, env.snapshot(), err
}

// LiteralTemplates enumerates server.html calls in every nested AST block.
func LiteralTemplates(program *ast.Program) map[string]lexer.Span {
	paths := map[string]lexer.Span{}
	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		if !v.IsValid() {
			return
		}
		if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
			if !v.IsNil() {
				walk(v.Elem())
			}
			return
		}
		if v.CanInterface() {
			var args []ast.Expression
			var module, name string
			switch n := v.Interface().(type) {
			case ast.CallExpr:
				module, name, args = n.Module, n.Name, n.Arguments
			case ast.CallStatement:
				module, name, args = n.Module, n.Name, n.Arguments
			}
			if module == "server" && name == "html" && len(args) > 0 {
				if lit, ok := args[0].(*ast.LiteralExpr); ok && lit.Kind == ast.TextLiteral {
					if s, e := strconv.Unquote(lit.Lexeme); e == nil {
						paths[s] = lit.Span
					}
				}
			}
		}
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				walk(v.Field(i))
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		}
	}
	walk(reflect.ValueOf(program))
	return paths
}

type Response struct {
	Status  int
	Headers http.Header
	Body    []byte
}

func invalidResponse(span lexer.Span, message string) error {
	return &Error{Kind: InvalidServerResponse, Span: span, Err: fmt.Errorf("%s", message)}
}
func statusValue(v grabrt.Value) (int, bool) {
	if v.Kind() != grabrt.IntegerKind || !v.IntegerValue().IsInt64() {
		return 0, false
	}
	n := v.IntegerValue().Int64()
	return int(n), n >= 100 && n <= 599
}
func responseRecord(status int, headers grabrt.Value, body grabrt.Value) grabrt.Value {
	return grabrt.Record([]grabrt.Entry{{Name: "_grab_response", Value: grabrt.Boolean(true)}, {Name: "status", Value: grabrt.IntegerFromInt64(int64(status))}, {Name: "headers", Value: headers}, {Name: "body", Value: body}})
}
func simpleResponse(status int, contentType, body string) grabrt.Value {
	return responseRecord(status, grabrt.Map([]grabrt.Entry{{Name: "Content-Type", Value: grabrt.Text(contentType)}}), grabrt.Text(body))
}

// DecodeResponse validates all custom headers before any bytes reach the socket.
func DecodeResponse(v grabrt.Value) (Response, error) {
	if v.Kind() == grabrt.TextKind {
		return Response{200, http.Header{"Content-Type": []string{"text/plain; charset=utf-8"}}, []byte(v.TextValue())}, nil
	}
	fail := func() (Response, error) {
		return Response{}, invalidResponse(lexer.Span{}, "return a server response or text from the route")
	}
	if v.Kind() != grabrt.RecordKind {
		return fail()
	}
	marker, _ := v.LookupEntry("_grab_response")
	if !marker.BooleanValue() {
		return fail()
	}
	s, _ := v.LookupEntry("status")
	status, ok := statusValue(s)
	if !ok {
		return fail()
	}
	body, _ := v.LookupEntry("body")
	var b []byte
	switch body.Kind() {
	case grabrt.TextKind:
		b = []byte(body.TextValue())
	case grabrt.BytesKind:
		b = body.BytesValue()
	default:
		return fail()
	}
	h, _ := v.LookupEntry("headers")
	if h.Kind() != grabrt.RecordKind && h.Kind() != grabrt.MapKind {
		return fail()
	}
	headers := http.Header{}
	for _, e := range h.Entries() {
		if e.Name == "" || e.Value.Kind() != grabrt.TextKind || strings.ContainsAny(e.Value.TextValue(), "\r\n\x00") {
			return fail()
		}
		for _, c := range e.Name {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", c)) {
				return fail()
			}
		}
		// Framing belongs to net/http, never application-controlled headers.
		if strings.EqualFold(e.Name, "Content-Length") || strings.EqualFold(e.Name, "Transfer-Encoding") {
			return fail()
		}
		headers.Set(e.Name, e.Value.TextValue())
	}
	return Response{status, headers, b}, nil
}

var serverFunctions = map[string]stdlibFunc{
	"text": {arity: 2, fn: serverText}, "json": {arity: 2, fn: serverJSON},
	"html": {minArity: 2, maxArity: 3, fn: serverHTML}, "redirect": {minArity: 1, maxArity: 2, fn: serverRedirect},
	"response": {arity: 1, fn: serverResponse}, "parse_json": {arity: 1, fn: serverParseJSON},
}

func serverText(_ *execContext, s lexer.Span, a []grabrt.Value) (grabrt.Value, error) {
	n, ok := statusValue(a[0])
	if !ok || a[1].Kind() != grabrt.TextKind {
		return grabrt.Value{}, invalidResponse(s, "expected status 100–599 and text")
	}
	return simpleResponse(n, "text/plain; charset=utf-8", a[1].TextValue()), nil
}
func serverJSON(_ *execContext, s lexer.Span, a []grabrt.Value) (grabrt.Value, error) {
	n, ok := statusValue(a[0])
	b, err := encodeJSONValue(a[1], "", 0)
	if !ok || err != nil {
		return grabrt.Value{}, invalidResponse(s, "expected a valid status and JSON value")
	}
	return simpleResponse(n, "application/json; charset=utf-8", b), nil
}
func serverHTML(c *execContext, s lexer.Span, a []grabrt.Value) (grabrt.Value, error) {
	n := 200
	if len(a) == 3 {
		var ok bool
		n, ok = statusValue(a[2])
		if !ok {
			return grabrt.Value{}, invalidResponse(s, "invalid HTML status")
		}
	}
	if a[0].Kind() != grabrt.TextKind || (a[1].Kind() != grabrt.MapKind && a[1].Kind() != grabrt.RecordKind) || c.templateLoader == nil {
		return grabrt.Value{}, invalidResponse(s, "expected template path, data, and server template loader")
	}
	data := map[string]grabrt.Value{}
	for _, e := range a[1].Entries() {
		data[e.Name] = e.Value
	}
	b, err := c.templateLoader(a[0].TextValue(), data)
	if err != nil {
		return grabrt.Value{}, invalidResponse(s, err.Error())
	}
	return simpleResponse(n, "text/html; charset=utf-8", b), nil
}
func serverRedirect(_ *execContext, s lexer.Span, a []grabrt.Value) (grabrt.Value, error) {
	n := 302
	if len(a) == 2 {
		var ok bool
		n, ok = statusValue(a[1])
		if !ok {
			n = 0
		}
	}
	loc := a[0].TextValue()
	u, err := url.Parse(loc)
	safe := err == nil && loc != "" && !strings.HasPrefix(loc, "//") && !strings.ContainsAny(loc, "\r\n\x00\\") && ((u.Scheme == "http" || u.Scheme == "https") && u.Host != "" || u.Scheme == "" && u.Host == "" && strings.ContainsAny(loc[:1], "/?#"))
	if a[0].Kind() != grabrt.TextKind || !safe || !(n == 301 || n == 302 || n == 303 || n == 307 || n == 308) {
		return grabrt.Value{}, invalidResponse(s, "unsafe redirect or invalid redirect status")
	}
	return responseRecord(n, grabrt.Map([]grabrt.Entry{{Name: "Location", Value: grabrt.Text(loc)}}), grabrt.Text("")), nil
}
func serverResponse(_ *execContext, s lexer.Span, a []grabrt.Value) (grabrt.Value, error) {
	if a[0].Kind() != grabrt.RecordKind && a[0].Kind() != grabrt.MapKind {
		return grabrt.Value{}, invalidResponse(s, "response requires a record or map")
	}
	st, _ := a[0].LookupEntry("status")
	n, ok := statusValue(st)
	if !ok {
		return grabrt.Value{}, invalidResponse(s, "invalid response status")
	}
	h, ok := a[0].LookupEntry("headers")
	if !ok {
		h = grabrt.Map(nil)
	}
	b, _ := a[0].LookupEntry("body")
	v := responseRecord(n, h, b)
	if _, err := DecodeResponse(v); err != nil {
		return grabrt.Value{}, invalidResponse(s, "invalid response headers or body")
	}
	return v, nil
}
func serverParseJSON(_ *execContext, s lexer.Span, a []grabrt.Value) (grabrt.Value, error) {
	b, ok := a[0].LookupEntry("body")
	if !ok || b.Kind() != grabrt.TextKind {
		return grabrt.Value{}, invalidResponse(s, "bad request payload: expected request body")
	}
	v, err := parseJSONText(b.TextValue())
	if err != nil {
		return grabrt.Value{}, invalidResponse(s, "bad request payload: invalid or empty JSON")
	}
	return v, nil
}
