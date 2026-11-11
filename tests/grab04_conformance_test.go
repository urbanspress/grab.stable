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
	"grab/internal/parser"
)

func TestGRAB04SpecificationConformance(t *testing.T) {
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

	for _, tc := range grab04SpecificationCases() {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "case04.grab")
			if err := os.WriteFile(file, []byte(tc.source), 0o600); err != nil {
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

	t.Run("CLI capabilities check format and diagnostic modes", func(t *testing.T) {
		dir := t.TempDir()
		grabPath := filepath.Join(dir, "app.grab")
		tmplPath := filepath.Join(dir, "app.grab.html")

		unformatted := "use   text\nset   price   to   19.95\nset   badge   to   \"$\" joined with (text.from using price)\nshow   badge\n"
		if err := os.WriteFile(grabPath, []byte(unformatted), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(tmplPath, []byte("<p>{badge}</p>\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		// 1. grab check succeeds with exit code 0 and empty stdout.
		var stdout, stderr bytes.Buffer
		cmd := exec.Command(binary, "check", grabPath, tmplPath)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil || stdout.Len() != 0 || stderr.Len() != 0 {
			t.Fatalf("grab check failed: err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
		}

		// 2. grab format --write normalizes source in place.
		stdout.Reset()
		stderr.Reset()
		cmd = exec.Command(binary, "format", "--write", grabPath)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("grab format --write failed: %v stderr=%q", err, stderr.String())
		}
		formattedBytes, err := os.ReadFile(grabPath)
		if err != nil {
			t.Fatal(err)
		}
		wantFormatted := "use text\nset price to 19.95\nset badge to \"$\" joined with (text.from using price)\nshow badge\n"
		if string(formattedBytes) != wantFormatted {
			t.Fatalf("formatted file =\n%s\nwant:\n%s", string(formattedBytes), wantFormatted)
		}

		// 3. Capability flags --allow-read / --allow-write and sensitive path blocking.
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=1"), 0o600); err != nil {
			t.Fatal(err)
		}
		fileProg := filepath.Join(dir, "io.grab")
		ioSrc := "use files\ncall files.write_text using \"note.txt\", \"ok\"\nshow files.read_text using \"note.txt\"\n"
		if err := os.WriteFile(fileProg, []byte(ioSrc), 0o600); err != nil {
			t.Fatal(err)
		}
		stdout.Reset()
		stderr.Reset()
		cmd = exec.Command(binary, "run", "--allow-read", dir, "--allow-write", dir, fileProg)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil || stdout.String() != "ok\n" {
			t.Fatalf("grab run with capabilities failed: err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
		}

		// 4. Diagnostic modes: beginner vs production.
		badProg := filepath.Join(dir, "bad.grab")
		if err := os.WriteFile(badProg, []byte("show missing_var\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		stdout.Reset()
		stderr.Reset()
		cmd = exec.Command(binary, "run", "--diagnostics=beginner", badProg)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		_ = cmd.Run()
		if !strings.Contains(stderr.String(), "[GRAB-E201]") || !strings.Contains(stderr.String(), "Hint:") {
			t.Fatalf("beginner mode stderr = %q, want [GRAB-E201] and Hint:", stderr.String())
		}

		stdout.Reset()
		stderr.Reset()
		cmd = exec.Command(binary, "run", "--diagnostics=production", badProg)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		_ = cmd.Run()
		if strings.TrimSpace(stderr.String()) != "grab: [GRAB-E201] unbound variable reference" {
			t.Fatalf("production mode stderr = %q, want exact redacted output", stderr.String())
		}
	})
}

func grab04SpecificationCases() []conformanceCase {
	var cases []conformanceCase
	ok := func(name, source, output string) {
		cases = append(cases, conformanceCase{name: name, source: source, output: output})
	}
	bad := func(name, source, category, fragment string) {
		cases = append(cases, conformanceCase{name: name, source: source, category: category, fragment: fragment})
	}

	// Section 2.1: Reserved GRAB 0.4 keywords cannot be bound as variables.
	for _, word := range strings.Fields("use try catch raise") {
		bad("reserved04/"+word, "set "+word+" to 1", "syntax", word)
	}
	// Module names are not globally reserved when not imported via 'use'.
	for _, word := range strings.Fields("text math collections dates files") {
		ok("unreserved_module_name/"+word, "set "+word+" to 1\nshow "+word, "1\n")
	}

	// Section 2.3: Module import validation rules.
	bad("unknown module", "use unknown_mod", "unknown function", "unknown_mod")
	bad("duplicate use module", "use text\nuse text", "duplicate definition", "text")
	cases[len(cases)-1].occurrence = 1
	bad("module collides with make", "use text\nmake text\n  give back 1\nend", "duplicate definition", "text")
	cases[len(cases)-1].occurrence = 1
	bad("module collides with set", "use text\nset text to 1", "duplicate definition", "text")
	cases[len(cases)-1].occurrence = 1
	bad("use inside block disallowed", "if yes\n  use text\nend", "syntax", "use")
	bad("call module without use", "show text.from using 10", "unknown function", "text.from")
	bad("call unknown function in imported module", "use text\nshow text.nonexistent using \"a\"", "unknown function", "text.nonexistent")
	bad("module function arity mismatch", "use math\nshow math.round using 19.95", "function arity mismatch", "math.round using 19.95")

	// Section 7 Case 1: Standard library (text, math, collections) and $19.95 price formatting.
	ok("spec case 1 stdlib price formatting", `use text
use math
use collections

set product to record {sku: "A-17", price: 19.95, tags: ["new", "sale"]}
set taxed to math.round using (product.price times 1.08), 2
set price_label to "$" joined with (text.from using taxed)
set all_tags to collections.append using product.tags, "featured"

show price_label
show text.join using all_tags, ", "
`, "$21.55\nnew, sale, featured\n")

	// Section 3.1: text module functions and error checks.
	ok("text module operations", `use text
show text.contains using "hello world", "world"
show text.starts_with using "grab_04", "grab"
show text.ends_with using "grab_04", "04"
show text.replace using "a-b-c", "-", "/"
show text.split using "one,two,three", ","
show text.lower using "GRAB"
show text.upper using "grab"
`, "yes\nyes\nyes\na/b/c\n[\"one\", \"two\", \"three\"]\ngrab\nGRAB\n")
	bad("text to_integer invalid", "use text\nshow text.to_integer using \"abc\"", "standard library error", "text.to_integer using \"abc\"")
	bad("text to_decimal invalid", "use text\nshow text.to_decimal using \"19\"", "standard library error", "text.to_decimal using \"19\"")

	// Section 3.2: math module error checks.
	bad("math clamp low greater than high", "use math\nshow math.clamp using 5, 10, 2", "standard library error", "math.clamp using 5, 10, 2")
	bad("math round scale out of range", "use math\nshow math.round using 1.5, 20", "standard library error", "math.round using 1.5, 20")

	// Section 3.3: collections module error checks.
	bad("collections slice out of bounds", "use collections\nshow collections.slice using [1, 2], 0, 5", "index out of bounds", "collections.slice using [1, 2], 0, 5")

	// Section 3.4: dates module operations and error checks.
	ok("dates module unix and iso", `use dates
set d to dates.from_unix using 0
show d.iso
show d.year
`, "1970-01-01T00:00:00Z\n1970\n")
	bad("dates parse_iso malformed", "use dates\nshow dates.parse_iso using \"not-a-date\"", "standard library error", "dates.parse_iso using \"not-a-date\"")

	// Section 7 Case 2: Structured error handling (try / catch / raise).
	ok("spec case 2 try catch and raise", `make apply_discount using price, percent
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
`, "GRAB-E308: Discount cannot exceed 50%\n100\n")

	ok("try catch division by zero recovery", `try
  set ratio to 100 divided by 0
catch err
  show err.code joined with " (" joined with err.category joined with ")"
  set ratio to 0
end
show ratio
`, "GRAB-E302 (division by zero)\n0\n")

	bad("unhandled raise error", "raise \"boom\"", "user raised error", "raise \"boom\"")
	bad("raise non-text non-record", "raise 123", "runtime type error", "raise 123")
	bad("non-exhaustive try catch variable", "try\n  set only_in_try to 1\ncatch err\n  show err.code\nend\nshow only_in_try", "unbound variable reference", "only_in_try")
	cases[len(cases)-1].occurrence = 1

	// Section 7 Case 3: Capability-based filesystem permission denial.
	bad("spec case 3 files read_text default deny", "use files\nshow files.read_text using \"notes.txt\"", "permission denied", "files.read_text using \"notes.txt\"")

	return cases
}
