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

## Repositories

Writes, transactions, locks and reads right after a write run on the leader: `database.Leader(ctx, db)`, or go-sdk
`repository/sql`'s `GetConnection(ctx)`, both of which join the context transaction. Lists, search and reports run
on a follower (`db.Follower()` or `GetReadConnection(ctx)`), which falls back to the leader when followers are
unhealthy. Services make several writes atomic with `database.WithTx`.

### Named-column updates

Expose intent methods that write only their own columns, never a full-row update, so a field the caller never
loaded is never overwritten:

```go
func (r *Repository) SetStatus(ctx context.Context, tenantID, id uuid.UUID, status string) error {
    res, err := database.Leader(ctx, r.db).ExecContext(ctx,
        `UPDATE note SET status = $3, updated_at = now() WHERE id = $2 AND tenant_id = $1`,
        tenantID, id, status)
    if err != nil {
        return errorz.Wrap(err).WithCode(errorz.CodeInternal).WithMessage("note: set status")
    }
    if n, _ := res.RowsAffected(); n == 0 {
        return errorz.NotFound() // never 403: other tenants' IDs cannot be probed
    }
    return nil
}
```

### Scope in every `WHERE`

Every update and delete filters on `id` and `tenant_id`, plus `owner_id` when the caller is owner-scoped. Zero rows
is a 404, never a 403:

```sql
DELETE FROM note WHERE id = $1 AND tenant_id = $2 AND owner_id = $3;
```

### Optimistic locking on editable aggregates

Editable tables carry `version int not null default 1`. Updates check it and bump it; zero rows on a row that
exists is a 409:

```go
res, err := conn.ExecContext(ctx,
    `UPDATE note SET title = $4, version = version + 1, updated_at = now()
     WHERE id = $2 AND tenant_id = $1 AND version = $3`,
    tenantID, id, version, title)
// ...
if n == 0 {
    if exists { return errorz.Conflict() }
    return errorz.NotFound()
}
```

### Soft delete

Opt-in per table. One statement, and unique constraints become partial indexes:

```sql
UPDATE note SET deleted_at = now() WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL;
CREATE UNIQUE INDEX note_title_live ON note (tenant_id, title) WHERE deleted_at IS NULL;
```

### Secrets in their own tables

Password hashes, token hashes and API keys live in a table of their own, read by a struct that is never serialised
into a response, a log line or the cache.

### Side effects through the outbox

Emails and queue messages are enqueued in the same transaction as the change, and the worker sends them:

```go
err := database.WithTx(ctx, s.db, func(ctx context.Context) error {
    if err := s.notes.Create(ctx, n); err != nil {
        return err
    }
    return s.outbox.Enqueue(ctx, "note.created", n.ID.String(), event)
})
```

Senders must tolerate duplicates: a crash between sending and recording the send resends. A payload may carry
`expires_at`; a row past it is never sent.
