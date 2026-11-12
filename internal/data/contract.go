package data

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	rt "grab/internal/runtime"
)

// TypeSpec is an explicit, validated transport contract, independent of drivers.
type TypeSpec struct {
	Type             string
	Nullable         bool
	Precision, Scale int
	Zone             string
	FractionDigits   int
	Storage          string
	Min, Max         *big.Int
}

type Field struct {
	Name string
	Spec TypeSpec
}
type Contract struct{ Params, Columns []Field }

func ContractFromValue(v rt.Value) (Contract, error) {
	var c Contract
	if v.Kind() != rt.RecordKind || len(v.Entries()) != 2 {
		return c, fail(Invalid, "contract requires params and columns records")
	}
	p, pok := v.LookupEntry("params")
	cols, cok := v.LookupEntry("columns")
	if !pok || !cok {
		return c, fail(Invalid, "contract requires params and columns records")
	}
	var err error
	if c.Params, err = fieldsFromValue(p); err != nil {
		return c, err
	}
	if c.Columns, err = fieldsFromValue(cols); err != nil {
		return c, err
	}
	return c, nil
}

func fieldsFromValue(v rt.Value) ([]Field, error) {
	if v.Kind() != rt.RecordKind {
		return nil, fail(Invalid, "contract fields must be a record")
	}
	var fields []Field
	seen := map[string]bool{}
	for _, e := range v.Entries() {
		if !Identifier(e.Name) || seen[e.Name] {
			return nil, fail(Invalid, "invalid or duplicate contract field")
		}
		seen[e.Name] = true
		s, err := typeFromValue(e.Value)
		if err != nil {
			return nil, err
		}
		fields = append(fields, Field{e.Name, s})
	}
	return fields, nil
}

func typeFromValue(v rt.Value) (TypeSpec, error) {
	var s TypeSpec
	bad := func() (TypeSpec, error) { return TypeSpec{}, fail(Invalid, "invalid database type descriptor") }
	values := map[string]rt.Value{}
	if v.Kind() == rt.TextKind {
		s.Type = v.TextValue()
	} else if v.Kind() == rt.RecordKind {
		for _, e := range v.Entries() {
			if _, ok := values[e.Name]; ok {
				return bad()
			}
			values[e.Name] = e.Value
		}
		t, ok := values["type"]
		if !ok || t.Kind() != rt.TextKind {
			return bad()
		}
		s.Type = t.TextValue()
	} else {
		return bad()
	}
	allowed := map[string]bool{"type": true, "nullable": true}
	switch s.Type {
	case "integer":
		allowed["min"] = true
		allowed["max"] = true
	case "decimal":
		allowed["precision"] = true
		allowed["scale"] = true
		allowed["storage"] = true
	case "timestamp":
		allowed["zone"] = true
		allowed["fraction_digits"] = true
	case "text", "boolean", "bytes", "float":
	default:
		return bad()
	}
	for k := range values {
		if !allowed[k] {
			return bad()
		}
	}
	if n, ok := values["nullable"]; ok {
		if n.Kind() != rt.BooleanKind {
			return bad()
		}
		s.Nullable = n.BooleanValue()
	}
	intField := func(key string, min, max int) (int, bool) {
		v, ok := values[key]
		if !ok || v.Kind() != rt.IntegerKind {
			return 0, false
		}
		n := v.IntegerValue()
		if !n.IsInt64() || n.Int64() < int64(min) || n.Int64() > int64(max) {
			return 0, false
		}
		return int(n.Int64()), true
	}
	if s.Type == "decimal" {
		var ok bool
		// A bounded descriptor also bounds big.Int work. Precision cannot exceed
		// the specification's maximum possible per-value payload (10 MiB).
		if s.Precision, ok = intField("precision", 1, 10<<20); !ok {
			return bad()
		}
		if s.Scale, ok = intField("scale", 0, s.Precision); !ok {
			return bad()
		}
		if v, exists := values["storage"]; exists {
			if v.Kind() != rt.TextKind || v.TextValue() != "text" {
				return bad()
			}
			s.Storage = "text"
		}
	}
	if s.Type == "timestamp" {
		v, ok := values["zone"]
		if !ok || v.Kind() != rt.TextKind || v.TextValue() != "utc" && v.TextValue() != "naive" {
			return bad()
		}
		s.Zone = v.TextValue()
		if s.FractionDigits, ok = intField("fraction_digits", 0, 9); !ok {
			return bad()
		}
	}
	for _, key := range []string{"min", "max"} {
		if v, ok := values[key]; ok {
			if v.Kind() != rt.IntegerKind {
				return bad()
			}
			n := v.IntegerValue()
			if key == "min" {
				s.Min = n
			} else {
				s.Max = n
			}
		}
	}
	if s.Min != nil && s.Max != nil && s.Min.Cmp(s.Max) > 0 {
		return bad()
	}
	return s, nil
}

