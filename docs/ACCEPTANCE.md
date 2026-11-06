# Phase 1 acceptance

One row per `SPEC 1.9` criterion: how it was checked, when, and the result. A failure becomes a new task in the
implementation plan; it is not fixed in the acceptance run.

Run on 2026-11-06 against `main` 1c9e7dc, on Windows 11 with Docker Desktop 27.3.1, kind (Kubernetes 1.31),
Helm 3, `hey` v0.1.5.

**Legend:** ✅ passes · ⚠️ passes in part, a follow-up task covers the rest · ❌ fails, a follow-up task fixes it ·
⬜ not checked.

| # | Criterion | Result | How it was checked |
|---|---|---|---|
| 1 | `gonew` into a fresh module path, then `make up` and `make check` pass with no manual edits | ❌ T4.4 | `gonew github.com/biairmal/go-service-template@main github.com/example/demo` into an empty folder, then `cp .env.example .env`, `make up` and `make check`. `make up` exit 0, every container healthy. `make check` exit 2: `TestArchitecture` panics because `internal/app/arch_test.go` lists packages under the hard-coded `github.com/biairmal/go-service-template/internal/`, which `gonew` does not rewrite (it only rewrites imports). Every other package passes, and `govulncheck` is clean |
| 2 | A new client backend runs locally in under one hour, from an empty folder | ✅ | Same run, timed: `gonew` 33 s, `make up` 79 s (image build included), `make check` 57 s, 169 s in all. The Go module and Docker layer caches were warm; a cold machine adds downloads, still far under an hour. A note round-trips through Caddy right after `make up` (row 10) |
| 3 | Never ready before migrations (`serve` refuses), `/readyz` 503 during shutdown and 200 otherwise | ✅ | On the image: after `migrate down`, `serve` exits 1 with `database schema is at version 20261105000001 but this binary needs 20261105000002: run \`server migrate up\``. A container with `DRAIN_DELAY=5s`: `/readyz` 200, then after `docker stop` 503 at +1 s, +2 s, +3 s and +4 s, exit code 0. Also `TestShutdownFlipsReadinessThenExits` and `TestSmoke` (SIGTERM → exit 0, CI) |
| 4 | Scope integration tests pass: no cross-owner or cross-tenant reads or writes | ✅ | `make test-integration` exit 0, every package `ok`. Scope tests: `TestUserCannotReachAnotherUsersNote`, `TestTenantNeverSeesAnotherTenantsNotes`, `TestAnyOwnerSkipsTheOwnerFilterOnly`, `TestNoTenantInContextFails`, `TestUpdateWritesOnlyNamedColumns`, `TestStaleVersionIsAConflict` (`internal/module/note`), and `TestTenantFilter`, `TestOwnerFilter` (`internal/platform/scope`) |
| 5 | A rolling restart under steady load (`hey` at 50 rps) shows zero 5xx | ⚠️ T4.5 | **kind:** `hey -z 75s -q 50` from a pod against the Service, `kubectl rollout restart` mid-run (both api pods replaced in 11 s): 3749 × 200, no other status. **Compose, two api replicas restarted one at a time** (`docker restart`) behind Caddy: 2248 × 200. **Compose, `docker compose up -d --no-deps --scale api=2 --force-recreate api`** (the plan's command): 1845 × 200 and **3 × 502**. Compose recreates both replicas together, and Caddy has no health check or retry to route around them. The README's VPS deploy (`docker compose pull && docker compose up -d`) recreates the api the same way, and with one replica by default |
| 6 | Helm chart installs on kind and on OpenShift with the restricted SCC, migrations as a hook | ⚠️ T4.6 | **kind:** namespace enforcing Pod Security `restricted`, Postgres in another namespace, `helm upgrade --install … --wait` → `STATUS: deployed`. Events: `notes-service-migrate-1` created at :54 and completed before the Deployments were created at :57. The HPA scaled the api to `minReplicas` 2 within 15 s (kind has no metrics-server, so CPU scaling stays untested). **OpenShift:** ⬜ not installed, because no OpenShift cluster is available (no CRC or sandbox). Static check of `helm template --set route.enabled=true,route.kind=openshift`: 2 Routes and no Ingress; no `runAsUser`, `runAsGroup` or `fsGroup` (restricted-v2 assigns the UID); `runAsNonRoot`, `allowPrivilegeEscalation: false`, `readOnlyRootFilesystem`, `seccompProfile: RuntimeDefault` and `drop: ["ALL"]` on all three pod specs |
| 7 | `make run`/`make build` regenerate `api/swagger/`; `/swagger/index.html` lists every example handler, asks for basic auth, is gone when disabled; enabling without credentials fails validation | ✅ | `make build` on the `gonew` copy rewrote `api/swagger/docs.go` and `swagger.json` (new mtimes, no diff). The spec has `/v1/notes` (get, post) and `/v1/notes/{id}` (get, patch, delete). The image with `SWAGGER_ENABLED=true` and credentials: `/swagger/index.html` 401 without them, 200 with; `doc.json` lists both note paths. `SWAGGER_ENABLED=true` without credentials: `error: config swagger: … username and password are required when swagger is enabled`. Disabled (default): 404 through Caddy. `make run` calls the same `swagger` target (Makefile) |
| 8 | Every filter and sort pattern of 1.6 works on `GET /v1/notes` and has a swagger `@Param`; the same query string works against identity's admin lists | ✅ | `TestListQueryPatterns` and `TestListQueryRejects` (one case per table row plus the 400s and the 403), `TestListQueriesAgainstPostgres` (multi-sort, `;like`, `search`, date edges). Every key has an `@Param` in `handler.go`. With `make up-identity` and the super admin's token: `?page=1&size=5&sort=created_at,DESC&search=e` → 200 on both `/v1/notes` and identity's `/v1/admin/users`; adding `&bogus=1` → 400 `invalid query parameter bogus: unknown query parameter` on both; `sort=nope,ASC` → 400 `field not allowed: nope` on both; `;like` works on `/v1/notes?title=admin%3Blike` and identity's `/v1/admin/audit?action=user%3Blike` |
| 9 | `make up-identity`, sign in to identity as the dev super admin, create and list notes with that token; a member without `notes:read:any` sees only their own notes | ✅ | The README's steps, with `IDENTITY_SRC` at go-identity 48f5cb7. Super admin: create 201, list 200, no token 401. Member: create 201; list shows one `owner_id` (their own); `?owner_id=<admin>` 403; the admin's note by ID 404. The local volume already held the changed super-admin password; the first sign-in and forced password change were checked in T3.2 (#20) |
| 10 | With `auth.enabled=false`, `make up` plus Postgres alone serves the full note CRUD (dev claims) | ✅ | On the `gonew` copy, through Caddy: create 201 (owner `…d001`, the dev subject), list 200, get 200, PATCH without `If-Match` 428, with `If-Match: 1` 200, stale 409, delete 204, get after delete 404. `/v1/internal/*` → 404 at Caddy |
| 11 | Logs are JSON with request, correlation and trace IDs; no request or response bodies | ✅ | `make up-obs` (tracing on): `{"level":"info","request_id":"acc-trace-1","correlation_id":"cf517d97…","trace_id":"cd59df92…","path":"/v1/notes","method":"GET","status":200,…}`. With tracing off, `trace_id` is absent; `request_id` and `correlation_id` echo the incoming headers. A note created with body `secret-body-text` never appears in the api logs (0 matches). Also `logging_test.go` |
| 12 | README covers: start a client project, add a module, add a migration, deploy to VPS, deploy to a cluster | ✅ | README sections of those names (#23). A fresh agent with only the README added a second module end to end (`make check` and `make test-integration` exit 0); where it got stuck was fixed in #23 |

## Also found

- **`gonew …@latest` resolves to `v0.0.1-rc.1`**, the broken release candidate, because it is the only tag. The
  run used `@main`. Tagging `v0.1.0` (a "Phase 1 done" step) fixes it with no change, since `@latest` prefers a
  release over a pre-release.
- The `gonew` copy still names `ghcr.io/biairmal/go-service-template` and `note-service` in deploy files, as the
  README's rename table says; nothing there blocks `make up`.

## Method notes

- The `gonew` copy ran under `COMPOSE_PROJECT_NAME=demo` (an environment variable, no file edit), so it got fresh
  volumes and left the existing `go-service-template_pgdata` development data alone. Its volumes were removed
  afterwards.
- `hey` against Caddy runs with TLS verification off (its default), against `https://localhost/v1/notes` with auth
  off. On kind, `hey` ran in a pod against `http://notes-service.notes.svc/v1/notes` with `APP_ENV=staging` and auth
  off, so every request reached Postgres.
