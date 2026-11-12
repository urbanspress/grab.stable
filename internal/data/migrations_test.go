package data

import (
	"testing"

	rt "grab/internal/runtime"
)

func migrationValue(version int64, name string, steps ...string) rt.Value {
	var list []rt.Value
	for _, s := range steps {
		list = append(list, rt.Text(s))
	}
	return rt.Record([]rt.Entry{{Name: "version", Value: rt.IntegerFromInt64(version)}, {Name: "name", Value: rt.Text(name)}, {Name: "steps", Value: rt.List(list)}})
}

func TestMigrationManifest(t *testing.T) {
	for _, tt := range []struct {
		name  string
		items []rt.Value
		valid bool
	}{
		{"one", []rt.Value{migrationValue(1, "create_products", "CREATE TABLE products (id INTEGER PRIMARY KEY)")}, true},
		{"gap", []rt.Value{migrationValue(2, "create_products", "CREATE TABLE products (id INTEGER PRIMARY KEY)")}, false},
		{"duplicate name", []rt.Value{migrationValue(1, "create_products", "CREATE TABLE products (id INTEGER)"), migrationValue(2, "create_products", "CREATE INDEX ix ON products(id)")}, false},
		{"seed DML", []rt.Value{migrationValue(1, "seed", "INSERT INTO products VALUES (1)")}, false},
		{"parameter", []rt.Value{migrationValue(1, "schema", "CREATE TABLE products (id INTEGER DEFAULT :id)")}, false},
		{"multiple statements", []rt.Value{migrationValue(1, "schema", "CREATE TABLE products(id INTEGER); DROP TABLE other")}, false},
		{"create as select", []rt.Value{migrationValue(1, "schema", "CREATE TABLE products AS SELECT 1")}, false},
		{"ledger reserved", []rt.Value{migrationValue(1, "schema", "DROP TABLE grab_schema_migrations")}, false},
		{"function default", []rt.Value{migrationValue(1, "schema", "CREATE TABLE products(id INTEGER DEFAULT (random()))")}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, e := MigrationManifest(rt.List(tt.items), "sqlite")
			if (e == nil) != tt.valid {
				t.Fatalf("valid=%v error=%v", tt.valid, e)
			}
		})
	}
}

func TestMigrationChecksum(t *testing.T) {
	one, e := MigrationManifest(rt.List([]rt.Value{migrationValue(1, "schema", "CREATE TABLE products(\r\nid INTEGER)")}), "sqlite")
	if e != nil {
		t.Fatal(e)
	}
	two, e := MigrationManifest(rt.List([]rt.Value{migrationValue(1, "schema", "CREATE TABLE products(\nid INTEGER)")}), "sqlite")
	if e != nil {
		t.Fatal(e)
	}
	if one[0].Checksum != two[0].Checksum {
		t.Fatal("LF normalization changed checksum")
	}
	if one[0].Checksum == migrationChecksum(MigrationVersion{Version: 1, Name: "schema", Steps: []string{"CREATE TABLE products(\nid INTEGER) "}}) {
		t.Fatal("exact SQL change not detected")
	}
	if migrationChecksum(MigrationVersion{Version: 1, Name: "ab", Steps: []string{"c"}}) == migrationChecksum(MigrationVersion{Version: 1, Name: "a", Steps: []string{"bc"}}) {
		t.Fatal("checksum components lack length delimiters")
	}
}
