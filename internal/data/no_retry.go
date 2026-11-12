package data

import (
	"context"
	"database/sql/driver"
	"errors"
	"sync/atomic"
)

// database/sql normally retries ErrBadConn operations. GRAB deliberately exposes
// one outcome per operation instead. Mark the physical connection invalid for
// disposal, but do not return the retry sentinel from an attempted operation.
type noRetryConnector struct{ driver.Connector }

func (c noRetryConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &noRetryConn{Conn: conn}, nil
}

type noRetryConn struct {
	driver.Conn
	invalid atomic.Bool
}

func (c *noRetryConn) outcome(err error) error {
	if errors.Is(err, driver.ErrBadConn) {
		c.invalid.Store(true)
		return fail(Operation, "database connection failed; operation was not retried")
	}
	return err
}
func (c *noRetryConn) IsValid() bool {
	if c.invalid.Load() {
		return false
	}
	if v, ok := c.Conn.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}
func (c *noRetryConn) ResetSession(ctx context.Context) error {
	if c.invalid.Load() {
		return driver.ErrBadConn
	}
	if r, ok := c.Conn.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return ctx.Err()
}
func (c *noRetryConn) CheckNamedValue(v *driver.NamedValue) error {
	if n, ok := c.Conn.(driver.NamedValueChecker); ok {
		return n.CheckNamedValue(v)
	}
	return driver.ErrSkip
}
func (c *noRetryConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}
func (c *noRetryConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	var s driver.Stmt
	var err error
	if p, ok := c.Conn.(driver.ConnPrepareContext); ok {
		s, err = p.PrepareContext(ctx, query)
	} else if err = ctx.Err(); err == nil {
		s, err = c.Conn.Prepare(query)
	}
	if err != nil {
		return nil, c.outcome(err)
	}
	return &noRetryStmt{Stmt: s, conn: c}, nil
}
func (c *noRetryConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *noRetryConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	var tx driver.Tx
	var err error
	if b, ok := c.Conn.(driver.ConnBeginTx); ok {
		tx, err = b.BeginTx(ctx, o)
	} else if o.Isolation != 0 || o.ReadOnly {
		return nil, fail(Ownership, "transaction options unsupported")
	} else if err = ctx.Err(); err == nil {
		tx, err = c.Conn.Begin()
	}
	return tx, c.outcome(err)
}
func (c *noRetryConn) Ping(ctx context.Context) error {
	if p, ok := c.Conn.(driver.Pinger); ok {
		return c.outcome(p.Ping(ctx))
	}
	return ctx.Err()
}

// All application execution uses prepared Stmt methods, so no direct Execer or
// Queryer escape hatch is exposed by this wrapper.
type noRetryStmt struct {
	driver.Stmt
	conn *noRetryConn
}

func (s *noRetryStmt) Exec(args []driver.Value) (driver.Result, error) {
	r, e := s.Stmt.Exec(args)
	return r, s.conn.outcome(e)
}
func (s *noRetryStmt) Query(args []driver.Value) (driver.Rows, error) {
	r, e := s.Stmt.Query(args)
	return r, s.conn.outcome(e)
}
func (s *noRetryStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	if x, ok := s.Stmt.(driver.StmtExecContext); ok {
		r, e := x.ExecContext(ctx, args)
		return r, s.conn.outcome(e)
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	return s.Exec(plainValues(args))
}
func (s *noRetryStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	if x, ok := s.Stmt.(driver.StmtQueryContext); ok {
		r, e := x.QueryContext(ctx, args)
		return r, s.conn.outcome(e)
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	return s.Query(plainValues(args))
}
func plainValues(args []driver.NamedValue) []driver.Value {
	v := make([]driver.Value, len(args))
	for i, a := range args {
		v[i] = a.Value
	}
	return v
}
