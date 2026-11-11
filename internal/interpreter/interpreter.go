package interpreter

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"grab/internal/ast"
	"grab/internal/lexer"
	grabrt "grab/internal/runtime"
)

// ErrorKind identifies a name-resolution or execution failure.
type ErrorKind uint8

const (
	UnboundVariable ErrorKind = iota
	JoinedRequiresText
	InvalidLiteral
)

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
	default:
		return fmt.Sprintf("invalid literal at line %d, column %d: %v", e.Span.Start.Line, e.Span.Start.Column, e.Err)
	}
}

// Run validates names in the complete program before executing statements.
// Runtime output from successful earlier show statements is retained if a
// later execution error occurs.
func Run(program *ast.Program, output io.Writer) error {
	_, err := RunWithEnvironment(program, output)
	return err
}

// RunWithEnvironment validates names in the complete program, executes all
// statements in source order, and returns a read-only snapshot of the final
// variable bindings.
func RunWithEnvironment(program *ast.Program, output io.Writer) (map[string]grabrt.Value, error) {
	if err := resolve(program); err != nil {
		return nil, err
	}
	environment := make(map[string]grabrt.Value)
	for _, statement := range program.Statements {
		switch node := statement.(type) {
		case *ast.SetStatement:
			value, err := evaluate(node.Value, environment)
			if err != nil {
				return nil, err
			}
			environment[node.Name] = value
		case *ast.ShowStatement:
			value, err := evaluate(node.Value, environment)
			if err != nil {
				return nil, err
			}
			if _, err := io.WriteString(output, value.Display()+"\n"); err != nil {
				return nil, err
			}
		}
	}
	return environment, nil
}

// Validate performs pre-execution name resolution without executing statements
// and returns the set of variable names bound by the program.
func Validate(program *ast.Program) (map[string]struct{}, error) {
	return resolveBindings(program)
}

func resolve(program *ast.Program) error {
	_, err := resolveBindings(program)
	return err
}

func resolveBindings(program *ast.Program) (map[string]struct{}, error) {
	bound := make(map[string]struct{})
	for _, statement := range program.Statements {
		var expression ast.Expression
		var binding string
		switch node := statement.(type) {
		case *ast.SetStatement:
			expression, binding = node.Value, node.Name
		case *ast.ShowStatement:
			expression = node.Value
		}
		if err := resolveExpression(expression, bound); err != nil {
			return nil, err
		}
		if binding != "" {
			bound[binding] = struct{}{}
		}
	}
	return bound, nil
}

func resolveExpression(expression ast.Expression, bound map[string]struct{}) error {
	switch node := expression.(type) {
	case *ast.IdentifierExpr:
		if _, ok := bound[node.Name]; !ok {
			return &Error{Kind: UnboundVariable, Name: node.Name, Span: node.Span}
		}
	case *ast.JoinedExpr:
		if err := resolveExpression(node.Left, bound); err != nil {
			return err
		}
		return resolveExpression(node.Right, bound)
	case *ast.ListExpr:
		for _, element := range node.Elements {
			if err := resolveExpression(element, bound); err != nil {
				return err
			}
		}
	case *ast.MapExpr:
		for _, entry := range node.Entries {
			if err := resolveExpression(entry.Value, bound); err != nil {
				return err
			}
		}
	case *ast.RecordExpr:
		for _, field := range node.Fields {
			if err := resolveExpression(field.Value, bound); err != nil {
				return err
			}
		}
	}
	return nil
}

func evaluate(expression ast.Expression, environment map[string]grabrt.Value) (grabrt.Value, error) {
	switch node := expression.(type) {
	case *ast.IdentifierExpr:
		return environment[node.Name], nil
	case *ast.LiteralExpr:
		return literal(node)
	case *ast.JoinedExpr:
		left, err := evaluate(node.Left, environment)
		if err != nil {
			return grabrt.Value{}, err
		}
		right, err := evaluate(node.Right, environment)
		if err != nil {
			return grabrt.Value{}, err
		}
		if left.Kind() != grabrt.TextKind || right.Kind() != grabrt.TextKind {
			return grabrt.Value{}, &Error{Kind: JoinedRequiresText, Span: node.Span}
		}
		return grabrt.Text(left.TextValue() + right.TextValue()), nil
	case *ast.ListExpr:
		values := make([]grabrt.Value, len(node.Elements))
		for i, item := range node.Elements {
			value, err := evaluate(item, environment)
			if err != nil {
				return grabrt.Value{}, err
			}
			values[i] = value
		}
		return grabrt.List(values), nil
	case *ast.MapExpr:
		entries := make([]grabrt.Entry, len(node.Entries))
		for i, item := range node.Entries {
			value, err := evaluate(item.Value, environment)
			if err != nil {
				return grabrt.Value{}, err
			}
			entries[i] = grabrt.Entry{Name: item.Key, Value: value}
		}
		return grabrt.Map(entries), nil
	case *ast.RecordExpr:
		entries := make([]grabrt.Entry, len(node.Fields))
		for i, item := range node.Fields {
			value, err := evaluate(item.Value, environment)
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