func (c Contract) ValidateSQL(s SQL, dialect string) error {
	if len(c.Params) != len(s.Parameters) {
		return fail(Invalid, "parameter contract does not match SQL")
	}
	names := map[string]bool{}
	for _, f := range c.Params {
		names[f.Name] = true
	}
	for _, name := range s.Parameters {
		if !names[name] {
			return fail(Invalid, "parameter contract does not match SQL")
		}
	}
	for _, fields := range [][]Field{c.Params, c.Columns} {
		for _, f := range fields {
			if dialect != "" && f.Spec.Type == "decimal" && (dialect == "sqlite" && f.Spec.Storage != "text" || dialect != "sqlite" && f.Spec.Storage != "") {
				return fail(Invalid, "decimal storage contract does not match driver")
			}
		}
	}
	return nil
}

// Bind returns only database/sql-supported primitive values; no containers,
// handles, implicit coercions, or approximate currency conversions are allowed.
func (s TypeSpec) Bind(v rt.Value, dialect string) (any, error) {
	bad := func() (any, error) { return nil, fail(Type, "database parameter does not match its type contract") }
	if v.Kind() == rt.NothingKind {
		if s.Nullable {
			return nil, nil
		}
		return bad()
	}
	switch s.Type {
	case "integer":
		if v.Kind() != rt.IntegerKind {
			return bad()
		}
		n := v.IntegerValue()
		if s.Min != nil && n.Cmp(s.Min) < 0 || s.Max != nil && n.Cmp(s.Max) > 0 {
			return bad()
		}
		if dialect == "sqlite" {
			if !n.IsInt64() {
				return bad()
			}
			return n.Int64(), nil
		}
		return n.String(), nil
	case "decimal":
		if v.Kind() != rt.DecimalKind {
			return bad()
		}
		d := v.DecimalValue()
		text, err := s.decimalText(d.Coefficient, d.Scale)
		if err != nil {
			return nil, err
		}
		return text, nil
	case "text":
		if v.Kind() != rt.TextKind || !utf8.ValidString(v.TextValue()) {
			return bad()
		}
		return v.TextValue(), nil
	case "bytes":
		if v.Kind() != rt.BytesKind {
			return bad()
		}
		b := v.BytesValue()
		if b == nil {
			b = []byte{}
		}
		return b, nil
	case "boolean":
		if v.Kind() != rt.BooleanKind {
			return bad()
		}
		if dialect == "postgres" {
			return v.BooleanValue(), nil
		}
		if v.BooleanValue() {
			return int64(1), nil
		}
		return int64(0), nil
	case "float":
		if v.Kind() != rt.FloatKind || math.IsNaN(v.FloatValue()) || math.IsInf(v.FloatValue(), 0) {
			return bad()
		}
		return v.FloatValue(), nil
	case "timestamp":
		if v.Kind() != rt.TextKind {
			return bad()
		}
		if dialect != "sqlite" && s.FractionDigits > 6 {
			return bad()
		}
		canonical, e := s.timestamp(v.TextValue())
		if e != nil {
			return nil, e
		}
		if dialect == "mariadb" {
			canonical = strings.Replace(canonical, "T", " ", 1)
			canonical = strings.TrimSuffix(canonical, "Z")
		}
		return canonical, nil
	}
	return bad()
}

func (s TypeSpec) decimalText(coefficient big.Int, scale int) (string, error) {
	// Reject precision/scale overflow before exponentiation to bound work.
	digits := len(new(big.Int).Abs(&coefficient).String())
	if scale > s.Scale {
		remove := scale - s.Scale
		if remove > digits {
			if coefficient.Sign() != 0 {
				return "", fail(Type, "decimal would require rounding")
			}
			coefficient.SetInt64(0)
		} else {
			power := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(remove)), nil)
			q, r := new(big.Int).QuoRem(&coefficient, power, new(big.Int))
			if r.Sign() != 0 {
				return "", fail(Type, "decimal would require rounding")
			}
			coefficient.Set(q)
		}
	} else if scale < s.Scale {
		if coefficient.Sign() != 0 && digits+s.Scale-scale > s.Precision {
			return "", fail(Type, "decimal precision exceeded")
		}
		coefficient.Mul(&coefficient, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(s.Scale-scale)), nil))
	}
	abs := new(big.Int).Abs(&coefficient).String()
	if len(abs) > s.Precision {
		return "", fail(Type, "decimal precision exceeded")
	}
	if s.Scale > 0 {
		if len(abs) <= s.Scale {
			abs = strings.Repeat("0", s.Scale-len(abs)+1) + abs
		}
		abs = abs[:len(abs)-s.Scale] + "." + abs[len(abs)-s.Scale:]
	}
	if coefficient.Sign() < 0 {
		abs = "-" + abs
	}
	return abs, nil
}

