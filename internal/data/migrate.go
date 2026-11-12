package data

import (
	"context"
	"database/sql"
	"time"

	rt "grab/internal/runtime"
)

type sqlExecutor interface {
	PrepareContext(context.Context, string) (*sql.Stmt, error)
}

func internalExec(ctx context.Context, executor sqlExecutor, source string, args ...any) (sql.Result, error) {
	stmt, e := executor.PrepareContext(ctx, source)
	if e != nil {
		return nil, e
	}
	defer stmt.Close()
	return stmt.ExecContext(ctx, args...)
}

// Migrate holds an engine migration lock and uses only the already validated
// literal manifest. MariaDB dirty entries are durable before nontransactional DDL.
func (s *Scope) Migrate(ctx context.Context, p *Pool, versions []MigrationVersion) (rt.Value, error) {
	if e := s.valid(p); e != nil {
		return rt.Value{}, e
	}
	if s.manager.server || p.mode != Migrate {
		return rt.Value{}, fail(Denied, "migrations require a maintenance execution and migrate grant")
	}
	if s.tx != nil {
		return rt.Value{}, fail(Ownership, "migrations cannot run inside a user transaction")
	}
	op, cancel := bounded(ctx, p)
	defer cancel()
	conn, e := p.db.Conn(op)
	if e != nil {
		return rt.Value{}, safeError(op, e)
	}
	defer conn.Close()
	if p.policy != nil {
		p.policy.Maintenance.Store(true)
		p.policy.Ledger.Store(true)
		defer func() { p.policy.Ledger.Store(false); p.policy.Maintenance.Store(false) }()
	}
	var executor sqlExecutor = conn
	var tx *sql.Tx
	if p.def.Driver != "mariadb" {
		tx, e = conn.BeginTx(op, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if e != nil {
			return rt.Value{}, safeError(op, e)
		}
		defer tx.Rollback()
		executor = tx
		if p.def.Driver == "postgres" {
			if _, e = internalExec(op, tx, "SELECT pg_advisory_xact_lock(71832197007)"); e != nil {
				return rt.Value{}, fail(Migration, "migration lock unavailable")
			}
		}
	} else {
		stmt, e := conn.PrepareContext(op, "SELECT GET_LOCK(CONCAT('grab_schema_',DATABASE()), 5)")
		if e != nil {
			return rt.Value{}, fail(Migration, "migration lock unavailable")
		}
		var locked sql.NullInt64
		e = stmt.QueryRowContext(op).Scan(&locked)
		_ = stmt.Close()
		if e != nil || !locked.Valid || locked.Int64 != 1 {
			return rt.Value{}, fail(Migration, "migration lock unavailable")
		}
		defer func() {
			cleanup, c := context.WithTimeout(context.Background(), time.Second)
			defer c()
			_, _ = internalExec(cleanup, conn, "SELECT RELEASE_LOCK(CONCAT('grab_schema_',DATABASE()))")
		}()
	}
	ledger := "CREATE TABLE IF NOT EXISTS grab_schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL, applied_at TEXT NOT NULL, dirty INTEGER NOT NULL)"
	if p.def.Driver == "mariadb" {
		ledger = "CREATE TABLE IF NOT EXISTS grab_schema_migrations (version INTEGER PRIMARY KEY, name VARCHAR(255) NOT NULL, checksum VARCHAR(64) NOT NULL, applied_at VARCHAR(40) NOT NULL, dirty INTEGER NOT NULL)"
	}
	if _, e = internalExec(op, executor, ledger); e != nil {
		return rt.Value{}, fail(Migration, "migration ledger unavailable")
	}
	stmt, e := executor.PrepareContext(op, "SELECT version, name, checksum, dirty FROM grab_schema_migrations ORDER BY version")
	if e != nil {
		return rt.Value{}, fail(Migration, "migration history unavailable")
	}
	rows, e := stmt.QueryContext(op)
	if e != nil {
		_ = stmt.Close()
		return rt.Value{}, fail(Migration, "migration history unavailable")
	}
	current := 0
	for rows.Next() {
		var version, dirty int
		var name, checksum string
		if e = rows.Scan(&version, &name, &checksum, &dirty); e != nil {
			break
		}
		if dirty != 0 || version != current+1 || version > len(versions) || versions[version-1].Name != name || versions[version-1].Checksum != checksum {
			e = fail(Migration, "migration history is changed, incomplete, ahead or dirty; operator review is required")
			break
		}
		current = version
	}
	if e == nil {
		e = rows.Err()
	}
	_ = rows.Close()
	_ = stmt.Close()
	if e != nil {
		return rt.Value{}, fail(Migration, "migration history is changed, incomplete, ahead or dirty; operator review is required")
	}
	var applied []rt.Value
	for _, version := range versions[current:] {
		insert := "INSERT INTO grab_schema_migrations(version,name,checksum,applied_at,dirty) VALUES(?,?,?,?,?)"
		if p.def.Driver == "postgres" {
			insert = "INSERT INTO grab_schema_migrations(version,name,checksum,applied_at,dirty) VALUES($1,$2,$3,$4,$5)"
		}
		dirty := 0
		if p.def.Driver == "mariadb" {
			dirty = 1
		}
		if _, e = internalExec(op, executor, insert, version.Version, version.Name, version.Checksum, time.Now().UTC().Format(time.RFC3339Nano), dirty); e != nil {
			return rt.Value{}, fail(Migration, "migration version could not be recorded")
		}
		for _, step := range version.Steps {
			if _, e = internalExec(op, executor, step); e != nil {
				return rt.Value{}, fail(Migration, "migration failed; inspect schema and history before any repair or restore")
			}
		}
		if dirty == 1 {
			if _, e = internalExec(op, executor, "UPDATE grab_schema_migrations SET dirty=0 WHERE version=?", version.Version); e != nil {
				return rt.Value{}, fail(Migration, "migration remains dirty; operator review is required")
			}
		}
		applied = append(applied, rt.IntegerFromInt64(int64(version.Version)))
		current = version.Version
	}
	if tx != nil {
		if e = tx.Commit(); e != nil {
			return rt.Value{}, fail(UnknownCommit, "migration commit outcome unknown; operator review is required")
		}
	}
	return rt.Record([]rt.Entry{{Name: "applied", Value: rt.List(applied)}, {Name: "current_version", Value: rt.IntegerFromInt64(int64(current))}}), nil
}
