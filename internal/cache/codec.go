// Package cache implements bounded, generation-owned temporary stores. It never
// supplies authoritative business data or silently falls back between drivers.
package cache

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"unicode/utf8"

	"grab/internal/platform"
	rt "grab/internal/runtime"
)

var ErrValue = errors.New("GRAB-E314: invalid cache value or argument")
var ErrFull = errors.New("GRAB-E314: cache capacity exceeded")
var ErrUnavailable = errors.New("GRAB-E314: cache unavailable")
var ErrUncertain = errors.New("GRAB-E314: cache write outcome uncertain")
var ErrClosed = errors.New("GRAB-E314: cache generation closed")

const MaxValueBytes = 1 << 20
const MaxTTL = 604800000

type wire struct {
	Version int  `json:"version"`
	Value   node `json:"value"`
}
type node struct {
	Kind    rt.Kind `json:"kind"`
	Scalar  string  `json:"scalar,omitempty"`
	Scale   int     `json:"scale,omitempty"`
	Values  []node  `json:"values,omitempty"`
	Entries []pair  `json:"entries,omitempty"`
}
type pair struct {
	Name  string `json:"name"`
	Value node   `json:"value"`
}
type codecBudget struct{ elements, bytes int }

func (b *codecBudget) visit(depth, size int) error {
	if depth > 0 {
		b.elements++
	}
	b.bytes += size
	if depth > 32 || b.elements > 10000 || b.bytes > MaxValueBytes {
		return ErrValue
	}
	return nil
}

// Encode preserves kind, exact coefficient/scale, bytes and collection order.
func Encode(v rt.Value) ([]byte, error) {
	n, e := encodeNode(v, 0, &codecBudget{})
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(wire{1, n})
	if e != nil || len(raw) > MaxValueBytes {
		return nil, ErrValue
	}
	return raw, nil
}
func encodeNode(v rt.Value, depth int, b *codecBudget) (node, error) {
	n := node{Kind: v.Kind()}
	if e := b.visit(depth, 16); e != nil {
		return n, e
	}
	switch v.Kind() {
	case rt.NothingKind:
	case rt.BooleanKind:
		n.Scalar = strconv.FormatBool(v.BooleanValue())
	case rt.TextKind:
		n.Scalar = v.TextValue()
		if !utf8.ValidString(n.Scalar) {
			return n, ErrValue
		}
	case rt.IntegerKind:
		n.Scalar = v.IntegerValue().String()
	case rt.DecimalKind:
		d := v.DecimalValue()
		if d.Scale < 0 || d.Scale > MaxValueBytes {
			return n, ErrValue
		}
		n.Scalar = d.Coefficient.String()
		n.Scale = d.Scale
	case rt.FloatKind:
		n.Scalar = strconv.FormatFloat(v.FloatValue(), 'g', -1, 64)
	case rt.BytesKind:
		n.Scalar = base64.StdEncoding.EncodeToString(v.BytesValue())
	case rt.ListKind:
		for _, x := range v.ListValues() {
			child, e := encodeNode(x, depth+1, b)
			if e != nil {
				return n, e
			}
			n.Values = append(n.Values, child)
		}
	case rt.RecordKind, rt.MapKind:
		seen := map[string]bool{}
		for _, x := range v.Entries() {
			if seen[x.Name] || !utf8.ValidString(x.Name) {
				return n, ErrValue
			}
			seen[x.Name] = true
			b.bytes += len(x.Name)
			child, e := encodeNode(x.Value, depth+1, b)
			if e != nil {
				return n, e
			}
			n.Entries = append(n.Entries, pair{x.Name, child})
		}
	default:
		return n, ErrValue
	}
	b.bytes += len(n.Scalar)
	if b.bytes > MaxValueBytes {
		return n, ErrValue
	}
	return n, nil
}

func Decode(raw []byte) (rt.Value, error) {
	var w wire
	// The codec nesting includes pair/node wrappers; JSON's own decoder is safe
	// at this byte bound. Semantic depth is checked independently below.
	if len(raw) > MaxValueBytes || !utf8.Valid(raw) {
		return rt.Value{}, ErrValue
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&w) != nil || w.Version != 1 {
		return rt.Value{}, ErrValue
	}
	v, e := decodeNode(w.Value, 0, &codecBudget{})
	if e != nil {
		return rt.Value{}, ErrValue
	}
	// Exact canonical comparison rejects duplicates, ignored/wrong-kind fields,
	// trailing input, alternate numeric forms and unsupported version shapes.
	canonical, e := Encode(v)
	if e != nil || !bytes.Equal(canonical, raw) {
		return rt.Value{}, ErrValue
	}
	return v, nil
}
func decodeNode(n node, depth int, b *codecBudget) (rt.Value, error) {
	if b.visit(depth, len(n.Scalar)) != nil {
		return rt.Value{}, ErrValue
	}
	switch n.Kind {
	case rt.NothingKind:
		return rt.Nothing(), nil
	case rt.BooleanKind:
		v, e := strconv.ParseBool(n.Scalar)
		return rt.Boolean(v), e
	case rt.TextKind:
		return rt.Text(n.Scalar), nil
	case rt.IntegerKind:
		return rt.IntegerFromString(n.Scalar)
	case rt.DecimalKind:
		if n.Scale < 0 || n.Scale > MaxValueBytes {
			return rt.Value{}, ErrValue
		}
		c, ok := new(big.Int).SetString(n.Scalar, 10)
		if !ok {
			return rt.Value{}, ErrValue
		}
		return rt.DecimalFromBig(c, n.Scale), nil
	case rt.FloatKind:
		return rt.FloatFromString(n.Scalar)
	case rt.BytesKind:
		v, e := base64.StdEncoding.Strict().DecodeString(n.Scalar)
		return rt.Bytes(v), e
	case rt.ListKind:
		var values []rt.Value
		for _, x := range n.Values {
			v, e := decodeNode(x, depth+1, b)
			if e != nil {
				return rt.Value{}, ErrValue
			}
			values = append(values, v)
		}
		return rt.List(values), nil
	case rt.MapKind, rt.RecordKind:
		var entries []rt.Entry
		for _, x := range n.Entries {
			b.bytes += len(x.Name)
			v, e := decodeNode(x.Value, depth+1, b)
			if e != nil {
				return rt.Value{}, ErrValue
			}
			entries = append(entries, rt.Entry{Name: x.Name, Value: v})
		}
		if n.Kind == rt.MapKind {
			return rt.Map(entries), nil
		}
		return rt.Record(entries), nil
	default:
		return rt.Value{}, ErrValue
	}
}

// Keep permission errors separate from temporary failures for language bindings.
func Denied(err error) bool { return errors.Is(err, platform.ErrDenied) }
