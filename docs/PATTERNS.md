# Patterns

Worked examples of the rules in [AGENTS.md](../AGENTS.md). Each section shows the one way the template does a thing.

## Adding a config block

A block is a struct in `internal/app/config.go` with a `validate` method (or go-sdk's own `Validate`), and one row
in `Config.sections()`. Startup runs every row and names the failing one:

```go
{"metrics", c.validateMetrics},
```

```text
error: config metrics: path is required when metrics are enabled
```

Values that differ per environment go into `config/config.yaml` as `${ENV_VAR:default}`, and every new variable
gets a line in `.env.example`.
