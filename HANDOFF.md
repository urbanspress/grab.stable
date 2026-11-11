# GRAB Tandem Handoff

- **Current Owner:** Google Antigravity
- **Last Completed:** OpenAI Codex completed the GRAB 0.1 specification conformance milestone on November 11, 2026. Read the full approved specification; added table-driven executable and pipeline tests in `tests/conformance_test.go`, exact AST span/association checks in `tests/structure_test.go`, and a section-by-section coverage map in `tests/README.md`. Fixed raw carriage returns being accepted inside text and narrowed unterminated-text diagnostics to the opening quote. Regression coverage includes lexical preflight suppressing all output. The full `go test ./...` suite passes 100%; `go build ./...` and `git diff --check` succeed.
- **Next Pending Task:** Begin GRAB 0.2 pre-implementation design: settle the approved roadmap's open `.grab.html` interpolation grammar, lookup/reuse rules, value binding, context-aware escaping, output/diagnostics, and minimum localhost-server lifecycle/security requirements. Update the relevant specification/ADR before implementing the server. No production server, browser compiler, React integration, or general UI framework is authorized by this milestone.

## Completion notes

- Codex verified the passing baseline and took ownership before implementation, then transferred ownership back under the tandem protocol after completion.
- The suite checks every specified behavior family, all 256 byte values, exact output, pre-execution versus runtime error boundaries, and complete diagnostic spans. It is not an exhaustive proof over every possible source program.
- The specification does not explicitly settle float overflow/non-finite results or underflow policy. Existing runtime choices are recorded in `tests/README.md`, not promoted to new approved language rules.
- The local GRAB 0.1 checkpoint commit includes all reviewed working-tree changes, including pre-existing negative-number lexer fixes, CLI tests, examples, roadmap, specification and architecture edits. No push, deployment, VM, server, UI or CMS work was performed.
- Go is installed at `C:\Program Files\Go\bin\go.exe` but was absent from this terminal's PATH. Commands were run using that executable. The conformance harness locates the Go tool from `runtime.GOROOT()` and needs no PATH adjustment or external test dependencies.
