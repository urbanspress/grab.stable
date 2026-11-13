package interpreter

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"grab/internal/ast"
	"grab/internal/cache"
	"grab/internal/data"
	"grab/internal/ir"
	"grab/internal/lexer"
	grabrt "grab/internal/runtime"
	"grab/internal/vm"
)

// EngineMode selects how GRAB programs are executed.
type EngineMode string

const (
	EngineAuto EngineMode = "auto"
	EngineAST  EngineMode = "ast"
	EngineVM   EngineMode = "vm"
)

// Default profile-driven tiering thresholds for GRAB 0.9.
const (
	DefaultHotFunctionThreshold = 16
	DefaultHotLoopThreshold     = 64
	DefaultHotRouteThreshold    = 8
)

// ParseEngineMode parses an --engine flag value ("auto", "ast", or "vm").
func ParseEngineMode(raw string) (EngineMode, bool) {
	switch EngineMode(strings.TrimSpace(raw)) {
	case EngineAuto:
		return EngineAuto, true
	case EngineAST:
		return EngineAST, true
	case EngineVM:
		return EngineVM, true
	default:
		return "", false
	}
}

func EngineAutoOpts(opts Options) Options {
	opts.Engine = EngineAuto
	return opts
}

func resolveEngineMode(opts Options) EngineMode {
	if opts.Engine == "" {
		return EngineAuto
	}
	return opts.Engine
}

type vmUnitSlot struct {
	calls     atomic.Int64
	loopIters atomic.Int64
	once      sync.Once
	chunk     atomic.Pointer[vm.Chunk]
	err       error
}

type vmProgramState struct {
	mode              EngineMode
	hotFnThreshold    int64
	hotLoopThreshold  int64
	hotRouteThreshold int64
	usesData          bool

	funcDefs  []*ast.FunctionDef
	funcNames []string
	funcIndex map[string]int
	fnSlots   []*vmUnitSlot

	topStmt   []ast.Statement
	topSpan   lexer.Span
	topSlot   *vmUnitSlot

	baseNames  []string
	baseValues []grabrt.Value
	routes     map[*Route]*vmUnitSlot
}

func newVMProgramState(program *ast.Program, functions map[string]*ast.FunctionDef, routes []*Route, base map[string]grabrt.Value, opts Options) (*vmProgramState, error) {
	mode := resolveEngineMode(opts)
	if mode == EngineAST {
		return nil, nil
	}

	hotFn := int64(opts.HotFunctionThreshold)
	if hotFn <= 0 {
		hotFn = DefaultHotFunctionThreshold
	}
	hotLoop := int64(opts.HotLoopThreshold)
	if hotLoop <= 0 {
		hotLoop = DefaultHotLoopThreshold
	}
	hotRoute := int64(opts.HotRouteThreshold)
	if hotRoute <= 0 {
		hotRoute = DefaultHotRouteThreshold
	}

	usesData := false
	var fnDefs []*ast.FunctionDef
	funcIndex := make(map[string]int)
	for _, stmt := range program.Statements {
		switch node := stmt.(type) {
		case *ast.UseDecl:
			if node.Module == "data" {
				usesData = true
			}
		case *ast.FunctionDef:
			funcIndex[node.Name] = len(fnDefs)
			fnDefs = append(fnDefs, node)
		}
	}

	fnNames := make([]string, len(fnDefs))
	fnSlots := make([]*vmUnitSlot, len(fnDefs))
	for i, fn := range fnDefs {
		fnNames[i] = fn.Name
		fnSlots[i] = &vmUnitSlot{}
	}

	var baseNames []string
	var baseValues []grabrt.Value
	if len(base) > 0 {
		baseNames = make([]string, 0, len(base))
		for k := range base {
			baseNames = append(baseNames, k)
		}
		sort.Strings(baseNames)
		baseValues = make([]grabrt.Value, len(baseNames))
		for i, k := range baseNames {
			baseValues[i] = base[k]
		}
	}

	routeSlots := make(map[*Route]*vmUnitSlot, len(routes))
	for _, r := range routes {
		routeSlots[r] = &vmUnitSlot{}
	}

	st := &vmProgramState{
		mode:              mode,
		hotFnThreshold:    hotFn,
		hotLoopThreshold:  hotLoop,
		hotRouteThreshold: hotRoute,
		usesData:          usesData,
		funcDefs:          fnDefs,
		funcNames:         fnNames,
		funcIndex:         funcIndex,
		fnSlots:           fnSlots,
		topStmt:           program.Statements,
		topSpan:           program.Span,
		topSlot:           &vmUnitSlot{},
		baseNames:         baseNames,
		baseValues:        baseValues,
		routes:            routeSlots,
	}

	if mode == EngineVM {
		if err := st.compileAll(program); err != nil {
			return nil, err
		}
	}
	return st, nil
}

