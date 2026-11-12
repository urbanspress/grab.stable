// Package contracts implements the public version-1 exact JSON wire format.
// It never resolves secrets, fetches schemas, or executes application code.
package contracts

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"grab/internal/platform"
	rt "grab/internal/runtime"
)

var ErrContract = errors.New("GRAB-E315: typed contract validation failed")
var ErrStatic = errors.New("GRAB-E210: invalid API contract")
var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
var integer = regexp.MustCompile(`^(0|-[1-9][0-9]*|[1-9][0-9]*)$`)
var decimal = regexp.MustCompile(`^-?(0|[1-9][0-9]*)\.[0-9]+$`)
var timestamp = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.[0-9]{1,9})?Z$`)

type Schema struct {
	Type      string  `json:"type"`
	Ref       string  `json:"ref,omitempty"`
	Nullable  bool    `json:"nullable,omitempty"`
	MaxLength int     `json:"max_length,omitempty"`
	MaxCount  int     `json:"max_count,omitempty"`
	Precision int     `json:"precision,omitempty"`
	Scale     *int    `json:"scale,omitempty"`
	Minimum   string  `json:"minimum,omitempty"`
	Maximum   string  `json:"maximum,omitempty"`
	Items     *Schema `json:"items,omitempty"`
	Fields    []Field `json:"fields,omitempty"`
}
type Field struct {
	Name     string `json:"name"`
	Schema   Schema `json:"schema"`
	Optional bool   `json:"optional,omitempty"`
}
type Success struct {
	Status int    `json:"status"`
	Schema Schema `json:"schema"`
}
type Operation struct {
	ID         string    `json:"id"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	PathFields []Field   `json:"path_fields,omitempty"`
	Query      []Field   `json:"query,omitempty"`
	Body       *Schema   `json:"body,omitempty"`
	Success    []Success `json:"success"`
	Errors     []int     `json:"errors,omitempty"`
}
type Document struct {
	Version    int               `json:"version"`
	Types      map[string]Schema `json:"types"`
	Operations []Operation       `json:"operations"`
}

