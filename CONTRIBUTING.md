# Contributing to GRAB

Thank you for your interest in contributing to **GRAB**! GRAB is a Go-implemented, general-purpose programming language and persistent web application server designed for simplicity, deterministic performance, and default-deny security.

---

## Architectural Principles

Before contributing, please review our core architectural guidelines:
1. **Deterministic Semantics:** GRAB avoids implicit type coercion, silent arithmetic overflow, or floating-point rounding errors for currency. Exact integers and arbitrary-precision decimals are native first-class types.
2. **Security by Default:** Capabilities (filesystem, outbound network, database, cache, channels, telemetry) are default-deny. Features must not bypass security checks.
3. **AST-First Runtime:** The core runtime evaluates a validated Abstract Syntax Tree with isolated call frames and pre-execution validation before bytecode/VM compilation.
4. **Clean Conformance & Test Coverage:** No PR or commit is accepted without automated test coverage and a 100% green test suite.

---

## Development Setup

### Prerequisites
- **Go 1.26 or newer** on your system `PATH`.
- **Git** (version 2.30+ recommended).
- Optional: **Docker Desktop** (for running real Redis, MariaDB, and PostgreSQL test vectors).

### Build & Run
From the repository root:

```sh
# Clone the repository
git clone https://github.com/urbanspress/grab.git
cd grab

# Build all Go binaries and packages
go build ./...

# Run the root hello script
go run ./cmd/grab run index.grab

# Launch the persistent web server preview
go run ./cmd/grab serve examples/landing.grab

# Format code canonically
go run ./cmd/grab format --write examples/landing.grab
```

---

## Mandatory Testing Gate

GRAB enforces a strict **100% passing test rule**:
- All bug fixes and language features must include table-driven automated tests.
- Run the full test suite before submitting changes:
  ```sh
  go test ./...
  ```
- All tests must pass with zero failures.

---

## Code Style & Formatting

- **Go Code:** Follow standard idiomatic Go practices (`gofmt`, `go vet`).
- **GRAB Scripts (`.grab`):** Use the canonical formatter:
  ```sh
  go run ./cmd/grab format --write <path/to/script.grab>
  ```
- **Error Codes:** Use stable `GRAB-E...` error codes for runtime and lexical/syntax errors:
  - `GRAB-E001`–`GRAB-E007`: Lexical errors
  - `GRAB-E101`–`GRAB-E103`: Syntax/Parser errors
  - `GRAB-E201`–`GRAB-E209`: Pre-execution validation errors
  - `GRAB-E301`–`GRAB-E312`: Runtime execution errors
  - `GRAB-E401`–`GRAB-E404`: Capability and security errors

---

## Submitting Pull Requests

1. **Create a Topic Branch:**
   ```sh
   git checkout -b feature/my-new-feature
   ```
2. **Commit Your Changes:**
   Write clear, descriptive commit messages summarizing the user-facing benefit or fix.
3. **Run the Full Test Suite:**
   ```sh
   go test ./...
   ```
4. **Push & Open a Pull Request:**
   Submit your pull request against the `main` branch. Maintainers will review your contribution against the milestone specifications.
