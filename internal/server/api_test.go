package server

import (
	"encoding/json"
	"grab/internal/interpreter"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTypedAPIBoundary(t *testing.T) {
	contract := filepath.Join(t.TempDir(), "grab.api.json")
	b, e := os.ReadFile("../../tests/fixtures/typed-client/grab.api.json")
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(contract, b, 0600)
	path := appFile(t, `use api
route "GET" "/products/{id}" using request
 set input to api.input using request, "product"
 give back api.respond using "product", 200, record {id: input.path.id, price: 19.90, name: "Book", bytes: b"\x00\x01"}
end
route "POST" "/products" using request
 set input to api.input using request, "createProduct"
 give back api.respond using "createProduct", 201, input.body
end
`)
	a, e := NewApplication(path, "", interpreter.Options{APIContractPath: contract}, DefaultConfig(), io.Discard)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	for _, tc := range []struct {
		name, path string
		status     int
	}{{"exact", "/products/90071992547409931234567890", 200}, {"duplicate scalar", "/products/1?search=x&search=y", 400}, {"unknown query", "/products/1?unexpected=x", 400}, {"coercion", "/products/01", 400}} {
		t.Run(tc.name, func(t *testing.T) {
			w := request(a, "GET", tc.path, "")
			if w.Code != tc.status {
				t.Fatal(w.Code, w.Body.String())
			}
			if w.Code == 200 && !strings.Contains(w.Body.String(), `"90071992547409931234567890"`) {
				t.Fatal(w.Body.String())
			}
			if w.Code == 400 {
				var envelope map[string]any
				if json.Unmarshal(w.Body.Bytes(), &envelope) != nil || envelope["error"] == nil {
					t.Fatal(w.Body.String())
				}
			}
		})
	}
}
func TestContractResponseSanitizedAndRouteMismatch(t *testing.T) {
	contract := filepath.Join(t.TempDir(), "grab.api.json")
	os.WriteFile(contract, []byte(`{"version":1,"types":{},"operations":[{"id":"private","method":"GET","path":"/","success":[{"status":200,"schema":{"type":"integer"}}]}]}`), 0600)
	for _, body := range []string{`give back "secret response"`, `give back api.respond using "private", 200, "secret response"`} {
		path := appFile(t, "use api\nroute \"GET\" \"/\"\n"+body+"\nend\n")
		a, e := NewApplication(path, "", interpreter.Options{APIContractPath: contract}, DefaultConfig(), io.Discard)
		if e != nil {
			t.Fatal(e)
		}
		w := request(a, "GET", "/", "")
		a.Close()
		if w.Code != 500 || strings.Contains(w.Body.String(), "secret response") || !strings.Contains(w.Body.String(), "request_id") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	path := appFile(t, "route \"GET\" \"/other\"\ngive back \"ok\"\nend\n")
	a, e := NewApplication(path, "", interpreter.Options{APIContractPath: contract}, DefaultConfig(), io.Discard)
	if e == nil {
		a.Close()
		t.Fatal("unmatched operation accepted")
	}
}
