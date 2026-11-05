# go-service-template

A Go service template for resource services that trust [go-identity](https://github.com/biairmal/go-identity),
built on [go-sdk](https://github.com/biairmal/go-sdk). Under construction: the full guide arrives with the example
module.

## Run locally

```bash
cp .env.example .env
make up      # Postgres, api, worker and Caddy (https://localhost) in Docker
make run     # http://localhost:8080
curl localhost:8080/healthz
curl localhost:8080/readyz
```

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

## Endpoints so far

| Path | Purpose |
|---|---|
| `GET /healthz` | Liveness |
| `GET /readyz` | Readiness: 503 while starting, shutting down, or when the leader database is unhealthy |
| `GET /version` | Build version |
| `GET /metrics` | Prometheus, when enabled |

See [AGENTS.md](AGENTS.md) for the rules every change follows.
