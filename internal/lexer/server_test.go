package lexer

import "testing"

func TestRouteKeyword(t *testing.T) {
	for _, tc := range []struct {
		text string
		kind TokenKind
	}{{"route", TokenRoute}, {"routes", TokenIdentifier}, {"server", TokenIdentifier}, {"req", TokenIdentifier}, {"status", TokenIdentifier}, {"headers", TokenIdentifier}, {"GET", TokenIdentifier}} {
		l, e := NewLexer("app.grab", tc.text)
		if e != nil {
			t.Fatal(e)
		}
		tokens, e := l.LexAll()
		if e != nil || tokens[0].Kind != tc.kind {
			t.Fatalf("%s: %v %v", tc.text, tokens, e)
		}
	}
}
