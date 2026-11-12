package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDatabaseCLI(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "grab.data.json")
	program := filepath.Join(dir, "query.grab")
	if e := os.WriteFile(manifest, []byte(`{"connections":{"shop":{"driver":"sqlite","path":":memory:"}}}`), 0600); e != nil {
		t.Fatal(e)
	}
	source := "use data\nset db to data.open using \"shop\"\nset q to data.prepare using db, \"SELECT :n AS n\", record {params: record {n: \"integer\"}, columns: record {n: \"integer\"}}\nset row to data.one using q, record {n: 42}\nshow row.n\n"
	if e := os.WriteFile(program, []byte(source), 0600); e != nil {
		t.Fatal(e)
	}
	for _, tt := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"run", []string{"run", "--db-config", manifest, "--allow-db", "shop:read", program}, 0, "42\n"},
		{"default deny", []string{"run", program}, 1, "GRAB-E403"},
		{"file grants do not grant SQL", []string{"run", "--allow-read", dir, "--allow-write", dir, program}, 1, "GRAB-E403"},
		{"missing manifest", []string{"run", "--allow-db", "shop:read", program}, 1, "GRAB-E403"},
		{"unknown alias", []string{"run", "--db-config", manifest, "--allow-db", "unknown:read", program}, 1, "GRAB-E403"},
		{"conflicting grant", []string{"run", "--db-config", manifest, "--allow-db", "shop:read", "--allow-db", "shop:read-write", program}, 1, "GRAB-E403"},
		{"server migrate forbidden", []string{"serve", "--db-config", manifest, "--allow-db", "shop:migrate", program}, 1, "GRAB-E403"},
		{"check without database IO", []string{"check", "--db-config", filepath.Join(dir, "does_not_exist.json"), program}, 0, ""},
		{"format without database IO", []string{"format", program}, 0, "use data"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var out, err bytes.Buffer
			code := run(tt.args, &out, &err)
			if code != tt.code {
				t.Fatalf("exit %d, stderr %s", code, err.String())
			}
			combined := out.String() + err.String()
			if !strings.Contains(combined, tt.want) {
				t.Fatalf("want %q in %q", tt.want, combined)
			}
		})
	}
	if e := os.WriteFile(program, []byte("use data\nset db to data.open using \"shop\"\nshow data.migrate using db, [record {version: 1, name: \"create_items\", steps: [\"CREATE TABLE items(id INTEGER PRIMARY KEY)\"]}]\n"), 0600); e != nil {
		t.Fatal(e)
	}
	var out, err bytes.Buffer
	if code := run([]string{"migrate", "--db-config", manifest, "--allow-db", "shop:migrate", program}, &out, &err); code != 0 || !strings.Contains(out.String(), "current_version: 1") {
		t.Fatalf("migrate: %d %s %s", code, &out, &err)
	}
}
