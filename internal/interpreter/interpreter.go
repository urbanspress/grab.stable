package interpreter

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"grab/internal/ast"
	"grab/internal/lexer"
	grabrt "grab/internal/runtime"
)

// Default execution safety bounds for GRAB 0.3.
const (
	DefaultMaxLoopSteps = 1_000_000
	DefaultMaxCallDepth = 256
)

// ErrorKind identifies a name-resolution, pre-execution validation, or runtime failure.
type ErrorKind uint8

const (
	UnboundVariable ErrorKind = iota
	JoinedRequiresText
	InvalidLiteral
	DuplicateDefinition
	UnknownFunction
	FunctionArityMismatch
	InvalidGiveBack
	TypeMismatch
	DivisionByZero
	AbsentFieldOrKey
	IndexOutOfBounds
	NegativeRepeatCount
	CallStackOverflow
	ExecutionLimitExceeded
)

// String returns the stable specification category name for the interpreter error.
func (kind ErrorKind) String() string {
	switch kind {
	case UnboundVariable:
		return "unbound variable reference"
	case JoinedRequiresText, TypeMismatch:
		return "runtime type error"
	case InvalidLiteral:
		return "invalid literal"
	case DuplicateDefinition:
		return "duplicate definition"
	case UnknownFunction:
		return "unknown function"
	case FunctionArityMismatch:
		return "function arity mismatch"
	case InvalidGiveBack:
		return "invalid give back"
	case DivisionByZero:
		return "division by zero"
	case AbsentFieldOrKey:
		return "absent field or key"
	case IndexOutOfBounds:
		return "index out of bounds"
	case NegativeRepeatCount:
		return "negative repeat count"
	case CallStackOverflow:
		return "call stack overflow"
	case ExecutionLimitExceeded:
		return "execution limit exceeded"
	default:
		return "unknown interpreter error"
	}
}

// Error carries the source span and stable category for an interpreter error.
type Error struct {
	Kind ErrorKind
	Name string
	Span lexer.Span
	Err  error
}

func (e *Error) Error() string {
	switch e.Kind {
	case UnboundVariable:
		return fmt.Sprintf("unbound variable %q at line %d, column %d", e.Name, e.Span.Start.Line, e.Span.Start.Column)
	case JoinedRequiresText:
		return fmt.Sprintf("joined with requires text values at line %d, column %d", e.Span.Start.Line, e.Span.Start.Column)
	case InvalidLiteral:
		return fmt.Sprintf("invalid literal at line %d, column %d: %v", e.Span.Start.Line, e.Span.Start.Column, e.Err)
	case DuplicateDefinition, UnknownFunction, FunctionArityMismatch, AbsentFieldOrKey:
		return fmt.Sprintf("%s %q at line %d, column %d", e.Kind, e.Name, e.Span.Start.Line, e.Span.Start.Column)
	default:
		return fmt.Sprintf("%s at line %d, column %d", e.Kind, e.Span.Start.Line, e.Span.Start.Column)
	}
}

// Run validates names and functions in the complete program before executing statements.
// Runtime output from successful earlier show statements is retained if a
// later execution error occurs.
func Run(program *ast.Program, output io.Writer) error {
	_, err := RunWithEnvironment(program, output)
	return err
}

// RunWithEnvironment validates the complete program, executes all top-level
// statements in source order, and returns a read-only snapshot of the final
// top-level variable bindings.
func RunWithEnvironment(program *ast.Program, output io.Writer) (map[string]grabrt.Value, error) {
	functions, _, err := validateProgram(program)
	if err != nil {
		return nil, err
	}
	ctx := &execContext{
		output:       output,
		functions:    functions,
		maxLoopSteps: DefaultMaxLoopSteps,
		maxCallDepth: DefaultMaxCallDepth,
	}
	globalScope := newScope(nil)
	if _, _, err := ctx.execBlock(program.Statements, globalScope); err != nil {
		return nil, err
	}
	return globalScope.snapshot(), nil
}

// Validate performs pre-execution validation and name resolution without
// executing statements, returning the set of top-level variable names bound by
// the program.
func Validate(program *ast.Program) (map[string]struct{}, error) {
	_, bound, err := validateProgram(program)
	return bound, err
}

