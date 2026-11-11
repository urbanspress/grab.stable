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
