package interpreter

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"grab/internal/lexer"
	grabrt "grab/internal/runtime"
)

const (
	maxJSONDepth         = 64
	defaultHTTPTimeoutMS = 10000
	maxHTTPTimeoutMS     = 60000
	maxHTTPRetries       = 3
	maxHTTPRedirects     = 5
)

var sensitiveRedirectHeaders = []string{
	"Authorization",
	"Proxy-Authorization",
	"Cookie",
	"Set-Cookie",
	"X-Api-Key",
}

// ── json module ───────────────────────────────────────────────────────────────

func stdJSONParse(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if args[0].Kind() != grabrt.TextKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	raw := args[0].TextValue()
	if len(raw) > maxFileSizeBytes {
		return grabrt.Value{}, &Error{
			Kind: StandardLibraryError,
			Span: span,
			Err:  fmt.Errorf("json.parse input exceeds 10 MiB limit"),
		}
	}
	val, err := parseJSONText(raw)
	if err != nil {
		return grabrt.Value{}, &Error{
			Kind: StandardLibraryError,
			Span: span,
			Err:  fmt.Errorf("json.parse: %w", err),
		}
	}
	return val, nil
}

func stdJSONStringify(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	out, err := encodeJSONValue(args[0], "", 0)
	if err != nil {
		return grabrt.Value{}, &Error{
			Kind: StandardLibraryError,
			Span: span,
			Err:  fmt.Errorf("json.stringify: %w", err),
		}
	}
	return grabrt.Text(out), nil
}

func stdJSONStringifyPretty(_ *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	out, err := encodeJSONValue(args[0], "  ", 0)
	if err != nil {
		return grabrt.Value{}, &Error{
			Kind: StandardLibraryError,
			Span: span,
			Err:  fmt.Errorf("json.stringify_pretty: %w", err),
		}
	}
	return grabrt.Text(out), nil
}

func encodeJSONValue(v grabrt.Value, indent string, depth int) (string, error) {
	if depth > maxJSONDepth {
		return "", fmt.Errorf("maximum JSON nesting depth (%d) exceeded", maxJSONDepth)
	}
	switch v.Kind() {
	case grabrt.NothingKind:
		return "null", nil
	case grabrt.BooleanKind:
		if v.BooleanValue() {
			return "true", nil
		}
		return "false", nil
	case grabrt.IntegerKind, grabrt.DecimalKind, grabrt.FloatKind:
		return v.Display(), nil
	case grabrt.TextKind:
		return encodeJSONString(v.TextValue()), nil
	case grabrt.BytesKind:
		return "", fmt.Errorf("raw bytes cannot be encoded directly to JSON")
	case grabrt.ListKind:
		items := v.ListValues()
		if len(items) == 0 {
			return "[]", nil
		}
		parts := make([]string, len(items))
		for i, item := range items {
			enc, err := encodeJSONValue(item, indent, depth+1)
			if err != nil {
				return "", err
			}
			if indent != "" {
				parts[i] = strings.Repeat(indent, depth+1) + enc
			} else {
				parts[i] = enc
			}
		}
		if indent != "" {
			return "[\n" + strings.Join(parts, ",\n") + "\n" + strings.Repeat(indent, depth) + "]", nil
		}
		return "[" + strings.Join(parts, ",") + "]", nil
	case grabrt.MapKind, grabrt.RecordKind:
		entries := v.Entries()
		if len(entries) == 0 {
			return "{}", nil
		}
		parts := make([]string, len(entries))
		sep := ":"
		if indent != "" {
			sep = ": "
		}
		for i, entry := range entries {
			valEnc, err := encodeJSONValue(entry.Value, indent, depth+1)
			if err != nil {
				return "", err
			}
			kv := encodeJSONString(entry.Name) + sep + valEnc
			if indent != "" {
				parts[i] = strings.Repeat(indent, depth+1) + kv
			} else {
				parts[i] = kv
			}
		}
		if indent != "" {
			return "{\n" + strings.Join(parts, ",\n") + "\n" + strings.Repeat(indent, depth) + "}", nil
		}
		return "{" + strings.Join(parts, ",") + "}", nil
	default:
		return "", fmt.Errorf("unsupported value kind for JSON encoding")
	}
}

func encodeJSONString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

type jsonParser struct {
	src string
	pos int
}