func validateProgram(program *ast.Program) (map[string]*ast.FunctionDef, map[string]struct{}, error) {
	functions := make(map[string]*ast.FunctionDef)
	for _, statement := range program.Statements {
		fn, ok := statement.(*ast.FunctionDef)
		if !ok {
			continue
		}
		if _, exists := functions[fn.Name]; exists {
			return nil, nil, &Error{Kind: DuplicateDefinition, Name: fn.Name, Span: fn.NameSpan}
		}
		seenParams := make(map[string]struct{}, len(fn.Parameters))
		for _, param := range fn.Parameters {
			if _, exists := seenParams[param.Name]; exists {
				return nil, nil, &Error{Kind: DuplicateDefinition, Name: param.Name, Span: param.Span}
			}
			seenParams[param.Name] = struct{}{}
		}
		functions[fn.Name] = fn
	}

	topBound := make(map[string]struct{})
	for _, statement := range program.Statements {
		if fn, ok := statement.(*ast.FunctionDef); ok {
			if _, exists := topBound[fn.Name]; exists {
				return nil, nil, &Error{Kind: DuplicateDefinition, Name: fn.Name, Span: fn.NameSpan}
			}
			fnScope := make(map[string]struct{}, len(fn.Parameters))
			for _, param := range fn.Parameters {
				fnScope[param.Name] = struct{}{}
			}
			if err := resolveStatements(fn.Body, fnScope, functions, true); err != nil {
				return nil, nil, err
			}
			continue
		}
		if setStmt, ok := statement.(*ast.SetStatement); ok {
			if _, exists := functions[setStmt.Name]; exists {
				return nil, nil, &Error{Kind: DuplicateDefinition, Name: setStmt.Name, Span: setStmt.NameSpan}
			}
		}
		if err := resolveStatement(statement, topBound, functions, false); err != nil {
			return nil, nil, err
		}
	}
	return functions, topBound, nil
}

func resolveStatements(statements []ast.Statement, bound map[string]struct{}, functions map[string]*ast.FunctionDef, inFunction bool) error {
	for _, statement := range statements {
		if err := resolveStatement(statement, bound, functions, inFunction); err != nil {
			return err
		}
	}
	return nil
}

func resolveStatement(statement ast.Statement, bound map[string]struct{}, functions map[string]*ast.FunctionDef, inFunction bool) error {
	switch node := statement.(type) {
	case *ast.SetStatement:
		if err := resolveExpression(node.Value, bound, functions); err != nil {
			return err
		}
		bound[node.Name] = struct{}{}
		return nil
	case *ast.ShowStatement:
		return resolveExpression(node.Value, bound, functions)
	case *ast.IfStatement:
		branchScopes := make([]map[string]struct{}, len(node.Branches))
		for i, branch := range node.Branches {
			if err := resolveExpression(branch.Condition, bound, functions); err != nil {
				return err
			}
			branchScopes[i] = cloneBound(bound)
			if err := resolveStatements(branch.Body, branchScopes[i], functions, inFunction); err != nil {
				return err
			}
		}
		if node.HasElse {
			elseScope := cloneBound(bound)
			if err := resolveStatements(node.Otherwise, elseScope, functions, inFunction); err != nil {
				return err
			}
			for name := range elseScope {
				inAll := true
				for _, bs := range branchScopes {
					if _, ok := bs[name]; !ok {
						inAll = false
						break
					}
				}
				if inAll {
					bound[name] = struct{}{}
				}
			}
		}
		return nil
	case *ast.RepeatStatement:
		if err := resolveExpression(node.Count, bound, functions); err != nil {
			return err
		}
		bodyScope := cloneBound(bound)
		return resolveStatements(node.Body, bodyScope, functions, inFunction)
	case *ast.ForEachStatement:
		if err := resolveExpression(node.Collection, bound, functions); err != nil {
			return err
		}
		bodyScope := cloneBound(bound)
		bodyScope[node.Item] = struct{}{}
		return resolveStatements(node.Body, bodyScope, functions, inFunction)
	case *ast.WhileStatement:
		if err := resolveExpression(node.Condition, bound, functions); err != nil {
			return err
		}
		bodyScope := cloneBound(bound)
		return resolveStatements(node.Body, bodyScope, functions, inFunction)
	case *ast.GiveBackStatement:
		if !inFunction {
			return &Error{Kind: InvalidGiveBack, Span: node.Span}
		}
		if node.Value != nil {
			return resolveExpression(node.Value, bound, functions)
		}
		return nil
	case *ast.CallStatement:
		return resolveCall(node.Name, node.NameSpan, node.Span, node.Arguments, bound, functions)
	default:
		return nil
	}
}

