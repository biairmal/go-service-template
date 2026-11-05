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
