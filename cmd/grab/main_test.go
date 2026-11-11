package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTemp creates a temporary .grab file with the given source and returns its path.
func writeTemp(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "prog.grab")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("writeTemp: %v", err)
	}
	return path
}

func TestRun_CLI(t *testing.T) {
	cases := []struct {
		name       string
		args       []string        // arguments after the binary name
		sourceFile string          // non-empty: write this source to a temp file and append the path
		wantCode   int             // expected exit code
		wantOut    string          // expected stdout (exact)
		wantErrSub string          // expected substring in stderr (empty → no check)
	}{
		// ── Usage errors ──────────────────────────────────────────────────────
		{
			name:     "no arguments",
			args:     []string{},
			wantCode: 2,
			wantErrSub: "usage:",
		},
		{
			name:     "wrong subcommand",
			args:     []string{"execute"},
			wantCode: 2,
			wantErrSub: "usage:",
		},
		{
			name:     "too many arguments",
			args:     []string{"run", "a.grab", "extra"},
			wantCode: 2,
			wantErrSub: "usage:",
		},
		{
			name:     "run without file",
			args:     []string{"run"},
			wantCode: 2,
			wantErrSub: "usage:",
		},

		// ── Missing file ──────────────────────────────────────────────────────
		{
			name:       "missing file",
			args:       []string{"run", "does_not_exist.grab"},
			wantCode:   1,
			wantErrSub: "grab:",
		},

		// ── Valid programs ────────────────────────────────────────────────────
		{
			name:       "approved vertical slice",
			args:       []string{"run"},
			sourceFile: "set name to \"Allen\"\nset language to \"GRAB\"\nshow \"Welcome to \" joined with language\nshow \"Hello \" joined with name\n",
			wantCode:   0,
			wantOut:    "Welcome to GRAB\nHello Allen\n",
		},
		{
			name:       "show text literal",
			args:       []string{"run"},
			sourceFile: "show \"hello, world\"\n",
			wantCode:   0,
			wantOut:    "hello, world\n",
		},
		{
			name:       "show boolean",
			args:       []string{"run"},
			sourceFile: "show yes\nshow no\n",
			wantCode:   0,
			wantOut:    "yes\nno\n",
		},
		{
			name:       "show nothing",
			args:       []string{"run"},
			sourceFile: "show nothing\n",
			wantCode:   0,
			wantOut:    "nothing\n",
		},
		{
			name:       "show integer",
			args:       []string{"run"},
			sourceFile: "show 42\n",
			wantCode:   0,
			wantOut:    "42\n",
		},
		{
			name:       "show negative integer",
			args:       []string{"run"},
			sourceFile: "show -100\n",
			wantCode:   0,
			wantOut:    "-100\n",
		},
		{
			// Regression: single-digit negative integers must lex correctly
			// (fixed a bug in nextRuneIsDigit that peeked two chars ahead).
			name:       "show single-digit negative integer regression",
			args:       []string{"run"},
			sourceFile: "show -7\n",
			wantCode:   0,
			wantOut:    "-7\n",
		},
		{
			name:       "show exact decimal",
			args:       []string{"run"},
			sourceFile: "show 19.95\n",
			wantCode:   0,
			wantOut:    "19.95\n",
		},
		{
			// Regression: negative decimals must lex correctly after nextRuneIsDigit fix.
			name:       "show negative decimal regression",
			args:       []string{"run"},
			sourceFile: "show -5.5\n",
			wantCode:   0,
			wantOut:    "-5.5\n",
		},
		{
			name:       "show float",
			args:       []string{"run"},
			sourceFile: "show 1.25e-2\n",
			wantCode:   0,
			wantOut:    "0.0125\n",
		},
		{
			name:       "show bytes",
			args:       []string{"run"},
			sourceFile: "show b\"\\x00\\xff\"\n",
			wantCode:   0,
			wantOut:    "b\"\\x00\\xff\"\n",
		},
		{
			name:       "show list",
			args:       []string{"run"},
			sourceFile: "show [1, \"two\", nothing]\n",
			wantCode:   0,
			wantOut:    "[1, \"two\", nothing]\n",
		},
		{
			name:       "show empty list",
			args:       []string{"run"},
			sourceFile: "show []\n",
			wantCode:   0,
			wantOut:    "[]\n",
		},
		{
			name:       "show map",
			args:       []string{"run"},
			sourceFile: "show {\"sku\": \"A-17\", \"active\": yes}\n",
			wantCode:   0,
			wantOut:    "{\"sku\": \"A-17\", \"active\": yes}\n",
		},
		{
			name:       "show record",
			args:       []string{"run"},
			sourceFile: "show record {name: \"GRAB\", version: 1}\n",
			wantCode:   0,
			wantOut:    "record {name: \"GRAB\", version: 1}\n",
		},
		{
			name:       "set rebind different kinds",
			args:       []string{"run"},
			sourceFile: "set x to 99\nset x to \"now text\"\nshow x\n",
			wantCode:   0,
			wantOut:    "now text\n",
		},
		{
			name:       "joined with chained",
			args:       []string{"run"},
			sourceFile: "show \"a\" joined with \"b\" joined with \"c\"\n",
			wantCode:   0,
			wantOut:    "abc\n",
		},
		{
			name:       "comment only line ignored",
			args:       []string{"run"},
			sourceFile: "# this is a comment\nshow \"ok\"\n",
			wantCode:   0,
			wantOut:    "ok\n",
		},
		{
			name:       "portable values conformance case",
			args:       []string{"run"},
			sourceFile: "set product to record {sku: \"A-17\", active: yes, stock: 12, price: 19.95, ratio: 1.25e-2, labels: [\"new\", \"sale\"], payload: b\"\\x00\\xff\", note: nothing}\nshow product\n",
			wantCode:   0,
			wantOut:    "record {sku: \"A-17\", active: yes, stock: 12, price: 19.95, ratio: 0.0125, labels: [\"new\", \"sale\"], payload: b\"\\x00\\xff\", note: nothing}\n",
		},

		// ── Syntax errors ─────────────────────────────────────────────────────
		{
			name:       "unterminated string",
			args:       []string{"run"},
			sourceFile: "show \"Hello\n",
			wantCode:   1,
			wantErrSub: "grab:",
		},
		{
			name:       "malformed statement",
			args:       []string{"run"},
			sourceFile: "set\n",
			wantCode:   1,
			wantErrSub: "grab:",
		},
		{
			name:       "set without to",
			args:       []string{"run"},
			sourceFile: "set x 42\n",
			wantCode:   1,
			wantErrSub: "grab:",
		},
		{
			name:       "duplicate map key",
			args:       []string{"run"},
			sourceFile: "show {\"k\": 1, \"k\": 2}\n",
			wantCode:   1,
			wantErrSub: "grab:",
		},
		{
			name:       "duplicate record field",
			args:       []string{"run"},
			sourceFile: "show record {a: 1, a: 2}\n",
			wantCode:   1,
			wantErrSub: "grab:",
		},

		// ── Name-resolution errors (pre-execution, produce no output) ─────────
		{
			name:       "unbound variable reference",
			args:       []string{"run"},
			sourceFile: "show missing\n",
			wantCode:   1,
			wantOut:    "",
			wantErrSub: "grab:",
		},
		{
			name:       "forward reference blocked",
			args:       []string{"run"},
			sourceFile: "show x\nset x to \"late\"\n",
			wantCode:   1,
			wantOut:    "",
			wantErrSub: "grab:",
		},

		// ── Runtime errors ────────────────────────────────────────────────────
		{
			name:       "joined with non-text produces error",
			args:       []string{"run"},
			sourceFile: "show yes joined with \"x\"\n",
			wantCode:   1,
			wantErrSub: "grab:",
		},
		{
			name:       "runtime error after successful show retains earlier output",
			args:       []string{"run"},
			sourceFile: "show \"first\"\nshow yes joined with \"x\"\n",
			wantCode:   1,
			wantOut:    "first\n",
			wantErrSub: "grab:",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := make([]string, len(tc.args))
			copy(args, tc.args)

			// If a source file is specified, write it and append the path.
			if tc.sourceFile != "" {
				path := writeTemp(t, tc.sourceFile)
				args = append(args, path)
			}

			var stdout, stderr bytes.Buffer
			code := run(args, &stdout, &stderr)

			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d\nstdout: %q\nstderr: %q",
					code, tc.wantCode, stdout.String(), stderr.String())
			}
			if tc.wantOut != "" && stdout.String() != tc.wantOut {
				t.Errorf("stdout = %q, want %q", stdout.String(), tc.wantOut)
			}
			// When wantOut is "", only check that there is no unexpected output
			// for cases that explicitly set wantCode=0 (valid programs should not error).
			if tc.wantOut == "" && tc.wantCode == 0 && stdout.String() != "" {
				t.Errorf("expected no stdout, got %q", stdout.String())
			}
			if tc.wantErrSub != "" && !strings.Contains(stderr.String(), tc.wantErrSub) {
				t.Errorf("stderr = %q, want containing %q", stderr.String(), tc.wantErrSub)
			}
		})
	}
}