func resolveCall(name string, nameSpan, callSpan lexer.Span, args []ast.Expression, bound map[string]struct{}, functions map[string]*ast.FunctionDef) error {
	fn, ok := functions[name]
	if !ok {
		return &Error{Kind: UnknownFunction, Name: name, Span: nameSpan}
	}
	if len(args) != len(fn.Parameters) {
		return &Error{Kind: FunctionArityMismatch, Name: name, Span: callSpan}
	}
	for _, arg := range args {
		if err := resolveExpression(arg, bound, functions); err != nil {
			return err
		}
	}
	return nil
}

func resolveExpression(expression ast.Expression, bound map[string]struct{}, functions map[string]*ast.FunctionDef) error {
	switch node := expression.(type) {
	case *ast.IdentifierExpr:
		if _, ok := bound[node.Name]; !ok {
			return &Error{Kind: UnboundVariable, Name: node.Name, Span: node.Span}
		}
	case *ast.ParenExpr:
		return resolveExpression(node.Inner, bound, functions)
	case *ast.UnaryExpr:
		return resolveExpression(node.Operand, bound, functions)
	case *ast.JoinedExpr:
		if err := resolveExpression(node.Left, bound, functions); err != nil {
			return err
		}
		return resolveExpression(node.Right, bound, functions)
	case *ast.BinaryExpr:
		if err := resolveExpression(node.Left, bound, functions); err != nil {
			return err
		}
		return resolveExpression(node.Right, bound, functions)
	case *ast.FieldAccessExpr:
		return resolveExpression(node.Target, bound, functions)
	case *ast.IndexAccessExpr:
		if err := resolveExpression(node.Target, bound, functions); err != nil {
			return err
		}
		return resolveExpression(node.Index, bound, functions)
	case *ast.HasExpr:
		return resolveExpression(node.Target, bound, functions)
	case *ast.CallExpr:
		return resolveCall(node.Name, node.NameSpan, node.Span, node.Arguments, bound, functions)
	case *ast.ListExpr:
		for _, element := range node.Elements {
			if err := resolveExpression(element, bound, functions); err != nil {
				return err
			}
		}
	case *ast.MapExpr:
		for _, entry := range node.Entries {
			if err := resolveExpression(entry.Value, bound, functions); err != nil {
				return err
			}
		}
	case *ast.RecordExpr:
		for _, field := range node.Fields {
			if err := resolveExpression(field.Value, bound, functions); err != nil {
				return err
			}
		}
	}
	return nil
}

func cloneBound(bound map[string]struct{}) map[string]struct{} {
	copy := make(map[string]struct{}, len(bound))
	for k := range bound {
		copy[k] = struct{}{}
	}
	return copy
}

type scope struct {
	parent *scope
	values map[string]grabrt.Value
}

func newScope(parent *scope) *scope {
	return &scope{
		parent: parent,
		values: make(map[string]grabrt.Value),
	}
}

func (s *scope) get(name string) grabrt.Value {
	for cur := s; cur != nil; cur = cur.parent {
		if val, ok := cur.values[name]; ok {
			return val
		}
	}
	return grabrt.Nothing()
}

func (s *scope) set(name string, val grabrt.Value) {
	for cur := s; cur != nil; cur = cur.parent {
		if _, ok := cur.values[name]; ok {
			cur.values[name] = val
			return
		}
		if cur.parent == nil {
			cur.values[name] = val
			return
		}
	}
	s.values[name] = val
}

func (s *scope) defineLocal(name string, val grabrt.Value) {
	s.values[name] = val
}

