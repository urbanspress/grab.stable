# GRAB Roadmap

This roadmap records owner-approved milestone targets. It does not claim that a
milestone is complete, set delivery dates, or approve implementation details
that remain open in the architecture decision register.

## GRAB 0.1 — Language foundation

**Status:** Complete — November 11, 2026  
**Target:** Small, deterministic language interpreter and CLI.

Scope:

- Formal language specification, lexer, parser, AST, interpreter and basic
  runtime.
- `grab run <file.grab>` command-line execution.
- Conformance tests for the approved language behavior.

Server, UI, browser execution, HTML templates, bytecode VM and browser compiler
are outside this milestone.

The approved language behavior is covered by the end-to-end conformance suite
in `tests/`; see `tests/README.md` for the coverage map and documented limits.

## GRAB 0.2 — Localhost browser preview

**Status:** Approved target; not started  
**Approval date:** November 10, 2026  
**Target:** Enable the first browser-based test of GRAB on localhost.

Approved milestone outcome:

- A minimal local development server can render and serve a GRAB page for
  browser testing.
- Program logic remains in `.grab` files, with reusable HTML in
  `.grab.html` templates.
- Templates can interpolate GRAB values and must use context-aware HTML
  escaping.
- The milestone defines how templates are located and reused, how values are
  bound from a `.grab` program, and how rendering errors and output are
  handled.

Before implementation, the milestone must settle the exact interpolation
grammar, template lookup and reuse rules, value-binding contract, escaping
contexts, output behavior, diagnostics, and the minimum local-server lifecycle
and security requirements. The architecture register records the approved
template goals and open design details.

This target is for local development and browser testing. It does not approve a
production server, browser compiler, React/TypeScript interoperability,
incremental rendering, hydration, or a general client-side UI framework. No
delivery date is set.
