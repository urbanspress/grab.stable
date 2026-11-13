package vm

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"grab/internal/ir"
	"grab/internal/lexer"
	"grab/internal/parser"
)

type testProgramLookup struct {
	funcs []*Chunk
	names []string
}

func (l *testProgramLookup) FuncChunk(idx int) (*Chunk, string) {
	if idx < 0 || idx >= len(l.funcs) {
		return nil, ""
	}
	return l.funcs[idx], l.names[idx]
}

func compileAndRunTestVM(t *testing.T, src string) (string, error) {
	t.Helper()
	prog, err := parser.Parse("vm_test.grab", src)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	irProg, err := ir.CompileProgram(prog, nil)
	if err != nil {
		t.Fatalf("IR compile error: %v", err)
	}
	lookup := &testProgramLookup{
		funcs: make([]*Chunk, len(irProg.Functions)),
		names: make([]string, len(irProg.Functions)),
	}
	for i, fn := range irProg.Functions {
		c, err := CompileUnit(fn, len(irProg.Functions))
		if err != nil {
			t.Fatalf("CompileUnit(%s) error: %v", fn.Name, err)
		}
		lookup.funcs[i] = c
		lookup.names[i] = fn.Name
	}
	topChunk, err := CompileUnit(irProg.TopLevel, len(irProg.Functions))
	if err != nil {
		t.Fatalf("CompileUnit(top) error: %v", err)
	}
	var out bytes.Buffer
	state := &ExecState{
		Ctx:          context.Background(),
		Output:       &out,
		MaxLoopSteps: DefaultMaxLoopSteps,
		MaxCallDepth: DefaultMaxCallDepth,
	}
	_, _, execErr := Execute(topChunk, lookup, state, nil, nil, nil, false)
	return out.String(), execErr
}

func TestVMArithmeticRecursionTryCatch(t *testing.T) {
	src := `make fib using n
  if n is at most 1
    give back n
  end
  give back (fib using n minus 1) plus (fib using n minus 2)
end

set total to fib using 8
show total

try
  set bad to 10 divided by 0
  show bad
catch err
  show err.code
end
`
	out, err := compileAndRunTestVM(t, src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "21\nGRAB-E302\n"
	if out != want {
		t.Fatalf("got output %q, want %q", out, want)
	}
}

func TestVMLoopAndCallDepthGuards(t *testing.T) {
	// Loop limit
	_, err := compileAndRunTestVM(t, `set i to 0
while yes
  set i to i plus 1
end
`)
	var re *RuntimeError
	if !errors.As(err, &re) || re.Code() != "GRAB-E307" || !re.IsFatal() {
		t.Fatalf("expected fatal GRAB-E307, got %v", err)
	}

	// Call depth limit
	_, err = compileAndRunTestVM(t, `make inf using x
  give back inf using x plus 1
end
set r to inf using 0
`)
	if !errors.As(err, &re) || re.Code() != "GRAB-E306" || !re.IsFatal() {
		t.Fatalf("expected fatal GRAB-E306, got %v", err)
	}
}

func TestVMVerifierRejectsMalformedChunk(t *testing.T) {
	bad := &Chunk{
		Name: "bad",
		Code: []Instr{
			{Op: ir.OpPop}, // stack underflow at pc 0!
			{Op: ir.OpReturnNothing},
		},
		Spans: make([]lexer.Span, 2),
	}
	if err := VerifyChunk(bad, 0); err == nil {
		t.Fatalf("expected verifier to reject malformed chunk")
	}
}