func (s *scope) snapshot() map[string]grabrt.Value {
	out := make(map[string]grabrt.Value, len(s.values))
	for k, v := range s.values {
		out[k] = v
	}
	return out
}

type execContext struct {
	output       io.Writer
	functions    map[string]*ast.FunctionDef
	loopSteps    int
	maxLoopSteps int
	callDepth    int
	maxCallDepth int
}

func (ctx *execContext) stepLoop(span lexer.Span) error {
	ctx.loopSteps++
	if ctx.loopSteps > ctx.maxLoopSteps {
		return &Error{Kind: ExecutionLimitExceeded, Span: span}
	}
	return nil
}

func (ctx *execContext) execBlock(statements []ast.Statement, env *scope) (grabrt.Value, bool, error) {
	for _, statement := range statements {
		switch node := statement.(type) {
		case *ast.FunctionDef:
			// Already registered during pre-execution validation.
			continue
		case *ast.SetStatement:
			val, err := ctx.evaluate(node.Value, env)
			if err != nil {
				return grabrt.Value{}, false, err
			}
			env.set(node.Name, val)
		case *ast.ShowStatement:
			val, err := ctx.evaluate(node.Value, env)
			if err != nil {
				return grabrt.Value{}, false, err
			}
			if _, err := io.WriteString(ctx.output, val.Display()+"\n"); err != nil {
				return grabrt.Value{}, false, err
			}
		case *ast.IfStatement:
			matched := false
			for _, branch := range node.Branches {
				cond, err := ctx.evaluate(branch.Condition, env)
				if err != nil {
					return grabrt.Value{}, false, err
				}
				if cond.Kind() != grabrt.BooleanKind {
					return grabrt.Value{}, false, &Error{Kind: TypeMismatch, Span: branch.Condition.SourceSpan()}
				}
				if cond.BooleanValue() {
					matched = true
					retVal, returned, err := ctx.execBlock(branch.Body, env)
					if err != nil || returned {
						return retVal, returned, err
					}
					break
				}
			}
			if !matched && node.HasElse {
				retVal, returned, err := ctx.execBlock(node.Otherwise, env)
				if err != nil || returned {
					return retVal, returned, err
				}
			}
		case *ast.RepeatStatement:
			countVal, err := ctx.evaluate(node.Count, env)
			if err != nil {
				return grabrt.Value{}, false, err
			}
			if countVal.Kind() != grabrt.IntegerKind {
				return grabrt.Value{}, false, &Error{Kind: TypeMismatch, Span: node.Count.SourceSpan()}
			}
			bigCount := countVal.IntegerValue()
			if bigCount.Sign() < 0 {
				return grabrt.Value{}, false, &Error{Kind: NegativeRepeatCount, Span: node.Count.SourceSpan()}
			}
			if !bigCount.IsInt64() || bigCount.Int64() > int64(ctx.maxLoopSteps) {
				return grabrt.Value{}, false, &Error{Kind: ExecutionLimitExceeded, Span: node.Span}
			}
			n := int(bigCount.Int64())
			for i := 0; i < n; i++ {
				if err := ctx.stepLoop(node.Span); err != nil {
					return grabrt.Value{}, false, err
				}
				retVal, returned, err := ctx.execBlock(node.Body, env)
				if err != nil || returned {
					return retVal, returned, err
				}
			}
		case *ast.ForEachStatement:
			collVal, err := ctx.evaluate(node.Collection, env)
			if err != nil {
				return grabrt.Value{}, false, err
			}
			switch collVal.Kind() {
			case grabrt.ListKind:
				items := collVal.ListValues()
				for _, item := range items {
					if err := ctx.stepLoop(node.Span); err != nil {
						return grabrt.Value{}, false, err
					}
					iterScope := newScope(env)
					iterScope.defineLocal(node.Item, item)
					retVal, returned, err := ctx.execBlock(node.Body, iterScope)
					if err != nil || returned {
						return retVal, returned, err
					}
				}
			case grabrt.MapKind:
				entries := collVal.Entries()
				for _, entry := range entries {
					if err := ctx.stepLoop(node.Span); err != nil {
						return grabrt.Value{}, false, err
					}
					iterScope := newScope(env)
					iterScope.defineLocal(node.Item, grabrt.Text(entry.Name))
					retVal, returned, err := ctx.execBlock(node.Body, iterScope)
					if err != nil || returned {
						return retVal, returned, err
					}
				}
			default:
				return grabrt.Value{}, false, &Error{Kind: TypeMismatch, Span: node.Collection.SourceSpan()}
			}
		case *ast.WhileStatement:
			for {
				cond, err := ctx.evaluate(node.Condition, env)
				if err != nil {
					return grabrt.Value{}, false, err
				}
				if cond.Kind() != grabrt.BooleanKind {
					return grabrt.Value{}, false, &Error{Kind: TypeMismatch, Span: node.Condition.SourceSpan()}
				}
				if !cond.BooleanValue() {
					break
				}
				if err := ctx.stepLoop(node.Span); err != nil {
					return grabrt.Value{}, false, err
				}
				retVal, returned, err := ctx.execBlock(node.Body, env)
				if err != nil || returned {
					return retVal, returned, err
				}
			}
		case *ast.GiveBackStatement:
			if node.Value == nil {
				return grabrt.Nothing(), true, nil
			}
			val, err := ctx.evaluate(node.Value, env)
			if err != nil {
				return grabrt.Value{}, false, err
			}
			return val, true, nil
		case *ast.CallStatement:
			if _, err := ctx.invokeFunction(node.Name, node.Span, node.Arguments, env); err != nil {
				return grabrt.Value{}, false, err
			}
		}
	}
	return grabrt.Nothing(), false, nil
}

