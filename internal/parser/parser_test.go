package parser

import (
	"errors"
	"testing"

	"grab/internal/ast"
	"grab/internal/lexer"
)

func TestParseFirstConformanceProgram(t *testing.T) {
	source := "set name to \"Allen\"\nset language to \"GRAB\"\nshow \"Welcome to \" joined with language\nshow \"Hello \" joined with name\n"
	program, err := Parse("first.grab", source)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(program.Statements) != 4 {
		t.Fatalf("len(Statements) = %d, want 4", len(program.Statements))
	}
	if _, ok := program.Statements[0].(*ast.SetStatement); !ok {
		t.Fatalf("statement 0 type = %T, want *ast.SetStatement", program.Statements[0])
	}
	if _, ok := program.Statements[2].(*ast.ShowStatement); !ok {
		t.Fatalf("statement 2 type = %T, want *ast.ShowStatement", program.Statements[2])
	}
	show, ok := program.Statements[2].(*ast.ShowStatement)
	if !ok {
		t.Fatalf("statement 2 type = %T, want *ast.ShowStatement", program.Statements[2])
	}
	joined, ok := show.Value.(*ast.JoinedExpr)
	if !ok {
		t.Fatalf("show expression type = %T, want *ast.JoinedExpr", show.Value)
	}
	if joined.Span.Start.Line != 3 || joined.Span.End.Line != 3 {
		t.Fatalf("joined span = %+v, want on line 3", joined.Span)
	}
}

func TestParseAllApprovedValueLiterals(t *testing.T) {
	source := `set product to record {sku: "A-17", active: yes, stock: 12, price: 19.95, ratio: 1.25e-2, labels: ["new", "sale"], payload: b"\x00\xff", note: nothing}`
	program, err := Parse("values.grab", source)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(program.Statements) != 1 {
		t.Fatalf("len(Statements) = %d, want 1", len(program.Statements))
	}
	statement, ok := program.Statements[0].(*ast.SetStatement)
	if !ok {
		t.Fatalf("statement type = %T, want *ast.SetStatement", program.Statements[0])
	}
	product, ok := statement.Value.(*ast.RecordExpr)
	if !ok {
		t.Fatalf("value type = %T, want *ast.RecordExpr", statement.Value)
	}
	if len(product.Fields) != 8 {
		t.Fatalf("record field count = %d, want 8", len(product.Fields))
	}
	if product.Fields[0].Name != "sku" || product.Fields[1].Name != "active" || product.Fields[7].Name != "note" {
		t.Fatalf("record field order = (%q, %q, ..., %q), want (sku, active, ..., note)", product.Fields[0].Name, product.Fields[1].Name, product.Fields[7].Name)
	}
	if _, ok := product.Fields[4].Value.(*ast.LiteralExpr); !ok {
		t.Fatalf("ratio field type = %T, want *ast.LiteralExpr", product.Fields[4].Value)
	}
	labels, ok := product.Fields[5].Value.(*ast.ListExpr)
	if !ok || len(labels.Elements) != 2 {
		t.Fatalf("labels field = %#v, want a two-element list", product.Fields[5].Value)
	}
}

func TestParseEmptyAndTrailingCommaContainers(t *testing.T) {
	tests := []struct {
		name  string
		value string
		check func(ast.Expression) bool
	}{
		{name: "empty list", value: "[]", check: func(expr ast.Expression) bool { list, ok := expr.(*ast.ListExpr); return ok && len(list.Elements) == 0 }},
		{name: "list trailing comma", value: "[1,]", check: func(expr ast.Expression) bool { list, ok := expr.(*ast.ListExpr); return ok && len(list.Elements) == 1 }},
		{name: "empty map", value: "{}", check: func(expr ast.Expression) bool { value, ok := expr.(*ast.MapExpr); return ok && len(value.Entries) == 0 }},
		{name: "map trailing comma", value: `{"x": 1,}`, check: func(expr ast.Expression) bool { value, ok := expr.(*ast.MapExpr); return ok && len(value.Entries) == 1 }},
		{name: "empty record", value: "record {}", check: func(expr ast.Expression) bool {
			value, ok := expr.(*ast.RecordExpr)
			return ok && len(value.Fields) == 0
		}},
		{name: "record trailing comma", value: "record {x: 1,}", check: func(expr ast.Expression) bool {
			value, ok := expr.(*ast.RecordExpr)
			return ok && len(value.Fields) == 1
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program, err := Parse("container.grab", "show "+tt.value)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			show := program.Statements[0].(*ast.ShowStatement)
			if !tt.check(show.Value) {
				t.Fatalf("parsed expression = %#v, failed expected container check", show.Value)
			}
		})
	}
}

func TestParseJoinedWithAssociatesLeft(t *testing.T) {
	program, err := Parse("joined.grab", `show "a" joined with "b" joined with "c"`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	show := program.Statements[0].(*ast.ShowStatement)
	outer, ok := show.Value.(*ast.JoinedExpr)
	if !ok {
		t.Fatalf("expression type = %T, want *ast.JoinedExpr", show.Value)
	}
	if _, ok := outer.Left.(*ast.JoinedExpr); !ok {
		t.Fatalf("left operand type = %T, want *ast.JoinedExpr for left association", outer.Left)
	}
	if _, ok := outer.Right.(*ast.LiteralExpr); !ok {
		t.Fatalf("right operand type = %T, want *ast.LiteralExpr", outer.Right)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name   string
		source string
		kind   ParseErrorKind
	}{
		{name: "missing to", source: "set x 1", kind: ParseExpectedToken},
		{name: "missing with", source: `show "a" joined "b"`, kind: ParseExpectedToken},
		{name: "leading comma in list", source: "show [,1]", kind: ParseUnexpectedToken},
		{name: "newline in list", source: "show [1,\n2]", kind: ParseUnexpectedToken},
		{name: "duplicate map key", source: `show {"a": 1, "a": 2}`, kind: ParseDuplicateMapKey},
		{name: "duplicate record field", source: "show record {x: 1, x: 2}", kind: ParseDuplicateRecordField},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse("bad.grab", tt.source)
			if err == nil {
				t.Fatal("Parse() error = nil, want parse error")
			}
			var parseErr *ParseError
			if !errors.As(err, &parseErr) {
				t.Fatalf("Parse() error type = %T, want *ParseError", err)
			}
			if parseErr.Kind != tt.kind {
				t.Errorf("ParseError.Kind = %d, want %d", parseErr.Kind, tt.kind)
			}
			if parseErr.Span.Start.Line < 1 || parseErr.Span.Start.Column < 1 {
				t.Errorf("ParseError.Span = %+v, want one-based line and column", parseErr.Span)
			}
		})
	}
}

