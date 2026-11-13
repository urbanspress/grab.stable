# GRAB 0.9 — Profile-driven IR and bytecode VM

**Status:** Complete (Approved & Implemented November 12, 2026; verified by `go test -count=1 ./...`)  
**Version:** 1.0 — November 12, 2026 (Approved & Implemented November 12, 2026)  
**Current Owner:** Google Antigravity  
**Implementation record:** The project owner approved this specification and authorized GRAB 0.9 implementation under **Google Antigravity** on November 12, 2026. Implemented in `internal/ir`, `internal/vm`, `internal/interpreter`, `internal/runtime`, `internal/server`, `cmd/grab`, and verified in `tests/grab09_vm_equivalence_test.go`.

---

## 1. Authority, scope, and compatibility

### 1.1 Architectural authority
This specification refines and implements the GRAB 0.9 target defined in:
- **Blueprint v1.0, Section 4** (*Language engine and execution model — Bytecode VM, Semantic compatibility*), **Section 12** (*GRAB 0.9 — Bytecode VM*), and **Section 13** (*Conformance suite, Benchmarking, Performance discipline*).
- **Blueprint v1.0 Corrected Edition** (repository layout under `internal/`).
- **ADR-003** (*Initial execution model*: AST interpreter first; bytecode VM later; native machine-code compilation deferred).
- **ADR-011** (*Prepared applications and request isolation*).
- **ADR-012** (*Parse-on-load and bytecode caching*).
- **ADR-013** (*Optional intermediate representation*: IR driven by profiling and stabilized semantics, preserving 100% interpreter conformance).
- **ADR-016** (*Evidence-based performance goals*).

### 1.2 Included scope
- **Profile-driven compilation tiering (`auto` mode):** Keep cold top-level startup scripts and infrequently executed code on the existing AST interpreter (`internal/interpreter`), while lowering hot functions, loops, and prepared server routes into a compact intermediate representation (`internal/ir`) and stack-based bytecode (`internal/vm`).
- **Explicit engine selection flags (`--engine=auto|ast|vm`):**
  - `auto` (default): Profile-driven hybrid execution.
  - `ast`: Pure AST reference interpreter (semantic oracle and debugging baseline).
  - `vm`: Eager compilation of all validated functions, routes, and top-level units to bytecode before execution (used for deterministic conformance parity, differential testing, and VM benchmarks).
- **Lexical slot resolution & structured exception tables:** Eliminate per-access string-map variable lookups in hot code and represent `try` / `catch` regions via deterministic PC-range handler tables.
- **Bytecode verifier:** Fail-closed static verification of every compiled bytecode chunk before execution.
- **100% semantic equivalence & safety parity:** Identical `runtime.Value` semantics, error codes (`GRAB-E...`), source spans, diagnostic modes, `1,000,000`-step loop guard, `256`-frame call depth guard, `do together` deterministic ordering, and capability enforcement.
- **Benchmark scorecard & regression suite:** Reproducible `AST` vs `VM` benchmarks extending `internal/interpreter/benchmark_test.go` and `internal/server/benchmark_test.go`.

### 1.3 Preserved invariants & exclusions
- **Zero syntax or grammar changes:** GRAB 0.9 introduces no new keywords, no new operators, and no changes to `.grab` or `.grab.html` syntax.
- **Zero capability or host-adapter changes:** All 14 standard library modules (`text`, `math`, `collections`, `dates`, `files`, `json`, `http`, `server`, `data`, `cache`, `api`, `connect`, `events`, `sockets`) and all default-deny capability flags (`--allow-read`, `--allow-write`, `--allow-net`, `--allow-db`, `--allow-cache`, `--allow-connector`, `--allow-events`, `--allow-channel`, `--allow-telemetry`) remain enforced through the exact same host bridge.
- **Excluded from GRAB 0.9:**
  - Native machine-code compilation or JIT (`ADR-003`).
  - Persistent on-disk bytecode cache files across process restarts (`ADR-012` — compilation artifacts live in-memory per `PreparedProgram` generation and are discarded on reload/shutdown).
  - Speculative type-unsafe optimizations that could alter `integer`/`decimal`/`float` semantics, overflow behavior, or error spans.
  - Mid-loop On-Stack Replacement (OSR) with complex frame reconstruction (compilation promotion occurs cleanly at function/route entry boundaries).
  - GRAB 1.0 package registry, MCP server, Grex JS, or GrabLab CMS features.

---

## 2. Profile-driven compilation strategy (`ADR-003`, `ADR-013`)

### 2.1 Why profile-driven hybrid execution?
Compiling every cold script or one-time initialization block to IR and bytecode adds startup work without improving throughput. Conversely, repeated `PreparedApp` route handlers, recursive or frequently called `make` functions, and loop-heavy blocks spend most of their time re-traversing AST nodes and looking up variable names in Go maps.