func (ctx *execContext) invokeFunction(name string, span lexer.Span, argExprs []ast.Expression, callerEnv *scope) (grabrt.Value, error) {
	fn := ctx.functions[name]
	argVals := make([]grabrt.Value, len(argExprs))
	for i, argExpr := range argExprs {
		val, err := ctx.evaluate(argExpr, callerEnv)
		if err != nil {
			return grabrt.Value{}, err
		}
		argVals[i] = val
	}

	ctx.callDepth++
	if ctx.callDepth > ctx.maxCallDepth {
		ctx.callDepth--
		return grabrt.Value{}, &Error{Kind: CallStackOverflow, Span: span}
	}
	defer func() {
		ctx.callDepth--
	}()

	fnScope := newScope(nil)
	for i, param := range fn.Parameters {
		fnScope.defineLocal(param.Name, argVals[i])
	}
	retVal, returned, err := ctx.execBlock(fn.Body, fnScope)
	if err != nil {
		return grabrt.Value{}, err
	}
	if !returned {
		return grabrt.Nothing(), nil
	}
	return retVal, nil
}

func (ctx *execContext) evaluate(expression ast.Expression, env *scope) (grabrt.Value, error) {
	switch node := expression.(type) {
	case *ast.IdentifierExpr:
		return env.get(node.Name), nil
	case *ast.LiteralExpr:
		return literal(node)
	case *ast.ParenExpr:
		return ctx.evaluate(node.Inner, env)
	case *ast.UnaryExpr:
		operand, err := ctx.evaluate(node.Operand, env)
		if err != nil {
			return grabrt.Value{}, err
		}
		if operand.Kind() != grabrt.BooleanKind {
			return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: node.Span}
		}
		return grabrt.Boolean(!operand.BooleanValue()), nil
	case *ast.JoinedExpr:
		left, err := ctx.evaluate(node.Left, env)
		if err != nil {
			return grabrt.Value{}, err
		}
		right, err := ctx.evaluate(node.Right, env)
		if err != nil {
			return grabrt.Value{}, err
		}
		if left.Kind() != grabrt.TextKind || right.Kind() != grabrt.TextKind {
			return grabrt.Value{}, &Error{Kind: JoinedRequiresText, Span: node.Span}
		}
		return grabrt.Text(left.TextValue() + right.TextValue()), nil
	case *ast.BinaryExpr:
		return ctx.evaluateBinary(node, env)
	case *ast.FieldAccessExpr:
		target, err := ctx.evaluate(node.Target, env)
		if err != nil {
			return grabrt.Value{}, err
		}
		if target.Kind() != grabrt.RecordKind {
			return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: node.Span}
		}
		val, ok := target.LookupEntry(node.Field)
		if !ok {
			return grabrt.Value{}, &Error{Kind: AbsentFieldOrKey, Name: node.Field, Span: node.FieldSpan}
		}
		return val, nil
	case *ast.IndexAccessExpr:
		target, err := ctx.evaluate(node.Target, env)
		if err != nil {
			return grabrt.Value{}, err
		}
		indexVal, err := ctx.evaluate(node.Index, env)
		if err != nil {
			return grabrt.Value{}, err
		}
		switch target.Kind() {
		case grabrt.ListKind:
			if indexVal.Kind() != grabrt.IntegerKind {
				return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: node.Span}
			}
			bigIdx := indexVal.IntegerValue()
			if bigIdx.Sign() < 0 || !bigIdx.IsInt64() {
				return grabrt.Value{}, &Error{Kind: IndexOutOfBounds, Span: node.Index.SourceSpan()}
			}
			idx := int(bigIdx.Int64())
			val, ok := target.ListAt(idx)
			if !ok {
				return grabrt.Value{}, &Error{Kind: IndexOutOfBounds, Span: node.Index.SourceSpan()}
			}
			return val, nil
		case grabrt.MapKind:
			if indexVal.Kind() != grabrt.TextKind {
				return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: node.Span}
			}
			key := indexVal.TextValue()
			val, ok := target.LookupEntry(key)
			if !ok {
				return grabrt.Value{}, &Error{Kind: AbsentFieldOrKey, Name: key, Span: node.Index.SourceSpan()}
			}
			return val, nil
		default:
			return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: node.Span}
		}
	case *ast.HasExpr:
		target, err := ctx.evaluate(node.Target, env)
		if err != nil {
			return grabrt.Value{}, err
		}
		if node.IsTextKey {
			if target.Kind() != grabrt.MapKind {
				return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: node.Span}
			}
			_, ok := target.LookupEntry(node.Key)
			return grabrt.Boolean(ok), nil
		}
		if target.Kind() != grabrt.RecordKind {
			return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: node.Span}
		}
		_, ok := target.LookupEntry(node.Key)
		return grabrt.Boolean(ok), nil
	case *ast.CallExpr:
		return ctx.invokeFunction(node.Name, node.Span, node.Arguments, env)
	case *ast.ListExpr:
		values := make([]grabrt.Value, len(node.Elements))
		for i, item := range node.Elements {
			value, err := ctx.evaluate(item, env)
			if err != nil {
				return grabrt.Value{}, err
			}
			values[i] = value
		}
		return grabrt.List(values), nil
	case *ast.MapExpr:
		entries := make([]grabrt.Entry, len(node.Entries))
		for i, item := range node.Entries {
			value, err := ctx.evaluate(item.Value, env)
			if err != nil {
				return grabrt.Value{}, err
			}
			entries[i] = grabrt.Entry{Name: item.Key, Value: value}
		}
		return grabrt.Map(entries), nil
	case *ast.RecordExpr:
		entries := make([]grabrt.Entry, len(node.Fields))
		for i, item := range node.Fields {
			value, err := ctx.evaluate(item.Value, env)
			if err != nil {
				return grabrt.Value{}, err
			}
			entries[i] = grabrt.Entry{Name: item.Name, Value: value}
		}
		return grabrt.Record(entries), nil
	default:
		return grabrt.Value{}, fmt.Errorf("unsupported expression %T", expression)
	}
}

