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

// Each case runs through both the public executable and the complete language
// pipeline. Typed diagnostics expose categories and exclusive end positions that
// the CLI deliberately does not print. Expected output never uses GRAB formatters.
type conformanceCase struct {
	name, source, output, category string
	// The marked source fragment must be the complete diagnostic span. Empty
	// means EOF. occurrence selects a repeated fragment (zero means first).
	fragment   string
	occurrence int
}

func TestSpecificationConformance(t *testing.T) {
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
	for _, tc := range specificationCases() {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "case.grab")
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
			category, span := diagnostic(err)
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

func diagnostic(err error) (string, lexer.Span) {
	switch e := err.(type) {
	case nil:
		return "", lexer.Span{}
	case *lexer.InvalidUTF8Error:
		return "encoding", e.Span
	case *lexer.LexError:
		return e.Kind.String(), e.Span
	case *parser.ParseError:
		switch e.Kind {
		case parser.ParseDuplicateMapKey:
			return "duplicate map", e.Span
		case parser.ParseDuplicateRecordField:
			return "duplicate record", e.Span
		default:
			return "syntax", e.Span
		}
	case *interpreter.Error:
		switch e.Kind {
		case interpreter.UnboundVariable:
			return "name", e.Span
		case interpreter.JoinedRequiresText:
			return "type", e.Span
		default:
			return "literal", e.Span
		}
	default:
		return fmt.Sprintf("unknown %T", err), lexer.Span{}
	}
}

func fragmentSpan(t *testing.T, tc conformanceCase) lexer.Span {
	t.Helper()
	start := len(tc.source)
	if tc.fragment != "" {
		start = 0
		for n := 0; n <= tc.occurrence; n++ {
			i := strings.Index(tc.source[start:], tc.fragment)
			if i < 0 {
				t.Fatalf("missing diagnostic fragment %q", tc.fragment)
			}
			start += i
			if n < tc.occurrence {
				start += len(tc.fragment)
			}
		}
	}
	position := func(offset int) lexer.Position {
		prefix := strings.ReplaceAll(tc.source[:offset], "\r\n", "\n")
		lines := strings.Split(prefix, "\n")
		return lexer.Position{Offset: offset, Line: len(lines), Column: len([]rune(lines[len(lines)-1])) + 1}
	}
	return lexer.Span{Start: position(start), End: position(start + len(tc.fragment))}
}

func specificationCases() []conformanceCase {
	var cases []conformanceCase
	ok := func(name, source, output string) {
		cases = append(cases, conformanceCase{name: name, source: source, output: output})
	}
	bad := func(name, source, category, fragment string) {
		cases = append(cases, conformanceCase{name: name, source: source, category: category, fragment: fragment})
	}
	// Sections 3.1-3.4 and 4: source, statements, identifiers and text.
	for i, source := range []string{"", "\n\r\n\n", " \t", "# comment", "# é 😀\r\n\t# another\n"} {
		ok(fmt.Sprintf("empty/%d", i), source, "")
	}
	ok("line endings and indentation", "\r\n\t set x to \"ok\" # comment\r\n\n show x\nshow\t\"end\"# eof", "ok\nend\n")
	ok("adjacent punctuation", `show[1,2]`, "[1, 2]\n")
	ok("identifier grammar and case", "set _ to 1\nset A_z09 to 2\nset a_z09 to 3\nshow [_, A_z09, a_z09]", "[1, 2, 3]\n")
	for _, word := range strings.Fields("if else loop for function type import module try catch async html block true false null b Set SHOW Record date uuid") {
		ok("unreserved/"+word, "set "+word+" to 1\nshow "+word, "1\n")
	}
	for _, word := range strings.Fields("set to show joined with yes no nothing record") {
		bad("reserved binding/"+word, "set "+word+" to 1", "syntax", word)
		if word == "set" {
			cases[len(cases)-1].occurrence = 1
		}
		bad("reserved field/"+word, "show record {"+word+": 1}", "syntax", word)
		if word == "show" || word == "record" {
			cases[len(cases)-1].occurrence = 1
		}
	}
	ok("all text escapes", `show "\\\"\n\r\t"`, "\\\"\n\r\t\n")
	ok("nested text escapes", `show ["\\\"\n\r\t"]`, "[\"\\\\\\\"\\n\\r\\t\"]\n")
	ok("Unicode no normalization or interpolation", "show \"é é 😀 # ${name} {name}\"", "é é 😀 # ${name} {name}\n")
	for _, r := range []rune{0, 1, 7, 8, 9, 11, 12, 31, 127, 0x85, 0x2028, 0x2029, 0xfffd, 0x10ffff} {
		s := string(r)
		ok(fmt.Sprintf("raw scalar/%04x", r), "show \""+s+"\"", s+"\n")
		ok(fmt.Sprintf("raw scalar map key/%04x", r), "show {\""+s+"\": 1}", "{\""+strings.ReplaceAll(s, "\t", `\t`)+"\": 1}\n")
	}
	for _, ending := range []string{"", "\n", "\r\n", "\r"} {
		bad(fmt.Sprintf("unterminated/%q", ending), "show \"Hello"+ending, "unterminated text literal", "\"")
	}
	bad("raw CR must prevent all execution", "show \"before\"\nshow \"a\rb\"", "unterminated text literal", "\"a")
	// The opening quote alone is the nearest meaningful unterminated-literal location.
	cases[len(cases)-1].fragment = "\""
	cases[len(cases)-1].occurrence = 2
	for _, esc := range []string{`\q`, `\x`, `\u`, `\U`, `\0`, `\a`, `\b`, `\v`, `\f`, `\/`, "\\\n", "\\\r\n", "\\"} {
		bad(fmt.Sprintf("invalid text escape/%q", esc), "show \""+esc, "invalid escape", esc)
	}
	for _, ch := range []string{";", "\r", "é", "😀", "\u00a0", "\v", "\f", "\x00", "\ufeff", "+", "(", ")", "=", ".", "'"} {
		bad(fmt.Sprintf("invalid character/%q", ch), "show \"ok\"\n"+ch, "invalid character", ch)
	}
	for i, invalid := range []string{"\xff", "\xc0\x80", "\xed\xa0\x80", "\xf4\x90\x80\x80", "\xe2\x82"} {
		bad(fmt.Sprintf("encoding/%d", i), "# é😀\r\n\t"+invalid, "encoding", invalid[:1])
	}
	// Sections 3.5, 5.2 and 5.4: every scalar kind and canonical display.
	for _, v := range [][2]string{{`""`, ""}, {"yes", "yes"}, {"no", "no"}, {"nothing", "nothing"}, {"0", "0"}, {"-0", "0"}, {"00012", "12"}, {"-9", "-9"}, {"-12", "-12"}, {"1234567890123456789012345678901234567890", "1234567890123456789012345678901234567890"}, {"-1234567890123456789012345678901234567890", "-1234567890123456789012345678901234567890"}, {"19.950", "19.950"}, {"-0.00100", "-0.00100"}, {"-0.00", "0.00"}, {"00012.0300", "12.0300"}, {"123456789012345678901234567890.12345678901234567890", "123456789012345678901234567890.12345678901234567890"}, {"1e0", "1.0"}, {"-1E+2", "-100.0"}, {"1.25e-2", "0.0125"}, {"0e0", "0.0"}, {"-0e0", "-0.0"}, {"1e20", "1.0e20"}, {"1e-9", "1.0e-9"}, {"5e-324", "5.0e-324"}, {"1.7976931348623157e308", "1.7976931348623157e308"}, {"9007199254740993e0", "9.007199254740992e15"}, {`b""`, `b""`}, {`b"A #~\x00\x7F\x80\xFF\"\\"`, `b"A #~\x00\x7f\x80\xff\"\\"`}} {
		ok("value/"+v[0], "show "+v[0], v[1]+"\n")
	}
	for _, n := range []string{"1.", "-1.", "1e", "1E+", "-1e-", "1.2e+"} {
		bad("malformed number/"+n, "show "+n, "malformed number", n)
	}
	for _, s := range []string{`b"\n`, `b"\r`, `b"\t`, `b"\q`, `b"\xg`, `b"\x0g`, `b"\x`, `b"\x0`, `b"\`} {
		bad("byte escape/"+s, "show "+s, "invalid escape", s[2:])
	}
	for _, ch := range []string{"é", "😀", "\x00", "\t", "\r", "\x7f"} {
		bad(fmt.Sprintf("raw byte/%q", ch), "show b\""+ch+"\"", "invalid byte literal character", ch)
	}
	for _, suffix := range []string{"", "\n", "\r\n"} {
		bad(fmt.Sprintf("unterminated bytes/%q", suffix), "show b\"abc"+suffix, "unterminated byte literal", "b\"abc"+suffix)
	}
	// Exhaust all byte values, both hex alphabets and canonical printable escapes.
	for n := 0; n < 256; n++ {
		want := fmt.Sprintf(`b"\x%02x"`, n)
		if n >= 32 && n <= 126 {
			want = `b"` + string(rune(n)) + `"`
		}
		if n == 34 {
			want = `b"\""`
		}
		if n == 92 {
			want = `b"\\"`
		}
		ok(fmt.Sprintf("byte/%02x", n), fmt.Sprintf(`show b"\x%02X"`, n), want+"\n")
	}
	// Sections 4-5: collections, ordering, bindings, immutable snapshots.
	for _, v := range [][2]string{{"[]", "[]"}, {"{}", "{}"}, {"record {}", "record {}"}, {"[1,]", "[1]"}, {`{"z":1,"a":nothing,}`, `{"z": 1, "a": nothing}`}, {`record {z:1,a:nothing,}`, `record {z: 1, a: nothing}`}, {`["x" joined with "y", {"": record {_a: [yes,no,nothing]}}]`, `["xy", {"": record {_a: [yes, no, nothing]}}]`}, {`[{},{"x":nothing},record {},record {x:nothing}]`, `[{}, {"x": nothing}, record {}, record {x: nothing}]`}, {`{"é":1,"é":2}`, `{"é": 1, "é": 2}`}, {`record {a:1,A:2}`, `record {a: 1, A: 2}`}} {
		ok("collection/"+v[0], "show "+v[0], v[1]+"\n")
	}
	ok("rebind and snapshots", "set x to [1]\nset old to x\nset x to nothing\nshow old\nshow x\nset x to \"a\"\nset x to x joined with \"b\"\nshow x", "[1]\nnothing\nab\n")
	ok("join associativity", `show "a" joined with "b" joined with "c"`, "abc\n")
	bad("duplicate map decoded", "show {\"a\\t\":1,\"a\t\":2}", "duplicate map", "\"a\t\"")
	bad("duplicate record", "show record {a:1,a:2}", "duplicate record", "a")
	cases[len(cases)-1].occurrence = 1
	for _, v := range []struct{ source, fragment string }{
		{"show", ""}, {"set", ""}, {"set x", ""}, {"set x 1", "1"}, {"set x to", ""}, {"set 1 to 2", "1"}, {"show 1 show 2", "show"},
		{`show "a" joined "b"`, `"b"`}, {`show "a" joined with`, ""}, {"show\n1", "\n"}, {"show [\n]", "\n"}, {"show {\n}", "\n"}, {"show record {\n}", "\n"}, {"show [1,# comment\n2]", "\n"},
		{"show [1 2]", "2"}, {"show [,]", ","}, {"show [1,,2]", ","}, {"show [1", ""}, {"show {1:2}", "1"}, {"show {a:2}", "a"}, {`show {"a" 2}`, "2"}, {`show {"a":}`, "}"}, {`show {"a":1`, ""}, {`show record {"a":1}`, `"a"`}, {"show record a", "a"}, {"show record {a 1}", "1"}, {"show record {a:}", "}"}, {"show record {a:1", ""}, {"show b \"x\"", `"x"`}, {"show 1abc", "abc"}, {"show [1][0]", "["},
	} {
		bad("grammar/"+v.source, v.source, "syntax", v.fragment)
		if v.source == "show 1 show 2" || v.source == "show [1,,2]" || v.source == "show [1][0]" {
			cases[len(cases)-1].occurrence = 1
		}
	}
	// Sections 5.1, 5.3, 5.5: resolve before execution; evaluate in source order.
	for _, source := range []string{"show missing", "show \"before\"\nshow missing\nset missing to 1", "set missing to missing", "show [missing]", `show {"x":missing}`, `show record {x:missing}`, `show "a" joined with missing`, "set Name to 1\nshow missing", "show 1 joined with 2\nshow missing"} {
		bad("name/"+source, source, "name", "missing")
		if source == "set missing to missing" {
			cases[len(cases)-1].occurrence = 1
		}
	}
	bad("case-sensitive name", "set Name to 1\nshow name", "name", "name")
	bad("Unicode and tab columns", "# heading\r\n\tshow \"😀é\" joined with missing", "name", "missing")
	for _, value := range []string{"yes", "no", "nothing", "1", "1.0", "1e0", `b"a"`, "[]", "{}", "record {}"} {
		for _, expr := range []string{value + ` joined with "x"`, `"x" joined with ` + value} {
			bad("text only/"+expr, "show \"before\"\nshow "+expr+"\nshow \"after\"", "type", expr)
			cases[len(cases)-1].output = "before\n"
		}
	}
	for _, expr := range []string{`[1 joined with "x", 2 joined with "y"]`, `{"z":1 joined with "x","a":2 joined with "y"}`, `record {z:1 joined with "x",a:2 joined with "y"}`, `1 joined with "x" joined with "y"`} {
		bad("left-first/"+expr, "show "+expr, "type", `1 joined with "x"`)
	}
	bad("set evaluates expression", "show \"before\"\nset x to [1 joined with \"a\"]\nshow \"after\"", "type", `1 joined with "a"`)
	cases[len(cases)-1].output = "before\n"
	bad("syntax preflight", "show \"before\"\nset x", "syntax", "")
	bad("lexical preflight", "show \"before\"\n;", "invalid character", ";")
	// Sections 7-9, verbatim approved programs.
	ok("section 7", "set name to \"Allen\"\nset language to \"GRAB\"\nshow \"Welcome to \" joined with language\nshow \"Hello \" joined with name\n", "Welcome to GRAB\nHello Allen\n")
	ok("section 9", `set product to record {sku: "A-17", active: yes, stock: 12, price: 19.95, ratio: 1.25e-2, labels: ["new", "sale"], payload: b"\x00\xff", note: nothing}`+"\nshow product\n", `record {sku: "A-17", active: yes, stock: 12, price: 19.95, ratio: 0.0125, labels: ["new", "sale"], payload: b"\x00\xff", note: nothing}`+"\n")
	// Additional grammar boundaries: every collection delimiter, separator and
	// line-break position, plus syntax explicitly excluded by Sections 1 and 10.
	for _, v := range []struct{ source, fragment string }{
		{"show {,}", ","}, {"show record {,}", ","},
		{`show {"a":1 "b":2}`, `"b"`}, {"show record {a:1 b:2}", "b"},
		{`show {"a":1,,}`, ",,"}, {"show record {a:1,,}", ",,"},
		{"show [1\n]", "\n"}, {"show {\"a\":1\n}", "\n"}, {"show record {a:1\n}", "\n"},
		{"show record\n{}", "\n"}, {"set x\nto 1", "\n"},
		{"show \"a\" joined\nwith \"b\"", "\n"}, {"show \"a\" joined with\n\"b\"", "\n"},
		{"if yes", "if"}, {"import thing", "import"}, {"show html \"x\"", `"x"`},
	} {
		bad("boundary/"+v.source, v.source, "syntax", v.fragment)
		if v.fragment == ",," {
			cases[len(cases)-1].fragment = ","
			cases[len(cases)-1].occurrence = 1
		}
	}
	ok("whitespace within collections", "show [\t1 , { \"x\" : record { a : yes , } , } , ]", "[1, {\"x\": record {a: yes}}]\n")
	ok("map and record snapshots", "set x to {\"a\":nothing}\nset y to record {x:x}\nset x to {}\nshow y\nshow x", "record {x: {\"a\": nothing}}\n{}\n")
	ok("binding references in every container", "set x to \"a\"\nshow [x, {\"x\":x}, record {x:x}]", "[\"a\", {\"x\": \"a\"}, record {x: \"a\"}]\n")
	for _, expr := range []string{`missing joined with second`, `[missing, second]`, `{"z":missing,"a":second}`, `record {z:missing,a:second}`} {
		bad("name order/"+expr, "show "+expr, "name", "missing")
	}
	bad("duplicate map blocks execution", "show \"before\"\nshow {\"a\":1,\"a\":2}", "duplicate map", `"a"`)
	cases[len(cases)-1].occurrence = 1
	bad("duplicate record blocks execution", "show \"before\"\nshow record {x:1,x:2}", "duplicate record", "x")
	cases[len(cases)-1].occurrence = 1
	return cases
}
