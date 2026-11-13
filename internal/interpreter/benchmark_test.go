package interpreter

import (
	"context"
	"io"
	"testing"

	"grab/internal/parser"
	grabrt "grab/internal/runtime"
)

func BenchmarkInterpreterWorkloads(b *testing.B) {
	workloads := []struct {
		name   string
		source string
	}{
		{
			name: "StraightLineAndJoin",
			source: `set first to "Allen"
set lang to "GRAB"
set msg to "Welcome to " joined with lang joined with ", " joined with first
show msg
`,
		},
		{
			name: "ArithmeticAndConditionals",
			source: `set subtotal to 250
set rate to 0.08
set tax to subtotal times rate
set total to subtotal plus tax
if total is greater than 200
  set tier to "gold"
otherwise if total is at least 100
  set tier to "silver"
otherwise
  set tier to "standard"
end
show tier
`,
		},
		{
			name: "CollectionIterationAndLookup",
			source: `set product to record {sku: "A-17", tags: ["alpha", "beta", "gamma", "delta"]}
set count to 0
for each tag in product.tags
  if product has sku
    set count to count plus 1
  end
end
show count
`,
		},
		{
			name: "FunctionsAndCallFrames",
			source: `make fib using n
  if n is at most 1
    give back n
  end
  give back (fib using n minus 1) plus (fib using n minus 2)
end
set result to fib using 8
show result
`,
		},
		{
			name: "TightLoopArithmetic",
			source: `set sum to 0
set i to 0
while i is less than 2000
  set sum to sum plus i
  set i to i plus 1
end
show sum
`,
		},
		{
			name: "TryCatchHappyPath",
			source: `make safe_step using x
  try
    give back x plus 1
  catch err
    give back 0
  end
end
set total to 0
repeat 200 times
  set total to total plus (safe_step using total)
end
show total
`,
		},
	}

	engines := []struct {
		label string
		mode  EngineMode
	}{
		{label: "AST", mode: EngineAST},
		{label: "VM", mode: EngineVM},
		{label: "Auto", mode: EngineAuto},
	}

	for _, wl := range workloads {
		program, err := parser.Parse(wl.name+".grab", wl.source)
		if err != nil {
			b.Fatalf("Parse(%s): %v", wl.name, err)
		}
		for _, eng := range engines {
			b.Run(wl.name+"/"+eng.label, func(b *testing.B) {
				opts := Options{Engine: eng.mode}
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if err := RunWithOptions(program, io.Discard, opts); err != nil {
						b.Fatalf("Run(%s/%s): %v", wl.name, eng.label, err)
					}
				}
			})
		}
		// Prepared execution isolates steady-state engine speed from one-time validation/manager setup.
		for _, eng := range engines {
			b.Run("Prepared/"+wl.name+"/"+eng.label, func(b *testing.B) {
				prep, err := Prepare(program, Options{Engine: eng.mode}, io.Discard)
				if err != nil {
					b.Fatalf("Prepare(%s/%s): %v", wl.name, eng.label, err)
				}
				defer prep.Close()
				ctx := context.Background()
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if _, _, err := prep.Execute(ctx, nil, grabrt.Nothing(), io.Discard); err != nil {
						b.Fatalf("Execute(%s/%s): %v", wl.name, eng.label, err)
					}
				}
			})
		}
	}
}
