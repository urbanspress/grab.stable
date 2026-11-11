package parser

import (
	"strconv"
	"strings"

	"grab/internal/ast"
)

// Format lexes and parses source as a GRAB program and returns the canonically
// formatted GRAB source text with 2-space block indentation, single spaces
// around keywords/delimiters, preserved '#' comments, and LF line endings.
func Format(name, source string) (string, error) {
	program, err := Parse(name, source)
	if err != nil {
		return "", err
	}

	normalized := strings.ReplaceAll(source, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")

	f := &sourceFormatter{
		lines:    lines,
		nextLine: 1,
	}
	f.formatStatements(program.Statements, 0)
	f.flushRemainingComments(len(lines), 0)

	if len(f.out) == 0 {
		return "", nil
	}
	return strings.Join(f.out, "\n") + "\n", nil
}

type sourceFormatter struct {
	lines     []string
	nextLine  int
	out       []string
	lastBlank bool
}

func (f *sourceFormatter) emitPriorComments(upToLine int, indent int) {
	for f.nextLine < upToLine && f.nextLine <= len(f.lines) {
		raw := f.lines[f.nextLine-1]
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			if len(f.out) > 0 && !f.lastBlank {
				f.out = append(f.out, "")
				f.lastBlank = true
			}
		} else if strings.HasPrefix(trimmed, "#") {
			f.out = append(f.out, strings.Repeat("  ", indent)+trimmed)
			f.lastBlank = false
		}
		f.nextLine++
	}
}

func (f *sourceFormatter) flushRemainingComments(maxLine int, indent int) {
	for f.nextLine <= maxLine && f.nextLine <= len(f.lines) {
		raw := f.lines[f.nextLine-1]
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			if len(f.out) > 0 && !f.lastBlank {
				f.out = append(f.out, "")
				f.lastBlank = true
			}
		} else if strings.HasPrefix(trimmed, "#") {
			f.out = append(f.out, strings.Repeat("  ", indent)+trimmed)
			f.lastBlank = false
		}
		f.nextLine++
	}
	for len(f.out) > 0 && f.out[len(f.out)-1] == "" {
		f.out = f.out[:len(f.out)-1]
	}
}

func (f *sourceFormatter) emitCodeLine(lineNum int, indent int, text string) {
	f.emitPriorComments(lineNum, indent)
	line := strings.Repeat("  ", indent) + text
	if lineNum >= 1 && lineNum <= len(f.lines) {
		if comment := extractInlineComment(f.lines[lineNum-1]); comment != "" {
			line += " " + comment
		}
	}
	f.out = append(f.out, line)
	f.lastBlank = false
	if f.nextLine <= lineNum {
		f.nextLine = lineNum + 1
	}
}

func (f *sourceFormatter) formatStatements(statements []ast.Statement, indent int) {
	for _, stmt := range statements {
		f.formatStatement(stmt, indent)
	}
}

func (f *sourceFormatter) formatStatement(stmt ast.Statement, indent int) {
	switch node := stmt.(type) {
	case *ast.UseDecl:
		f.emitCodeLine(node.Span.Start.Line, indent, "use "+node.Module)
	case *ast.FunctionDef:
		header := "make " + node.Name
		if len(node.Parameters) > 0 {
			params := make([]string, len(node.Parameters))
			for i, p := range node.Parameters {
				params[i] = p.Name
			}
			header += " using " + strings.Join(params, ", ")
		}
		f.emitCodeLine(node.Span.Start.Line, indent, header)
		f.formatStatements(node.Body, indent+1)
		f.emitPriorComments(node.Span.End.Line, indent+1)
		f.emitCodeLine(node.Span.End.Line, indent, "end")
	case *ast.SetStatement:
		f.emitCodeLine(node.Span.Start.Line, indent, "set "+node.Name+" to "+FormatExpression(node.Value))
	case *ast.ShowStatement:
		f.emitCodeLine(node.Span.Start.Line, indent, "show "+FormatExpression(node.Value))
	case *ast.IfStatement:
		for i, branch := range node.Branches {
			kw := "if "
			if i > 0 {
				kw = "otherwise if "
			}
			if i > 0 {
				f.emitPriorComments(branch.Span.Start.Line, indent+1)
			}
			f.emitCodeLine(branch.Span.Start.Line, indent, kw+FormatExpression(branch.Condition))
			f.formatStatements(branch.Body, indent+1)
		}
		if node.HasElse {
			elseLine := f.findKeywordLineBefore(node.Span.End.Line, node.Otherwise, "otherwise")
			f.emitPriorComments(elseLine, indent+1)
			f.emitCodeLine(elseLine, indent, "otherwise")
			f.formatStatements(node.Otherwise, indent+1)
		}
		f.emitPriorComments(node.Span.End.Line, indent+1)
		f.emitCodeLine(node.Span.End.Line, indent, "end")
	case *ast.RepeatStatement:
		f.emitCodeLine(node.Span.Start.Line, indent, "repeat "+FormatExpression(node.Count)+" times")
		f.formatStatements(node.Body, indent+1)
		f.emitPriorComments(node.Span.End.Line, indent+1)
		f.emitCodeLine(node.Span.End.Line, indent, "end")
	case *ast.ForEachStatement:
		f.emitCodeLine(node.Span.Start.Line, indent, "for each "+node.Item+" in "+FormatExpression(node.Collection))
		f.formatStatements(node.Body, indent+1)
		f.emitPriorComments(node.Span.End.Line, indent+1)
		f.emitCodeLine(node.Span.End.Line, indent, "end")
	case *ast.WhileStatement:
		f.emitCodeLine(node.Span.Start.Line, indent, "while "+FormatExpression(node.Condition))
		f.formatStatements(node.Body, indent+1)
		f.emitPriorComments(node.Span.End.Line, indent+1)
		f.emitCodeLine(node.Span.End.Line, indent, "end")
	case *ast.TryStatement:
		f.emitCodeLine(node.Span.Start.Line, indent, "try")
		f.formatStatements(node.TryBody, indent+1)
		catchLine := node.CatchNameSpan.Start.Line
		f.emitPriorComments(catchLine, indent+1)
		f.emitCodeLine(catchLine, indent, "catch "+node.CatchName)
		f.formatStatements(node.CatchBody, indent+1)
		f.emitPriorComments(node.Span.End.Line, indent+1)
		f.emitCodeLine(node.Span.End.Line, indent, "end")
	case *ast.RaiseStatement:
		f.emitCodeLine(node.Span.Start.Line, indent, "raise "+FormatExpression(node.Value))
	case *ast.GiveBackStatement:
		if node.Value == nil {
			f.emitCodeLine(node.Span.Start.Line, indent, "give back")
		} else {
			f.emitCodeLine(node.Span.Start.Line, indent, "give back "+FormatExpression(node.Value))
		}
	case *ast.CallStatement:
		if len(node.Arguments) == 0 {
			f.emitCodeLine(node.Span.Start.Line, indent, "call "+node.QualifiedName())
		} else {
			args := make([]string, len(node.Arguments))
			for i, a := range node.Arguments {
				args[i] = FormatExpression(a)
			}
			f.emitCodeLine(node.Span.Start.Line, indent, "call "+node.QualifiedName()+" using "+strings.Join(args, ", "))
		}
	}
}

