package interpreter

import (
	"bytes"
	"strings"
	"testing"

	"grab/internal/parser"
)

func TestRun(t *testing.T) {
	cases := []struct{ name, source, output, errorText string }{
		{"approved vertical slice", "set name to \"Allen\"\nset language to \"GRAB\"\nshow \"Welcome to \" joined with language\nshow \"Hello \" joined with name", "Welcome to GRAB\nHello Allen\n", ""},
		{"portable values", "set product to record {sku: \"A-17\", active: yes, stock: 12, price: 19.95, ratio: 1.25e-2, labels: [\"new\", \"sale\"], payload: b\"\\x00\\xff\", note: nothing}\nshow product", "record {sku: \"A-17\", active: yes, stock: 12, price: 19.95, ratio: 0.0125, labels: [\"new\", \"sale\"], payload: b\"\\x00\\xff\", note: nothing}\n", ""},
		{"forward reference blocks output", "show \"hidden\"\nshow missing\nset missing to yes", "", "unbound variable"},
		{"joined type error retains prior output", "show \"first\"\nshow yes joined with \"x\"", "first\n", "joined with requires text"},
		{"math comparisons and logic", "show (10 plus 5) times 2 minus 6 divided by 3\nshow 17 modulo 5\nshow 10 is 10.00\nshow 5 is at least 5 and not no", "28\n2\nyes\nyes\n", ""},
		{"record list and map lookup", "set p to record {sku: \"A-17\", tags: [\"new\", \"sale\"]}\nset m to {\"k\": 42}\nshow p.sku\nshow p.tags at 1\nshow m at \"k\"\nshow p has sku\nshow m has \"missing\"", "A-17\nsale\n42\nyes\nno\n", ""},
		{"if repeat for each and while", "set total to 0\nrepeat 3 times\n  set total to total plus 2\nend\nfor each x in [1, 2, 3]\n  if x is greater than 1\n    set total to total plus x\n  otherwise\n    set total to total plus 10\n  end\nend\nwhile total is less than 25\n  set total to total plus 1\nend\nshow total", "25\n", ""},
		{"functions and recursion", "make fact using n\n  if n is at most 1\n    give back 1\n  end\n  give back n times (fact using n minus 1)\nend\nshow fact using 5", "120\n", ""},
		{"function arity mismatch preflight", "make add using a, b\n  give back a plus b\nend\nshow \"hidden\"\nshow add using 1", "", "function arity mismatch"},
		{"division by zero", "show 10 divided by 0", "", "division by zero"},
		{"absent field", "set r to record {a: 1}\nshow r.b", "", "absent field or key"},
		{"index out of bounds", "set items to [10, 20]\nshow items at 5", "", "index out of bounds"},
		{"negative repeat count", "repeat -1 times\n  show \"no\"\nend", "", "negative repeat count"},
		{"call stack overflow", "make inf using n\n  give back inf using n plus 1\nend\nshow inf using 0", "", "call stack overflow"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			program, err := parser.Parse("test.grab", test.source)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			err = Run(program, &output)
			if output.String() != test.output {
				t.Fatalf("output = %q, want %q", output.String(), test.output)
			}
			if test.errorText == "" {
				if err != nil {
					t.Fatalf("Run() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.errorText) {
				t.Fatalf("Run() error = %v, want containing %q", err, test.errorText)
			}
		})
	}
}
