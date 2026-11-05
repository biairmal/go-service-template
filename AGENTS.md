# AGENTS.md

**Development guidelines for `github.com/biairmal/go-service-template`**, for humans and AI agents alike. Read this
before any change. Examples live in [docs/PATTERNS.md](docs/PATTERNS.md); the reasons behind choices in
[docs/DECISIONS.md](docs/DECISIONS.md).

## Orientation

`go-service-template` is the starting point for a resource service that trusts
[go-identity](https://github.com/biairmal/go-identity): it validates identity's tokens, reads permissions through
go-sdk `authz` and scopes every query by tenant and owner. It is built on
[go-sdk](https://github.com/biairmal/go-sdk) and follows its conventions. Start a client project with
`gonew github.com/biairmal/go-service-template github.com/<client>/<service>`.

| Path | Role |
|---|---|
| `cmd/server` | One binary: `serve` (default), `worker`, `migrate` |
| `internal/app` | Config, dependency wiring, middleware chain, routes, lifecycle |
| `internal/platform/*` | Generic plumbing, no business rules. Never imports `app` or a module |
| `internal/module/<name>` | One feature boundary each; helpers used by one module are its subpackages |
| `config/config.yaml` | Defaults with `${VAR:default}` |

`internal/` holds only these three directories.

## Commands

```bash
make up                # Postgres via Compose
make run               # serve against it
make check             # CI gate: format-check, lint, unit tests, coverage, vulncheck (must pass)
make test              # go test -short ./...
make test-integration  # all tests, Testcontainers needs Docker
make help
```

## Rules

- `context.Context` is the first parameter of every I/O function. No globals; no `init()` except driver
  registration.
- Return `error`, never `*errorz.Error` (typed-nil trap). Wrap causes and attach an `errorz` code; compare with
  `errors.Is`. Never invent a custom `code` (it maps to 500): finer reasons go in `meta.reason`.
- Routing is go-chi/chi v5. Handlers are `handler.Func` wrapped by `httpx.Handle`; path parameters come from
  `chi.URLParam`. Never write status codes by hand.
- Layers inside a module: `handler` → `service` → `repository`. No upward imports.
- **No module imports another.** Declare a small port in the consumer and wire an adapter in `internal/app`.
- Each config block validates itself and is one row in the validation table in `internal/app/config.go`.
- Response DTOs are never the row struct.
- Repositories: named-column intent updates only, `tenant_id` in every `WHERE`, a `version` check on editable
  aggregates. Side effects that leave the service go through the outbox in the caller's transaction.
- Never log secrets, tokens, passwords or request/response bodies.
- Files `lower_snake.go`. Table-driven tests. Integration tests in `*_integration_test.go`, skipped under
  `testing.Short()`.
- Lint limits: line ≤ 120, func ≤ 100 lines / 50 statements, cyclomatic < 15, cognitive < 25.
- Comments describe the code, not its history (no task IDs).
- When the spec is unclear, pick the simplest option consistent with it and add a line to `docs/DECISIONS.md`.

## Definition of Done

- [ ] `make check` passes; `make test-integration` passes when the change touches the database.
- [ ] New behaviour has table-driven tests.
- [ ] New exported symbols have doc comments.
- [ ] No module-to-module imports; no secrets in logs.