func parseJSONText(raw string) (grabrt.Value, error) {
	if !utf8.ValidString(raw) {
		return grabrt.Value{}, fmt.Errorf("invalid UTF-8 in JSON text")
	}
	p := &jsonParser{src: raw}
	p.skipWS()
	val, err := p.parseValue(0)
	if err != nil {
		return grabrt.Value{}, err
	}
	p.skipWS()
	if p.pos != len(p.src) {
		return grabrt.Value{}, fmt.Errorf("unexpected trailing token at byte %d", p.pos)
	}
	return val, nil
}

func (p *jsonParser) skipWS() {
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *jsonParser) parseValue(depth int) (grabrt.Value, error) {
	if depth > maxJSONDepth {
		return grabrt.Value{}, fmt.Errorf("maximum JSON nesting depth (%d) exceeded", maxJSONDepth)
	}
	p.skipWS()
	if p.pos >= len(p.src) {
		return grabrt.Value{}, fmt.Errorf("unexpected end of JSON input")
	}
	switch p.src[p.pos] {
	case '"':
		s, err := p.parseString()
		if err != nil {
			return grabrt.Value{}, err
		}
		return grabrt.Text(s), nil
	case '{':
		return p.parseObject(depth + 1)
	case '[':
		return p.parseArray(depth + 1)
	case 't':
		if strings.HasPrefix(p.src[p.pos:], "true") {
			p.pos += 4
			return grabrt.Boolean(true), nil
		}
		return grabrt.Value{}, fmt.Errorf("invalid literal at byte %d", p.pos)
	case 'f':
		if strings.HasPrefix(p.src[p.pos:], "false") {
			p.pos += 5
			return grabrt.Boolean(false), nil
		}
		return grabrt.Value{}, fmt.Errorf("invalid literal at byte %d", p.pos)
	case 'n':
		if strings.HasPrefix(p.src[p.pos:], "null") {
			p.pos += 4
			return grabrt.Nothing(), nil
		}
		return grabrt.Value{}, fmt.Errorf("invalid literal at byte %d", p.pos)
	default:
		if p.src[p.pos] == '-' || (p.src[p.pos] >= '0' && p.src[p.pos] <= '9') {
			return p.parseNumber()
		}
		return grabrt.Value{}, fmt.Errorf("unexpected character %q at byte %d", p.src[p.pos], p.pos)
	}
}

func (p *jsonParser) parseString() (string, error) {
	if p.pos >= len(p.src) || p.src[p.pos] != '"' {
		return "", fmt.Errorf("expected '\"' at byte %d", p.pos)
	}
	p.pos++ //skip opening '"'
	var b strings.Builder
	for p.pos < len(p.src) {
		ch := p.src[p.pos]
		if ch == '"' {
			p.pos++
			return b.String(), nil
		}
		if ch < 0x20 {
			return "", fmt.Errorf("unescaped control character in JSON string at byte %d", p.pos)
		}
		if ch != '\\' {
			r, width := utf8.DecodeRuneInString(p.src[p.pos:])
			b.WriteRune(r)
			p.pos += width
			continue
		}
		p.pos++ // skip '\'
		if p.pos >= len(p.src) {
			return "", fmt.Errorf("unterminated escape sequence in JSON string")
		}
		esc := p.src[p.pos]
		p.pos++
		switch esc {
		case '"', '\\', '/':
			b.WriteByte(esc)
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'u':
			r1, err := p.readHex4()
			if err != nil {
				return "", err
			}
			if utf16.IsSurrogate(r1) {
				if p.pos+2 <= len(p.src) && p.src[p.pos:p.pos+2] == `\u` {
					saved := p.pos
					p.pos += 2
					r2, err := p.readHex4()
					if err != nil {
						return "", err
					}
					combined := utf16.DecodeRune(r1, r2)
					if combined != utf8.RuneError {
						b.WriteRune(combined)
						continue
					}
					p.pos = saved
				}
				return "", fmt.Errorf("invalid Unicode surrogate in JSON string")
			}
			b.WriteRune(r1)
		default:
			return "", fmt.Errorf("invalid escape '\\%c' in JSON string", esc)
		}
	}
	return "", fmt.Errorf("unterminated JSON string")
}

