package vm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"grab/internal/ir"
	"grab/internal/lexer"
	grabrt "grab/internal/runtime"
)

const (
	DefaultMaxLoopSteps = 1_000_000
	DefaultMaxCallDepth = 256
)

// ProgramLookup resolves compiled function chunks by index for OpCall.
type ProgramLookup interface {
	FuncChunk(fnIdx int) (chunk *Chunk, name string)
}

// ExecState tracks execution safety bounds and call stack across AST/VM boundaries.
type ExecState struct {
	Ctx          context.Context
	RequestCtx   context.Context
	Output       io.Writer
	LoopSteps    int
	MaxLoopSteps int
	CallDepth    int
	MaxCallDepth int
	CallStack    []StackFrame
}

type iterState struct {
	remaining int64
	isMap     bool
	items     []grabrt.Value
	entries   []grabrt.Entry
	idx       int
}

type callFrame struct {
	chunk      *Chunk
	pc         int
	stackBase  int
	localBase  int
	iterBase   int
	txBase     int
	openTx     int
	returnSlot bool // true if caller expects 1 return value on stack
}

var machinePool = sync.Pool{
	New: func() any {
		return &machine{
			stack:  make([]grabrt.Value, 0, 32),
			locals: make([]grabrt.Value, 0, 32),
			iters:  make([]iterState, 0, 8),
			frames: make([]callFrame, 0, 16),
		}
	},
}

func borrowMachine(chunk *Chunk, prog ProgramLookup, state *ExecState, host HostBridge, trackRootSnapshot bool) *machine {
	m := machinePool.Get().(*machine)
	m.prog = prog
	m.state = state
	m.host = host
	m.trackRootSnapshot = trackRootSnapshot

	minStack := maxInt(chunk.MaxStack, 16)
	if cap(m.stack) < minStack {
		m.stack = make([]grabrt.Value, 0, minStack)
	} else {
		m.stack = m.stack[:0]
	}
	if cap(m.locals) < chunk.NumLocals {
		m.locals = make([]grabrt.Value, chunk.NumLocals)
	} else {
		m.locals = m.locals[:chunk.NumLocals]
		clear(m.locals)
	}
	if cap(m.iters) < chunk.NumIters {
		m.iters = make([]iterState, chunk.NumIters)
	} else {
		m.iters = m.iters[:chunk.NumIters]
		clear(m.iters)
	}
	if cap(m.txTokens) < chunk.MaxTxDepth {
		m.txTokens = make([]any, chunk.MaxTxDepth)
	} else {
		m.txTokens = m.txTokens[:chunk.MaxTxDepth]
		clear(m.txTokens)
	}
	if cap(m.frames) < 16 {
		m.frames = make([]callFrame, 1, 16)
	} else {
		m.frames = m.frames[:1]
	}
	if trackRootSnapshot {
		if cap(m.rootAssigned) < chunk.NumLocals {
			m.rootAssigned = make([]bool, chunk.NumLocals)
		} else {
			m.rootAssigned = m.rootAssigned[:chunk.NumLocals]
			clear(m.rootAssigned)
		}
	} else {
		m.rootAssigned = m.rootAssigned[:0]
	}
	return m
}

func releaseMachine(m *machine) {
	if cap(m.stack) > 4096 || cap(m.locals) > 4096 || cap(m.frames) > 512 {
		return
	}
	m.prog = nil
	m.state = nil
	m.host = nil
	clear(m.stack[:cap(m.stack)])
	clear(m.locals)
	clear(m.iters)
	clear(m.txTokens)
	clear(m.frames)
	m.stack = m.stack[:0]
	m.locals = m.locals[:0]
	m.iters = m.iters[:0]
	m.txTokens = m.txTokens[:0]
	m.frames = m.frames[:0]
	machinePool.Put(m)
}