func (ctx *execContext) evaluateBinary(node *ast.BinaryExpr, env *scope) (grabrt.Value, error) {
	if node.Op == ast.OpAnd {
		left, err := ctx.evaluate(node.Left, env)
		if err != nil {
			return grabrt.Value{}, err
		}
		if left.Kind() != grabrt.BooleanKind {
			return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: node.Span}
		}
		if !left.BooleanValue() {
			return grabrt.Boolean(false), nil
		}
		right, err := ctx.evaluate(node.Right, env)
		if err != nil {
			return grabrt.Value{}, err
		}
		if right.Kind() != grabrt.BooleanKind {
			return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: node.Span}
		}
		return right, nil
	}
	if node.Op == ast.OpOr {
		left, err := ctx.evaluate(node.Left, env)
		if err != nil {
			return grabrt.Value{}, err
		}
		if left.Kind() != grabrt.BooleanKind {
			return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: node.Span}
		}
		if left.BooleanValue() {
			return grabrt.Boolean(true), nil
		}
		right, err := ctx.evaluate(node.Right, env)
		if err != nil {
			return grabrt.Value{}, err
		}
		if right.Kind() != grabrt.BooleanKind {
			return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: node.Span}
		}
		return right, nil
	}

	left, err := ctx.evaluate(node.Left, env)
	if err != nil {
		return grabrt.Value{}, err
	}
	right, err := ctx.evaluate(node.Right, env)
	if err != nil {
		return grabrt.Value{}, err
	}

	switch node.Op {
	case ast.OpPlus:
		val, err := grabrt.Add(left, right)
		return wrapNumericResult(val, err, node.Span)
	case ast.OpMinus:
		val, err := grabrt.Sub(left, right)
		return wrapNumericResult(val, err, node.Span)
	case ast.OpTimes:
		val, err := grabrt.Mul(left, right)
		return wrapNumericResult(val, err, node.Span)
	case ast.OpDividedBy:
		val, err := grabrt.Div(left, right)
		return wrapNumericResult(val, err, node.Span)
	case ast.OpModulo:
		val, err := grabrt.Mod(left, right)
		return wrapNumericResult(val, err, node.Span)
	case ast.OpIs:
		return grabrt.Boolean(left.Equal(right)), nil
	case ast.OpIsNot:
		return grabrt.Boolean(!left.Equal(right)), nil
	case ast.OpGreaterThan:
		cmp, ok := left.Compare(right)
		if !ok {
			return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: node.Span}
		}
		return grabrt.Boolean(cmp > 0), nil
	case ast.OpLessThan:
		cmp, ok := left.Compare(right)
		if !ok {
			return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: node.Span}
		}
		return grabrt.Boolean(cmp < 0), nil
	case ast.OpAtLeast:
		cmp, ok := left.Compare(right)
		if !ok {
			return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: node.Span}
		}
		return grabrt.Boolean(cmp >= 0), nil
	case ast.OpAtMost:
		cmp, ok := left.Compare(right)
		if !ok {
			return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: node.Span}
		}
		return grabrt.Boolean(cmp <= 0), nil
	default:
		return grabrt.Value{}, fmt.Errorf("unsupported binary operator %d", node.Op)
	}
}

