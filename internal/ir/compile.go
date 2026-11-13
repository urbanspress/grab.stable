package ir

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"grab/internal/ast"
	"grab/internal/lexer"
	grabrt "grab/internal/runtime"
)

// CompileProgram lowers a validated AST program into IR units.
// baseNames lists the sorted startup variable names from BaseEnv when compiling routes.
func CompileProgram(prog *ast.Program, baseNames []string) (*Program, error) {
	if prog == nil {
		return nil, fmt.Errorf("nil AST program")
	}

	usesData := false
	var fnDefs []*ast.FunctionDef
	var routeDefs []*ast.RouteDef
	funcIndex := make(map[string]int)

	for _, stmt := range prog.Statements {
		switch node := stmt.(type) {
		case *ast.UseDecl:
			if node.Module == "data" {
				usesData = true
			}
		case *ast.FunctionDef:
			funcIndex[node.Name] = len(fnDefs)
			fnDefs = append(fnDefs, node)
		case *ast.RouteDef:
			routeDefs = append(routeDefs, node)
		}
	}

	out := &Program{
		Functions: make([]*Unit, len(fnDefs)),
		FuncIndex: funcIndex,
		Routes:    make(map[*ast.RouteDef]*Unit, len(routeDefs)),
		UsesData:  usesData,
	}

	for i, fn := range fnDefs {
		unit, err := CompileFunction(fn, funcIndex, usesData)
		if err != nil {
			return nil, err
		}
		out.Functions[i] = unit
	}

	topUnit, err := compileStatementsUnit("<program>", UnitProgram, prog.Statements, nil, nil, funcIndex, usesData, prog.Span)
	if err != nil {
		return nil, err
	}
	out.TopLevel = topUnit

	for _, rt := range routeDefs {
		unit, err := CompileRoute(rt, baseNames, funcIndex, usesData)
		if err != nil {
			return nil, err
		}
		out.Routes[rt] = unit
	}

	return out, nil
}

// CompileTopLevel lowers only the top-level statements of a validated AST program.
func CompileTopLevel(prog *ast.Program) (*Unit, error) {
	if prog == nil {
		return nil, fmt.Errorf("nil AST program")
	}
	usesData := false
	funcIndex := make(map[string]int)
	fnCount := 0
	for _, stmt := range prog.Statements {
		switch node := stmt.(type) {
		case *ast.UseDecl:
			if node.Module == "data" {
				usesData = true
			}
		case *ast.FunctionDef:
			funcIndex[node.Name] = fnCount
			fnCount++
		}
	}
	return compileStatementsUnit("<program>", UnitProgram, prog.Statements, nil, nil, funcIndex, usesData, prog.Span)
}

// CompileFunction lowers a single validated FunctionDef into an IR Unit.
func CompileFunction(fn *ast.FunctionDef, funcIndex map[string]int, usesData bool) (*Unit, error) {
	params := make([]string, len(fn.Parameters))
	for i, p := range fn.Parameters {
		params[i] = p.Name
	}
	return compileStatementsUnit(fn.Name, UnitFunction, fn.Body, nil, params, funcIndex, usesData, fn.Span)
}

// CompileRoute lowers a single validated RouteDef into an IR Unit.
func CompileRoute(rt *ast.RouteDef, baseNames []string, funcIndex map[string]int, usesData bool) (*Unit, error) {
	var params []string
	if rt.RequestName != "" {
		params = []string{rt.RequestName}
	}
	name := rt.Method + " " + rt.Path
	return compileStatementsUnit(name, UnitRoute, rt.Body, baseNames, params, funcIndex, usesData, rt.Span)
}

type unitBuilder struct {
	unit      *Unit
	funcIndex map[string]int
	constMap  map[string]int32
	strMap    map[string]int32
	hostMap   map[string]int32
	rootSlots map[string]int32
	txDepth   int
}

type lexicalEnv struct {
	parent *lexicalEnv
	slots  map[string]int32
}

func newLexicalEnv(parent *lexicalEnv) *lexicalEnv {
	return &lexicalEnv{parent: parent, slots: make(map[string]int32)}
}

func (e *lexicalEnv) lookup(name string) (int32, bool) {
	for cur := e; cur != nil; cur = cur.parent {
		if slot, ok := cur.slots[name]; ok {
			return slot, true
		}
	}
	return -1, false
}

