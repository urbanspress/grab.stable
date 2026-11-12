package tests

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"grab/internal/interpreter"
	"grab/internal/parser"
)

type grab05PipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
}

func newGRAB05PipeListener() *grab05PipeListener {
	return &grab05PipeListener{
		conns:  make(chan net.Conn),
		closed: make(chan struct{}),
	}
}

func (l *grab05PipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *grab05PipeListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return nil
}

func (l *grab05PipeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
}

func (l *grab05PipeListener) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
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

func TestGRAB05SpecificationConformance(t *testing.T) {
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

	for _, tc := range grab05SpecificationCases() {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "case05.grab")
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

	t.Run("HTTP wire round-trip retries redirects and SSRF protection", func(t *testing.T) {
		var flakyCount int32
		var leakedAuthHeader atomic.Bool

		pl := newGRAB05PipeListener()
		srv := &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/product":
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"sku":"A-17","price":19.95,"stock":12,"meta-tag":{"warehouse":"east"}}`))
				case "/orders":
					w.Header().Set("Content-Type", "application/json")
					buf := new(bytes.Buffer)
					_, _ = buf.ReadFrom(r.Body)
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte(`{"method":"` + r.Method + `","received":` + buf.String() + `}`))
				case "/flaky":
					if atomic.AddInt32(&flakyCount, 1) == 1 {
						w.WriteHeader(http.StatusServiceUnavailable)
						_, _ = w.Write([]byte("retry later"))
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"ok":true}`))
				case "/redirect-cross-host":
					http.Redirect(w, r, "http://localhost:8080/redirect-target", http.StatusFound)
				case "/redirect-target":
					if r.Header.Get("Authorization") != "" {
						leakedAuthHeader.Store(true)
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"redirected":true}`))
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

		src := `use http
use json

do together
  set item to wait for (http.get_json using "http://127.0.0.1:8080/product")
  set created to wait for (http.post using "http://127.0.0.1:8080/orders", record {sku: "A-17", qty: 2})
  set updated to wait for (http.put using "http://127.0.0.1:8080/orders", record {sku: "A-17", qty: 5})
  set patched to wait for (http.patch using "http://127.0.0.1:8080/orders", record {qty: 3})
  set deleted to wait for (http.delete using "http://127.0.0.1:8080/product")
  set custom to wait for (http.request using record {method: "GET", url: "http://127.0.0.1:8080/flaky", retries: 2, timeout_ms: 5000})
  set redir to wait for (http.get using "http://127.0.0.1:8080/redirect-cross-host", record {headers: {"Authorization": "Bearer secret"}})
end

show item.sku
show item.price plus 0.05
set meta to item at "meta-tag"
show meta.warehouse
set c_body to json.parse using created.body
show c_body.method joined with ":" joined with c_body.received.sku
set u_body to json.parse using updated.body
show u_body.method
set p_body to json.parse using patched.body
show p_body.method
show deleted.status
set f_body to json.parse using custom.body
show f_body.ok
show redir.url
`
		prog, err := parser.Parse("http05.grab", src)
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		var out bytes.Buffer
		err = interpreter.RunWithOptions(prog, &out, interpreter.Options{
			Permissions: interpreter.Permissions{
				AllowNet: []string{"127.0.0.1:8080", "localhost:8080"},
			},
			DialContext: pl.DialContext,
		})
		if err != nil {
			t.Fatalf("RunWithOptions: %v", err)
		}
		if leakedAuthHeader.Load() {
			t.Fatalf("expected Authorization header to be stripped on cross-host redirect")
		}
		want := strings.Join([]string{
			"A-17",
			"20.00",
			"east",
			"POST:A-17",
			"PUT",
			"PATCH",
			"200",
			"yes",
			"http://localhost:8080/redirect-target",
		}, "\n") + "\n"
		if out.String() != want {
			t.Fatalf("output =\n%s\nwant:\n%s", out.String(), want)
		}
	})

	t.Run("CLI check format and allow-net flags for GRAB 0.5", func(t *testing.T) {
		dir := t.TempDir()
		grabPath := filepath.Join(dir, "tasks.grab")
		unformatted := "use   json\ndo   together\nset   a   to   wait   for   (json.parse using \"{\\\"x\\\": 10}\")\nset   b   to   20\nend\nshow   a.x plus b\n"
		if err := os.WriteFile(grabPath, []byte(unformatted), 0o600); err != nil {
			t.Fatal(err)
		}

		var stdout, stderr bytes.Buffer
		cmd := exec.Command(binary, "check", grabPath)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil || stdout.Len() != 0 || stderr.Len() != 0 {
			t.Fatalf("grab check failed: err=%v stdout=%q stderr=%q", err, stdout.String(), stderr.String())
		}

		stdout.Reset()
		stderr.Reset()
		cmd = exec.Command(binary, "format", "--write", grabPath)
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("grab format --write failed: %v stderr=%q", err, stderr.String())
		}
		formatted, err := os.ReadFile(grabPath)
		if err != nil {
			t.Fatal(err)
		}
		wantFormatted := "use json\ndo together\n  set a to wait for (json.parse using \"{\\\"x\\\": 10}\")\n  set b to wait for 20\nend\nshow a.x plus b\n"
		if string(formatted) != wantFormatted {
			t.Fatalf("formatted =\n%s\nwant:\n%s", string(formatted), wantFormatted)
		}
	})
}

func grab05SpecificationCases() []conformanceCase {
	return []conformanceCase{
		{
			name: "Section 3: use json lossless numbers and unified record/map lookup",
			source: `use json
