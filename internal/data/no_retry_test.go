package data

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"testing"
)

type failingConnector struct{ executions, commits, connections int }

func (f *failingConnector) Connect(context.Context) (driver.Conn, error) {
	f.connections++
	return &failingConn{f}, nil
}
func (f *failingConnector) Driver() driver.Driver { return failingDriver{f} }

type failingDriver struct{ f *failingConnector }

func (f failingDriver) Open(string) (driver.Conn, error) { return f.f.Connect(context.Background()) }

type failingConn struct{ f *failingConnector }

func (f *failingConn) Prepare(string) (driver.Stmt, error) { return &failingStmt{f.f}, nil }
func (f *failingConn) Close() error                        { return nil }
func (f *failingConn) Begin() (driver.Tx, error)           { return &failingTx{f.f}, nil }

type failingStmt struct{ f *failingConnector }

func (f *failingConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) { return f.Begin() }

func (f *failingStmt) Close() error  { return nil }
func (f *failingStmt) NumInput() int { return 0 }
func (f *failingStmt) Exec([]driver.Value) (driver.Result, error) {
	f.f.executions++
	return nil, driver.ErrBadConn
}
func (f *failingStmt) Query([]driver.Value) (driver.Rows, error) {
	f.f.executions++
	return nil, driver.ErrBadConn
}

type failingTx struct{ f *failingConnector }

func (f *failingTx) Commit() error   { f.f.commits++; return driver.ErrBadConn }
func (f *failingTx) Rollback() error { return nil }

func TestBadConnectionIsNotRetried(t *testing.T) {
	for _, query := range []bool{false, true} {
		t.Run(map[bool]string{false: "write", true: "read"}[query], func(t *testing.T) {
			f := &failingConnector{}
			db := sql.OpenDB(noRetryConnector{f})
			defer db.Close()
			s, err := db.Prepare("fixture")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if query {
				_, err = s.Query()
			} else {
				_, err = s.Exec()
			}
			if errorCode(err) != Operation || f.executions != 1 {
				t.Fatalf("operation retried: attempts=%d err=%v", f.executions, err)
			}
			if db.Stats().OpenConnections != 0 {
				t.Fatal("broken physical connection was retained")
			}
		})
	}
}
func TestCommitUncertaintyHasOneAttempt(t *testing.T) {
	f := &failingConnector{}
	db := sql.OpenDB(noRetryConnector{f})
	m := NewManager(nil, false)
	defer m.Close()
	s := m.Scope()
	defer s.Close()
	p := &Pool{owner: m, db: db, mode: ReadWrite}
	m.pools["fixture"] = p
	tx, err := s.Begin(context.Background(), p, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Commit(tx); errorCode(err) != UnknownCommit || f.commits != 1 {
		t.Fatal("ambiguous commit retried or hidden", err, f.commits)
	}
	if err = s.Commit(tx); errorCode(err) != Ownership || f.commits != 1 {
		t.Fatal("closed transaction committed again", err)
	}
}
