package ir

// optimizeUnit performs safe, semantics-preserving IR passes that do not alter
// instruction indices or exception-handler ranges.
func optimizeUnit(u *Unit) {
	if u == nil || len(u.Code) == 0 {
		return
	}
	// Jump threading: follow chains of unconditional OpJump instructions.
	for i := range u.Code {
		instr := &u.Code[i]
		switch instr.Op {
		case OpJump, OpJumpIfFalse, OpJumpIfFalseKeep, OpJumpIfTrueKeep:
			instr.A = int32(followJumps(u.Code, int(instr.A)))
		case OpRepeatNext:
			instr.B = int32(followJumps(u.Code, int(instr.B)))
		case OpIterNext:
			instr.C = int32(followJumps(u.Code, int(instr.C)))
		}
	}
}

func followJumps(code []Instr, target int) int {
	for steps := 0; steps < 32 && target >= 0 && target < len(code); steps++ {
		if code[target].Op != OpJump {
			break
		}
		next := int(code[target].A)
		if next == target {
			break
		}
		target = next
	}
	return target
}
