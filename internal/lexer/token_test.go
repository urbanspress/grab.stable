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
		{kind: TokenLeftParen, want: "("},
		{kind: TokenRightParen, want: ")"},
		{kind: TokenDot, want: "."},
		{kind: TokenIf, want: "if"},
		{kind: TokenOtherwise, want: "otherwise"},
		{kind: TokenEnd, want: "end"},
		{kind: TokenRepeat, want: "repeat"},
		{kind: TokenTimes, want: "times"},
		{kind: TokenFor, want: "for"},
		{kind: TokenEach, want: "each"},
		{kind: TokenIn, want: "in"},
		{kind: TokenWhile, want: "while"},
		{kind: TokenMake, want: "make"},
		{kind: TokenUsing, want: "using"},
		{kind: TokenGive, want: "give"},
		{kind: TokenBack, want: "back"},
		{kind: TokenCall, want: "call"},
		{kind: TokenIs, want: "is"},
		{kind: TokenNot, want: "not"},
		{kind: TokenGreater, want: "greater"},
		{kind: TokenLess, want: "less"},
		{kind: TokenThan, want: "than"},
		{kind: TokenAt, want: "at"},
		{kind: TokenLeast, want: "least"},
		{kind: TokenMost, want: "most"},
		{kind: TokenAnd, want: "and"},
		{kind: TokenOr, want: "or"},
		{kind: TokenPlus, want: "plus"},
		{kind: TokenMinus, want: "minus"},
		{kind: TokenDivided, want: "divided"},
		{kind: TokenBy, want: "by"},
		{kind: TokenModulo, want: "modulo"},
		{kind: TokenHas, want: "has"},
		{kind: TokenUse, want: "use"},
		{kind: TokenTry, want: "try"},
		{kind: TokenCatch, want: "catch"},
		{kind: TokenRaise, want: "raise"},
		{kind: TokenDo, want: "do"},
		{kind: TokenTogether, want: "together"},
		{kind: TokenWait, want: "wait"},
		{kind: TokenRoute, want: "route"},
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
