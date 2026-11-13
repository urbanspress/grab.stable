package server

import (
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"grab/internal/interpreter"
)

// Run: go test ./internal/server -run '^$' -bench . -benchmem
func BenchmarkPreparedApp(b *testing.B) {
	engines := []struct {
		label string
		mode  interpreter.EngineMode
	}{
		{"AST", interpreter.EngineAST},
		{"VM", interpreter.EngineVM},
		{"Auto", interpreter.EngineAuto},
	}
	for _, tc := range []struct{ name, source, path string }{
		{"text", "route \"GET\" \"/\"\ngive back \"hello\"\nend\n", "/"},
		{"json", "use server\nroute \"GET\" \"/items/{id}\" using req\ngive back server.json using 200, record {id: req.params.id, price: 19.95}\nend\n", "/items/A17"},
		{"html", "use server\nroute \"GET\" \"/\"\ngive back server.html using \"view.grab.html\", record {title: \"Hello\"}\nend\n", "/"},
		{"compute_json", "use server\nmake sum_to using n\n  set acc to 0\n  set i to 0\n  while i is less than n\n    set acc to acc plus i\n    set i to i plus 1\n  end\n  give back acc\nend\nroute \"GET\" \"/calc/{n}\" using req\n  set total to sum_to using 200\n  give back server.json using 200, record {id: req.params.n, total: total}\nend\n", "/calc/200"},
	} {
		for _, eng := range engines {
			b.Run(tc.name+"/"+eng.label, func(b *testing.B) {
				path := appFile(b, tc.source)
				if e := os.WriteFile(filepath.Join(filepath.Dir(path), "view.grab.html"), []byte("<h1>{title}</h1>"), 0600); e != nil {
					b.Fatal(e)
				}
				a := application(b, path, DefaultConfig(), io.Discard)
				a.Close()
				var err error
				a, err = NewApplication(path, "", interpreter.Options{Engine: eng.mode}, DefaultConfig(), io.Discard)
				if err != nil {
					b.Fatal(err)
				}
				defer a.Close()
				r := httptest.NewRequest("GET", tc.path, nil)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					a.ServeHTTP(httptest.NewRecorder(), r)
				}
			})
		}
	}
}

func BenchmarkRouteMatch(b *testing.B) {
	a := application(b, appFile(b, "route \"GET\" \"/items/{id}\"\ngive back \"ok\"\nend\n"), DefaultConfig(), nil)
	route := a.active.Load().code.Routes()[0]
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = route.Match("/items/A17")
	}
}
