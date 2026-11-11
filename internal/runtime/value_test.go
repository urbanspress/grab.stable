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

func TestArithmeticAndComparisons(t *testing.T) {
	t.Run("exact integer and decimal arithmetic", func(t *testing.T) {
		sum, err := Add(mustInteger(t, "100"), mustDecimal(t, "8.00"))
		if err != nil || sum.Display() != "108.00" {
			t.Fatalf("Add(100, 8.00) = %v, %v, want 108.00", sum.Display(), err)
		}
		prod, err := Mul(mustInteger(t, "100"), mustDecimal(t, "0.08"))
		if err != nil || prod.Display() != "8.00" {
			t.Fatalf("Mul(100, 0.08) = %v, %v, want 8.00", prod.Display(), err)
		}
		divExact, err := Div(mustInteger(t, "10"), mustInteger(t, "2"))
		if err != nil || divExact.Kind() != IntegerKind || divExact.Display() != "5" {
			t.Fatalf("Div(10, 2) = %v (%v), %v, want integer 5", divExact.Display(), divExact.Kind(), err)
		}
		divDec, err := Div(mustInteger(t, "1"), mustInteger(t, "4"))
		if err != nil || divDec.Kind() != DecimalKind || divDec.Display() != "0.25" {
			t.Fatalf("Div(1, 4) = %v (%v), %v, want decimal 0.25", divDec.Display(), divDec.Kind(), err)
		}
		divRep, err := Div(mustInteger(t, "1"), mustInteger(t, "3"))
		if err != nil || divRep.Display() != "0.3333333333333333" {
			t.Fatalf("Div(1, 3) = %v, %v, want 0.3333333333333333", divRep.Display(), err)
		}
		modVal, err := Mod(mustInteger(t, "17"), mustInteger(t, "5"))
		if err != nil || modVal.Display() != "2" {
			t.Fatalf("Mod(17, 5) = %v, %v, want 2", modVal.Display(), err)
		}
	})

	t.Run("division by zero and float mixing errors", func(t *testing.T) {
		if _, err := Div(mustInteger(t, "10"), mustInteger(t, "0")); err != ErrDivisionByZero {
			t.Fatalf("Div by 0 err = %v, want ErrDivisionByZero", err)
		}
		if _, err := Mod(mustInteger(t, "10"), mustInteger(t, "0")); err != ErrDivisionByZero {
			t.Fatalf("Mod by 0 err = %v, want ErrDivisionByZero", err)
		}
		if _, err := Add(mustInteger(t, "10"), mustFloat(t, "1e0")); err != ErrTypeMismatch {
			t.Fatalf("Add(int, float) err = %v, want ErrTypeMismatch", err)
		}
	})

	t.Run("equality and ordering", func(t *testing.T) {
		if !mustInteger(t, "10").Equal(mustDecimal(t, "10.00")) {
			t.Fatal("expected 10 == 10.00")
		}
		if mustInteger(t, "10").Equal(mustFloat(t, "1e1")) {
			t.Fatal("expected 10 != 1e1 (float does not mix with integer)")
		}
		cmp, ok := mustDecimal(t, "108.00").Compare(mustInteger(t, "100"))
		if !ok || cmp <= 0 {
			t.Fatalf("Compare(108.00, 100) = (%d, %v), want (>0, true)", cmp, ok)
		}
	})
}
