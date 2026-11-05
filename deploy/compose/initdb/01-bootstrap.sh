#!/bin/sh
# First start only: create the app schema and role (deploy/sql/bootstrap.sql).
set -e
psql -v ON_ERROR_STOP=1 -v app_password="$APP_DB_PASSWORD" \
  -U "$POSTGRES_USER" -d "$POSTGRES_DB" -f /bootstrap/bootstrap.sql
