# GRAB Language Specification 001

## Status and authority

**Status:** APPROVED for GRAB 0.1. Project-owner approval recorded November 10, 2026.
**Target milestone:** GRAB 0.1
**Owner approval date:** November 10, 2026

This specification defines a deliberately small language for the first interpreter milestone. It follows Blueprint v1.0 as the architectural baseline and the recorded architecture decisions in `docs/adr/ADR-DECISION-REGISTER.md`. Blueprint v1.1 remains unapproved as a whole. The corrected repository structure uses the existing `internal/` packages.

The value scope, syntax, runtime semantics, diagnostics, and conformance cases in this document are approved for GRAB 0.1, except for items explicitly deferred below. This document does not claim that any lexer, parser, interpreter, CLI, or conformance test is implemented.

## Owner-approved value scope

On November 10, 2026, the project owner approved this planning scope for GRAB 0.1:

- Include text, Boolean, `nothing`, arbitrary-precision integer, exact decimal, binary floating-point, bytes, lists, maps, and records as portable core value kinds.
- Distinguish `nothing` from an absent map key or record field.
- Keep database connections and vendor-specific database values for later milestones.
- Design future database adapters to avoid silent truncation or lossy conversion; report unsupported conversions explicitly.

## Owner-approved syntax

On November 10, 2026, the project owner approved the 0.1 syntax in Sections 3-4 as written, including identifiers and keywords, line and comment rules, string escapes, numeric and byte literal forms, list/map/record forms, and the statement and expression grammar. The runtime and diagnostic rules were approved separately as written on November 10, 2026. Numeric operators and equality remain deferred beyond 0.1.

The approved syntax and value scope do not include database connections, vendor-specific values, HTML templates, multiline strings, or interpolation in GRAB 0.1. On November 10, 2026, the owner separately approved a future HTML-template milestone, with `.grab.html` as the preferred direction; its goals and remaining design work are recorded in `docs/adr/ADR-DECISION-REGISTER.md`. This later milestone does not change GRAB 0.1.

## 1. Purpose and scope

GRAB 0.1 establishes a small, deterministic language that can bind portable values, combine text, and display results from a `.grab` source file. Its value model should support later database adapters without making database connections part of the language foundation.

The GRAB 0.1 subset contains:

- UTF-8 source text.
- Single-line comments and double-quoted text literals.
- Case-sensitive identifiers.
- `set` statements that bind or rebind a value.
- `show` statements that display a value.
- Text expressions using variables and the `joined with` operator.
- Portable values: text, Boolean, `nothing`, arbitrary-precision integers, exact decimals, binary floating-point numbers, bytes, lists, maps, and records.
- Source-located lexical, syntax, and name-resolution diagnostics.
- A command-line path for running a `.grab` program and a conformance suite for the specified behavior.

GRAB 0.1 excludes numeric operators and coercions; conditions; loops; functions; user-defined types; modules and imports; exceptions; concurrency; server or browser behavior; database connections and vendor-specific database types; and bytecode or IR execution. These features require separate owner-approved language design.

## 2. Terminology

- **Source file:** UTF-8 text containing a GRAB program.
- **Statement:** A `set` or `show` operation on one logical source line.
- **Portable value:** A value represented by GRAB's database-neutral core value model, independently of any database vendor.
- **Nothing:** GRAB's explicit null value. It is distinct from an absent map key or record field, which is not itself a value.
- **Record:** A collection of named fields. A missing field is distinct from a present field whose value is `nothing`.
- **Text value:** A sequence of Unicode scalar values produced by decoding a text literal or reading a variable.
- **Source span:** A half-open range in the source, with a one-based starting line and column and an end position.
- **Conformance case:** A source program paired with its expected output or expected diagnostic category and location.

The words **MUST**, **MUST NOT**, **SHOULD**, and **MAY** in this approved specification are normative requirements.

## 3. Approved source and lexical rules

### 3.1 Encoding and line endings

1. A source file is UTF-8. Invalid UTF-8 is a lexical error.
2. LF and CRLF are accepted as line endings. Each represents one logical newline.
3. A newline separates statements. Blank lines are ignored.
4. Spaces and tabs outside strings separate tokens. Indentation has no syntactic meaning.
5. The punctuation tokens in this subset are `[`, `]`, `{`, `}`, `,`, and `:`. Braces in map and record literals do not form statement blocks.
6. A semicolon does not separate statements and is not valid syntax in this subset.
7. A final newline is optional.

### 3.2 Comments

A `#` outside a string starts a comment that continues through, but does not include, the next newline. A comment-only line is ignored. The `#` character inside a string is ordinary text.

