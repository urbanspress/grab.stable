package lexer

import (
	"errors"
	"reflect"
	"testing"
)

func TestLexerLexAll(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		kinds   []TokenKind
		lexemes []string
	}{
		{
			name:  "keywords and punctuation",
			input: "set to show joined with yes no nothing record []{} ,:\n",
			kinds: []TokenKind{
				TokenSet, TokenTo, TokenShow, TokenJoined, TokenWith,
				TokenBooleanLiteral, TokenBooleanLiteral, TokenNothingLiteral, TokenRecord,
				TokenLeftBracket, TokenRightBracket, TokenLeftBrace, TokenRightBrace,
				TokenComma, TokenColon, TokenNewline, TokenEOF,
			},
			lexemes: []string{
				"set", "to", "show", "joined", "with", "yes", "no", "nothing", "record",
				"[", "]", "{", "}", ",", ":", "\n", "",
			},
		},
		{
			name:    "numbers",
			input:   "-12 0 12.40 1e3 1.25E-2",
			kinds:   []TokenKind{TokenIntegerLiteral, TokenIntegerLiteral, TokenDecimalLiteral, TokenFloatLiteral, TokenFloatLiteral, TokenEOF},
			lexemes: []string{"-12", "0", "12.40", "1e3", "1.25E-2", ""},
		},
		{
			name:    "text and byte literals",
			input:   `"a\n\t\"\\" b"A\x00\xff\"\\z"`,
			kinds:   []TokenKind{TokenTextLiteral, TokenBytesLiteral, TokenEOF},
			lexemes: []string{`"a\n\t\"\\"`, `b"A\x00\xff\"\\z"`, ""},
		},
		{
			name:    "comments preserve newlines",
			input:   "# before\r\nset x to 1 # inline\n",
			kinds:   []TokenKind{TokenNewline, TokenSet, TokenIdentifier, TokenTo, TokenIntegerLiteral, TokenNewline, TokenEOF},
			lexemes: []string{"\r\n", "set", "x", "to", "1", "\n", ""},
		},
		{
			name:    "bytes prefix requires adjacent quote",
			input:   `b b "text"`,
			kinds:   []TokenKind{TokenIdentifier, TokenIdentifier, TokenTextLiteral, TokenEOF},
			lexemes: []string{"b", "b", `"text"`, ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer, err := NewLexer("test.grab", tt.input)
			if err != nil {
				t.Fatalf("NewLexer() error = %v", err)
			}
			tokens, err := lexer.LexAll()
			if err != nil {
				t.Fatalf("LexAll() error = %v", err)
			}
			if len(tokens) != len(tt.kinds) {
				t.Fatalf("LexAll() returned %d tokens, want %d: %#v", len(tokens), len(tt.kinds), tokens)
			}
			for i, token := range tokens {
				if token.Kind != tt.kinds[i] {
					t.Errorf("token %d kind = %s, want %s", i, token.Kind, tt.kinds[i])
				}
				if token.Lexeme != tt.lexemes[i] {
					t.Errorf("token %d lexeme = %q, want %q", i, token.Lexeme, tt.lexemes[i])
				}
			}
		})
	}
}

func TestLexerLexErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
		kind  LexErrorKind
	}{
		{name: "non-ASCII identifier", input: "café", kind: LexInvalidCharacter},
		{name: "invalid character", input: ";", kind: LexInvalidCharacter},
		{name: "lone carriage return", input: "\r", kind: LexInvalidCharacter},
		{name: "invalid text escape", input: `"a\q"`, kind: LexInvalidEscape},
		{name: "incomplete text escape", input: `"a\`, kind: LexInvalidEscape},
		{name: "unterminated text", input: `"abc`, kind: LexUnterminatedText},
		{name: "raw newline in text", input: "\"a\nb\"", kind: LexUnterminatedText},
		{name: "raw carriage return in text", input: "\"a\rb\"", kind: LexUnterminatedText},
		{name: "malformed exponent", input: "1e+", kind: LexMalformedNumber},
		{name: "decimal point without fraction", input: "1.", kind: LexMalformedNumber},
		{name: "invalid byte escape", input: `b"\x0"`, kind: LexInvalidEscape},
		{name: "invalid byte character", input: `b"é"`, kind: LexInvalidByte},
		{name: "unterminated byte literal", input: `b"abc`, kind: LexUnterminatedBytes},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer, err := NewLexer("test.grab", tt.input)
			if err != nil {
				t.Fatalf("NewLexer() error = %v", err)
			}
			_, err = lexer.LexAll()
			if err == nil {
				t.Fatal("LexAll() error = nil, want lexical error")
			}

			var lexErr *LexError
			if !errors.As(err, &lexErr) {
				t.Fatalf("LexAll() error type = %T, want *LexError", err)
			}
			if lexErr.Kind != tt.kind {
				t.Errorf("LexError.Kind = %s, want %s", lexErr.Kind, tt.kind)
			}
			if lexErr.Span.Start.Line < 1 || lexErr.Span.Start.Column < 1 || lexErr.Span.End.Offset < lexErr.Span.Start.Offset {
				t.Errorf("LexError.Span is invalid: %+v", lexErr.Span)
			}
		})
	}
}

func TestLexerTokenSpansAndRepeatedEOF(t *testing.T) {
	lexer, err := NewLexer("test.grab", "show\r\n\"é\"")
	if err != nil {
		t.Fatalf("NewLexer() error = %v", err)
	}

	var spans []Span
	for {
		token, err := lexer.NextToken()
		if err != nil {
			t.Fatalf("NextToken() error = %v", err)
		}
		spans = append(spans, token.Span)
		if token.Kind == TokenEOF {
			break
		}
	}
	want := []Span{
		{Start: Position{Offset: 0, Line: 1, Column: 1}, End: Position{Offset: 4, Line: 1, Column: 5}},
		{Start: Position{Offset: 4, Line: 1, Column: 5}, End: Position{Offset: 6, Line: 2, Column: 1}},
		{Start: Position{Offset: 6, Line: 2, Column: 1}, End: Position{Offset: 10, Line: 2, Column: 4}},
		{Start: Position{Offset: 10, Line: 2, Column: 4}, End: Position{Offset: 10, Line: 2, Column: 4}},
	}
	if !reflect.DeepEqual(spans, want) {
		t.Fatalf("token spans = %#v, want %#v", spans, want)
	}
	firstEOF, err := lexer.NextToken()
	if err != nil {
		t.Fatalf("NextToken() after EOF error = %v", err)
	}
	secondEOF, err := lexer.NextToken()
	if err != nil {
		t.Fatalf("second NextToken() after EOF error = %v", err)
	}
	if firstEOF.Kind != TokenEOF || secondEOF.Kind != TokenEOF || firstEOF.Span != secondEOF.Span {
		t.Fatalf("repeated EOF tokens = (%+v, %+v), want equal EOF tokens", firstEOF, secondEOF)
	}
}