func compileStatementsUnit(
	name string,
	kind UnitKind,
	stmts []ast.Statement,
	baseNames []string,
	paramNames []string,
	funcIndex map[string]int,
	usesData bool,
	endSpan lexer.Span,
) (*Unit, error) {
	b := &unitBuilder{
		unit: &Unit{
			Name:          name,
			Kind:          kind,
			UsesData:      usesData,
			BaseSlotCount: len(baseNames),
		},
		funcIndex: funcIndex,
		constMap:  make(map[string]int32),
		strMap:    make(map[string]int32),
		hostMap:   make(map[string]int32),
		rootSlots: make(map[string]int32),
	}

	rootEnv := newLexicalEnv(nil)
	for _, baseName := range baseNames {
		slot := b.allocRootSlot(baseName, rootEnv)
		_ = slot
	}
	for _, pName := range paramNames {
		if _, exists := b.rootSlots[pName]; exists {
			slot := b.allocRootSlot(pName, rootEnv)
			_ = slot
		} else {
			b.allocRootSlot(pName, rootEnv)
		}
	}
	b.unit.NumParams = len(paramNames)

	if err := b.enterTxBlock(endSpan); err != nil {
		return nil, err
	}
	if err := b.compileBlock(stmts, rootEnv); err != nil {
		return nil, err
	}
	b.exitTxBlock(endSpan)
	b.emit(OpReturnNothing, 0, 0, 0, endSpan)

	optimizeUnit(b.unit)
	return b.unit, nil
}

func (b *unitBuilder) allocRootSlot(name string, rootEnv *lexicalEnv) int32 {
	if existing, ok := b.rootSlots[name]; ok {
		rootEnv.slots[name] = existing
		return existing
	}
	slot := int32(b.unit.NumLocals)
	b.unit.NumLocals++
	b.rootSlots[name] = slot
	rootEnv.slots[name] = slot
	b.unit.RootSlots = append(b.unit.RootSlots, SlotInfo{Name: name, Slot: int(slot)})
	return slot
}

func (b *unitBuilder) allocShadowSlot(name string, env *lexicalEnv) int32 {
	slot := int32(b.unit.NumLocals)
	b.unit.NumLocals++
	env.slots[name] = slot
	return slot
}

func (b *unitBuilder) resolveSetSlot(name string, env *lexicalEnv, rootEnv *lexicalEnv) int32 {
	if slot, ok := env.lookup(name); ok {
		return slot
	}
	return b.allocRootSlot(name, rootEnv)
}

func (b *unitBuilder) pc() int {
	return len(b.unit.Code)
}

func (b *unitBuilder) emit(op Op, a, argB, c int32, span lexer.Span) int {
	idx := len(b.unit.Code)
	b.unit.Code = append(b.unit.Code, Instr{Op: op, A: a, B: argB, C: c, Span: span})
	return idx
}

func (b *unitBuilder) patchJump(instrIdx int, targetPC int) {
	b.unit.Code[instrIdx].A = int32(targetPC)
}

func (b *unitBuilder) patchJumpC(instrIdx int, targetPC int) {
	b.unit.Code[instrIdx].C = int32(targetPC)
}

func (b *unitBuilder) addConst(val grabrt.Value) int32 {
	key := fmt.Sprintf("%d:%s", val.Kind(), val.Display())
	if val.Kind() == grabrt.BytesKind {
		key = fmt.Sprintf("bytes:%x", val.BytesValue())
	} else if val.Kind() == grabrt.DecimalKind {
		d := val.DecimalValue()
		key = fmt.Sprintf("dec:%s:%d", d.Coefficient.String(), d.Scale)
	}
	if idx, ok := b.constMap[key]; ok {
		return idx
	}
	idx := int32(len(b.unit.Constants))
	b.unit.Constants = append(b.unit.Constants, val)
	b.constMap[key] = idx
	return idx
}

func (b *unitBuilder) addString(s string) int32 {
	if idx, ok := b.strMap[s]; ok {
		return idx
	}
	idx := int32(len(b.unit.Strings))
	b.unit.Strings = append(b.unit.Strings, s)
	b.strMap[s] = idx
	return idx
}

func (b *unitBuilder) addSpan(s lexer.Span) int32 {
	idx := int32(len(b.unit.Spans))
	b.unit.Spans = append(b.unit.Spans, s)
	return idx
}

