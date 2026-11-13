package tests

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"grab/internal/data"
	"grab/internal/interpreter"
	"grab/internal/parser"
	"grab/internal/server"
)

func assertASTAndVMParity(t *testing.T, name, source string, opts interpreter.Options) {
	t.Helper()
	prog, parseErr := parser.Parse(name+".grab", source)
	if parseErr != nil {
		return
	}

	runWith := func(mode interpreter.EngineMode) (string, error) {
		var out bytes.Buffer
		o := opts
		o.Engine = mode
		if mode == interpreter.EngineAuto {
			o.HotFunctionThreshold = 2
			o.HotLoopThreshold = 2
		}
		err := interpreter.RunWithOptions(prog, &out, o)
		return out.String(), err
	}

	astOut, astErr := runWith(interpreter.EngineAST)
	vmOut, vmErr := runWith(interpreter.EngineVM)
	autoOut, autoErr := runWith(interpreter.EngineAuto)

	if vmOut != astOut {
		t.Fatalf("%s: VM stdout %q != AST stdout %q", name, vmOut, astOut)
	}
	if autoOut != astOut {
		t.Fatalf("%s: Auto stdout %q != AST stdout %q", name, autoOut, astOut)
	}

	checkErrParity := func(label string, gotErr error) {
		if (astErr == nil) != (gotErr == nil) {
			t.Fatalf("%s: %s err=%v, AST err=%v", name, label, gotErr, astErr)
		}
		if astErr == nil {
			return
		}
		astCode, astCat, astSpan, astStack := interpreter.ExtractErrorMeta(astErr)
		gotCode, gotCat, gotSpan, gotStack := interpreter.ExtractErrorMeta(gotErr)
		if gotCode != astCode || gotCat != astCat || gotSpan != astSpan {
			t.Fatalf("%s: %s error meta = (%s, %s, %+v), want (%s, %s, %+v)",
				name, label, gotCode, gotCat, gotSpan, astCode, astCat, astSpan)
		}
		if len(gotStack) != len(astStack) {
			t.Fatalf("%s: %s stack len %d != AST %d (%v vs %v)", name, label, len(gotStack), len(astStack), gotStack, astStack)
		}
		for i := range astStack {
			if gotStack[i] != astStack[i] {
				t.Fatalf("%s: %s stack[%d] = %+v, want %+v", name, label, i, gotStack[i], astStack[i])
			}
		}
		astDiag := interpreter.FormatDiagnostic(source, astErr, interpreter.ModeDeveloper)
		gotDiag := interpreter.FormatDiagnostic(source, gotErr, interpreter.ModeDeveloper)
		if gotDiag != astDiag {
			t.Fatalf("%s: %s diagnostic %q != AST %q", name, label, gotDiag, astDiag)
		}
	}

	checkErrParity("VM", vmErr)
	checkErrParity("Auto", autoErr)
}

func TestGRAB09ConformanceParityWithAST(t *testing.T) {
	for _, tc := range specificationCases() {
		t.Run("01/"+tc.name, func(t *testing.T) {
			assertASTAndVMParity(t, "c01", tc.source, interpreter.Options{})
		})
	}
	for _, tc := range grab03SpecificationCases() {
		t.Run("03/"+tc.name, func(t *testing.T) {
			assertASTAndVMParity(t, "c03", tc.source, interpreter.Options{})
		})
	}
	for _, tc := range grab04SpecificationCases() {
		t.Run("04/"+tc.name, func(t *testing.T) {
			assertASTAndVMParity(t, "c04", tc.source, interpreter.Options{})
		})
	}
}

