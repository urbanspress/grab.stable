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

// Parameter is a named function parameter in a make declaration.
type Parameter struct {
	Name string
	Span lexer.Span
}

// FunctionDef defines a top-level reusable function.
type FunctionDef struct {
	Name       string
	NameSpan   lexer.Span
	Parameters []Parameter
	Body       []Statement
	Span       lexer.Span
}

func (statement *FunctionDef) SourceSpan() lexer.Span { return statement.Span }
func (*FunctionDef) statementNode()                   {}

// IfBranch is a conditional branch ('if' or 'otherwise if') in an IfStatement.
type IfBranch struct {
	Condition Expression
	Body      []Statement
	Span      lexer.Span
}

// IfStatement executes the first branch whose condition evaluates to yes, or
// the optional Otherwise body.
type IfStatement struct {
	Branches  []IfBranch
	Otherwise []Statement
	HasElse   bool
	Span      lexer.Span
}

func (statement *IfStatement) SourceSpan() lexer.Span { return statement.Span }
func (*IfStatement) statementNode()                   {}

// RepeatStatement executes its body a fixed non-negative integer number of times.
type RepeatStatement struct {
	Count Expression
	Body  []Statement
	Span  lexer.Span
}

func (statement *RepeatStatement) SourceSpan() lexer.Span { return statement.Span }
func (*RepeatStatement) statementNode()                   {}

// ForEachStatement iterates over the elements of a list or keys of a map.
type ForEachStatement struct {
	Item       string
	ItemSpan   lexer.Span
	Collection Expression
	Body       []Statement
	Span       lexer.Span
}

func (statement *ForEachStatement) SourceSpan() lexer.Span { return statement.Span }
func (*ForEachStatement) statementNode()                   {}

// WhileStatement executes its body while its boolean condition evaluates to yes.
type WhileStatement struct {
	Condition Expression
	Body      []Statement
	Span      lexer.Span
}

func (statement *WhileStatement) SourceSpan() lexer.Span { return statement.Span }
func (*WhileStatement) statementNode()                   {}

// GiveBackStatement returns a value (or nothing if Value is nil) from a function.
type GiveBackStatement struct {
	Value Expression
	Span  lexer.Span
}

func (statement *GiveBackStatement) SourceSpan() lexer.Span { return statement.Span }
func (*GiveBackStatement) statementNode()                   {}

// CallStatement invokes a function as a standalone statement.
type CallStatement struct {
	Name      string
	NameSpan  lexer.Span
	Arguments []Expression
	Span      lexer.Span
}

func (statement *CallStatement) SourceSpan() lexer.Span { return statement.Span }
func (*CallStatement) statementNode()                   {}

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

// ParenExpr wraps a parenthesized sub-expression.
type ParenExpr struct {
	Inner Expression
	Span  lexer.Span
}

func (expression *ParenExpr) SourceSpan() lexer.Span { return expression.Span }
func (*ParenExpr) expressionNode()                   {}

// UnaryOp identifies a unary operator in GRAB 0.3.
type UnaryOp uint8

const (
	UnaryNot UnaryOp = iota
)

// UnaryExpr applies a unary operator to an operand.
type UnaryExpr struct {
	Op      UnaryOp
	OpSpan  lexer.Span
	Operand Expression
	Span    lexer.Span
}

func (expression *UnaryExpr) SourceSpan() lexer.Span { return expression.Span }
func (*UnaryExpr) expressionNode()                   {}

// BinaryOp identifies a binary operator in GRAB 0.3.
type BinaryOp uint8

const (
	OpPlus BinaryOp = iota
	OpMinus
	OpTimes
	OpDividedBy
	OpModulo
	OpIs
	OpIsNot
	OpGreaterThan
	OpLessThan
	OpAtLeast
	OpAtMost
	OpAnd
	OpOr
)

// BinaryExpr applies a binary arithmetic, comparison, equality, or logical operator.
type BinaryExpr struct {
	Op     BinaryOp
	OpSpan lexer.Span
	Left   Expression
	Right  Expression
	Span   lexer.Span
}

func (expression *BinaryExpr) SourceSpan() lexer.Span { return expression.Span }
func (*BinaryExpr) expressionNode()                   {}

// FieldAccessExpr reads a named field from a record value (`record_val.field`).
type FieldAccessExpr struct {
	Target    Expression
	Field     string
	FieldSpan lexer.Span
	Span      lexer.Span
}

func (expression *FieldAccessExpr) SourceSpan() lexer.Span { return expression.Span }
func (*FieldAccessExpr) expressionNode()                   {}

// IndexAccessExpr reads an element by index from a list or by key from a map (`coll at key`).
type IndexAccessExpr struct {
	Target Expression
	Index  Expression
	OpSpan lexer.Span
	Span   lexer.Span
}

func (expression *IndexAccessExpr) SourceSpan() lexer.Span { return expression.Span }
func (*IndexAccessExpr) expressionNode()                   {}

// HasExpr checks whether a map contains a text key or a record contains a field (`coll has key`).
type HasExpr struct {
	Target    Expression
	Key       string
	IsTextKey bool
	KeySpan   lexer.Span
	OpSpan    lexer.Span
	Span      lexer.Span
}

func (expression *HasExpr) SourceSpan() lexer.Span { return expression.Span }
func (*HasExpr) expressionNode()                   {}

// CallExpr invokes a defined function inside an expression (`fn using args` or `call fn`).
type CallExpr struct {
	Name      string
	NameSpan  lexer.Span
	Arguments []Expression
	Span      lexer.Span
}

func (expression *CallExpr) SourceSpan() lexer.Span { return expression.Span }
func (*CallExpr) expressionNode()                   {}
