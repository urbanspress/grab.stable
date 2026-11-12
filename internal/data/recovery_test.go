package data

import (
	"context"
	"errors"
	"testing"

	rt "grab/internal/runtime"
)

func errorCode(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestMigrationRecoveryRealAdapters(t *testing.T) {
	for _, driver := range []string{"sqlite", "mariadb", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			_, s, p := testManager(t, driver)
			ctx := context.Background()
			// This fixture owns only these named tables/history entries. It never
			// removes the adapter conformance suite's version-one schema.
			if p.policy != nil {
				p.policy.Maintenance.Store(true)
				p.policy.Ledger.Store(true)
			}
			for _, sql := range []string{"DROP TABLE IF EXISTS grab07_recovery", "DROP TABLE IF EXISTS grab07_missing"} {
				if _, err := internalExec(ctx, p.db, sql); err != nil {
					t.Fatal(err)
				}
			}
			if p.policy != nil {
				p.policy.Maintenance.Store(false)
				p.policy.Ledger.Store(false)
			}
			priceType := "TEXT"
			if driver != "sqlite" {
				priceType = "DECIMAL(24,2)"
			}
			base := migrationValue(1, "create_products", "CREATE TABLE grab07_products (id BIGINT PRIMARY KEY, name TEXT NOT NULL, price "+priceType+" NOT NULL)")
			manifest := func(values ...rt.Value) []MigrationVersion {
				t.Helper()
				m, err := MigrationManifest(rt.List(values), driver)
				if err != nil {
					t.Fatal(err)
				}
				return m
			}
			initial := manifest(base)
			if _, err := s.Migrate(ctx, p, initial); err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name     string
				versions []MigrationVersion
			}{
				{"ahead", nil},
				{"changed_checksum", manifest(migrationValue(1, "create_products", "CREATE TABLE another_table (id INTEGER)"))},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if _, err := s.Migrate(ctx, p, tc.versions); errorCode(err) != Migration {
						t.Fatalf("expected history error: %v", err)
					}
				})
			}
			broken := manifest(base, migrationValue(2, "recovery_failure", "CREATE TABLE grab07_recovery (id INTEGER)", "ALTER TABLE grab07_missing ADD COLUMN value INTEGER"))
			if _, err := s.Migrate(ctx, p, broken); errorCode(err) != Migration {
				t.Fatalf("expected failed migration: %v", err)
			}
			var count int
			if err := p.db.QueryRow("SELECT count(version) FROM grab_schema_migrations WHERE version=2").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if driver == "mariadb" {
				if count != 1 {
					t.Fatal("nontransactional DDL did not leave durable dirty history")
				}
				if _, err := s.Migrate(ctx, p, broken); errorCode(err) != Migration {
					t.Fatal("dirty migration retried", err)
				}
				var dirty int
				if err := p.db.QueryRow("SELECT dirty FROM grab_schema_migrations WHERE version=2").Scan(&dirty); err != nil || dirty != 1 {
					t.Fatal("dirty marker lost", err)
				}
				if err := p.db.QueryRow("SELECT count(id) FROM grab07_recovery").Scan(&count); err != nil {
					t.Fatal("test did not exercise committed DDL", err)
				}
				// Explicit operator-style repair of this disposable test fixture.
				for _, sql := range []string{"DROP TABLE grab07_recovery", "DELETE FROM grab_schema_migrations WHERE version=2"} {
					if _, err := internalExec(ctx, p.db, sql); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				if count != 0 {
					t.Fatal("failed transactional migration retained ledger entry")
				}
				if err := p.db.QueryRow("SELECT count(id) FROM grab07_recovery").Scan(&count); err == nil {
					t.Fatal("failed transactional migration retained schema")
				}
			}
			if _, err := s.Migrate(ctx, p, initial); err != nil {
				t.Fatal("clean history did not recover", err)
			}
		})
	}
}

func TestQueryBudgetsReleaseCursors(t *testing.T) {
	for _, tc := range []struct {
		name, sql                      string
		rows, bytes, value, statements int
	}{
		{"rows", "SELECT 1 AS value UNION ALL SELECT 2 AS value", 1, 100, 100, 10},
		{"result_bytes", "SELECT 'abc' AS value", 10, 2, 100, 10},
		{"value_bytes", "SELECT 'abc' AS value", 10, 100, 2, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, s, p := testManager(t, "sqlite")
			p.def.Limits.MaxRows = &tc.rows
			p.def.Limits.MaxResultBytes = &tc.bytes
			p.def.Limits.MaxValueBytes = &tc.value
			typeName := "text"
			if tc.name == "rows" {
				typeName = "integer"
			}
			stmt, err := s.Prepare(context.Background(), p, tc.sql, Contract{Columns: []Field{{"value", TypeSpec{Type: typeName}}}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Query(context.Background(), stmt, rt.Record(nil), nil, false, false); errorCode(err) != Limit {
				t.Fatal("budget not enforced", err)
			}
			if p.db.Stats().InUse != 0 {
				t.Fatal("cursor leaked on budget failure")
			}
		})
	}
}

func TestRemoteDialRejectsUnconfiguredAndMetadataTargets(t *testing.T) {
	for _, host := range []string{"169.254.169.254", "0.0.0.0", "0.1.2.3", "255.255.255.255", "224.0.0.1", "::"} {
		d := definition{Host: host, Port: 5432}
		if _, err := pinnedDial(d)(context.Background(), "tcp", d.endpoint()); errorCode(err) != Denied {
			t.Fatalf("unsafe target %s: %v", host, err)
		}
	}
	d := definition{Host: "127.0.0.1", Port: 5432}
	if _, err := pinnedDial(d)(context.Background(), "tcp", "127.0.0.1:5433"); errorCode(err) != Denied {
		t.Fatal("endpoint changed", err)
	}
}
