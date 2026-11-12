# Typed APIs and generated browser clients (GRAB 0.8 Part 2)

Supply a public `grab.api.json` explicitly to `grab check`, `grab serve`, and
`grab generate client`. This is separate from private platform configuration.
Existing routes and `server.json` keep their existing behavior unless a route is
bound to an API contract. Contracts provide data validation, not authentication.

The complete schema example is `tests/fixtures/typed-client/grab.api.json`.
The root contains `version: 1`, named `types`, and an `operations` array. Each
operation has `id`, `method`, `path`, optional `path_fields`, `query`, and `body`,
an array of `success` status/schema pairs, and optional error status codes.
Fields are ordered arrays of `{name,schema,optional?}`. Schema `type` is text,
boolean, integer, decimal, float, bytes, timestamp, nothing, record, list, or ref.
Records declare `fields`; lists declare `items` and `max_count`; refs declare
`ref`. Text and bytes require `max_length` (Unicode code points and decoded
bytes, respectively). `nullable` is independent of an optional record field.
Integer/decimal constraints are `minimum`, `maximum` (exact strings), `precision`
(total written digits), and decimal `scale` (fractional digits).

```grab
use api
route "GET" "/products/{id}" using request
  set input to api.input using request, "product"
  give back api.respond using "product", 200, record {id: input.path.id, price: 19.90, name: "Book", bytes: b"\x00\x01"}
end
```

The host validates input before invoking the handler and checks the final HTTP
response, including a response made through another server helper. Bad input
gets 400, oversized input 413, and a bad handler response a sanitized 500. Typed
errors use `{error:{code,message,request_id}}`. Unknown or duplicate JSON fields,
recursive types, duplicate scalar query parameters, and number coercion fail.
Path/query values are decoded once. Unknown query fields fail too.

Integer and decimal JSON values are strings: `"90071992547409931234567890"` and
`"19.90"`. Decimals preserve their scale; optional fields remain absent. Bytes
use canonical base64; timestamps use UTC RFC3339 with up to nine fractional digits.
Neither prices nor identifiers silently convert to floating point.

```powershell
go run ./cmd/grab generate client --api-contract tests/fixtures/typed-client/grab.api.json --out generated/shop --react
```

The offline command writes `types.ts`, `validators.ts`, `client.ts`, `contract.json`,
`README`, optional `react.ts`, and a hash ownership manifest. It refuses symlinks,
handwritten targets, and edits to previously generated files. Per-file replacement
is atomic; the ownership manifest is written last. An interrupted multi-file
generation can require restoring the previous owned set before regeneration.
There are no downloads, secret lookups, application execution, timestamps, or local
paths in output. React and TypeScript are test dependencies, not Go dependencies.

Use `grabInteger`, `grabDecimal`, and `integerToBigInt` for exact values. `GrabClient`
uses fetch, bounded request/response sizes, runtime validators, AbortSignal, and a
10-second timeout (maximum 60). Same-origin URL/credentials are defaults. Bearer
and CSRF tokens are passed per call; the adapter stores neither. Mutations never
retry automatically. React GET hooks expose data/error/loading/refetch, cancel on
cleanup, and ignore stale completions. Other methods have explicit mutation
functions and never run from an effect.

Application handlers must authenticate and authorize users. Cookie mutations also
require an application-issued CSRF token validated against the session. The typed
host checks same-origin Origin and presence of the token; presence alone does not
validate its authenticity. Cross-origin use requires an explicit client base URL
and an application-managed exact origin allowlist; GRAB does not enable wildcard
credentialed CORS or create an authentication/session framework.

The pinned, compiled React/TypeScript fixture is under `tests/fixtures/typed-client`.
Its lockfile records the tested versions. Installing its dependencies is explicit
maintenance using `npm ci --ignore-scripts`; generation never installs them.
