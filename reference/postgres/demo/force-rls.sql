-- force-rls.sql — THE FIX for the "table owner bypasses RLS" trap.
--
-- By default, Postgres does not apply RLS to the table's owner. If an app connects as the
-- owner (very common: "the one DB login we use for everything" created the tables),
-- RLS looks enabled but protects nothing.
--
-- FORCE makes RLS apply to the owner too. (Superusers and BYPASSRLS roles still bypass.)
-- Run it:
--   docker compose exec -T postgres psql -U postgres -d supportdesk -f /demo/force-rls.sql
ALTER TABLE support.conversations FORCE ROW LEVEL SECURITY;

-- Undo (to replay the demo):
--   ALTER TABLE support.conversations NO FORCE ROW LEVEL SECURITY;
