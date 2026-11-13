package vm

import (
	"fmt"

	"grab/internal/ir"
)

// VerifyChunk statically validates bytecode invariants (bounds, jump targets,
// handler ranges, and abstract stack depth) before execution.
func VerifyChunk(c *Chunk, numFuncs int) error {
	if c == nil || len(c.Code) == 0 {
		return fmt.Errorf("bytecode verify: empty chunk")
	}
	if len(c.Spans) != len(c.Code) {
		return fmt.Errorf("bytecode verify: span count %d != code len %d", len(c.Spans), len(c.Code))
	}
	if c.NumLocals < 0 || c.NumParams < 0 || c.NumParams > c.NumLocals || c.BaseSlotCount < 0 || c.BaseSlotCount > c.NumLocals {
		return fmt.Errorf("bytecode verify: invalid slot layout in %q", c.Name)
	}
	for i, h := range c.Handlers {
		if h.StartPC < 0 || h.StartPC > h.EndPC || h.EndPC > len(c.Code) {
			return fmt.Errorf("bytecode verify: handler %d range [%d,%d) out of bounds", i, h.StartPC, h.EndPC)
		}
		if h.HandlerPC < 0 || h.HandlerPC >= len(c.Code) {
			return fmt.Errorf("bytecode verify: handler %d target %d out of bounds", i, h.HandlerPC)
		}
		if h.ErrSlot < 0 || h.ErrSlot >= c.NumLocals {
			return fmt.Errorf("bytecode verify: handler %d err slot %d out of bounds", i, h.ErrSlot)
		}
		if h.TxDepth < 0 || h.TxDepth > c.MaxTxDepth {
			return fmt.Errorf("bytecode verify: handler %d tx depth %d out of bounds", i, h.TxDepth)
		}
	}

	depths := make([]int, len(c.Code))
	for i := range depths {
		depths[i] = -1
	}

	type workItem struct {
		pc    int
		depth int
	}
	queue := []workItem{{pc: 0, depth: 0}}
	for _, h := range c.Handlers {
		// Exception unwinding restores operand stack to 0 (statement level) before entering catch.
		queue = append(queue, workItem{pc: h.HandlerPC, depth: h.StackDepth})
	}

	maxStack := 0
	enqueue := func(pc, depth int) error {
		if pc < 0 || pc >= len(c.Code) {
			return fmt.Errorf("bytecode verify: jump target %d out of bounds [0,%d)", pc, len(c.Code))
		}
		if depth < 0 {
			return fmt.Errorf("bytecode verify: stack underflow at pc %d", pc)
		}
		if depths[pc] != -1 {
			if depths[pc] != depth {
				return fmt.Errorf("bytecode verify: stack depth mismatch at pc %d (%d vs %d)", pc, depths[pc], depth)
			}
			return nil
		}
		depths[pc] = depth
		queue = append(queue, workItem{pc: pc, depth: depth})
		return nil
	}

	if err := enqueue(0, 0); err != nil {
		return err
	}
	for _, h := range c.Handlers {
		if err := enqueue(h.HandlerPC, h.StackDepth); err != nil {
			return err
		}
	}

	for head := 0; head < len(queue); head++ {
		item := queue[head]
		pc := item.pc
		depth := item.depth
		ins := c.Code[pc]

		delta, nextPC, altPC, err := verifyInstr(c, numFuncs, pc, ins, depth)
		if err != nil {
			return fmt.Errorf("bytecode verify (%s pc=%d %s): %w", c.Name, pc, ins.Op, err)
		}
		if depth+delta > maxStack {
			maxStack = depth + delta
		}
		if depth > maxStack {
			maxStack = depth
		}
		newDepth := depth + delta
		if newDepth < 0 {
			return fmt.Errorf("bytecode verify (%s pc=%d %s): stack underflow", c.Name, pc, ins.Op)
		}
		if nextPC >= 0 {
			if err := enqueue(nextPC, newDepth); err != nil {
				return err
			}
		}
		if altPC >= 0 {
			altDepth := newDepth
			// Short-circuit JumpIfFalseKeep / JumpIfTrueKeep keep 1 bool on stack on jump path,
			// and pop it (delta -1) on fallthrough path! Handled cleanly via verifyInstr below.
			if ins.Op == ir.OpJumpIfFalseKeep || ins.Op == ir.OpJumpIfTrueKeep {
				altDepth = depth // keep peeked boolean on stack
			}
			if err := enqueue(altPC, altDepth); err != nil {
				return err
			}
		}
	}

	if maxStack < 1 {
		maxStack = 1
	}
	c.MaxStack = maxStack
	return nil
}