set parsed to json.parse using "{\"sku\": \"A-17\", \"price\": 19.95, \"qty\": 3, \"tags\": [\"new\", \"featured\"], \"custom-meta\": {\"tier\": \"gold\"}}"
show parsed.sku
show parsed at "sku"
show parsed has sku
show parsed has "sku"
show parsed.price times parsed.qty
show parsed.tags at 1
set meta to parsed at "custom-meta"
show meta.tier
show json.stringify using record {sku: parsed.sku, total: parsed.price times parsed.qty, active: yes}
`,
			output: "A-17\nA-17\nyes\nyes\n59.85\nfeatured\ngold\n{\"sku\":\"A-17\",\"total\":59.85,\"active\":true}\n",
		},
		{
			name: "Section 6: do together snapshot isolation and deterministic top-to-bottom output",
			source: `use text
make format_item using label, price
  show "building " joined with label
  give back label joined with ": $" joined with (text.from using price)
end

set tax_multiplier to 1.10
do together
  set first_badge to wait for (format_item using " Coffee ", (10.00 times tax_multiplier))
  set second_badge to format_item using " Mug ", (15.00 times tax_multiplier)
  wait for format_item using " Sticker ", 2.50
end
show text.trim using first_badge
show text.trim using second_badge
`,
			output: "building  Coffee \nbuilding  Mug \nbuilding  Sticker \nCoffee : $11.0000\nMug : $16.5000\n",
		},
		{
			name: "Section 6: do together error handling inside try/catch",
			source: `use http
try
  do together
    set res to wait for (http.get using "https://api.example.com/items")
  end
catch err
  show err.code joined with " / " joined with err.category
end
`,
			output: "GRAB-E402 / network permission denied\n",
		},
		{
			name:     "Section 2: reserved keyword do cannot be used as variable identifier",
			source:   "set do to 1\n",
			category: "syntax",
			fragment: "do",
		},
		{
			name:     "Section 2: reserved keyword together cannot be used as variable identifier",
			source:   "set together to 1\n",
			category: "syntax",
			fragment: "together",
		},
		{
			name:     "Section 2: reserved keyword wait cannot be used as variable identifier",
			source:   "set wait to 1\n",
			category: "syntax",
			fragment: "wait",
		},
		{
			name:     "Section 5: default-deny network permission raises GRAB-E402",
			source:   "use http\nshow http.get using \"https://api.example.com/status\"\n",
			category: "network permission denied",
			fragment: "http.get using \"https://api.example.com/status\"",
		},
		{
			name:     "Section 5: cloud metadata 169.254.169.254 is blocked with GRAB-E402",
			source:   "use http\nshow http.get using \"http://169.254.169.254/latest/meta-data/\"\n",
			category: "network permission denied",
			fragment: "http.get using \"http://169.254.169.254/latest/meta-data/\"",
		},
		{
			name:     "Section 4: invalid URL scheme raises network error GRAB-E310",
			source:   "use http\nshow http.get using \"ftp://example.com/file\"\n",
			category: "network error",
			fragment: "http.get using \"ftp://example.com/file\"",
		},
		{
			name:       "Section 6: duplicate target variable in do together raises GRAB-E208",
			source:     "do together\n  set dup to 1\n  set dup to 2\nend\n",
			category:   "invalid task group",
			fragment:   "dup",
			occurrence: 1,
		},
	}
}
