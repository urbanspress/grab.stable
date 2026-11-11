package runtime

import "testing"

func TestCanonicalDisplay(t *testing.T) {
	cases := []struct {
		name  string
		value Value
		want  string
	}{
		{"text", Text("hello\n\"world"), "hello\n\"world"},
		{"boolean", Boolean(true), "yes"},
		{"nothing", Nothing(), "nothing"},
		{"integer", mustInteger(t, "123456789012345678901234567890"), "123456789012345678901234567890"},
		{"decimal scale", mustDecimal(t, "19.950"), "19.950"},
		{"float", mustFloat(t, "1.25e-2"), "0.0125"},
		{"float exponent", mustFloat(t, "1e+20"), "1.0e20"},
		{"bytes", Bytes([]byte{0, 0xff, 'A', '\\', '"'}), "b\"\\x00\\xffA\\\\\\\"\""},
		{"collections", Record([]Entry{{"sku", Text("A-17")}, {"active", Boolean(true)}, {"stock", mustInteger(t, "12")}, {"price", mustDecimal(t, "19.95")}, {"payload", Bytes([]byte{0, 255})}}), "record {sku: \"A-17\", active: yes, stock: 12, price: 19.95, payload: b\"\\x00\\xff\"}"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := test.value.Display(); got != test.want {
				t.Fatalf("Display() = %q, want %q", got, test.want)
			}
		})
	}
}

func mustInteger(t *testing.T, text string) Value {
	t.Helper()
	value, err := IntegerFromString(text)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func mustDecimal(t *testing.T, text string) Value {
	t.Helper()
	value, err := DecimalFromString(text)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func mustFloat(t *testing.T, text string) Value {
	t.Helper()
	value, err := FloatFromString(text)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
