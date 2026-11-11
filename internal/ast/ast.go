package ast

import "grab/internal/lexer"

// Node is any syntax-tree element with an associated source span.
type Node interface {
	SourceSpan() lexer.Span
}

// Program is a complete GRAB source file.
type Program struct {
	Statements []Statement
	Span       lexer.Span
}

func (program *Program) SourceSpan() lexer.Span { return program.Span }

// Statement is a top-level executable syntax node.
type Statement interface {
	Node
	statementNode()
}

// SetStatement binds or rebinds a variable.
type SetStatement struct {
	Name     string
	NameSpan lexer.Span
	Value    Expression
	Span     lexer.Span
}

func (statement *SetStatement) SourceSpan() lexer.Span { return statement.Span }
func (*SetStatement) statementNode()                   {}

// ShowStatement writes the value of its expression.
type ShowStatement struct {
	Value Expression
	Span  lexer.Span
}

func (statement *ShowStatement) SourceSpan() lexer.Span { return statement.Span }
func (*ShowStatement) statementNode()                   {}

// Expression is a value-producing syntax node.
type Expression interface {
	Node
	expressionNode()
}

// LiteralKind identifies the value kind represented by a LiteralExpr.
type LiteralKind uint8

const (
	TextLiteral LiteralKind = iota
	BooleanLiteral
	NothingLiteral
	IntegerLiteral
	DecimalLiteral
	FloatLiteral
	BytesLiteral
)

// LiteralExpr preserves the original spelling of a scalar literal.
type LiteralExpr struct {
	Kind   LiteralKind
	Lexeme string
	Span   lexer.Span
}

func (expression *LiteralExpr) SourceSpan() lexer.Span { return expression.Span }
func (*LiteralExpr) expressionNode()                   {}

// IdentifierExpr reads a variable binding.
type IdentifierExpr struct {
	Name string
	Span lexer.Span
}

func (expression *IdentifierExpr) SourceSpan() lexer.Span { return expression.Span }
func (*IdentifierExpr) expressionNode()                   {}

// JoinedExpr concatenates two text expressions.
type JoinedExpr struct {
	Left  Expression
	Right Expression
	Span  lexer.Span
}

func (expression *JoinedExpr) SourceSpan() lexer.Span { return expression.Span }
func (*JoinedExpr) expressionNode()                   {}

// ListExpr contains an ordered sequence of values.
type ListExpr struct {
	Elements []Expression
	Span     lexer.Span
}

func (expression *ListExpr) SourceSpan() lexer.Span { return expression.Span }
func (*ListExpr) expressionNode()                   {}

// MapEntry is a string-keyed map value.
type MapEntry struct {
	Key     string
	KeySpan lexer.Span
	Value   Expression
}

// MapExpr contains map entries in source order.
type MapExpr struct {
	Entries []MapEntry
	Span    lexer.Span
}

func (expression *MapExpr) SourceSpan() lexer.Span { return expression.Span }
func (*MapExpr) expressionNode()                   {}

// RecordField is a named record value.
type RecordField struct {
	Name     string
	NameSpan lexer.Span
	Value    Expression
}

// RecordExpr contains fields in source order.
type RecordExpr struct {
	Fields []RecordField
	Span   lexer.Span
}

func (expression *RecordExpr) SourceSpan() lexer.Span { return expression.Span }
func (*RecordExpr) expressionNode()                   {}
