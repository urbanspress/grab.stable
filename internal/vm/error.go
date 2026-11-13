package vm

import (
	"context"
	"fmt"
	"io"

	"grab/internal/lexer"
	grabrt "grab/internal/runtime"
)

// ErrKind mirrors interpreter error categories without introducing an import cycle.
type ErrKind uint8

const (
	ErrTypeMismatch ErrKind = iota
	ErrJoinedRequiresText
	ErrInvalidLiteral
	ErrDivisionByZero
	ErrAbsentFieldOrKey
	ErrIndexOutOfBounds
	ErrNegativeRepeatCount
	ErrCallStackOverflow
	ErrExecutionLimitExceeded
	ErrUserRaisedError
	ErrStandardLibraryError
	ErrRequestTimeout
	ErrDatabaseOwnership
	ErrCacheValue
)

// StackFrame records an active function invocation on the call stack.
type StackFrame struct {
	Function string
	Span     lexer.Span
}

// HostBridge connects the bytecode VM to interpreter-owned host modules,
// capability checks, transaction scoping, cold AST fallback, and error types.
type HostBridge interface {
	CallModule(ctx context.Context, module, name string, span lexer.Span, args []grabrt.Value) (grabrt.Value, error)
	PreCheckModule(module, name string, span lexer.Span) error
	EnterTxScope() any
	ExitTxScope(prev any)
	ActiveTransaction() any
	CallColdFunction(fnIdx int, name string, span lexer.Span, args []grabrt.Value, state *ExecState) (grabrt.Value, error)
	ForkTask(taskCtx context.Context, output io.Writer, stack []StackFrame) (HostBridge, func())
	NewError(kind ErrKind, name, customCode, message string, span lexer.Span, stack []StackFrame, cause error) error
	InspectError(err error) (record grabrt.Value, fatal bool, ok bool)
	AttachStack(err error, stack []StackFrame) error
	IsCanceledError(err error) bool
	CheckContainsCacheHandle(val grabrt.Value) bool
}

// RuntimeError is the fallback VM error used when running without an interpreter HostBridge.
type RuntimeError struct {
	Kind       ErrKind
	Name       string
	CustomCode string
	Message    string
	Span       lexer.Span
	Stack      []StackFrame
	Err        error
}

func (e *RuntimeError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s at line %d, column %d", e.Message, e.Span.Start.Line, e.Span.Start.Column)
	}
	return fmt.Sprintf("vm error (%d) at line %d, column %d", e.Kind, e.Span.Start.Line, e.Span.Start.Column)
}

func (e *RuntimeError) IsFatal() bool {
	return e.Kind == ErrCallStackOverflow || e.Kind == ErrExecutionLimitExceeded
}

func (e *RuntimeError) Code() string {
	if e.CustomCode != "" {
		return e.CustomCode
	}
	switch e.Kind {
	case ErrDivisionByZero:
		return "GRAB-E302"
	case ErrAbsentFieldOrKey:
		return "GRAB-E303"
	case ErrIndexOutOfBounds:
		return "GRAB-E304"
	case ErrNegativeRepeatCount:
		return "GRAB-E305"
	case ErrCallStackOverflow:
		return "GRAB-E306"
	case ErrExecutionLimitExceeded:
		return "GRAB-E307"
	case ErrUserRaisedError:
		return "GRAB-E308"
	case ErrStandardLibraryError:
		return "GRAB-E309"
	case ErrRequestTimeout:
		return "GRAB-E311"
	default:
		return "GRAB-E301"
	}
}

func (e *RuntimeError) ErrorRecord() grabrt.Value {
	msg := e.Message
	if msg == "" {
		msg = e.Error()
	}
	return grabrt.Record([]grabrt.Entry{
		{Name: "code", Value: grabrt.Text(e.Code())},
		{Name: "category", Value: grabrt.Text("runtime error")},
		{Name: "message", Value: grabrt.Text(msg)},
		{Name: "line", Value: grabrt.IntegerFromInt64(int64(e.Span.Start.Line))},
		{Name: "column", Value: grabrt.IntegerFromInt64(int64(e.Span.Start.Column))},
	})
}
