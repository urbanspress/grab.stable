package interpreter

import (
	"bytes"
	"os"
	"path/filepath"
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

func TestGRAB04StdlibErrorsAndCapabilities(t *testing.T) {
	cases := []struct {
		name      string
		source    string
		output    string
		errorText string
	}{
		{
			name: "text math and collections stdlib",
			source: `use text
use math
use collections

set product to record {sku: "A-17", price: 19.95, tags: ["new", "sale"]}
set taxed to math.round using (product.price times 1.08), 2
set price_label to "$" joined with (text.from using taxed)
set all_tags to collections.append using product.tags, "featured"
show price_label
show text.join using all_tags, ", "
show text.upper using (text.trim using "  grab 0.4  ")
show text.length using "é😀"
show math.abs using -12.50
show math.min using 10, 4.5
show math.max using 10, 4.5
show math.clamp using 15, 0, 10
show math.floor using -2.3
show math.ceil using 2.3
show (text.to_integer using "-42") plus 2
show (text.to_decimal using "19.95") plus 0.05
`,
			output: "$21.55\nnew, sale, featured\nGRAB 0.4\n2\n12.50\n4.5\n10\n10\n-3\n3\n-40\n20.00\n",
		},
		{
			name: "collections slice keys put and remove",
			source: `use collections
set items to [10, 20, 30, 40]
show collections.length using items
show collections.slice using items, 1, 3
set m to {"a": 1, "b": 2}
set m2 to collections.put using m, "c", 3
set m3 to collections.remove using m2, "a"
show collections.keys using m3
show m3
`,
			output: "4\n[20, 30]\n[\"b\", \"c\"]\n{\"b\": 2, \"c\": 3}\n",
		},
		{
			name: "dates module",
			source: `use dates
set d1 to dates.parse_iso using "2026-11-11"
set d2 to dates.add_days using d1, 5
show d2.iso
show d2.day
show dates.diff_seconds using d2, d1
set now to call dates.now_utc
show now has year
`,
			output: "2026-11-16T00:00:00Z\n16\n432000\nyes\n",
		},
		{
			name: "try catch and raise",
			source: `make apply_discount using price, percent
  if percent is greater than 50
    raise "Discount cannot exceed 50%"
  end
  give back price minus (price times percent divided by 100)
end

try
  set final_price to apply_discount using 100, 75
catch err
  show err.code joined with ": " joined with err.message
  set final_price to 100
end
show final_price
`,
			output: "GRAB-E308: Discount cannot exceed 50%\n100\n",
		},
		{
			name: "raise custom record code",
			source: `try
  raise record {code: "GRAB-ECUSTOM", message: "Custom failure"}
catch err
  show err.code joined with " / " joined with err.message
end
`,
			output: "GRAB-ECUSTOM / Custom failure\n",
		},
		{
			name:      "fatal call stack overflow is not caught by try",
			source:    "make inf using n\n  give back inf using n plus 1\nend\ntry\n  show inf using 0\ncatch err\n  show \"caught\"\nend\n",
			errorText: "call stack overflow",
		},
		{
			name:      "calling module without use is rejected preflight",
			source:    "show text.from using 42\n",
			errorText: "unknown function \"text.from\"",
		},
		{
			name:      "files default deny without capabilities",
			source:    "use files\nshow files.read_text using \"notes.txt\"\n",
			errorText: "permission denied",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			program, err := parser.Parse("test04.grab", tc.source)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			var output bytes.Buffer
			err = Run(program, &output)
			if output.String() != tc.output {
				t.Fatalf("output = %q, want %q", output.String(), tc.output)
			}
			if tc.errorText == "" {
				if err != nil {
					t.Fatalf("Run() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.errorText) {
				t.Fatalf("Run() error = %v, want containing %q", err, tc.errorText)
			}
		})
	}
}

func TestFilesystemCapabilitiesAndSensitiveBlocklist(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "key.pem"), []byte("PRIVATE\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	src := `use files
call files.write_text using "hello.txt", "Line 1\r\nLine 2"
show files.exists using "hello.txt"
show files.read_text using "hello.txt"
show files.list_dir using "."
`
	program, err := parser.Parse("files.grab", src)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = RunWithOptions(program, &out, Options{
		Permissions: Permissions{
			AllowRead:  []string{dir},
			AllowWrite: []string{dir},
		},
	})
	if err != nil {
		t.Fatalf("RunWithOptions() error = %v", err)
	}
	want := "yes\nLine 1\nLine 2\n[\"hello.txt\"]\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}

	// Sensitive paths (.env, .git, *.pem, ..) must always be denied with GRAB-E401 even inside an allowed root.
	for _, blocked := range []string{".env", ".env.local", ".git/config", "key.pem", "id_rsa", "../outside.txt"} {
		readSrc := `use files
show files.read_text using "` + blocked + `"`
		prog, err := parser.Parse("blocked.grab", readSrc)
		if err != nil {
			t.Fatal(err)
		}
		err = RunWithOptions(prog, &out, Options{
			Permissions: Permissions{AllowRead: []string{dir}},
		})
		if err == nil || !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("reading %q: err = %v, want permission denied", blocked, err)
		}
	}
}

func TestFormatDiagnosticModes(t *testing.T) {
	src := "set price to 10\nshow prce\n"
	prog, err := parser.Parse("diag.grab", src)
	if err != nil {
		t.Fatal(err)
	}
	err = Run(prog, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected unbound variable error")
	}

	devOut := FormatDiagnostic(src, err, ModeDeveloper)
	if !strings.Contains(devOut, "[GRAB-E201]") || !strings.Contains(devOut, "line 2, column 6") {
		t.Fatalf("developer diagnostic = %q", devOut)
	}

	begOut := FormatDiagnostic(src, err, ModeBeginner)
	if !strings.Contains(begOut, "Hint:") || !strings.Contains(begOut, "2 | show prce") {
		t.Fatalf("beginner diagnostic = %q", begOut)
	}

	prodOut := FormatDiagnostic(src, err, ModeProduction)
	if prodOut != "grab: [GRAB-E201] unbound variable reference" {
		t.Fatalf("production diagnostic = %q", prodOut)
	}
}
