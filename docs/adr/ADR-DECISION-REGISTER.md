# GRAB Architecture Decision Register

## Purpose

This register records the project owner's architecture planning decisions for proposed ADR-011 through ADR-018. It captures approved principles, limits their scope, and identifies work that remains deferred or requires a later decision. It does not claim that any described capability is implemented.

**Approval date:** November 10, 2026

## Document authority

- GRAB Master Engineering Blueprint v1.0 remains the approved architectural baseline.
- Blueprint v1.1 remains a proposed update and is not approved wholesale.
- Existing baseline decisions ADR-001 through ADR-010 are preserved as recorded in Blueprint v1.0, Section 15. This register does not amend them.
- The corrected repository structure on the final page of each blueprint is authoritative. The initial language-engine packages use the existing Go `internal/` tree: `internal/lexer`, `internal/parser`, `internal/ast`, `internal/interpreter`, and `internal/runtime`. Future package locations remain open unless separately decided.
- ADR-011 through ADR-018 below are approved only at the planning or principle level stated in each entry. This register does not approve unresolved details or authorize implementation of deferred features.

## Decision register

| ADR | Decision and rationale | Scope and future dependencies | Explicit deferrals |
|---|---|---|---|
| **ADR-011 — Prepared applications and request isolation** | **MODIFY — approved principle.** Treat immutable prepared application definitions and request-owned execution isolation as design goals. This supports reuse while reducing cross-request state leakage. | A future server contract will define execution frames, ownership and lifecycle. It depends on defined runtime value/state behavior and server lifecycle requirements. | No request machinery or server lifecycle implementation in GRAB 0.1. Immutability boundaries, frame contents, state sharing and reload behavior remain open. |
| **ADR-012 — Parse-on-load and bytecode caching** | **MODIFY — approved principle.** Retain parsing and validation once per application load as a future persistent-server goal, avoiding repeated frontend work. | The parse-on-load policy belongs to future server loading/reload design and depends on stable parsing and validation behavior. | Defer bytecode caching until a VM exists and measurements justify it. Cache format, keys, invalidation, dependency tracking and integrity rules are not approved. |
| **ADR-013 — Optional intermediate representation** | **APPROVE.** An intermediate representation (IR) is optional, not a required pipeline stage. GRAB 0.1 uses an AST interpreter without a mandatory IR, keeping the first implementation direct and testable. | Reconsider an IR only in a later optimization/VM milestone, based on profiling and stabilized language semantics. Any later VM must preserve the conformance behavior of the interpreter. | No IR, bytecode VM, optimizer or native/JIT backend is part of GRAB 0.1. Their designs and implementation remain deferred. |
| **ADR-014 — React/TypeScript interoperability and browser compiler** | **DEFER.** React/TypeScript interoperability and a GRAB-to-browser compiler remain roadmap proposals. Deferring them avoids committing the language to an undefined frontend integration model. | Reconsider under a future UI strategy, with an explicit use case and defined API/type contracts, module boundaries, serialization and browser/server responsibilities. | No particular React integration, generated client, component model, rendering model or browser compiler target is approved. This decision does not approve Blueprint v1.1 wholesale. |
| **ADR-015 — Language concurrency and server resource policy** | **MODIFY — approved principle.** Preserve explicit language-level concurrency goals, consistent with baseline ADR-007, while separating language semantics from server backpressure and resource-management policy. This keeps language behavior distinct from deployment controls. | Future language/concurrency specifications will define task-group behavior. A separate server contract will define admission control, queueing, backpressure and resource budgets. These designs must coordinate without making Go implementation details the language semantics. | Exact task-group syntax and behavior, cancellation, error propagation, result ordering, fairness, shared state, server limits and overload handling remain open. Concurrency constructs are outside GRAB 0.1. |
| **ADR-016 — Evidence-based performance goals** | **APPROVE.** Use reproducible, workload-specific benchmarks and evidence to set performance goals. This makes comparisons actionable and avoids unsupported performance claims. | Define workloads, environment, methodology and repeatability for the milestones where performance is relevant. Set numeric targets only after representative baselines exist. | No unsupported numeric targets or blanket performance claims are approved. Benchmark criteria do not replace GRAB 0.1 correctness and conformance requirements. |
| **ADR-017 — Server/browser security separation** | **MODIFY — approved principle.** Preserve a clear security boundary between server-side capabilities and browser-delivered code. This reduces accidental exposure of secrets and privileged APIs. | Define detailed contracts at the relevant module, server, security and UI milestones, coordinating imports, data transfer and trust boundaries. | Module rules, serialization formats, output escaping behavior and authorization contracts remain deferred and unapproved. No browser architecture or implementation is part of GRAB 0.1. |
| **ADR-018 — Incremental server rendering and browser interactivity** | **DEFER.** Incremental server rendering and selective browser interactivity remain future implementation proposals, not approved architecture. Their value and design depend on a chosen UI integration model. | Reconsider only after owner approval of a UI strategy and relevant server/browser contracts. Dependencies include ADR-014's integration decision and ADR-017's security boundaries. | No server-rendering model, hydration behavior, client compiler, component syntax or React compatibility is approved. This is not part of GRAB 0.1. |

