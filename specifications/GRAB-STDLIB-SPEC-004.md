# GRAB Standard Library, Error Model & Permissions Specification 004

## Status and authority

**Status:** APPROVED for GRAB 0.4. Project-owner approval recorded November 11, 2026.
**Target milestone:** GRAB 0.4 — Core standard library, error model, and permissions
**Owner approval date:** November 11, 2026
**Prerequisite baseline:** `specifications/GRAB-LANGUAGE-SPEC-001.md` (GRAB 0.1), `specifications/GRAB-LOCALHOST-SPEC-002.md` (GRAB 0.2), `specifications/GRAB-LANGUAGE-SPEC-003.md` (GRAB 0.3), and `docs/adr/ADR-DECISION-REGISTER.md`

This specification defines the language, runtime, security, and CLI expansion for **GRAB 0.4**, settling the design questions in `docs/ROADMAP.md` and Blueprint v1.0 Sections 9–10 (`use` standard library modules, `try` / `catch` / `raise` structured error handling, stable error codes and diagnostic modes, capability-based filesystem permissions, and `grab check` / `grab format`).

It does not claim that any GRAB 0.4 feature is implemented yet. Implementation begins only after explicit project-owner approval of this specification.

---

## 1. Purpose and scope

GRAB 0.4 equips the GRAB language with essential standard-library utilities, structured error recovery, capability-gated file access, and developer tooling (`grab check` and `grab format`) while preserving 100% backward compatibility with GRAB 0.1, 0.2, and 0.3 programs:

- **Core standard library modules (`use <module>`):** Built-in `text`, `math`, `collections`, `dates`, and `files` modules imported explicitly via `use <module>` and called with `<module>.<function> using <args>` (or `call <module>.<function>`).
- **Number/value-to-text formatting (`text.from`):** Allows `.grab` programs to convert numbers (such as `19.95`) and other portable values into text (so `"$" joined with (text.from using product.price)` produces `"$19.95"` directly inside `.grab`).
- **Structured error model (`try` / `catch` / `raise`):** Recoverable runtime error handling with structured error records (`code`, `category`, `message`, `line`, `column`), call stack traces, stable `GRAB-E...` error codes, and three presentation modes (`beginner`, `developer`, `production`).
- **Capability-based filesystem permissions (`ADR-009`):** Default-deny filesystem access for `files.*` unless explicitly granted via `--allow-read <dir>` and/or `--allow-write <dir>`, with strict directory confinement and mandatory denial of sensitive paths (`.git`, `.env`, `.ssh`, key files).
- **Developer CLI tooling:** `grab check <file.grab> [template.grab.html]` for static validation without execution, and `grab format [--write] <file.grab>` for canonical code formatting.

### Explicitly outside GRAB 0.4

GRAB 0.4 does **not** include:
- User-authored multi-file packages, external package registries (`grab.json` / `grab install`), or third-party module imports;
- Outbound HTTP client requests, JSON codecs, webhooks, or `do together` / `wait for` async task groups (planned for **GRAB 0.5**);
- Persistent multi-route production HTTP server or reverse-proxy supervision (planned for **GRAB 0.6**);
- Database drivers or SQL queries (planned for **GRAB 0.7**);
- Bytecode VM or intermediate representation (`ADR-013`, planned for **GRAB 0.9**).

---

## 2. Lexical and grammar additions

### 2.1 New reserved keywords in GRAB 0.4

In addition to the GRAB 0.1–0.3 reserved keywords, GRAB 0.4 reserves four words from Blueprint v1.0's candidate registry:
- **Modules:** `use`
- **Error handling:** `try`, `catch`, `raise`

