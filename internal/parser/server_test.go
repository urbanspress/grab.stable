package parser

import (
	"grab/internal/ast"
	"strings"
	"testing"
)

func TestRouteGrammarAndFormatting(t *testing.T) {
	source := "route \"GET\" \"/items/{id}\" using req\ngive back req.params.id\nend\n"
	p, e := Parse("app.grab", source)
	if e != nil {
		t.Fatal(e)
	}
	r, ok := p.Statements[0].(*ast.RouteDef)
	if !ok || r.RequestName != "req" || r.Span.Start.Line != 1 || r.Span.End.Line != 3 {
		t.Fatalf("route=%+v", r)
	}
	formatted, e := Format("app.grab", source)
	if e != nil || !strings.Contains(formatted, "\n  give back req.params.id\n") {
		t.Fatalf("%q %v", formatted, e)
	}
	twice, e := Format("app.grab", formatted)
	if e != nil || twice != formatted {
		t.Fatal("format is not stable")
	}
	for _, tc := range []struct{ name, source string }{
		{"missing method", "route /\nend\n"},
		{"missing path", "route \"GET\"\nend\n"},
		{"missing request", "route \"GET\" \"/\" using\nend\n"},
		{"missing end", "route \"GET\" \"/\"\n"},
		{"nested if", "if yes\n" + source + "end\n"},
		{"nested function", "make f\n" + source + "end\n"},
		{"nested route", "route \"GET\" \"/\"\n" + source + "end\n"},
		{"nested loop", "while yes\n" + source + "end\n"},
		{"nested try", "try\n" + source + "catch err\nend\n"},
		{"nested together", "do together\n" + source + "end\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := Parse("app.grab", tc.source); e == nil {
				t.Fatal("expected syntax error")
			}
		})
	}
}
