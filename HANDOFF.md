# GRAB Tandem Handoff

- **Current Owner:** Google Antigravity
- **Last Completed:**
  - Completed **GRAB 0.3 — Control flow, functions, and richer language** implementation and conformance suite on November 11, 2026 per `specifications/GRAB-LANGUAGE-SPEC-003.md`:
    - Extended `internal/lexer`, `internal/ast`, `internal/parser`, `internal/runtime`, and `internal/interpreter` with arithmetic (`plus`, `minus`, `times`, `divided by`, `modulo`), comparisons (`is`, `is not`, `[is] greater than`, `[is] less than`, `[is] at least`, `[is] at most`), Boolean logic (`not`, `and`, `or`), collection lookup (`.field`, `at`, `has`), control flow (`if`/`otherwise if`/`otherwise`/`end`, `repeat <count> times`, `for each <item> in <coll>`, `while <cond>` with 1,000,000-step guard), and top-level functions (`make`/`using`/`give back`/`call` with isolated call frames, pre-execution arity checking, and 256-frame recursion limit).
    - Added Go performance and allocation benchmarks in `internal/interpreter/benchmark_test.go` (`ADR-020f`).
    - Added end-to-end GRAB 0.3 conformance tests in `tests/grab03_conformance_test.go` and new runnable sample programs (`examples/pricing.grab`, `examples/inventory.grab`, and updated `examples/product.grab`).
    - Verified `go test ./...`, `go build ./...`, and `git diff --check` pass 100%.
- **Next Pending Task:** Await project-owner approval to begin pre-implementation specification design for **GRAB 0.4 — Core standard library, error model, and permissions** (`docs/ROADMAP.md`). Do not implement GRAB 0.4–1.0 features until explicitly approved.
