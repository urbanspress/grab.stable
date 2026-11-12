package interpreter

import (
	"errors"

	"grab/internal/ast"
	"grab/internal/data"
	"grab/internal/lexer"
	rt "grab/internal/runtime"
)

var dataFunctions = map[string]stdlibFunc{
	"open": {arity: 1, fn: stdDataOpen}, "prepare": {arity: 3, fn: stdDataPrepare},
	"query": {minArity: 2, maxArity: 3, fn: stdDataQuery}, "one": {minArity: 2, maxArity: 3, fn: stdDataOne}, "execute": {minArity: 2, maxArity: 3, fn: stdDataExecute},
	"begin": {minArity: 1, maxArity: 2, fn: stdDataBegin}, "commit": {arity: 1, fn: stdDataCommit}, "rollback": {arity: 1, fn: stdDataRollback},
	"migrate": {arity: 2, fn: stdDataMigrate}, "close": {arity: 1, fn: stdDataClose},
}

func databaseError(span lexer.Span, e error) error {
	if e == nil {
		return nil
	}
	var de *data.Error
	if !errors.As(e, &de) {
		return &Error{Kind: DatabaseOperation, Span: span}
	}
	kind := DatabaseOperation
	switch de.Code {
	case data.Invalid:
		kind = InvalidDatabaseDefinition
	case data.Type:
		kind = DatabaseType
	case data.Ownership:
		kind = DatabaseOwnership
	case data.Migration:
		kind = DatabaseMigration
	case data.Limit:
		kind = DatabaseLimit
	case data.UnknownCommit:
		kind = DatabaseUnknownCommit
	case data.Denied:
		kind = DatabaseDenied
	}
	return &Error{Kind: kind, Span: span, Message: de.Message, Err: errors.New(de.Message)}
}
func dataTypeError(span lexer.Span) error {
	return databaseError(span, &data.Error{Code: data.Type, Message: "invalid database API argument"})
}
func poolArg(v rt.Value) (*data.Pool, bool) {
	p, ok := v.ResourceValue().(*data.Pool)
	return p, ok && p != nil
}
func transactionArg(v rt.Value) (*data.Transaction, bool) {
	p, ok := v.ResourceValue().(*data.Transaction)
	return p, ok && p != nil
}
func stdDataOpen(c *execContext, span lexer.Span, args []rt.Value) (rt.Value, error) {
	if c.eventWork != nil && args[0].TextValue() != c.eventDatabase {
		return rt.Value{}, workerDenied(span)
	}
	if args[0].Kind() != rt.TextKind {
		return rt.Value{}, dataTypeError(span)
	}
	p, e := c.data.Open(c.context(), args[0].TextValue())
	if e != nil {
		return rt.Value{}, databaseError(span, e)
	}
	return rt.Resource(p), nil
}
func stdDataPrepare(c *execContext, span lexer.Span, args []rt.Value) (rt.Value, error) {
	p, ok := poolArg(args[0])
	if !ok || args[1].Kind() != rt.TextKind {
		return rt.Value{}, dataTypeError(span)
	}
	contract, e := data.ContractFromValue(args[2])
	if e != nil {
		return rt.Value{}, databaseError(span, e)
	}
	stmt, e := c.data.Prepare(c.context(), p, args[1].TextValue(), contract)
	if e != nil {
		return rt.Value{}, databaseError(span, e)
	}
	return rt.Resource(stmt), nil
}
func dataQuery(c *execContext, span lexer.Span, args []rt.Value, one, execute bool) (rt.Value, error) {
	stmt, ok := args[0].ResourceValue().(*data.Statement)
	if !ok || stmt == nil {
		return rt.Value{}, dataTypeError(span)
	}
	var tx *data.Transaction
	if len(args) == 3 {
		tx, ok = transactionArg(args[2])
		if !ok {
			return rt.Value{}, dataTypeError(span)
		}
	}
	v, e := c.data.Query(c.context(), stmt, args[1], tx, one, execute)
	return v, databaseError(span, e)
}
func stdDataQuery(c *execContext, s lexer.Span, a []rt.Value) (rt.Value, error) {
	return dataQuery(c, s, a, false, false)
}
func stdDataOne(c *execContext, s lexer.Span, a []rt.Value) (rt.Value, error) {
	return dataQuery(c, s, a, true, false)
}
func stdDataExecute(c *execContext, s lexer.Span, a []rt.Value) (rt.Value, error) {
	return dataQuery(c, s, a, false, true)
}
func stdDataBegin(c *execContext, span lexer.Span, args []rt.Value) (rt.Value, error) {
	p, ok := poolArg(args[0])
	if !ok {
		return rt.Value{}, dataTypeError(span)
	}
	readOnly := false
	isolation := "serializable"
	if len(args) == 2 {
		if args[1].Kind() != rt.RecordKind {
			return rt.Value{}, dataTypeError(span)
		}
		for _, e := range args[1].Entries() {
			switch e.Name {
			case "read_only":
				if e.Value.Kind() != rt.BooleanKind {
					return rt.Value{}, dataTypeError(span)
				}
				readOnly = e.Value.BooleanValue()
			case "isolation":
				if e.Value.Kind() != rt.TextKind {
					return rt.Value{}, dataTypeError(span)
				}
				isolation = e.Value.TextValue()
			default:
				return rt.Value{}, dataTypeError(span)
			}
		}
	}
	tx, e := c.data.Begin(c.context(), p, readOnly, isolation)
	if e != nil {
		return rt.Value{}, databaseError(span, e)
	}
	return rt.Resource(tx), nil
}
func stdDataCommit(c *execContext, span lexer.Span, args []rt.Value) (rt.Value, error) {
	tx, ok := transactionArg(args[0])
	if !ok {
		return rt.Value{}, dataTypeError(span)
	}
	return rt.Nothing(), databaseError(span, c.data.Commit(tx))
}
func stdDataRollback(c *execContext, span lexer.Span, args []rt.Value) (rt.Value, error) {
	tx, ok := transactionArg(args[0])
	if !ok {
		return rt.Value{}, dataTypeError(span)
	}
	return rt.Nothing(), databaseError(span, c.data.Rollback(tx))
}
func stdDataClose(c *execContext, span lexer.Span, args []rt.Value) (rt.Value, error) {
	p, ok := poolArg(args[0])
	if !ok {
		return rt.Value{}, dataTypeError(span)
	}
	return rt.Nothing(), databaseError(span, c.data.ClosePool(p))
}
func stdDataMigrate(c *execContext, span lexer.Span, args []rt.Value) (rt.Value, error) {
	p, ok := poolArg(args[0])
	if !ok {
		return rt.Value{}, dataTypeError(span)
	}
	versions, e := data.MigrationManifest(args[1], p.Driver())
	if e != nil {
		return rt.Value{}, databaseError(span, e)
	}
	v, e := c.data.Migrate(c.context(), p, versions)
	return v, databaseError(span, e)
}

