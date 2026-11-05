# go-service-template

A Go service template for resource services that trust [go-identity](https://github.com/biairmal/go-identity),
built on [go-sdk](https://github.com/biairmal/go-sdk). Under construction: the full guide arrives with the example
module.

## Run locally

```bash
cp .env.example .env
make up      # Postgres in Docker
make run     # http://localhost:8080
curl localhost:8080/healthz
curl localhost:8080/readyz
```

## Endpoints so far

| Path | Purpose |
|---|---|
| `GET /healthz` | Liveness |
| `GET /readyz` | Readiness: 503 while starting, shutting down, or when the leader database is unhealthy |
| `GET /version` | Build version |
| `GET /metrics` | Prometheus, when enabled |

See [AGENTS.md](AGENTS.md) for the rules every change follows.