## GRAB 0.1 scope

GRAB 0.1 remains limited to the formal language specification, lexer, parser, AST, basic interpreter/runtime, CLI, and conformance tests. Its purpose is a small, deterministic language foundation. It does not include a bytecode VM, mandatory IR, request machinery, server, UI, browser compiler, language-level concurrency implementation, or CMS.

## Open design questions requiring future owner approval

The following questions remain open; this register does not decide them:

1. **Later language semantics:** The GRAB 0.1 value scope, syntax, semantics, diagnostics and conformance cases in `specifications/GRAB-LANGUAGE-SPEC-001.md` were approved on November 10, 2026. Numeric operators, equality, coercion, additional value kinds, modules and other features listed as deferred in that specification require future design and owner approval.
2. **Runtime state and server lifecycle:** What is immutable in a prepared application, what state belongs to each request, how are shared values handled, and what are the load, reload, cancellation and cleanup rules?
3. **Concurrency contract:** What syntax and semantics govern task creation and joining, result order, cancellation, failures, fairness, timeouts and shared mutable state? Which resource controls belong only to the server?
4. **Server preparation and caching:** What constitutes an application load or reload, and what evidence would justify bytecode caching after a VM exists? If justified, what cache compatibility, invalidation and integrity rules are required?
5. **Performance evidence:** Which workloads and environments represent GRAB's goals, and what numeric thresholds, if any, should be set after baselines are measured?
6. **Module and browser security boundary:** How should server-only and browser-safe modules be identified, and what serialization, escaping and authorization contracts are required?
7. **Frontend strategy:** What specific React/TypeScript interoperability use case is intended, if any? Is a GRAB browser compiler wanted, and what target and compatibility obligations would it carry?
8. **Rendering model:** Is incremental server rendering with selective client interactivity needed? If so, what rendering, hydration and component model should it use?
9. **HTML templates:** The owner approved a separate future `.grab.html` template milestone on November 10, 2026, and approved its full specification (`specifications/GRAB-LOCALHOST-SPEC-002.md`) on November 11, 2026.

A future proposal must resolve the relevant questions and receive owner approval before these deferred details become architectural commitments or implementation work.

## Future roadmap proposals and approved future milestones

### HTML template files and localhost preview (GRAB 0.2) — approved milestone target

**Status: APPROVED FOR IMPLEMENTATION (`specifications/GRAB-LOCALHOST-SPEC-002.md`, approved November 11, 2026).** On November 10, 2026, the project owner approved a separate milestone for reusable HTML templates and a minimal localhost browser preview (`GRAB 0.2`), with `.grab.html` as the preferred direction. Program logic remains in `.grab` files, and a `.grab.html` template is reusable by multiple `.grab` programs.