// literalDataValue never evaluates code or resolves names. This is shared by
// static checking and the embedded/runtime call boundary.
func literalDataValue(expr ast.Expression) (rt.Value, bool) {
	switch node := expr.(type) {
	case *ast.LiteralExpr:
		v, e := literal(node)
		return v, e == nil
	case *ast.RecordExpr:
		var entries []rt.Entry
		for _, f := range node.Fields {
			v, ok := literalDataValue(f.Value)
			if !ok {
				return rt.Value{}, false
			}
			entries = append(entries, rt.Entry{Name: f.Name, Value: v})
		}
		return rt.Record(entries), true
	case *ast.ListExpr:
		var values []rt.Value
		for _, element := range node.Elements {
			v, ok := literalDataValue(element)
			if !ok {
				return rt.Value{}, false
			}
			values = append(values, v)
		}
		return rt.List(values), true
	}
	return rt.Value{}, false
}

func validateDataCall(name string, args []ast.Expression, span lexer.Span) error {
	bad := func() error {
		return databaseError(span, &data.Error{Code: data.Invalid, Message: "database aliases, SQL, contracts and migration manifests must be valid direct literals"})
	}
	switch name {
	case "open":
		if len(args) != 1 {
			return bad()
		}
		n, ok := args[0].(*ast.LiteralExpr)
		if !ok || n.Kind != ast.TextLiteral {
			return bad()
		}
		v, ok := literalDataValue(n)
		if !ok || !data.Identifier(v.TextValue()) {
			return bad()
		}
	case "prepare":
		if len(args) != 3 {
			return bad()
		}
		sqlNode, ok := args[1].(*ast.LiteralExpr)
		if !ok || sqlNode.Kind != ast.TextLiteral {
			return bad()
		}
		text, ok := literalDataValue(sqlNode)
		if !ok {
			return bad()
		}
		v, ok := literalDataValue(args[2])
		if !ok {
			return bad()
		}
		contract, e := data.ContractFromValue(v)
		if e != nil {
			return databaseError(span, e)
		}
		valid := false
		for _, dialect := range []string{"sqlite", "mariadb", "postgres"} {
			scanned, e := data.ScanSQL(text.TextValue(), dialect)
			if e == nil {
				_, e = data.ValidateStatement(scanned)
			}
			if e == nil {
				e = contract.ValidateSQL(scanned, "")
			}
			if e == nil {
				valid = true
				break
			}
		}
		if !valid {
			return bad()
		}
	case "migrate":
		if len(args) != 2 {
			return bad()
		}
		v, ok := literalDataValue(args[1])
		if !ok {
			return bad()
		}
		valid := false
		for _, dialect := range []string{"sqlite", "mariadb", "postgres"} {
			if _, e := data.MigrationManifest(v, dialect); e == nil {
				valid = true
				break
			}
		}
		if !valid {
			return bad()
		}
	}
	return nil
}
