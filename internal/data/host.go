package data

import (
	"context"
	"database/sql"
	"strings"
)

func (s *Scope) HostMaintenanceDatabase(p *Pool) (*sql.DB, error) {
	if e := s.valid(p); e != nil {
		return nil, e
	}
	if s.manager.server || p.mode != Migrate {
		return nil, fail(Denied, "event maintenance requires migrate permission")
	}
	return p.db, nil
}

// HostDatabase is available only to host adapters, never as a language value.
// The existing database grant and generation ownership still apply.
func (s *Scope) HostDatabase(p *Pool) (*sql.DB, error) {
	if e := s.valid(p); e != nil {
		return nil, e
	}
	if p.mode != ReadWrite {
		return nil, fail(Denied, "event stores require database read-write permission")
	}
	return p.db, nil
}
func (s *Scope) HostTransaction(t *Transaction, p *Pool) (*sql.Tx, error) {
	if e := s.valid(p); e != nil {
		return nil, e
	}
	if e := s.transaction(t, p); e != nil {
		return nil, e
	}
	if t.readOnly {
		return nil, fail(Denied, "event work requires a write transaction")
	}
	return t.tx, nil
}

// PoisonTransaction ensures a caught host enqueue failure cannot commit partial
// admission accounting or business changes.
func (s *Scope) PoisonTransaction(t *Transaction) {
	if t != nil && t.scope == s {
		t.rollbackOnly = true
	}
}

type migrationExecutor struct {
	sqlExecutor
	ledger string
}

func (m migrationExecutor) PrepareContext(ctx context.Context, source string) (*sql.Stmt, error) {
	return m.sqlExecutor.PrepareContext(ctx, strings.ReplaceAll(source, "grab_schema_migrations", m.ledger))
}

// MigrateEvents keeps event history independent of user migration numbering.
func (s *Scope) MigrateEvents(ctx context.Context, p *Pool, steps []string) error {
	v := MigrationVersion{Version: 1, Name: "grab_events_v1", Steps: steps}
	v.Checksum = migrationChecksum(v)
	_, e := s.migrate(ctx, p, []MigrationVersion{v}, "grab_events_migrations")
	return e
}

func (s *Scope) HostOwnTransaction(t *Transaction) error {
	if e := s.transaction(t, t.pool); e != nil {
		return e
	}
	s.hostOwned = true
	return nil
}

func EventMigrationChecksum(steps []string) string {
	return migrationChecksum(MigrationVersion{Version: 1, Name: "grab_events_v1", Steps: steps})
}
