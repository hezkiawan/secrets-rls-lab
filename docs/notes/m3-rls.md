# M3 notes — Row Level Security (PostgreSQL)

Tested end to end on 2026-10-05: OpenBao 2.7.1 → Go API → PgBouncer (transaction pooling) → PostgreSQL.
Files: `reference/postgres/initdb/*.sql`, `reference/pgbouncer/`, `api/internal/db/db.go`, `api/conversations.go`.

## The idea in one line
The API's queries have **no** `WHERE company_id = …`. At the start of each request's transaction the API tells Postgres
who is asking; **policies on the table** decide which rows exist for that person.

```
BEGIN;
SELECT set_config('app.company_id', '<uuid>', true),   -- true = only for this transaction (SET LOCAL)
       set_config('app.user_id',    '<uuid>', true),
       set_config('app.role',       'agent',  true);
SELECT … FROM support.conversations;                    -- RLS filters this
COMMIT;                                                 -- settings disappear: pooler-safe
```

## Roles (the part that fixes the team's current setup)
| Role | Purpose | RLS applies? |
|---|---|---|
| `postgres` | superuser, setup only | never (superusers bypass) |
| `app_owner` | owns the tables, runs migrations | **no, unless `FORCE ROW LEVEL SECURITY`** |
| `app_runtime` | what the API uses; owns nothing | yes |
| OpenBao's `v-…` users (M5) | members of `app_runtime` | yes |

**The trap:** "one DB login for everything" usually created the tables → it is the owner → RLS looks enabled but protects nothing.
**Fix, in order:** (1) split owner (migrations) from runtime (app) logins; (2) `ALTER TABLE … FORCE ROW LEVEL SECURITY` as a safety net.

## Policies on `support.conversations`
| Policy | Type | Rule |
|---|---|---|
| `tenant_isolation` | RESTRICTIVE, all commands | `company_id = current company` (USING + WITH CHECK) |
| `agent_read` | PERMISSIVE, SELECT | assigned to me, or unassigned |
| `supervisor_read` | PERMISSIVE, SELECT | my role is supervisor |
| `agent_update` | PERMISSIVE, UPDATE | my own (supervisors: any in the company) |
| `create_in_own_company` | PERMISSIVE, INSERT | WITH CHECK company = mine |

Combination rule: **all RESTRICTIVE must pass AND at least one PERMISSIVE must pass.** No permissive match → no access.

## Verified results
| Test | Result |
|---|---|
| Ana (agent, Acme) | her 2 + unassigned |
| Sari (supervisor, Acme) | all Acme rows, no Bumi rows |
| Dewi / Eko (Bumi) | only Bumi rows |
| No context set | **0 rows** (fail-safe, not "all rows") |
| Insert into the other company | `ERROR: new row violates row-level security policy` (SQLSTATE 42501) → API returns 403 |
| Budi updates Ana's conversation | 0 rows changed → 404 "not found or not allowed" |
| Same query as table owner, no FORCE | **all companies leak** |
| Same query as table owner, with FORCE | only Ana's rows |
| Through PgBouncer, after COMMIT on same connection | context gone → 0 rows |

## Gotchas worth knowing
- **Empty-string setting:** once `app.company_id` has been used on a pooled connection, `current_setting(…, true)` returns `''`
  afterwards (not NULL). `''::uuid` raises an error, so the helper functions use `NULLIF(…, '')`.
- **Plain `SET` is unsafe behind PgBouncer transaction pooling** (the setting stays on the server connection and can leak to
  another client). Use `SET LOCAL` / `set_config(…, true)` inside a transaction.
- **pgx behind PgBouncer:** we set `QueryExecModeExec` (unnamed statements) so pgx's prepared-statement cache can't break.
- **UPDATE/DELETE that RLS hides return "0 rows"**, not an error. Treat 0 as "not found or not allowed".
- **Index the tenant column** (`company_id`, and `(company_id, assigned_to)`): RLS adds that filter to every query.
- **RLS is rows, not columns.** Hiding a column (e.g. customer phone) needs column privileges or a view.
- **Directory tables** (`users`, `companies`) have no RLS in this lab. In production protect them too, and do the login lookup
  through a narrow `SECURITY DEFINER` function.

## PgBouncer: why `auth_query`
PgBouncer checks passwords itself. A fixed `userlist.txt` can't know users created later (e.g. OpenBao's temporary users).
With `auth_user` + `auth_query`, PgBouncer asks Postgres through `pgbouncer.get_auth()` (a `SECURITY DEFINER` function, same logic
as PgBouncer's documented default: users past `VALID UNTIL` get no password → refused).
