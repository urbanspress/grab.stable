package interpreter

import (
	"io"
	"testing"

	"grab/internal/parser"
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
	}

	for _, wl := range workloads {
		program, err := parser.Parse(wl.name+".grab", wl.source)
		if err != nil {
			b.Fatalf("Parse(%s): %v", wl.name, err)
		}
		b.Run(wl.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := Run(program, io.Discard); err != nil {
					b.Fatalf("Run(%s): %v", wl.name, err)
				}
			}
		})
	}
}
