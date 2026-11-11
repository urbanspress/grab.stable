package tests

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"grab/internal/interpreter"
	"grab/internal/lexer"
	"grab/internal/parser"
)

func TestGRAB03SpecificationConformance(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "grab")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	goTool := filepath.Join(runtime.GOROOT(), "bin", "go")
	if runtime.GOOS == "windows" {
		goTool += ".exe"
	}
	build := exec.Command(goTool, "build", "-o", binary, "./cmd/grab")
	build.Dir = ".."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}

	for _, tc := range grab03SpecificationCases() {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "case03.grab")
			if err := os.WriteFile(file, []byte(tc.source), 0600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			cmd := exec.Command(binary, "run", file)
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if tc.category == "" {
				if err != nil || stderr.Len() != 0 {
					t.Fatalf("CLI: %v, stderr %q", err, stderr.String())
				}
			} else {
				if _, ok := err.(*exec.ExitError); !ok || stderr.Len() == 0 {
					t.Fatalf("expected failing CLI diagnostic; err=%v stderr=%q", err, stderr.String())
				}
			}
			if stdout.String() != tc.output {
				t.Errorf("CLI stdout=%q, want %q", stdout.String(), tc.output)
			}

			var output bytes.Buffer
			program, err := parser.Parse(file, tc.source)
			if err == nil {
				err = interpreter.Run(program, &output)
			}
			if output.String() != tc.output {
				t.Errorf("pipeline stdout=%q, want %q", output.String(), tc.output)
			}
			category, span := diagnostic03(err)
			if category != tc.category {
				t.Fatalf("category=%q (%v), want %q", category, err, tc.category)
			}
			if tc.category != "" {
				want := fragmentSpan(t, tc)
				if span != want {
					t.Errorf("span=%+v, want %+v", span, want)
				}
				location := fmt.Sprintf("line %d, column %d", want.Start.Line, want.Start.Column)
				if !strings.Contains(stderr.String(), location) {
					t.Errorf("CLI diagnostic %q lacks %q", stderr.String(), location)
				}
			}
		})
	}
}

func diagnostic03(err error) (string, lexer.Span) {
	switch e := err.(type) {
	case nil:
		return "", lexer.Span{}
	case *lexer.InvalidUTF8Error:
		return "encoding", e.Span
	case *lexer.LexError:
		return e.Kind.String(), e.Span
	case *parser.ParseError:
		return "syntax", e.Span
	case *interpreter.Error:
		return e.Kind.String(), e.Span
	default:
		return fmt.Sprintf("unknown %T", err), lexer.Span{}
	}
}

