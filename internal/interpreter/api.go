package interpreter

import (
	"grab/internal/ast"
	"grab/internal/contracts"
	"grab/internal/lexer"
	rt "grab/internal/runtime"
	"strconv"
)

var apiFunctions = map[string]stdlibFunc{"input": {arity: 2, fn: stdAPIInput}, "respond": {arity: 3, fn: stdAPIRespond}}

func apiError(s lexer.Span, e error) error {
	if e == nil {
		return nil
	}
	return &Error{Kind: StandardLibraryError, CustomCode: "GRAB-E315", Message: "typed API contract validation failed", Span: s, Err: e}
}
func validateAPICall(name string, args []ast.Expression, s lexer.Span) error {
	i := 0
	if name == "input" {
		i = 1
	}
	if len(args) > i {
		if l, ok := args[i].(*ast.LiteralExpr); ok && l.Kind == ast.TextLiteral {
			if id, e := strconv.Unquote(l.Lexeme); e == nil && id != "" {
				return nil
			}
		}
	}
	return &Error{Kind: StandardLibraryError, CustomCode: "GRAB-E210", Message: "API operation must be a direct text literal", Span: s}
}
func stdAPIInput(c *execContext, s lexer.Span, a []rt.Value) (rt.Value, error) {
	if c.apiOperation == nil || a[1].TextValue() != c.apiOperation.ID || !a[0].Equal(c.apiRequest) {
		return rt.Value{}, apiError(s, contracts.ErrContract)
	}
	return c.apiInput, nil
}
func stdAPIRespond(c *execContext, s lexer.Span, a []rt.Value) (rt.Value, error) {
	n, ok := statusValue(a[1])
	if !ok || c.apiOperation == nil || a[0].TextValue() != c.apiOperation.ID {
		return rt.Value{}, apiError(s, contracts.ErrContract)
	}
	b, e := c.contracts.Response(*c.apiOperation, n, a[2])
	if e != nil {
		return rt.Value{}, apiError(s, e)
	}
	return simpleResponse(n, "application/json; charset=utf-8", string(b)), nil
}

// ValidateContracts checks all route bindings and literal references offline.
func ValidateContracts(p *ast.Program, d *contracts.Document) error {
	routes := map[string]bool{}
	for _, st := range p.Statements {
		if r, ok := st.(*ast.RouteDef); ok {
			routes[r.Method+" "+r.Path] = true
		}
	}
	if d != nil {
		if e := d.ValidateRoutes(routes); e != nil {
			return e
		}
	}
	return walkCalls(p, func(module, name string, args []ast.Expression, s lexer.Span) error {
		if module != "api" {
			return nil
		}
		if e := validateAPICall(name, args, s); e != nil {
			return e
		}
		i := 0
		if name == "input" {
			i = 1
		}
		id, _ := strconv.Unquote(args[i].(*ast.LiteralExpr).Lexeme)
		if _, ok := d.Operation(id); !ok {
			return contracts.ErrStatic
		}
		return nil
	})
}