GRAB 0.9 solves this with a **two-tier, profile-driven pipeline**:

| Tier | Engine | When used (`--engine=auto`) | Characteristics |
| --- | --- | --- | --- |
| **Tier 0** | AST Interpreter (`internal/interpreter`) | Top-level startup (`BaseEnv`), cold `grab run` scripts, cold functions/routes | Zero compile overhead; immediate execution; semantic reference oracle |
| **Tier 1** | IR + Bytecode VM (`internal/ir`, `internal/vm`) | Hot functions, hot loops (via enclosing unit), hot `PreparedApp` routes, or `--engine=vm` | Pre-resolved local slots, constant pool, flat bytecode dispatch, zero AST traversal |

### 2.2 Profiling triggers and deterministic thresholds
Profiling counters are lightweight atomic or frame-local integer counters attached to validated `PreparedProgram` units (`make` functions and `route` handlers):

1. **Function hotness (`HotFunctionThreshold = 16`):** Each `make` function starts in Tier 0. On each call, its invocation counter increments. Once it reaches `16` calls, it is compiled once to IR → Bytecode and atomically published to the `PreparedProgram` unit slot for all subsequent calls.
2. **Loop back-edge promotion (`HotLoopThreshold = 64`):** Inside Tier 0 loops (`repeat`, `for each`, `while`), every iteration increments the enclosing function/route's back-edge counter. Reaching `64` iterations marks the enclosing function or route hot so its next invocation enters Tier 1 cleanly at the function/route boundary—avoiding fragile mid-loop OSR state reconstruction.
3. **Prepared server route promotion (`HotRouteThreshold = 8`):** Under `grab serve`, `route` handlers are expected to serve repeated traffic. A route compiles to Tier 1 after `8` requests (or immediately on load if `--engine=vm` is selected).
4. **Top-level script execution:** Top-level statements in `grab run` execute once in Tier 0 under `auto` mode (keeping CLI startup instant), unless `--engine=vm` is explicitly requested (for conformance testing and benchmarks).

### 2.3 Compilation concurrency and `PreparedApp` generation ownership (`ADR-011`, `ADR-012`)
- **Single-flight per unit:** In `grab serve`, multiple concurrent requests may hit the same hot route or function simultaneously. Promotion uses a per-unit `sync.Once` (or atomic state machine: `Cold → Compiling → Compiled | Failed`) so a hot unit is compiled **at most once** per `PreparedApp` generation.
- **No blocking of other requests:** While one request compiles a hot unit, concurrent requests may either wait briefly on the unit's `sync.Once` or continue on Tier 0; once published, the compiled `*vm.Chunk` is **100% immutable** for the lifetime of that `PreparedApp` generation.
- **Atomic reload cleanup:** When `grab serve --reload` swaps in a new `PreparedApp` generation, old bytecode chunks are retired and garbage-collected alongside the retired `PreparedApp`, never shared across generations.
- **Fail-closed compilation:** If IR lowering or bytecode verification ever fails (which should never happen for a validated AST), the error is surfaced deterministically (`GRAB-E399` internal compiler error in `vm` mode, or logged and isolated without corrupting state).

---

## 3. Lowering pipeline: AST → IR → Bytecode

### 3.1 Pre-execution validation gate (unchanged)
The compiler **never** accepts raw or unvalidated ASTs. Every program must first pass:
1. `lexer` + `parser.Parse`
2. Existing `internal/interpreter` static pre-execution validation (module `use` checks, variable binding/exhaustiveness across `if`/`try`/`catch`, function arity checks, `do together` task group rules `GRAB-E208`, `route` conflict checks `GRAB-E209`, literal SQL/alias/contract checks).

### 3.2 Linear Intermediate Representation (`internal/ir`)
Validated AST nodes for a function, route, or program body lower into a flat, typed-slot IR (`ir.Unit`):
- **Lexical Slot Allocation:** Every parameter, local `set` variable, `for each` loop variable, and `catch <err>` variable is assigned a dense integer slot index (`uint16`) in the unit's frame layout. Globals from immutable `BaseEnv` (startup bindings) are resolved by index into a read-only base-slot table.
- **Constant Pool:** Literal `runtime.Value` entries (`IntegerKind`, `DecimalKind`, `FloatKind`, `TextKind`, `BooleanKind`, `NothingKind`, `BytesKind`) and constant field/key names are deduplicated into an immutable `[]runtime.Value` constant pool.
- **Structured Control Flow:** `if`/`otherwise if`/`otherwise`, `repeat`, `for each`, `while`, and short-circuit `and`/`or` lower to explicit basic-block labels and relative jump offsets.
- **Exception Handler Table:** Each `try ... catch <err> ... end` block emits an entry `{StartPC, EndPC, HandlerPC, ErrSlot}` sorted innermost-first, eliminating runtime try-frame push/pop overhead on the happy path.
- **Source Span Table:** Every IR/bytecode instruction maps to its originating `lexer.Span` (compact run-length encoded `[]SpanEntry`) so runtime errors and `StackFrame` traces report exact `line` and `column` positions.

