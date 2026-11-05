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
| 2026-11-05 | T0.6 | Stacked on T0.3 | The cleanup job takes a `clock.Clock` and its tests use `testkit.NewClock`, both from T0.3 |
| 2026-11-05 | T0.6 | The worker registers no senders, so a row on any topic stays unsent with `last_error` set until cleanup drops it after 30 days | The template sends nothing yet; a module that enqueues a topic adds its sender to `senders()` |
| 2026-11-05 | T0.5 | `scope.Owner` replaces go-identity's `scope.User`; `Session` is left out | Both read the token subject as a UUID, so keeping both would be two names for one value; refresh sessions are identity's |
| 2026-11-05 | T0.5 | `scope.Owner` reads the subject from the token claims (`auth.SubjectFromContext`), not `ctxkit.UserID` | `Tenant` already reads from the claims, and the dev claims with auth off (T1.1) only need to put claims in the context |
| 2026-11-05 | T0.5 | `OwnerFilter` returns `[]repository.FilterCondition` (empty when the caller holds the `:any` permission) | Appends straight onto `ListOptions.Filter.Conditions` with no branch at the call site |
| 2026-11-05 | T0.7 | `/swagger/*` is behind HTTP basic auth only; the token auth rule that makes it public is added with the `auth` block (T1.1) | There is no token auth yet, so no rule is needed until then |
| 2026-11-05 | T1.1 | With auth off, `LoadConfig` rewrites `authz` to `source: config`, `roles: {admin: ["*"]}`, so the loaded `Config` matches what runs | One place decides it, and `newAuthorizer` stays a plain `authz.New` |
| 2026-11-05 | T1.1 | `LoadConfig` fills empty `authz` claim names and `cache_ttl` from `authz.DefaultConfig()` before validation | `authz.Config.Validate` doesn't apply defaults (only `authz.New` does), so the YAML would otherwise have to repeat `perms_claim`, `tenant_claim`, `super_claim` |
| 2026-11-05 | T1.1 | Dev caller UUIDs are fixed: subject `00000000-0000-4000-8000-00000000d001`, tenant `…d002`, role `admin` | Rows created in one local run are still visible in the next |
| 2026-11-05 | T1.1 | `tenancy.mode` is validated (`single` or `multi`) but nothing reads it yet | The SPEC only requires it to match identity's; both modes put the tenant in `tid`, so scoped code doesn't branch |
