package tests

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	"grab/internal/data"
	"grab/internal/interpreter"
	"grab/internal/parser"
)

func grab07Code(e error) string {
	var ie *interpreter.Error
	if errors.As(e, &ie) {
		return ie.Kind.Code()
	}
	return ""
}

func grab07Options(t *testing.T) interpreter.Options {
	t.Helper()
	c, e := data.ParseConfig([]byte(`{"connections":{"shop":{"driver":"sqlite","path":":memory:"}}}`), filepath.Join(t.TempDir(), "grab.data.json"))
	if e != nil {
		t.Fatal(e)
	}
	g, e := c.Grant([]string{"shop:migrate"}, false)
	if e != nil {
		t.Fatal(e)
	}
	return interpreter.Options{DataGrants: g}
}

const grab07Setup = `use data
set shop to data.open using "shop"
call data.migrate using shop, [record {version: 1, name: "create_products", steps: ["CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT NOT NULL, price TEXT NOT NULL)"]}]
set insert to data.prepare using shop, "INSERT INTO products(id,name,price) VALUES (:id,:name,:price)", record {params: record {id: "integer", name: "text", price: record {type: "decimal", precision: 18, scale: 2, storage: "text"}}, columns: record {}}
set find to data.prepare using shop, "SELECT id, name, price FROM products WHERE id = :id", record {params: record {id: "integer"}, columns: record {id: "integer", name: "text", price: record {type: "decimal", precision: 18, scale: 2, storage: "text"}}}
`

func TestGRAB07Programs(t *testing.T) {
	for _, tt := range []struct{ name, source, want, code string }{
		{"money and prepared values", grab07Setup + `call data.execute using insert, record {id: 1, name: "Tea'); DROP TABLE products; --", price: 1234567890123456.78}
set product to data.one using find, record {id: 1}
show product.name
show product.price
`, "Tea'); DROP TABLE products; --\n1234567890123456.78\n", ""},
		{"explicit rollback", grab07Setup + `set tx to data.begin using shop
call data.execute using insert, record {id: 1, name: "Tea", price: 12.50}, tx
call data.rollback using tx
show data.one using find, record {id: 1}
`, "nothing\n", ""},
		{"block rollback", grab07Setup + `if yes
  set tx to data.begin using shop
  call data.execute using insert, record {id: 1, name: "Tea", price: 12.50}, tx
end
show data.one using find, record {id: 1}
`, "nothing\n", ""},
		{"missing transaction argument", grab07Setup + `set tx to data.begin using shop
call data.execute using insert, record {id: 1, name: "Tea", price: 12.50}
`, "", "GRAB-E315"},
		{"reject rounded money", grab07Setup + `call data.execute using insert, record {id: 1, name: "Tea", price: 12.501}
`, "", "GRAB-E314"},
		{"opaque show", grab07Setup + `show [shop]
`, "", "GRAB-E301"},
		{"opaque comparison", grab07Setup + `show shop is shop
`, "", "GRAB-E301"},
		{"opaque JSON", grab07Setup + `use json
show json.stringify using record {handle: shop}
`, "", "GRAB-E309"},
		{"task group inside transaction", grab07Setup + `set tx to data.begin using shop
do together
  set row to wait for (data.one using find, record {id: 1}, tx)
end
`, "", "GRAB-E315"},
		{"task transaction ownership", grab07Setup + `do together
  set tx to wait for (data.begin using shop)
end
call data.commit using tx
`, "", "GRAB-E315"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p, e := parser.Parse("test.grab", tt.source)
			if e != nil {
				t.Fatal(e)
			}
			formatted, e := parser.Format("test.grab", tt.source)
			if e != nil {
				t.Fatal(e)
			}
			again, e := parser.Format("formatted.grab", formatted)
			if e != nil || formatted != again {
				t.Fatal("database program formatting is unstable", e)
			}
			var output bytes.Buffer
			e = interpreter.RunWithOptions(p, &output, grab07Options(t))
			if tt.code != "" {
				if e == nil || grab07Code(e) != tt.code {
					t.Fatalf("want %s, got %v", tt.code, e)
				}
			} else if e != nil {
				t.Fatal(e)
			}
			if output.String() != tt.want {
				t.Fatalf("output=%q want=%q", output.String(), tt.want)
			}
		})
	}
}

func TestGRAB07StaticChecks(t *testing.T) {
	for _, source := range []string{
		`use data
set alias to "shop"
set db to data.open using alias`,
		`use data
set db to data.open using "shop"
set sql to "SELECT 1 AS n"
set q to data.prepare using db, sql, record {params: record {}, columns: record {n: "integer"}}`,
		`use data
set db to data.open using "shop"
set q to data.prepare using db, "SELECT * FROM products", record {params: record {}, columns: record {}}`,
		`use data
set db to data.open using "shop"
set q to data.prepare using db, "SELECT :id AS id; DELETE FROM products", record {params: record {id: "integer"}, columns: record {id: "integer"}}`,
	} {
		t.Run(source, func(t *testing.T) {
			p, e := parser.Parse("test.grab", source)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = interpreter.Validate(p); e == nil || grab07Code(e) != "GRAB-E210" {
				t.Fatalf("static check: %v", e)
			}
		})
	}
	p, e := parser.Parse("test.grab", "use data\nset db to data.open using \"shop\"\n")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = interpreter.Validate(p); e != nil {
		t.Fatal(e)
	}
	if e = interpreter.Run(p, &bytes.Buffer{}); e == nil || grab07Code(e) != "GRAB-E403" {
		t.Fatalf("default deny: %v", e)
	}
}