func (p *jsonParser) readHex4() (rune, error) {
	if p.pos+4 > len(p.src) {
		return 0, fmt.Errorf("incomplete \\uXXXX escape in JSON string")
	}
	hexStr := p.src[p.pos : p.pos+4]
	val, err := strconv.ParseUint(hexStr, 16, 16)
	if err != nil {
		return 0, fmt.Errorf("invalid hex in \\u%s", hexStr)
	}
	p.pos += 4
	return rune(val), nil
}

func (p *jsonParser) parseNumber() (grabrt.Value, error) {
	start := p.pos
	if p.src[p.pos] == '-' {
		p.pos++
		if p.pos >= len(p.src) {
			return grabrt.Value{}, fmt.Errorf("malformed JSON number")
		}
	}
	if p.src[p.pos] == '0' {
		p.pos++
		if p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			return grabrt.Value{}, fmt.Errorf("leading zero in JSON number")
		}
	} else if p.src[p.pos] >= '1' && p.src[p.pos] <= '9' {
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
		}
	} else {
		return grabrt.Value{}, fmt.Errorf("malformed JSON number")
	}

	hasDot := false
	if p.pos < len(p.src) && p.src[p.pos] == '.' {
		hasDot = true
		p.pos++
		if p.pos >= len(p.src) || p.src[p.pos] < '0' || p.src[p.pos] > '9' {
			return grabrt.Value{}, fmt.Errorf("malformed fraction in JSON number")
		}
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
		}
	}

	hasExp := false
	if p.pos < len(p.src) && (p.src[p.pos] == 'e' || p.src[p.pos] == 'E') {
		hasExp = true
		p.pos++
		if p.pos < len(p.src) && (p.src[p.pos] == '+' || p.src[p.pos] == '-') {
			p.pos++
		}
		if p.pos >= len(p.src) || p.src[p.pos] < '0' || p.src[p.pos] > '9' {
			return grabrt.Value{}, fmt.Errorf("malformed exponent in JSON number")
		}
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
		}
	}

	lexeme := p.src[start:p.pos]
	if hasExp {
		return grabrt.FloatFromString(lexeme)
	}
	if hasDot {
		return grabrt.DecimalFromString(lexeme)
	}
	return grabrt.IntegerFromString(lexeme)
}

func (p *jsonParser) parseArray(depth int) (grabrt.Value, error) {
	p.pos++ // skip '['
	p.skipWS()
	var items []grabrt.Value
	if p.pos < len(p.src) && p.src[p.pos] == ']' {
		p.pos++
		return grabrt.List(items), nil
	}
	for {
		val, err := p.parseValue(depth)
		if err != nil {
			return grabrt.Value{}, err
		}
		items = append(items, val)
		p.skipWS()
		if p.pos >= len(p.src) {
			return grabrt.Value{}, fmt.Errorf("unterminated JSON array")
		}
		if p.src[p.pos] == ']' {
			p.pos++
			return grabrt.List(items), nil
		}
		if p.src[p.pos] != ',' {
			return grabrt.Value{}, fmt.Errorf("expected ',' or ']' in JSON array at byte %d", p.pos)
		}
		p.pos++
	}
}

func (p *jsonParser) parseObject(depth int) (grabrt.Value, error) {
	p.pos++ // skip '{'
	p.skipWS()
	var entries []grabrt.Entry
	if p.pos < len(p.src) && p.src[p.pos] == '}' {
		p.pos++
		return grabrt.Record(entries), nil
	}
	allValidRecordFields := true
	seenKeys := make(map[string]int)

	for {
		p.skipWS()
		if p.pos >= len(p.src) || p.src[p.pos] != '"' {
			return grabrt.Value{}, fmt.Errorf("expected string key in JSON object at byte %d", p.pos)
		}
		key, err := p.parseString()
		if err != nil {
			return grabrt.Value{}, err
		}
		if !isValidGRABIdentifier(key) {
			allValidRecordFields = false
		}
		p.skipWS()
		if p.pos >= len(p.src) || p.src[p.pos] != ':' {
			return grabrt.Value{}, fmt.Errorf("expected ':' after key in JSON object at byte %d", p.pos)
		}
		p.pos++
		val, err := p.parseValue(depth)
		if err != nil {
			return grabrt.Value{}, err
		}
		if idx, exists := seenKeys[key]; exists {
			allValidRecordFields = false
			entries[idx].Value = val
		} else {
			seenKeys[key] = len(entries)
			entries = append(entries, grabrt.Entry{Name: key, Value: val})
		}

		p.skipWS()
		if p.pos >= len(p.src) {
			return grabrt.Value{}, fmt.Errorf("unterminated JSON object")
		}
		if p.src[p.pos] == '}' {
			p.pos++
			if allValidRecordFields {
				return grabrt.Record(entries), nil
			}
			return grabrt.Map(entries), nil
		}
		if p.src[p.pos] != ',' {
			return grabrt.Value{}, fmt.Errorf("expected ',' or '}' in JSON object at byte %d", p.pos)
		}
		p.pos++
	}
}

func isValidGRABIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if i == 0 {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_') {
				return false
			}
		} else {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_') {
				return false
			}
		}
	}
	lx, err := lexer.NewLexer("id", s)
	if err != nil {
		return false
	}
	toks, err := lx.LexAll()
	if err != nil || len(toks) != 2 {
		return false
	}
	return toks[0].Kind == lexer.TokenIdentifier
}

// ── http module ───────────────────────────────────────────────────────────────

type httpRequestSpec struct {
	method    string
	url       string
	headers   map[string]string
	body      grabrt.Value
	hasBody   bool
	timeoutMS int
	retries   int
}

func stdHTTPGet(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	spec, err := buildMethodSpec("GET", span, args, false)
	if err != nil {
		return grabrt.Value{}, err
	}
	return executeHTTPRequest(ctx, span, spec)
}

func stdHTTPGetJSON(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	spec, err := buildMethodSpec("GET", span, args, false)
	if err != nil {
		return grabrt.Value{}, err
	}
	hasAccept := false
	for k := range spec.headers {
		if strings.EqualFold(k, "Accept") {
			hasAccept = true
			break
		}
	}
	if !hasAccept {
		spec.headers["Accept"] = "application/json"
	}
	respVal, err := executeHTTPRequest(ctx, span, spec)
	if err != nil {
		return grabrt.Value{}, err
	}
	okVal, _ := respVal.LookupEntry("ok")
	statusVal, _ := respVal.LookupEntry("status")
	bodyVal, _ := respVal.LookupEntry("body")
	if !okVal.BooleanValue() {
		return grabrt.Value{}, &Error{
			Kind: NetworkError,
			Span: span,
			Err:  fmt.Errorf("HTTP %s returned status %s", spec.method, statusVal.Display()),
		}
	}
	parsed, err := parseJSONText(bodyVal.TextValue())
	if err != nil {
		return grabrt.Value{}, &Error{
			Kind: StandardLibraryError,
			Span: span,
			Err:  fmt.Errorf("http.get_json: %w", err),
		}
	}
	return parsed, nil
}

func stdHTTPDelete(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	spec, err := buildMethodSpec("DELETE", span, args, false)
	if err != nil {
		return grabrt.Value{}, err
	}
	return executeHTTPRequest(ctx, span, spec)
}

func stdHTTPPost(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	spec, err := buildMethodSpec("POST", span, args, true)
	if err != nil {
		return grabrt.Value{}, err
	}
	return executeHTTPRequest(ctx, span, spec)
}

func stdHTTPPut(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	spec, err := buildMethodSpec("PUT", span, args, true)
	if err != nil {
		return grabrt.Value{}, err
	}
	return executeHTTPRequest(ctx, span, spec)
}

func stdHTTPPatch(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	spec, err := buildMethodSpec("PATCH", span, args, true)
	if err != nil {
		return grabrt.Value{}, err
	}
	return executeHTTPRequest(ctx, span, spec)
}