func (f *sourceFormatter) findKeywordLineBefore(endLine int, body []ast.Statement, keyword string) int {
	limit := endLine
	if len(body) > 0 {
		limit = body[0].SourceSpan().Start.Line
	}
	for line := f.nextLine; line < limit && line <= len(f.lines); line++ {
		trimmed := strings.TrimSpace(f.lines[line-1])
		if strings.HasPrefix(trimmed, keyword) {
			return line
		}
	}
	return f.nextLine
}

// FormatExpression returns the canonical single-line representation of an AST expression.
func FormatExpression(expr ast.Expression) string {
	switch node := expr.(type) {
	case *ast.LiteralExpr:
		return node.Lexeme
	case *ast.IdentifierExpr:
		return node.Name
	case *ast.ParenExpr:
		return "(" + FormatExpression(node.Inner) + ")"
	case *ast.UnaryExpr:
		return "not " + FormatExpression(node.Operand)
	case *ast.JoinedExpr:
		return FormatExpression(node.Left) + " joined with " + FormatExpression(node.Right)
	case *ast.BinaryExpr:
		return FormatExpression(node.Left) + " " + formatBinaryOp(node.Op) + " " + FormatExpression(node.Right)
	case *ast.FieldAccessExpr:
		return FormatExpression(node.Target) + "." + node.Field
	case *ast.IndexAccessExpr:
		return FormatExpression(node.Target) + " at " + FormatExpression(node.Index)
	case *ast.HasExpr:
		if node.IsTextKey {
			return FormatExpression(node.Target) + " has " + strconv.Quote(node.Key)
		}
		return FormatExpression(node.Target) + " has " + node.Key
	case *ast.CallExpr:
		if len(node.Arguments) == 0 {
			return "call " + node.QualifiedName()
		}
		args := make([]string, len(node.Arguments))
		for i, a := range node.Arguments {
			args[i] = FormatExpression(a)
		}
		return node.QualifiedName() + " using " + strings.Join(args, ", ")
	case *ast.ListExpr:
		elems := make([]string, len(node.Elements))
		for i, el := range node.Elements {
			elems[i] = FormatExpression(el)
		}
		return "[" + strings.Join(elems, ", ") + "]"
	case *ast.MapExpr:
		entries := make([]string, len(node.Entries))
		for i, entry := range node.Entries {
			entries[i] = strconv.Quote(entry.Key) + ": " + FormatExpression(entry.Value)
		}
		return "{" + strings.Join(entries, ", ") + "}"
	case *ast.RecordExpr:
		fields := make([]string, len(node.Fields))
		for i, field := range node.Fields {
			fields[i] = field.Name + ": " + FormatExpression(field.Value)
		}
		return "record {" + strings.Join(fields, ", ") + "}"
	default:
		return ""
	}
}

func formatBinaryOp(op ast.BinaryOp) string {
	switch op {
	case ast.OpPlus:
		return "plus"
	case ast.OpMinus:
		return "minus"
	case ast.OpTimes:
		return "times"
	case ast.OpDividedBy:
		return "divided by"
	case ast.OpModulo:
		return "modulo"
	case ast.OpIs:
		return "is"
	case ast.OpIsNot:
		return "is not"
	case ast.OpGreaterThan:
		return "is greater than"
	case ast.OpLessThan:
		return "is less than"
	case ast.OpAtLeast:
		return "is at least"
	case ast.OpAtMost:
		return "is at most"
	case ast.OpAnd:
		return "and"
	case ast.OpOr:
		return "or"
	default:
		return ""
	}
}

func extractInlineComment(line string) string {
	inQuotes := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if inQuotes {
			if ch == '\\' && i+1 < len(line) {
				i++
				continue
			}
			if ch == '"' {
				inQuotes = false
			}
			continue
		}
		if ch == '"' {
			inQuotes = true
			continue
		}
		if ch == '#' {
			if strings.TrimSpace(line[:i]) == "" {
				// Standalone comment line, not an inline trailing comment.
				return ""
			}
			return strings.TrimRight(line[i:], " \t")
		}
	}
	return ""
}