func (s TypeSpec) timestamp(raw string) (string, error) {
	layout := "2006-01-02T15:04:05"
	parseLayout := layout
	if s.Zone == "utc" {
		parseLayout = time.RFC3339Nano
	}
	t, err := time.Parse(parseLayout, raw)
	if err != nil || t.Year() < 1 || t.Year() > 9999 {
		return "", fail(Type, "invalid database timestamp")
	}
	power := int(math.Pow10(9 - s.FractionDigits))
	if t.Nanosecond()%power != 0 {
		return "", fail(Type, "timestamp precision would be lost")
	}
	if s.FractionDigits > 0 {
		layout += "." + strings.Repeat("0", s.FractionDigits)
	}
	if s.Zone == "utc" {
		t = t.UTC()
		layout += "Z"
	}
	return t.Format(layout), nil
}

// Decode accepts the actual storage value, preventing SQLite NUMERIC/REAL
// affinity from silently masquerading as exact TEXT-backed currency.
func (s TypeSpec) Decode(raw any, dialect string) (rt.Value, error) {
	bad := func() (rt.Value, error) {
		return rt.Value{}, fail(Type, "database result does not match its type contract")
	}
	if raw == nil {
		if s.Nullable {
			return rt.Nothing(), nil
		}
		return bad()
	}
	text := func() (string, bool) {
		switch v := raw.(type) {
		case string:
			return v, true
		case []byte:
			if dialect != "sqlite" {
				return string(v), true
			}
		}
		return "", false
	}
	switch s.Type {
	case "integer":
		var str string
		switch v := raw.(type) {
		case int64:
			str = strconv.FormatInt(v, 10)
		case uint64:
			str = strconv.FormatUint(v, 10)
		default:
			var ok bool
			str, ok = text()
			if !ok || dialect == "sqlite" {
				return bad()
			}
		}
		if len(str) == 0 {
			return bad()
		}
		for i, c := range []byte(str) {
			if c == '-' && i == 0 {
				continue
			}
			if c < '0' || c > '9' {
				return bad()
			}
		}
		v, err := rt.IntegerFromString(str)
		if err != nil {
			return bad()
		}
		if _, err = s.Bind(v, dialect); err != nil {
			return bad()
		}
		return v, nil
	case "decimal":
		str, ok := text()
		if !ok {
			return bad()
		}
		parts := strings.Split(str, ".")
		if len(parts) > 2 || len(parts) == 0 || parts[0] == "" {
			return bad()
		}
		scale := 0
		digits := parts[0]
		if len(parts) == 2 {
			if len(parts[1]) == 0 {
				return bad()
			}
			scale = len(parts[1])
			digits += parts[1]
		}
		for i, c := range []byte(digits) {
			if c == '-' && i == 0 {
				continue
			}
			if c < '0' || c > '9' {
				return bad()
			}
		}
		n, ok := new(big.Int).SetString(digits, 10)
		if !ok {
			return bad()
		}
		canonical, err := s.decimalText(*n, scale)
		if err != nil {
			return bad()
		}
		if s.Scale == 0 {
			integer, ok := new(big.Int).SetString(canonical, 10)
			if !ok {
				return bad()
			}
			return rt.DecimalFromBig(integer, 0), nil
		}
		v, err := rt.DecimalFromString(canonical)
		if err != nil {
			return bad()
		}
		return v, nil
	case "text":
		str, ok := text()
		if !ok || !utf8.ValidString(str) {
			return bad()
		}
		return rt.Text(str), nil
	case "bytes":
		b, ok := raw.([]byte)
		if !ok {
			return bad()
		}
		return rt.Bytes(b), nil
	case "boolean":
		if v, ok := raw.(bool); ok && dialect == "postgres" {
			return rt.Boolean(v), nil
		}
		if v, ok := raw.(int64); ok && dialect != "postgres" && (v == 0 || v == 1) {
			return rt.Boolean(v == 1), nil
		}
		return bad()
	case "float":
		f, ok := raw.(float64)
		if !ok {
			return bad()
		}
		v, err := rt.FloatFromFloat64(f)
		if err != nil {
			return bad()
		}
		return v, nil
	case "timestamp":
		str, ok := text()
		if !ok {
			if t, yes := raw.(time.Time); yes && dialect != "sqlite" {
				if s.Zone == "utc" {
					str = t.Format(time.RFC3339Nano)
				} else {
					str = t.Format("2006-01-02T15:04:05.999999999")
				}
				ok = true
			}
		}
		if !ok {
			return bad()
		}
		if dialect == "mariadb" {
			str = strings.Replace(str, " ", "T", 1)
			if s.Zone == "utc" {
				str += "Z"
			}
		}
		canonical, err := s.timestamp(str)
		if err != nil {
			return bad()
		}
		return rt.Text(canonical), nil
	}
	return bad()
}

func (s TypeSpec) String() string { return fmt.Sprintf("%s", s.Type) }