func stdHTTPRequest(ctx *execContext, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	optVal := args[0]
	if optVal.Kind() != grabrt.RecordKind && optVal.Kind() != grabrt.MapKind {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
	}
	spec := httpRequestSpec{
		headers:   make(map[string]string),
		timeoutMS: defaultHTTPTimeoutMS,
		retries:   0,
	}
	hasMethod := false
	hasURL := false

	for _, entry := range optVal.Entries() {
		switch entry.Name {
		case "method":
			if entry.Value.Kind() != grabrt.TextKind {
				return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
			}
			spec.method = strings.ToUpper(strings.TrimSpace(entry.Value.TextValue()))
			hasMethod = true
		case "url":
			if entry.Value.Kind() != grabrt.TextKind {
				return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span}
			}
			spec.url = entry.Value.TextValue()
			hasURL = true
		case "body":
			spec.body = entry.Value
			spec.hasBody = true
		case "headers", "timeout_ms", "retries":
			if err := applyOptionEntry(&spec, entry, span); err != nil {
				return grabrt.Value{}, err
			}
		default:
			return grabrt.Value{}, &Error{
				Kind: StandardLibraryError,
				Span: span,
				Err:  fmt.Errorf("unknown http option %q", entry.Name),
			}
		}
	}
	if !hasMethod || !hasURL {
		return grabrt.Value{}, &Error{
			Kind: StandardLibraryError,
			Span: span,
			Err:  fmt.Errorf("http.request requires both 'method' and 'url' fields"),
		}
	}
	switch spec.method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD":
	default:
		return grabrt.Value{}, &Error{
			Kind: StandardLibraryError,
			Span: span,
			Err:  fmt.Errorf("unsupported HTTP method %q", spec.method),
		}
	}
	return executeHTTPRequest(ctx, span, spec)
}

func buildMethodSpec(method string, span lexer.Span, args []grabrt.Value, withBody bool) (httpRequestSpec, error) {
	if args[0].Kind() != grabrt.TextKind {
		return httpRequestSpec{}, &Error{Kind: TypeMismatch, Span: span}
	}
	spec := httpRequestSpec{
		method:    method,
		url:       args[0].TextValue(),
		headers:   make(map[string]string),
		timeoutMS: defaultHTTPTimeoutMS,
		retries:   0,
	}
	optIndex := 1
	if withBody {
		spec.body = args[1]
		spec.hasBody = true
		optIndex = 2
	}
	if len(args) > optIndex {
		optVal := args[optIndex]
		if optVal.Kind() != grabrt.RecordKind && optVal.Kind() != grabrt.MapKind {
			return httpRequestSpec{}, &Error{Kind: TypeMismatch, Span: span}
		}
		for _, entry := range optVal.Entries() {
			if err := applyOptionEntry(&spec, entry, span); err != nil {
				return httpRequestSpec{}, err
			}
		}
	}
	return spec, nil
}

func applyOptionEntry(spec *httpRequestSpec, entry grabrt.Entry, span lexer.Span) error {
	switch entry.Name {
	case "headers":
		if entry.Value.Kind() != grabrt.RecordKind && entry.Value.Kind() != grabrt.MapKind {
			return &Error{Kind: TypeMismatch, Span: span}
		}
		for _, h := range entry.Value.Entries() {
			if h.Value.Kind() != grabrt.TextKind {
				return &Error{Kind: TypeMismatch, Span: span}
			}
			if strings.ContainsAny(h.Name, "\r\n") || strings.ContainsAny(h.Value.TextValue(), "\r\n") {
				return &Error{
					Kind: StandardLibraryError,
					Span: span,
					Err:  fmt.Errorf("invalid newline in HTTP header"),
				}
			}
			spec.headers[h.Name] = h.Value.TextValue()
		}
		return nil
	case "timeout_ms":
		if entry.Value.Kind() != grabrt.IntegerKind {
			return &Error{Kind: TypeMismatch, Span: span}
		}
		bigT := entry.Value.IntegerValue()
		if !bigT.IsInt64() || bigT.Int64() < 1 || bigT.Int64() > maxHTTPTimeoutMS {
			return &Error{
				Kind: StandardLibraryError,
				Span: span,
				Err:  fmt.Errorf("timeout_ms must be between 1 and %d", maxHTTPTimeoutMS),
			}
		}
		spec.timeoutMS = int(bigT.Int64())
		return nil
	case "retries":
		if entry.Value.Kind() != grabrt.IntegerKind {
			return &Error{Kind: TypeMismatch, Span: span}
		}
		bigR := entry.Value.IntegerValue()
		if !bigR.IsInt64() || bigR.Int64() < 0 || bigR.Int64() > maxHTTPRetries {
			return &Error{
				Kind: StandardLibraryError,
				Span: span,
				Err:  fmt.Errorf("retries must be between 0 and %d", maxHTTPRetries),
			}
		}
		spec.retries = int(bigR.Int64())
		return nil
	default:
		return &Error{
			Kind: StandardLibraryError,
			Span: span,
			Err:  fmt.Errorf("unknown http option %q", entry.Name),
		}
	}
}

