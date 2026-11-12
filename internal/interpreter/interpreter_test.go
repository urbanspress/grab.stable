package interpreter

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

func TestGRAB05JSONAndUnifiedLookup(t *testing.T) {
	src := `use json

set data to json.parse using "{\"sku\": \"A-17\", \"price\": 19.95, \"stock\": 12, \"ratio\": 1.5e2, \"active\": true, \"note\": null, \"tags\": [\"new\", \"sale\"], \"meta-info\": {\"x-rate\": 5}}"
show data.sku
show data at "sku"
show data has sku
show data has "sku"
show data.price plus 0.05
show data.stock plus 8
show data.ratio
show data.active
show data.note
show data.tags at 1
set meta to data at "meta-info"
show meta at "x-rate"

set m to {"alpha": 100, "beta-key": 200}
show m.alpha
show m has alpha
show m has "beta-key"

set compact to json.stringify using record {sku: "A-17", price: 19.95, active: yes, note: nothing, items: [1, 2]}
show compact
set pretty to json.stringify_pretty using record {sku: "A-17", active: no}
show pretty
`
	prog, err := parser.Parse("json05.grab", src)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	var out bytes.Buffer
	if err := Run(prog, &out); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := strings.Join([]string{
		"A-17",
		"A-17",
		"yes",
		"yes",
		"20.00",
		"20",
		"150.0",
		"yes",
		"nothing",
		"sale",
		"5",
		"100",
		"yes",
		"yes",
		`{"sku":"A-17","price":19.95,"active":true,"note":null,"items":[1,2]}`,
		"{\n  \"sku\": \"A-17\",\n  \"active\": false\n}",
	}, "\n") + "\n"
	if out.String() != want {
		t.Fatalf("output =\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestGRAB05DoTogetherConcurrencyAndValidation(t *testing.T) {
	cases := []struct {
		name      string
		source    string
		output    string
		errorText string
	}{
		{
			name: "concurrent tasks commit and buffer output in top-to-bottom order",
			source: `use text
make work using label, amount
  show "task " joined with label
  give back amount times 2
end

set base to 10
do together
  set first to wait for (work using "one", base)
  set second to work using "two", (base plus 5)
  wait for work using "three", 1
end
show first
show second
`,
			output: "task one\ntask two\ntask three\n20\n30\n",
		},
		{
			name: "duplicate target variable in do together rejected preflight with GRAB-E208",
			source: `do together
  set item to 1
  set item to 2
end
`,
			errorText: "invalid task group \"item\"",
		},
		{
			name: "more than 16 tasks in do together rejected preflight with GRAB-E208",
			source: `do together
  set t1 to 1
  set t2 to 2
  set t3 to 3
  set t4 to 4
  set t5 to 5
  set t6 to 6
  set t7 to 7
  set t8 to 8
  set t9 to 9
  set t10 to 10
  set t11 to 11
  set t12 to 12
  set t13 to 13
  set t14 to 14
  set t15 to 15
  set t16 to 16
  set t17 to 17
end
`,
			errorText: "invalid task group at line 1",
		},
		{
			name: "error in do together suppresses partial commits and propagates earliest source order",
			source: `make fail_task using msg
  show "before " joined with msg
  raise msg
end

set safe to "initial"
try
  do together
    set a to wait for (fail_task using "first failure")
    set safe to wait for (fail_task using "second failure")
  end
catch err
  show "caught: " joined with err.code joined with " / " joined with err.message
end
show safe
`,
			output: "caught: GRAB-E308 / first failure\ninitial\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := parser.Parse("dotogether.grab", tc.source)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			var out bytes.Buffer
			err = Run(prog, &out)
			if out.String() != tc.output {
				t.Fatalf("output = %q, want %q", out.String(), tc.output)
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

type testPipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
}

func newTestPipeListener() *testPipeListener {
	return &testPipeListener{
		conns:  make(chan net.Conn),
		closed: make(chan struct{}),
	}
}

func (l *testPipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *testPipeListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return nil
}

func (l *testPipeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
}

func (l *testPipeListener) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	client, srv := net.Pipe()
	select {
	case l.conns <- srv:
		return client, nil
	case <-l.closed:
		client.Close()
		srv.Close()
		return nil, net.ErrClosed
	case <-ctx.Done():
		client.Close()
		srv.Close()
		return nil, ctx.Err()
	}
}

func TestGRAB05HTTPClientCapabilitiesAndSSRF(t *testing.T) {
	var flakyAttempts int32
	pl := newTestPipeListener()
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/item":
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Custom", "GRAB-05")
				_, _ = w.Write([]byte(`{"sku":"A-17","price":19.95,"stock":12}`))
			case "/echo":
				w.Header().Set("Content-Type", "application/json")
				buf := new(bytes.Buffer)
				_, _ = buf.ReadFrom(r.Body)
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write(buf.Bytes())
			case "/flaky":
				n := atomic.AddInt32(&flakyAttempts, 1)
				if n < 3 {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte("busy"))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"recovered":true}`))
			case "/slow":
				time.Sleep(150 * time.Millisecond)
				_, _ = w.Write([]byte(`{"slow":true}`))
			case "/redirect-blocked":
				http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}),
	}
	go func() {
		_ = srv.Serve(pl)
	}()
	defer func() {
		_ = srv.Close()
		_ = pl.Close()
	}()
	baseURL := "http://127.0.0.1:8080"
	allowHost := "127.0.0.1:8080"

	// 1. Default-deny network permissions without --allow-net must fail with GRAB-E402.
	denySrc := `use http
show http.get using "` + baseURL + `/item"
`
	denyProg, err := parser.Parse("deny.grab", denySrc)
	if err != nil {
		t.Fatal(err)
	}
	err = Run(denyProg, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "network permission denied") {
		t.Fatalf("expected network permission denied, got %v", err)
	}

	// 2. Allowed 127.0.0.1:8080 host succeeds for GET, GET_JSON, POST, retries, and do together.
	allowSrc := `use http
use json
do together
  set item to wait for (http.get_json using "` + baseURL + `/item")
  set created to wait for (http.post using "` + baseURL + `/echo", record {order: "ORD-1", total: 42.50})
  set retried to wait for (http.get using "` + baseURL + `/flaky", record {retries: 2, timeout_ms: 5000})
end
show item.sku
show item.price plus 0.05
show created.status
show created.ok
set created_data to json.parse using created.body
show created_data.order
show created_data.total
show retried.status
set retried_data to json.parse using retried.body
show retried_data.recovered
`
	allowProg, err := parser.Parse("allow.grab", allowSrc)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = RunWithOptions(allowProg, &out, Options{
		Permissions: Permissions{AllowNet: []string{allowHost}},
		DialContext: pl.DialContext,
	})
	if err != nil {
		t.Fatalf("RunWithOptions() error = %v", err)
	}
	want := "A-17\n20.00\n201\nyes\nORD-1\n42.50\n200\nyes\n"
	if out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}

	// 3. Request timeout raises GRAB-E311.
	timeoutSrc := `use http
try
  show http.get using "` + baseURL + `/slow", record {timeout_ms: 25}
catch err
  show err.code
end
`
	timeoutProg, err := parser.Parse("timeout.grab", timeoutSrc)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := RunWithOptions(timeoutProg, &out, Options{
		Permissions: Permissions{AllowNet: []string{allowHost}},
		DialContext: pl.DialContext,
	}); err != nil {
		t.Fatalf("timeout run error = %v", err)
	}
	if out.String() != "GRAB-E311\n" {
		t.Fatalf("timeout output = %q, want GRAB-E311", out.String())
	}

	// 4. Redirect to cloud metadata 169.254.169.254 is always blocked with GRAB-E402 even if allowlisted.
	ssrfSrc := `use http
try
  show http.get using "` + baseURL + `/redirect-blocked"
catch err
  show err.code
end
`
	ssrfProg, err := parser.Parse("ssrf.grab", ssrfSrc)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := RunWithOptions(ssrfProg, &out, Options{
		Permissions: Permissions{AllowNet: []string{allowHost, "169.254.169.254"}},
		DialContext: pl.DialContext,
	}); err != nil {
		t.Fatalf("ssrf run error = %v", err)
	}
	if out.String() != "GRAB-E402\n" {
		t.Fatalf("ssrf output = %q, want GRAB-E402", out.String())
	}
}
