package lexer

import "testing"

func TestTokenKindString(t *testing.T) {
	tests := []struct {
		kind TokenKind
		want string
	}{
		{kind: TokenInvalid, want: "invalid"},
		{kind: TokenEOF, want: "EOF"},
		{kind: TokenNewline, want: "newline"},
		{kind: TokenIdentifier, want: "identifier"},
		{kind: TokenTextLiteral, want: "text literal"},
		{kind: TokenBooleanLiteral, want: "boolean literal"},
		{kind: TokenNothingLiteral, want: "nothing literal"},
		{kind: TokenIntegerLiteral, want: "integer literal"},
		{kind: TokenDecimalLiteral, want: "decimal literal"},
		{kind: TokenFloatLiteral, want: "float literal"},
		{kind: TokenBytesLiteral, want: "bytes literal"},
		{kind: TokenSet, want: "set"},
		{kind: TokenTo, want: "to"},
		{kind: TokenShow, want: "show"},
		{kind: TokenJoined, want: "joined"},
		{kind: TokenWith, want: "with"},
		{kind: TokenRecord, want: "record"},
		{kind: TokenLeftBracket, want: "["},
		{kind: TokenRightBracket, want: "]"},
		{kind: TokenLeftBrace, want: "{"},
		{kind: TokenRightBrace, want: "}"},
		{kind: TokenComma, want: ","},
		{kind: TokenColon, want: ":"},
		{kind: TokenKind(255), want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.want {
				t.Fatalf("TokenKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}
