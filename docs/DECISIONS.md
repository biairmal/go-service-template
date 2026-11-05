# Decisions

Choices made where the SPEC was silent or unclear. One line each: `date · task · decision · reason`.

| Date | Task | Decision | Reason |
|---|---|---|---|
| 2026-11-05 | T0.1 | `config.yaml` keeps go-identity's `database.leader.max_retries`, `database.health` and `validator` blocks, which the SPEC 1.3 sample omits | `/readyz` reads the leader's health check, and the validator block lets the module `Deps` use it without another config change |
| 2026-11-05 | T0.1 | Compose Postgres logs in as `app` directly | The bootstrap script that creates the `app` role and schema arrives with migrations (T0.2) |
| 2026-11-05 | T0.1 | `server worker` holds a database connection and waits for SIGTERM | The plan's "empty runner": the outbox dispatcher and cleanup job join it in T0.6 |
| 2026-11-05 | T0.1 | Shutdown readiness is checked against the real binary, not an in-process test | The in-process test needs `testkit.Postgres` (T0.3), which carries go-identity's shutdown test over |
| 2026-11-05 | T0.2 | `testkit/postgres.go` lands with the database package instead of the rest of the test kit (T0.3) | The migration, outbox and lock integration tests need a migrated Postgres; T0.3 still adds the HTTP harness, keys and clock |
| 2026-11-05 | T0.2 | `github.com/moby/go-archive` pinned at v0.3.0 (testcontainers asks for v0.2.0) | v0.2.0 fails govulncheck (GO-2026-6253); go-identity pins the same version |
| 2026-11-05 | T0.4 | `apierr` keeps two reasons, `PERMISSION_NOT_HELD` (403) and `VERSION_CONFLICT` (409); validation (400 with `meta.fields`) and not found (404) are plain `errorz` codes | go-identity has no reason for those two; every other reason serves its sign-in flows |
| 2026-11-05 | T0.4 | `apierr.WithRetryAfter` and the Retry-After header in `httpx.Handle` are left out; `Handle` passes through to `handler.Handle` | No reason here is a 429 or a lockout; they come back with a rate limiter |
| 2026-11-05 | T0.4 | `Deps.Authz` is `authz.NewNoOp()` and `/v1/internal` has no service-token guard yet | The authorizer and the token checks arrive with auth (T1.1, T1.2); no module mounts a route yet |
| 2026-11-05 | T0.4 | The architecture test stats every file under `internal/` before running `go list` | `go list` runs in a child process the test cache cannot see, so a new cross-module import otherwise passes on a cached result |
| 2026-11-05 | T0.3 | `testkit.SigningKeys(t, iss, aud)` returns a JWKS served on a local port plus a token issuer, instead of go-identity's key directory | The template validates identity's tokens and never signs any, so its tests need what identity publishes (a JWKS) and a way to mint tokens with `sub`, `tid` and `roles` |
| 2026-11-05 | T0.3 | go-identity's `TestOperationalEndpoints` and `TestShutdownFlipsReadinessThenExits` come over with the test kit | They need `testkit.Postgres`; T0.1 checked shutdown against the real binary until now |