### 3.3 Safe, semantics-preserving IR passes
GRAB 0.9 permits **only** passes proven not to alter observable behavior, error codes, or source spans:
- **Redundant jump / dead-code elimination** after `give back` or `raise`.
- **Direct local-slot & base-slot addressing** (replacing runtime map lookups by name).
- **Pre-resolved function & stdlib dispatch indices** (resolving `make` function IDs and `<module>.<func>` host bridge IDs at compile time).
- **No unsafe constant folding:** Expressions that can fail at runtime (such as `divided by`, `modulo`, `at` indexing, or `float` overflow) are **not** folded away if doing so could alter error ordering or spans.

### 3.4 Compact bytecode format (`internal/vm`)
The IR lowers to a compact stack-based bytecode (`vm.Chunk`) with:
- `Code []byte` (opcode `uint8` + compact `uint16`/`uint32` operands)
- `Constants []grabrt.Value`
- `MaxLocals uint16` and `MaxStack uint16` (statically computed!)
- `Handlers []ExceptionHandler`
- `Spans []SpanRange`

Core instruction families:
- **Constants & Slots:** `OpConst`, `OpNothing`, `OpTrue`, `OpFalse`, `OpLoadLocal`, `OpStoreLocal`, `OpLoadBase`, `OpPop`, `OpDup`
- **Arithmetic & Text:** `OpAdd`, `OpSub`, `OpMul`, `OpDiv`, `OpMod`, `OpNeg`, `OpJoin`
- **Comparison & Logic:** `OpEq`, `OpNeq`, `OpGt`, `OpLt`, `OpGte`, `OpLte`, `OpNot` (plus jump-if-false/true for short-circuit `and`/`or`)
- **Collections & Lookup:** `OpBuildList`, `OpBuildMap`, `OpBuildRecord`, `OpField` (`.field`), `OpAt` (`at`), `OpHas` (`has`), `OpIterInit`, `OpIterNext` (`for each`)
- **Control Flow & Safety:** `OpJump`, `OpJumpIfFalse`, `OpLoopBack` (increments step counter + checks `ctx.Done()`), `OpRepeatInit`, `OpRepeatNext`
- **Calls & Errors:** `OpCall` (user `make` function), `OpCallHost` (stdlib/host module bridge), `OpReturn`, `OpRaise`, `OpShow`
- **Concurrency:** `OpDoTogether` (spawns isolated snapshot tasks using the shared `do together` coordinator, committing bindings and `show` output in exact top-to-bottom source order)

---

## 4. Strict semantic equivalence gate

The bytecode VM is **not** a new language dialect. Under `--engine=vm` and `--engine=auto`, every supported GRAB 0.1–0.8 program must behave **identically** to `--engine=ast`:

1. **Value & Numeric Parity:**
   - Uses `internal/runtime` (`grabrt.Value`, `Add`, `Sub`, `Mul`, `Div`, `Mod`, `Equal`, `Compare`, `Display`) directly.
   - Preserves exact arbitrary-precision `integer` and base-10 `decimal` arithmetic, lossless `integer` → `decimal` promotion, strict `float` (`binary64`) isolation, and `GRAB-E302` on division/modulo by zero.
   - Preserves strict `yes`/`no` Boolean checks in `if`, `while`, `not`, `and`, `or` (no truthy/falsy coercion; `GRAB-E301` on non-Boolean).
2. **Collection & Lookup Parity:**
   - Preserves ordered `record` and `map` semantics, 0-based `list at index`, unified `.field`/`at`/`has` lookup across records and maps, and the distinction between an absent key/field (`GRAB-E303`) and `nothing`.
3. **Error & Diagnostic Parity:**
   - `try` / `catch` / `raise` binds the exact same immutable error record (`code`, `category`, `message`, `line`, `column`).
   - Non-catchable fatal safety limits (`GRAB-E306` call stack overflow, `GRAB-E307` execution limit exceeded) bypass `catch` blocks identically in both engines.
   - `beginner`, `developer`, and `production` diagnostic formatting (`internal/interpreter/diagnostics.go`) produces identical output and stack traces.