### 3.3 Identifiers and keywords

The identifier form is ASCII-only:

```text
[A-Za-z_][A-Za-z0-9_]*
```

Identifiers are case-sensitive. The words `set`, `to`, `show`, `joined`, `with`, `yes`, `no`, `nothing`, and `record` are reserved and cannot be used as identifiers. No other words are reserved in this subset.

### 3.4 Text literals

1. A text literal begins and ends with a double quote (`"`) and cannot contain a raw newline.
2. Literal content is Unicode text encoded in UTF-8. There is no interpolation or implicit evaluation inside a string.
3. The escape sequences are `\\`, `\"`, `\n`, `\r`, and `\t`, representing backslash, quote, line feed, carriage return, and tab respectively.
4. An incomplete or unsupported escape and an unterminated string are lexical errors.
5. The language does not normalize Unicode text. Literal code points are preserved when decoded and displayed.

### 3.5 Approved value literal syntax

The following syntax is approved for 0.1. Runtime constraints on these values are described separately in Section 5 and remain under review:

- Boolean values use `yes` and `no`, matching Blueprint v1.0's candidate vocabulary. `nothing` is the null-value literal.
- Integer literals are base-10 digits with an optional leading minus and represent arbitrary-precision signed integers.
- Decimal literals have a decimal point and no exponent; they represent exact base-10 values and preserve the number of fractional digits as scale. Float literals have an `e` or `E` exponent and represent IEEE 754 binary64 values. A leading minus is permitted for either kind. These notations keep decimal and binary floating-point distinct.
- Byte literals use `b"..."`. Printable ASCII characters other than quote and backslash represent their byte values. `\\`, `\"`, and `\xHH` escapes represent backslash, quote, and one byte written as two hexadecimal digits. Non-ASCII bytes must use `\xHH` escapes.
- Lists use square brackets and comma-separated expressions, for example `[1, "two", nothing]`. Empty lists are written `[]`; a trailing comma is allowed.
- Maps use braces with quoted text keys and values, for example `{"sku": "A-17", "active": yes}`. Empty maps are written `{}`; a trailing comma is allowed.
- Records use the keyword `record`, braces, and identifier field names, for example `record {sku: "A-17", active: yes}`. Empty records are written `record {}`; a trailing comma is allowed.
- List, map, and record literals are single-line in 0.1. Whitespace is allowed between their tokens; newlines are not allowed inside them.
- Date/time values, UUIDs, spatial values, enums, database-specific numeric widths, and vendor-defined values are not in the 0.1 core value set. Later standard-library or database adapters may add typed support for them.

## 4. Approved grammar

The following EBNF describes the approved 0.1 grammar. A newline token is required between adjacent statements; leading, trailing, blank-line, and comment-only newlines are permitted. Runtime and display behavior is specified separately in Section 5 and remains subject to its own approval.

```ebnf
program          = { newline }, [ statement, { newline, { newline }, statement } ],
                   { newline } ;

statement        = set_statement | show_statement ;

set_statement    = "set", identifier, "to", expression ;
show_statement   = "show", expression ;

expression       = primary, { "joined", "with", primary } ;
primary          = text_literal | boolean_literal | nothing_literal
                 | integer_literal | decimal_literal | float_literal
                 | bytes_literal | list_literal | map_literal
                 | record_literal | identifier ;

boolean_literal  = "yes" | "no" ;
nothing_literal  = "nothing" ;
integer_literal  = [ "-" ], digit, { digit } ;
decimal_literal  = [ "-" ], digit, { digit }, ".", digit, { digit } ;
float_literal    = [ "-" ], digit, { digit }, [ ".", digit, { digit } ],
                   ( "e" | "E" ), [ "+" | "-" ], digit, { digit } ;
bytes_literal    = "b", U+0022, { byte_character | byte_escape }, U+0022 ;
byte_character  = printable ASCII except U+0022 and U+005C ;
byte_escape     = U+005C, ( U+005C | U+0022 | "x", hex_digit, hex_digit ) ;
list_literal     = "[", [ expression, { ",", expression }, [ "," ] ], "]" ;
map_literal      = "{", [ map_entry, { ",", map_entry }, [ "," ] ], "}" ;
map_entry        = text_literal, ":", expression ;
record_literal   = "record", "{", [ record_field, { ",", record_field }, [ "," ] ], "}" ;
record_field     = identifier, ":", expression ;

identifier       = letter_or_underscore, { letter | digit | "_" } ;
text_literal     = U+0022, { text_character | escape }, U+0022 ;
text_character   = any Unicode scalar value except U+0022, U+005C, U+000A, or U+000D ;
escape           = U+005C, ( U+005C | U+0022 | "n" | "r" | "t" ) ;

newline          = LF | CRLF ;
letter           = "A"…"Z" | "a"…"z" ;
digit            = "0"…"9" ;
hex_digit        = digit | "A"…"F" | "a"…"f" ;
letter_or_underscore = letter | "_" ;
```

