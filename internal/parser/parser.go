package parser

import (
	"fmt"
	"strconv"

	"grab/internal/ast"
	"grab/internal/lexer"
)

// ParseErrorKind classifies parser failures.
type ParseErrorKind uint8

const (
	ParseUnexpectedToken ParseErrorKind = iota
	ParseExpectedToken
	ParseDuplicateMapKey
	ParseDuplicateRecordField
)

// ParseError identifies an unexpected or invalid syntax construct.
type ParseError struct {
	Kind     ParseErrorKind
	Span     lexer.Span
	Got      lexer.TokenKind
	Expected lexer.TokenKind
	Name     string
}

func (e *ParseError) Error() string {
	switch e.Kind {
	case ParseExpectedToken:
		return fmt.Sprintf("expected %s at line %d, column %d; found %s", e.Expected, e.Span.Start.Line, e.Span.Start.Column, e.Got)
	case ParseDuplicateMapKey:
		return fmt.Sprintf("duplicate map key %q at line %d, column %d", e.Name, e.Span.Start.Line, e.Span.Start.Column)
	case ParseDuplicateRecordField:
		return fmt.Sprintf("duplicate record field %q at line %d, column %d", e.Name, e.Span.Start.Line, e.Span.Start.Column)
	default:
		return fmt.Sprintf("unexpected %s at line %d, column %d", e.Got, e.Span.Start.Line, e.Span.Start.Column)
	}
}

// Parse lexes and parses a complete GRAB source file.
func Parse(name, source string) (*ast.Program, error) {
	lexerInstance, err := lexer.NewLexer(name, source)
	if err != nil {
		return nil, err
	}
	tokens, err := lexerInstance.LexAll()
	if err != nil {
		return nil, err
	}
	return ParseTokens(tokens)
}

// ParseTokens parses a token stream ending in EOF.
func ParseTokens(tokens []lexer.Token) (*ast.Program, error) {
	parser := &Parser{tokens: tokens}
	return parser.parseProgram()
}

// Parser parses GRAB 0.1 tokens into an AST.
type Parser struct {
	tokens  []lexer.Token
	current int
}

func (parser *Parser) parseProgram() (*ast.Program, error) {
	parser.skipNewlines()
	start := parser.currentToken().Span.Start
	statements := make([]ast.Statement, 0)

	for !parser.check(lexer.TokenEOF) {
		statement, err := parser.parseStatement()
		if err != nil {
			return nil, err
		}
		statements = append(statements, statement)

		if !parser.check(lexer.TokenNewline) && !parser.check(lexer.TokenEOF) {
			return nil, parser.unexpected(parser.currentToken())
		}
		parser.skipNewlines()
	}

	end := parser.currentToken().Span.End
	if len(statements) > 0 {
		end = statements[len(statements)-1].SourceSpan().End
	}
	return &ast.Program{
		Statements: statements,
		Span:       lexer.Span{Start: start, End: end},
	}, nil
}

func (parser *Parser) parseStatement() (ast.Statement, error) {
	switch parser.currentToken().Kind {
	case lexer.TokenSet:
		return parser.parseSetStatement()
	case lexer.TokenShow:
		return parser.parseShowStatement()
	default:
		return nil, parser.unexpected(parser.currentToken())
	}
}

func (parser *Parser) parseSetStatement() (ast.Statement, error) {
	start := parser.advance().Span.Start
	name, err := parser.consume(lexer.TokenIdentifier)
	if err != nil {
		return nil, err
	}
	if _, err := parser.consume(lexer.TokenTo); err != nil {
		return nil, err
	}
	value, err := parser.parseExpression()
	if err != nil {
		return nil, err
	}
	return &ast.SetStatement{
		Name:     name.Lexeme,
		NameSpan: name.Span,
		Value:    value,
		Span:     lexer.Span{Start: start, End: value.SourceSpan().End},
	}, nil
}

func (parser *Parser) parseShowStatement() (ast.Statement, error) {
	start := parser.advance().Span.Start
	value, err := parser.parseExpression()
	if err != nil {
		return nil, err
	}
	return &ast.ShowStatement{
		Value: value,
		Span:  lexer.Span{Start: start, End: value.SourceSpan().End},
	}, nil
}

func (parser *Parser) parseExpression() (ast.Expression, error) {
	left, err := parser.parsePrimary()
	if err != nil {
		return nil, err
	}
	for parser.match(lexer.TokenJoined) {
		if _, err := parser.consume(lexer.TokenWith); err != nil {
			return nil, err
		}
		right, err := parser.parsePrimary()
		if err != nil {
			return nil, err
		}
		left = &ast.JoinedExpr{
			Left:  left,
			Right: right,
			Span:  lexer.Span{Start: left.SourceSpan().Start, End: right.SourceSpan().End},
		}
	}
	return left, nil
}

func (parser *Parser) parsePrimary() (ast.Expression, error) {
	token := parser.currentToken()
	switch token.Kind {
	case lexer.TokenIdentifier:
		parser.advance()
		return &ast.IdentifierExpr{Name: token.Lexeme, Span: token.Span}, nil
	case lexer.TokenTextLiteral, lexer.TokenBooleanLiteral, lexer.TokenNothingLiteral,
		lexer.TokenIntegerLiteral, lexer.TokenDecimalLiteral, lexer.TokenFloatLiteral,
		lexer.TokenBytesLiteral:
		parser.advance()
		return &ast.LiteralExpr{Kind: literalKind(token.Kind), Lexeme: token.Lexeme, Span: token.Span}, nil
	case lexer.TokenLeftBracket:
		return parser.parseList()
	case lexer.TokenLeftBrace:
		return parser.parseMap()
	case lexer.TokenRecord:
		return parser.parseRecord()
	default:
		return nil, parser.unexpected(token)
	}
}