*(Built-in module names `text`, `math`, `collections`, `dates`, and `files` and their function names are not globally reserved keywords; a module name becomes bound in the file's module namespace only when `use <module>` is declared at the top level.)*

### 2.2 Formal EBNF grammar extensions

```ebnf
program            = { newline }, [ top_item, { newline, { newline }, top_item } ],
                     { newline } ;

top_item           = use_decl | function_def | statement ;

use_decl           = "use", identifier ;

statement          = set_statement
                   | show_statement
                   | if_statement
                   | repeat_statement
                   | for_each_statement
                   | while_statement
                   | try_statement
                   | raise_statement
                   | give_back_statement
                   | call_statement ;

try_statement      = "try", newline, block_body,
                     "catch", identifier, newline, block_body,
                     "end" ;

raise_statement    = "raise", expression ;

call_statement     = "call", callable_target, [ "using", arg_list ]
                   | callable_target, "using", arg_list ;

call_expr          = "call", callable_target, [ "using", arg_list ]
                   | callable_target, "using", arg_list ;

callable_target    = identifier, [ ".", identifier ] ;
```

### 2.3 Module import rules (`use`)

1. `use <module>` declarations may appear only at the top level of a `.grab` file.
2. The supported standard-library module names in GRAB 0.4 are `text`, `math`, `collections`, `dates`, and `files`.
3. Importing an unknown module name (e.g. `use unknown_mod`), importing the same module twice, or colliding a `use` module name with a top-level variable or `make` function name is rejected during pre-execution validation.
4. Calling `<module>.<func>` without a corresponding `use <module>` declaration (or calling an unknown function or wrong argument count on an imported module) is rejected during pre-execution validation before any statement executes.

---

## 3. Core standard library modules

All standard library functions are pure or deterministically bounded, take immutable portable `runtime.Value` arguments, and return a portable `runtime.Value`.

### 3.1 `text` module (`use text`)

| Function & signature | Behavior and return value |
| --- | --- |
| `text.from using value` | Converts any portable GRAB value (`integer`, `decimal`, `float`, `boolean`, `nothing`, `text`, `bytes`, `list`, `map`, `record`) into its canonical GRAB `text` representation (e.g. `text.from using 19.95` $\to$ `"19.95"`). |
| `text.length using s` | Requires `text` `s`. Returns an `integer` count of Unicode scalar values (runes) in `s`. |
| `text.upper using s` | Requires `text` `s`. Returns `s` converted to Unicode uppercase. |
| `text.lower using s` | Requires `text` `s`. Returns `s` converted to Unicode lowercase. |
| `text.trim using s` | Requires `text` `s`. Returns `s` with leading and trailing Unicode whitespace removed. |
| `text.contains using s, substr` | Requires `text` `s`, `substr`. Returns `yes` if `substr` occurs in `s`, else `no`. |
| `text.starts_with using s, prefix` | Requires `text` `s`, `prefix`. Returns `yes` if `s` starts with `prefix`, else `no`. |
| `text.ends_with using s, suffix` | Requires `text` `s`, `suffix`. Returns `yes` if `s` ends with `suffix`, else `no`. |
| `text.replace using s, old, new` | Requires `text` `s`, `old`, `new`. Replaces all non-overlapping occurrences of `old` in `s` with `new`. |
| `text.split using s, sep` | Requires `text` `s`, `sep`. Returns a `list` of `text` parts split around `sep`. |
| `text.join using items, sep` | Requires `list` of `text` `items` and `text` `sep`. Concatenates elements with `sep` between adjacent items. |
| `text.to_integer using s` | Requires `text` `s`. Parses an optional `-` and base-10 digits into an `integer`, or raises `GRAB-E309` (`standard library error`) if invalid. |
| `text.to_decimal using s` | Requires `text` `s`. Parses an exact decimal literal string (e.g. `"19.95"`) into a `decimal`, or raises `GRAB-E309` if invalid. |

### 3.2 `math` module (`use math`)

| Function & signature | Behavior and return value |
| --- | --- |
| `math.abs using n` | Requires `integer`, `decimal`, or `float`. Returns the absolute value preserving the input's numeric kind and decimal scale. |
| `math.min using a, b` | Requires compatible numbers (`integer`/`decimal` together, or `float` with `float`). Returns the smaller value. |
| `math.max using a, b` | Requires compatible numbers (`integer`/`decimal` together, or `float` with `float`). Returns the larger value. |
| `math.clamp using val, low, high` | Requires compatible numbers with `low <= high`. Clamps `val` to `[low, high]`. |
| `math.round using n, scale` | Requires `integer` or `decimal` `n` and non-negative `integer` `scale` (`0 <= scale <= 16`). Rounds `n` half-even to `scale` fractional digits (returns `integer` if `scale == 0`, else `decimal` with exact `scale`). |
| `math.floor using n` | Requires `integer`, `decimal`, or `float`. Returns the greatest `integer` less than or equal to `n`. |
| `math.ceil using n` | Requires `integer`, `decimal`, or `float`. Returns the least `integer` greater than or equal to `n`. |

### 3.3 `collections` module (`use collections`)

All `collections` functions preserve immutability by returning a new collection value without mutating the input collection:

| Function & signature | Behavior and return value |
| --- | --- |
| `collections.length using coll` | Requires `list`, `map`, `record`, or `bytes`. Returns the `integer` number of elements/entries/bytes. |
| `collections.append using list_val, item` | Requires `list` `list_val`. Returns a new `list` with `item` appended at the end. |
| `collections.slice using list_val, start, end` | Requires `list` `list_val` and integers `0 <= start <= end <= length`. Returns the sublist `list_val[start:end]` (raises `GRAB-E304` `index out of bounds` if out of range). |
| `collections.keys using coll` | Requires `map` or `record`. Returns a `list` of `text` key/field names in insertion/source order. |
| `collections.put using map_val, key, val` | Requires `map` `map_val` and `text` `key`. Returns a new `map` with `key` bound to `val` (updating in place of existing key order if present, or appending if new). |
| `collections.remove using map_val, key` | Requires `map` `map_val` and `text` `key`. Returns a new `map` with `key` removed (or an identical copy if absent). |

### 3.4 `dates` module (`use dates`)

Dates and UTC timestamps are represented as portable GRAB records with fields:
`record {iso: "2026-11-11T14:30:00Z", year: 2026, month: 11, day: 11, hour: 14, minute: 30, second: 0, unix: 1794407400}`

| Function & signature | Behavior and return value |
| --- | --- |
| `call dates.now_utc` | Zero arguments. Returns the current UTC timestamp as a date `record`. |
| `dates.parse_iso using s` | Requires `text` `s` in RFC 3339 (`YYYY-MM-DDTHH:MM:SSZ`) or ISO date (`YYYY-MM-DD`, interpreted as `00:00:00Z`) format. Returns a UTC date `record`, or raises `GRAB-E309` if malformed. |
| `dates.from_unix using sec` | Requires `integer` `sec` (Unix epoch seconds). Returns the corresponding UTC date `record`. |
| `dates.add_days using date_rec, days` | Requires a valid date `record` and `integer` `days`. Returns a new UTC date `record` shifted by `days`. |
| `dates.diff_seconds using a, b` | Requires two date `record` values. Returns `a.unix minus b.unix` as an `integer`. |

### 3.5 `files` module (`use files`)

All `files` operations are subject to the Capability-Based Filesystem Permissions in Section 5.

| Function & signature | Behavior and return value |
| --- | --- |
| `files.read_text using path` | Requires `text` `path` and read capability covering `path`. Reads the file, validates UTF-8, normalizes `CRLF` to `LF`, and returns its `text` contents (raises `GRAB-E401` if permission denied, or `GRAB-E309` if missing/invalid UTF-8). |
| `files.write_text using path, content` | Requires `text` `path`, `text` `content`, and write capability covering `path`. Writes `content` encoded as UTF-8 and returns `nothing`. |
| `files.exists using path` | Requires `text` `path` and read capability covering `path`. Returns `yes` if a regular file or directory exists at `path`, or `no` if it does not exist. |
| `files.list_dir using path` | Requires `text` `path` and read capability covering `path`. Returns a `list` of `text` entry names in `path` sorted lexicographically (excluding sensitive hidden entries). |

---

## 4. Structured error model (`try` / `catch` / `raise`) and diagnostics

### 4.1 `try` / `catch` / `end` execution semantics

```grab
try
  set ratio to 100 divided by count
catch err
  show "Handled " joined with err.code
  set ratio to 0
end
```

1. The `try` body executes in the current scope. If no error occurs inside `try`, the `catch` block is skipped.
2. If a **recoverable runtime error** occurs inside the `try` body (including inside any function called from `try`), execution unwinds immediately to the innermost enclosing `catch <ident>` block.
3. In the `catch <ident>` block, `<ident>` is bound locally to an immutable error `record`:
   ```grab
   record {
     code: "GRAB-E302",
     category: "division by zero",
     message: "division by zero at line 2, column 16",
     line: 2,
     column: 16
   }
   ```
4. **Exhaustive variable binding across `try` / `catch`:** Just like `if` / `otherwise`, if a variable (such as `ratio` above) is bound via `set` in both the `try` block and the `catch` block, pre-execution validation recognizes it as guaranteed-bound after `end`.
5. **Non-catchable fatal limits:** Resource-protection errors (`GRAB-E306` `call stack overflow` and `GRAB-E307` `execution limit exceeded`) cannot be caught by `try` / `catch` and always terminate program execution immediately.

### 4.2 `raise <expression>`

1. `raise <expression>` evaluates `<expression>`:
   - If `<expression>` evaluates to a `text` value `msg`, it raises a recoverable error with `code: "GRAB-E308"`, `category: "user raised error"`, and `message: msg`.
   - If `<expression>` evaluates to a `record` containing a `text` `message` field (and optional `text` `code` field), it raises a recoverable error using that custom `code` (defaulting to `"GRAB-E308"`) and `message`.
   - Passing any other value kind to `raise` raises a `runtime type error` (`GRAB-E301`).

### 4.3 Stable error codes and diagnostic modes

| Code | Category | Phase |
| --- | --- | --- |
| `GRAB-E001` | `invalid source encoding` | Lexical |
| `GRAB-E002` | `invalid character` | Lexical |
| `GRAB-E003` | `invalid escape` | Lexical |
| `GRAB-E004` | `unterminated text literal` | Lexical |
| `GRAB-E005` | `unterminated byte literal` | Lexical |
| `GRAB-E006` | `malformed number` | Lexical |
| `GRAB-E007` | `invalid byte literal character` | Lexical |
| `GRAB-E101` | `unexpected token or malformed statement` | Syntax |
| `GRAB-E102` | `duplicate map key` | Syntax |
| `GRAB-E103` | `duplicate record field` | Syntax |
| `GRAB-E201` | `unbound variable reference` | Pre-execution validation |
| `GRAB-E202` | `duplicate definition` | Pre-execution validation |
| `GRAB-E203` | `unknown function` (or unknown module) | Pre-execution validation |
| `GRAB-E204` | `function arity mismatch` | Pre-execution validation |
| `GRAB-E205` | `invalid give back` | Pre-execution validation |
| `GRAB-E301` | `runtime type error` | Runtime |
| `GRAB-E302` | `division by zero` | Runtime |
| `GRAB-E303` | `absent field or key` | Runtime |
| `GRAB-E304` | `index out of bounds` | Runtime |
| `GRAB-E305` | `negative repeat count` | Runtime |
| `GRAB-E306` | `call stack overflow` | Runtime (fatal) |
| `GRAB-E307` | `execution limit exceeded` | Runtime (fatal) |
| `GRAB-E308` | `user raised error` | Runtime |
| `GRAB-E309` | `standard library error` | Runtime |
| `GRAB-E401` | `permission denied` | Runtime / Capability |

#### Three diagnostic formatting modes (`--diagnostics beginner|developer|production`)
- **`developer` (default):** Includes the error message, line and column, stable error code, and call stack frames when failure occurs inside a function.
- **`beginner`:** Includes the developer diagnostic plus a plain-English explanation and a suggested fix hint.
- **`production`:** Outputs only the stable error code (`GRAB-E...`) and category without exposing filesystem paths, source code snippets, or internal variable values.

---

## 5. Capability-based filesystem permissions (`ADR-009`)

1. **Default-deny:** By default, `grab run` and `grab serve` execute with **zero filesystem capabilities**. Any call to `files.read_text`, `files.write_text`, `files.exists`, or `files.list_dir` without an active capability raises `GRAB-E401` (`permission denied`).
2. **Explicit CLI capability flags:**
   - `--allow-read <dir>`: Grants read capability (`files.read_text`, `files.exists`, `files.list_dir`) confined to `<dir>` and its regular subdirectories.
   - `--allow-write <dir>`: Grants write capability (`files.write_text`) confined to `<dir>` and its regular subdirectories.
3. **Path confinement and mandatory sensitive-path denial:**
   - Every path argument to `files.*` is cleaned and resolved relative to the allowed capability root.
   - Any path containing NUL bytes, escaping the capability root via `..` or symbolic links, or targeting a non-regular file (for `read_text`/`write_text`) is rejected with `GRAB-E401` (`permission denied`).
   - **Mandatory sensitive-path blocklist:** Even within an allowed directory, any path segment matching `.git`, `.env` (or `.env.*`), `.ssh`, `id_rsa`, `id_ed25519`, `*.pem`, or `*.key` is unconditionally denied with `GRAB-E401`.
   - `files.read_text` enforces a maximum file size of `10 MiB` (`10,485,760` bytes) to prevent memory exhaustion.

---

## 6. CLI subcommands (`grab check` and `grab format`)

### 6.1 `grab check`

```text
grab check [--diagnostics beginner|developer|production] <file.grab> [template.grab.html]
```

- Lexes, parses, and runs full pre-execution validation (name resolution, `use` module validation, function registration, arity checking, and optional `.grab.html` template context and binding validation) **without executing** the program.
- Writes no stdout on success and exits with code `0`; on error, writes the formatted diagnostic to `stderr` and exits with code `1`.

### 6.2 `grab format`

```text
grab format [--write] <file.grab>
```

- Lexes and parses `<file.grab>` (preserving `#` comments) and formats it into canonical GRAB source style:
  - 2 spaces per indentation level inside `make`, `if` / `otherwise if` / `otherwise`, `repeat`, `for each`, `while`, and `try` / `catch` blocks.
  - Single spaces around keywords and after `,` and `:`.
  - Normalized `LF` (`\n`) line endings with a single trailing newline.
- By default, writes the formatted source to `stdout`. When `--write` is passed, atomically updates `<file.grab>` in place.

---

## 7. Conformance cases for GRAB 0.4

### Case 1: Standard library (`text`, `math`, `collections`) and `$19.95` price formatting

Source:
```grab
use text
use math
use collections

set product to record {sku: "A-17", price: 19.95, tags: ["new", "sale"]}
set taxed to math.round using (product.price times 1.08), 2
set price_label to "$" joined with (text.from using taxed)
set all_tags to collections.append using product.tags, "featured"

show price_label
show text.join using all_tags, ", "
```

Expected standard output:
```text
$21.55
new, sale, featured
```

### Case 2: Structured error handling (`try` / `catch` / `raise`)

Source:
```grab
make apply_discount using price, percent
  if percent is greater than 50
    raise "Discount cannot exceed 50%"
  end
  give back price minus (price times percent divided by 100)
end

try
  set final_price to apply_discount using 100, 75
catch err
  show err.code joined with ": " joined with err.message
  set final_price to 100
end
show final_price
```

Expected standard output:
```text
GRAB-E308: Discount cannot exceed 50%
100
```

### Case 3: Capability-based filesystem permission denial (error case)

Source (`read_secret.grab`, run without `--allow-read`):
```grab
use files
show files.read_text using "notes.txt"
```

Expected result: `permission denied` (`GRAB-E401`) runtime error on line 2.

---

## 8. Owner approval checklist for GRAB 0.4

On November 11, 2026, the project owner approved the following decisions for GRAB 0.4 implementation:

- [x] Approve `use <module>` syntax and the five core standard library modules (`text`, `math`, `collections`, `dates`, `files`), including `text.from` for converting numbers and values to text.
- [x] Approve `try ... catch <err> ... end` and `raise <expr>` structured error handling, error records (`code`, `category`, `message`, `line`, `column`), and non-catchable fatal limits (`call stack overflow`, `execution limit exceeded`).
- [x] Approve stable `GRAB-E...` error codes and the three diagnostic presentation modes (`beginner`, `developer`, `production`).
- [x] Approve default-deny filesystem permissions (`--allow-read <dir>`, `--allow-write <dir>`) and mandatory blocking of sensitive paths (`.git`, `.env`, `.ssh`, private keys).
- [x] Approve the `grab check` and `grab format [--write]` CLI subcommands.