In the grammar above, comments and horizontal whitespace may appear between tokens according to Section 3. Newlines are not permitted inside an expression. Parentheses and other operators are not part of this subset.

## 5. Approved value and statement semantics

### 5.1 Values and variables

1. The core value kinds are text, Boolean, nothing, integer, exact decimal, binary floating-point, bytes, list, map, and record.
2. Each program has one environment containing its variable bindings. There are no nested scopes in this subset.
3. `set name to expression` evaluates the expression, then binds its result to `name`. `set` may replace an existing binding, including with a value of another kind.
4. Name resolution scans statements in source order. Before each statement's binding is added, every identifier reference in its expression must already have a binding. A reference before the first binding is an unbound-variable name-resolution error, even if a later statement binds that name.
5. An identifier in an expression reads the current binding. Reading an unbound identifier is a name-resolution error.
6. Variable names are case-sensitive; `name` and `Name` are distinct.

### 5.2 Portable value behavior

1. Integer values have arbitrary precision in the language model; no fixed-width overflow or wraparound occurs in the language value itself.
2. Exact decimal values preserve their mathematical base-10 value and scale. Binary floating-point values are a separate IEEE 754 binary64 kind. No implicit conversion occurs between numeric kinds.
3. A map key or record field may be absent. Absence is distinct from a present field whose value is `nothing`.
4. Lists preserve element order. Maps preserve insertion order. Records preserve field order as written. Duplicate map keys and duplicate record fields are rejected.
5. Lists, maps, and records are immutable after construction in this subset. `set` may rebind a variable to a new value; there are no element or field mutation operations in 0.1.
6. Bytes are not automatically decoded as text. A conversion between bytes and text requires an explicit operation, which is outside GRAB 0.1.
7. The language value model is independent of Go and database driver representations. Later adapters perform explicit conversions and must not silently truncate integers, round exact decimals, or stringify unsupported values.

### 5.3 Text concatenation

1. `left joined with right` concatenates the text of its operands without an inserted separator.
2. Repeated `joined with` operations associate left to right.
3. Operands are evaluated from left to right.
4. The `joined with` operator accepts text values only; other value kinds require an explicit conversion, which is outside GRAB 0.1. Supplying another value kind is a runtime type error with a source span.

### 5.4 Display

1. `show expression` evaluates the expression and writes a deterministic representation of its value to standard output.
2. Each successful `show` writes exactly one LF after the value, independent of the host platform.
3. A top-level text value is displayed without quotation marks. Within composite values, text uses the escaped double-quoted text-literal form. Canonical displays are: `yes` / `no` for booleans; `nothing` for null; base-10 digits for integers; decimal notation preserving scale for exact decimals; shortest round-trip decimal notation for finite binary64 values, using lowercase `e` and an explicit decimal marker when needed to distinguish a floating-point value from an integer; `b"..."` with quote and backslash escaped and non-printable bytes escaped as lowercase `\xhh`; and the corresponding list, map, and record literal forms with one space after commas and colons. Map entries and record fields display in insertion/source order.
4. If expression evaluation fails, that `show` writes no partial value.

### 5.5 Execution order and errors

1. Statements execute in source order.
2. A `set` evaluates its complete expression before changing its binding.
3. The parser processes the complete source before interpretation begins. A lexical or syntax error therefore prevents program execution and produces no program output.
4. The name-resolution phase checks all variable references before interpretation. A name-resolution error prevents program execution and produces no program output.
5. Execution stops on the first runtime error. Output from earlier successful `show` statements is retained; no rollback is implied.
6. There is no error recovery within a program in this subset.

The pre-execution name-resolution boundary and runtime-error behavior above are approved for 0.1.

## 6. Approved diagnostics and source locations

1. Diagnostic locations are one-based by line and column. Columns count Unicode scalar values, with tabs counting as one column.
2. Spans identify the smallest relevant token or expression available to the phase reporting the error. The end position is exclusive.
3. Diagnostics have a stable category and source span. Human-readable wording and CLI presentation are implementation/CLI contract details and are not frozen here.
4. The minimum diagnostic categories are:
   - invalid source encoding;
   - invalid character or escape;
   - unterminated text literal;
   - unexpected token or malformed statement;
   - unbound variable reference;
   - runtime type error, including use of a non-text value with `joined with`.