func TestGRAB09EdgeCasesAndConcurrencyParity(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{
			name: "for_each_and_catch_shadowing",
			source: `set item to "outer"
set err to "outer-err"
for each item in ["a", "b"]
  show item
end
show item
try
  raise "boom"
catch err
  show err.code
  show err.message
end
show err
`,
		},
		{
			name: "unexecuted_overflow_float_literal_and_caught_overflow",
			source: `if no
  set bad to 1e999
  show bad
end
try
  set bad to 1e999
  show bad
catch e
  show e.code
end
`,
		},
		{
			name: "nested_try_across_function_calls_and_stack_trace",
			source: `make inner using x
  if x is 0
    raise record {code: "E-CUSTOM", message: "zero input"}
  end
  give back 100 divided by x
end
make outer using x
  give back inner using x
end
try
  set v to outer using 0
  show v
catch e
  show e.code
  show e.message
end
set okv to outer using 4
show okv
`,
		},
		{
			name: "do_together_snapshot_source_order_and_cancellation",
			source: `make slow using label
  set sum to 0
  repeat 5 times
    set sum to sum plus 1
  end
  show label
  give back sum
end
set x to 10
do together
  set a to wait for slow using "first"
  set b to x plus 5
  set c to wait for slow using "third"
end
show a
show b
show c
try
  do together
    set r1 to 10 divided by 0
    set r2 to 20 plus 1
  end
catch err
  show err.code
end
`,
		},
		{
			name: "mixed_tier_hot_function_promotion_and_recursion",
			source: `make step_calc using n
  set acc to 0
  repeat n times
    set acc to acc plus 2
  end
  give back acc
end
set total to 0
repeat 25 times
  set total to total plus (step_calc using 3)
end
show total
`,
		},
		{
			name: "fatal_loop_limit_uncatchable_in_try",
			source: `try
  while yes
    set x to 1
  end
catch err
  show "must not catch"
end
`,
		},
		{
			name: "fatal_call_depth_uncatchable_in_try",
			source: `make recurse using n
  give back recurse using n plus 1
end
try
  set r to recurse using 0
catch err
  show "must not catch"
end
`,
		},
		{
			name: "short_circuit_and_or_with_division_by_zero",
			source: `if no and ((1 divided by 0) is 1)
  show "bad"
otherwise
  show "and-ok"
end
if yes or ((1 divided by 0) is 1)
  show "or-ok"
end
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertASTAndVMParity(t, tc.name, tc.source, interpreter.Options{})
		})
	}
}

func TestGRAB09DatabaseTransactionScopeRollbackParity(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "db.json")
	raw := []byte(`{"connections":{"mem":{"driver":"sqlite","path":":memory:"}}}`)
	cfg, err := data.ParseConfig(raw, cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := cfg.Grant([]string{"mem:migrate"}, false)
	if err != nil {
		t.Fatal(err)
	}

	source := `use data
set db to data.open using "mem"
call data.migrate using db, [record {version: 1, name: "init_items", steps: ["CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT NOT NULL)"]}]
set ins to data.prepare using db, "INSERT INTO items (id, name) VALUES (:id, :name)", record {params: record {id: "integer", name: "text"}, columns: record {}}
set sel to data.prepare using db, "SELECT COUNT(*) AS cnt FROM items", record {params: record {}, columns: record {cnt: "integer"}}

make insert_uncommitted using conn, stmt
  set tx to data.begin using conn
  call data.execute using stmt, record {id: 1, name: "temp"}
  give back "done"
end

set res to insert_uncommitted using db, ins
show res
set row1 to data.one using sel
show row1.cnt

try
  set tx2 to data.begin using db
  call data.execute using ins, record {id: 2, name: "fail"}
  raise "abort"
catch e
  show e.code
end
set row2 to data.one using sel
show row2.cnt

set tx3 to data.begin using db
call data.execute using ins, record {id: 3, name: "kept"}
call data.commit using tx3
set row3 to data.one using sel
show row3.cnt
`
	assertASTAndVMParity(t, "db_tx_rollback", source, interpreter.Options{DataGrants: grants})
}

func TestGRAB09ServerPreparedAppParityAndConcurrentWarmup(t *testing.T) {
	dir := t.TempDir()
	appPath := filepath.Join(dir, "app.grab")
	tmplPath := filepath.Join(dir, "page.grab.html")
	if err := os.WriteFile(tmplPath, []byte("<h1>{title}</h1><p>{total}</p>"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := `use server

make compute_price using base, qty
  set sum to 0
  repeat qty times
    set sum to sum plus base
  end
  give back sum
end

set store_name to "GRAB Shop"

route "GET" "/items/{id}" using req
  set total to compute_price using 19.95, 3
  give back server.json using 200, record {shop: store_name, id: req.params.id, total: total}
end

route "GET" "/page/{name}" using req
  set total to compute_price using 10, 4
  give back server.html using "page.grab.html", record {title: req.params.name, total: total}
end
`
	if err := os.WriteFile(appPath, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	runServerMode := func(mode interpreter.EngineMode) (string, string) {
		opts := interpreter.Options{
			Engine:               mode,
			HotFunctionThreshold: 4,
			HotLoopThreshold:     4,
			HotRouteThreshold:    4,
		}
		scfg := server.DefaultConfig()
		app, err := server.NewApplication(appPath, "", opts, scfg, nil)
		if err != nil {
			t.Fatalf("NewApplication(%s): %v", mode, err)
		}
		defer app.Close()

		// Exercise concurrent requests across cold->warm promotion.
		var wg sync.WaitGroup
		for g := 0; g < 16; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rec1 := httptest.NewRecorder()
				app.ServeHTTP(rec1, httptest.NewRequest("GET", "/items/A17", nil))
				if rec1.Code != 200 {
					t.Errorf("%s /items/A17 status = %d", mode, rec1.Code)
				}
				rec2 := httptest.NewRecorder()
				app.ServeHTTP(rec2, httptest.NewRequest("GET", "/page/Catalog", nil))
				if rec2.Code != 200 {
					t.Errorf("%s /page/Catalog status = %d", mode, rec2.Code)
				}
			}()
		}
		wg.Wait()

		r1 := httptest.NewRecorder()
		app.ServeHTTP(r1, httptest.NewRequest("GET", "/items/A17", nil))
		r2 := httptest.NewRecorder()
		app.ServeHTTP(r2, httptest.NewRequest("GET", "/page/Catalog", nil))
		return r1.Body.String(), r2.Body.String()
	}

	astJSON, astHTML := runServerMode(interpreter.EngineAST)
	vmJSON, vmHTML := runServerMode(interpreter.EngineVM)
	autoJSON, autoHTML := runServerMode(interpreter.EngineAuto)

	if vmJSON != astJSON || autoJSON != astJSON {
		t.Fatalf("JSON mismatch: ast=%q vm=%q auto=%q", astJSON, vmJSON, autoJSON)
	}
	if vmHTML != astHTML || autoHTML != astHTML {
		t.Fatalf("HTML mismatch: ast=%q vm=%q auto=%q", astHTML, vmHTML, autoHTML)
	}
}