func (s *vmProgramState) compileAll(program *ast.Program) error {
	irProg, err := ir.CompileProgram(program, s.baseNames)
	if err != nil {
		return err
	}
	numFuncs := len(s.funcDefs)
	for i, u := range irProg.Functions {
		chunk, err := vm.CompileUnit(u, numFuncs)
		if err != nil {
			return err
		}
		s.fnSlots[i].once.Do(func() {
			s.fnSlots[i].chunk.Store(chunk)
		})
	}
	topChunk, err := vm.CompileUnit(irProg.TopLevel, numFuncs)
	if err != nil {
		return err
	}
	s.topSlot.once.Do(func() {
		s.topSlot.chunk.Store(topChunk)
	})
	for r, slot := range s.routes {
		ru := irProg.Routes[r.Definition]
		rc, err := vm.CompileUnit(ru, numFuncs)
		if err != nil {
			return err
		}
		slot.once.Do(func() {
			slot.chunk.Store(rc)
		})
	}
	return nil
}

func (s *vmProgramState) FuncChunk(fnIdx int) (*vm.Chunk, string) {
	if fnIdx < 0 || fnIdx >= len(s.fnSlots) {
		return nil, ""
	}
	slot := s.fnSlots[fnIdx]
	chunk := slot.chunk.Load()
	if chunk == nil && s.mode == EngineAuto {
		if slot.calls.Add(1) >= s.hotFnThreshold || slot.loopIters.Load() >= s.hotLoopThreshold {
			chunk, _ = s.compileFunction(fnIdx)
		}
	}
	return chunk, s.funcNames[fnIdx]
}

func (s *vmProgramState) compileFunction(fnIdx int) (*vm.Chunk, error) {
	slot := s.fnSlots[fnIdx]
	slot.once.Do(func() {
		u, err := ir.CompileFunction(s.funcDefs[fnIdx], s.funcIndex, s.usesData)
		if err != nil {
			slot.err = err
			return
		}
		c, err := vm.CompileUnit(u, len(s.funcDefs))
		if err != nil {
			slot.err = err
			return
		}
		slot.chunk.Store(c)
	})
	return slot.chunk.Load(), slot.err
}

func (s *vmProgramState) compileRoute(r *Route) (*vm.Chunk, error) {
	slot := s.routes[r]
	if slot == nil {
		return nil, nil
	}
	slot.once.Do(func() {
		u, err := ir.CompileRoute(r.Definition, s.baseNames, s.funcIndex, s.usesData)
		if err != nil {
			slot.err = err
			return
		}
		c, err := vm.CompileUnit(u, len(s.funcDefs))
		if err != nil {
			slot.err = err
			return
		}
		slot.chunk.Store(c)
	})
	return slot.chunk.Load(), slot.err
}

func (s *vmProgramState) compileTopLevel(program *ast.Program) (*vm.Chunk, error) {
	s.topSlot.once.Do(func() {
		u, err := ir.CompileTopLevel(program)
		if err != nil {
			s.topSlot.err = err
			return
		}
		c, err := vm.CompileUnit(u, len(s.funcDefs))
		if err != nil {
			s.topSlot.err = err
			return
		}
		s.topSlot.chunk.Store(c)
	})
	return s.topSlot.chunk.Load(), s.topSlot.err
}

func (s *vmProgramState) fnSlotByName(name string) (int, *vmUnitSlot, bool) {
	if s == nil {
		return -1, nil, false
	}
	idx, ok := s.funcIndex[name]
	if !ok {
		return -1, nil, false
	}
	return idx, s.fnSlots[idx], true
}

// vmHostBridge connects internal/vm back to execContext without circular imports.
type vmHostBridge struct {
	ctx    *execContext
	vmProg *vmProgramState
}