func Load(path string) (*Document, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	return Parse(b)
}
func Parse(b []byte) (*Document, error) {
	var d Document
	if len(b) > 1<<20 || platform.StrictJSON(b, &d) != nil || d.Version != 1 || len(d.Operations) > 1000 || len(d.Types) > 1000 {
		return nil, ErrStatic
	}
	for n, s := range d.Types {
		if !identifier.MatchString(n) || d.check(s, nil, 0) != nil {
			return nil, ErrStatic
		}
	}
	ids, routes := map[string]bool{}, map[string]bool{}
	for _, o := range d.Operations {
		if !identifier.MatchString(o.ID) || ids[o.ID] || routes[o.Method+" "+o.Path] || !strings.HasPrefix(o.Path, "/") || strings.ContainsAny(o.Path, "?#\\\x00\r\n ") {
			return nil, ErrStatic
		}
		switch o.Method {
		case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE":
		default:
			return nil, ErrStatic
		}
		ids[o.ID] = true
		routes[o.Method+" "+o.Path] = true
		if d.fields(o.PathFields, nil, 0) != nil || d.fields(o.Query, nil, 0) != nil {
			return nil, ErrStatic
		}
		parts := map[string]bool{}
		for _, p := range strings.Split(o.Path, "/") {
			if p == "." || p == ".." {
				return nil, ErrStatic
			}
			if strings.ContainsAny(p, "{}") {
				if len(p) < 3 || p[0] != '{' || p[len(p)-1] != '}' || !identifier.MatchString(p[1:len(p)-1]) || parts[p[1:len(p)-1]] {
					return nil, ErrStatic
				}
				parts[p[1:len(p)-1]] = true
			}
		}
		for _, f := range o.PathFields {
			if !parts[f.Name] || f.Optional || !scalar(f.Schema) {
				return nil, ErrStatic
			}
			delete(parts, f.Name)
		}
		if len(parts) != 0 {
			return nil, ErrStatic
		}
		for _, f := range o.Query {
			if !scalar(f.Schema) {
				return nil, ErrStatic
			}
		}
		if o.Body != nil && d.check(*o.Body, nil, 0) != nil {
			return nil, ErrStatic
		}
		if len(o.Success) == 0 {
			return nil, ErrStatic
		}
		statuses := map[int]bool{}
		for _, r := range o.Success {
			if r.Status < 200 || r.Status > 299 || statuses[r.Status] || d.check(r.Schema, nil, 0) != nil {
				return nil, ErrStatic
			}
			statuses[r.Status] = true
		}
		for _, s := range o.Errors {
			if s < 400 || s > 599 || statuses[s] {
				return nil, ErrStatic
			}
			statuses[s] = true
		}
	}
	return &d, nil
}
func scalar(s Schema) bool {
	return s.Type != "record" && s.Type != "list" && s.Type != "ref" && s.Type != "nothing" && !s.Nullable
}
func (d *Document) fields(fs []Field, stack map[string]bool, depth int) error {
	seen := map[string]bool{}
	for _, f := range fs {
		if !identifier.MatchString(f.Name) || seen[f.Name] || d.check(f.Schema, stack, depth+1) != nil {
			return ErrStatic
		}
		seen[f.Name] = true
	}
	return nil
}
func (d *Document) check(s Schema, stack map[string]bool, depth int) error {
	if depth > 32 {
		return ErrStatic
	}
	if s.Type != "ref" && s.Ref != "" || s.Type != "list" && (s.Items != nil || s.MaxCount != 0) || s.Type != "record" && s.Fields != nil || s.Type != "text" && s.Type != "bytes" && s.MaxLength != 0 || s.Type != "decimal" && s.Scale != nil || s.Type != "decimal" && s.Type != "integer" && (s.Precision != 0 || s.Minimum != "" || s.Maximum != "") {
		return ErrStatic
	}
	switch s.Type {
	case "text", "bytes":
		if s.MaxLength < 1 || s.MaxLength > 1<<20 {
			return ErrStatic
		}
	case "integer", "decimal":
		if s.Precision < 0 || s.Precision > 10000 {
			return ErrStatic
		}
		if s.Scale != nil && (*s.Scale < 0 || *s.Scale > 10000) {
			return ErrStatic
		}
		for _, v := range []string{s.Minimum, s.Maximum} {
			if v != "" {
				if s.Type == "integer" && !integer.MatchString(v) || s.Type == "decimal" && !decimal.MatchString(v) {
					return ErrStatic
				}
			}
		}
		if s.Minimum != "" && s.Maximum != "" {
			a, _ := new(big.Rat).SetString(s.Minimum)
			b, _ := new(big.Rat).SetString(s.Maximum)
			if a.Cmp(b) > 0 {
				return ErrStatic
			}
		}
	case "boolean", "float", "timestamp", "nothing":
	case "list":
		if s.Items == nil || s.MaxCount < 1 || s.MaxCount > 10000 {
			return ErrStatic
		}
		return d.check(*s.Items, stack, depth+1)
	case "record":
		return d.fields(s.Fields, stack, depth)
	case "ref":
		if !identifier.MatchString(s.Ref) || stack[s.Ref] {
			return ErrStatic
		}
		next := map[string]bool{}
		for k, v := range stack {
			next[k] = v
		}
		next[s.Ref] = true
		t, ok := d.Types[s.Ref]
		if !ok {
			return ErrStatic
		}
		return d.check(t, next, depth+1)
	default:
		return ErrStatic
	}
	return nil
}
func (d *Document) Operation(id string) (Operation, bool) {
	if d != nil {
		for _, o := range d.Operations {
			if o.ID == id {
				return o, true
			}
		}
	}
	return Operation{}, false
}
func (d *Document) Route(method, path string) (Operation, bool) {
	if d != nil {
		for _, o := range d.Operations {
			if o.Method == method && o.Path == path {
				return o, true
			}
		}
	}
	return Operation{}, false
}
func (d *Document) Hash() string {
	b, _ := json.Marshal(d)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Decode rejects duplicate object members before any typed conversion.
func (d *Document) Decode(s Schema, b []byte) (rt.Value, error) {
	if len(b) > 1<<20 || !utf8.Valid(b) {
		return rt.Value{}, ErrContract
	}
	var x any
	if platform.StrictValueJSON(b, &x) != nil {
		return rt.Value{}, ErrContract
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if dec.Decode(&x) != nil {
		return rt.Value{}, ErrContract
	}
	n := 0
	return d.decode(s, x, 0, &n)
}
func (d *Document) decode(s Schema, x any, depth int, n *int) (rt.Value, error) {
	*n++
	if depth > 32 || *n > 10000 {
		return rt.Value{}, ErrContract
	}
	if x == nil {
		if s.Nullable || s.Type == "nothing" {
			return rt.Nothing(), nil
		}
		return rt.Value{}, ErrContract
	}
	if s.Type == "ref" {
		return d.decode(d.Types[s.Ref], x, depth+1, n)
	}
	str, isText := x.(string)
	switch s.Type {
	case "text":
		if isText && utf8.RuneCountInString(str) <= s.MaxLength {
			return rt.Text(str), nil
		}
	case "timestamp":
		if isText && timestamp.MatchString(str) {
			if _, e := time.Parse(time.RFC3339Nano, str); e == nil {
				return rt.Text(str), nil
			}
		}
	case "bytes":
		if isText {
			b, e := base64.StdEncoding.Strict().DecodeString(str)
			if e == nil && len(b) <= s.MaxLength && base64.StdEncoding.EncodeToString(b) == str {
				return rt.Bytes(b), nil
			}
		}
	case "boolean":
		if b, ok := x.(bool); ok {
			return rt.Boolean(b), nil
		}
	case "float":
		if f, ok := x.(json.Number); ok {
			v, e := strconv.ParseFloat(string(f), 64)
			if e == nil && !math.IsInf(v, 0) && !math.IsNaN(v) {
				return rt.FloatFromFloat64(v)
			}
		}
	case "integer", "decimal":
		if !isText || len(str) > 10002 {
			return rt.Value{}, ErrContract
		}
		if s.Type == "integer" && !integer.MatchString(str) || s.Type == "decimal" && !decimal.MatchString(str) {
			return rt.Value{}, ErrContract
		}
		digits := strings.ReplaceAll(strings.TrimPrefix(str, "-"), ".", "")
		if s.Precision > 0 && len(digits) > s.Precision {
			return rt.Value{}, ErrContract
		}
		if s.Scale != nil && len(strings.SplitN(str, ".", 2)[1]) != *s.Scale {
			return rt.Value{}, ErrContract
		}
		v, _ := new(big.Rat).SetString(str)
		for i, b := range []string{s.Minimum, s.Maximum} {
			if b != "" {
				lim, _ := new(big.Rat).SetString(b)
				if i == 0 && v.Cmp(lim) < 0 || i == 1 && v.Cmp(lim) > 0 {
					return rt.Value{}, ErrContract
				}
			}
		}
		if s.Type == "integer" {
			return rt.IntegerFromString(str)
		}
		return rt.DecimalFromString(str)
	case "list":
		if xs, ok := x.([]any); ok && len(xs) <= s.MaxCount {
			vs := make([]rt.Value, 0, len(xs))
			for _, x := range xs {
				v, e := d.decode(*s.Items, x, depth+1, n)
				if e != nil {
					return rt.Value{}, e
				}
				vs = append(vs, v)
			}
			return rt.List(vs), nil
		}
	case "record":
		if m, ok := x.(map[string]any); ok {
			var es []rt.Entry
			known := map[string]bool{}
			for _, f := range s.Fields {
				known[f.Name] = true
				x, ok := m[f.Name]
				if !ok {
					if f.Optional {
						continue
					}
					return rt.Value{}, ErrContract
				}
				v, e := d.decode(f.Schema, x, depth+1, n)
				if e != nil {
					return rt.Value{}, e
				}
				es = append(es, rt.Entry{Name: f.Name, Value: v})
			}
			for k := range m {
				if !known[k] {
					return rt.Value{}, ErrContract
				}
			}
			return rt.Record(es), nil
		}
	}
	return rt.Value{}, ErrContract
}
func (d *Document) Encode(s Schema, v rt.Value) ([]byte, error) {
	n := 0
	b, e := d.encode(s, v, 0, &n)
	if e != nil || len(b) > 1<<20 {
		return nil, ErrContract
	}
	return b, nil
}
func (d *Document) encode(s Schema, v rt.Value, depth int, n *int) ([]byte, error) {
	*n++
	if depth > 32 || *n > 10000 || v.ContainsResource() {
		return nil, ErrContract
	}
	if v.Kind() == rt.NothingKind && (s.Nullable || s.Type == "nothing") {
		return []byte("null"), nil
	}
	if s.Type == "ref" {
		return d.encode(d.Types[s.Ref], v, depth+1, n)
	}
	var x any
	switch s.Type {
	case "text", "timestamp":
		if v.Kind() != rt.TextKind {
			return nil, ErrContract
		}
		x = v.TextValue()
	case "bytes":
		if v.Kind() != rt.BytesKind {
			return nil, ErrContract
		}
		x = base64.StdEncoding.EncodeToString(v.BytesValue())
	case "integer":
		if v.Kind() != rt.IntegerKind {
			return nil, ErrContract
		}
		x = v.IntegerValue().String()
	case "decimal":
		if v.Kind() != rt.DecimalKind {
			return nil, ErrContract
		}
		x = v.Display()
	case "float":
		if v.Kind() != rt.FloatKind {
			return nil, ErrContract
		}
		x = v.FloatValue()
	case "boolean":
		if v.Kind() != rt.BooleanKind {
			return nil, ErrContract
		}
		x = v.BooleanValue()
	case "list":
		if v.Kind() != rt.ListKind || v.ListLen() > s.MaxCount {
			return nil, ErrContract
		}
		var b bytes.Buffer
		b.WriteByte('[')
		for i, a := range v.ListValues() {
			if i > 0 {
				b.WriteByte(',')
			}
			t, e := d.encode(*s.Items, a, depth+1, n)
			if e != nil {
				return nil, e
			}
			b.Write(t)
		}
		b.WriteByte(']')
		return b.Bytes(), nil
	case "record":
		if v.Kind() != rt.RecordKind && v.Kind() != rt.MapKind {
			return nil, ErrContract
		}
		var b bytes.Buffer
		b.WriteByte('{')
		known := map[string]bool{}
		count := 0
		for _, f := range s.Fields {
			known[f.Name] = true
			a, ok := v.LookupEntry(f.Name)
			if !ok {
				if f.Optional {
					continue
				}
				return nil, ErrContract
			}
			t, e := d.encode(f.Schema, a, depth+1, n)
			if e != nil {
				return nil, e
			}
			if count > 0 {
				b.WriteByte(',')
			}
			key, _ := json.Marshal(f.Name)
			b.Write(key)
			b.WriteByte(':')
			b.Write(t)
			count++
		}
		for _, e := range v.Entries() {
			if !known[e.Name] {
				return nil, ErrContract
			}
		}
		b.WriteByte('}')
		return b.Bytes(), nil
	default:
		return nil, ErrContract
	}
	b, e := json.Marshal(x)
	if e != nil {
		return nil, ErrContract
	}
	if _, e = d.Decode(s, b); e != nil {
		return nil, e
	}
	return b, nil
}
func (d *Document) Parameters(fields []Field, values rt.Value) (rt.Value, error) {
	var es []rt.Entry
	known := map[string]bool{}
	for _, f := range fields {
		known[f.Name] = true
		v, ok := values.LookupEntry(f.Name)
		if !ok {
			if f.Optional {
				continue
			}
			return rt.Value{}, ErrContract
		}
		if v.Kind() == rt.ListKind {
			if v.ListLen() != 1 {
				return rt.Value{}, ErrContract
			}
			v, _ = v.ListAt(0)
		}
		if v.Kind() != rt.TextKind {
			return rt.Value{}, ErrContract
		}
		var b []byte
		if f.Schema.Type == "boolean" || f.Schema.Type == "float" {
			b = []byte(v.TextValue())
		} else {
			b, _ = json.Marshal(v.TextValue())
		}
		a, e := d.Decode(f.Schema, b)
		if e != nil {
			return rt.Value{}, e
		}
		es = append(es, rt.Entry{Name: f.Name, Value: a})
	}
	for _, e := range values.Entries() {
		if !known[e.Name] {
			return rt.Value{}, ErrContract
		}
	}
	return rt.Record(es), nil
}
func (d *Document) Input(o Operation, request rt.Value) (rt.Value, error) {
	p, _ := request.LookupEntry("params")
	q, _ := request.LookupEntry("query")
	b, _ := request.LookupEntry("body")
	path, e := d.Parameters(o.PathFields, p)
	if e != nil {
		return rt.Value{}, e
	}
	query, e := d.Parameters(o.Query, q)
	if e != nil {
		return rt.Value{}, e
	}
	body := rt.Nothing()
	if o.Body != nil {
		body, e = d.Decode(*o.Body, []byte(b.TextValue()))
		if e != nil {
			return rt.Value{}, e
		}
	} else if b.TextValue() != "" {
		return rt.Value{}, ErrContract
	}
	return rt.Record([]rt.Entry{{Name: "path", Value: path}, {Name: "query", Value: query}, {Name: "body", Value: body}}), nil
}
func (d *Document) Response(o Operation, status int, v rt.Value) ([]byte, error) {
	for _, r := range o.Success {
		if r.Status == status {
			return d.Encode(r.Schema, v)
		}
	}
	for _, s := range o.Errors {
		if s == status {
			return d.Encode(ErrorSchema(), v)
		}
	}
	return nil, ErrContract
}
func ErrorSchema() Schema {
	return Schema{Type: "record", Fields: []Field{{Name: "error", Schema: Schema{Type: "record", Fields: []Field{{Name: "code", Schema: Schema{Type: "text", MaxLength: 128}}, {Name: "message", Schema: Schema{Type: "text", MaxLength: 1024}}, {Name: "request_id", Schema: Schema{Type: "text", MaxLength: 128}}}}}}}
}
func ErrorBody(code, message, id string) []byte {
	b, _ := json.Marshal(map[string]any{"error": map[string]string{"code": code, "message": message, "request_id": id}})
	return b
}
func (d *Document) ValidateRoutes(routes map[string]bool) error {
	for _, o := range d.Operations {
		if !routes[o.Method+" "+o.Path] {
			return fmt.Errorf("%w: operation has no matching route", ErrStatic)
		}
	}
	return nil
}
