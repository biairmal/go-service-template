# Database backup and restore

The `backup` service (`deploy/compose/docker-compose.yaml`) runs `pg_dump -Fc` against the
`app` database once a day, writing timestamped dumps into the `backups` named volume, then
deletes dumps older than `BACKUP_KEEP_DAYS` (default 7, `deploy/compose/.env.example`). It uses
the same `postgres:16-alpine` image as the `postgres` service, so `pg_dump`'s version always
matches the server. The first dump is written when the service starts.

Run the commands below from `deploy/compose/`.

## List available dumps

```bash
docker compose exec backup ls -la /backups
```

## Take a dump now

```bash
docker compose exec backup sh -c 'pg_dump -Fc -f /backups/app-manual.dump'
```

## Restore a dump into an empty database

Run this against a **fresh** database: `pg_restore` with `-Fc` expects the target not to
contain the objects it is about to create.

1. Open a shell that has the dumps and reaches Postgres:
   ```bash
   docker compose exec backup sh
   ```
2. Create the empty database (the `app` role already exists on this server; on a new server
   run `deploy/sql/bootstrap.sql` first):
   ```bash
   createdb restored_app
   ```
3. Restore the dump:
   ```bash
   pg_restore -d restored_app --no-owner --clean --if-exists /backups/app-20261105-030000.dump
   ```
   - `--clean --if-exists` is a no-op against an empty database and harmless otherwise.
   - `--no-owner` avoids failing on roles that don't exist on the restore target.
4. Confirm the `app` schema came back:
   ```bash
   psql -d restored_app -c '\dt app.*'
   ```

To replace the live database instead, stop `api` and `worker`
(`docker compose stop api worker`), restore into `app` **without** `--no-owner`, so the tables
stay owned by the `app` role the service and its migrations run as, then `docker compose up -d`:

```bash
pg_restore -d app --clean --if-exists /backups/app-20261105-030000.dump
```

## Out of scope

Off-host copies of the dumps, and Helm deploys (clusters use their platform's backup
mechanism).
