package lexer

import (
	"fmt"
)

// LexErrorKind classifies lexical failures.
type LexErrorKind uint8

const (
	LexInvalidCharacter LexErrorKind = iota
	LexInvalidEscape
	LexUnterminatedText
	LexUnterminatedBytes
	LexMalformedNumber
	LexInvalidByte
)

// LexError identifies a malformed source construct.
type LexError struct {
	Kind LexErrorKind
	Span Span
}

func (e *LexError) Error() string {
	return fmt.Sprintf("%s at line %d, column %d", e.Kind, e.Span.Start.Line, e.Span.Start.Column)
}

// String returns a stable category name for the lexical error.
func (kind LexErrorKind) String() string {
	switch kind {
	case LexInvalidCharacter:
		return "invalid character"
	case LexInvalidEscape:
		return "invalid escape"
	case LexUnterminatedText:
		return "unterminated text literal"
	case LexUnterminatedBytes:
		return "unterminated byte literal"
	case LexMalformedNumber:
		return "malformed number"
	case LexInvalidByte:
		return "invalid byte literal character"
	default:
		return "unknown lexical error"
	}
}

// Lexer converts GRAB source text into tokens while preserving each token's
// original spelling and source span.
type Lexer struct {
	reader *SourceReader
}

// NewLexer creates a lexer for valid UTF-8 source.
func NewLexer(name, source string) (*Lexer, error) {
	reader, err := NewSourceReader(name, source)
	if err != nil {
		return nil, err
	}
	return &Lexer{reader: reader}, nil
}

// LexAll returns every token, including the final EOF token.
func (l *Lexer) LexAll() ([]Token, error) {
	var tokens []Token
	for {
		token, err := l.NextToken()
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
		if token.Kind == TokenEOF {
			return tokens, nil
		}
	}
}

// NextToken returns the next token. Repeated calls after the source is
// exhausted return EOF tokens at the current position.
func (l *Lexer) NextToken() (Token, error) {
	for {
		value, ok := l.reader.PeekRune()
		if !ok {
			position := l.reader.Position()
			return Token{Kind: TokenEOF, Span: Span{Start: position, End: position}}, nil
		}

		if value == ' ' || value == '\t' {
			l.reader.ReadRune()
			continue
		}
		if value == '#' {
			token, hasNewline := l.skipComment()
			if hasNewline {
				return token, nil
			}
			continue
		}
		break
	}

	start := l.reader.Position()
	value, _, _ := l.reader.ReadRune()
	switch {
	case value == '\n':
		return l.token(TokenNewline, start), nil
	case isASCIIIdentifierStart(value):
		return l.scanIdentifierOrBytes(start, value)
	case isASCIIDigit(value) || value == '-' && l.nextRuneIsDigit():
		return l.scanNumber(start, value)
	case value == '"':
		return l.scanText(start)
	case value == '[':
		return l.token(TokenLeftBracket, start), nil
	case value == ']':
		return l.token(TokenRightBracket, start), nil
	case value == '{':
		return l.token(TokenLeftBrace, start), nil
	case value == '}':
		return l.token(TokenRightBrace, start), nil
	case value == ',':
		return l.token(TokenComma, start), nil
	case value == ':':
		return l.token(TokenColon, start), nil
	default:
		return Token{}, &LexError{Kind: LexInvalidCharacter, Span: l.spanFrom(start)}
	}
}

func (l *Lexer) skipComment() (Token, bool) {
	l.reader.ReadRune() // '#'
	for {
		value, ok := l.reader.PeekRune()
		if !ok {
			return Token{}, false
		}
		if value == '\n' {
			newlineStart := l.reader.Position()
			l.reader.ReadRune()
			return l.token(TokenNewline, newlineStart), true
		}
		l.reader.ReadRune()
	}
}

func (l *Lexer) scanIdentifierOrBytes(start Position, first rune) (Token, error) {
	for {
		value, ok := l.reader.PeekRune()
		if !ok || !isASCIIIdentifierContinue(value) {
			break
		}
		l.reader.ReadRune()
	}

	identifier := l.lexeme(start)
	if identifier == "b" {
		if next, ok := l.reader.PeekRune(); ok && next == '"' {
			return l.scanBytes(start)
		}
	}

	kind := TokenIdentifier
	switch identifier {
	case "set":
		kind = TokenSet
	case "to":
		kind = TokenTo
	case "show":
		kind = TokenShow
	case "joined":
		kind = TokenJoined
	case "with":
		kind = TokenWith
	case "yes", "no":
		kind = TokenBooleanLiteral
	case "nothing":
		kind = TokenNothingLiteral
	case "record":
		kind = TokenRecord
	}
	return l.token(kind, start), nil
}

