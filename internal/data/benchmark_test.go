package data

import (
	"context"
	rt "grab/internal/runtime"
	"path/filepath"
	"testing"
)

func BenchmarkSQLitePreparedOne(b *testing.B) {
	c, err := ParseConfig([]byte(`{"connections":{"shop":{"driver":"sqlite","path":":memory:"}}}`), filepath.Join(b.TempDir(), "grab.data.json"))
	if err != nil {
		b.Fatal(err)
	}
	g, err := c.Grant([]string{"shop:read-write"}, false)
	if err != nil {
		b.Fatal(err)
	}
	m := NewManager(g, false)
	defer m.Close()
	s := m.Scope()
	defer s.Close()
	p, err := s.Open(context.Background(), "shop")
	if err != nil {
		b.Fatal(err)
	}
	q, err := s.Prepare(context.Background(), p, "SELECT :id AS id, :name AS name", Contract{Params: []Field{{"id", TypeSpec{Type: "integer"}}, {"name", TypeSpec{Type: "text"}}}, Columns: []Field{{"id", TypeSpec{Type: "integer"}}, {"name", TypeSpec{Type: "text"}}}})
	if err != nil {
		b.Fatal(err)
	}
	params := rt.Record([]rt.Entry{{Name: "id", Value: rt.IntegerFromInt64(42)}, {Name: "name", Value: rt.Text("Tea")}})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err = s.Query(context.Background(), q, params, nil, true, false); err != nil {
			b.Fatal(err)
		}
	}
}
