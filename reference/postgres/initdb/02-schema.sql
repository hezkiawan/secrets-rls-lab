-- 02-schema.sql — tables for a mini multi-tenant conversation desk (Kouventa-like)
--
-- Every protected table carries company_id ON ITSELF, so RLS policies never need joins.

CREATE SCHEMA support AUTHORIZATION app_owner;

-- Create everything AS app_owner, so app_owner (not postgres) owns the tables.
SET ROLE app_owner;

CREATE TABLE support.companies (
  id   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL
);

CREATE TABLE support.users (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  company_id uuid NOT NULL REFERENCES support.companies(id),
  name       text NOT NULL,
  email      text NOT NULL UNIQUE,
  role       text NOT NULL CHECK (role IN ('agent', 'supervisor'))
);

CREATE TABLE support.conversations (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  company_id    uuid NOT NULL REFERENCES support.companies(id),
  assigned_to   uuid REFERENCES support.users(id),          -- NULL = unassigned queue
  customer_name text NOT NULL,
  channel       text NOT NULL CHECK (channel IN ('whatsapp', 'instagram', 'email', 'webchat')),
  subject       text NOT NULL,
  status        text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'pending', 'closed')),
  created_at    timestamptz NOT NULL DEFAULT now()
);

-- RLS adds "company_id = …" to every query, so index it (performance matters on big tables).
CREATE INDEX conversations_company_idx          ON support.conversations (company_id);
CREATE INDEX conversations_company_assignee_idx ON support.conversations (company_id, assigned_to);

RESET ROLE;

-- Runtime role: read/write rows only. No DELETE, no DDL, owns nothing.
GRANT USAGE ON SCHEMA support TO app_runtime;
GRANT SELECT ON support.companies, support.users TO app_runtime;
GRANT SELECT, INSERT, UPDATE ON support.conversations TO app_runtime;