func (parser *Parser) parseList() (ast.Expression, error) {
	start := parser.advance().Span.Start
	elements := make([]ast.Expression, 0)
	if !parser.check(lexer.TokenRightBracket) {
		for {
			element, err := parser.parseExpression()
			if err != nil {
				return nil, err
			}
			elements = append(elements, element)
			if !parser.match(lexer.TokenComma) {
				break
			}
			if parser.check(lexer.TokenRightBracket) {
				break
			}
		}
	}
	closing, err := parser.consume(lexer.TokenRightBracket)
	if err != nil {
		return nil, err
	}
	return &ast.ListExpr{Elements: elements, Span: lexer.Span{Start: start, End: closing.Span.End}}, nil
}

func (parser *Parser) parseMap() (ast.Expression, error) {
	start := parser.advance().Span.Start
	entries := make([]ast.MapEntry, 0)
	seen := make(map[string]struct{})
	if !parser.check(lexer.TokenRightBrace) {
		for {
			key, err := parser.consume(lexer.TokenTextLiteral)
			if err != nil {
				return nil, err
			}
			decodedKey, err := strconv.Unquote(key.Lexeme)
			if err != nil {
				return nil, &ParseError{Kind: ParseUnexpectedToken, Span: key.Span, Got: key.Kind}
			}
			if _, exists := seen[decodedKey]; exists {
				return nil, &ParseError{Kind: ParseDuplicateMapKey, Span: key.Span, Name: decodedKey}
			}
			seen[decodedKey] = struct{}{}
			if _, err := parser.consume(lexer.TokenColon); err != nil {
				return nil, err
			}
			value, err := parser.parseExpression()
			if err != nil {
				return nil, err
			}
			entries = append(entries, ast.MapEntry{Key: decodedKey, KeySpan: key.Span, Value: value})
			if !parser.match(lexer.TokenComma) {
				break
			}
			if parser.check(lexer.TokenRightBrace) {
				break
			}
		}
	}
	closing, err := parser.consume(lexer.TokenRightBrace)
	if err != nil {
		return nil, err
	}
	return &ast.MapExpr{Entries: entries, Span: lexer.Span{Start: start, End: closing.Span.End}}, nil
}

func (parser *Parser) parseRecord() (ast.Expression, error) {
	start := parser.advance().Span.Start
	if _, err := parser.consume(lexer.TokenLeftBrace); err != nil {
		return nil, err
	}
	fields := make([]ast.RecordField, 0)
	seen := make(map[string]struct{})
	if !parser.check(lexer.TokenRightBrace) {
		for {
			name, err := parser.consume(lexer.TokenIdentifier)
			if err != nil {
				return nil, err
			}
			if _, exists := seen[name.Lexeme]; exists {
				return nil, &ParseError{Kind: ParseDuplicateRecordField, Span: name.Span, Name: name.Lexeme}
			}
			seen[name.Lexeme] = struct{}{}
			if _, err := parser.consume(lexer.TokenColon); err != nil {
				return nil, err
			}
			value, err := parser.parseExpression()
			if err != nil {
				return nil, err
			}
			fields = append(fields, ast.RecordField{Name: name.Lexeme, NameSpan: name.Span, Value: value})
			if !parser.match(lexer.TokenComma) {
				break
			}
			if parser.check(lexer.TokenRightBrace) {
				break
			}
		}
	}
	closing, err := parser.consume(lexer.TokenRightBrace)
	if err != nil {
		return nil, err
	}
	return &ast.RecordExpr{Fields: fields, Span: lexer.Span{Start: start, End: closing.Span.End}}, nil
}

func (parser *Parser) match(kind lexer.TokenKind) bool {
	if !parser.check(kind) {
		return false
	}
	parser.advance()
	return true
}

func (parser *Parser) consume(kind lexer.TokenKind) (lexer.Token, error) {
	if parser.check(kind) {
		return parser.advance(), nil
	}
	current := parser.currentToken()
	return lexer.Token{}, &ParseError{Kind: ParseExpectedToken, Span: current.Span, Got: current.Kind, Expected: kind}
}

func (parser *Parser) check(kind lexer.TokenKind) bool {
	return parser.currentToken().Kind == kind
}

func (parser *Parser) advance() lexer.Token {
	token := parser.currentToken()
	if token.Kind != lexer.TokenEOF && parser.current < len(parser.tokens) {
		parser.current++
	}
	return token
}

func (parser *Parser) currentToken() lexer.Token {
	if len(parser.tokens) == 0 {
		return lexer.Token{Kind: lexer.TokenEOF}
	}
	if parser.current >= len(parser.tokens) {
		return parser.tokens[len(parser.tokens)-1]
	}
	return parser.tokens[parser.current]
}

func (parser *Parser) skipNewlines() {
	for parser.match(lexer.TokenNewline) {
	}
}

func (parser *Parser) unexpected(token lexer.Token) error {
	return &ParseError{Kind: ParseUnexpectedToken, Span: token.Span, Got: token.Kind}
}

func literalKind(kind lexer.TokenKind) ast.LiteralKind {
	switch kind {
	case lexer.TokenTextLiteral:
		return ast.TextLiteral
	case lexer.TokenBooleanLiteral:
		return ast.BooleanLiteral
	case lexer.TokenNothingLiteral:
		return ast.NothingLiteral
	case lexer.TokenIntegerLiteral:
		return ast.IntegerLiteral
	case lexer.TokenDecimalLiteral:
		return ast.DecimalLiteral
	case lexer.TokenFloatLiteral:
		return ast.FloatLiteral
	case lexer.TokenBytesLiteral:
		return ast.BytesLiteral
	default:
		panic("literalKind called with non-literal token")
	}
}
