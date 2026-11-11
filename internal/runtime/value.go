package runtime

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

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
