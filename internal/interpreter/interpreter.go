package interpreter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"

	"grab/internal/ast"
	"grab/internal/cache"
	"grab/internal/data"
	"grab/internal/lexer"
	"grab/internal/platform"
	grabrt "grab/internal/runtime"
)

// Default execution safety bounds for GRAB 0.3–0.5.
const (
	DefaultMaxLoopSteps = 1_000_000
	DefaultMaxCallDepth = 256
	MaxDoTogetherTasks  = 16
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
	InvalidTaskGroup
	TypeMismatch
	DivisionByZero
	AbsentFieldOrKey
	IndexOutOfBounds
	NegativeRepeatCount
	CallStackOverflow
	ExecutionLimitExceeded
	UserRaisedError
	StandardLibraryError
	NetworkError
	RequestTimeout
	PermissionDenied
	NetworkPermissionDenied
	InvalidRouteDefinition
	InvalidServerResponse
	InvalidDatabaseDefinition
	DatabaseOperation
	DatabaseType
	DatabaseOwnership
	DatabaseMigration
	DatabaseLimit
	DatabaseUnknownCommit
	DatabaseDenied
)

// String returns the stable specification category name for the interpreter error.
func (kind ErrorKind) String() string {
	if kind >= InvalidDatabaseDefinition && kind <= DatabaseDenied {
		return "database error"
	}
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
	case InvalidTaskGroup:
		return "invalid task group"
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
	case UserRaisedError:
		return "user raised error"
	case StandardLibraryError:
		return "standard library error"
	case NetworkError:
		return "network error"
	case RequestTimeout:
		return "request timeout"
	case PermissionDenied:
		return "permission denied"
	case NetworkPermissionDenied:
		return "network permission denied"
	case InvalidRouteDefinition:
		return "invalid route definition"
	case InvalidServerResponse:
		return "invalid server response"
	default:
		return "unknown interpreter error"
	}
}

// Code returns the stable GRAB 0.4–0.5 error code for the interpreter error kind.
func (kind ErrorKind) Code() string {
	if kind >= InvalidDatabaseDefinition && kind <= DatabaseDenied {
		return []string{data.Invalid, data.Operation, data.Type, data.Ownership, data.Migration, data.Limit, data.UnknownCommit, data.Denied}[kind-InvalidDatabaseDefinition]
	}
	switch kind {
	case UnboundVariable:
		return "GRAB-E201"
	case DuplicateDefinition:
		return "GRAB-E202"
	case UnknownFunction:
		return "GRAB-E203"
	case FunctionArityMismatch:
		return "GRAB-E204"
	case InvalidGiveBack:
		return "GRAB-E205"
	case InvalidTaskGroup:
		return "GRAB-E208"
	case JoinedRequiresText, InvalidLiteral, TypeMismatch:
		return "GRAB-E301"
	case DivisionByZero:
		return "GRAB-E302"
	case AbsentFieldOrKey:
		return "GRAB-E303"
	case IndexOutOfBounds:
		return "GRAB-E304"
	case NegativeRepeatCount:
		return "GRAB-E305"
	case CallStackOverflow:
		return "GRAB-E306"
	case ExecutionLimitExceeded:
		return "GRAB-E307"
	case UserRaisedError:
		return "GRAB-E308"
	case StandardLibraryError:
		return "GRAB-E309"
	case NetworkError:
		return "GRAB-E310"
	case RequestTimeout:
		return "GRAB-E311"
	case PermissionDenied:
		return "GRAB-E401"
	case NetworkPermissionDenied:
		return "GRAB-E402"
	case InvalidRouteDefinition:
		return "GRAB-E209"
	case InvalidServerResponse:
		return "GRAB-E312"
	default:
		return "GRAB-E301"
	}
}

// StackFrame records an active function invocation on the call stack.
type StackFrame struct {
	Function string
	Span     lexer.Span
}

// Error carries the source span and stable category for an interpreter error.
type Error struct {
	Kind       ErrorKind
	Name       string
	CustomCode string
	Message    string
	Span       lexer.Span
	Stack      []StackFrame
	Err        error
}

// Code returns the stable GRAB 0.4 error code for this error.
func (e *Error) Code() string {
	if e.CustomCode != "" {
		return e.CustomCode
	}
	return e.Kind.Code()
}

