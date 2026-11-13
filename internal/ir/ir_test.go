package ir

import (
	"testing"

	"grab/internal/parser"
)

func TestCompileProgramAndSlotResolution(t *testing.T) {
	src := `use data
make add using a, b
  set sum to a plus b
  give back sum
end
set x to 10
for each item in [1, 2, 3]
  set x to x plus item
end
try
  set r to add using x, 5
  show r
catch err
  show err.code
end
`
	prog, err := parser.Parse("test.grab", src)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	irProg, err := CompileProgram(prog, nil)
	if err != nil {
		t.Fatalf("CompileProgram failed: %v", err)
	}
	if !irProg.UsesData {
		t.Fatalf("expected UsesData=true")
	}
	if len(irProg.Functions) != 1 || irProg.Functions[0].Name != "add" {
		t.Fatalf("expected 1 function 'add', got %+v", irProg.Functions)
	}
	if irProg.Functions[0].NumParams != 2 {
		t.Fatalf("expected 2 params, got %d", irProg.Functions[0].NumParams)
	}
	if len(irProg.TopLevel.Handlers) != 1 {
		t.Fatalf("expected 1 exception handler, got %d", len(irProg.TopLevel.Handlers))
	}
	// Verify OpTxEnter/OpTxExit emitted when UsesData is true.
	foundTx := false
	for _, instr := range irProg.TopLevel.Code {
		if instr.Op == OpTxEnter {
			foundTx = true
			break
		}
	}
	if !foundTx {
		t.Fatalf("expected OpTxEnter in UsesData program")
	}
}

func TestCompileProgramOmitsTxOpsWithoutData(t *testing.T) {
	src := `set x to 1
repeat 3 times
  set x to x plus 1
end
show x
`
	prog, err := parser.Parse("nodata.grab", src)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	irProg, err := CompileProgram(prog, nil)
	if err != nil {
		t.Fatalf("CompileProgram failed: %v", err)
	}
	if irProg.UsesData {
		t.Fatalf("expected UsesData=false")
	}
	for _, instr := range irProg.TopLevel.Code {
		if instr.Op == OpTxEnter || instr.Op == OpTxExit {
			t.Fatalf("unexpected tx op %s without use data", instr.Op)
		}
	}
}