func wrapNumericResult(val grabrt.Value, err error, span lexer.Span) (grabrt.Value, error) {
	if err == nil {
		return val, nil
	}
	if errors.Is(err, grabrt.ErrDivisionByZero) {
		return grabrt.Value{}, &Error{Kind: DivisionByZero, Span: span}
	}
	return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: span, Err: err}
}

func literal(node *ast.LiteralExpr) (grabrt.Value, error) {
	var value grabrt.Value
	var err error
	switch node.Kind {
	case ast.TextLiteral:
		var decoded string
		decoded, err = strconv.Unquote(node.Lexeme)
		value = grabrt.Text(decoded)
	case ast.BooleanLiteral:
		value = grabrt.Boolean(node.Lexeme == "yes")
	case ast.NothingLiteral:
		value = grabrt.Nothing()
	case ast.IntegerLiteral:
		value, err = grabrt.IntegerFromString(node.Lexeme)
	case ast.DecimalLiteral:
		value, err = grabrt.DecimalFromString(node.Lexeme)
	case ast.FloatLiteral:
		value, err = grabrt.FloatFromString(node.Lexeme)
	case ast.BytesLiteral:
		var decoded []byte
		decoded, err = decodeBytes(node.Lexeme)
		value = grabrt.Bytes(decoded)
	default:
		err = fmt.Errorf("unknown literal kind %d", node.Kind)
	}
	if err != nil {
		return grabrt.Value{}, &Error{Kind: InvalidLiteral, Span: node.Span, Err: err}
	}
	return value, nil
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