func (b *unitBuilder) addKeyList(keys []string) int32 {
	idx := int32(len(b.unit.KeyLists))
	cp := make([]string, len(keys))
	copy(cp, keys)
	b.unit.KeyLists = append(b.unit.KeyLists, cp)
	return idx
}

func (b *unitBuilder) addHostCall(module, name string, arity int) int32 {
	key := fmt.Sprintf("%s.%s/%d", module, name, arity)
	if idx, ok := b.hostMap[key]; ok {
		return idx
	}
	idx := int32(len(b.unit.HostCalls))
	b.unit.HostCalls = append(b.unit.HostCalls, HostCallRef{Module: module, Name: name, Arity: arity})
	b.hostMap[key] = idx
	return idx
}

func (b *unitBuilder) enterTxBlock(span lexer.Span) error {
	if !b.unit.UsesData {
		return nil
	}
	depth := b.txDepth
	b.txDepth++
	if b.txDepth > b.unit.MaxTxDepth {
		b.unit.MaxTxDepth = b.txDepth
	}
	b.emit(OpTxEnter, int32(depth), 0, 0, span)
	return nil
}

func (b *unitBuilder) exitTxBlock(span lexer.Span) {
	if !b.unit.UsesData {
		return
	}
	b.txDepth--
	b.emit(OpTxExit, int32(b.txDepth), 0, 0, span)
}

func rootOf(env *lexicalEnv) *lexicalEnv {
	for env.parent != nil {
		env = env.parent
	}
	return env
}

