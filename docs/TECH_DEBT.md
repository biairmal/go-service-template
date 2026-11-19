# Tech debt

Findings from a full review of `go-service-template` (code, deploy files, CI), done on 2026-11-19 against go-sdk
v0.6.0 and rechecked against `main` after the tracing work (TR2.1 to TR2.4), which already traces SQL and the
identity calls and gives the identity client a timeout. Ordered by priority: work top to bottom. Tick the box when the fix is merged.

**Source and limits:** found by reading the code. `go build`, `go vet` and `go test -short ./...` passed at review
time. Not run: integration tests, `-race`, the Compose and Helm stacks, and the exploits themselves. Write a failing
test first for each item, then fix it.

Items that also need a change in [go-sdk](https://github.com/biairmal/go-sdk) name the go-sdk item, which lives in
go-sdk's `docs/TECH_DEBT.md`. The template-side fix does not wait for the SDK fix.

**Priority key**

| Tier | Meaning |
|---|---|
| P0 | Security hole or secret exposure. Fix before a client project starts from this template |
| P1 | Outage, crash, memory problem, or a failure you could not diagnose, under load |
| P2 | Wrong behaviour, unsafe default, missing test, doc that lies |
| P3 | Small cleanups |

---

## P0 — security

### [ ] TD-01 `/metrics` and `/version` are public through the edge, and the Helm block may not apply
- **Where:** [Caddyfile](../deploy/compose/Caddyfile), [ingress.yaml](../deploy/helm/service/templates/ingress.yaml), auth rules in [config.yaml](../config/config.yaml)
- **Problem:** The auth rules make `/metrics`, `/version` and `/readyz` public, and Caddy and the ingress hide only
  `/v1/internal`. Anyone on the internet reads the metrics page (routes, traffic, Go and process stats). The Helm
  `/v1/internal` block is an nginx `configuration-snippet`; current ingress-nginx turns snippets off by default, so
  the block can be silently ignored. Nothing tests it.
- **Fix:** Caddy: answer 404 for `/metrics*` as well as `/v1/internal*`. Helm: add a deny for `/metrics`, and use a
  mechanism that works without snippets (a path that routes to a 404 backend, or the controller's native rule).
  Scrape metrics on the internal network only.
- **Done when:** a test or CI smoke step shows `/metrics` and `/v1/internal/x` return 404 through Caddy, and the
  Helm template renders a rule for both.

### [ ] TD-02 `/v1/internal` and `/v1/admin` have no guard in the code
- **Where:** [routes.go](../internal/app/routes.go) (`groups`), [httpx/routes.go](../internal/platform/httpx/routes.go), [DECISIONS.md](DECISIONS.md) (2026-11-05, T0.4)
- **Problem:** The docs say `/v1/internal` is "service tokens only" and every `/v1/admin` route adds
  `RequirePermission`. `groups()` adds no middleware to any group. Any logged-in user token reaches an internal
  route, and a forgotten `RequirePermission` on an admin route is not caught. The edge block (TD-01) is the only
  protection.
- **Fix:** Add a guard to the `/internal` group that accepts only service tokens (the claim identity puts on
  client-credentials tokens). Add a default-deny to `/admin` (for example, a middleware that fails the request
  unless a permission check ran). If this is out of scope for now, change the docs to say "no guard yet".
- **Done when:** a test sends a normal user token to a dummy `/v1/internal` route and gets 403, and a dummy
  `/v1/admin` route with no permission check is rejected.

### [ ] TD-03 Default database passwords if `.env` is blank
- **Where:** [docker-compose.yaml:75-76,139](../deploy/compose/docker-compose.yaml), [deploy/compose/.env.example](../deploy/compose/.env.example), [config.yaml](../config/config.yaml) (`database.leader.password`), [config.go](../internal/app/config.go)
- **Problem:** Compose uses `${POSTGRES_PASSWORD:-postgres}` and `${DB_PASSWORD:-app}`, and the shipped `.env.example`
  leaves both blank, so a forgotten value becomes `postgres` or `app`. The app accepts the password `app` in
  production.
- **Fix:** In the VPS compose file use `${POSTGRES_PASSWORD:?set it}` and `${DB_PASSWORD:?set it}`. In `Config`
  validation, refuse the password `app` when `service.environment` is `production`.
- **Done when:** `docker compose config` fails with a blank `.env`, and a config test with `APP_ENV=production` and
  password `app` fails validation.

---

## P1 — outage, crash, memory, blind spots

### [ ] TD-04 Anyone can create unlimited metric series
- **Where:** [app.go](../internal/app/app.go) (`middlewareChain`, `middleware.Metrics(rec, nil)`)
- **Problem:** The path label is the raw URL path. Each note ID (`/v1/notes/<uuid>`) and each random 404 path is a new
  Prometheus series, and the middleware runs before auth. A client can fill the pod's memory. The SDK calls a
  `PathNormalizer` before routing, so it cannot read chi's route pattern (go-sdk TD-14).
- **Fix:** Until go-sdk TD-14 lands, pass a `PathNormalizer` that collapses UUID segments and maps paths that match no
  route to `unmatched`. After the SDK fix, delete it and take the default.
- **Done when:** a test sends 50 random 404 paths and 50 note IDs and the registry holds at most a handful of `path`
  values.

### [ ] TD-05 A failing outbox row is retried every second, and a full batch spins the CPU
- **Where:** [outbox.go](../internal/platform/database/outbox.go) (`Run`, `claim`), [20261105000001_init.sql](../migrations/20261105000001_init.sql)
- **Problem:** The claim query waits `2^attempts` seconds counted from `created_at`, with `attempts` capped at 10.
  After about 17 minutes a row is always due, so a row that always fails is retried every poll for up to 30 days.
  `Run` also skips its sleep when a batch is full (`n == d.batch`), so 100 failing rows loop with no pause. The
  default `senders()` map is empty, so any topic you enqueue before adding a sender does this.
- **Fix:** Add `next_attempt_at timestamptz NOT NULL DEFAULT now()`. On failure set it to
  `now() + least(2^attempts, 1024) seconds`. Claim `WHERE next_attempt_at <= now()`. Only skip the sleep after a
  full batch with no failures.
- **Done when:** an integration test with a sender that always fails shows the row is attempted a handful of times in
  the first minutes, not once per second, and the dispatcher sleeps between polls.

### [ ] TD-06 The cause of a 500 is never logged
- **Where:** [httpx/handle.go](../internal/platform/httpx/handle.go)
- **Problem:** `Handle` only calls the SDK's `handler.Handle`, which writes a masked error to the client and logs
  nothing (go-sdk TD-36). The access log shows `status=500` and no reason. The real error (a DB error, a timeout) is
  lost.
- **Fix:** In `httpx.Handle`, log once with `ErrorWithContext` (message, error, method, route) when the status is 500
  or more. Pass the logger in. After go-sdk TD-36, use the SDK option instead.
- **Done when:** a test with a handler that returns a wrapped DB error finds one error log line with the cause and
  the request ID.

### [ ] TD-07 Client IPs in logs are the proxy's address
- **Where:** [app.go](../internal/app/app.go) (`middleware.Logging`), [config.go](../internal/app/config.go) (`HTTPConfig`)
- **Problem:** go-sdk v0.6.0 ignores `X-Forwarded-For` unless an `IPResolver` is given. The template gives none, so
  behind Caddy or an ingress every log line shows the proxy IP. A rate limiter added later (`KeyByIP`) would put all
  users in one bucket.
- **Fix:** Add `http.trusted_proxies` (CIDR list) to config. Build `middleware.NewIPResolver(...)` and pass it in
  `LoggingOptions.IPResolver`. Default it to the Compose network CIDR, and set it in the Helm values.
- **Done when:** a test shows a forged `X-Forwarded-For` from an untrusted peer is ignored, and one from a trusted
  peer is used.

### [ ] TD-08 Connection pools do not fit Postgres's default limit
- **Where:** [config.yaml](../config/config.yaml) (`database.pool`), [deploy.sh](../deploy/compose/deploy.sh), [values.yaml](../deploy/helm/service/values.yaml) (`autoscaling`)
- **Problem:** Each pod opens up to 25 connections. The HPA goes to 6 pods plus a worker (about 175), and `deploy.sh`
  doubles the API replicas during a deploy (4 × 25 + 25 = 125). Postgres's default `max_connections` is 100, so new
  pods can fail with "too many clients" during a deploy or a busy hour.
- **Fix:** Default `max_open_conns` to 10 for the API and 3 for the worker, both from env. Add one rule to the README:
  `pods × pool < max_connections − 10`. Use pgbouncer when a project needs more.
- **Done when:** the README shows the rule with the default numbers, and `values.yaml` and compose use pools that fit.

### [ ] TD-09 The HTTP server has no read, write or idle timeout
- **Where:** [app.go](../internal/app/app.go) (`Serve`)
- **Problem:** The server sets only `ReadHeaderTimeout`. A slow client can hold a connection open for a long time
  (the body-size cap does not stop that), and idle keep-alive connections are never closed.
- **Fix:** Set `ReadTimeout`, `WriteTimeout` and `IdleTimeout` from config (defaults 15s, 30s, 60s).
- **Done when:** a test with a client that sends a body one byte at a time is cut off at the read timeout.

### [ ] TD-10 One slow or panicking outbox sender blocks all delivery
- **Where:** [outbox.go](../internal/platform/database/outbox.go) (`DispatchOnce`, `send`)
- **Problem:** Up to 100 sends run inside one transaction with no per-send timeout, so a hung call holds a
  connection and row locks. A panic in a sender is not caught, so the worker crashes, the row is claimed again, and
  it crash-loops with every later message stuck behind it.
- **Fix:** Wrap each send in `context.WithTimeout` (default 10s) and `recover()`, and record a panic as a failed
  attempt. Lower the batch to about 20. Note the "sends inside one transaction" limit in a `shortcut:` comment.
- **Done when:** tests with a sender that blocks forever and one that panics both end as a recorded failed attempt
  and the next row is still sent.

### [ ] TD-11 The worker cannot be observed
- **Where:** [worker.go](../internal/app/worker.go), [deployment-worker.yaml](../deploy/helm/service/templates/deployment-worker.yaml)
- **Problem:** The worker has no HTTP port, no metrics, no tracing and no Helm probes. A stuck worker looks healthy,
  and nobody can see how many messages are waiting or how old the oldest is.
- **Fix:** Add a small listener (for example `:9090`) with `/healthz` and `/metrics`. Export `outbox_pending`,
  `outbox_oldest_age_seconds`, and sent and failed counters. Add the Helm liveness probe and a ServiceMonitor.
- **Done when:** the worker's `/metrics` shows the four series, and `helm template` renders a probe for the worker.

---

## P2 — correctness, unsafe defaults, tests, docs

### [ ] TD-12 `bulk-delete` with an empty filter deletes everything the caller can reach
- **Where:** [dto.go](../internal/module/note/dto.go) (`BulkDeleteRequest`), [handler.go](../internal/module/note/handler.go) (`bulkDelete`)
- **Problem:** `{"filter": ""}` matches every note in scope (up to 1000), by design. A client bug that sends an empty
  string wipes the caller's notes, or the whole tenant's with `notes:write:any`. New modules will copy this.
- **Fix:** Return 400 for an empty filter. If "delete all" is needed, require an explicit `"all": true`. Update the
  swagger text.
- **Done when:** a test posts `{"filter": ""}` and gets 400 with no rows deleted.

### [ ] TD-13 Large numbers in `If-Match` and `page` cause a 500
- **Where:** [ifmatch.go](../internal/platform/httpx/ifmatch.go) (`checkVersion`), [listquery.go](../internal/platform/listquery/listquery.go) (`parsePage`, `ToListOptions`)
- **Problem:** `If-Match: "3000000000"` does not fit the `int` column, so Postgres errors and the caller gets 500.
  `page=9223372036854775807` overflows `(page - 1) * size` into a negative `OFFSET`, which is also a 500. Both come
  from user input and pollute 5xx alerts.
- **Fix:** `checkVersion`: 400 above `math.MaxInt32`. `parsePage`: 400 above a cap (for example 100000).
- **Done when:** tests for both inputs return 400.

### [ ] TD-14 The runtime database role can drop tables
- **Where:** [bootstrap.sql](../deploy/sql/bootstrap.sql), [job-migrate.yaml](../deploy/helm/service/templates/job-migrate.yaml), compose `migrate` service
- **Problem:** One role `app` owns the schema, runs migrations and runs the live service. A bug that lets an attacker
  run SQL means DROP and ALTER rights, not just data access.
- **Fix:** Create `app_migrator` (owner) and `app` (runtime, `SELECT/INSERT/UPDATE/DELETE` only, via default
  privileges). Use the owner only for `migrate up`. Add a second env var for its password.
- **Done when:** the app role cannot `DROP TABLE note` in an integration test, and migrations still apply.

### [ ] TD-15 CI: unpinned tools, broad token, stale generated docs
- **Where:** [ci.yml](../.github/workflows/ci.yml), [Makefile](../Makefile), [api/swagger](../api/swagger)
- **Problem:** `gofumpt` and `govulncheck` install with `@latest`. Actions are pinned by tag. There is no top-level
  `permissions:`. CI never checks that the committed Swagger files match the handlers; after go-sdk v0.6.0 added
  `trace_id` to the error envelope the committed `swagger.json` still has none.
- **Fix:** Add `permissions: contents: read`. Pin tool versions and action SHAs, and let Renovate bump them. Add a CI
  step: `make swagger` then `git diff --exit-code api/swagger`. Run `make swagger` once now.
- **Done when:** the pipeline fails on a handler change with no swagger update, and `swagger.json` has `trace_id`.

### [ ] TD-16 A renamed auth config key is silently ignored
- **Where:** [config.yaml:39](../config/config.yaml) (`default_protected`)
- **Problem:** go-sdk v0.6.0 renamed it to `default_public` (zero value = protected). The config loader ignores unknown
  keys (go-sdk TD-37), so the old key stays in the file and does nothing. Someone who sets it to `false` to open the
  API gets no effect and no warning.
- **Fix:** Delete the line, or replace it with a comment that unmatched routes are protected.
- **Done when:** `config.yaml` has no `default_protected`, and a test asserts an unmatched route needs a token.

### [ ] TD-17 Probe and scrape requests fill the access log
- **Where:** [app.go](../internal/app/app.go) (`middleware.Logging`)
- **Problem:** One Info line per response, including `/healthz` and `/readyz` every few seconds per pod and the
  Prometheus scrape. These are most of the log volume and hide real traffic. The SDK has no skip option (go-sdk
  TD-38).
- **Fix:** Wrap the logging middleware so it is skipped for the probe and metrics paths. After go-sdk TD-38, use the
  SDK option.
- **Done when:** a test shows no log line for `/healthz` and one for `/v1/notes`.

---

## P3 — small items

- [ ] **TD-18 Search and `like` wildcards:** `%` and `_` in a search or `like` value act as wildcards. Escape `\`, `%`,
  `_` and add `ESCAPE '\'`. ([repository.go](../internal/module/note/repository.go) `scopedWhere`,
  [listquery.go](../internal/platform/listquery/listquery.go) `toConditions`)
- [ ] **TD-19 Two body limits:** `DecodeJSON` wraps the body in a fixed 1 MiB reader, but `http.max_body_bytes`
  already limits it in `maxBody`. Raising the config value has no effect. Delete the constant and the extra reader.
  ([decode.go](../internal/platform/httpx/decode.go))
- [ ] **TD-20 Needless extension and build arg:** `CREATE EXTENSION pgcrypto` (the init migration and
  `bootstrap.sql`) is not needed, since `gen_random_uuid()` is built in from Postgres 13, and it fails on managed
  databases that lack the privilege. The builder stage's `ARG SERVICE_VERSION` is unused. Delete both; edit the init
  migration only while no client database exists. ([20261105000001_init.sql](../migrations/20261105000001_init.sql), [Dockerfile](../Dockerfile))
- [ ] **TD-21 Image and compose hardening:** pin `golang`, `caddy` and `postgres` images by digest; default the image
  tag to a release, not `main`; add `no-new-privileges`, `cap_drop: [ALL]` and `mem_limit` to compose services; copy
  backups off the VPS. ([docker-compose.yaml](../deploy/compose/docker-compose.yaml), [Dockerfile](../Dockerfile))
- [ ] **TD-22 Tracing and metrics config:** `tracing.insecure: true` is hard-coded (use an env var and set it false in
  Helm). `metrics.path` is configurable but the public auth rule is fixed to `/metrics`, so changing the path makes
  Prometheus get 401. Drop the option or generate the rule. ([config.yaml](../config/config.yaml))
- [ ] **TD-23 `X-Trace-Id` is not exposed to browsers:** add it to `corsExposeHeaders`.
  ([middleware.go](../internal/app/middleware.go))
- [ ] **TD-24 Service-token exchange is a per-service copy:** [service_token.go](../internal/app/service_token.go) does the
  client-credentials call itself. Any service that calls identity needs the same code (go-sdk TD-39).
