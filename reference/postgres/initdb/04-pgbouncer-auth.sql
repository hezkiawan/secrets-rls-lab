-- 04-pgbouncer-auth.sql — lets PgBouncer look up ANY user's password hash on demand.
--
-- Why: PgBouncer must check passwords itself. A fixed userlist file can't know the
-- temporary users OpenBao creates (M5), so PgBouncer asks Postgres instead (auth_query).
-- pgbouncer_auth may NOT read pg_authid directly, so it calls this SECURITY DEFINER
-- function, which runs with its owner's (postgres) rights. Same logic as PgBouncer's
-- documented default auth_query: expired users (VALID UNTIL in the past) get no password.

CREATE SCHEMA pgbouncer;
REVOKE ALL ON SCHEMA pgbouncer FROM PUBLIC;
GRANT USAGE ON SCHEMA pgbouncer TO pgbouncer_auth;

CREATE FUNCTION pgbouncer.get_auth(p_usename text)
RETURNS TABLE (usename text, passwd text)
LANGUAGE sql STABLE SECURITY DEFINER
SET search_path = pg_catalog
AS $$
  SELECT rolname::text,
         CASE WHEN rolvaliduntil < now() THEN NULL ELSE rolpassword END
  FROM pg_authid
  WHERE rolname = p_usename AND rolcanlogin
$$;

REVOKE ALL ON FUNCTION pgbouncer.get_auth(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION pgbouncer.get_auth(text) TO pgbouncer_auth;