func grab03SpecificationCases() []conformanceCase {
	var cases []conformanceCase
	ok := func(name, source, output string) {
		cases = append(cases, conformanceCase{name: name, source: source, output: output})
	}
	bad := func(name, source, category, fragment string) {
		cases = append(cases, conformanceCase{name: name, source: source, category: category, fragment: fragment})
	}

	// Section 2.3: Reserved GRAB 0.3 keywords cannot be bound as variables.
	for _, word := range strings.Fields("if otherwise end repeat times for each in while make using give back call is not greater less than at least most and or plus minus divided by modulo has") {
		bad("reserved03/"+word, "set "+word+" to 1", "syntax", word)
	}

	// Section 4.1 & 4.2: Precedence, grouping, and arithmetic.
	ok("precedence and grouping", "show 2 plus 3 times 4\nshow (2 plus 3) times 4\nshow 20 minus 5 minus 3\nshow 17 modulo 5", "14\n20\n12\n2\n")
	ok("decimal and integer promotion", "show 100 times 0.08\nshow 100 plus (100 times 0.08)\nshow 19.95 minus 5\nshow 10 divided by 2\nshow 1 divided by 4\nshow 1 divided by 3", "8.00\n108.00\n14.95\n5\n0.25\n0.3333333333333333\n")
	ok("float arithmetic", "show 1.5e1 plus 2.5e1\nshow 5.0e0 times 2.0e0\nshow 9.0e0 divided by 2.0e0", "40.0\n10.0\n4.5\n")
	bad("division by zero int", "show 10 divided by 0", "division by zero", "10 divided by 0")
	bad("division by zero decimal", "show 10.5 divided by 0.00", "division by zero", "10.5 divided by 0.00")
	bad("modulo by zero", "show 10 modulo 0", "division by zero", "10 modulo 0")
	bad("no implicit float mixing", "show 10 plus 1.0e0", "runtime type error", "10 plus 1.0e0")
	bad("modulo requires integers", "show 10.5 modulo 2", "runtime type error", "10.5 modulo 2")

	// Section 4.3 & 4.4: Equality, relational comparisons, and short-circuit logic.
	ok("equality and comparisons", "show 10 is 10.00\nshow 10 is not 11\nshow [1, 2] is [1, 2]\nshow record {a: 1, b: 2} is record {b: 2, a: 1}\nshow 20 is greater than 18\nshow 20 greater than 18\nshow 10 is less than 20\nshow 10 is at least 10\nshow 10 is at most 9\nshow \"apple\" is less than \"banana\"", "yes\nyes\nyes\nyes\nyes\nyes\nyes\nyes\nno\nyes\n")
	ok("boolean logic and short circuit", "show not no and yes\nshow no and (10 divided by 0 is 1)\nshow yes or (10 divided by 0 is 1)", "yes\nno\nyes\n")
	bad("not requires boolean", "show not 0", "runtime type error", "not 0")
	bad("and requires boolean", "show 1 and yes", "runtime type error", "1 and yes")
	bad("comparison incompatible types", "show \"10\" is greater than 5", "runtime type error", "\"10\" is greater than 5")

	// Section 4.5: Collection lookup and membership.
	ok("record field list at and map at", "set p to record {sku: \"A-17\", note: nothing, tags: [\"new\", \"sale\"]}\nset m to {\"sku\": \"A-17\", \"empty\": nothing}\nshow p.sku\nshow p.note\nshow p.tags at 0\nshow p.tags at 1\nshow m at \"sku\"\nshow m at \"empty\"\nshow p has sku\nshow p has note\nshow p has missing\nshow m has \"sku\"\nshow m has \"empty\"\nshow m has \"missing\"", "A-17\nnothing\nnew\nsale\nA-17\nnothing\nyes\nyes\nno\nyes\nyes\nno\n")
	bad("absent record field", "set p to record {sku: \"A-17\"}\nshow p.price", "absent field or key", "price")
	bad("absent map key", "set m to {\"sku\": \"A-17\"}\nshow m at \"price\"", "absent field or key", "\"price\"")
	bad("list negative index", "set items to [10, 20]\nshow items at -1", "index out of bounds", "-1")
	bad("list index out of bounds", "set items to [10, 20]\nshow items at 2", "index out of bounds", "2")
	cases[len(cases)-1].occurrence = 1

	// Section 5: Control flow (if / otherwise if / otherwise, repeat, for each, while).
	ok("if branches and exhaustive binding", "set score to 85\nif score is at least 90\n  set grade to \"A\"\notherwise if score is at least 80\n  set grade to \"B\"\notherwise\n  set grade to \"C\"\nend\nshow grade", "B\n")
	ok("repeat and while loops", "set n to 0\nrepeat 2 times 2 times\n  set n to n plus 3\nend\nwhile n is less than 15\n  set n to n plus 1\nend\nshow n", "15\n")
	ok("for each list and map", "set m to {\"first\": 1, \"second\": 2}\nfor each k in m\n  show k\nend", "first\nsecond\n")
	bad("if condition must be boolean", "if 1\n  show \"bad\"\nend", "runtime type error", "1")
	bad("negative repeat count", "repeat -2 times\n  show \"bad\"\nend", "negative repeat count", "-2")

	// Section 6: Functions (make, using, give back, call).
	ok("spec case 1 functions and conditions", "make calculate_total using price, rate\n  set tax to price times rate\n  give back price plus tax\nend\n\nset subtotal to 100\nset total to calculate_total using subtotal, 0.08\n\nif total is greater than 100\n  show \"Taxed total: \"\n  show total\notherwise\n  show \"No tax\"\nend\n", "Taxed total: \n108.00\n")
	ok("spec case 2 loops and collection lookup", "set product to record {sku: \"A-17\", name: \"GRAB Starter Kit\", tags: [\"new\", \"sale\"]}\nshow product.name joined with \" (\" joined with product.sku joined with \")\"\nshow \"First tag: \" joined with (product.tags at 0)\n\nset count to 0\nfor each tag in product.tags\n  set count to count plus 1\n  show \"Tag: \" joined with tag\nend\nshow count\n", "GRAB Starter Kit (A-17)\nFirst tag: new\nTag: new\nTag: sale\n2\n")
	ok("zero-arg functions and forward calls", "make main_banner\n  give back helper using \"GRAB 0.3\"\nend\nmake helper text\n  give back \"*** \" joined with text joined with \" ***\"\nend\nshow call main_banner", "*** GRAB 0.3 ***\n")

	// Section 7.1: Pre-execution function diagnostics.
	bad("spec case 3 function arity mismatch", "make add using a, b\n  give back a plus b\nend\n\nshow add using 1", "function arity mismatch", "add using 1")
	bad("unknown function", "show missing_fn using 1", "unknown function", "missing_fn")
	bad("duplicate function definition", "make f\nend\nmake f\nend", "duplicate definition", "f")
	cases[len(cases)-1].occurrence = 1
	bad("duplicate parameter name", "make f using a, a\nend", "duplicate definition", "a")
	cases[len(cases)-1].occurrence = 2
	bad("give back outside function", "give back 1", "invalid give back", "give back 1")
	bad("call stack overflow", "make boom using x\n  give back boom using x plus 1\nend\nshow boom using 0", "call stack overflow", "boom using x plus 1")

	return cases
}

func TestLoopStepLimitGuard(t *testing.T) {
	source := "while yes\nend\n"
	program, err := parser.Parse("infinite.grab", source)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	var out bytes.Buffer
	err = interpreter.Run(program, &out)
	if err == nil {
		t.Fatal("expected ExecutionLimitExceeded error for infinite loop, got nil")
	}
	interpErr, ok := err.(*interpreter.Error)
	if !ok || interpErr.Kind != interpreter.ExecutionLimitExceeded {
		t.Fatalf("Run() error = %v, want ExecutionLimitExceeded", err)
	}
}
