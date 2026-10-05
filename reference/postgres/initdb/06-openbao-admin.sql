-- 06-openbao-admin.sql — the login OpenBao uses to create temporary database users (M5).
--
-- A dedicated role, NOT the postgres superuser:
--   CREATEROLE          → may create/drop the temporary users
--   ADMIN on app_runtime → may make each temporary user a MEMBER of app_runtime,
--                          so it gets exactly app_runtime's rights — and RLS applies to it.
--
-- The password below is only the INITIAL one: reference/scripts/configure.sh calls
-- database/rotate-root right after setup, and from then on ONLY OpenBao knows it.
CREATE ROLE openbao_admin LOGIN CREATEROLE PASSWORD 'openbao-admin-initial';
GRANT app_runtime TO openbao_admin WITH ADMIN OPTION;
