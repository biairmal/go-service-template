# go-service-template

A starting point for a Go resource service that trusts [go-identity](https://github.com/biairmal/go-identity),
built on [go-sdk](https://github.com/biairmal/go-sdk). It validates identity's tokens, reads permissions through
go-sdk `authz`, and scopes every query by tenant and owner. One binary runs `serve`, `worker` and `migrate`; one
image runs on a VPS (Docker Compose) or a cluster (Helm, including OpenShift).

The `note` module (`internal/module/note`) is the worked example: CRUD, `If-Match` versioning, paged and filtered
lists, owner scoping. Copy it to start a new module.

Rules for every change are in [AGENTS.md](AGENTS.md), worked examples in [docs/PATTERNS.md](docs/PATTERNS.md), the
reasons behind choices in [docs/DECISIONS.md](docs/DECISIONS.md).

**Contents:** [Start a client project](#start-a-client-project) ·
[Run locally](#run-locally) · [Run with a real go-identity](#run-with-a-real-go-identity) ·
[Add a module](#add-a-module) · [Add a migration](#add-a-migration) ·
[Call identity's internal API](#call-identitys-internal-api) · [Deploy to a VPS](#deploy-to-a-vps) ·
[Deploy to a cluster](#deploy-to-a-cluster) · [Endpoints](#endpoints)

## Start a client project

You need Go 1.26+, Docker, and `make`. On Windows use Git Bash.

```bash
go run golang.org/x/tools/cmd/gonew@latest github.com/biairmal/go-service-template github.com/<client>/<service>
cd <service>
git init && git add -A && git commit -m "Start from go-service-template"
cp .env.example .env
make up      # builds the image; Postgres, migrate, api, worker, Caddy (https://localhost), backup
make check   # format, lint, unit tests, coverage, govulncheck
curl -sk https://localhost/readyz
```

If `make check` fails in `govulncheck` on a fresh copy, an advisory was published after the template's last
release: `go get <module>@<fixed version>` as the report names it, then `go mod tidy`.

`gonew` rewrites the module path and imports; nothing else needs editing to run. Before the first deploy, rename
what is still the template's:

| What | Where |
|---|---|
| Compose project | `name:` in `deploy/compose/docker-compose.yaml` (also the prefix of its volumes) |
| Image | `IMAGE` in `Makefile`, `image:` in `deploy/compose/docker-compose.yaml`, `image.repository` in `deploy/helm/service/values.yaml`. CI pushes to `ghcr.io/<owner>/<repo>` on its own |
| Service name and identity client ID (`note-service`) | `SERVICE_NAME` and `IDENTITY_CLIENT_ID` in `.env.example`, `deploy/compose/.env.example`, `config/config.yaml`, `deploy/helm/service/values.yaml`; the `service_clients` entry in `deploy/compose/identity.yaml` |
| The `note` module | Keep it as a reference until your first module works, then delete `internal/module/note`, its line in `internal/app/module.go`, its config block and its migration (see [Add a module](#add-a-module)) |

**Schema name.** Tables live in schema `app`, owned by role `app`. That only matters if two services built from
this template share one Postgres database; then give each its own name. `grep -rn "app" deploy/sql/bootstrap.sql
internal/platform/database/migrate.go internal/platform/testkit/postgres.go` finds the four spots:

1. `deploy/sql/bootstrap.sql`: the role, the schema and the `search_path`.
2. `internal/platform/database/migrate.go`: `VersionTable` (`app.goose_db_version`).
3. `internal/platform/testkit/postgres.go`: `appRole` and the schema in `truncate`.
4. The `DB_USER` default in `config/config.yaml`, `.env.example` and `deploy/helm/service/values.yaml`.

Then `make down`, `docker volume rm go-service-template_pgdata` (the init script only runs on an empty volume) and `make up`.

## Run locally

`make up` runs the whole stack in Docker with auth **off**: every request runs as a fixed dev admin (tenant
`00000000-0000-4000-8000-00000000d002`), so Postgres alone serves the full API.

```bash
make up
curl -sk https://localhost/v1/notes -H 'Content-Type: application/json' -d '{"title":"hello"}'
curl -sk https://localhost/v1/notes
make down
```

To work on the code, keep the stack up and run the API from source on `:8080` against the same Postgres:

```bash
make run          # regenerates api/swagger first
curl localhost:8080/healthz
```

| Command | Does |
|---|---|
| `make up` / `make down` | Start / stop the Compose stack (`make up-obs` adds Prometheus, Grafana on :3000, Loki, Tempo) |
| `make run`, `make build` | Regenerate `api/swagger/`, then run `serve` / build `bin/server` |
| `make test` | Unit tests (`go test -short ./...`) |
| `make test-integration` | Everything, including Testcontainers tests against a real Postgres (needs Docker) |
| `make check` | The CI gate: format check, lint, unit tests, coverage, govulncheck |
| `make migrate-up`, `migrate-down`, `migrate-create name=…` | Migrations against the database in `.env` |
| `make swagger` | Regenerate `api/swagger/` from handler annotations |
| `make help` | Every target |

API docs: set `SWAGGER_ENABLED=true`, `SWAGGER_USERNAME` and `SWAGGER_PASSWORD` in `.env`, `make run`, then open
`http://localhost:8080/swagger/index.html`.

## Run with a real go-identity

`make up-identity` starts the stack plus go-identity on `localhost:8081`, built from a checkout next to
this one (`IDENTITY_SRC`, default `../go-identity`; its `make keys` runs first if there are no keys). The
service then runs with auth on and `authz.source: identity`. Identity's config is
[deploy/compose/identity.yaml](deploy/compose/identity.yaml): the `notes:*` permissions (admins get all,
members get `notes:read` and `notes:write`), this service's client, and no email verification.

```bash
make up-identity
ID=http://localhost:8081

# 1. Sign in as the dev super admin. The first sign-in returns a token that can only change the password.
T=$(curl -s $ID/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"local-super-admin-1"}' | jq -r .data.access_token)
T=$(curl -s $ID/v1/me/password -H "Authorization: Bearer $T" -H 'Content-Type: application/json' \
  -d '{"current_password":"local-super-admin-1","new_password":"local-super-admin-2"}' | jq -r .data.access_token)
# Later sign-ins: the same login call with local-super-admin-2.

# 2. Create and list notes through Caddy with that token.
curl -sk https://localhost/v1/notes -H "Authorization: Bearer $T" -H 'Content-Type: application/json' -d '{"title":"hello"}'
curl -sk https://localhost/v1/notes -H "Authorization: Bearer $T"

# 3. A member (role `member`, no notes:read:any) sees only their own notes.
curl -s $ID/v1/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"member@example.com","password":"member-password-1","display_name":"member"}'
M=$(curl -s $ID/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"member@example.com","password":"member-password-1"}' | jq -r .data.access_token)
curl -sk https://localhost/v1/notes -H "Authorization: Bearer $M" -H 'Content-Type: application/json' -d '{"title":"mine"}'
curl -sk https://localhost/v1/notes -H "Authorization: Bearer $M"                  # only "mine"
curl -sk "https://localhost/v1/notes?owner_id=<super admin id>" -H "Authorization: Bearer $M"   # 403
```

`make down` stops every profile. The identity profile is for local development only; on a VPS, identity runs
from its own repository's Compose file.

## Add a module

A module is one package under `internal/module/<name>`: `module.go` (constructor, routes, permission keys),
`config.go`, `dto.go`, `handler.go`, `service.go`, `repository.go`, `model.go`, plus tests. Modules never import
each other (`make test` fails if one does; see [Call identity's internal API](#call-identitys-internal-api) for
the port-and-adapter way around it). Below, `task` stands for your module.

1. **Copy the example and rename it.** From the repo root:

   ```bash
   cp -r internal/module/note internal/module/task
   rm internal/module/task/mock_repository_test.go          # regenerated below
   perl -pi -e 's/Notes/Tasks/g; s/notes/tasks/g; s/Note/Task/g; s/note/task/g' internal/module/task/*.go
   ```

   That renames the package, types, route (`/tasks`), table and permission keys (`tasks:read`, `tasks:read:any`,
   `tasks:write`, `tasks:write:any`; drop the `:any` pair if every caller sees the whole tenant). Read the diff:
   the rename is blind.
2. **Strip what is specific to notes**, in the code *and* the tests (they assert on these fields too):
   - fields `body`, `status`, `pinned` in `model.go`, `dto.go`, `repository.go` (columns), `service.go` (the
     `typeFilters` check) and the handlers; add your own;
   - the list extras: `ListExtra` (`search`, `created_from`, `created_to`), `queryTime` in `handler.go`,
     `ListExtra.rangeConditions` in `service.go` and the `search` argument of `Repository.List`. Keep them only if your list needs them;
   - the list config in `service.go`: `AllowedSortFields`, `AllowedFilterFields`, `AllowedParams`;
   - the swag annotations: `@Description` text, and every `@Param` for a query key you removed (a stale one
     documents a parameter that returns 400);
   - `list_integration_test.go` seeds note rows: port its fixture or replace it with your own list cases.

   Then `go generate ./internal/module/task` (writes `mock_repository_test.go`; the first run downloads mockgen)
   and `make lint`. Expect `lll` (120 columns, comments and test tables included) to flag lines the longer name
   pushed over, and `gocritic` to flag a `switch` left with one case.
3. **Add the table:** `make migrate-create name=task` (needs no database) writes
   `migrations/<UTC timestamp>_task.sql` with placeholder `SELECT` statements. Replace them, copying
   `migrations/*_note.sql`: the base columns `id` (`gen_random_uuid()`), `tenant_id`, `owner_id` (if owner-scoped),
   `version`, `created_at`, `updated_at`, `created_by`; an index that starts with `tenant_id`; a `-- +goose Down`
   that drops the table.
4. **Add the config block.** In `internal/app/config.go`, import the package, add
   `Task task.Config \`mapstructure:"task"\`` to `ModulesConfig` and the row `{"modules.task", c.Modules.Task.Validate}`
   to `sections()`; in `config/config.yaml`, add `task:` under `modules:` with its values. A value that differs per
   environment is `${VAR:default}` and gets a line in `.env.example`. Run `make format`.
5. **Register it:** import the package in `internal/app/module.go` and add one entry to the `[]Module{…}` list in
   `buildModules` (routes register in list order):
   `task.New(cfg.Modules.Task, task.Deps{DB: deps.DB, V: deps.V, Authz: deps.Authz}),`
6. **Register the permissions in identity.** Every key in `module.go` goes into identity's
   `modules.rbac.permissions` and into the roles that should hold it (`modules.rbac.roles`); the template gives
   `admin` all four and `member` only `:read` and `:write`. Locally that file is
   [deploy/compose/identity.yaml](deploy/compose/identity.yaml); elsewhere it is identity's own config. The config
   seeds new tenants only: existing tenants get new permissions through identity's role API.
7. **Docs and checks.** `make swagger` (rewrites `api/swagger/docs.go` and `swagger.json`; commit both), add the
   endpoints to the [Endpoints](#endpoints) table, then `make check` and `make test-integration`.

Try it with auth off: with `make up` running, `make migrate-up` (the new table; `serve` refuses to start while the
schema is behind), `make run`, then `curl localhost:8080/v1/tasks`.

**A new service client** (a second template service calling identity) needs its own entry in identity's
`modules.auth.service_clients`: an `id` and the argon2id hash of a fresh secret. Generate both with Docker:

```bash
SECRET=$(openssl rand -base64 32)
echo "$SECRET"   # IDENTITY_CLIENT_SECRET for this service
docker run --rm alpine sh -c "apk add -q argon2 >/dev/null && printf %s '$SECRET' | \
  argon2 \"\$(head -c 16 /dev/urandom | base64)\" -id -t 3 -m 16 -p 2 -e"   # secret_hash for identity
```

## Add a migration

```bash
make migrate-create name=add_task_due_date   # writes migrations/<timestamp>_add_task_due_date.sql
make migrate-up                              # against the database in .env (make up's Postgres)
make migrate-down                            # reverts the newest one; every migration needs a working Down
```

Migrations are embedded in the binary. `serve` refuses to start while the schema is behind (it logs "run `server
migrate up`"), so on Compose the `migrate` service runs before `api`, and on a cluster the Helm hook runs before the
pods. Old pods keep running during a deploy, so every migration must work with the code that is already live.
Change a schema in two releases, **expand then contract**:

1. **Expand** (release N): add what the new code needs without breaking the old code. A new column is nullable or
   has a default; a rename is a new column plus a backfill, with the code writing both. Create indexes with
   `CREATE INDEX CONCURRENTLY` (mark that migration `-- +goose NO TRANSACTION`).
2. **Contract** (release N+1, once nothing old is running): drop the old column, add the `NOT NULL`, remove the
   double write.

Never edit a migration that has run anywhere; add a new one.

## Call identity's internal API

Identity serves service-to-service lookups under `/v1/internal` (for example `GET /v1/internal/users/{id}` →
`id`, `display_name`, `status`; no email). They need a service token, which this service already fetches for
`authz` (`internal/app/service_token.go`). The template never imports go-identity's Go code; it calls the HTTP API.

Keep the module ignorant of identity: it declares a **port**, and `internal/app` provides the **adapter**.

```go
// internal/module/task/module.go: the port, owned by the module that needs it.
type UserDirectory interface {
    DisplayName(ctx context.Context, userID uuid.UUID) (string, error)
}

type Deps struct {
    DB    *sqlkit.DB
    V     validator.Validator
    Authz authz.Authorizer
    Users UserDirectory
}
```

```go
// internal/app/identity_users.go: the adapter, the only code that knows identity's URL and response shape.
type identityUsers struct {
    baseURL string
    token   *serviceToken
    client  *http.Client
}

func (u *identityUsers) DisplayName(ctx context.Context, id uuid.UUID) (string, error) {
    tok, err := u.token.Token(ctx)
    if err != nil {
        return "", err
    }
    req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.baseURL+"/v1/internal/users/"+id.String(), http.NoBody)
    if err != nil {
        return "", err
    }
    req.Header.Set("Authorization", "Bearer "+tok)
    resp, err := u.client.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()
    if resp.StatusCode == http.StatusNotFound {
        return "", errorz.NotFound()
    }
    if resp.StatusCode != http.StatusOK {
        return "", fmt.Errorf("identity: GET user: %s", resp.Status)
    }
    var body struct {
        Data struct {
            DisplayName string `json:"display_name"`
        } `json:"data"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
        return "", err
    }
    return body.Data.DisplayName, nil
}
```

Wire it in `internal/app`: build one `serviceToken` (`newServiceToken(cfg.Authz.IdentityURL, cfg.Identity,
http.DefaultClient, clock.System{})`), share it between `authz` and the adapter, and pass the adapter in the
module's `Deps`. Test the module with a fake `UserDirectory`, and the adapter against an `httptest` server, as
`service_token_test.go` does. Cache results in the adapter if a hot path calls it.

`/v1/internal` on *this* service is for other services too: Caddy and the Helm ingress answer 404 for it, so it is
reachable only inside the Compose network or the cluster.

## Deploy to a VPS

One host with Docker. Identity runs from its own repository's Compose file; this stack reaches it by URL.

```bash
# On the VPS, in a checkout of your service (only deploy/compose is needed):
cd deploy/compose
cp .env.example .env
# Edit .env: IMAGE_TAG (a release version), DOMAIN (DNS must point here), CORS_ORIGINS,
# POSTGRES_PASSWORD and DB_PASSWORD (openssl rand -base64 32), identity's URLs,
# IDENTITY_CLIENT_SECRET (this service's client secret in identity).
docker compose pull && docker compose up -d --wait
curl https://<DOMAIN>/readyz
```

Deploying a new version is the same two commands after changing `IMAGE_TAG`: `migrate` runs first, then `api` and
`worker` are replaced. Caddy gets the TLS certificate for `DOMAIN` and does not forward `/v1/internal`. Postgres
listens on `127.0.0.1` only. A `backup` service writes a daily `pg_dump` into the `backups` volume and keeps
`BACKUP_KEEP_DAYS` days; restoring is in [docs/runbooks/backup-restore.md](docs/runbooks/backup-restore.md).
`docker compose --profile obs up -d` adds Prometheus, Grafana, Loki and Tempo (set `TRACING_ENABLED=true`;
Grafana listens on `127.0.0.1:3000`, so set an admin password before exposing it).

Images are published by CI: every push to `main` gives `sha-<short>` and `main`; a `v*` tag gives the version,
which `/version` returns.

## Deploy to a cluster

The chart in [deploy/helm/service](deploy/helm/service) runs `api` (Deployment, HPA, PDB, Service), `worker`, and a
`migrate` Job as a `pre-install,pre-upgrade` hook. Pods run as non-root with a read-only root filesystem and no fixed
UID, so the chart fits OpenShift's `restricted-v2` SCC and Kubernetes Pod Security `restricted`.

Postgres is not part of the chart. Create the database, run `deploy/sql/bootstrap.sql` once as an admin
(`psql -v app_password='…' -d app -f deploy/sql/bootstrap.sql`), then:

```bash
kubectl create secret generic note-service \
  --from-literal=DB_PASSWORD='…' --from-literal=IDENTITY_CLIENT_SECRET='…'
helm upgrade --install notes deploy/helm/service --wait \
  --set image.tag=v1.2.3 \
  --set secret.existingSecret=note-service \
  --set config.DB_HOST=postgres.db.svc \
  --set config.AUTH_JWKS_URL=http://identity.identity.svc/.well-known/jwks.json \
  --set config.AUTHZ_IDENTITY_URL=http://identity.identity.svc \
  --set route.enabled=true --set route.host=notes.example.com   # add --set route.kind=openshift on OpenShift
```

Every non-secret setting is a `config.*` value named like its environment variable (see `values.yaml`); secrets come
only from the Secret you name. Keep `lifecycle.*` in step with `config/config.yaml`'s `lifecycle` block: the pod's
termination grace period is computed from it. On kind, `kind load docker-image` a `make image` build and leave
`route.enabled` off (use `kubectl port-forward svc/notes-service 8080:80`).

## Endpoints

| Path | Purpose |
|---|---|
| `GET /healthz` | Liveness |
| `GET /readyz` | Readiness: 503 while starting, shutting down, or when the leader database is unhealthy |
| `GET /version` | Build version |
| `GET /metrics` | Prometheus, when enabled |
| `GET /swagger/index.html` | API docs, when `swagger.enabled` (basic auth) |
| `POST /v1/notes` | Create a note (`notes:write`) |
| `GET /v1/notes` | List notes: `page`, `size`, `sort=field,DIR`, filters `title` (`title=x;like`), `status`, `pinned`, `owner_id` (needs `notes:read:any`), `search`, `created_from`, `created_to` |
| `GET /v1/notes/{id}` | Read a note (`notes:read`, or `notes:read:any` for others' notes) |
| `PATCH /v1/notes/{id}` | Change a note; needs `If-Match: <version>` (`notes:write`, plus `notes:write:any` for others' notes) |
| `DELETE /v1/notes/{id}` | Delete a note (same permissions as `PATCH`) |
