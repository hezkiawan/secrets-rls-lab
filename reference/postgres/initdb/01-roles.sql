-- 01-roles.sql — database roles (runs once, on a fresh database, as the postgres superuser)
--
-- The key idea for Row Level Security: the role that OWNS the tables (runs migrations)
-- must NOT be the role the API uses at runtime. Table owners bypass RLS by default.
--
-- LAB ONLY: passwords below are fixed dev values. In production these logins are
-- either managed by OpenBao (static/dynamic roles) or set from a secrets manager.

-- Owns the schema and tables. Used for migrations, never by the running API.
-- (LOGIN only so the lab can demonstrate the "owner bypasses RLS" trap.)
CREATE ROLE app_owner LOGIN PASSWORD 'owner-dev-only';

-- What the API uses. Owns nothing, can only read/write rows — RLS always applies.
-- In M5 the temporary users created by OpenBao become MEMBERS of this role.
CREATE ROLE app_runtime LOGIN PASSWORD 'runtime-dev-only';

-- Used only by PgBouncer to look up other users' password hashes (auth_query).
CREATE ROLE pgbouncer_auth LOGIN PASSWORD 'pgbouncer-dev-only';