5. A diagnostic must not invent a location for an error where the source position is unavailable; the implementation should report the nearest meaningful span.

## 7. Approved first conformance program

Source:

```grab
set name to "Allen"
set language to "GRAB"
show "Welcome to " joined with language
show "Hello " joined with name
```

Expected standard output, including the final newline:

```text
Welcome to GRAB
Hello Allen
```

This is the approved first vertical slice. It is not a claim that it has been implemented.

## 8. Approved error conformance cases

### Unterminated string

Source:

```grab
show "Hello
```

Expected result: an unterminated-text-literal lexical diagnostic at the opening quote on line 1. The program produces no output.

### Unbound variable

Source:

```grab
show missing
```

Expected result: an unbound-variable diagnostic spanning `missing` on line 1. Under the pre-execution name-resolution rule, the program produces no output.

Exact diagnostic wording and CLI exit codes are not specified by this language draft.

## 9. Approved portable value conformance case

Source:

```grab
set product to record {sku: "A-17", active: yes, stock: 12, price: 19.95, ratio: 1.25e-2, labels: ["new", "sale"], payload: b"\x00\xff", note: nothing}
show product
```

Expected output under the canonical display rules:

```text
record {sku: "A-17", active: yes, stock: 12, price: 19.95, ratio: 0.0125, labels: ["new", "sale"], payload: b"\x00\xff", note: nothing}
```

This case demonstrates values and display only. Numeric operators, collection lookup/mutation, database access, and vendor-specific types remain outside 0.1.

## 10. Explicitly deferred language design

The following are outside this 0.1 proposal and require separate design before inclusion:

- numeric operators, equality and coercion rules;
- date/time, UUID, spatial, enum and vendor-defined database values;
- mutability distinctions beyond the approved `set` rebinding behavior, and any nested scope model;
- conditions, loops, functions, user-defined types and objects, and type inference;
- modules, imports, packages and server/client module separation;
- exceptions and user-defined error handling;
- concurrency syntax and task-group semantics;
- filesystem, network, database, server, UI and browser capabilities;
- database connections, schema inspection, driver selection, vendor codecs and database-specific type mapping;
- HTML template files, automatic HTML output, interpolation, and `show block` / `show html` template constructs are outside GRAB 0.1. A separate future `.grab.html` template milestone was approved on November 10, 2026; see the architecture register for its approved goals and unresolved contract details;
- mandatory IR, bytecode, VM, optimization and browser compilation.

## 11. Owner approval record

The project owner approved the following GRAB 0.1 decisions on November 10, 2026:

- [x] Include text, Boolean, `nothing`, arbitrary-precision integer, exact decimal, binary floating-point, bytes, lists, maps and records in the portable 0.1 value model.
- [x] Use `yes`, `no`, and `nothing` as the Boolean and null literals.
- [x] Use base-10 integer literals, exact decimal literals with a point and no exponent, and binary64 float literals with an exponent marker.
- [x] Use `b"..."` with `\xHH` byte escapes.
- [x] Use `[ ... ]` for lists, `{ "key": value }` for maps, and `record { field: value }` for records, with optional trailing commas and no newlines inside literals.
- [x] Reject duplicate map keys and record fields.
- [x] Use the deterministic `show` forms for numbers, bytes and collections; preserve map insertion order and record field order.
- [x] Keep lists, maps, and records immutable in 0.1, with `set` rebinding as the only state change.
- [x] Reject non-text operands to `joined with` at runtime with a source-located type error.
- [x] Defer numeric operators, equality and coercion beyond 0.1; no numeric operators or equality expressions are included in this specification.
- [x] Distinguish an absent field/key from a present field/key whose value is `nothing`.
- [x] Keep database connections and vendor-specific values for later milestones, with adapters designed to avoid silent loss or truncation.
- [x] Allow `set` to rebind an existing variable, including to a value of another kind.
- [x] Use ASCII-only, case-sensitive identifiers and reserve the listed words.
- [x] Treat newlines as statement separators and indentation as insignificant.
- [x] Use `#` for comments and the listed string escapes; text and container literals do not span lines, and strings do not interpolate.
- [x] Use `joined with` as the sole expression operator in the 0.1 grammar.
- [x] Evaluate chained `joined with` expressions from left to right.
- [x] Have `show` append one LF to each displayed value.
- [x] Resolve unbound names before execution, so such errors produce no program output.
- [x] Use one-based Unicode-scalar columns and half-open source spans.

The approved specification defines GRAB 0.1 language behavior. User-facing diagnostic wording and CLI exit codes remain CLI design details; features in Section 10 and the separately approved future HTML-template milestone are not part of 0.1.
