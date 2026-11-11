# GRAB Language Specification 003

## Status and authority

**Status:** APPROVED for GRAB 0.3. Project-owner approval recorded November 11, 2026.
**Target milestone:** GRAB 0.3 — Control flow, functions, and richer language
**Owner approval date:** November 11, 2026
**Prerequisite baseline:** `specifications/GRAB-LANGUAGE-SPEC-001.md` (GRAB 0.1), `specifications/GRAB-LOCALHOST-SPEC-002.md` (GRAB 0.2), and `docs/adr/ADR-DECISION-REGISTER.md`

This specification defines the approved language expansion for **GRAB 0.3**, settling the design questions listed in `docs/ROADMAP.md` and `docs/adr/ADR-DECISION-REGISTER.md` (Open Question #1 on numeric operators, equality, comparisons, collection lookup, control flow, functions, lexical scoping, and Open Question #5 / `ADR-016` on initial memory and allocation profiling baselines).

---

## 1. Purpose and scope

GRAB 0.3 evolves GRAB from a straight-line value-and-template language into a general-purpose programming engine capable of conditional logic, bounded iteration, reusable functions with isolated call frames, arithmetic and comparison expressions, and collection lookup—while remaining 100% backward-compatible with all valid GRAB 0.1 and GRAB 0.2 programs.

The GRAB 0.3 milestone adds:

- **Parenthesized grouping and richer expressions:** `( ... )` for explicit grouping; English-inspired arithmetic (`plus`, `minus`, `times`, `divided by`, `modulo`); equality and relational comparisons (`is`, `is not`, `is greater than`, `is less than`, `is at least`, `is at most`); and short-circuit Boolean logic (`not`, `and`, `or`).
- **Collection lookup and membership:** Record field access (`record_val.field`), list and map lookup (`list_val at index`, `map_val at key`), and key/field existence checks (`map_val has "key"`, `record_val has field`).
- **Structured control flow (`if` / `otherwise` / `end` and loops):**
  - Conditional branches: `if <condition> ... [otherwise if <condition> ...] [otherwise ...] end`
  - Counted loops: `repeat <count> times ... end`
  - Collection loops: `for each <item> in <collection> ... end`
  - Conditional loops: `while <condition> ... end`
- **Functions (`make` / `using` / `give back` / `call`):** Reusable top-level function definitions with explicit lexical scoping, isolated call frames, deterministic parameter binding, and a bounded recursion depth guard.
- **Initial profiling harness (`ADR-016`):** Reproducible Go benchmark and allocation baselines (`b.ReportAllocs()`) across representative GRAB 0.1–0.3 workloads.
- **Expanded conformance suite:** End-to-end positive and negative conformance cases covering every new construct and diagnostic category.

### Explicitly outside GRAB 0.3

GRAB 0.3 does **not** include:
- Standard library modules (text utilities, math functions, filesystem I/O, dates/times) or `try` / `catch` / `raise` user-handled exceptions (planned for **GRAB 0.4**);
- External HTTP client calls, JSON codecs, or `do together` / `wait for` async task groups (planned for **GRAB 0.5**);
- Multi-route production HTTP server features (planned for **GRAB 0.6**);
- Database connections or SQL queries (planned for **GRAB 0.7**);
- Bytecode VM or intermediate representation (`ADR-013`, planned for **GRAB 0.9**);
- In-place collection mutation (lists, maps, and records remain immutable values; variables are updated via `set` rebinding).

---

## 2. Lexical additions and reserved words

### 2.1 Backward compatibility with GRAB 0.1

All UTF-8 encoding, line-ending (`LF` and `CRLF`), comment (`#`), text literal, byte literal, numeric literal, and source-span rules from `GRAB-LANGUAGE-SPEC-001.md` Section 3 remain unchanged.

### 2.2 New punctuation tokens

In addition to the GRAB 0.1 punctuation tokens (`[`, `]`, `{`, `}`, `,`, `:`), GRAB 0.3 adds:
- `(` and `)` — parenthesized expression grouping.
- `.` — record field lookup (`product.sku`). Note: a `.` immediately between digits (`19.95`) remains part of a decimal or float literal token.

### 2.3 Reserved keywords in GRAB 0.3

The following ASCII words are reserved in GRAB 0.3 and cannot be used as variable, parameter, record field, or function identifiers:

- **From GRAB 0.1:** `set`, `to`, `show`, `joined`, `with`, `yes`, `no`, `nothing`, `record`
- **Control flow:** `if`, `otherwise`, `end`, `repeat`, `times`, `for`, `each`, `in`, `while`
- **Functions:** `make`, `using`, `give`, `back`, `call`
- **Operators:** `is`, `not`, `greater`, `less`, `than`, `at`, `least`, `most`, `and`, `or`, `plus`, `minus`, `divided`, `by`, `modulo`, `has`

*(Note: Multi-word operators such as `joined with`, `is not`, `is greater than`, `is less than`, `is at least`, `is at most`, `divided by`, `for each`, and `give back` are parsed as sequential keyword tokens separated by horizontal whitespace.)*

---

## 3. Formal grammar (EBNF)

```ebnf
program            = { newline }, [ top_item, { newline, { newline }, top_item } ],
                     { newline } ;

top_item           = function_def | statement ;

function_def       = "make", identifier, [ [ "using" ], param_list ],
                     newline, block_body, "end" ;
param_list         = identifier, { ",", identifier } ;

block_body         = { newline }, [ statement, { newline, { newline }, statement }, { newline } ] ;

statement          = set_statement
                   | show_statement
                   | if_statement
                   | repeat_statement
                   | for_each_statement
                   | while_statement
                   | give_back_statement
                   | call_statement ;

set_statement      = "set", identifier, "to", expression ;
show_statement     = "show", expression ;

if_statement       = "if", expression, newline, block_body,
                     { "otherwise", "if", expression, newline, block_body },
                     [ "otherwise", newline, block_body ],
                     "end" ;

repeat_statement   = "repeat", expression, "times", newline, block_body, "end" ;
for_each_statement = "for", "each", identifier, "in", expression, newline, block_body, "end" ;
while_statement    = "while", expression, newline, block_body, "end" ;

give_back_statement = "give", "back", [ expression ] ;
call_statement     = "call", identifier, [ "using", arg_list ] ;
arg_list           = expression, { ",", expression } ;

expression         = or_expr ;
or_expr            = and_expr, { "or", and_expr } ;
and_expr           = equality_expr, { "and", equality_expr } ;
equality_expr      = relational_expr, [ ( "is", "not" | "is" ), relational_expr ] ;
relational_expr    = join_expr, [ ( [ "is" ], "greater", "than"
                                  | [ "is" ], "less", "than"
                                  | [ "is" ], "at", "least"
                                  | [ "is" ], "at", "most" ), join_expr ] ;
join_expr          = additive_expr, { "joined", "with", additive_expr } ;
additive_expr      = multiplicative_expr, { ( "plus" | "minus" ), multiplicative_expr } ;
multiplicative_expr = unary_expr, { ( "times" | "divided", "by" | "modulo" ), unary_expr } ;
unary_expr         = "not", unary_expr | postfix_expr ;

postfix_expr       = primary_expr, { "." , field_name
                                   | "at", primary_expr
                                   | "has", ( text_literal | field_name ) } ;
field_name         = identifier ;

primary_expr       = text_literal | boolean_literal | nothing_literal
                   | integer_literal | decimal_literal | float_literal
                   | bytes_literal | list_literal | map_literal
                   | record_literal
                   | call_expr
                   | identifier
                   | "(", expression, ")" ;

call_expr          = "call", identifier, [ "using", arg_list ]
                   | identifier, "using", arg_list ;
```

---

## 4. Operator precedence and expression semantics

### 4.1 Precedence table (highest to lowest)

| Level | Category | Operators / Syntax | Associativity |
| --- | --- | --- | --- |
| 1 | Grouping & Literals | `( expr )`, literals, function calls (`fn using ...` / `call fn`) | — |
| 2 | Lookup & Membership | `.field`, `at <expr>`, `has <key_or_field>` | Left-to-right |
| 3 | Unary Logic | `not <expr>` | Right-to-left |
| 4 | Multiplicative | `times`, `divided by`, `modulo` | Left-to-right |
| 5 | Additive | `plus`, `minus` | Left-to-right |
| 6 | Text Concatenation | `joined with` | Left-to-right |
| 7 | Relational | `[is] greater than`, `[is] less than`, `[is] at least`, `[is] at most` | Non-associative |
| 8 | Equality | `is`, `is not` | Non-associative |
| 9 | Logical AND | `and` | Left-to-right (short-circuit) |
| 10 | Logical OR | `or` | Left-to-right (short-circuit) |

Because `plus` / `minus` / `times` bind tighter than `joined with` and comparisons, expressions like `price plus tax is greater than 100` evaluate `price plus tax` first and then compare the result to `100` without needing parentheses.

### 4.2 Numeric arithmetic (`plus`, `minus`, `times`, `divided by`, `modulo`)

1. **Exactness and numeric kind compatibility:**
   - **`integer` with `integer`:**
     - `plus`, `minus`, and `times` produce an exact arbitrary-precision `integer`.
     - `modulo` computes the Euclidean/truncated integer remainder (`a modulo b`); only `integer` operands are permitted for `modulo`.
     - `divided by`: If `a` is evenly divisible by `b`, or when computing exact decimal division? To prevent silent integer truncation for non-coders (e.g., `1 divided by 2`), `integer divided by integer` produces an `integer` when the division is exact (`10 divided by 2` $\to$ `5`) and an exact `decimal` (rounded to 16 fractional digits or exact terminating decimal, stripping trailing fractional zeros down to at least 1 fractional digit if non-integer, or fixed exact decimal scale) — **Wait / Design Rule:** Let's keep numeric types 100% predictable:
       - `integer` `plus` / `minus` / `times` `integer` $\to$ `integer`.
       - `decimal` `plus` / `minus` / `times` `decimal` (or `integer` mixed with `decimal`, where the `integer` is treated as an exact decimal of scale `0`, as in Blueprint v1.0's `price times 0.08`) $\to$ exact `decimal` preserving standard decimal scale (`max(scaleA, scaleB)` for `plus`/`minus`; `scaleA + scaleB` for `times`).
       - `divided by` on `integer` or `decimal` operands:
         - `integer divided by integer` where the remainder is `0` produces an `integer` (e.g. `10 divided by 2` $\to$ `5`).
         - Otherwise, `integer` / `decimal` division produces an exact `decimal` when the base-10 quotient terminates within 16 fractional digits (stripping unnecessary trailing zeros beyond the operand scales, e.g., `1 divided by 4` $\to$ `0.25`), or rounds half-even at 16 fractional digits for repeating decimals (e.g., `1 divided by 3`).
       - `float` with `float`: `plus`, `minus`, `times`, and `divided by` use IEEE 754 `binary64` arithmetic. If a `float` operation overflows to `+Inf`/`-Inf` or produces `NaN`, it raises a runtime error (`numeric overflow or invalid float operation`).
       - **No implicit `float` mixing:** Consistent with GRAB 0.1 (`GRAB-LANGUAGE-SPEC-001.md` Section 5.2), `float` (`binary64`) MUST NOT be mixed implicitly with `integer` or `decimal` (doing so is a `runtime type error`).
2. **Division by zero:**
   - Any `divided by` or `modulo` operation whose right operand is zero (`0`, `0.0`, `0.0e0`) stops execution with a `division by zero` runtime error spanning the operation.

### 4.3 Equality (`is`, `is not`) and relational comparisons

1. **Equality (`is` and `is not`):**
   - Returns a Boolean (`yes` or `no`).
   - Two values of different kinds evaluate to `no` for `is` (and `yes` for `is not`), with one mathematical exception: an `integer` and an exact `decimal` with the same mathematical value (such as `10 is 10.00`) compare equal (`yes`). `float` remains distinct from `integer` and `decimal`.
   - `nothing is nothing` is `yes`.
   - `text`, `boolean`, and `bytes` compare by exact sequence/value equality.
   - `list`, `map`, and `record` compare deeply:
     - Two lists are equal if they have the same length and each corresponding element is equal under `is`.
     - Two maps are equal if they have the same key-value pairs (independent of insertion order).
     - Two records are equal if they have the same field names and equal field values (independent of field order).
2. **Relational comparisons (`[is] greater than`, `[is] less than`, `[is] at least`, `[is] at most`):**
   - Both `age is greater than 18` and `age greater than 18` are accepted by the grammar.
   - Permitted on compatible numeric operands (`integer`/`decimal` together, or `float` with `float`) and on `text` with `text` (compared lexicographically by Unicode scalar value).
   - Comparing incompatible kinds (such as `text` with `integer`, or comparing `nothing`, `boolean`, `bytes`, or collections with `<`/`>`) is a `runtime type error`.

### 4.4 Boolean logic (`not`, `and`, `or`)

1. **Strict Boolean operands (no truthy/falsy coercion):**
   - All operands to `not`, `and`, and `or`, as well as conditions in `if`, `otherwise if`, and `while`, MUST evaluate to a `boolean` (`yes` or `no`). Passing `0`, `""`, or `nothing` as a condition is a `runtime type error`.
2. **Short-circuit evaluation:**
   - In `left and right`, `left` is evaluated first. If `left` is `no`, the result is `no` and `right` is not evaluated. If `left` is `yes`, `right` is evaluated (and must be a `boolean`).
   - In `left or right`, `left` is evaluated first. If `left` is `yes`, the result is `yes` and `right` is not evaluated. If `left` is `no`, `right` is evaluated (and must be a `boolean`).

### 4.5 Collection lookup (`.`, `at`, `has`)

1. **Record field lookup (`<record>.<field>`):**
   - Evaluates `<record>`, which must be a `record` value (otherwise a `runtime type error`).
   - Looks up `<field>`. If the field is present, returns its bound value (which may be `nothing`). If the field is absent from the record, raises an `absent field or key` runtime error with source span.
2. **List index lookup (`<list> at <index>`):**
   - When the left operand is a `list`, `<index>` must evaluate to an `integer` (otherwise a `runtime type error`).
   - Lists use 0-based indexing (`0` through `length - 1`). If `<index>` is negative or `>= length`, raises an `index out of bounds` runtime error with source span.
3. **Map key lookup (`<map> at <key>`):**
   - When the left operand is a `map`, `<key>` must evaluate to a `text` value (otherwise a `runtime type error`).
   - If `<key>` exists in the map, returns its bound value. If `<key>` is absent, raises an `absent field or key` runtime error with source span.
4. **Existence check (`<map> has <text_literal>` or `<record> has <field>`):**
   - Returns `yes` if the key/field is present in the map or record (even if its value is `nothing`), or `no` if the key/field is absent.

---

## 5. Control flow statements and lexical scope

### 5.1 Block scoping rules

1. **Nested lexical scopes:**
   - Top-level statements execute in the program's global environment.
   - Control-flow blocks (`if`, `otherwise`, `repeat`, `for each`, `while`) execute within a child scope chained to their enclosing scope:
     - If `set x to <expr>` inside a control-flow block refers to a variable `x` that already exists in an enclosing scope (within the same function or top-level program), it updates that existing binding (so loops and branches can accumulate totals or update state naturally).
     - If `x` does not exist in the enclosing scope, `set x to <expr>` binds `x` local to that block.
     - In `for each <item> in <collection>`, the loop variable `<item>` is scoped locally to the loop body on each iteration.

### 5.2 Conditional statements (`if` / `otherwise if` / `otherwise` / `end`)

```grab
if score is at least 90
  set grade to "A"
otherwise if score is at least 80
  set grade to "B"
otherwise
  set grade to "C"
end
```

- Conditions are evaluated top-to-bottom. Each evaluated condition must produce a `boolean` (`yes` or `no`).
- The first branch whose condition evaluates to `yes` executes its body, and remaining branches are skipped.
- If all conditions evaluate to `no` and an `otherwise` branch is present, the `otherwise` body executes.

### 5.3 Loops (`repeat`, `for each`, `while`)

1. **`repeat <count> times ... end`:**
   - `<count>` is evaluated once before the loop begins and must evaluate to a non-negative `integer` (`0` or greater).
   - If `<count>` is `0`, the body does not execute. If `<count>` is a negative integer, raises a `negative repeat count` runtime error. If `<count>` is not an integer, raises a `runtime type error`.
2. **`for each <item> in <collection> ... end`:**
   - `<collection>` is evaluated once before the loop begins and must evaluate to a `list` (iterating elements in index order) or a `map` (iterating keys as `text` values in insertion order).
   - On each iteration, `<item>` is bound in the loop body's scope to the current element or key.
3. **`while <condition> ... end`:**
   - Before each iteration, `<condition>` is evaluated and must produce a `boolean`. While `yes`, the body executes; when `no`, the loop terminates.
   - **Bounded step budget:** To protect the CLI and localhost preview server from accidental infinite loops (`while yes`), the interpreter enforces a configurable step budget (defaulting to `1,000,000` loop iterations per execution) and raises an `execution limit exceeded` runtime error if exceeded.

---

## 6. Functions (`make`, `using`, `give back`, `call`)

### 6.1 Function definitions

```grab
make calculate_total using price, tax_rate
  set tax to price times tax_rate
  give back price plus tax
end
```

1. **Top-level declaration:**
   - `make` declarations may appear only at the top level of a `.grab` file (nested `make` inside another function or block is a syntax error).
   - Both `make greet using person` and `make greet person` are valid syntax for declaring parameters; zero-parameter functions are written `make say_hello`.
   - Duplicate function names, duplicate parameter names within the same function, or defining a function with the same name as a top-level variable before/after is rejected during pre-execution validation.
2. **Pre-execution registration:**
   - All top-level `make` declarations are registered during the pre-execution validation pass before top-level statements execute, allowing functions to call other helper functions defined anywhere in the file.

### 6.2 Function calls and isolated call frames

1. **Calling syntax:**
   - With arguments in an expression or statement: `calculate_total using 100, 0.08` or `call calculate_total using 100, 0.08`.
   - With zero arguments: `call say_hello` (valid both as a standalone statement and as an expression).
2. **Arity check:**
   - The number of arguments passed in `using` must exactly match the number of parameters declared in `make`. An argument-count mismatch is caught during pre-execution validation (and reported with a `function arity mismatch` diagnostic).
3. **Isolated lexical call frames:**
   - Arguments are evaluated left-to-right in the caller's environment before entering the function.
   - Each call creates a fresh, isolated call frame containing only the function's parameters and local `set` bindings.
   - Functions do **not** read or mutate top-level mutable variables directly—all inputs are passed explicitly via `using` and outputs returned via `give back`. This guarantees deterministic, side-effect-clear functions and prepares cleanly for future request isolation (`ADR-011`) and bytecode compilation (`ADR-013`).
4. **Return semantics (`give back`):**
   - `give back <expression>` evaluates `<expression>`, immediately terminates the current function call, and returns the value to the caller.
   - `give back` without an expression (or reaching the `end` of a function without executing `give back`) returns `nothing`.
   - Using `give back` outside of a `make ... end` function body is a pre-execution validation error (`invalid give back outside function`).
5. **Recursion limit:**
   - Direct and mutual recursion are supported up to a maximum call stack depth of `256` active frames. Exceeding `256` nested calls stops execution with a `call stack overflow` runtime error.

### 6.3 Interaction with `.grab.html` templates (GRAB 0.2)

- Top-level variables bound via `set` (such as `set total to calculate_total using 100, 0.08`) are included in the final environment snapshot passed to `.grab.html` templates, allowing templates to render function results via `{total}` with zero changes to `.grab.html` syntax.

---

## 7. Diagnostics and profiling baseline

### 7.1 Diagnostic categories in GRAB 0.3

In addition to all GRAB 0.1 and GRAB 0.2 diagnostic categories, GRAB 0.3 defines the following source-located diagnostics:

- **Pre-execution validation diagnostics (prevent program execution and produce no output):**
  - `duplicate definition` — duplicate function name or duplicate parameter name in `make`.
  - `unknown function` — calling a function name that is not defined.
  - `function arity mismatch` — calling a function with the wrong number of arguments.
  - `invalid give back` — `give back` used at the top level outside any `make` function.
- **Runtime diagnostics:**
  - `division by zero` — `divided by` or `modulo` with a zero right operand.
  - `absent field or key` — reading a missing record field (`.`) or missing map key (`at`).
  - `index out of bounds` — list index in `at` is `< 0` or `>= len(list)`.
  - `negative repeat count` — `repeat <count> times` where `<count> < 0`.
  - `call stack overflow` — nested function calls exceed 256 frames.
  - `execution limit exceeded` — loop iterations exceed the step budget.

### 7.2 Initial profiling and benchmark harness (`ADR-016`)

Per `ADR-016` (Evidence-based performance goals), GRAB 0.3 introduces a reproducible Go benchmark suite (`tests/benchmark_test.go` or `internal/interpreter/benchmark_test.go`) using `testing.B` and `b.ReportAllocs()` covering four baseline workloads:
1. **Straight-line bindings & text joining** (GRAB 0.1 baseline).
2. **Numeric arithmetic & conditional branching** (`if` / `otherwise`).
3. **Collection iteration & lookup** (`for each` over lists/records).
4. **Function call & recursive frame allocation** (`make` / `using` / `give back`).

---

## 8. Conformance cases for GRAB 0.3

### Case 1: Conditions, arithmetic, and functions

Source:
```grab
make calculate_total using price, rate
  set tax to price times rate
  give back price plus tax
end

set subtotal to 100
set total to calculate_total using subtotal, 0.08

if total is greater than 100
  show "Taxed total: "
  show total
otherwise
  show "No tax"
end
```

Expected standard output:
```text
Taxed total: 
108.00
```

### Case 2: Loops and collection lookup

Source:
```grab
set product to record {sku: "A-17", name: "GRAB Starter Kit", tags: ["new", "sale"]}
show product.name joined with " (" joined with product.sku joined with ")"
show "First tag: " joined with (product.tags at 0)

set count to 0
for each tag in product.tags
  set count to count plus 1
  show "Tag: " joined with tag
end
show count
```

Expected standard output:
```text
GRAB Starter Kit (A-17)
First tag: new
Tag: new
Tag: sale
2
```

### Case 3: Function arity mismatch (pre-execution error case)

Source:
```grab
make add using a, b
  give back a plus b
end

show add using 1
```

Expected result: `function arity mismatch` diagnostic on line 5 before execution begins; the program produces no output.

---

## 9. Owner approval record for GRAB 0.3

The project owner approved the following GRAB 0.3 decisions on November 11, 2026:

- [x] Approve English-inspired operators (`plus`, `minus`, `times`, `divided by`, `modulo`, `is`, `is not`, `[is] greater than`, `[is] less than`, `[is] at least`, `[is] at most`, `not`, `and`, `or`) and `( ... )` grouping.
- [x] Approve exact numeric arithmetic rules (`integer` + `decimal` produces exact `decimal`; `float` remains separate with no implicit mixing; `division by zero` is a runtime error).
- [x] Approve collection lookup syntax: `record.field` for records, `collection at index_or_key` for lists and maps, and `collection has key_or_field` for membership checks.
- [x] Approve control-flow syntax: `if ... [otherwise if ...] [otherwise ...] end`, `repeat <count> times ... end`, `for each <item> in <collection> ... end`, and `while <condition> ... end` with strict Boolean conditions and a bounded loop step limit.
- [x] Approve function syntax (`make <name> [using <params>] ... end`, `give back [expr]`, `<name> using <args>`, `call <name> [using <args>]`) with isolated lexical call frames, pre-execution arity checking, and a 256-frame recursion limit.
- [x] Approve the initial Go benchmark and allocation profiling harness (`ADR-016`).