4. **Concurrency Parity (`do together` / `wait for`):**
   - Snapshot isolation of caller locals/base bindings, 16-task cap (`MaxDoTogetherTasks = 16`), deterministic top-to-bottom commit of bindings and `show` output, sibling cancellation on error, and earliest-in-source-order error propagation.
5. **Capability & Host Bridge Parity:**
   - All `use <module>` calls (`files`, `http`, `server`, `data`, `cache`, `api`, `connect`, `events`, `sockets`, etc.) invoke the exact same capability-checked host functions in `internal/interpreter`, ensuring zero permission bypasses and identical `GRAB-E401`–`GRAB-E403` errors.
6. **Differential Oracle Testing:**
   - A permanent differential test suite executes the entire GRAB 0.1–0.8 conformance corpus, server/HTTP tests, database tests, and randomized/fuzzed AST programs under both `--engine=ast` and `--engine=vm`, asserting byte-for-byte identical outputs, responses, error codes, and spans.

---

## 5. Safety & determinism guards

1. **Loop Step Budget (`DefaultMaxLoopSteps = 1_000_000`):**
   - `OpLoopBack` and `OpRepeatNext` increment the execution-owned step counter (`steps++`) and immediately terminate with non-catchable `ExecutionLimitExceeded` (`GRAB-E307`) when `steps > maxSteps`, matching the AST interpreter's exact iteration count.
2. **Call Frame Depth Limit (`DefaultMaxCallDepth = 256`):**
   - `OpCall` checks `len(frames) >= maxCallDepth` before pushing a new call frame and immediately terminates with non-catchable `CallStackOverflow` (`GRAB-E306`).
3. **Bounded Operand Stack & Frame Memory:**
   - Every `vm.Chunk` declares its statically computed `MaxStack` and `MaxLocals`.
   - The bytecode verifier rejects any chunk whose simulated stack depth underflows `< 0`, exceeds `MaxStack`, mismatches at jump merge points, or references out-of-bounds constants, locals, or jump targets.
   - An execution-wide total stack-slot cap (e.g., `256` frames × bounded frame slots) prevents memory exhaustion before Go panics.
4. **Context Cancellation & Request Deadlines:**
   - Loop back-edges (`OpLoopBack`, `OpRepeatNext`), function calls (`OpCall`), and host bridge calls (`OpCallHost`) check `ctx.Err()` so `grab serve --request-timeout` and `do together` sibling cancellation preempt cleanly without leaking goroutines.
5. **Panic Containment:**
   - Bytecode verification eliminates out-of-bounds indexing in the VM loop; `PreparedApp` per-request panic recovery remains active as defense in depth.

---

## 6. Benchmark & acceptance gates (`ADR-016`)

GRAB 0.9 cannot be marked complete without reproducible, evidence-backed measurements comparing `--engine=ast`, `--engine=vm`, and `--engine=auto` on the same hardware using `go test -run '^$' -bench . -benchmem -count=10`.

### 6.1 Benchmark suite
- **Language & VM microbenchmarks (`internal/interpreter/benchmark_test.go` + VM equivalents):**
  - `StraightLineAndJoin`
  - `ArithmeticAndConditionals`
  - `CollectionIterationAndLookup`
  - `FunctionsAndCallFrames` (recursive `fib`)
  - New GRAB 0.9 workloads: `TightLoopArithmetic` (100k iterations), `TryCatchHappyPath`, and `MixedTierWarmup` (cold startup + hot loop).
- **Persistent server benchmarks (`internal/server/benchmark_test.go`):**
  - `BenchmarkPreparedApp` (`text`, `json`, `html`) and `BenchmarkRouteMatch` comparing `ast` vs warm `vm`/`auto`.

### 6.2 Mandatory acceptance gates
1. **100% Conformance Parity:** All existing GRAB 0.1–0.8 tests (`go test ./...`) pass under `--engine=ast`, `--engine=vm`, and `--engine=auto`, plus the new differential equivalence suite.
2. **Verified Compute & Allocation Improvement:** On compute-, loop-, and function-heavy benchmarks (`FunctionsAndCallFrames`, `CollectionIterationAndLookup`, `TightLoopArithmetic`), `--engine=vm` demonstrates a **≥ 2.0× speedup** (`ns/op`) and **≥ 40% reduction in heap allocations** (`allocs/op`) relative to `--engine=ast`, with zero regression on `StraightLineAndJoin` or `BenchmarkPreparedApp`.
3. **Bounded Cold-Start Overhead:** Profile-driven `--engine=auto` adds **< 10%** cold-start latency on single-shot `grab run` scripts compared to `--engine=ast`.
4. **Concurrency & Security Hygiene:** `go test -race ./...` (Linux) and `govulncheck ./...` pass with zero data races and zero vulnerabilities.