// Execute runs chunk in a VM instance, returning the final return value
// and (if trackRootSnapshot is true) the assigned root variable bindings.
func Execute(
	chunk *Chunk,
	prog ProgramLookup,
	state *ExecState,
	host HostBridge,
	baseValues []grabrt.Value,
	params []grabrt.Value,
	trackRootSnapshot bool,
) (grabrt.Value, map[string]grabrt.Value, error) {
	if chunk == nil {
		return grabrt.Value{}, nil, fmt.Errorf("nil VM chunk")
	}
	if state == nil {
		state = &ExecState{
			Ctx:          context.Background(),
			Output:       io.Discard,
			MaxLoopSteps: DefaultMaxLoopSteps,
			MaxCallDepth: DefaultMaxCallDepth,
		}
	}
	if state.MaxLoopSteps <= 0 {
		state.MaxLoopSteps = DefaultMaxLoopSteps
	}
	if state.MaxCallDepth <= 0 {
		state.MaxCallDepth = DefaultMaxCallDepth
	}
	if state.Output == nil {
		state.Output = io.Discard
	}
	if state.Ctx == nil {
		state.Ctx = context.Background()
	}

	m := borrowMachine(chunk, prog, state, host, trackRootSnapshot)
	defer releaseMachine(m)

	for i := 0; i < len(baseValues) && i < chunk.BaseSlotCount; i++ {
		m.locals[i] = baseValues[i]
		if trackRootSnapshot {
			m.rootAssigned[i] = true
		}
	}
	paramStart := chunk.BaseSlotCount
	for i := 0; i < len(params) && i < chunk.NumParams; i++ {
		idx := paramStart + i
		if idx < len(m.locals) {
			m.locals[idx] = params[i]
			if trackRootSnapshot {
				m.rootAssigned[idx] = true
			}
		}
	}

	m.frames[0] = callFrame{
		chunk:     chunk,
		pc:        0,
		stackBase: 0,
		localBase: 0,
		iterBase:  0,
		txBase:    0,
	}

	retVal, err := m.run()
	if err != nil {
		return grabrt.Value{}, nil, err
	}

	var snapshot map[string]grabrt.Value
	if trackRootSnapshot {
		snapshot = make(map[string]grabrt.Value, len(chunk.RootSlots))
		for _, rs := range chunk.RootSlots {
			if rs.Slot >= 0 && rs.Slot < len(m.rootAssigned) && m.rootAssigned[rs.Slot] {
				snapshot[rs.Name] = m.locals[rs.Slot]
			}
		}
	}
	return retVal, snapshot, nil
}

type machine struct {
	prog              ProgramLookup
	state             *ExecState
	host              HostBridge
	trackRootSnapshot bool
	rootAssigned      []bool

	stack    []grabrt.Value
	locals   []grabrt.Value
	iters    []iterState
	txTokens []any
	frames   []callFrame
}

