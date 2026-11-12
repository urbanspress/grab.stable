package data

import (
	"reflect"
	"strings"
	"testing"
)

func TestScanSQL(t *testing.T) {
	for _, tt := range []struct {
		name, dialect, sql, want string
		names, bindings          []string
	}{
		{"sqlite repeated", "sqlite", "SELECT :id, :id, ':hidden' -- :comment\n;", "SELECT ?1, ?1, ':hidden' -- :comment\n;", []string{"id"}, []string{"id"}},
		{"postgres casts", "postgres", "SELECT :v::integer, :v, :other", "SELECT $1::integer, $1, $2", []string{"v", "other"}, []string{"v", "other"}},
		{"postgres quotes", "postgres", "SELECT $tag$:hidden;$tag$, $$:hidden$$, :v /* outer /* :inner */ end */", "SELECT $tag$:hidden;$tag$, $$:hidden$$, $1 /* outer /* :inner */ end */", []string{"v"}, []string{"v"}},
		{"mariadb repeated", "mariadb", "SELECT :a, :b, :a, `:hidden` # :comment", "SELECT ?, ?, ?, `:hidden` # :comment", []string{"a", "b"}, []string{"a", "b", "a"}},
		{"sqlite quotes", "sqlite", "SELECT ':hidden'';still quoted', [a:b], \"a:b\", :a", "SELECT ':hidden'';still quoted', [a:b], \"a:b\", ?1", []string{"a"}, []string{"a"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, e := ScanSQL(tt.sql, tt.dialect)
			if e != nil {
				t.Fatal(e)
			}
			if s.Text != tt.want || !reflect.DeepEqual(s.Parameters, tt.names) || !reflect.DeepEqual(s.Bindings, tt.bindings) {
				t.Fatalf("got %#v", s)
			}
		})
	}
}

func TestRejectInvalidSQL(t *testing.T) {
	for _, sql := range []string{"", "-- only comment", "SELECT 1; SELECT 2", "SELECT 1;;", "SELECT 'unterminated", "SELECT 1 /* unterminated", "SELECT ?", "SELECT @value", "SELECT $1", "SELECT :", "SELECT :9bad", "SELECT '\\'", "SELECT 1 /*! UNION SELECT 2 */", "SELECT 1 /*+ hint */", "SELECT 1\x00", "SELECT 1; /* comment */ SELECT 2", strings.Repeat("a", (1<<20)+1)} {
		t.Run(sql[:min(len(sql), 45)], func(t *testing.T) {
			if _, e := ScanSQL(sql, "sqlite"); e == nil {
				t.Fatal("accepted invalid SQL")
			}
		})
	}
	if _, e := ScanSQL("SELECT $tag$unterminated", "postgres"); e == nil {
		t.Fatal("accepted unterminated dollar quote")
	}
}
