-- Creates the app schema and the role that owns it. Run once per
-- environment with admin credentials, outside the app:
--
--   psql -v app_password='...' -d app -f deploy/sql/bootstrap.sql
--
-- Afterwards the app connects as `app`, whose search_path is `app`,
-- so code and migrations use unqualified table names.

SELECT format('CREATE ROLE app LOGIN PASSWORD %L', :'app_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'app')
\gexec

CREATE SCHEMA IF NOT EXISTS app AUTHORIZATION app;
ALTER ROLE app SET search_path = app;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