func (m *machine) run() (grabrt.Value, error) {
	for len(m.frames) > 0 {
		f := &m.frames[len(m.frames)-1]
		c := f.chunk
		if f.pc >= len(c.Code) {
			m.unwindFrameTx(f, 0)
			if len(m.frames) == 1 {
				return grabrt.Nothing(), nil
			}
			m.popFrame()
			m.stack = append(m.stack, grabrt.Nothing())
			continue
		}

		// Periodically / on request context check cancellation.
		if m.state.RequestCtx != nil && m.state.RequestCtx.Err() != nil {
			err := m.makeError(ErrRequestTimeout, "", "", "", c.Spans[f.pc], m.state.RequestCtx.Err())
			if handled := m.handleError(err); !handled {
				m.unwindAllTx()
				return grabrt.Value{}, err
			}
			continue
		}

		pc := f.pc
		ins := c.Code[pc]
		f.pc = pc + 1

		switch ins.Op {
		case ir.OpConst:
			m.stack = append(m.stack, c.Constants[ins.A])
		case ir.OpNothing:
			m.stack = append(m.stack, grabrt.Nothing())
		case ir.OpTrue:
			m.stack = append(m.stack, grabrt.Boolean(true))
		case ir.OpFalse:
			m.stack = append(m.stack, grabrt.Boolean(false))
		case ir.OpFailLiteral:
			err := m.makeError(ErrInvalidLiteral, "", "", "", c.Spans[pc], fmt.Errorf("%s", c.Strings[ins.A]))
			if !m.handleError(err) {
				m.unwindAllTx()
				return grabrt.Value{}, err
			}
		case ir.OpLoadSlot:
			m.stack = append(m.stack, m.locals[f.localBase+int(ins.A)])
		case ir.OpStoreSlot:
			val := m.pop()
			slot := f.localBase + int(ins.A)
			m.locals[slot] = val
			if m.trackRootSnapshot && len(m.frames) == 1 && int(ins.A) < len(m.rootAssigned) {
				m.rootAssigned[ins.A] = true
			}
		case ir.OpPop:
			m.stack = m.stack[:len(m.stack)-1]
		case ir.OpDup:
			m.stack = append(m.stack, m.stack[len(m.stack)-1])

		case ir.OpAdd, ir.OpSub, ir.OpMul, ir.OpDiv, ir.OpMod:
			right := m.pop()
			left := m.pop()
			if left.ContainsResource() || right.ContainsResource() {
				if err := m.raiseOrPropagate(m.makeError(ErrTypeMismatch, "", "", "", c.Spans[pc], nil)); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			var val grabrt.Value
			var opErr error
			switch ins.Op {
			case ir.OpAdd:
				val, opErr = grabrt.Add(left, right)
			case ir.OpSub:
				val, opErr = grabrt.Sub(left, right)
			case ir.OpMul:
				val, opErr = grabrt.Mul(left, right)
			case ir.OpDiv:
				val, opErr = grabrt.Div(left, right)
			case ir.OpMod:
				val, opErr = grabrt.Mod(left, right)
			}
			if opErr != nil {
				kind := ErrTypeMismatch
				var cause error = opErr
				if errors.Is(opErr, grabrt.ErrDivisionByZero) {
					kind = ErrDivisionByZero
					cause = nil
				}
				if err := m.raiseOrPropagate(m.makeError(kind, "", "", "", c.Spans[pc], cause)); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			m.stack = append(m.stack, val)

		case ir.OpJoin:
			right := m.pop()
			left := m.pop()
			if left.Kind() != grabrt.TextKind || right.Kind() != grabrt.TextKind {
				if err := m.raiseOrPropagate(m.makeError(ErrJoinedRequiresText, "", "", "", c.Spans[pc], nil)); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			m.stack = append(m.stack, grabrt.Text(left.TextValue()+right.TextValue()))

		case ir.OpEq:
			right := m.pop()
			left := m.pop()
			if left.ContainsResource() || right.ContainsResource() {
				if err := m.raiseOrPropagate(m.makeError(ErrTypeMismatch, "", "", "", c.Spans[pc], nil)); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			m.stack = append(m.stack, grabrt.Boolean(left.Equal(right)))

		case ir.OpNeq:
			right := m.pop()
			left := m.pop()
			if left.ContainsResource() || right.ContainsResource() {
				if err := m.raiseOrPropagate(m.makeError(ErrTypeMismatch, "", "", "", c.Spans[pc], nil)); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			m.stack = append(m.stack, grabrt.Boolean(!left.Equal(right)))

		case ir.OpGt, ir.OpLt, ir.OpGte, ir.OpLte:
			right := m.pop()
			left := m.pop()
			if left.ContainsResource() || right.ContainsResource() {
				if err := m.raiseOrPropagate(m.makeError(ErrTypeMismatch, "", "", "", c.Spans[pc], nil)); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			cmp, ok := left.Compare(right)
			if !ok {
				if err := m.raiseOrPropagate(m.makeError(ErrTypeMismatch, "", "", "", c.Spans[pc], nil)); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			var res bool
			switch ins.Op {
			case ir.OpGt:
				res = cmp > 0
			case ir.OpLt:
				res = cmp < 0
			case ir.OpGte:
				res = cmp >= 0
			case ir.OpLte:
				res = cmp <= 0
			}
			m.stack = append(m.stack, grabrt.Boolean(res))

		case ir.OpNot:
			operand := m.pop()
			if operand.Kind() != grabrt.BooleanKind {
				if err := m.raiseOrPropagate(m.makeError(ErrTypeMismatch, "", "", "", c.Spans[pc], nil)); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			m.stack = append(m.stack, grabrt.Boolean(!operand.BooleanValue()))

		case ir.OpBuildList:
			n := int(ins.A)
			start := len(m.stack) - n
			vals := make([]grabrt.Value, n)
			copy(vals, m.stack[start:])
			m.stack = m.stack[:start]
			m.stack = append(m.stack, grabrt.List(vals))

		case ir.OpBuildMap, ir.OpBuildRecord:
			n := int(ins.A)
			keys := c.KeyLists[ins.B]
			start := len(m.stack) - n
			entries := make([]grabrt.Entry, n)
			for i := 0; i < n; i++ {
				entries[i] = grabrt.Entry{Name: keys[i], Value: m.stack[start+i]}
			}
			m.stack = m.stack[:start]
			if ins.Op == ir.OpBuildMap {
				m.stack = append(m.stack, grabrt.Map(entries))
			} else {
				m.stack = append(m.stack, grabrt.Record(entries))
			}

		case ir.OpField:
			target := m.pop()
			if target.Kind() != grabrt.RecordKind && target.Kind() != grabrt.MapKind {
				if err := m.raiseOrPropagate(m.makeError(ErrTypeMismatch, "", "", "", c.Spans[pc], nil)); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			field := c.Strings[ins.A]
			val, ok := target.LookupEntry(field)
			if !ok {
				if err := m.raiseOrPropagate(m.makeError(ErrAbsentFieldOrKey, field, "", "", c.AuxSpans[ins.B], nil)); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			m.stack = append(m.stack, val)

		case ir.OpAt:
			idxVal := m.pop()
			target := m.pop()
			idxSpan := c.AuxSpans[ins.A]
			switch target.Kind() {
			case grabrt.ListKind:
				if idxVal.Kind() != grabrt.IntegerKind {
					if err := m.raiseOrPropagate(m.makeError(ErrTypeMismatch, "", "", "", c.Spans[pc], nil)); err != nil {
						return grabrt.Value{}, err
					}
					continue
				}
				bigIdx := idxVal.IntegerValue()
				if bigIdx.Sign() < 0 || !bigIdx.IsInt64() {
					if err := m.raiseOrPropagate(m.makeError(ErrIndexOutOfBounds, "", "", "", idxSpan, nil)); err != nil {
						return grabrt.Value{}, err
					}
					continue
				}
				val, ok := target.ListAt(int(bigIdx.Int64()))
				if !ok {
					if err := m.raiseOrPropagate(m.makeError(ErrIndexOutOfBounds, "", "", "", idxSpan, nil)); err != nil {
						return grabrt.Value{}, err
					}
					continue
				}
				m.stack = append(m.stack, val)
			case grabrt.MapKind, grabrt.RecordKind:
				if idxVal.Kind() != grabrt.TextKind {
					if err := m.raiseOrPropagate(m.makeError(ErrTypeMismatch, "", "", "", c.Spans[pc], nil)); err != nil {
						return grabrt.Value{}, err
					}
					continue
				}
				key := idxVal.TextValue()
				val, ok := target.LookupEntry(key)
				if !ok {
					if err := m.raiseOrPropagate(m.makeError(ErrAbsentFieldOrKey, key, "", "", idxSpan, nil)); err != nil {
						return grabrt.Value{}, err
					}
					continue
				}
				m.stack = append(m.stack, val)
			default:
				if err := m.raiseOrPropagate(m.makeError(ErrTypeMismatch, "", "", "", c.Spans[pc], nil)); err != nil {
					return grabrt.Value{}, err
				}
			}

		case ir.OpHas:
			target := m.pop()
			if target.Kind() != grabrt.MapKind && target.Kind() != grabrt.RecordKind {
				if err := m.raiseOrPropagate(m.makeError(ErrTypeMismatch, "", "", "", c.Spans[pc], nil)); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			_, ok := target.LookupEntry(c.Strings[ins.A])
			m.stack = append(m.stack, grabrt.Boolean(ok))

		case ir.OpJump:
			f.pc = int(ins.A)

		case ir.OpJumpIfFalse:
			cond := m.pop()
			if cond.Kind() != grabrt.BooleanKind {
				if err := m.raiseOrPropagate(m.makeError(ErrTypeMismatch, "", "", "", c.Spans[pc], nil)); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			if !cond.BooleanValue() {
				f.pc = int(ins.A)
			}

		case ir.OpJumpIfFalseKeep:
			top := m.stack[len(m.stack)-1]
			if top.Kind() != grabrt.BooleanKind {
				m.pop()
				if err := m.raiseOrPropagate(m.makeError(ErrTypeMismatch, "", "", "", c.Spans[pc], nil)); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			if !top.BooleanValue() {
				f.pc = int(ins.A)
			} else {
				m.pop()
			}

		case ir.OpJumpIfTrueKeep:
			top := m.stack[len(m.stack)-1]
			if top.Kind() != grabrt.BooleanKind {
				m.pop()
				if err := m.raiseOrPropagate(m.makeError(ErrTypeMismatch, "", "", "", c.Spans[pc], nil)); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			if top.BooleanValue() {
				f.pc = int(ins.A)
			} else {
				m.pop()
			}

		case ir.OpCheckBool:
			top := m.stack[len(m.stack)-1]
			if top.Kind() != grabrt.BooleanKind {
				m.pop()
				if err := m.raiseOrPropagate(m.makeError(ErrTypeMismatch, "", "", "", c.Spans[pc], nil)); err != nil {
					return grabrt.Value{}, err
				}
			}

		case ir.OpRepeatInit:
			countVal := m.pop()
			countSpan := c.AuxSpans[ins.B]
			if countVal.Kind() != grabrt.IntegerKind {
				if err := m.raiseOrPropagate(m.makeError(ErrTypeMismatch, "", "", "", countSpan, nil)); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			bigCount := countVal.IntegerValue()
			if bigCount.Sign() < 0 {
				if err := m.raiseOrPropagate(m.makeError(ErrNegativeRepeatCount, "", "", "", countSpan, nil)); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			if !bigCount.IsInt64() || bigCount.Int64() > int64(m.state.MaxLoopSteps) {
				if err := m.raiseOrPropagate(m.makeError(ErrExecutionLimitExceeded, "", "", "", c.Spans[pc], nil)); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			m.iters[f.iterBase+int(ins.A)] = iterState{remaining: bigCount.Int64()}

		case ir.OpRepeatNext:
			it := &m.iters[f.iterBase+int(ins.A)]
			if it.remaining <= 0 {
				f.pc = int(ins.B)
				continue
			}
			it.remaining--
			if err := m.stepLoop(c.Spans[pc]); err != nil {
				if err := m.raiseOrPropagate(err); err != nil {
					return grabrt.Value{}, err
				}
			}

		case ir.OpIterInit:
			collVal := m.pop()
			collSpan := c.AuxSpans[ins.B]
			itIdx := f.iterBase + int(ins.A)
			switch collVal.Kind() {
			case grabrt.ListKind:
				m.iters[itIdx] = iterState{isMap: false, items: collVal.ListValues(), idx: 0}
			case grabrt.MapKind:
				m.iters[itIdx] = iterState{isMap: true, entries: collVal.Entries(), idx: 0}
			default:
				if err := m.raiseOrPropagate(m.makeError(ErrTypeMismatch, "", "", "", collSpan, nil)); err != nil {
					return grabrt.Value{}, err
				}
			}

		case ir.OpIterNext:
			it := &m.iters[f.iterBase+int(ins.A)]
			var item grabrt.Value
			if it.isMap {
				if it.idx >= len(it.entries) {
					f.pc = int(ins.C)
					continue
				}
				if err := m.stepLoop(c.Spans[pc]); err != nil {
					if err := m.raiseOrPropagate(err); err != nil {
						return grabrt.Value{}, err
					}
					continue
				}
				item = grabrt.Text(it.entries[it.idx].Name)
			} else {
				if it.idx >= len(it.items) {
					f.pc = int(ins.C)
					continue
				}
				if err := m.stepLoop(c.Spans[pc]); err != nil {
					if err := m.raiseOrPropagate(err); err != nil {
						return grabrt.Value{}, err
					}
					continue
				}
				item = it.items[it.idx]
			}
			it.idx++
			m.locals[f.localBase+int(ins.B)] = item

		case ir.OpLoopStep:
			if err := m.stepLoop(c.Spans[pc]); err != nil {
				if err := m.raiseOrPropagate(err); err != nil {
					return grabrt.Value{}, err
				}
			}

		case ir.OpTxEnter:
			if m.host != nil {
				depth := int(ins.A)
				m.txTokens[f.txBase+depth] = m.host.EnterTxScope()
				if depth+1 > f.openTx {
					f.openTx = depth + 1
				}
			}

		case ir.OpTxExit:
			if m.host != nil {
				depth := int(ins.A)
				m.host.ExitTxScope(m.txTokens[f.txBase+depth])
				m.txTokens[f.txBase+depth] = nil
				if f.openTx > depth {
					f.openTx = depth
				}
			}

		case ir.OpPreCheckHost:
			if m.host != nil {
				ref := c.HostCalls[ins.A]
				if err := m.host.PreCheckModule(ref.Module, ref.Name, c.Spans[pc]); err != nil {
					if err := m.raiseOrPropagate(m.attachStack(err)); err != nil {
						return grabrt.Value{}, err
					}
				}
			}

		case ir.OpCallHost:
			ref := c.HostCalls[ins.A]
			arity := int(ins.B)
			start := len(m.stack) - arity
			args := m.stack[start:]
			if m.host == nil {
				m.stack = m.stack[:start]
				if err := m.raiseOrPropagate(m.makeError(ErrStandardLibraryError, ref.Module+"."+ref.Name, "", "", c.Spans[pc], fmt.Errorf("missing host bridge"))); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			// Copy args slice only if needed? CallModule shouldn't retain args slice,
			// so passing m.stack[start:] after popping is safe as long as CallModule is synchronous
			// (unless it invokes cold/VM callbacks which use m.stack—wait: host calls NEVER call user functions except sockets/events which make new ExecContexts!).
			// To be 100% safe against aliasing if any stdlib mutates or grows stack, copy if arity > 0 or slice before shrinking.
			var callArgs []grabrt.Value
			if arity > 0 {
				var buf [8]grabrt.Value
				if arity <= len(buf) {
					callArgs = buf[:arity]
				} else {
					callArgs = make([]grabrt.Value, arity)
				}
				copy(callArgs, args)
			}
			m.stack = m.stack[:start]
			val, err := m.host.CallModule(m.state.Ctx, ref.Module, ref.Name, c.Spans[pc], callArgs)
			if err != nil {
				if err := m.raiseOrPropagate(m.attachStack(err)); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			m.stack = append(m.stack, val)

		case ir.OpCall:
			fnIdx := int(ins.A)
			arity := int(ins.B)
			callSpan := c.Spans[pc]
			var callee *Chunk
			var fnName string
			if m.prog != nil {
				callee, fnName = m.prog.FuncChunk(fnIdx)
			}
			if m.state.CallDepth+1 > m.state.MaxCallDepth {
				m.stack = m.stack[:len(m.stack)-arity]
				err := m.makeError(ErrCallStackOverflow, "", "", "", callSpan, nil)
				if err := m.raiseOrPropagate(err); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			if callee == nil {
				// Cold function fallback via HostBridge in auto tiering mode.
				start := len(m.stack) - arity
				args := make([]grabrt.Value, arity)
				copy(args, m.stack[start:])
				m.stack = m.stack[:start]
				if m.host == nil {
					err := m.makeError(ErrStandardLibraryError, fnName, "", "", callSpan, fmt.Errorf("missing callee chunk"))
					if err := m.raiseOrPropagate(err); err != nil {
						return grabrt.Value{}, err
					}
					continue
				}
				val, err := m.host.CallColdFunction(fnIdx, fnName, callSpan, args, m.state)
				if err != nil {
					if err := m.raiseOrPropagate(m.attachStack(err)); err != nil {
						return grabrt.Value{}, err
					}
					continue
				}
				m.stack = append(m.stack, val)
				continue
			}

			m.state.CallDepth++
			m.state.CallStack = append(m.state.CallStack, StackFrame{Function: fnName, Span: callSpan})
			argStart := len(m.stack) - arity
			newLocalBase := f.localBase + c.NumLocals
			newIterBase := f.iterBase + c.NumIters
			newTxBase := f.txBase + c.MaxTxDepth
			m.ensureCapacity(newLocalBase+callee.NumLocals, newIterBase+callee.NumIters, newTxBase+callee.MaxTxDepth)
			for i := 0; i < arity; i++ {
				m.locals[newLocalBase+i] = m.stack[argStart+i]
			}
			// Zero remaining local slots for clean state across calls.
			for i := arity; i < callee.NumLocals; i++ {
				m.locals[newLocalBase+i] = grabrt.Value{}
			}
			m.stack = m.stack[:argStart]
			m.frames = append(m.frames, callFrame{
				chunk:     callee,
				pc:        0,
				stackBase: len(m.stack),
				localBase: newLocalBase,
				iterBase:  newIterBase,
				txBase:    newTxBase,
			})

		case ir.OpReturn:
			retVal := m.pop()
			m.unwindFrameTx(f, 0)
			if len(m.frames) == 1 {
				return retVal, nil
			}
			m.popFrame()
			m.stack = append(m.stack, retVal)

		case ir.OpReturnNothing:
			m.unwindFrameTx(f, 0)
			if len(m.frames) == 1 {
				return grabrt.Nothing(), nil
			}
			m.popFrame()
			m.stack = append(m.stack, grabrt.Nothing())

		case ir.OpShow:
			val := m.pop()
			if val.ContainsResource() {
				if err := m.raiseOrPropagate(m.makeError(ErrTypeMismatch, "", "", "", c.Spans[pc], nil)); err != nil {
					return grabrt.Value{}, err
				}
				continue
			}
			if _, err := io.WriteString(m.state.Output, val.Display()+"\n"); err != nil {
				m.unwindAllTx()
				return grabrt.Value{}, err
			}

		case ir.OpRaise:
			val := m.pop()
			span := c.Spans[pc]
			var raiseErr error
			switch val.Kind() {
			case grabrt.TextKind:
				raiseErr = m.makeError(ErrUserRaisedError, "", "", val.TextValue(), span, nil)
			case grabrt.RecordKind:
				msgVal, ok := val.LookupEntry("message")
				if !ok || msgVal.Kind() != grabrt.TextKind {
					raiseErr = m.makeError(ErrTypeMismatch, "", "", "", span, nil)
				} else {
					customCode := ""
					if codeVal, hasCode := val.LookupEntry("code"); hasCode {
						if codeVal.Kind() != grabrt.TextKind || codeVal.TextValue() == "" {
							raiseErr = m.makeError(ErrTypeMismatch, "", "", "", span, nil)
							break
						}
						customCode = codeVal.TextValue()
					}
					raiseErr = m.makeError(ErrUserRaisedError, "", customCode, msgVal.TextValue(), span, nil)
				}
			default:
				raiseErr = m.makeError(ErrTypeMismatch, "", "", "", span, nil)
			}
			if err := m.raiseOrPropagate(raiseErr); err != nil {
				return grabrt.Value{}, err
			}

		case ir.OpCatchEnter:
			// Caught error record was pushed onto stack by handleError.
			rec := m.pop()
			m.locals[f.localBase+int(ins.A)] = rec
			if m.trackRootSnapshot && len(m.frames) == 1 && int(ins.A) < len(m.rootAssigned) {
				m.rootAssigned[ins.A] = true
			}

		case ir.OpDoTogether:
			plan := c.DoTogethers[ins.A]
			if err := m.execDoTogether(f, plan); err != nil {
				if err := m.raiseOrPropagate(err); err != nil {
					return grabrt.Value{}, err
				}
			}

		default:
			m.unwindAllTx()
			return grabrt.Value{}, fmt.Errorf("unknown VM opcode %d", ins.Op)
		}
	}
	return grabrt.Nothing(), nil
}

func (m *machine) pop() grabrt.Value {
	top := m.stack[len(m.stack)-1]
	m.stack = m.stack[:len(m.stack)-1]
	return top
}

func (m *machine) popFrame() {
	m.frames = m.frames[:len(m.frames)-1]
	m.state.CallDepth--
	if len(m.state.CallStack) > 0 {
		m.state.CallStack = m.state.CallStack[:len(m.state.CallStack)-1]
	}
}

func (m *machine) ensureCapacity(reqLocals, reqIters, reqTx int) {
	if reqLocals > len(m.locals) {
		grown := make([]grabrt.Value, maxInt(reqLocals, len(m.locals)*2))
		copy(grown, m.locals)
		m.locals = grown
	}
	if reqIters > len(m.iters) {
		grown := make([]iterState, maxInt(reqIters, len(m.iters)*2))
		copy(grown, m.iters)
		m.iters = grown
	}
	if reqTx > len(m.txTokens) {
		grown := make([]any, maxInt(reqTx, len(m.txTokens)*2))
		copy(grown, m.txTokens)
		m.txTokens = grown
	}
}

func (m *machine) stepLoop(span lexer.Span) error {
	m.state.LoopSteps++
	if m.state.LoopSteps > m.state.MaxLoopSteps {
		return m.makeError(ErrExecutionLimitExceeded, "", "", "", span, nil)
	}
	if m.state.Ctx != nil {
		if err := m.state.Ctx.Err(); err != nil {
			return m.makeError(ErrRequestTimeout, "", "", "", span, err)
		}
	}
	return nil
}

func (m *machine) unwindFrameTx(f *callFrame, targetDepth int) {
	if m.host == nil || f.openTx <= targetDepth {
		return
	}
	for d := f.openTx - 1; d >= targetDepth; d-- {
		m.host.ExitTxScope(m.txTokens[f.txBase+d])
		m.txTokens[f.txBase+d] = nil
	}
	f.openTx = targetDepth
}

func (m *machine) unwindAllTx() {
	for i := len(m.frames) - 1; i >= 0; i-- {
		m.unwindFrameTx(&m.frames[i], 0)
	}
}

func (m *machine) currentReverseStack() []StackFrame {
	if len(m.state.CallStack) == 0 {
		return nil
	}
	frames := make([]StackFrame, len(m.state.CallStack))
	for i := range m.state.CallStack {
		frames[i] = m.state.CallStack[len(m.state.CallStack)-1-i]
	}
	return frames
}

func (m *machine) makeError(kind ErrKind, name, customCode, message string, span lexer.Span, cause error) error {
	stack := m.currentReverseStack()
	if m.host != nil {
		return m.host.NewError(kind, name, customCode, message, span, stack, cause)
	}
	return &RuntimeError{
		Kind:       kind,
		Name:       name,
		CustomCode: customCode,
		Message:    message,
		Span:       span,
		Stack:      stack,
		Err:        cause,
	}
}

func (m *machine) attachStack(err error) error {
	if err == nil {
		return nil
	}
	if m.host != nil {
		return m.host.AttachStack(err, m.currentReverseStack())
	}
	return err
}

func (m *machine) inspectError(err error) (grabrt.Value, bool, bool) {
	if m.host != nil {
		return m.host.InspectError(err)
	}
	var re *RuntimeError
	if errors.As(err, &re) {
		return re.ErrorRecord(), re.IsFatal(), true
	}
	return grabrt.Value{}, false, false
}

func (m *machine) raiseOrPropagate(err error) error {
	if m.handleError(err) {
		return nil
	}
	m.unwindAllTx()
	return err
}

// handleError searches active frames from innermost to outermost for a matching
// try/catch handler. Returns true if caught and control jumped to HandlerPC.
func (m *machine) handleError(err error) bool {
	rec, fatal, ok := m.inspectError(err)
	if !ok || fatal {
		return false
	}
	for len(m.frames) > 0 {
		f := &m.frames[len(m.frames)-1]
		faultPC := f.pc - 1
		for _, h := range f.chunk.Handlers {
			if faultPC >= h.StartPC && faultPC < h.EndPC {
				m.unwindFrameTx(f, h.TxDepth)
				m.stack = m.stack[:f.stackBase+h.StackDepth]
				m.stack = append(m.stack, rec) // consumed immediately by OpCatchEnter
				f.pc = h.HandlerPC
				return true
			}
		}
		m.unwindFrameTx(f, 0)
		m.stack = m.stack[:f.stackBase]
		if len(m.frames) == 1 {
			return false
		}
		m.popFrame()
	}
	return false
}

func (m *machine) execDoTogether(f *callFrame, plan CompiledDoTogether) error {
	if m.host != nil {
		for _, slot := range plan.CaptureSlots {
			if m.host.CheckContainsCacheHandle(m.locals[f.localBase+int(slot)]) {
				return m.makeError(ErrCacheValue, "", "", "", plan.Span, nil)
			}
		}
		if m.host.ActiveTransaction() != nil {
			return m.makeError(ErrDatabaseOwnership, "", "", "transactions cannot be shared with task groups", plan.Span, nil)
		}
	}

	taskBase := make([]grabrt.Value, len(plan.CaptureSlots))
	for i, slot := range plan.CaptureSlots {
		taskBase[i] = m.locals[f.localBase+int(slot)]
	}

	taskCtx, cancel := context.WithCancel(m.state.Ctx)
	defer cancel()

	type taskResult struct {
		val        grabrt.Value
		out        string
		err        error
		beforeStop bool
	}

	results := make([]taskResult, len(plan.Tasks))
	var wg sync.WaitGroup
	var cancelOnce sync.Once
	var mu sync.Mutex
	cancelledBySibling := false

	for i, t := range plan.Tasks {
		wg.Add(1)
		var buf bytes.Buffer
		var childHost HostBridge
		var cleanup func()
		if m.host != nil {
			childHost, cleanup = m.host.ForkTask(taskCtx, &buf, m.state.CallStack)
		}
		childState := &ExecState{
			Ctx:          taskCtx,
			RequestCtx:   m.state.RequestCtx,
			Output:       &buf,
			MaxLoopSteps: m.state.MaxLoopSteps,
			MaxCallDepth: m.state.MaxCallDepth,
			CallDepth:    m.state.CallDepth,
			CallStack:    append([]StackFrame(nil), m.state.CallStack...),
		}

		go func(idx int, task CompiledTask, ch HostBridge, cl func(), cs *ExecState, outBuf *bytes.Buffer) {
			defer wg.Done()
			if cl != nil {
				defer cl()
			}
			defer func() {
				if recover() != nil {
					mu.Lock()
					results[idx] = taskResult{
						err:        m.makeError(ErrStandardLibraryError, "", "", "", task.Span, fmt.Errorf("internal task failure")),
						beforeStop: !cancelledBySibling,
					}
					cancelledBySibling = true
					mu.Unlock()
					cancelOnce.Do(cancel)
				}
			}()

			val, _, err := Execute(task.Chunk, m.prog, cs, ch, taskBase, nil, false)
			mu.Lock()
			wasBeforeStop := !cancelledBySibling
			if err != nil {
				cancelledBySibling = true
			}
			results[idx] = taskResult{
				val:        val,
				out:        outBuf.String(),
				err:        err,
				beforeStop: wasBeforeStop,
			}
			mu.Unlock()
			if err != nil {
				cancelOnce.Do(cancel)
			}
		}(i, t, childHost, cleanup, childState, &buf)
	}

	wg.Wait()

	// 1. Fatal errors (GRAB-E306 / GRAB-E307) in source order.
	for _, res := range results {
		if res.err != nil {
			if _, fatal, ok := m.inspectError(res.err); ok && fatal {
				return res.err
			}
		}
	}
	// 2. Primary errors (not caused solely by sibling context cancellation) in source order.
	for _, res := range results {
		if res.err != nil && !m.isSiblingCanceled(res.err, res.beforeStop) {
			return res.err
		}
	}
	// 3. Fallback.
	for _, res := range results {
		if res.err != nil {
			return res.err
		}
	}

	if m.host != nil {
		for _, res := range results {
			if m.host.CheckContainsCacheHandle(res.val) {
				return m.makeError(ErrCacheValue, "", "", "", plan.Span, nil)
			}
		}
	}

	for i, t := range plan.Tasks {
		if results[i].out != "" {
			if _, err := io.WriteString(m.state.Output, results[i].out); err != nil {
				return err
			}
		}
		if !t.IsCallStmt {
			m.locals[f.localBase+int(t.TargetSlot)] = results[i].val
			if m.trackRootSnapshot && len(m.frames) == 1 && int(t.TargetSlot) < len(m.rootAssigned) {
				m.rootAssigned[t.TargetSlot] = true
			}
		}
	}
	return nil
}

func (m *machine) isSiblingCanceled(err error, beforeStop bool) bool {
	if beforeStop {
		return false
	}
	if m.host != nil {
		return m.host.IsCanceledError(err)
	}
	return errors.Is(err, context.Canceled)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
