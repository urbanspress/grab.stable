package lexer

// TokenKind identifies a lexical token in the GRAB 0.1 grammar.
type TokenKind uint8

const (
	TokenInvalid TokenKind = iota
	TokenEOF
	TokenNewline
	TokenIdentifier
	TokenTextLiteral
	TokenBooleanLiteral
	TokenNothingLiteral
	TokenIntegerLiteral
	TokenDecimalLiteral
	TokenFloatLiteral
	TokenBytesLiteral
	TokenSet
	TokenTo
	TokenShow
	TokenJoined
	TokenWith
	TokenRecord
	TokenLeftBracket
	TokenRightBracket
	TokenLeftBrace
	TokenRightBrace
	TokenComma
	TokenColon
)

var tokenKindNames = [...]string{
	TokenInvalid:        "invalid",
	TokenEOF:            "EOF",
	TokenNewline:        "newline",
	TokenIdentifier:     "identifier",
	TokenTextLiteral:    "text literal",
	TokenBooleanLiteral: "boolean literal",
	TokenNothingLiteral: "nothing literal",
	TokenIntegerLiteral: "integer literal",
	TokenDecimalLiteral: "decimal literal",
	TokenFloatLiteral:   "float literal",
	TokenBytesLiteral:   "bytes literal",
	TokenSet:            "set",
	TokenTo:             "to",
	TokenShow:           "show",
	TokenJoined:         "joined",
	TokenWith:           "with",
	TokenRecord:         "record",
	TokenLeftBracket:    "[",
	TokenRightBracket:   "]",
	TokenLeftBrace:      "{",
	TokenRightBrace:     "}",
	TokenComma:          ",",
	TokenColon:          ":",
}

// String returns a readable name for the token kind.
func (kind TokenKind) String() string {
	if int(kind) >= len(tokenKindNames) {
		return "unknown"
	}
	return tokenKindNames[kind]
}

// Token retains the raw source spelling and its source span. Literal values
// are decoded by later lexer/parser stages, not by the token model.
type Token struct {
	Kind   TokenKind
	Lexeme string
	Span   Span
}