func encodeRequestBody(spec httpRequestSpec, span lexer.Span) ([]byte, string, error) {
	if !spec.hasBody || spec.body.Kind() == grabrt.NothingKind {
		return nil, "", nil
	}
	switch spec.body.Kind() {
	case grabrt.TextKind:
		return []byte(spec.body.TextValue()), "text/plain; charset=utf-8", nil
	case grabrt.BytesKind:
		return spec.body.BytesValue(), "application/octet-stream", nil
	case grabrt.RecordKind, grabrt.MapKind, grabrt.ListKind:
		jsonStr, err := encodeJSONValue(spec.body, "", 0)
		if err != nil {
			return nil, "", &Error{Kind: StandardLibraryError, Span: span, Err: err}
		}
		return []byte(jsonStr), "application/json", nil
	default:
		return nil, "", &Error{Kind: TypeMismatch, Span: span}
	}
}

func executeHTTPRequest(ctx *execContext, span lexer.Span, spec httpRequestSpec) (grabrt.Value, error) {
	parsedURL, allowLocalInitial, err := ctx.permissions.checkNetworkURL(spec.url)
	if err != nil {
		if errors.Is(err, errNetworkPermissionDenied) {
			return grabrt.Value{}, &Error{Kind: NetworkPermissionDenied, Span: span, Err: err}
		}
		return grabrt.Value{}, &Error{Kind: NetworkError, Span: span, Err: err}
	}

	bodyBytes, defaultContentType, err := encodeRequestBody(spec, span)
	if err != nil {
		return grabrt.Value{}, err
	}

	var mu sync.Mutex
	currentAllowLocal := allowLocalInitial

	dialer := &net.Dialer{Timeout: time.Duration(spec.timeoutMS) * time.Millisecond}
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		DialContext: func(dialCtx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			mu.Lock()
			allowLocal := currentAllowLocal
			mu.Unlock()

			var ipAddrs []net.IPAddr
			if ip := net.ParseIP(host); ip != nil {
				ipAddrs = []net.IPAddr{{IP: ip}}
			} else {
				resolved, err := net.DefaultResolver.LookupIPAddr(dialCtx, host)
				if err != nil {
					return nil, err
				}
				ipAddrs = resolved
			}
			if len(ipAddrs) == 0 {
				return nil, fmt.Errorf("no IP addresses resolved for %q", host)
			}
			for _, ipAddr := range ipAddrs {
				if isBlockedSSRFIP(ipAddr.IP, allowLocal) {
					return nil, errNetworkPermissionDenied
				}
			}
			targetAddr := net.JoinHostPort(ipAddrs[0].IP.String(), port)
			if ctx.dialContext != nil {
				return ctx.dialContext(dialCtx, network, targetAddr)
			}
			return dialer.DialContext(dialCtx, network, targetAddr)
		},
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxHTTPRedirects {
				return fmt.Errorf("stopped after %d redirects", maxHTTPRedirects)
			}
			_, allowLocalHop, err := ctx.permissions.checkNetworkURL(req.URL.String())
			if err != nil {
				return err
			}
			mu.Lock()
			currentAllowLocal = allowLocalHop
			mu.Unlock()

			if len(via) > 0 && !strings.EqualFold(req.URL.Host, via[len(via)-1].URL.Host) {
				for _, h := range sensitiveRedirectHeaders {
					req.Header.Del(h)
				}
			}
			return nil
		},
	}

	parentCtx := ctx.context()
	reqCtx, cancel := context.WithTimeout(parentCtx, time.Duration(spec.timeoutMS)*time.Millisecond)
	defer cancel()

	backoffs := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}

	for attempt := 0; attempt <= spec.retries; attempt++ {
		if attempt > 0 {
			delay := backoffs[attempt-1]
			timer := time.NewTimer(delay)
			select {
			case <-reqCtx.Done():
				timer.Stop()
				return grabrt.Value{}, classifyHTTPError(reqCtx.Err(), parentCtx, span)
			case <-timer.C:
			}
		}

		mu.Lock()
		currentAllowLocal = allowLocalInitial
		mu.Unlock()

		var bodyReader io.Reader
		if len(bodyBytes) > 0 {
			bodyReader = bytes.NewReader(bodyBytes)
		}
		req, err := http.NewRequestWithContext(reqCtx, spec.method, parsedURL.String(), bodyReader)
		if err != nil {
			return grabrt.Value{}, &Error{Kind: NetworkError, Span: span, Err: err}
		}

		hasContentType := false
		for k, v := range spec.headers {
			if strings.EqualFold(k, "Content-Type") {
				hasContentType = true
			}
			req.Header.Set(k, v)
		}
		if defaultContentType != "" && !hasContentType {
			req.Header.Set("Content-Type", defaultContentType)
		}

		resp, err := client.Do(req)
		if err != nil {
			if errors.Is(err, errNetworkPermissionDenied) {
				return grabrt.Value{}, &Error{Kind: NetworkPermissionDenied, Span: span, Err: errNetworkPermissionDenied}
			}
			if attempt < spec.retries && reqCtx.Err() == nil {
				continue
			}
			return grabrt.Value{}, classifyHTTPError(err, parentCtx, span)
		}

		rawBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxFileSizeBytes+1))
		_ = resp.Body.Close()
		if readErr != nil {
			if attempt < spec.retries && reqCtx.Err() == nil {
				continue
			}
			return grabrt.Value{}, classifyHTTPError(readErr, parentCtx, span)
		}
		if len(rawBody) > maxFileSizeBytes {
			return grabrt.Value{}, &Error{
				Kind: NetworkError,
				Span: span,
				Err:  fmt.Errorf("HTTP response body exceeds 10 MiB limit"),
			}
		}
		if !utf8.Valid(rawBody) {
			return grabrt.Value{}, &Error{
				Kind: NetworkError,
				Span: span,
				Err:  fmt.Errorf("HTTP response body is not valid UTF-8 text"),
			}
		}

		if attempt < spec.retries && shouldRetryStatus(spec.method, resp.StatusCode) {
			continue
		}

		headerKeys := make([]string, 0, len(resp.Header))
		headerMap := make(map[string]string, len(resp.Header))
		for k, vals := range resp.Header {
			lk := strings.ToLower(k)
			headerKeys = append(headerKeys, lk)
			headerMap[lk] = strings.Join(vals, ", ")
		}
		sort.Strings(headerKeys)
		headerEntries := make([]grabrt.Entry, len(headerKeys))
		for i, k := range headerKeys {
			headerEntries[i] = grabrt.Entry{Name: k, Value: grabrt.Text(headerMap[k])}
		}

		finalURL := parsedURL.String()
		if resp.Request != nil && resp.Request.URL != nil {
			finalURL = resp.Request.URL.String()
		}

		return grabrt.Record([]grabrt.Entry{
			{Name: "status", Value: grabrt.IntegerFromInt64(int64(resp.StatusCode))},
			{Name: "ok", Value: grabrt.Boolean(resp.StatusCode >= 200 && resp.StatusCode <= 299)},
			{Name: "headers", Value: grabrt.Map(headerEntries)},
			{Name: "body", Value: grabrt.Text(string(rawBody))},
			{Name: "url", Value: grabrt.Text(finalURL)},
		}), nil
	}

	return grabrt.Value{}, &Error{Kind: NetworkError, Span: span}
}

func shouldRetryStatus(method string, status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return true
	case http.StatusBadGateway, http.StatusGatewayTimeout:
		return method == "GET" || method == "HEAD" || method == "PUT" || method == "DELETE"
	default:
		return false
	}
}

func classifyHTTPError(err error, parentCtx context.Context, span lexer.Span) error {
	if errors.Is(err, errNetworkPermissionDenied) {
		return &Error{Kind: NetworkPermissionDenied, Span: span, Err: errNetworkPermissionDenied}
	}
	if errors.Is(err, context.DeadlineExceeded) && parentCtx.Err() == nil {
		return &Error{Kind: RequestTimeout, Span: span, Err: fmt.Errorf("request timed out")}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() && parentCtx.Err() == nil {
		return &Error{Kind: RequestTimeout, Span: span, Err: fmt.Errorf("request timed out")}
	}
	return &Error{Kind: NetworkError, Span: span, Err: err}
}