On November 11, 2026, the project owner approved the GRAB 0.2 specification in `specifications/GRAB-LOCALHOST-SPEC-002.md`:
- **Interpolation grammar (`ADR-019a` approved):** `{<identifier>}` (such as `{title}`) with `{{` and `}}` for literal braces; no `show` keyword, expressions, or control flow inside `.grab.html`.
- **Lookup and reuse (`ADR-019b` approved):** `grab serve [--addr 127.0.0.1:8080] <file.grab> [template.grab.html]`, defaulting to `<basename>.grab.html` in the same directory and rejecting path traversal outside the workspace.
- **Value binding (`ADR-019c` approved):** `.grab` executes first; its final variable environment is passed as a read-only snapshot to the template; unbound template variables fail pre-render verification with a source-located diagnostic; bound values format via GRAB 0.1 canonical display before escaping.
- **Context-aware HTML escaping (`ADR-019d` approved):** Mandatory escaping in HTML element text, quoted attributes, and quoted URL attributes (`http:`, `https:`, `mailto:`, and relative URLs only); default-deny rejection of interpolations in `<script>`, `<style>`, comments, tag/attribute names, `on*` event attributes, and unquoted attributes.
- **Localhost server security and lifecycle (`ADR-019e` approved):** Loopback-only binding (`127.0.0.1` / `localhost` / `::1`), `GET /` and `HEAD /` only, per-request isolated execution frames (`ADR-011`), mandatory security headers (`X-Content-Type-Options`, `X-Frame-Options`, `Content-Security-Policy`), bounded request timeouts, and graceful `SIGINT`/`SIGTERM` shutdown.

This approval authorizes GRAB 0.2 implementation only. It does not approve a production server, browser compiler, React/TypeScript interoperability, or general client-side UI framework.

### Control flow, functions, and richer language (GRAB 0.3) — approved milestone target

**Status: APPROVED FOR IMPLEMENTATION (`specifications/GRAB-LANGUAGE-SPEC-003.md`, approved and implemented November 11, 2026).** On November 11, 2026, the project owner approved **GRAB 0.3 — Control flow, functions, and richer language** and its formal specification in `specifications/GRAB-LANGUAGE-SPEC-003.md`:
- **Expression grammar and operator precedence (`ADR-020a` approved):** Parenthesized grouping `( ... )`; postfix lookup (`.field`, `at`, `has`); unary `not`; multiplicative (`times`, `divided by`, `modulo`); additive (`plus`, `minus`); text concatenation (`joined with`); relational (`[is] greater than`, `[is] less than`, `[is] at least`, `[is] at most`); equality (`is`, `is not`); and short-circuit `and` / `or`.
- **Exact numeric and comparison semantics (`ADR-020b` approved):** Preserve GRAB 0.1 portable numeric rules: `integer` combined with `decimal` promotes losslessly to exact `decimal`; `float` (`binary64`) never mixes implicitly with `integer` or `decimal`; `divided by` or `modulo` by zero raises a source-located `division by zero` runtime error; conditions and logical operators require strict `boolean` (`yes`/`no`) values without truthy/falsy coercion.
- **Collection lookup and membership (`ADR-020c` approved):** `record.field` accesses record fields; `list at index` (0-based integer) and `map at key` (`text` key) access list and map elements; `collection has key_or_field` tests key/field presence and returns `yes`/`no`, preserving the GRAB 0.1 distinction between an absent key/field and `nothing`.
- **Control flow and block scoping (`ADR-020d` approved):** `if` / `otherwise if` / `otherwise` / `end`, `repeat <count> times ... end`, `for each <item> in <collection> ... end`, and `while <condition> ... end` execute with deterministic lexical scoping and a configurable loop iteration budget (default `1,000,000`) guarding against accidental infinite loops.
- **Functions and isolated call frames (`ADR-020e` approved):** Top-level `make <name> [using <params>] ... end` declarations, `give back [expr]`, and calls via `<name> using <args>` or `call <name> [using <args>]`. Functions execute in isolated lexical call frames, enforce arity at pre-execution validation, and bound recursion at 256 frames.
- **Initial profiling harness (`ADR-020f` approved, applying `ADR-016`):** Reproducible Go `testing.B` benchmarks with `b.ReportAllocs()` (`internal/interpreter/benchmark_test.go`) across straight-line, arithmetic/branching, collection-loop, and function-call workloads.