func (l *Lexer) scanNumber(start Position, first rune) (Token, error) {
	if first == '-' {
		l.reader.ReadRune()
	}
	l.consumeDigits()

	kind := TokenIntegerLiteral
	if value, ok := l.reader.PeekRune(); ok && value == '.' {
		l.reader.ReadRune()
		if next, ok := l.reader.PeekRune(); !ok || !isASCIIDigit(next) {
			return Token{}, &LexError{Kind: LexMalformedNumber, Span: l.spanFrom(start)}
		}
		l.consumeDigits()
		kind = TokenDecimalLiteral
	}

	if value, ok := l.reader.PeekRune(); ok && (value == 'e' || value == 'E') {
		l.reader.ReadRune()
		if sign, ok := l.reader.PeekRune(); ok && (sign == '+' || sign == '-') {
			l.reader.ReadRune()
		}
		if digit, ok := l.reader.PeekRune(); !ok || !isASCIIDigit(digit) {
			return Token{}, &LexError{Kind: LexMalformedNumber, Span: l.spanFrom(start)}
		}
		l.consumeDigits()
		kind = TokenFloatLiteral
	}

	return l.token(kind, start), nil
}

func (l *Lexer) consumeDigits() {
	for {
		value, ok := l.reader.PeekRune()
		if !ok || !isASCIIDigit(value) {
			return
		}
		l.reader.ReadRune()
	}
}

func (l *Lexer) scanText(start Position) (Token, error) {
	// The opening quote is the smallest meaningful location for a literal
	// that never closes (specification Sections 6 and 8).
	opening := Span{Start: start, End: l.reader.Position()}
	for {
		value, span, ok := l.reader.ReadRune()
		if !ok {
			return Token{}, &LexError{Kind: LexUnterminatedText, Span: opening}
		}
		switch value {
		case '"':
			return l.token(TokenTextLiteral, start), nil
		case '\n', '\r':
			return Token{}, &LexError{Kind: LexUnterminatedText, Span: opening}
		case '\\':
			escaped, escapedSpan, ok := l.reader.ReadRune()
			if !ok || !isTextEscape(escaped) {
				end := l.reader.Position()
				if ok {
					end = escapedSpan.End
				}
				return Token{}, &LexError{Kind: LexInvalidEscape, Span: Span{Start: span.Start, End: end}}
			}
		}
	}
}

func (l *Lexer) scanBytes(start Position) (Token, error) {
	l.reader.ReadRune() // opening quote after the 'b' prefix
	for {
		value, span, ok := l.reader.ReadRune()
		if !ok {
			return Token{}, &LexError{Kind: LexUnterminatedBytes, Span: Span{Start: start, End: l.reader.Position()}}
		}
		switch {
		case value == '"':
			return l.token(TokenBytesLiteral, start), nil
		case value == '\n':
			return Token{}, &LexError{Kind: LexUnterminatedBytes, Span: Span{Start: start, End: span.End}}
		case value == '\\':
			if err := l.scanByteEscape(span.Start); err != nil {
				return Token{}, err
			}
		case value >= 0x20 && value <= 0x7e:
			// Printable ASCII is a byte unless it needs escaping.
			if value == '"' {
				return Token{}, &LexError{Kind: LexInvalidByte, Span: span}
			}
		default:
			return Token{}, &LexError{Kind: LexInvalidByte, Span: span}
		}
	}
}

func (l *Lexer) scanByteEscape(start Position) error {
	value, span, ok := l.reader.ReadRune()
	if !ok {
		return &LexError{Kind: LexInvalidEscape, Span: Span{Start: start, End: l.reader.Position()}}
	}
	if value == '\\' || value == '"' {
		return nil
	}
	if value != 'x' {
		return &LexError{Kind: LexInvalidEscape, Span: Span{Start: start, End: span.End}}
	}

	for i := 0; i < 2; i++ {
		digit, digitSpan, ok := l.reader.ReadRune()
		if !ok || !isASCIIHexDigit(digit) {
			end := l.reader.Position()
			if ok {
				end = digitSpan.End
			}
			return &LexError{Kind: LexInvalidEscape, Span: Span{Start: start, End: end}}
		}
	}
	return nil
}

func (l *Lexer) token(kind TokenKind, start Position) Token {
	span := l.spanFrom(start)
	lexeme, _ := l.reader.Slice(span)
	return Token{Kind: kind, Lexeme: lexeme, Span: span}
}

func (l *Lexer) lexeme(start Position) string {
	span := l.spanFrom(start)
	value, _ := l.reader.Slice(span)
	return value
}

func (l *Lexer) spanFrom(start Position) Span {
	return Span{Start: start, End: l.reader.Position()}
}

// nextRuneIsDigit reports whether the rune immediately ahead (the character
// right after the already-consumed '-') is an ASCII digit.  The '-' has been
// consumed from l.reader before this helper is called, so we only need to
// peek at the current position — no extra ReadRune is required.
func (l *Lexer) nextRuneIsDigit() bool {
	value, ok := l.reader.PeekRune()
	return ok && isASCIIDigit(value)
}

func isASCIIIdentifierStart(value rune) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isASCIIIdentifierContinue(value rune) bool {
	return isASCIIIdentifierStart(value) || isASCIIDigit(value)
}

func isASCIIDigit(value rune) bool {
	return value >= '0' && value <= '9'
}

func isASCIIHexDigit(value rune) bool {
	return isASCIIDigit(value) || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}

func isTextEscape(value rune) bool {
	switch value {
	case '\\', '"', 'n', 'r', 't':
		return true
	default:
		return false
	}
}
