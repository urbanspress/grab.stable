# GRAB 0.1 specification conformance

Run `go test ./...` from the repository root. No external test framework is
required. `conformance_test.go` builds the real CLI once in a temporary directory
and executes every table row as an independent UTF-8 source file. It checks exact
stdout bytes, stderr presence/absence, success/failure, and diagnostic start
locations. The same source also passes through the parser and interpreter to
assert typed diagnostic categories and complete half-open spans, including byte
offsets. CLI wording and exact exit codes are not language requirements.

## Specification coverage map

| Specification | Executable coverage |
| --- | --- |
| Status, scope, sections 1–2 | Approved portable kinds and source pipeline; absent versus explicit `nothing`; excluded syntax is rejected. Planning/approval history is documentation, not executable behavior. |
| 3.1 | Invalid UTF-8 (overlong, surrogate, truncated and out-of-range encodings), LF/CRLF, empty and blank programs, tabs/indentation, punctuation, semicolon rejection, optional final newline. |
| 3.2 | Comment-only and inline comments, comment at EOF, Unicode in comments, literal `#`, comments cannot continue a collection onto another line. |
| 3.3 | ASCII name forms, digits/underscore, case sensitivity, every reserved keyword as both binding and record field, representative unreserved words including future-feature names. |
| 3.4 | All five text escapes, invalid/incomplete escapes, EOF/LF/CRLF/lone-CR termination, Unicode scalar boundaries and controls, non-normalization, no interpolation, top-level and nested text. |
| 3.5 and 4 | Every primary/statement production, negative and large integers, exact decimal scale, exponent forms, finite binary64 boundaries/rounding, all 256 byte values, byte escape errors, empty/nested/trailing-comma containers, delimiters, separators, and forbidden internal newlines. |
| 5.1 | Binding, rebinding across kinds, self-reference after binding, first-binding self-reference rejection, forward-reference rejection, references in all expression kinds, case-sensitive reads. |
| 5.2 | Arbitrary-precision integers and decimals, separate numeric displays, present `nothing` versus absent entries, insertion/source order, decoded duplicate keys, duplicate fields, immutable collection snapshots after rebinding, no automatic byte/text conversion. |
| 5.3 | Chained joining, both operand positions for every non-text kind, first-error order in joins and all collections. `structure_test.go` directly verifies left association because successful text output alone cannot distinguish association. |
| 5.4 | Exact stdout and final LF in every successful case, canonical formatting for every value kind, nested quoting, lowercase byte hex, ordering, no partial composite output. |
| 5.5 | Whole-source lexical/syntax checks and whole-program name resolution suppress prior output; runtime failures retain earlier output and stop later statements; failing `set` expressions stop execution. A failed binding's later contents are unobservable because 0.1 has no recovery. |
| 6 | Stable typed categories, exact start/end byte offsets and scalar columns, tabs, multibyte text, CRLF, EOF zero-width spans, opening-quote unterminated-text spans. `structure_test.go` adds fixed AST span expectations. |
| 7–9 | Approved welcome and product programs; unterminated text and unbound-name error cases. |
| 10–11 | Scope exclusions remain unimplemented; approval checklist behavior is covered by the sections above. Future adapters, VM, server, templates and UI are not claimed as tested implementations. |

## Limits and implementation details

This is a rule-based conformance suite, not an exhaustive enumeration of all
possible programs or proof of correctness. The existing package tests complement
the executable tests, including CLI usage and missing-file behavior. There is no
collection lookup in 0.1: absence is tested through distinct collection shapes.
There are no arithmetic/conversion operations with which to test numeric coercion.

The specification defines canonical **finite** binary64 output but does not
specify overflow/non-finite results or underflow policy. The current runtime
rejects overflow/non-finite literals and uses Go's binary64 parsing for underflow;
this suite does not turn those unspecified choices into new language rules.
Similarly, CLI wording and exact exit codes remain implementation contracts.

The suite exposed and now guards two lexer fixes: raw U+000D inside text is
rejected before execution, and unterminated-text diagnostics identify only the
opening quote rather than extending through subsequent content/newline.

## GRAB 0.2 localhost and HTML template conformance

`localhost_conformance_test.go` verifies `specifications/GRAB-LOCALHOST-SPEC-002.md`:
- `.grab.html` `{<identifier>}` interpolation and `{{` / `}}` literal braces;
- context-aware HTML escaping across element text, quoted attributes, and quoted URL attributes (`href`, `src`, `action`, `formaction`);
- preflight and render-time diagnostics (`invalid template encoding`, `malformed template interpolation`, `disallowed interpolation context`, `unbound template variable`, and `unsafe URL scheme`);
- checked-in example templates (`examples/hello.grab.html`, `examples/product.grab.html`).

## GRAB 0.3 control flow, functions, and richer language conformance

`grab03_conformance_test.go` verifies `specifications/GRAB-LANGUAGE-SPEC-003.md`:
- reserved GRAB 0.3 keywords, parenthesized grouping, and operator precedence;
- exact integer/decimal arithmetic, binary64 float operations, division-by-zero checks, equality (`is` / `is not`), relational comparisons, and short-circuit Boolean logic (`not`, `and`, `or`);
- record field lookup (`.field`), list and map lookup (`at`), and existence checks (`has`);
- `if` / `otherwise if` / `otherwise` / `end`, `repeat <count> times`, `for each <item> in <collection>`, and `while <condition>` loops (including the `1,000,000`-step safety guard);
- top-level functions (`make` / `using` / `give back` / `call`), isolated lexical call frames, pre-execution arity verification, and the `256`-frame recursion guard.

## GRAB 0.4 standard library, error model, and permissions conformance

`grab04_conformance_test.go` verifies `specifications/GRAB-STDLIB-SPEC-004.md`:
- reserved GRAB 0.4 keywords (`use`, `try`, `catch`, `raise`) and top-level `use <module>` validation (`GRAB-E205`, `GRAB-E206`, `GRAB-E207`);
- all five core standard library modules (`text`, `math`, `collections`, `dates`, `files`) and value-returning helper calls;
- structured error model (`try ... catch <ident> ... end` and `raise <expr>`), error records (`code`, `category`, `message`, `line`, `column`), exhaustive branch bindings, and non-catchable fatal safety limits (`GRAB-E306`, `GRAB-E307`);
- capability-based default-deny filesystem permissions (`--allow-read <dir>`, `--allow-write <dir>`), path confinement, symlink checking, `10 MiB` file limit, and mandatory sensitive-path blocklist (`GRAB-E401`);
- CLI subcommands `grab check` and `grab format [--write]` plus diagnostic modes (`--diagnostics beginner|developer|production`).