func (b *vmHostBridge) CallModule(ctx context.Context, module, name string, span lexer.Span, args []grabrt.Value) (grabrt.Value, error) {
	if b.ctx.eventWork != nil && (module == "http" || module == "connect" || module == "data" && (name == "commit" || name == "rollback" || name == "close" || name == "begin" || name == "migrate")) {
		return grabrt.Value{}, workerDenied(span)
	}
	stdFn, ok := lookupStdlibFunc(module, name)
	if !ok {
		return grabrt.Value{}, &Error{Kind: UnknownFunction, Name: module + "." + name, Span: span}
	}
	prevCtx := b.ctx.ctx
	b.ctx.ctx = ctx
	val, err := stdFn.fn(b.ctx, span, args)
	b.ctx.ctx = prevCtx
	return val, err
}

func (b *vmHostBridge) PreCheckModule(module, name string, span lexer.Span) error {
	if b.ctx.eventWork != nil && (module == "http" || module == "connect" || module == "data" && (name == "commit" || name == "rollback" || name == "close" || name == "begin" || name == "migrate")) {
		return workerDenied(span)
	}
	return nil
}

func (b *vmHostBridge) EnterTxScope() any {
	if b.ctx.data == nil {
		return nil
	}
	return b.ctx.data.ActiveTransaction()
}

func (b *vmHostBridge) ExitTxScope(prev any) {
	if b.ctx.data == nil {
		return
	}
	var tx *data.Transaction
	if prev != nil {
		tx, _ = prev.(*data.Transaction)
	}
	b.ctx.data.RollbackNew(tx)
}

func (b *vmHostBridge) ActiveTransaction() any {
	if b.ctx.data == nil {
		return nil
	}
	tx := b.ctx.data.ActiveTransaction()
	if tx == nil {
		return nil
	}
	return tx
}

func (b *vmHostBridge) CallColdFunction(fnIdx int, name string, span lexer.Span, args []grabrt.Value, state *vm.ExecState) (grabrt.Value, error) {
	b.ctx.loopSteps = state.LoopSteps
	b.ctx.callDepth = state.CallDepth
	b.ctx.callStack = toInterpreterStack(state.CallStack)
	prevUnit := b.ctx.activeUnit
	if b.vmProg != nil && fnIdx >= 0 && fnIdx < len(b.vmProg.fnSlots) {
		b.ctx.activeUnit = b.vmProg.fnSlots[fnIdx]
	}

	val, err := b.ctx.invokeUserFunctionAST(name, span, args)

	b.ctx.activeUnit = prevUnit
	state.LoopSteps = b.ctx.loopSteps
	state.CallDepth = b.ctx.callDepth
	state.CallStack = toVMStack(b.ctx.callStack)
	return val, err
}

func (b *vmHostBridge) ForkTask(taskCtx context.Context, output io.Writer, stack []vm.StackFrame) (vm.HostBridge, func()) {
	childCtx := &execContext{
		metrics:        b.ctx.metrics,
		cacheManager:   b.ctx.cacheManager,
		integrations:   b.ctx.integrations,
		sockets:        b.ctx.sockets,
		socketIdentity: b.ctx.socketIdentity,
		socketCallback: b.ctx.socketCallback,
		trustedContext: b.ctx.trustedContext,
		contracts:      b.ctx.contracts,
		apiOperation:   b.ctx.apiOperation,
		apiRequest:     b.ctx.apiRequest,
		apiInput:       b.ctx.apiInput,
		data:           b.ctx.dataManager.Scope(),
		dataManager:    b.ctx.dataManager,
		ctx:            taskCtx,
		requestCtx:     b.ctx.requestCtx,
		output:         output,
		functions:      b.ctx.functions,
		vmProg:         b.ctx.vmProg,
		permissions:    b.ctx.permissions,
		dialContext:    b.ctx.dialContext,
		templateLoader: b.ctx.templateLoader,
		maxLoopSteps:   b.ctx.maxLoopSteps,
		maxCallDepth:   b.ctx.maxCallDepth,
		callDepth:      len(stack),
		callStack:      toInterpreterStack(stack),
	}
	return &vmHostBridge{ctx: childCtx, vmProg: b.vmProg}, func() { childCtx.data.Close() }
}

