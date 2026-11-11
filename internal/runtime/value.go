package runtime

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// ErrDivisionByZero is returned when dividing or taking modulo by zero.
var ErrDivisionByZero = errors.New("division by zero")

// ErrTypeMismatch is returned when an operation receives incompatible operand kinds.
var ErrTypeMismatch = errors.New("runtime type error")

// ErrNumericOverflow is returned when a binary64 float operation overflows or produces NaN.
var ErrNumericOverflow = errors.New("numeric overflow or invalid float operation")

// Kind identifies one of the portable GRAB 0.1 value kinds.
type Kind uint8

const (
	NothingKind Kind = iota
	BooleanKind
	TextKind
	IntegerKind
	DecimalKind
	FloatKind
	BytesKind
	ListKind
	MapKind
	RecordKind
)

// Decimal stores an exact base-10 coefficient and the number of fractional
// digits. Its zero value represents 0.
type Decimal struct {
	Coefficient big.Int
	Scale       int
}

// Entry is an ordered key/value pair used by maps and records.
type Entry struct {
	Name  string
	Value Value
}

// Value is an immutable GRAB value. Constructors copy mutable inputs and
// accessors return copies where needed.
type Value struct {
	kind    Kind
	boolean bool
	text    string
	integer big.Int
	decimal Decimal
	float   float64
	bytes   []byte
	values  []Value
	entries []Entry
}

func Nothing() Value           { return Value{kind: NothingKind} }
func Boolean(value bool) Value { return Value{kind: BooleanKind, boolean: value} }
func Text(value string) Value  { return Value{kind: TextKind, text: value} }

func IntegerFromString(value string) (Value, error) {
	integer, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return Value{}, fmt.Errorf("invalid integer %q", value)
	}
	result := Value{kind: IntegerKind}
	result.integer.Set(integer)
	return result, nil
}

func DecimalFromString(value string) (Value, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 || len(parts[1]) == 0 {
		return Value{}, fmt.Errorf("invalid decimal %q", value)
	}
	coefficientText := parts[0] + parts[1]
	coefficient, ok := new(big.Int).SetString(coefficientText, 10)
	if !ok {
		return Value{}, fmt.Errorf("invalid decimal %q", value)
	}
	result := Value{kind: DecimalKind}
	result.decimal.Coefficient.Set(coefficient)
	result.decimal.Scale = len(parts[1])
	return result, nil
}

func FloatFromString(value string) (Value, error) {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
		if err == nil {
			err = fmt.Errorf("non-finite binary64 value")
		}
		return Value{}, fmt.Errorf("invalid float %q: %w", value, err)
	}
	return Value{kind: FloatKind, float: parsed}, nil
}

func IntegerFromBig(value *big.Int) Value {
	result := Value{kind: IntegerKind}
	result.integer.Set(value)
	return result
}

func IntegerFromInt64(value int64) Value {
	result := Value{kind: IntegerKind}
	result.integer.SetInt64(value)
	return result
}

func DecimalFromBig(coefficient *big.Int, scale int) Value {
	if scale < 0 {
		scale = 0
	}
	result := Value{kind: DecimalKind}
	result.decimal.Coefficient.Set(coefficient)
	result.decimal.Scale = scale
	return result
}

func FloatFromFloat64(value float64) (Value, error) {
	if math.IsInf(value, 0) || math.IsNaN(value) {
		return Value{}, ErrNumericOverflow
	}
	return Value{kind: FloatKind, float: value}, nil
}

func Bytes(value []byte) Value { return Value{kind: BytesKind, bytes: append([]byte(nil), value...)} }
func List(values []Value) Value {
	return Value{kind: ListKind, values: append([]Value(nil), values...)}
}
func Map(entries []Entry) Value    { return Value{kind: MapKind, entries: cloneEntries(entries)} }
func Record(entries []Entry) Value { return Value{kind: RecordKind, entries: cloneEntries(entries)} }

func cloneEntries(entries []Entry) []Entry { return append([]Entry(nil), entries...) }

