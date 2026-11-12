package data

import (
	"context"
	"database/sql/driver"
	"errors"
	rt "grab/internal/runtime"
	"testing"
)

type failedCommit struct{ rollbacks int }

func (t *failedCommit) Commit() error   { return errors.New("uncertain commit") }
func (t *failedCommit) Rollback() error { t.rollbacks++; return nil }
func TestFailedCommitDiscardsPhysicalConnection(t *testing.T) {
	raw := &failedCommit{}
	conn := &noRetryConn{}
	tx := &noRetryTx{Tx: raw, conn: conn}
	if tx.Commit() == nil || conn.IsValid() || raw.rollbacks != 1 {
		t.Fatal("failed commit reused connection", raw.rollbacks)
	}
	var _ driver.Tx = tx
}
func TestHostTransactionPreparationAndReservedTables(t *testing.T) {
	m, s, p := testManager(t, "sqlite")
	_ = m
	ctx := context.Background()
	versions, e := MigrationManifest(rt.List([]rt.Value{rt.Record([]rt.Entry{{Name: "version", Value: rt.IntegerFromInt64(1)}, {Name: "name", Value: rt.Text("host")}, {Name: "steps", Value: rt.List([]rt.Value{rt.Text("CREATE TABLE business (id INTEGER PRIMARY KEY)")})}})}), "sqlite")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Migrate(ctx, p, versions); e != nil {
		t.Fatal(e)
	}
	tx, e := s.Begin(ctx, p, false, "")
	if e != nil {
		t.Fatal(e)
	}
	contract := Contract{}
	if _, e = s.Prepare(ctx, p, "SELECT 1", contract); e == nil {
		t.Fatal("ordinary transaction preparation changed")
	}
	if e = s.HostOwnTransaction(tx); e != nil {
		t.Fatal(e)
	}
	st, e := s.Prepare(ctx, p, "INSERT INTO business(id) VALUES(1)", contract)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Query(ctx, st, rt.Record(nil), tx, false, true); e != nil {
		t.Fatal(e)
	}
	if e = s.Commit(tx); e != nil {
		t.Fatal(e)
	}
	for _, source := range []string{"UPDATE grab_events_work SET state='completed'", "DELETE FROM grab_events_budget"} {
		sql, e := ScanSQL(source, "sqlite")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = ValidateStatement(sql); e == nil {
			t.Fatal("host ledger write allowed", source)
		}
	}
}
