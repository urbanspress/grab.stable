package vm

import (
	"fmt"

	"grab/internal/ir"
	"grab/internal/lexer"
	grabrt "grab/internal/runtime"
)

// Instr is a compact 16-byte VM instruction. Source spans are stored separately
// in Chunk.Spans to keep the hot dispatch loop cache-friendly.
type Instr struct {
	Op ir.Op
	A  int32
	B  int32
	C  int32
}

// CompiledTask is a compiled concurrent task inside a DoTogether block.
type CompiledTask struct {
	Name       string
	TargetSlot int32
	IsCallStmt bool
	Chunk      *Chunk
	Span       lexer.Span
}

// CompiledDoTogether holds the compiled tasks and captured slot indices.
type CompiledDoTogether struct {
	Tasks        []CompiledTask
	CaptureSlots []int32
	Span         lexer.Span
}

// Chunk is an immutable, verified bytecode unit ready for execution.
type Chunk struct {
	Name          string
	Kind          ir.UnitKind
	Code          []Instr
	Spans         []lexer.Span
	Constants     []grabrt.Value
	Strings       []string
	AuxSpans      []lexer.Span
	KeyLists      [][]string
	HostCalls     []ir.HostCallRef
	Handlers      []ir.ExceptionHandler
	DoTogethers   []CompiledDoTogether
	NumLocals     int
	NumParams     int
	NumIters      int
	MaxTxDepth    int
	MaxStack      int
	RootSlots     []ir.SlotInfo
	BaseSlotCount int
	UsesData      bool
}

// CompileUnit converts an IR Unit into a verified bytecode Chunk.
func CompileUnit(u *ir.Unit, numFuncs int) (*Chunk, error) {
	if u == nil {
		return nil, fmt.Errorf("nil IR unit")
	}
	code := make([]Instr, len(u.Code))
	spans := make([]lexer.Span, len(u.Code))
	for i, ins := range u.Code {
		code[i] = Instr{Op: ins.Op, A: ins.A, B: ins.B, C: ins.C}
		spans[i] = ins.Span
	}

	handlers := make([]ir.ExceptionHandler, len(u.Handlers))
	copy(handlers, u.Handlers)

	doTogethers := make([]CompiledDoTogether, len(u.DoTogethers))
	for i, dt := range u.DoTogethers {
		tasks := make([]CompiledTask, len(dt.Tasks))
		for j, t := range dt.Tasks {
			tc, err := CompileUnit(t.Unit, numFuncs)
			if err != nil {
				return nil, fmt.Errorf("do together task %d: %w", j, err)
			}
			tasks[j] = CompiledTask{
				Name:       t.Name,
				TargetSlot: t.TargetSlot,
				IsCallStmt: t.IsCallStmt,
				Chunk:      tc,
				Span:       t.Span,
			}
		}
		doTogethers[i] = CompiledDoTogether{
			Tasks:        tasks,
			CaptureSlots: append([]int32(nil), dt.CaptureSlots...),
			Span:         dt.Span,
		}
	}

	c := &Chunk{
		Name:          u.Name,
		Kind:          u.Kind,
		Code:          code,
		Spans:         spans,
		Constants:     append([]grabrt.Value(nil), u.Constants...),
		Strings:       append([]string(nil), u.Strings...),
		AuxSpans:      append([]lexer.Span(nil), u.Spans...),
		KeyLists:      u.KeyLists,
		HostCalls:     append([]ir.HostCallRef(nil), u.HostCalls...),
		Handlers:      handlers,
		DoTogethers:   doTogethers,
		NumLocals:     u.NumLocals,
		NumParams:     u.NumParams,
		NumIters:      u.NumIters,
		MaxTxDepth:    u.MaxTxDepth,
		RootSlots:     append([]ir.SlotInfo(nil), u.RootSlots...),
		BaseSlotCount: u.BaseSlotCount,
		UsesData:      u.UsesData,
	}

	if err := VerifyChunk(c, numFuncs); err != nil {
		return nil, err
	}
	return c, nil
}