func (b *unitBuilder) compileBlock(stmts []ast.Statement, env *lexicalEnv) error {
	rootEnv := rootOf(env)
	for _, stmt := range stmts {
		switch node := stmt.(type) {
		case *ast.UseDecl, *ast.FunctionDef, *ast.RouteDef:
			continue
		case *ast.SetStatement:
			if err := b.compileExpr(node.Value, env); err != nil {
				return err
			}
			slot := b.resolveSetSlot(node.Name, env, rootEnv)
			b.emit(OpStoreSlot, slot, 0, 0, node.Span)
		case *ast.ShowStatement:
			if err := b.compileExpr(node.Value, env); err != nil {
				return err
			}
			b.emit(OpShow, 0, 0, 0, node.Span)
		case *ast.IfStatement:
			var endJumps []int
			for _, br := range node.Branches {
				if err := b.compileExpr(br.Condition, env); err != nil {
					return err
				}
				jFalse := b.emit(OpJumpIfFalse, 0, 0, 0, br.Condition.SourceSpan())
				_ = b.enterTxBlock(br.Span)
				if err := b.compileBlock(br.Body, env); err != nil {
					return err
				}
				b.exitTxBlock(br.Span)
				endJumps = append(endJumps, b.emit(OpJump, 0, 0, 0, br.Span))
				b.patchJump(jFalse, b.pc())
			}
			if node.HasElse {
				_ = b.enterTxBlock(node.Span)
				if err := b.compileBlock(node.Otherwise, env); err != nil {
					return err
				}
				b.exitTxBlock(node.Span)
			}
			endPC := b.pc()
			for _, j := range endJumps {
				b.patchJump(j, endPC)
			}
		case *ast.RepeatStatement:
			if err := b.compileExpr(node.Count, env); err != nil {
				return err
			}
			iterSlot := int32(b.unit.NumIters)
			b.unit.NumIters++
			countSpanIdx := b.addSpan(node.Count.SourceSpan())
			b.emit(OpRepeatInit, iterSlot, countSpanIdx, 0, node.Span)
			loopStart := b.pc()
			jExit := b.emit(OpRepeatNext, iterSlot, 0, 0, node.Span)
			_ = b.enterTxBlock(node.Span)
			if err := b.compileBlock(node.Body, env); err != nil {
				return err
			}
			b.exitTxBlock(node.Span)
			b.emit(OpJump, int32(loopStart), 0, 0, node.Span)
			b.unit.Code[jExit].B = int32(b.pc())
		case *ast.ForEachStatement:
			if err := b.compileExpr(node.Collection, env); err != nil {
				return err
			}
			iterSlot := int32(b.unit.NumIters)
			b.unit.NumIters++
			collSpanIdx := b.addSpan(node.Collection.SourceSpan())
			b.emit(OpIterInit, iterSlot, collSpanIdx, 0, node.Span)
			iterEnv := newLexicalEnv(env)
			itemSlot := b.allocShadowSlot(node.Item, iterEnv)
			loopStart := b.pc()
			jExit := b.emit(OpIterNext, iterSlot, itemSlot, 0, node.Span)
			_ = b.enterTxBlock(node.Span)
			if err := b.compileBlock(node.Body, iterEnv); err != nil {
				return err
			}
			b.exitTxBlock(node.Span)
			b.emit(OpJump, int32(loopStart), 0, 0, node.Span)
			b.patchJumpC(jExit, b.pc())
		case *ast.WhileStatement:
			loopStart := b.pc()
			if err := b.compileExpr(node.Condition, env); err != nil {
				return err
			}
			jExit := b.emit(OpJumpIfFalse, 0, 0, 0, node.Condition.SourceSpan())
			// execBlock in AST calls stepLoop right before execBlock(node.Body)
			// Using OpLoopStep? Wait: let's add OpLoopStep or reuse OpRepeatNext?
			// Let's check: OpJumpIfFalse checks condition; if true, we need stepLoop(node.Span)!
			// Let's emit OpRepeatInit? No, we should have a dedicated OpLoopStep!
			// Wait: let's check if we added OpLoopStep in ir.go. We didn't yet—let's check!
			// Wait: OpRepeatNext checks iter > 0; for While we want OpLoopStep! Let's add OpLoopStep to ir.go right after this file!
			b.emit(OpLoopStep, 0, 0, 0, node.Span)
			_ = b.enterTxBlock(node.Span)
			if err := b.compileBlock(node.Body, env); err != nil {
				return err
			}
			b.exitTxBlock(node.Span)
			b.emit(OpJump, int32(loopStart), 0, 0, node.Span)
			b.patchJump(jExit, b.pc())
		case *ast.TryStatement:
			tryTxDepth := b.txDepth
			tryStart := b.pc()
			_ = b.enterTxBlock(node.Span)
			if err := b.compileBlock(node.TryBody, env); err != nil {
				return err
			}
			b.exitTxBlock(node.Span)
			tryEnd := b.pc()
			jDone := b.emit(OpJump, 0, 0, 0, node.Span)

			handlerPC := b.pc()
			catchEnv := newLexicalEnv(env)
			errSlot := b.allocShadowSlot(node.CatchName, catchEnv)
			b.emit(OpCatchEnter, errSlot, 0, 0, node.CatchNameSpan)
			_ = b.enterTxBlock(node.Span)
			if err := b.compileBlock(node.CatchBody, catchEnv); err != nil {
				return err
			}
			b.exitTxBlock(node.Span)
			b.patchJump(jDone, b.pc())

			// Prepend inner-first order: since nested try blocks inside TryBody
			// are compiled before this outer TryStatement, appending here places
			// inner handlers before outer handlers in unit.Handlers!
			b.unit.Handlers = append(b.unit.Handlers, ExceptionHandler{
				StartPC:   tryStart,
				EndPC:     tryEnd,
				HandlerPC: handlerPC,
				ErrSlot:   int(errSlot),
				TxDepth:   tryTxDepth,
			})
		case *ast.RaiseStatement:
			if err := b.compileExpr(node.Value, env); err != nil {
				return err
			}
			b.emit(OpRaise, 0, 0, 0, node.Span)
		case *ast.GiveBackStatement:
			if node.Value == nil {
				b.emit(OpReturnNothing, 0, 0, 0, node.Span)
			} else {
				if err := b.compileExpr(node.Value, env); err != nil {
					return err
				}
				b.emit(OpReturn, 0, 0, 0, node.Span)
			}
		case *ast.CallStatement:
			if err := b.compileCall(node.Module, node.Name, node.Arguments, node.Span, env); err != nil {
				return err
			}
			b.emit(OpPop, 0, 0, 0, node.Span)
		case *ast.DoTogetherStatement:
			if err := b.compileDoTogether(node, env, rootEnv); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *unitBuilder) compileDoTogether(node *ast.DoTogetherStatement, env, rootEnv *lexicalEnv) error {
	// Snapshot all currently visible bindings into TaskUnits.
	// Wait: a TaskStatement executes against env.cloneSnapshot().
	// To make snapshot fast and exact, we collect all lexical slots visible in `env`
	// plus any root slots and map them into the TaskUnit's initial slots 0..K-1.
	visible := collectVisibleSlots(env, b.rootSlots)
	baseSlots := make([]int32, len(visible))
	taskBaseNames := make([]string, len(visible))
	for i, sv := range visible {
		baseSlots[i] = sv.slot
		taskBaseNames[i] = sv.name
	}
	_ = baseSlots

	plan := DoTogetherPlan{
		Tasks: make([]TaskPlan, len(node.Tasks)),
		Span:  node.Span,
	}
	for i, task := range node.Tasks {
		tb := &unitBuilder{
			unit: &Unit{
				Name:          fmt.Sprintf("%s$task%d", b.unit.Name, i),
				Kind:          UnitTask,
				UsesData:      b.unit.UsesData,
				BaseSlotCount: len(visible),
			},
			funcIndex: b.funcIndex,
			constMap:  make(map[string]int32),
			strMap:    make(map[string]int32),
			hostMap:   make(map[string]int32),
			rootSlots: make(map[string]int32),
		}
		taskRoot := newLexicalEnv(nil)
		for _, sv := range visible {
			slot := tb.allocRootSlot(sv.name, taskRoot)
			_ = slot
		}
		if task.Call != nil {
			if err := tb.compileCall(task.Call.Module, task.Call.Name, task.Call.Arguments, task.Call.Span, taskRoot); err != nil {
				return err
			}
			tb.emit(OpReturn, 0, 0, 0, task.Span)
			optimizeUnit(tb.unit)
			plan.Tasks[i] = TaskPlan{
				IsCallStmt: true,
				TargetSlot: -1,
				Unit:       tb.unit,
				Span:       task.Span,
			}
		} else {
			if err := tb.compileExpr(task.Value, taskRoot); err != nil {
				return err
			}
			tb.emit(OpReturn, 0, 0, 0, task.Span)
			optimizeUnit(tb.unit)
			targetSlot := b.resolveSetSlot(task.Name, env, rootEnv)
			plan.Tasks[i] = TaskPlan{
				Name:       task.Name,
				TargetSlot: targetSlot,
				IsCallStmt: false,
				Unit:       tb.unit,
				Span:       task.Span,
			}
		}
	}

	// Store the slot capture mapping on DoTogetherPlan via TaskUnit RootSlots?
	// Better: store CaptureSlots []int32 on DoTogetherPlan! Let's add CaptureSlots to DoTogetherPlan in ir.go.
	// Wait—let's check: TaskUnit's RootSlots[0..len(visible)-1] has the exact names,
	// and if CaptureSlots []int32 is stored on DoTogetherPlan, the VM just copies
	// frame.locals[captureSlot] into taskFrame.locals[j]!
	_ = taskBaseNames
	planIdx := int32(len(b.unit.DoTogethers))
	plan.CaptureSlots = baseSlots
	b.unit.DoTogethers = append(b.unit.DoTogethers, plan)
	b.emit(OpDoTogether, planIdx, 0, 0, node.Span)
	return nil
}

type namedSlot struct {
	name string
	slot int32
}

func collectVisibleSlots(env *lexicalEnv, rootSlots map[string]int32) []namedSlot {
	merged := make(map[string]int32, len(rootSlots))
	for name, slot := range rootSlots {
		merged[name] = slot
	}
	var chain []*lexicalEnv
	for cur := env; cur != nil; cur = cur.parent {
		chain = append(chain, cur)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		for name, slot := range chain[i].slots {
			merged[name] = slot
		}
	}
	// Deterministic order by slot index
	out := make([]namedSlot, 0, len(merged))
	for name, slot := range merged {
		out = append(out, namedSlot{name: name, slot: slot})
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].slot < out[j-1].slot; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func (b *unitBuilder) compileCall(module, name string, args []ast.Expression, span lexer.Span, env *lexicalEnv) error {
	if module != "" {
		hostIdx := b.addHostCall(module, name, len(args))
		if needsEventWorkPreCheck(module, name) {
			b.emit(OpPreCheckHost, hostIdx, 0, 0, span)
		}
		for _, arg := range args {
			if err := b.compileExpr(arg, env); err != nil {
				return err
			}
		}
		b.emit(OpCallHost, hostIdx, int32(len(args)), 0, span)
		return nil
	}
	fnIdx, ok := b.funcIndex[name]
	if !ok {
		return fmt.Errorf("unknown function %q during IR lowering", name)
	}
	for _, arg := range args {
		if err := b.compileExpr(arg, env); err != nil {
			return err
		}
	}
	b.emit(OpCall, int32(fnIdx), int32(len(args)), 0, span)
	return nil
}

func needsEventWorkPreCheck(module, name string) bool {
	if module == "http" || module == "connect" {
		return true
	}
	if module == "data" {
		switch name {
		case "commit", "rollback", "close", "begin", "migrate":
			return true
		}
	}
	return false
}

func (b *unitBuilder) compileExpr(expr ast.Expression, env *lexicalEnv) error {
	switch node := expr.(type) {
	case *ast.IdentifierExpr:
		slot, ok := env.lookup(node.Name)
		if !ok {
			if rSlot, rok := b.rootSlots[node.Name]; rok {
				slot = rSlot
			} else {
				return fmt.Errorf("unbound identifier %q during IR lowering", node.Name)
			}
		}
		b.emit(OpLoadSlot, slot, 0, 0, node.Span)
	case *ast.LiteralExpr:
		val, errMsg := evalLiteral(node)
		if errMsg != "" {
			msgIdx := b.addString(errMsg)
			b.emit(OpFailLiteral, msgIdx, 0, 0, node.Span)
			return nil
		}
		switch val.Kind() {
		case grabrt.NothingKind:
			b.emit(OpNothing, 0, 0, 0, node.Span)
		case grabrt.BooleanKind:
			if val.BooleanValue() {
				b.emit(OpTrue, 0, 0, 0, node.Span)
			} else {
				b.emit(OpFalse, 0, 0, 0, node.Span)
			}
		default:
			cIdx := b.addConst(val)
			b.emit(OpConst, cIdx, 0, 0, node.Span)
		}
	case *ast.ParenExpr:
		return b.compileExpr(node.Inner, env)
	case *ast.UnaryExpr:
		if err := b.compileExpr(node.Operand, env); err != nil {
			return err
		}
		b.emit(OpNot, 0, 0, 0, node.Span)
	case *ast.JoinedExpr:
		if err := b.compileExpr(node.Left, env); err != nil {
			return err
		}
		if err := b.compileExpr(node.Right, env); err != nil {
			return err
		}
		b.emit(OpJoin, 0, 0, 0, node.Span)
	case *ast.BinaryExpr:
		if node.Op == ast.OpAnd {
			if err := b.compileExpr(node.Left, env); err != nil {
				return err
			}
			jShort := b.emit(OpJumpIfFalseKeep, 0, 0, 0, node.Span)
			if err := b.compileExpr(node.Right, env); err != nil {
				return err
			}
			b.emit(OpCheckBool, 0, 0, 0, node.Span)
			b.patchJump(jShort, b.pc())
			return nil
		}
		if node.Op == ast.OpOr {
			if err := b.compileExpr(node.Left, env); err != nil {
				return err
			}
			jShort := b.emit(OpJumpIfTrueKeep, 0, 0, 0, node.Span)
			if err := b.compileExpr(node.Right, env); err != nil {
				return err
			}
			b.emit(OpCheckBool, 0, 0, 0, node.Span)
			b.patchJump(jShort, b.pc())
			return nil
		}
		if err := b.compileExpr(node.Left, env); err != nil {
			return err
		}
		if err := b.compileExpr(node.Right, env); err != nil {
			return err
		}
		var op Op
		switch node.Op {
		case ast.OpPlus:
			op = OpAdd
		case ast.OpMinus:
			op = OpSub
		case ast.OpTimes:
			op = OpMul
		case ast.OpDividedBy:
			op = OpDiv
		case ast.OpModulo:
			op = OpMod
		case ast.OpIs:
			op = OpEq
		case ast.OpIsNot:
			op = OpNeq
		case ast.OpGreaterThan:
			op = OpGt
		case ast.OpLessThan:
			op = OpLt
		case ast.OpAtLeast:
			op = OpGte
		case ast.OpAtMost:
			op = OpLte
		default:
			return fmt.Errorf("unsupported binary op %d", node.Op)
		}
		b.emit(op, 0, 0, 0, node.Span)
	case *ast.FieldAccessExpr:
		if err := b.compileExpr(node.Target, env); err != nil {
			return err
		}
		fieldIdx := b.addString(node.Field)
		fieldSpanIdx := b.addSpan(node.FieldSpan)
		b.emit(OpField, fieldIdx, fieldSpanIdx, 0, node.Span)
	case *ast.IndexAccessExpr:
		if err := b.compileExpr(node.Target, env); err != nil {
			return err
		}
		if err := b.compileExpr(node.Index, env); err != nil {
			return err
		}
		idxSpanIdx := b.addSpan(node.Index.SourceSpan())
		b.emit(OpAt, idxSpanIdx, 0, 0, node.Span)
	case *ast.HasExpr:
		if err := b.compileExpr(node.Target, env); err != nil {
			return err
		}
		keyIdx := b.addString(node.Key)
		b.emit(OpHas, keyIdx, 0, 0, node.Span)
	case *ast.CallExpr:
		return b.compileCall(node.Module, node.Name, node.Arguments, node.Span, env)
	case *ast.ListExpr:
		for _, el := range node.Elements {
			if err := b.compileExpr(el, env); err != nil {
				return err
			}
		}
		b.emit(OpBuildList, int32(len(node.Elements)), 0, 0, node.Span)
	case *ast.MapExpr:
		keys := make([]string, len(node.Entries))
		for i, entry := range node.Entries {
			keys[i] = entry.Key
			if err := b.compileExpr(entry.Value, env); err != nil {
				return err
			}
		}
		keyListIdx := b.addKeyList(keys)
		b.emit(OpBuildMap, int32(len(node.Entries)), keyListIdx, 0, node.Span)
	case *ast.RecordExpr:
		keys := make([]string, len(node.Fields))
		for i, field := range node.Fields {
			keys[i] = field.Name
			if err := b.compileExpr(field.Value, env); err != nil {
				return err
			}
		}
		keyListIdx := b.addKeyList(keys)
		b.emit(OpBuildRecord, int32(len(node.Fields)), keyListIdx, 0, node.Span)
	default:
		return fmt.Errorf("unsupported expression %T", expr)
	}
	return nil
}

func evalLiteral(node *ast.LiteralExpr) (grabrt.Value, string) {
	switch node.Kind {
	case ast.TextLiteral:
		decoded, err := strconv.Unquote(node.Lexeme)
		if err != nil {
			return grabrt.Value{}, err.Error()
		}
		return grabrt.Text(decoded), ""
	case ast.BooleanLiteral:
		return grabrt.Boolean(node.Lexeme == "yes"), ""
	case ast.NothingLiteral:
		return grabrt.Nothing(), ""
	case ast.IntegerLiteral:
		v, err := grabrt.IntegerFromString(node.Lexeme)
		if err != nil {
			return grabrt.Value{}, err.Error()
		}
		return v, ""
	case ast.DecimalLiteral:
		v, err := grabrt.DecimalFromString(node.Lexeme)
		if err != nil {
			return grabrt.Value{}, err.Error()
		}
		return v, ""
	case ast.FloatLiteral:
		v, err := grabrt.FloatFromString(node.Lexeme)
		if err != nil {
			return grabrt.Value{}, err.Error()
		}
		if math.IsInf(v.FloatValue(), 0) || math.IsNaN(v.FloatValue()) {
			return grabrt.Value{}, "numeric overflow"
		}
		return v, ""
	case ast.BytesLiteral:
		decoded, err := decodeBytes(node.Lexeme)
		if err != nil {
			return grabrt.Value{}, err.Error()
		}
		return grabrt.Bytes(decoded), ""
	default:
		return grabrt.Value{}, fmt.Sprintf("unknown literal kind %d", node.Kind)
	}
}

func decodeBytes(lexeme string) ([]byte, error) {
	if len(lexeme) < 3 || !strings.HasPrefix(lexeme, "b\"") || lexeme[len(lexeme)-1] != '"' {
		return nil, fmt.Errorf("malformed byte literal")
	}
	body := lexeme[2 : len(lexeme)-1]
	result := make([]byte, 0, len(body))
	for i := 0; i < len(body); i++ {
		if body[i] != '\\' {
			result = append(result, body[i])
			continue
		}
		i++
		if i >= len(body) {
			return nil, fmt.Errorf("incomplete byte escape")
		}
		switch body[i] {
		case '\\':
			result = append(result, '\\')
		case '"':
			result = append(result, '"')
		case 'x':
			if i+2 >= len(body) {
				return nil, fmt.Errorf("incomplete hexadecimal byte escape")
			}
			parsed, err := strconv.ParseUint(body[i+1:i+3], 16, 8)
			if err != nil {
				return nil, err
			}
			result = append(result, byte(parsed))
			i += 2
		default:
			return nil, fmt.Errorf("unsupported byte escape")
		}
	}
	return result, nil
}
