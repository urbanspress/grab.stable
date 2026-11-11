package tests

import (
	"testing"

	"grab/internal/ast"
	"grab/internal/lexer"
	"grab/internal/parser"
)

// Text concatenation is associative in its output, so output alone cannot
// prove the required left-associated AST. Also check exact byte/scalar spans
// using fixed expectations independent of the conformance harness's locator.
func TestSourceSpansAndAssociation(t *testing.T) {
	source := "# 😀\r\n\tshow \"é\" joined with \"b\" joined with \"c\"\r\n"
	program, err := parser.Parse("spans.grab", source)
	if err != nil {
		t.Fatal(err)
	}
	show := program.Statements[0].(*ast.ShowStatement)
	outer := show.Value.(*ast.JoinedExpr)
	left, ok := outer.Left.(*ast.JoinedExpr)
	if !ok {
		t.Fatalf("left operand %T is not a joined expression", outer.Left)
	}
	for _, tc := range []struct {
		name       string
		node       ast.Node
		start, end lexer.Position
	}{
		{"program", program, lexer.Position{Offset: 9, Line: 2, Column: 2}, lexer.Position{Offset: 50, Line: 2, Column: 42}},
		{"show", show, lexer.Position{Offset: 9, Line: 2, Column: 2}, lexer.Position{Offset: 50, Line: 2, Column: 42}},
		{"outer", outer, lexer.Position{Offset: 14, Line: 2, Column: 7}, lexer.Position{Offset: 50, Line: 2, Column: 42}},
		{"left", left, lexer.Position{Offset: 14, Line: 2, Column: 7}, lexer.Position{Offset: 34, Line: 2, Column: 26}},
		{"Unicode literal", left.Left, lexer.Position{Offset: 14, Line: 2, Column: 7}, lexer.Position{Offset: 18, Line: 2, Column: 10}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := lexer.Span{Start: tc.start, End: tc.end}
			if got := tc.node.SourceSpan(); got != want {
				t.Fatalf("span=%+v, want %+v", got, want)
			}
		})
	}
}