func (value Value) Kind() Kind             { return value.kind }
func (value Value) BooleanValue() bool     { return value.boolean }
func (value Value) TextValue() string      { return value.text }
func (value Value) IntegerValue() *big.Int { return new(big.Int).Set(&value.integer) }
func (value Value) DecimalValue() Decimal {
	var copy Decimal
	copy.Coefficient.Set(&value.decimal.Coefficient)
	copy.Scale = value.decimal.Scale
	return copy
}
func (value Value) FloatValue() float64 { return value.float }
func (value Value) BytesValue() []byte  { return append([]byte(nil), value.bytes...) }
func (value Value) ListValues() []Value { return append([]Value(nil), value.values...) }
func (value Value) Entries() []Entry    { return cloneEntries(value.entries) }

func (value Value) ListLen() int { return len(value.values) }

func (value Value) ListAt(index int) (Value, bool) {
	if index < 0 || index >= len(value.values) {
		return Value{}, false
	}
	return value.values[index], true
}

func (value Value) LookupEntry(name string) (Value, bool) {
	for _, entry := range value.entries {
		if entry.Name == name {
			return entry.Value, true
		}
	}
	return Value{}, false
}

// Equal reports whether two GRAB values are equal under the 'is' operator.
func (value Value) Equal(other Value) bool {
	if (value.kind == IntegerKind || value.kind == DecimalKind) &&
		(other.kind == IntegerKind || other.kind == DecimalKind) {
		return compareExactNumeric(value, other) == 0
	}
	if value.kind != other.kind {
		return false
	}
	switch value.kind {
	case NothingKind:
		return true
	case BooleanKind:
		return value.boolean == other.boolean
	case TextKind:
		return value.text == other.text
	case FloatKind:
		return value.float == other.float
	case BytesKind:
		return bytes.Equal(value.bytes, other.bytes)
	case ListKind:
		if len(value.values) != len(other.values) {
			return false
		}
		for i := range value.values {
			if !value.values[i].Equal(other.values[i]) {
				return false
			}
		}
		return true
	case MapKind, RecordKind:
		if len(value.entries) != len(other.entries) {
			return false
		}
		for _, entry := range value.entries {
			otherVal, ok := other.LookupEntry(entry.Name)
			if !ok || !entry.Value.Equal(otherVal) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// Compare compares two ordered GRAB values (numbers or text). It returns
// (-1, true), (0, true), or (1, true) when comparable, or (0, false) when
// the operand kinds cannot be ordered.
func (value Value) Compare(other Value) (int, bool) {
	if (value.kind == IntegerKind || value.kind == DecimalKind) &&
		(other.kind == IntegerKind || other.kind == DecimalKind) {
		return compareExactNumeric(value, other), true
	}
	if value.kind == FloatKind && other.kind == FloatKind {
		if value.float < other.float {
			return -1, true
		}
		if value.float > other.float {
			return 1, true
		}
		return 0, true
	}
	if value.kind == TextKind && other.kind == TextKind {
		return strings.Compare(value.text, other.text), true
	}
	return 0, false
}

// Add computes value + other per GRAB 0.3 numeric rules.
func Add(left, right Value) (Value, error) {
	if left.kind == IntegerKind && right.kind == IntegerKind {
		return IntegerFromBig(new(big.Int).Add(&left.integer, &right.integer)), nil
	}
	if (left.kind == IntegerKind || left.kind == DecimalKind) &&
		(right.kind == IntegerKind || right.kind == DecimalKind) {
		a, b, scale := alignExactNumeric(left, right)
		return DecimalFromBig(new(big.Int).Add(a, b), scale), nil
	}
	if left.kind == FloatKind && right.kind == FloatKind {
		return FloatFromFloat64(left.float + right.float)
	}
	return Value{}, ErrTypeMismatch
}

// Sub computes left - right per GRAB 0.3 numeric rules.
func Sub(left, right Value) (Value, error) {
	if left.kind == IntegerKind && right.kind == IntegerKind {
		return IntegerFromBig(new(big.Int).Sub(&left.integer, &right.integer)), nil
	}
	if (left.kind == IntegerKind || left.kind == DecimalKind) &&
		(right.kind == IntegerKind || right.kind == DecimalKind) {
		a, b, scale := alignExactNumeric(left, right)
		return DecimalFromBig(new(big.Int).Sub(a, b), scale), nil
	}
	if left.kind == FloatKind && right.kind == FloatKind {
		return FloatFromFloat64(left.float - right.float)
	}
	return Value{}, ErrTypeMismatch
}

// Mul computes left * right per GRAB 0.3 numeric rules.
func Mul(left, right Value) (Value, error) {
	if left.kind == IntegerKind && right.kind == IntegerKind {
		return IntegerFromBig(new(big.Int).Mul(&left.integer, &right.integer)), nil
	}
	if (left.kind == IntegerKind || left.kind == DecimalKind) &&
		(right.kind == IntegerKind || right.kind == DecimalKind) {
		da := asDecimal(left)
		db := asDecimal(right)
		prod := new(big.Int).Mul(&da.Coefficient, &db.Coefficient)
		return DecimalFromBig(prod, da.Scale+db.Scale), nil
	}
	if left.kind == FloatKind && right.kind == FloatKind {
		return FloatFromFloat64(left.float * right.float)
	}
	return Value{}, ErrTypeMismatch
}

// Div computes left / right per GRAB 0.3 numeric rules.
func Div(left, right Value) (Value, error) {
	if left.kind == IntegerKind && right.kind == IntegerKind {
		if right.integer.Sign() == 0 {
			return Value{}, ErrDivisionByZero
		}
		quo, rem := new(big.Int).QuoRem(&left.integer, &right.integer, new(big.Int))
		if rem.Sign() == 0 {
			return IntegerFromBig(quo), nil
		}
		return divideDecimals(asDecimal(left), asDecimal(right))
	}
	if (left.kind == IntegerKind || left.kind == DecimalKind) &&
		(right.kind == IntegerKind || right.kind == DecimalKind) {
		return divideDecimals(asDecimal(left), asDecimal(right))
	}
	if left.kind == FloatKind && right.kind == FloatKind {
		if right.float == 0 {
			return Value{}, ErrDivisionByZero
		}
		return FloatFromFloat64(left.float / right.float)
	}
	return Value{}, ErrTypeMismatch
}

// Mod computes left % right on integer values per GRAB 0.3 numeric rules.
func Mod(left, right Value) (Value, error) {
	if left.kind != IntegerKind || right.kind != IntegerKind {
		return Value{}, ErrTypeMismatch
	}
	if right.integer.Sign() == 0 {
		return Value{}, ErrDivisionByZero
	}
	return IntegerFromBig(new(big.Int).Rem(&left.integer, &right.integer)), nil
}

func asDecimal(value Value) Decimal {
	if value.kind == DecimalKind {
		return value.DecimalValue()
	}
	var d Decimal
	d.Coefficient.Set(&value.integer)
	d.Scale = 0
	return d
}

func pow10(n int) *big.Int {
	if n <= 0 {
		return big.NewInt(1)
	}
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
}

func alignExactNumeric(left, right Value) (*big.Int, *big.Int, int) {
	da := asDecimal(left)
	db := asDecimal(right)
	scale := da.Scale
	if db.Scale > scale {
		scale = db.Scale
	}
	a := new(big.Int).Set(&da.Coefficient)
	b := new(big.Int).Set(&db.Coefficient)
	if da.Scale < scale {
		a.Mul(a, pow10(scale-da.Scale))
	}
	if db.Scale < scale {
		b.Mul(b, pow10(scale-db.Scale))
	}
	return a, b, scale
}

func compareExactNumeric(left, right Value) int {
	a, b, _ := alignExactNumeric(left, right)
	return a.Cmp(b)
}

func divideDecimals(da, db Decimal) (Value, error) {
	if db.Coefficient.Sign() == 0 {
		return Value{}, ErrDivisionByZero
	}
	const precisionScale = 16
	minScale := da.Scale - db.Scale
	if minScale < 1 {
		minScale = 1
	}
	targetScale := precisionScale
	if minScale > targetScale {
		targetScale = minScale
	}

	num := new(big.Int).Abs(&da.Coefficient)
	den := new(big.Int).Abs(&db.Coefficient)
	shift := targetScale + db.Scale - da.Scale
	if shift > 0 {
		num.Mul(num, pow10(shift))
	} else if shift < 0 {
		den.Mul(den, pow10(-shift))
	}

	quo, rem := new(big.Int).QuoRem(num, den, new(big.Int))
	scale := targetScale
	ten := big.NewInt(10)
	zero := big.NewInt(0)

	if rem.Sign() == 0 {
		for scale > minScale && quo.Sign() > 0 {
			q, r := new(big.Int).QuoRem(quo, ten, new(big.Int))
			if r.Cmp(zero) != 0 {
				break
			}
			quo = q
			scale--
		}
	} else {
		twiceRem := new(big.Int).Mul(rem, big.NewInt(2))
		cmp := twiceRem.Cmp(den)
		if cmp > 0 || (cmp == 0 && quo.Bit(0) == 1) {
			quo.Add(quo, big.NewInt(1))
		}
	}

	if (da.Coefficient.Sign() < 0) != (db.Coefficient.Sign() < 0) && quo.Sign() != 0 {
		quo.Neg(quo)
	}
	return DecimalFromBig(quo, scale), nil
}

// Display returns the deterministic canonical representation specified for
// GRAB 0.1. A top-level text value is emitted without quotes.
func (value Value) Display() string { return value.display(false) }

func (value Value) display(nested bool) string {
	switch value.kind {
	case NothingKind:
		return "nothing"
	case BooleanKind:
		if value.boolean {
			return "yes"
		}
		return "no"
	case TextKind:
		if nested {
			return quoteText(value.text)
		}
		return value.text
	case IntegerKind:
		return value.integer.String()
	case DecimalKind:
		return formatDecimal(value.decimal)
	case FloatKind:
		return formatFloat(value.float)
	case BytesKind:
		return formatBytes(value.bytes)
	case ListKind:
		parts := make([]string, len(value.values))
		for i, item := range value.values {
			parts[i] = item.display(true)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case MapKind:
		return formatEntries(value.entries, false)
	case RecordKind:
		return "record " + formatEntries(value.entries, true)
	default:
		return "<invalid>"
	}
}

func formatEntries(entries []Entry, record bool) string {
	parts := make([]string, len(entries))
	for i, entry := range entries {
		key := quoteText(entry.Name)
		if record {
			key = entry.Name
		}
		parts[i] = key + ": " + entry.Value.display(true)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

func quoteText(value string) string {
	var result strings.Builder
	result.WriteByte('"')
	for _, character := range value {
		switch character {
		case '\\':
			result.WriteString("\\\\")
		case '"':
			result.WriteString("\\\"")
		case '\n':
			result.WriteString("\\n")
		case '\r':
			result.WriteString("\\r")
		case '\t':
			result.WriteString("\\t")
		default:
			result.WriteRune(character)
		}
	}
	result.WriteByte('"')
	return result.String()
}

func formatDecimal(decimal Decimal) string {
	coefficient := decimal.Coefficient.String()
	negative := strings.HasPrefix(coefficient, "-")
	if negative {
		coefficient = coefficient[1:]
	}
	if decimal.Scale <= 0 {
		return decimal.Coefficient.String()
	}
	if len(coefficient) <= decimal.Scale {
		coefficient = strings.Repeat("0", decimal.Scale-len(coefficient)+1) + coefficient
	}
	point := len(coefficient) - decimal.Scale
	formatted := coefficient[:point] + "." + coefficient[point:]
	if negative {
		return "-" + formatted
	}
	return formatted
}

func formatFloat(value float64) string {
	formatted := strconv.FormatFloat(value, 'g', -1, 64)
	formatted = strings.ToLower(formatted)
	if exponent := strings.IndexByte(formatted, 'e'); exponent >= 0 {
		mantissa, power := formatted[:exponent], formatted[exponent+1:]
		sign := ""
		if strings.HasPrefix(power, "+") {
			power = power[1:]
		} else if strings.HasPrefix(power, "-") {
			sign, power = "-", power[1:]
		}
		power = strings.TrimLeft(power, "0")
		if power == "" {
			power = "0"
		}
		if !strings.Contains(mantissa, ".") {
			mantissa += ".0"
		}
		return mantissa + "e" + sign + power
	}
	if !strings.Contains(formatted, ".") {
		formatted += ".0"
	}
	return formatted
}

func formatBytes(value []byte) string {
	var result strings.Builder
	result.WriteString("b\"")
	for _, item := range value {
		switch {
		case item == '\\':
			result.WriteString("\\\\")
		case item == '"':
			result.WriteString("\\\"")
		case item >= 0x20 && item <= 0x7e:
			result.WriteByte(item)
		default:
			result.WriteString(fmt.Sprintf("\\x%02x", item))
		}
	}
	result.WriteByte('"')
	return result.String()
}
