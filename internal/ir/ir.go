package ir

import (
	"grab/internal/ast"
	"grab/internal/lexer"
	grabrt "grab/internal/runtime"
)

// Op identifies a single intermediate-representation instruction.
type Op uint8

const (
	OpConst Op = iota
	OpNothing
	OpTrue
	OpFalse
	OpFailLiteral

	OpLoadSlot
	OpStoreSlot

	OpPop
	OpDup

	OpAdd
	OpSub
	OpMul
	OpDiv
	OpMod
	OpJoin

	OpEq
	OpNeq
	OpGt
	OpLt
	OpGte
	OpLte
	OpNot

	OpBuildList
	OpBuildMap
	OpBuildRecord
	OpField
	OpAt
	OpHas

	OpJump
	OpJumpIfFalse
	OpJumpIfFalseKeep
	OpJumpIfTrueKeep
	OpCheckBool

	OpRepeatInit
	OpRepeatNext
	OpIterInit
	OpIterNext
	OpLoopStep

	OpTxEnter
	OpTxExit

	OpPreCheckHost
	OpCall
	OpCallHost
	OpReturn
	OpReturnNothing
	OpShow
	OpRaise
	OpCatchEnter
	OpDoTogether
)

// String returns a human-readable mnemonic for debugging and disassembly.
func (op Op) String() string {
	names := [...]string{
		OpConst:           "CONST",
		OpNothing:         "NOTHING",
		OpTrue:            "TRUE",
		OpFalse:           "FALSE",
		OpFailLiteral:     "FAIL_LITERAL",
		OpLoadSlot:        "LOAD_SLOT",
		OpStoreSlot:       "STORE_SLOT",
		OpPop:             "POP",
		OpDup:             "DUP",
		OpAdd:             "ADD",
		OpSub:             "SUB",
		OpMul:             "MUL",
		OpDiv:             "DIV",
		OpMod:             "MOD",
		OpJoin:            "JOIN",
		OpEq:              "EQ",
		OpNeq:             "NEQ",
		OpGt:              "GT",
		OpLt:              "LT",
		OpGte:             "GTE",
		OpLte:             "LTE",
		OpNot:             "NOT",
		OpBuildList:       "BUILD_LIST",
		OpBuildMap:        "BUILD_MAP",
		OpBuildRecord:     "BUILD_RECORD",
		OpField:           "FIELD",
		OpAt:              "AT",
		OpHas:             "HAS",
		OpJump:            "JUMP",
		OpJumpIfFalse:     "JUMP_IF_FALSE",
		OpJumpIfFalseKeep: "JUMP_IF_FALSE_KEEP",
		OpJumpIfTrueKeep:  "JUMP_IF_TRUE_KEEP",
		OpCheckBool:       "CHECK_BOOL",
		OpRepeatInit:      "REPEAT_INIT",
		OpRepeatNext:      "REPEAT_NEXT",
		OpIterInit:        "ITER_INIT",
		OpIterNext:        "ITER_NEXT",
		OpLoopStep:        "LOOP_STEP",
		OpTxEnter:         "TX_ENTER",
		OpTxExit:          "TX_EXIT",
		OpPreCheckHost:    "PRE_CHECK_HOST",
		OpCall:            "CALL",
		OpCallHost:        "CALL_HOST",
		OpReturn:          "RETURN",
		OpReturnNothing:   "RETURN_NOTHING",
		OpShow:            "SHOW",
		OpRaise:           "RAISE",
		OpCatchEnter:      "CATCH_ENTER",
		OpDoTogether:      "DO_TOGETHER",
	}
	if int(op) < len(names) && names[op] != "" {
		return names[op]
	}
	return "UNKNOWN"
}

// Instr is a single IR instruction with up to three compact operands and a source span.
type Instr struct {
	Op   Op
	A    int32
	B    int32
	C    int32
	Span lexer.Span
}

// ExceptionHandler maps a protected instruction range [StartPC, EndPC) to its catch target.
type ExceptionHandler struct {
	StartPC    int
	EndPC      int
	HandlerPC  int
	ErrSlot    int
	TxDepth    int
	StackDepth int
}

// HostCallRef identifies a validated standard-library or platform module function call.
type HostCallRef struct {
	Module string
	Name   string
	Arity  int
}

// TaskPlan describes one concurrent task inside a compiled DoTogetherStatement.
type TaskPlan struct {
	Name       string
	TargetSlot int32
	IsCallStmt bool
	Unit       *Unit
	Span       lexer.Span
}

// DoTogetherPlan describes all tasks in a compiled DoTogetherStatement.
type DoTogetherPlan struct {
	Tasks        []TaskPlan
	CaptureSlots []int32
	Span         lexer.Span
}

// SlotInfo maps a root-level slot index back to its GRAB variable name.
type SlotInfo struct {
	Name string
	Slot int
}

// Unit is the compiled IR for a single function, route handler, top-level program, or task.
type Unit struct {
	Name          string
	Kind          UnitKind
	Code          []Instr
	Constants     []grabrt.Value
	Strings       []string
	Spans         []lexer.Span
	KeyLists      [][]string
	HostCalls     []HostCallRef
	Handlers      []ExceptionHandler
	DoTogethers   []DoTogetherPlan
	NumLocals     int
	NumParams     int
	NumIters      int
	MaxTxDepth    int
	RootSlots     []SlotInfo
	BaseSlotCount int
	UsesData      bool
}

// UnitKind identifies the origin of a compiled IR Unit.
type UnitKind uint8

const (
	UnitProgram UnitKind = iota
	UnitFunction
	UnitRoute
	UnitTask
)

// Program holds the compiled IR for a validated GRAB program.
type Program struct {
	TopLevel  *Unit
	Functions []*Unit
	FuncIndex map[string]int
	Routes    map[*ast.RouteDef]*Unit
	UsesData  bool
}