// IsFatal reports whether the error is a non-catchable resource safety limit.
func (e *Error) IsFatal() bool {
	return e.Kind == CallStackOverflow || e.Kind == ExecutionLimitExceeded
}

// ErrorRecord converts a recoverable runtime error into its GRAB record value
// for binding in a 'catch' block.
func (e *Error) ErrorRecord() grabrt.Value {
	msg := e.Message
	if msg == "" {
		msg = e.Error()
	}
	entries := []grabrt.Entry{
		{Name: "code", Value: grabrt.Text(e.Code())},
		{Name: "category", Value: grabrt.Text(e.Kind.String())},
		{Name: "message", Value: grabrt.Text(msg)},
		{Name: "line", Value: grabrt.IntegerFromInt64(int64(e.Span.Start.Line))},
		{Name: "column", Value: grabrt.IntegerFromInt64(int64(e.Span.Start.Column))},
	}
	if errors.Is(e.Err, cache.ErrUncertain) {
		entries = append(entries, grabrt.Entry{Name: "cause", Value: grabrt.Text("uncertain")})
	}
	return grabrt.Record(entries)
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
	case UserRaisedError:
		if e.Message != "" {
			return fmt.Sprintf("%s: %s at line %d, column %d", e.Kind, e.Message, e.Span.Start.Line, e.Span.Start.Column)
		}
		return fmt.Sprintf("%s at line %d, column %d", e.Kind, e.Span.Start.Line, e.Span.Start.Column)
	case StandardLibraryError, NetworkError, RequestTimeout, NetworkPermissionDenied, InvalidTaskGroup, InvalidRouteDefinition, InvalidServerResponse:
		fallthrough
	case InvalidDatabaseDefinition, DatabaseOperation, DatabaseType, DatabaseOwnership, DatabaseMigration, DatabaseLimit, DatabaseUnknownCommit, DatabaseDenied:
		if e.Err != nil {
			return fmt.Sprintf("%s (%v) at line %d, column %d", e.Kind, e.Err, e.Span.Start.Line, e.Span.Start.Column)
		}
		if e.Name != "" {
			return fmt.Sprintf("%s %q at line %d, column %d", e.Kind, e.Name, e.Span.Start.Line, e.Span.Start.Column)
		}
		return fmt.Sprintf("%s at line %d, column %d", e.Kind, e.Span.Start.Line, e.Span.Start.Column)
	default:
		return fmt.Sprintf("%s at line %d, column %d", e.Kind, e.Span.Start.Line, e.Span.Start.Column)
	}
}

// Options configures runtime capabilities and safety bounds for execution.
type Options struct {
	PlatformPath   string
	PlatformLoader func() (*platform.Config, *platform.Grants, error)
	PlatformConfig *platform.Config
	PlatformGrants *platform.Grants
	CacheManager   *cache.Manager
	Context        context.Context
	DataGrants     *data.Grants
	DataManager    *data.Manager
	Permissions    Permissions
	DialContext    func(ctx context.Context, network, addr string) (net.Conn, error)
	// TemplateLoader renders server.html through the host's confined template
	// cache. A nil callback makes server.html unavailable outside the server.
	TemplateLoader func(path string, data map[string]grabrt.Value) (string, error)
}

// Run validates names and functions in the complete program before executing statements.
// Runtime output from successful earlier show statements is retained if a
// later execution error occurs.
func Run(program *ast.Program, output io.Writer) error {
	_, err := RunWithEnvironmentAndOptions(program, output, Options{})
	return err
}

// RunWithOptions validates and executes program with the supplied runtime options.
func RunWithOptions(program *ast.Program, output io.Writer, opts Options) error {
	_, err := RunWithEnvironmentAndOptions(program, output, opts)
	return err
}

// RunWithEnvironment validates the complete program, executes all top-level
// statements in source order, and returns a read-only snapshot of the final
// top-level variable bindings.
func RunWithEnvironment(program *ast.Program, output io.Writer) (map[string]grabrt.Value, error) {
	return RunWithEnvironmentAndOptions(program, output, Options{})
}

