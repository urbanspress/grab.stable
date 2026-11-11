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

// Parser parses GRAB tokens into an AST.
type Parser struct {
	tokens         []lexer.Token
	current        int
	inRepeatHeader bool
}

func (parser *Parser) parseProgram() (*ast.Program, error) {
	parser.skipNewlines()
	start := parser.currentToken().Span.Start
	statements := make([]ast.Statement, 0)

	for !parser.check(lexer.TokenEOF) {
		var statement ast.Statement
		var err error
		if parser.check(lexer.TokenMake) {
			statement, err = parser.parseFunctionDef()
		} else {
			statement, err = parser.parseStatement()
		}
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

func (parser *Parser) parseFunctionDef() (ast.Statement, error) {
	start := parser.advance().Span.Start // 'make'
	name, err := parser.consume(lexer.TokenIdentifier)
	if err != nil {
		return nil, err
	}

	var params []ast.Parameter
	if !parser.check(lexer.TokenNewline) && !parser.check(lexer.TokenEOF) {
		parser.match(lexer.TokenUsing)
		for {
			param, err := parser.consume(lexer.TokenIdentifier)
			if err != nil {
				return nil, err
			}
			params = append(params, ast.Parameter{Name: param.Lexeme, Span: param.Span})
			if !parser.match(lexer.TokenComma) {
				break
			}
		}
	}

	if _, err := parser.consume(lexer.TokenNewline); err != nil {
		return nil, err
	}
	body, err := parser.parseBlockBody(lexer.TokenEnd)
	if err != nil {
		return nil, err
	}
	endTok, err := parser.consume(lexer.TokenEnd)
	if err != nil {
		return nil, err
	}
	return &ast.FunctionDef{
		Name:       name.Lexeme,
		NameSpan:   name.Span,
		Parameters: params,
		Body:       body,
		Span:       lexer.Span{Start: start, End: endTok.Span.End},
	}, nil
}

func (parser *Parser) parseBlockBody(terminators ...lexer.TokenKind) ([]ast.Statement, error) {
	parser.skipNewlines()
	statements := make([]ast.Statement, 0)
	for !parser.check(lexer.TokenEOF) && !parser.checkAny(terminators...) {
		stmt, err := parser.parseStatement()
		if err != nil {
			return nil, err
		}
		statements = append(statements, stmt)
		if !parser.check(lexer.TokenNewline) && !parser.check(lexer.TokenEOF) {
			return nil, parser.unexpected(parser.currentToken())
		}
		parser.skipNewlines()
	}
	return statements, nil
}

func (parser *Parser) parseStatement() (ast.Statement, error) {
	switch parser.currentToken().Kind {
	case lexer.TokenSet:
		return parser.parseSetStatement()
	case lexer.TokenShow:
		return parser.parseShowStatement()
	case lexer.TokenIf:
		return parser.parseIfStatement()
	case lexer.TokenRepeat:
		return parser.parseRepeatStatement()
	case lexer.TokenFor:
		return parser.parseForEachStatement()
	case lexer.TokenWhile:
		return parser.parseWhileStatement()
	case lexer.TokenGive:
		return parser.parseGiveBackStatement()
	case lexer.TokenCall:
		return parser.parseCallStatement()
	case lexer.TokenIdentifier:
		if parser.peekToken().Kind == lexer.TokenUsing {
			return parser.parseImplicitCallStatement()
		}
		return nil, parser.unexpected(parser.currentToken())
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

func (parser *Parser) parseIfStatement() (ast.Statement, error) {
	start := parser.advance().Span.Start // 'if'
	cond, err := parser.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := parser.consume(lexer.TokenNewline); err != nil {
		return nil, err
	}
	body, err := parser.parseBlockBody(lexer.TokenOtherwise, lexer.TokenEnd)
	if err != nil {
		return nil, err
	}
	branches := []ast.IfBranch{
		{
			Condition: cond,
			Body:      body,
			Span:      lexer.Span{Start: start, End: cond.SourceSpan().End},
		},
	}

	var otherwiseBody []ast.Statement
	hasElse := false
	for parser.check(lexer.TokenOtherwise) {
		otherwiseTok := parser.advance()
		if parser.match(lexer.TokenIf) {
			elifCond, err := parser.parseExpression()
			if err != nil {
				return nil, err
			}
			if _, err := parser.consume(lexer.TokenNewline); err != nil {
				return nil, err
			}
			elifBody, err := parser.parseBlockBody(lexer.TokenOtherwise, lexer.TokenEnd)
			if err != nil {
				return nil, err
			}
			branches = append(branches, ast.IfBranch{
				Condition: elifCond,
				Body:      elifBody,
				Span:      lexer.Span{Start: otherwiseTok.Span.Start, End: elifCond.SourceSpan().End},
			})
			continue
		}
		if _, err := parser.consume(lexer.TokenNewline); err != nil {
			return nil, err
		}
		elseBody, err := parser.parseBlockBody(lexer.TokenEnd)
		if err != nil {
			return nil, err
		}
		hasElse = true
		otherwiseBody = elseBody
		break
	}

	endTok, err := parser.consume(lexer.TokenEnd)
	if err != nil {
		return nil, err
	}
	return &ast.IfStatement{
		Branches:  branches,
		Otherwise: otherwiseBody,
		HasElse:   hasElse,
		Span:      lexer.Span{Start: start, End: endTok.Span.End},
	}, nil
}

func (parser *Parser) parseRepeatStatement() (ast.Statement, error) {
	start := parser.advance().Span.Start // 'repeat'
	prevRepeat := parser.inRepeatHeader
	parser.inRepeatHeader = true
	count, err := parser.parseExpression()
	parser.inRepeatHeader = prevRepeat
	if err != nil {
		return nil, err
	}
	if _, err := parser.consume(lexer.TokenTimes); err != nil {
		return nil, err
	}
	if _, err := parser.consume(lexer.TokenNewline); err != nil {
		return nil, err
	}
	body, err := parser.parseBlockBody(lexer.TokenEnd)
	if err != nil {
		return nil, err
	}
	endTok, err := parser.consume(lexer.TokenEnd)
	if err != nil {
		return nil, err
	}
	return &ast.RepeatStatement{
		Count: count,
		Body:  body,
		Span:  lexer.Span{Start: start, End: endTok.Span.End},
	}, nil
}

func (parser *Parser) parseForEachStatement() (ast.Statement, error) {
	start := parser.advance().Span.Start // 'for'
	if _, err := parser.consume(lexer.TokenEach); err != nil {
		return nil, err
	}
	item, err := parser.consume(lexer.TokenIdentifier)
	if err != nil {
		return nil, err
	}
	if _, err := parser.consume(lexer.TokenIn); err != nil {
		return nil, err
	}
	collection, err := parser.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := parser.consume(lexer.TokenNewline); err != nil {
		return nil, err
	}
	body, err := parser.parseBlockBody(lexer.TokenEnd)
	if err != nil {
		return nil, err
	}
	endTok, err := parser.consume(lexer.TokenEnd)
	if err != nil {
		return nil, err
	}
	return &ast.ForEachStatement{
		Item:       item.Lexeme,
		ItemSpan:   item.Span,
		Collection: collection,
		Body:       body,
		Span:       lexer.Span{Start: start, End: endTok.Span.End},
	}, nil
}

func (parser *Parser) parseWhileStatement() (ast.Statement, error) {
	start := parser.advance().Span.Start // 'while'
	cond, err := parser.parseExpression()
	if err != nil {
		return nil, err
	}
	if _, err := parser.consume(lexer.TokenNewline); err != nil {
		return nil, err
	}
	body, err := parser.parseBlockBody(lexer.TokenEnd)
	if err != nil {
		return nil, err
	}
	endTok, err := parser.consume(lexer.TokenEnd)
	if err != nil {
		return nil, err
	}
	return &ast.WhileStatement{
		Condition: cond,
		Body:      body,
		Span:      lexer.Span{Start: start, End: endTok.Span.End},
	}, nil
}

func (parser *Parser) parseGiveBackStatement() (ast.Statement, error) {
	start := parser.advance().Span.Start // 'give'
	backTok, err := parser.consume(lexer.TokenBack)
	if err != nil {
		return nil, err
	}
	if parser.check(lexer.TokenNewline) || parser.check(lexer.TokenEOF) {
		return &ast.GiveBackStatement{
			Value: nil,
			Span:  lexer.Span{Start: start, End: backTok.Span.End},
		}, nil
	}
	val, err := parser.parseExpression()
	if err != nil {
		return nil, err
	}
	return &ast.GiveBackStatement{
		Value: val,
		Span:  lexer.Span{Start: start, End: val.SourceSpan().End},
	}, nil
}

func (parser *Parser) parseCallStatement() (ast.Statement, error) {
	start := parser.advance().Span.Start // 'call'
	name, err := parser.consume(lexer.TokenIdentifier)
	if err != nil {
		return nil, err
	}
	var args []ast.Expression
	end := name.Span.End
	if parser.match(lexer.TokenUsing) {
		args, err = parser.parseArgList()
		if err != nil {
			return nil, err
		}
		end = args[len(args)-1].SourceSpan().End
	}
	return &ast.CallStatement{
		Name:      name.Lexeme,
		NameSpan:  name.Span,
		Arguments: args,
		Span:      lexer.Span{Start: start, End: end},
	}, nil
}

func (parser *Parser) parseImplicitCallStatement() (ast.Statement, error) {
	name := parser.advance() // identifier
	parser.advance()         // 'using'
	args, err := parser.parseArgList()
	if err != nil {
		return nil, err
	}
	return &ast.CallStatement{
		Name:      name.Lexeme,
		NameSpan:  name.Span,
		Arguments: args,
		Span:      lexer.Span{Start: name.Span.Start, End: args[len(args)-1].SourceSpan().End},
	}, nil
}

func (parser *Parser) parseArgList() ([]ast.Expression, error) {
	var args []ast.Expression
	for {
		arg, err := parser.parseExpression()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		if !parser.match(lexer.TokenComma) {
			break
		}
	}
	return args, nil
}

func (parser *Parser) parseExpression() (ast.Expression, error) {
	return parser.parseOr()
}

func (parser *Parser) parseOr() (ast.Expression, error) {
	left, err := parser.parseAnd()
	if err != nil {
		return nil, err
	}
	for parser.match(lexer.TokenOr) {
		opTok := parser.previousToken()
		right, err := parser.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &ast.BinaryExpr{
			Op:     ast.OpOr,
			OpSpan: opTok.Span,
			Left:   left,
			Right:  right,
			Span:   lexer.Span{Start: left.SourceSpan().Start, End: right.SourceSpan().End},
		}
	}
	return left, nil
}

func (parser *Parser) parseAnd() (ast.Expression, error) {
	left, err := parser.parseEquality()
	if err != nil {
		return nil, err
	}
	for parser.match(lexer.TokenAnd) {
		opTok := parser.previousToken()
		right, err := parser.parseEquality()
		if err != nil {
			return nil, err
		}
		left = &ast.BinaryExpr{
			Op:     ast.OpAnd,
			OpSpan: opTok.Span,
			Left:   left,
			Right:  right,
			Span:   lexer.Span{Start: left.SourceSpan().Start, End: right.SourceSpan().End},
		}
	}
	return left, nil
}

func (parser *Parser) parseEquality() (ast.Expression, error) {
	left, err := parser.parseRelational()
	if err != nil {
		return nil, err
	}
	if parser.match(lexer.TokenIs) {
		isTok := parser.previousToken()
		op := ast.OpIs
		opSpan := isTok.Span
		if parser.match(lexer.TokenNot) {
			notTok := parser.previousToken()
			op = ast.OpIsNot
			opSpan = lexer.Span{Start: isTok.Span.Start, End: notTok.Span.End}
		}
		right, err := parser.parseRelational()
		if err != nil {
			return nil, err
		}
		return &ast.BinaryExpr{
			Op:     op,
			OpSpan: opSpan,
			Left:   left,
			Right:  right,
			Span:   lexer.Span{Start: left.SourceSpan().Start, End: right.SourceSpan().End},
		}, nil
	}
	return left, nil
}

func (parser *Parser) parseRelational() (ast.Expression, error) {
	left, err := parser.parseJoined()
	if err != nil {
		return nil, err
	}

	if parser.check(lexer.TokenIs) {
		peek := parser.peekToken().Kind
		if peek == lexer.TokenGreater || peek == lexer.TokenLess || peek == lexer.TokenAt {
			isTok := parser.advance()
			op, endSpan, err := parser.parseRelationalTail()
			if err != nil {
				return nil, err
			}
			right, err := parser.parseJoined()
			if err != nil {
				return nil, err
			}
			return &ast.BinaryExpr{
				Op:     op,
				OpSpan: lexer.Span{Start: isTok.Span.Start, End: endSpan.End},
				Left:   left,
				Right:  right,
				Span:   lexer.Span{Start: left.SourceSpan().Start, End: right.SourceSpan().End},
			}, nil
		}
	} else if parser.check(lexer.TokenGreater) || parser.check(lexer.TokenLess) ||
		(parser.check(lexer.TokenAt) && (parser.peekToken().Kind == lexer.TokenLeast || parser.peekToken().Kind == lexer.TokenMost)) {
		startTok := parser.currentToken()
		op, endSpan, err := parser.parseRelationalTail()
		if err != nil {
			return nil, err
		}
		right, err := parser.parseJoined()
		if err != nil {
			return nil, err
		}
		return &ast.BinaryExpr{
			Op:     op,
			OpSpan: lexer.Span{Start: startTok.Span.Start, End: endSpan.End},
			Left:   left,
			Right:  right,
			Span:   lexer.Span{Start: left.SourceSpan().Start, End: right.SourceSpan().End},
		}, nil
	}

	return left, nil
}

func (parser *Parser) parseRelationalTail() (ast.BinaryOp, lexer.Span, error) {
	switch {
	case parser.match(lexer.TokenGreater):
		thanTok, err := parser.consume(lexer.TokenThan)
		if err != nil {
			return 0, lexer.Span{}, err
		}
		return ast.OpGreaterThan, thanTok.Span, nil
	case parser.match(lexer.TokenLess):
		thanTok, err := parser.consume(lexer.TokenThan)
		if err != nil {
			return 0, lexer.Span{}, err
		}
		return ast.OpLessThan, thanTok.Span, nil
	case parser.match(lexer.TokenAt):
		if parser.match(lexer.TokenLeast) {
			return ast.OpAtLeast, parser.previousToken().Span, nil
		}
		mostTok, err := parser.consume(lexer.TokenMost)
		if err != nil {
			return 0, lexer.Span{}, err
		}
		return ast.OpAtMost, mostTok.Span, nil
	default:
		return 0, lexer.Span{}, parser.unexpected(parser.currentToken())
	}
}

func (parser *Parser) parseJoined() (ast.Expression, error) {
	left, err := parser.parseAdditive()
	if err != nil {
		return nil, err
	}
	for parser.match(lexer.TokenJoined) {
		if _, err := parser.consume(lexer.TokenWith); err != nil {
			return nil, err
		}
		right, err := parser.parseAdditive()
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

func (parser *Parser) parseAdditive() (ast.Expression, error) {
	left, err := parser.parseMultiplicative()
	if err != nil {
		return nil, err
	}
	for parser.check(lexer.TokenPlus) || parser.check(lexer.TokenMinus) {
		opTok := parser.advance()
		op := ast.OpPlus
		if opTok.Kind == lexer.TokenMinus {
			op = ast.OpMinus
		}
		right, err := parser.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		left = &ast.BinaryExpr{
			Op:     op,
			OpSpan: opTok.Span,
			Left:   left,
			Right:  right,
			Span:   lexer.Span{Start: left.SourceSpan().Start, End: right.SourceSpan().End},
		}
	}
	return left, nil
}

func (parser *Parser) parseMultiplicative() (ast.Expression, error) {
	left, err := parser.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		if parser.check(lexer.TokenTimes) {
			if parser.inRepeatHeader && (parser.peekToken().Kind == lexer.TokenNewline || parser.peekToken().Kind == lexer.TokenEOF) {
				break
			}
			opTok := parser.advance()
			right, err := parser.parseUnary()
			if err != nil {
				return nil, err
			}
			left = &ast.BinaryExpr{
				Op:     ast.OpTimes,
				OpSpan: opTok.Span,
				Left:   left,
				Right:  right,
				Span:   lexer.Span{Start: left.SourceSpan().Start, End: right.SourceSpan().End},
			}
			continue
		}
		if parser.match(lexer.TokenDivided) {
			divTok := parser.previousToken()
			byTok, err := parser.consume(lexer.TokenBy)
			if err != nil {
				return nil, err
			}
			right, err := parser.parseUnary()
			if err != nil {
				return nil, err
			}
			left = &ast.BinaryExpr{
				Op:     ast.OpDividedBy,
				OpSpan: lexer.Span{Start: divTok.Span.Start, End: byTok.Span.End},
				Left:   left,
				Right:  right,
				Span:   lexer.Span{Start: left.SourceSpan().Start, End: right.SourceSpan().End},
			}
			continue
		}
		if parser.match(lexer.TokenModulo) {
			modTok := parser.previousToken()
			right, err := parser.parseUnary()
			if err != nil {
				return nil, err
			}
			left = &ast.BinaryExpr{
				Op:     ast.OpModulo,
				OpSpan: modTok.Span,
				Left:   left,
				Right:  right,
				Span:   lexer.Span{Start: left.SourceSpan().Start, End: right.SourceSpan().End},
			}
			continue
		}
		break
	}
	return left, nil
}

func (parser *Parser) parseUnary() (ast.Expression, error) {
	if parser.match(lexer.TokenNot) {
		notTok := parser.previousToken()
		operand, err := parser.parseUnary()
		if err != nil {
			return nil, err
		}
		return &ast.UnaryExpr{
			Op:      ast.UnaryNot,
			OpSpan:  notTok.Span,
			Operand: operand,
			Span:    lexer.Span{Start: notTok.Span.Start, End: operand.SourceSpan().End},
		}, nil
	}
	return parser.parsePostfix()
}

func (parser *Parser) parsePostfix() (ast.Expression, error) {
	expr, err := parser.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		if parser.match(lexer.TokenDot) {
			field, err := parser.consume(lexer.TokenIdentifier)
			if err != nil {
				return nil, err
			}
			expr = &ast.FieldAccessExpr{
				Target:    expr,
				Field:     field.Lexeme,
				FieldSpan: field.Span,
				Span:      lexer.Span{Start: expr.SourceSpan().Start, End: field.Span.End},
			}
			continue
		}
		if parser.check(lexer.TokenAt) && parser.peekToken().Kind != lexer.TokenLeast && parser.peekToken().Kind != lexer.TokenMost {
			atTok := parser.advance()
			indexExpr, err := parser.parsePrimary()
			if err != nil {
				return nil, err
			}
			expr = &ast.IndexAccessExpr{
				Target: expr,
				Index:  indexExpr,
				OpSpan: atTok.Span,
				Span:   lexer.Span{Start: expr.SourceSpan().Start, End: indexExpr.SourceSpan().End},
			}
			continue
		}
		if parser.match(lexer.TokenHas) {
			hasTok := parser.previousToken()
			if parser.check(lexer.TokenTextLiteral) {
				keyTok := parser.advance()
				decodedKey, err := strconv.Unquote(keyTok.Lexeme)
				if err != nil {
					return nil, &ParseError{Kind: ParseUnexpectedToken, Span: keyTok.Span, Got: keyTok.Kind}
				}
				expr = &ast.HasExpr{
					Target:    expr,
					Key:       decodedKey,
					IsTextKey: true,
					KeySpan:   keyTok.Span,
					OpSpan:    hasTok.Span,
					Span:      lexer.Span{Start: expr.SourceSpan().Start, End: keyTok.Span.End},
				}
			} else {
				fieldTok, err := parser.consume(lexer.TokenIdentifier)
				if err != nil {
					return nil, err
				}
				expr = &ast.HasExpr{
					Target:    expr,
					Key:       fieldTok.Lexeme,
					IsTextKey: false,
					KeySpan:   fieldTok.Span,
					OpSpan:    hasTok.Span,
					Span:      lexer.Span{Start: expr.SourceSpan().Start, End: fieldTok.Span.End},
				}
			}
			continue
		}
		break
	}
	return expr, nil
}

func (parser *Parser) parsePrimary() (ast.Expression, error) {
	token := parser.currentToken()
	switch token.Kind {
	case lexer.TokenCall:
		start := parser.advance().Span.Start
		name, err := parser.consume(lexer.TokenIdentifier)
		if err != nil {
			return nil, err
		}
		var args []ast.Expression
		end := name.Span.End
		if parser.match(lexer.TokenUsing) {
			args, err = parser.parseArgList()
			if err != nil {
				return nil, err
			}
			end = args[len(args)-1].SourceSpan().End
		}
		return &ast.CallExpr{
			Name:      name.Lexeme,
			NameSpan:  name.Span,
			Arguments: args,
			Span:      lexer.Span{Start: start, End: end},
		}, nil
	case lexer.TokenIdentifier:
		parser.advance()
		if parser.match(lexer.TokenUsing) {
			args, err := parser.parseArgList()
			if err != nil {
				return nil, err
			}
			return &ast.CallExpr{
				Name:      token.Lexeme,
				NameSpan:  token.Span,
				Arguments: args,
				Span:      lexer.Span{Start: token.Span.Start, End: args[len(args)-1].SourceSpan().End},
			}, nil
		}
		return &ast.IdentifierExpr{Name: token.Lexeme, Span: token.Span}, nil
	case lexer.TokenLeftParen:
		start := parser.advance().Span.Start
		prevRepeat := parser.inRepeatHeader
		parser.inRepeatHeader = false
		inner, err := parser.parseExpression()
		parser.inRepeatHeader = prevRepeat
		if err != nil {
			return nil, err
		}
		closeTok, err := parser.consume(lexer.TokenRightParen)
		if err != nil {
			return nil, err
		}
		return &ast.ParenExpr{
			Inner: inner,
			Span:  lexer.Span{Start: start, End: closeTok.Span.End},
		}, nil
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

func (parser *Parser) checkAny(kinds ...lexer.TokenKind) bool {
	current := parser.currentToken().Kind
	for _, kind := range kinds {
		if current == kind {
			return true
		}
	}
	return false
}

func (parser *Parser) advance() lexer.Token {
	token := parser.currentToken()
	if token.Kind != lexer.TokenEOF && parser.current < len(parser.tokens) {
		parser.current++
	}
	return token
}

func (parser *Parser) previousToken() lexer.Token {
	if parser.current == 0 {
		return parser.currentToken()
	}
	return parser.tokens[parser.current-1]
}

func (parser *Parser) peekToken() lexer.Token {
	if parser.current+1 >= len(parser.tokens) {
		return parser.tokens[len(parser.tokens)-1]
	}
	return parser.tokens[parser.current+1]
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