func TestParseBlankProgramAndBlankLines(t *testing.T) {
	tests := []struct {
		name   string
		source string
		count  int
	}{
		{name: "empty", source: "", count: 0},
		{name: "blank lines", source: "\n\r\n\t\n", count: 0},
		{name: "comments and statements", source: "# one\nshow yes\n\n# two\r\nshow nothing", count: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program, err := Parse("blank.grab", tt.source)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if len(program.Statements) != tt.count {
				t.Fatalf("statement count = %d, want %d", len(program.Statements), tt.count)
			}
		})
	}
}

func TestParseTokens(t *testing.T) {
	lexerInstance, err := lexer.NewLexer("tokens.grab", "show yes")
	if err != nil {
		t.Fatalf("NewLexer() error = %v", err)
	}
	tokens, err := lexerInstance.LexAll()
	if err != nil {
		t.Fatalf("LexAll() error = %v", err)
	}
	program, err := ParseTokens(tokens)
	if err != nil {
		t.Fatalf("ParseTokens() error = %v", err)
	}
	if len(program.Statements) != 1 {
		t.Fatalf("statement count = %d, want 1", len(program.Statements))
	}
}

func TestParseGRAB03Constructs(t *testing.T) {
	tests := []struct {
		name   string
		source string
		check  func(*testing.T, *ast.Program)
	}{
		{
			name:   "operator precedence and grouping",
			source: "show (1 plus 2) times 3 minus 4 divided by 2 modulo 3 is at least 1 and not no or yes",
			check: func(t *testing.T, p *ast.Program) {
				show := p.Statements[0].(*ast.ShowStatement)
				orExpr, ok := show.Value.(*ast.BinaryExpr)
				if !ok || orExpr.Op != ast.OpOr {
					t.Fatalf("top expr = %#v, want BinaryExpr OpOr", show.Value)
				}
			},
		},
		{
			name:   "collection lookup and membership",
			source: "show product.tags at 0\nshow product has sku\nshow catalog has \"sku\"",
			check: func(t *testing.T, p *ast.Program) {
				if len(p.Statements) != 3 {
					t.Fatalf("len(Statements) = %d, want 3", len(p.Statements))
				}
				s0 := p.Statements[0].(*ast.ShowStatement)
				idx, ok := s0.Value.(*ast.IndexAccessExpr)
				if !ok {
					t.Fatalf("stmt 0 expr = %T, want *ast.IndexAccessExpr", s0.Value)
				}
				if _, ok := idx.Target.(*ast.FieldAccessExpr); !ok {
					t.Fatalf("idx.Target = %T, want *ast.FieldAccessExpr", idx.Target)
				}
			},
		},
		{
			name: "if otherwise if otherwise end",
			source: `if score is at least 90
  show "A"
otherwise if score is greater than 70
  show "B"
otherwise
  show "C"
end`,
			check: func(t *testing.T, p *ast.Program) {
				ifStmt, ok := p.Statements[0].(*ast.IfStatement)
				if !ok || len(ifStmt.Branches) != 2 || !ifStmt.HasElse {
					t.Fatalf("ifStmt = %#v, want 2 branches and else", p.Statements[0])
				}
			},
		},
		{
			name: "repeat for each and while loops",
			source: `repeat 2 times 3 times
  show "hi"
end
for each item in items
  show item
end
while active
  set active to no
end`,
			check: func(t *testing.T, p *ast.Program) {
				if len(p.Statements) != 3 {
					t.Fatalf("len(Statements) = %d, want 3", len(p.Statements))
				}
				if _, ok := p.Statements[0].(*ast.RepeatStatement); !ok {
					t.Fatalf("stmt 0 = %T, want *ast.RepeatStatement", p.Statements[0])
				}
				if _, ok := p.Statements[1].(*ast.ForEachStatement); !ok {
					t.Fatalf("stmt 1 = %T, want *ast.ForEachStatement", p.Statements[1])
				}
				if _, ok := p.Statements[2].(*ast.WhileStatement); !ok {
					t.Fatalf("stmt 2 = %T, want *ast.WhileStatement", p.Statements[2])
				}
			},
		},
		{
			name: "functions make using give back and call",
			source: `make add using a, b
  give back a plus b
end
make ping
  give back
end
call ping
set sum to add using 10, 20`,
			check: func(t *testing.T, p *ast.Program) {
				if len(p.Statements) != 4 {
					t.Fatalf("len(Statements) = %d, want 4", len(p.Statements))
				}
				fn, ok := p.Statements[0].(*ast.FunctionDef)
				if !ok || fn.Name != "add" || len(fn.Parameters) != 2 {
					t.Fatalf("stmt 0 = %#v, want FunctionDef add(a,b)", p.Statements[0])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			program, err := Parse("grab03.grab", tt.source)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			tt.check(t, program)
		})
	}
}