// RunWithEnvironmentAndOptions validates the complete program, executes all
// top-level statements with the supplied runtime options, and returns a
// read-only snapshot of the final top-level variable bindings.
func RunWithEnvironmentAndOptions(program *ast.Program, output io.Writer, opts Options) (map[string]grabrt.Value, error) {
	functions, _, _, err := validateProgram(program)
	if err != nil {
		return nil, err
	}
	if err := ValidatePlatform(program, opts.PlatformConfig); err != nil {
		return nil, err
	}
	if opts.CacheManager == nil {
		opts.CacheManager = cache.NewManager(opts.PlatformGrants, nil)
		defer opts.CacheManager.Close()
	}
	if opts.DataManager == nil {
		opts.DataManager = data.NewManager(opts.DataGrants, false)
		defer opts.DataManager.Close()
	}
	parent := opts.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx := newExecContext(parent, output, functions, opts)
	defer ctx.data.Close()
	globalScope := newScope(nil)
	if _, _, err := ctx.execBlock(program.Statements, globalScope); err != nil {
		return nil, err
	}
	return globalScope.snapshot(), nil
}

func newExecContext(parent context.Context, output io.Writer, functions map[string]*ast.FunctionDef, opts Options) *execContext {
	if opts.DataManager == nil {
		opts.DataManager = data.NewManager(opts.DataGrants, false)
	}
	return &execContext{
		cacheManager:   opts.CacheManager,
		data:           opts.DataManager.Scope(),
		dataManager:    opts.DataManager,
		ctx:            parent,
		requestCtx:     parent,
		output:         output,
		functions:      functions,
		permissions:    opts.Permissions,
		dialContext:    opts.DialContext,
		templateLoader: opts.TemplateLoader,
		maxLoopSteps:   DefaultMaxLoopSteps,
		maxCallDepth:   DefaultMaxCallDepth,
	}
}

// Validate performs pre-execution validation and name resolution without
// executing statements, returning the set of top-level variable names bound by
// the program.
func Validate(program *ast.Program) (map[string]struct{}, error) {
	_, bound, _, err := validateProgram(program)
	return bound, err
}

type validationEnv struct {
	functions  map[string]*ast.FunctionDef
	modules    map[string]struct{}
	inFunction bool
	inRoute    bool
}

func validateProgram(program *ast.Program) (map[string]*ast.FunctionDef, map[string]struct{}, []*Route, error) {
	functions := make(map[string]*ast.FunctionDef)
	modules := make(map[string]struct{})

	for _, statement := range program.Statements {
		switch node := statement.(type) {
		case *ast.UseDecl:
			if !isValidStdlibModule(node.Module) {
				return nil, nil, nil, &Error{Kind: UnknownFunction, Name: node.Module, Span: node.ModuleSpan}
			}
			if _, exists := modules[node.Module]; exists {
				return nil, nil, nil, &Error{Kind: DuplicateDefinition, Name: node.Module, Span: node.ModuleSpan}
			}
			if _, exists := functions[node.Module]; exists {
				return nil, nil, nil, &Error{Kind: DuplicateDefinition, Name: node.Module, Span: node.ModuleSpan}
			}
			modules[node.Module] = struct{}{}
		case *ast.FunctionDef:
			if _, exists := functions[node.Name]; exists {
				return nil, nil, nil, &Error{Kind: DuplicateDefinition, Name: node.Name, Span: node.NameSpan}
			}
			if _, exists := modules[node.Name]; exists {
				return nil, nil, nil, &Error{Kind: DuplicateDefinition, Name: node.Name, Span: node.NameSpan}
			}
			seenParams := make(map[string]struct{}, len(node.Parameters))
			for _, param := range node.Parameters {
				if _, exists := seenParams[param.Name]; exists {
					return nil, nil, nil, &Error{Kind: DuplicateDefinition, Name: param.Name, Span: param.Span}
				}
				seenParams[param.Name] = struct{}{}
			}
			functions[node.Name] = node
		}
	}

	topBound := make(map[string]struct{})
	topEnv := validationEnv{functions: functions, modules: modules, inFunction: false}
	fnEnv := validationEnv{functions: functions, modules: modules, inFunction: true}
	var routeDefs []*ast.RouteDef

	for _, statement := range program.Statements {
		if _, ok := statement.(*ast.UseDecl); ok {
			continue
		}
		if route, ok := statement.(*ast.RouteDef); ok {
			routeDefs = append(routeDefs, route)
			continue
		}
		if fn, ok := statement.(*ast.FunctionDef); ok {
			if _, exists := topBound[fn.Name]; exists {
				return nil, nil, nil, &Error{Kind: DuplicateDefinition, Name: fn.Name, Span: fn.NameSpan}
			}
			fnScope := make(map[string]struct{}, len(fn.Parameters))
			for _, param := range fn.Parameters {
				fnScope[param.Name] = struct{}{}
			}
			if err := resolveStatements(fn.Body, fnScope, fnEnv); err != nil {
				return nil, nil, nil, err
			}
			continue
		}
		if err := resolveStatement(statement, topBound, topEnv); err != nil {
			return nil, nil, nil, err
		}
	}

	routes, err := compileRoutes(routeDefs)
	if err != nil {
		return nil, nil, nil, err
	}
	routeEnv := validationEnv{functions: functions, modules: modules, inRoute: true}
	for _, route := range routeDefs {
		routeScope := cloneBound(topBound)
		if route.RequestName != "" {
			if _, exists := functions[route.RequestName]; exists {
				return nil, nil, nil, &Error{Kind: DuplicateDefinition, Name: route.RequestName, Span: route.RequestSpan}
			}
			if _, exists := modules[route.RequestName]; exists {
				return nil, nil, nil, &Error{Kind: DuplicateDefinition, Name: route.RequestName, Span: route.RequestSpan}
			}
			routeScope[route.RequestName] = struct{}{}
		}
		if err := resolveStatements(route.Body, routeScope, routeEnv); err != nil {
			return nil, nil, nil, err
		}
	}
	return functions, topBound, routes, nil
}