func verifyInstr(c *Chunk, numFuncs int, pc int, ins Instr, depth int) (delta int, nextPC int, altPC int, err error) {
	nextPC = pc + 1
	altPC = -1

	switch ins.Op {
	case ir.OpConst:
		if int(ins.A) < 0 || int(ins.A) >= len(c.Constants) {
			return 0, -1, -1, fmt.Errorf("const index %d out of bounds", ins.A)
		}
		return 1, nextPC, -1, nil
	case ir.OpNothing, ir.OpTrue, ir.OpFalse:
		return 1, nextPC, -1, nil
	case ir.OpFailLiteral:
		if int(ins.A) < 0 || int(ins.A) >= len(c.Strings) {
			return 0, -1, -1, fmt.Errorf("string index %d out of bounds", ins.A)
		}
		return 1, nextPC, -1, nil
	case ir.OpLoadSlot:
		if int(ins.A) < 0 || int(ins.A) >= c.NumLocals {
			return 0, -1, -1, fmt.Errorf("local slot %d out of bounds", ins.A)
		}
		return 1, nextPC, -1, nil
	case ir.OpStoreSlot:
		if int(ins.A) < 0 || int(ins.A) >= c.NumLocals {
			return 0, -1, -1, fmt.Errorf("local slot %d out of bounds", ins.A)
		}
		if depth < 1 {
			return 0, -1, -1, fmt.Errorf("stack underflow")
		}
		return -1, nextPC, -1, nil
	case ir.OpPop:
		if depth < 1 {
			return 0, -1, -1, fmt.Errorf("stack underflow")
		}
		return -1, nextPC, -1, nil
	case ir.OpDup:
		if depth < 1 {
			return 0, -1, -1, fmt.Errorf("stack underflow")
		}
		return 1, nextPC, -1, nil
	case ir.OpAdd, ir.OpSub, ir.OpMul, ir.OpDiv, ir.OpMod, ir.OpJoin,
		ir.OpEq, ir.OpNeq, ir.OpGt, ir.OpLt, ir.OpGte, ir.OpLte:
		if depth < 2 {
			return 0, -1, -1, fmt.Errorf("stack underflow")
		}
		return -1, nextPC, -1, nil
	case ir.OpNot, ir.OpCheckBool:
		if depth < 1 {
			return 0, -1, -1, fmt.Errorf("stack underflow")
		}
		return 0, nextPC, -1, nil
	case ir.OpBuildList:
		n := int(ins.A)
		if n < 0 || depth < n {
			return 0, -1, -1, fmt.Errorf("invalid list count %d for depth %d", n, depth)
		}
		return 1 - n, nextPC, -1, nil
	case ir.OpBuildMap, ir.OpBuildRecord:
		n := int(ins.A)
		if n < 0 || depth < n || int(ins.B) < 0 || int(ins.B) >= len(c.KeyLists) || len(c.KeyLists[ins.B]) != n {
			return 0, -1, -1, fmt.Errorf("invalid map/record args")
		}
		return 1 - n, nextPC, -1, nil
	case ir.OpField:
		if depth < 1 || int(ins.A) < 0 || int(ins.A) >= len(c.Strings) || int(ins.B) < 0 || int(ins.B) >= len(c.AuxSpans) {
			return 0, -1, -1, fmt.Errorf("invalid field access")
		}
		return 0, nextPC, -1, nil
	case ir.OpAt:
		if depth < 2 || int(ins.A) < 0 || int(ins.A) >= len(c.AuxSpans) {
			return 0, -1, -1, fmt.Errorf("invalid index access")
		}
		return -1, nextPC, -1, nil
	case ir.OpHas:
		if depth < 1 || int(ins.A) < 0 || int(ins.A) >= len(c.Strings) {
			return 0, -1, -1, fmt.Errorf("invalid has key")
		}
		return 0, nextPC, -1, nil
	case ir.OpJump:
		return 0, int(ins.A), -1, nil
	case ir.OpJumpIfFalse:
		if depth < 1 {
			return 0, -1, -1, fmt.Errorf("stack underflow")
		}
		return -1, nextPC, int(ins.A), nil
	case ir.OpJumpIfFalseKeep, ir.OpJumpIfTrueKeep:
		if depth < 1 {
			return 0, -1, -1, fmt.Errorf("stack underflow")
		}
		// Fallthrough pops the checked boolean; jump keeps it on stack (handled by caller).
		return -1, nextPC, int(ins.A), nil
	case ir.OpRepeatInit:
		if depth < 1 || int(ins.A) < 0 || int(ins.A) >= c.NumIters || int(ins.B) < 0 || int(ins.B) >= len(c.AuxSpans) {
			return 0, -1, -1, fmt.Errorf("invalid repeat init")
		}
		return -1, nextPC, -1, nil
	case ir.OpRepeatNext:
		if int(ins.A) < 0 || int(ins.A) >= c.NumIters {
			return 0, -1, -1, fmt.Errorf("invalid repeat iter %d", ins.A)
		}
		return 0, nextPC, int(ins.B), nil
	case ir.OpIterInit:
		if depth < 1 || int(ins.A) < 0 || int(ins.A) >= c.NumIters || int(ins.B) < 0 || int(ins.B) >= len(c.AuxSpans) {
			return 0, -1, -1, fmt.Errorf("invalid iter init")
		}
		return -1, nextPC, -1, nil
	case ir.OpIterNext:
		if int(ins.A) < 0 || int(ins.A) >= c.NumIters || int(ins.B) < 0 || int(ins.B) >= c.NumLocals {
			return 0, -1, -1, fmt.Errorf("invalid iter next")
		}
		return 0, nextPC, int(ins.C), nil
	case ir.OpLoopStep:
		return 0, nextPC, -1, nil
	case ir.OpTxEnter, ir.OpTxExit:
		if int(ins.A) < 0 || int(ins.A) >= c.MaxTxDepth {
			return 0, -1, -1, fmt.Errorf("invalid tx depth %d", ins.A)
		}
		return 0, nextPC, -1, nil
	case ir.OpPreCheckHost:
		if int(ins.A) < 0 || int(ins.A) >= len(c.HostCalls) {
			return 0, -1, -1, fmt.Errorf("host call index %d out of bounds", ins.A)
		}
		return 0, nextPC, -1, nil
	case ir.OpCall:
		if int(ins.A) < 0 || int(ins.A) >= numFuncs {
			return 0, -1, -1, fmt.Errorf("func index %d out of bounds (%d)", ins.A, numFuncs)
		}
		n := int(ins.B)
		if n < 0 || depth < n {
			return 0, -1, -1, fmt.Errorf("invalid call arity %d", n)
		}
		return 1 - n, nextPC, -1, nil
	case ir.OpCallHost:
		if int(ins.A) < 0 || int(ins.A) >= len(c.HostCalls) {
			return 0, -1, -1, fmt.Errorf("host call index %d out of bounds", ins.A)
		}
		n := int(ins.B)
		if n < 0 || depth < n || n != c.HostCalls[ins.A].Arity {
			return 0, -1, -1, fmt.Errorf("invalid host call arity %d", n)
		}
		return 1 - n, nextPC, -1, nil
	case ir.OpReturn:
		if depth < 1 {
			return 0, -1, -1, fmt.Errorf("stack underflow")
		}
		return -1, -1, -1, nil
	case ir.OpReturnNothing:
		return 0, -1, -1, nil
	case ir.OpShow:
		if depth < 1 {
			return 0, -1, -1, fmt.Errorf("stack underflow")
		}
		return -1, nextPC, -1, nil
	case ir.OpRaise:
		if depth < 1 {
			return 0, -1, -1, fmt.Errorf("stack underflow")
		}
		return -1, -1, -1, nil
	case ir.OpCatchEnter:
		if int(ins.A) < 0 || int(ins.A) >= c.NumLocals {
			return 0, -1, -1, fmt.Errorf("invalid catch slot %d", ins.A)
		}
		return 0, nextPC, -1, nil
	case ir.OpDoTogether:
		if int(ins.A) < 0 || int(ins.A) >= len(c.DoTogethers) {
			return 0, -1, -1, fmt.Errorf("do together index %d out of bounds", ins.A)
		}
		dt := c.DoTogethers[ins.A]
		for _, s := range dt.CaptureSlots {
			if int(s) < 0 || int(s) >= c.NumLocals {
				return 0, -1, -1, fmt.Errorf("do together capture slot %d out of bounds", s)
			}
		}
		for _, t := range dt.Tasks {
			if !t.IsCallStmt && (int(t.TargetSlot) < 0 || int(t.TargetSlot) >= c.NumLocals) {
				return 0, -1, -1, fmt.Errorf("do together target slot %d out of bounds", t.TargetSlot)
			}
		}
		return 0, nextPC, -1, nil
	default:
		return 0, -1, -1, fmt.Errorf("unknown opcode %d", ins.Op)
	}
}