func (b *vmHostBridge) NewError(kind vm.ErrKind, name, customCode, message string, span lexer.Span, stack []vm.StackFrame, cause error) error {
	interpKind := mapVMErrorKind(kind)
	if kind == vm.ErrDatabaseOwnership {
		return databaseError(span, &data.Error{Code: data.Ownership, Message: message})
	}
	if kind == vm.ErrCacheValue {
		return cacheError(span, cache.ErrValue)
	}
	return &Error{
		Kind:       interpKind,
		Name:       name,
		CustomCode: customCode,
		Message:    message,
		Span:       span,
		Stack:      toInterpreterStack(stack),
		Err:        cause,
	}
}

func (b *vmHostBridge) InspectError(err error) (grabrt.Value, bool, bool) {
	var interpErr *Error
	if !errors.As(err, &interpErr) {
		return grabrt.Value{}, false, false
	}
	return interpErr.ErrorRecord(), interpErr.IsFatal(), true
}

func (b *vmHostBridge) AttachStack(err error, stack []vm.StackFrame) error {
	var interpErr *Error
	if errors.As(err, &interpErr) && len(stack) > 0 && len(interpErr.Stack) == 0 {
		interpErr.Stack = toInterpreterStack(stack)
	}
	return err
}

func (b *vmHostBridge) IsCanceledError(err error) bool {
	var interpErr *Error
	if errors.As(err, &interpErr) && interpErr.Err != nil {
		return errors.Is(interpErr.Err, context.Canceled)
	}
	return errors.Is(err, context.Canceled)
}

func (b *vmHostBridge) CheckContainsCacheHandle(val grabrt.Value) bool {
	return containsCacheHandle(val)
}

func mapVMErrorKind(k vm.ErrKind) ErrorKind {
	switch k {
	case vm.ErrJoinedRequiresText:
		return JoinedRequiresText
	case vm.ErrInvalidLiteral:
		return InvalidLiteral
	case vm.ErrDivisionByZero:
		return DivisionByZero
	case vm.ErrAbsentFieldOrKey:
		return AbsentFieldOrKey
	case vm.ErrIndexOutOfBounds:
		return IndexOutOfBounds
	case vm.ErrNegativeRepeatCount:
		return NegativeRepeatCount
	case vm.ErrCallStackOverflow:
		return CallStackOverflow
	case vm.ErrExecutionLimitExceeded:
		return ExecutionLimitExceeded
	case vm.ErrUserRaisedError:
		return UserRaisedError
	case vm.ErrStandardLibraryError:
		return StandardLibraryError
	case vm.ErrRequestTimeout:
		return RequestTimeout
	case vm.ErrDatabaseOwnership:
		return DatabaseOwnership
	default:
		return TypeMismatch
	}
}

func toInterpreterStack(in []vm.StackFrame) []StackFrame {
	if len(in) == 0 {
		return nil
	}
	out := make([]StackFrame, len(in))
	for i, f := range in {
		out[i] = StackFrame{Function: f.Function, Span: f.Span}
	}
	return out
}

func toVMStack(in []StackFrame) []vm.StackFrame {
	if len(in) == 0 {
		return nil
	}
	out := make([]vm.StackFrame, len(in))
	for i, f := range in {
		out[i] = vm.StackFrame{Function: f.Function, Span: f.Span}
	}
	return out
}

func (ctx *execContext) execVMChunk(chunk *vm.Chunk, baseValues, params []grabrt.Value, trackRootSnapshot bool) (grabrt.Value, map[string]grabrt.Value, error) {
	state := vm.ExecState{
		Ctx:          ctx.ctx,
		RequestCtx:   ctx.requestCtx,
		Output:       ctx.output,
		LoopSteps:    ctx.loopSteps,
		MaxLoopSteps: ctx.maxLoopSteps,
		CallDepth:    ctx.callDepth,
		MaxCallDepth: ctx.maxCallDepth,
		CallStack:    toVMStack(ctx.callStack),
	}
	bridge := vmHostBridge{ctx: ctx, vmProg: ctx.vmProg}
	val, snap, err := vm.Execute(chunk, ctx.vmProg, &state, &bridge, baseValues, params, trackRootSnapshot)
	ctx.loopSteps = state.LoopSteps
	ctx.callDepth = state.CallDepth
	if len(state.CallStack) > 0 || len(ctx.callStack) > 0 {
		ctx.callStack = toInterpreterStack(state.CallStack)
	}
	return val, snap, err
}