func resolveStatements(statements []ast.Statement, bound map[string]struct{}, venv validationEnv) error {
	for _, statement := range statements {
		if err := resolveStatement(statement, bound, venv); err != nil {
			return err
		}
	}
	return nil
}

func resolveStatement(statement ast.Statement, bound map[string]struct{}, venv validationEnv) error {
	switch node := statement.(type) {
	case *ast.SetStatement:
		if !venv.inFunction {
			if _, exists := venv.functions[node.Name]; exists {
				return &Error{Kind: DuplicateDefinition, Name: node.Name, Span: node.NameSpan}
			}
			if _, exists := venv.modules[node.Name]; exists {
				return &Error{Kind: DuplicateDefinition, Name: node.Name, Span: node.NameSpan}
			}
		}
		if err := resolveExpression(node.Value, bound, venv); err != nil {
			return err
		}
		bound[node.Name] = struct{}{}
		return nil
	case *ast.ShowStatement:
		return resolveExpression(node.Value, bound, venv)
	case *ast.IfStatement:
		branchScopes := make([]map[string]struct{}, len(node.Branches))
		for i, branch := range node.Branches {
			if err := resolveExpression(branch.Condition, bound, venv); err != nil {
				return err
			}
			branchScopes[i] = cloneBound(bound)
			if err := resolveStatements(branch.Body, branchScopes[i], venv); err != nil {
				return err
			}
		}
		if node.HasElse {
			elseScope := cloneBound(bound)
			if err := resolveStatements(node.Otherwise, elseScope, venv); err != nil {
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
		if err := resolveExpression(node.Count, bound, venv); err != nil {
			return err
		}
		bodyScope := cloneBound(bound)
		return resolveStatements(node.Body, bodyScope, venv)
	case *ast.ForEachStatement:
		if err := resolveExpression(node.Collection, bound, venv); err != nil {
			return err
		}
		bodyScope := cloneBound(bound)
		bodyScope[node.Item] = struct{}{}
		return resolveStatements(node.Body, bodyScope, venv)
	case *ast.WhileStatement:
		if err := resolveExpression(node.Condition, bound, venv); err != nil {
			return err
		}
		bodyScope := cloneBound(bound)
		return resolveStatements(node.Body, bodyScope, venv)
	case *ast.TryStatement:
		tryScope := cloneBound(bound)
		if err := resolveStatements(node.TryBody, tryScope, venv); err != nil {
			return err
		}
		catchScope := cloneBound(bound)
		catchScope[node.CatchName] = struct{}{}
		if err := resolveStatements(node.CatchBody, catchScope, venv); err != nil {
			return err
		}
		for name := range tryScope {
			if name == node.CatchName {
				continue
			}
			if _, ok := catchScope[name]; ok {
				bound[name] = struct{}{}
			}
		}
		return nil
	case *ast.RaiseStatement:
		return resolveExpression(node.Value, bound, venv)
	case *ast.DoTogetherStatement:
		if len(node.Tasks) == 0 || len(node.Tasks) > MaxDoTogetherTasks {
			return &Error{Kind: InvalidTaskGroup, Span: node.Span}
		}
		seenTargets := make(map[string]struct{}, len(node.Tasks))
		for _, task := range node.Tasks {
			taskSnapshot := cloneBound(bound)
			if task.Call != nil {
				if err := resolveCall(task.Call.Module, task.Call.Name, task.Call.NameSpan, task.Call.Span, task.Call.Arguments, taskSnapshot, venv); err != nil {
					return err
				}
				continue
			}
			if !venv.inFunction {
				if _, exists := venv.functions[task.Name]; exists {
					return &Error{Kind: DuplicateDefinition, Name: task.Name, Span: task.NameSpan}
				}
				if _, exists := venv.modules[task.Name]; exists {
					return &Error{Kind: DuplicateDefinition, Name: task.Name, Span: task.NameSpan}
				}
			}
			if _, exists := seenTargets[task.Name]; exists {
				return &Error{Kind: InvalidTaskGroup, Name: task.Name, Span: task.NameSpan}
			}
			seenTargets[task.Name] = struct{}{}
			if err := resolveExpression(task.Value, taskSnapshot, venv); err != nil {
				return err
			}
		}
		for name := range seenTargets {
			bound[name] = struct{}{}
		}
		return nil
	case *ast.GiveBackStatement:
		if !venv.inFunction && !venv.inRoute {
			return &Error{Kind: InvalidGiveBack, Span: node.Span}
		}
		if node.Value != nil {
			return resolveExpression(node.Value, bound, venv)
		}
		return nil
	case *ast.CallStatement:
		return resolveCall(node.Module, node.Name, node.NameSpan, node.Span, node.Arguments, bound, venv)
	default:
		return nil
	}
}

func resolveCall(module, name string, nameSpan, callSpan lexer.Span, args []ast.Expression, bound map[string]struct{}, venv validationEnv) error {
	if module != "" {
		qname := module + "." + name
		if _, imported := venv.modules[module]; !imported {
			return &Error{Kind: UnknownFunction, Name: qname, Span: nameSpan}
		}
		stdFn, ok := lookupStdlibFunc(module, name)
		if !ok {
			return &Error{Kind: UnknownFunction, Name: qname, Span: nameSpan}
		}
		if !stdFn.acceptsArity(len(args)) {
			return &Error{Kind: FunctionArityMismatch, Name: qname, Span: callSpan}
		}
		if module == "cache" {
			if e := validateCacheCall(name, args, callSpan); e != nil {
				return e
			}
		}
		if module == "data" {
			if e := validateDataCall(name, args, callSpan); e != nil {
				return e
			}
		}
	} else {
		fn, ok := venv.functions[name]
		if !ok {
			return &Error{Kind: UnknownFunction, Name: name, Span: nameSpan}
		}
		if len(args) != len(fn.Parameters) {
			return &Error{Kind: FunctionArityMismatch, Name: name, Span: callSpan}
		}
	}
	for _, arg := range args {
		if err := resolveExpression(arg, bound, venv); err != nil {
			return err
		}
	}
	return nil
}

func resolveExpression(expression ast.Expression, bound map[string]struct{}, venv validationEnv) error {
	switch node := expression.(type) {
	case *ast.IdentifierExpr:
		if _, ok := bound[node.Name]; !ok {
			return &Error{Kind: UnboundVariable, Name: node.Name, Span: node.Span}
		}
	case *ast.ParenExpr:
		return resolveExpression(node.Inner, bound, venv)
	case *ast.UnaryExpr:
		return resolveExpression(node.Operand, bound, venv)
	case *ast.JoinedExpr:
		if err := resolveExpression(node.Left, bound, venv); err != nil {
			return err
		}
		return resolveExpression(node.Right, bound, venv)
	case *ast.BinaryExpr:
		if err := resolveExpression(node.Left, bound, venv); err != nil {
			return err
		}
		return resolveExpression(node.Right, bound, venv)
	case *ast.FieldAccessExpr:
		return resolveExpression(node.Target, bound, venv)
	case *ast.IndexAccessExpr:
		if err := resolveExpression(node.Target, bound, venv); err != nil {
			return err
		}
		return resolveExpression(node.Index, bound, venv)
	case *ast.HasExpr:
		return resolveExpression(node.Target, bound, venv)
	case *ast.CallExpr:
		return resolveCall(node.Module, node.Name, node.NameSpan, node.Span, node.Arguments, bound, venv)
	case *ast.ListExpr:
		for _, element := range node.Elements {
			if err := resolveExpression(element, bound, venv); err != nil {
				return err
			}
		}
	case *ast.MapExpr:
		for _, entry := range node.Entries {
			if err := resolveExpression(entry.Value, bound, venv); err != nil {
				return err
			}
		}
	case *ast.RecordExpr:
		for _, field := range node.Fields {
			if err := resolveExpression(field.Value, bound, venv); err != nil {
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

func (s *scope) cloneSnapshot() *scope {
	flat := make(map[string]grabrt.Value)
	var chain []*scope
	for cur := s; cur != nil; cur = cur.parent {
		chain = append(chain, cur)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		for k, v := range chain[i].values {
			flat[k] = v
		}
	}
	return &scope{values: flat}
}

type execContext struct {
	cacheManager   *cache.Manager
	data           *data.Scope
	dataManager    *data.Manager
	ctx            context.Context
	requestCtx     context.Context
	output         io.Writer
	functions      map[string]*ast.FunctionDef
	permissions    Permissions
	dialContext    func(ctx context.Context, network, addr string) (net.Conn, error)
	templateLoader func(path string, data map[string]grabrt.Value) (string, error)
	loopSteps      int
	maxLoopSteps   int
	callDepth      int
	maxCallDepth   int
	callStack      []StackFrame
}

func (ctx *execContext) context() context.Context {
	if ctx.ctx != nil {
		return ctx.ctx
	}
	return context.Background()
}

func (ctx *execContext) attachStack(err error) error {
	if err == nil {
		return nil
	}
	if interpErr, ok := err.(*Error); ok && len(ctx.callStack) > 0 && len(interpErr.Stack) == 0 {
		frames := make([]StackFrame, len(ctx.callStack))
		for i := range ctx.callStack {
			frames[i] = ctx.callStack[len(ctx.callStack)-1-i]
		}
		interpErr.Stack = frames
	}
	return err
}

func (ctx *execContext) stepLoop(span lexer.Span) error {
	ctx.loopSteps++
	if ctx.loopSteps > ctx.maxLoopSteps {
		return ctx.attachStack(&Error{Kind: ExecutionLimitExceeded, Span: span})
	}
	// GRAB 0.6: honor request deadlines and cancellation inside loops so a
	// timed-out server request stops consuming CPU promptly.
	if ctx.ctx != nil {
		if err := ctx.ctx.Err(); err != nil {
			return ctx.attachStack(&Error{Kind: RequestTimeout, Span: span, Err: err})
		}
	}
	return nil
}

func (ctx *execContext) execBlock(statements []ast.Statement, env *scope) (grabrt.Value, bool, error) {
	if ctx.data != nil {
		previous := ctx.data.ActiveTransaction()
		defer ctx.data.RollbackNew(previous)
	}
	for _, statement := range statements {
		if ctx.requestCtx != nil && ctx.requestCtx.Err() != nil {
			err := ctx.requestCtx.Err()
			return grabrt.Value{}, false, ctx.attachStack(&Error{Kind: RequestTimeout, Span: statement.SourceSpan(), Err: err})
		}
		switch node := statement.(type) {
		case *ast.UseDecl, *ast.FunctionDef, *ast.RouteDef:
			// Already registered during pre-execution validation; route
			// handlers execute only when GRAB Server dispatches a request.
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
			if val.ContainsResource() {
				return grabrt.Value{}, false, &Error{Kind: TypeMismatch, Span: node.Span}
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
		case *ast.TryStatement:
			retVal, returned, err := ctx.execBlock(node.TryBody, env)
			if err == nil {
				if returned {
					return retVal, true, nil
				}
				continue
			}
			var interpErr *Error
			if !errors.As(err, &interpErr) || interpErr.IsFatal() {
				return grabrt.Value{}, false, err
			}
			catchScope := newScope(env)
			catchScope.defineLocal(node.CatchName, interpErr.ErrorRecord())
			retVal, returned, err = ctx.execBlock(node.CatchBody, catchScope)
			if err != nil || returned {
				return retVal, returned, err
			}
		case *ast.RaiseStatement:
			val, err := ctx.evaluate(node.Value, env)
			if err != nil {
				return grabrt.Value{}, false, err
			}
			switch val.Kind() {
			case grabrt.TextKind:
				return grabrt.Value{}, false, ctx.attachStack(&Error{
					Kind:    UserRaisedError,
					Message: val.TextValue(),
					Span:    node.Span,
				})
			case grabrt.RecordKind:
				msgVal, ok := val.LookupEntry("message")
				if !ok || msgVal.Kind() != grabrt.TextKind {
					return grabrt.Value{}, false, ctx.attachStack(&Error{Kind: TypeMismatch, Span: node.Span})
				}
				customCode := ""
				if codeVal, hasCode := val.LookupEntry("code"); hasCode {
					if codeVal.Kind() != grabrt.TextKind || codeVal.TextValue() == "" {
						return grabrt.Value{}, false, ctx.attachStack(&Error{Kind: TypeMismatch, Span: node.Span})
					}
					customCode = codeVal.TextValue()
				}
				return grabrt.Value{}, false, ctx.attachStack(&Error{
					Kind:       UserRaisedError,
					CustomCode: customCode,
					Message:    msgVal.TextValue(),
					Span:       node.Span,
				})
			default:
				return grabrt.Value{}, false, ctx.attachStack(&Error{Kind: TypeMismatch, Span: node.Span})
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
		case *ast.DoTogetherStatement:
			if err := ctx.execDoTogether(node, env); err != nil {
				return grabrt.Value{}, false, err
			}
		case *ast.CallStatement:
			if _, err := ctx.invokeCall(node.Module, node.Name, node.Span, node.Arguments, env); err != nil {
				return grabrt.Value{}, false, err
			}
		}
	}
	return grabrt.Nothing(), false, nil
}

func (ctx *execContext) execDoTogether(node *ast.DoTogetherStatement, env *scope) error {
	for _, v := range env.cloneSnapshot().values {
		if containsCacheHandle(v) {
			return cacheError(node.Span, cache.ErrValue)
		}
	}
	if ctx.data != nil && ctx.data.ActiveTransaction() != nil {
		return databaseError(node.Span, &data.Error{Code: data.Ownership, Message: "transactions cannot be shared with task groups"})
	}
	taskCtx, cancel := context.WithCancel(ctx.context())
	defer cancel()

	type taskResult struct {
		val        grabrt.Value
		out        string
		err        error
		beforeStop bool
	}

	results := make([]taskResult, len(node.Tasks))
	var wg sync.WaitGroup
	var cancelOnce sync.Once
	var mu sync.Mutex
	cancelledBySibling := false

	for i, task := range node.Tasks {
		wg.Add(1)
		taskEnv := env.cloneSnapshot()
		var buf bytes.Buffer
		childCtx := &execContext{
			cacheManager:   ctx.cacheManager,
			data:           ctx.dataManager.Scope(),
			dataManager:    ctx.dataManager,
			ctx:            taskCtx,
			requestCtx:     ctx.requestCtx,
			output:         &buf,
			functions:      ctx.functions,
			permissions:    ctx.permissions,
			dialContext:    ctx.dialContext,
			templateLoader: ctx.templateLoader,
			maxLoopSteps:   ctx.maxLoopSteps,
			maxCallDepth:   ctx.maxCallDepth,
			callDepth:      ctx.callDepth,
			callStack:      append([]StackFrame(nil), ctx.callStack...),
		}

		go func(idx int, t *ast.TaskStatement, c *execContext, tEnv *scope, outBuf *bytes.Buffer) {
			defer wg.Done()
			defer c.data.Close()
			defer func() {
				if recover() != nil {
					mu.Lock()
					results[idx] = taskResult{err: &Error{Kind: StandardLibraryError, Span: t.Span, Err: fmt.Errorf("internal task failure")}, beforeStop: !cancelledBySibling}
					cancelledBySibling = true
					mu.Unlock()
					cancelOnce.Do(cancel)
				}
			}()
			var val grabrt.Value
			var err error
			if t.Call != nil {
				_, err = c.invokeCall(t.Call.Module, t.Call.Name, t.Call.Span, t.Call.Arguments, tEnv)
			} else {
				val, err = c.evaluate(t.Value, tEnv)
			}
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
		}(i, task, childCtx, taskEnv, &buf)
	}

	wg.Wait()

	// 1. Fatal errors (GRAB-E306/GRAB-E307) always take highest precedence in source order.
	for _, res := range results {
		if res.err != nil {
			var interpErr *Error
			if errors.As(res.err, &interpErr) && interpErr.IsFatal() {
				return res.err
			}
		}
	}
	// 2. Primary task errors (not caused solely by sibling context cancellation) in top-to-bottom source order.
	for _, res := range results {
		if res.err != nil && !isSiblingCanceledError(res.err, res.beforeStop) {
			return res.err
		}
	}
	// 3. Fallback if any error occurred.
	for _, res := range results {
		if res.err != nil {
			return res.err
		}
	}

	// Commit outputs and variable bindings in deterministic top-to-bottom source order.
	for _, res := range results {
		if containsCacheHandle(res.val) {
			return cacheError(node.Span, cache.ErrValue)
		}
	}
	for i, task := range node.Tasks {
		if results[i].out != "" {
			if _, err := io.WriteString(ctx.output, results[i].out); err != nil {
				return err
			}
		}
		if task.Call == nil {
			env.set(task.Name, results[i].val)
		}
	}
	return nil
}

func isSiblingCanceledError(err error, beforeStop bool) bool {
	if beforeStop {
		return false
	}
	var interpErr *Error
	if errors.As(err, &interpErr) && interpErr.Err != nil {
		return errors.Is(interpErr.Err, context.Canceled)
	}
	return errors.Is(err, context.Canceled)
}

func (ctx *execContext) invokeCall(module, name string, span lexer.Span, argExprs []ast.Expression, callerEnv *scope) (grabrt.Value, error) {
	if module == "data" {
		if e := validateDataCall(name, argExprs, span); e != nil {
			return grabrt.Value{}, e
		}
	}
	argVals := make([]grabrt.Value, len(argExprs))
	for i, argExpr := range argExprs {
		val, err := ctx.evaluate(argExpr, callerEnv)
		if err != nil {
			return grabrt.Value{}, err
		}
		argVals[i] = val
	}

	if module != "" {
		stdFn, _ := lookupStdlibFunc(module, name)
		val, err := stdFn.fn(ctx, span, argVals)
		return val, ctx.attachStack(err)
	}

	fn := ctx.functions[name]
	ctx.callDepth++
	if ctx.callDepth > ctx.maxCallDepth {
		ctx.callDepth--
		return grabrt.Value{}, ctx.attachStack(&Error{Kind: CallStackOverflow, Span: span})
	}
	ctx.callStack = append(ctx.callStack, StackFrame{Function: name, Span: span})
	defer func() {
		ctx.callDepth--
		ctx.callStack = ctx.callStack[:len(ctx.callStack)-1]
	}()

	fnScope := newScope(nil)
	for i, param := range fn.Parameters {
		fnScope.defineLocal(param.Name, argVals[i])
	}
	retVal, returned, err := ctx.execBlock(fn.Body, fnScope)
	if err != nil {
		return grabrt.Value{}, ctx.attachStack(err)
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
		if target.Kind() != grabrt.RecordKind && target.Kind() != grabrt.MapKind {
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
		case grabrt.MapKind, grabrt.RecordKind:
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
		if target.Kind() != grabrt.MapKind && target.Kind() != grabrt.RecordKind {
			return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: node.Span}
		}
		_, ok := target.LookupEntry(node.Key)
		return grabrt.Boolean(ok), nil
	case *ast.CallExpr:
		return ctx.invokeCall(node.Module, node.Name, node.Span, node.Arguments, env)
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
	if left.ContainsResource() || right.ContainsResource() {
		return grabrt.Value{}, &Error{Kind: TypeMismatch, Span: node.Span}
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
