package data

import (
	"math"
	"reflect"
	"testing"

	rt "grab/internal/runtime"
)

func decimal(t *testing.T, s string) rt.Value {
	t.Helper()
	v, e := rt.DecimalFromString(s)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func TestTypeMapping(t *testing.T) {
	money := TypeSpec{Type: "decimal", Precision: 24, Scale: 2, Storage: "text"}
	for _, tt := range []struct {
		name  string
		spec  TypeSpec
		value rt.Value
		raw   any
		want  string
	}{
		{"integer minimum", TypeSpec{Type: "integer"}, rt.IntegerFromInt64(math.MinInt64), int64(math.MinInt64), "-9223372036854775808"},
		{"integer maximum", TypeSpec{Type: "integer"}, rt.IntegerFromInt64(math.MaxInt64), int64(math.MaxInt64), "9223372036854775807"},
		{"large money", money, decimal(t, "1234567890123456789012.34"), "1234567890123456789012.34", "1234567890123456789012.34"},
		{"money padding", money, decimal(t, "1.2"), "1.20", "1.20"},
		{"money exact zero removal", money, decimal(t, "1.2300"), "1.23", "1.23"},
		{"negative money", money, decimal(t, "-0.01"), "-0.01", "-0.01"},
		{"unicode", TypeSpec{Type: "text"}, rt.Text("café 🍵"), "café 🍵", "café 🍵"},
		{"boolean", TypeSpec{Type: "boolean"}, rt.Boolean(true), int64(1), "yes"},
		{"empty text", TypeSpec{Type: "text"}, rt.Text(""), "", ""},
		{"nullable", TypeSpec{Type: "text", Nullable: true}, rt.Nothing(), nil, "nothing"},
		{"utc nanoseconds", TypeSpec{Type: "timestamp", Zone: "utc", FractionDigits: 9}, rt.Text("2026-11-11T01:02:03.123456789-05:00"), "2026-11-11T06:02:03.123456789Z", "2026-11-11T06:02:03.123456789Z"},
		{"naive", TypeSpec{Type: "timestamp", Zone: "naive", FractionDigits: 3}, rt.Text("2026-11-11T01:02:03.120"), "2026-11-11T01:02:03.120", "2026-11-11T01:02:03.120"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw, e := tt.spec.Bind(tt.value, "sqlite")
			if e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(raw, tt.raw) {
				t.Fatalf("bind %#v, want %#v", raw, tt.raw)
			}
			v, e := tt.spec.Decode(raw, "sqlite")
			if e != nil {
				t.Fatal(e)
			}
			if v.Display() != tt.want {
				t.Fatalf("decode %q, want %q", v.Display(), tt.want)
			}
		})
	}
}

func TestRejectLossyMapping(t *testing.T) {
	for _, tt := range []struct {
		name string
		spec TypeSpec
		raw  any
	}{
		{"real money", TypeSpec{Type: "decimal", Precision: 18, Scale: 2}, 1.23},
		{"integer money", TypeSpec{Type: "decimal", Precision: 18, Scale: 2}, int64(123)},
		{"blob money", TypeSpec{Type: "decimal", Precision: 18, Scale: 2}, []byte("1.23")},
		{"rounding", TypeSpec{Type: "decimal", Precision: 18, Scale: 2}, "1.234"},
		{"precision", TypeSpec{Type: "decimal", Precision: 3, Scale: 2}, "10.00"},
		{"invalid decimal", TypeSpec{Type: "decimal", Precision: 18, Scale: 2}, "1e2"},
		{"invalid UTF8", TypeSpec{Type: "text"}, string([]byte{255})},
		{"coerced text", TypeSpec{Type: "text"}, int64(4)},
		{"coerced integer", TypeSpec{Type: "integer"}, "4"},
		{"invalid boolean", TypeSpec{Type: "boolean"}, int64(2)},
		{"null forbidden", TypeSpec{Type: "text"}, nil},
		{"timestamp precision", TypeSpec{Type: "timestamp", Zone: "utc", FractionDigits: 3}, "2026-11-11T01:02:03.1234Z"},
		{"invalid date", TypeSpec{Type: "timestamp", Zone: "utc"}, "2026-02-30T00:00:00Z"},
		{"naive with timezone", TypeSpec{Type: "timestamp", Zone: "naive"}, "2026-11-11T00:00:00Z"},
		{"float infinity", TypeSpec{Type: "float"}, math.Inf(1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, e := tt.spec.Decode(tt.raw, "sqlite"); e == nil {
				t.Fatal("accepted lossy/invalid result")
			}
		})
	}
}

func TestBytesAreCopied(t *testing.T) {
	s := TypeSpec{Type: "bytes"}
	raw := []byte{0, 255}
	v, e := s.Decode(raw, "sqlite")
	if e != nil {
		t.Fatal(e)
	}
	raw[0] = 1
	if v.BytesValue()[0] != 0 {
		t.Fatal("result aliases driver memory")
	}
	b, e := s.Bind(rt.Bytes(nil), "sqlite")
	if e != nil {
		t.Fatal(e)
	}
	if b.([]byte) == nil {
		t.Fatal("empty bytes became NULL")
	}
}

func TestTypeDescriptors(t *testing.T) {
	record := func(entries ...rt.Entry) rt.Value { return rt.Record(entries) }
	for _, tt := range []struct {
		name  string
		value rt.Value
		valid bool
	}{
		{"integer", rt.Text("integer"), true},
		{"bare decimal", rt.Text("decimal"), false},
		{"bare timestamp", rt.Text("timestamp"), false},
		{"unknown", rt.Text("money"), false},
		{"nullable integer", record(rt.Entry{Name: "type", Value: rt.Text("integer")}, rt.Entry{Name: "nullable", Value: rt.Boolean(true)}), true},
		{"unknown option", record(rt.Entry{Name: "type", Value: rt.Text("text")}, rt.Entry{Name: "scale", Value: rt.IntegerFromInt64(2)}), false},
		{"invalid nullable", record(rt.Entry{Name: "type", Value: rt.Text("text")}, rt.Entry{Name: "nullable", Value: rt.Text("yes")}), false},
		{"inverted bounds", record(rt.Entry{Name: "type", Value: rt.Text("integer")}, rt.Entry{Name: "min", Value: rt.IntegerFromInt64(2)}, rt.Entry{Name: "max", Value: rt.IntegerFromInt64(1)}), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, e := typeFromValue(tt.value)
			if (e == nil) != tt.valid {
				t.Fatalf("valid=%v error=%v", tt.valid, e)
			}
		})
	}
}
