-- 03-rls.sql — Row Level Security on support.conversations
--
-- HOW THE API SAYS "WHO IS ASKING":
--   at the start of every request transaction it runs
--     SELECT set_config('app.company_id', '<uuid>', true),
--            set_config('app.user_id',    '<uuid>', true),
--            set_config('app.role',       'agent' | 'supervisor', true);
--   The final `true` = "local to this transaction" (same as SET LOCAL). The values vanish
--   at COMMIT/ROLLBACK, so a pooled connection can never leak them to the next request.

-- ---- Helper functions --------------------------------------------------------------
-- current_setting(name, true) returns NULL if never set... but once a setting has been
-- used on a (pooled) connection, it comes back as '' (empty string) afterwards.
-- ''::uuid would raise an ERROR, so NULLIF(…, '') turns it back into NULL.
-- NULL never equals anything → no rows match → "no context = no data" (fail-safe).
SET ROLE app_owner;

CREATE FUNCTION support.current_company_id() RETURNS uuid
  LANGUAGE sql STABLE AS $$ SELECT NULLIF(current_setting('app.company_id', true), '')::uuid $$;

CREATE FUNCTION support.current_user_id() RETURNS uuid
  LANGUAGE sql STABLE AS $$ SELECT NULLIF(current_setting('app.user_id', true), '')::uuid $$;

CREATE FUNCTION support.current_app_role() RETURNS text
  LANGUAGE sql STABLE AS $$ SELECT NULLIF(current_setting('app.role', true), '') $$;

-- ---- Turn RLS on ----------------------------------------------------------------------
-- From now on, for every role except the owner and superusers: no matching policy = no rows.
ALTER TABLE support.conversations ENABLE ROW LEVEL SECURITY;

-- NOTE: we deliberately do NOT run "FORCE ROW LEVEL SECURITY" here, so the lab can show
-- the trap (the table OWNER still sees everything). The fix is in ../demo/force-rls.sql.

-- ---- Policies -------------------------------------------------------------------------
-- RESTRICTIVE policies are AND-ed with everything. PERMISSIVE policies are OR-ed together.
-- Final rule for a row:  (all restrictive pass)  AND  (at least one permissive passes)

-- 1. Tenant isolation — the safety net. Applies to every command (SELECT/INSERT/UPDATE/DELETE).
--    USING checks rows you read/modify; WITH CHECK checks rows you write.
CREATE POLICY tenant_isolation ON support.conversations
  AS RESTRICTIVE FOR ALL
  USING      (company_id = support.current_company_id())
  WITH CHECK (company_id = support.current_company_id());

-- 2. Agents read conversations assigned to them, plus the unassigned queue.
CREATE POLICY agent_read ON support.conversations
  AS PERMISSIVE FOR SELECT
  USING (assigned_to = support.current_user_id() OR assigned_to IS NULL);

-- 3. Supervisors read every conversation (of their own company — policy 1 still applies).
CREATE POLICY supervisor_read ON support.conversations
  AS PERMISSIVE FOR SELECT
  USING (support.current_app_role() = 'supervisor');

-- 4. Updates: agents only their own conversations; supervisors any in their company.
CREATE POLICY agent_update ON support.conversations
  AS PERMISSIVE FOR UPDATE
  USING      (assigned_to = support.current_user_id() OR support.current_app_role() = 'supervisor')
  WITH CHECK (assigned_to = support.current_user_id() OR support.current_app_role() = 'supervisor'
              OR assigned_to IS NULL);

-- 5. Inserts: allowed, but only into your own company (policy 1's WITH CHECK enforces it too).
CREATE POLICY create_in_own_company ON support.conversations
  AS PERMISSIVE FOR INSERT
  WITH CHECK (company_id = support.current_company_id());

RESET ROLE;
