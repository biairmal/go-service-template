# Security

go-service-template is the starting point for client services, so a flaw here is copied into every project made from
it. This page covers how to report a vulnerability, what the template defends against, how to deploy a service built
from it safely, and the limits it has today.

## Reporting a vulnerability

**Do not open a public issue, PR or discussion for a security problem.**

Report it privately through GitHub: open the repository's **Security** tab and choose **Report a vulnerability**.
Include:

- what an attacker can do, and what they need first (a token, a role, network access);
- steps or a request sequence that reproduces it;
- the commit or image tag you tested.

You can expect an acknowledgement within 7 days. Once the issue is confirmed, the fix is released, and a GitHub
security advisory credits you unless you'd rather stay anonymous. Please give us a reasonable time to ship the fix
before you disclose it.

**In scope:** this repository's code, its configuration defaults, its Docker image, and the Compose and Helm files
in `deploy/`.
**Out of scope:** findings that need a deployment to ignore the [deployment checklist](#deploying-safely);
vulnerabilities in [go-identity](https://github.com/biairmal/go-identity) or [go-sdk](https://github.com/biairmal/go-sdk)
(report those in their repositories); and code a client project added after starting from the template.

**For client projects:** a project made with `gonew` is its own repository. Keep this file, replace the reporting
details with the client's, and re-check the defence table when you add a module.

## Supported versions

Fixes land on `main` and in the next `v*` tag. Projects started from an older version don't update automatically:
watch this repository's security advisories and apply the fix to each project by hand.

## What the template defends against

| Threat | Defence | Where |
|---|---|---|
| Forged or tampered tokens | Tokens are verified with go-sdk `auth` against identity's JWKS: signature, issuer, audience and expiry. Every route needs a token unless `auth.rules` lists it as public | `config/config.yaml` `auth`, `internal/app/middleware.go` |
| Running in production with auth off | With auth off every caller is a fixed dev admin. The config refuses to start in `production` that way | `internal/app/config.go`, `internal/app/middleware.go` |
| Missing permission checks | Every module route checks a permission with go-sdk `authz`. Permissions come from identity's role map, fetched with a service token and cached | `internal/module/note/module.go`, `internal/app/service_token.go` |
| Reading another tenant's or user's data | The tenant comes only from the token's tenant claim and the owner from its subject. A missing tenant is a 401, not "all tenants". Reading other owners' rows needs the `:any` permission | `internal/platform/scope` |
| Lost updates | Editable rows carry a `version`. `PATCH` and `DELETE` need `If-Match`, and a stale version returns 409 | `internal/platform/httpx/ifmatch.go` |
| Oversized or malformed requests | Request bodies are capped at 1 MiB and decoded strictly (unknown fields are refused) | `internal/app/middleware.go`, `internal/platform/httpx` |
| Unvalidated list queries | Sort and filter fields are allow-listed per endpoint | `internal/platform/listquery` |
| Cross-origin requests from other sites | CORS answers only the configured origins | `internal/app/middleware.go` |
| Service-to-service API exposed publicly | Caddy and the Helm ingress answer 404 for `/v1/internal` | `deploy/compose/Caddyfile`, `deploy/helm/service/templates/ingress.yaml` |
| Database reachable from outside | In Compose, Postgres listens on `127.0.0.1` only | `deploy/compose/docker-compose.yaml` |
| Container breakout | Pods run as non-root with a read-only root filesystem and no privilege escalation | `deploy/helm/service/templates/_helpers.tpl` |
| Secrets leaking into logs | Tokens, passwords and request and response bodies are never logged | [AGENTS.md](AGENTS.md) |
| Values leaking into traces | SQL spans record the statement text but never its arguments, so values are always bound as `$n` arguments. Span names use route patterns, never raw paths or ids | `internal/platform/database/driver.go`, [docs/PATTERNS.md](docs/PATTERNS.md#tracing) |
| Vulnerable dependencies | `govulncheck` runs in CI (`make check`). Renovate keeps dependencies current | `Makefile`, `renovate.json` |

How these fit together is in [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Deploying safely

The service refuses to start in `production` with auth off. In any environment, it refuses to start with auth on,
`authz.source: identity` and no client credentials. Everything below is the operator's job:

- [ ] `APP_ENV=production` (or `staging`), and `AUTH_ENABLED=true` everywhere except your own machine.
- [ ] `AUTH_JWKS_URL`, issuer and audience match your go-identity. The service must reject tokens identity issued
      for another audience.
- [ ] `IDENTITY_CLIENT_SECRET` is long and random, stored only as a secret. Identity stores only its argon2id hash.
- [ ] `TENANCY_MODE` matches identity's.
- [ ] `/v1/internal` and `/metrics` aren't reachable from the internet (keep the Caddy rule or the ingress
      annotation).
- [ ] `SWAGGER_ENABLED=false`, or a strong `SWAGGER_PASSWORD`.
- [ ] `CORS_ORIGINS` lists only your real frontends.
- [ ] `POSTGRES_PASSWORD` and `DB_PASSWORD` are random (`openssl rand -base64 32`), and `DB_SSLMODE=require` or
      stricter when Postgres is on another host.
- [ ] Grafana (the `obs` profile) has an admin password before it is exposed beyond `127.0.0.1`.
- [ ] Database backups are encrypted and access to them is restricted. See the
      [backup runbook](docs/runbooks/backup-restore.md).

## Known limitations

These are deliberate for now. They are not bugs to report:

- **No rate limiting.** The template has no per-IP or per-user limits. Add go-sdk `ratelimit` for endpoints that
  are expensive or open to abuse.
- **Access tokens stay valid until they expire,** even after the user signs out or loses a role. Permission changes
  reach this service within `authz.cache_ttl` (1 minute by default). Suspending a tenant takes effect at token
  expiry, unless the service checks `GET /v1/internal/tenants/{id}`.
- **No Postgres row-level security.** Tenant isolation relies on the query rules and their tests.
- **No outbox senders.** The worker dispatches the outbox, but the template registers no topics. Each project adds
  its own senders, and a sender must tolerate duplicate delivery.
