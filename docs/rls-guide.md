# Row Level Security (RLS) in PostgreSQL — guide

**Who this is for:** the production team. It covers what RLS can do, how to write it, the traps, and how to roll it out on a live product.
**Working example:** `reference/postgres/initdb/03-rls.sql` + `api/internal/db/db.go` (milestone M3, tested through PgBouncer).
**Sources:** PostgreSQL 18 docs ([Row Security Policies](https://www.postgresql.org/docs/current/ddl-rowsecurity.html), [CREATE POLICY](https://www.postgresql.org/docs/current/sql-createpolicy.html)), [PgBouncer docs](https://www.pgbouncer.org/features.html). Checked 2026-10-06. RLS exists since PostgreSQL 9.5, and the current version is 18.

---

## 1. The idea in one picture

```
Without RLS                                   With RLS
-----------                                   --------
SELECT * FROM conversations                   SELECT * FROM conversations
WHERE company_id = $1   ← every query must    ← no WHERE needed: Postgres adds the
                          remember this          policy's condition to every query
```

- RLS = **rules attached to a table** that decide which rows each request can see or change.
- They apply to **every query**, including the one someone forgets to filter.
- RLS is **defense in depth**. The Go code should still check permissions; RLS is the safety net when it doesn't.

---

## 2. Turning it on

```sql
ALTER TABLE support.conversations ENABLE ROW LEVEL SECURITY;  -- policies now apply
ALTER TABLE support.conversations FORCE  ROW LEVEL SECURITY;  -- ...to the table owner too
```

| After `ENABLE` | Effect |
|---|---|
| No policies on the table | **Default deny**: no rows visible, nothing can be modified |
| Superuser or role with `BYPASSRLS` | **Always bypasses**, even with `FORCE` |
| Table **owner** | Bypasses, **unless `FORCE`** |
| Everyone else | Sees only rows the policies allow |
| `TRUNCATE` | Not subject to RLS (whole-table operation). Only grant it to roles that should have it |

> ⚠️ **The #1 trap (our team's current state):** if the app logs in with the user that *created* the tables, it is the owner → RLS looks enabled but filters nothing. Fix: a separate **owner/migration role** and **runtime role**, plus `FORCE` as a safety net. Shown live in M3 (`/demo/as-owner`).

---

## 3. Writing a policy

```sql
CREATE POLICY name ON table
  AS { PERMISSIVE | RESTRICTIVE }              -- default PERMISSIVE
  FOR { ALL | SELECT | INSERT | UPDATE | DELETE } -- default ALL
  TO { role | PUBLIC }                         -- default PUBLIC (everyone)
  USING ( condition on existing rows )
  WITH CHECK ( condition on new/changed rows );
```

### USING vs WITH CHECK

| Clause | Checks | When it fails |
|---|---|---|
| `USING` | **Existing** rows: which you can read, update, delete | Row is **silently hidden** (no error) |
| `WITH CHECK` | **New** row values: what you insert, or what a row becomes after UPDATE | **Error** `new row violates row-level security policy` (SQLSTATE `42501`) |

Which command uses what:

| Command | `USING` | `WITH CHECK` |
|---|---|---|
| `SELECT` | ✅ | ❌ not allowed |
| `INSERT` | ❌ not allowed | ✅ |
| `UPDATE` | ✅ (which rows can be targeted) | ✅ (what they may become). If omitted, `USING` is reused |
| `DELETE` | ✅ | ❌ |
| `ALL` | ✅ | ✅. If you omit `WITH CHECK`, `USING` is reused for it (verified) |

Details from the docs:
- `UPDATE … WHERE …` or `RETURNING` also needs the row to pass the **SELECT** policies (it reads columns).
- `INSERT … ON CONFLICT DO UPDATE`: if the existing row fails `USING`, you get an **error**, not a silent skip.
- An UPDATE/DELETE on a hidden row reports **0 rows affected**. Treat 0 as "not found or not allowed" (our API returns 404).

### PERMISSIVE vs RESTRICTIVE

```
row allowed  =  (ALL restrictive policies pass)  AND  (AT LEAST ONE permissive policy passes)
```

- **Permissive** = "ways in" (OR-ed). Agent sees own rows **or** supervisor sees all.
- **Restrictive** = "walls" (AND-ed). Must always be in the same company.
- You need **at least one permissive** policy. With only restrictive ones, nobody sees anything.
- Good pattern: put tenant isolation in **one RESTRICTIVE** policy. Then a mistake in any permissive policy can never leak across companies.

Our M3 policies:

| Policy | Type | Command | Rule |
|---|---|---|---|
| `tenant_isolation` | RESTRICTIVE | ALL | `company_id = current company` |
| `agent_read` | permissive | SELECT | assigned to me, or unassigned |
| `supervisor_read` | permissive | SELECT | I am a supervisor |
| `agent_update` | permissive | UPDATE | mine (supervisors: any) |
| `create_in_own_company` | permissive | INSERT | `company_id = mine` |

Change policies with `ALTER POLICY` (roles, `USING`, `WITH CHECK` only), or drop and recreate. List them with `SELECT * FROM pg_policies;`. Only the **table owner** can create or change policies.

---

## 4. Telling Postgres "who is asking"

Policies need to know the current user, company and role. Options:

| Option | How | Verdict |
|---|---|---|
| **A. Transaction settings** *(our choice)* | `set_config('app.company_id', $1, true)` at the start of each request transaction; policies read `current_setting('app.company_id', true)` | ✅ Works with one shared login + PgBouncer. Used in M3 |
| B. JWT claims as one setting | Same as A, but put the whole verified JWT in one setting (`app.jwt`) and read fields with `->>` | ✅ Variant of A. Handy if many claims |
| C. One DB login per end user | Policies use `current_user` | ❌ Thousands of DB roles, breaks connection pooling |
| D. Membership table | Policy does `EXISTS (SELECT 1 FROM memberships WHERE user_id = <A's user> AND team_id = row.team_id)` | ✅ Combine with A for teams/groups (see §5) |

**Why `set_config(…, true)` and not `SET`:**
- `true` = local to the **transaction**, same as `SET LOCAL`. The values disappear at `COMMIT`/`ROLLBACK`.
- PgBouncer in **transaction pooling** gives your connection to someone else after each transaction. PgBouncer lists plain `SET` as **never compatible** with transaction pooling, because the value would stay on the connection and leak to the next client.
- We verified this: after `COMMIT`, the same pooled connection had no context → 0 rows.

The Go side (`api/internal/db/db.go`):

```go
pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{}, func(tx pgx.Tx) error {
    tx.Exec(ctx, `SELECT set_config('app.company_id', $1, true),
                         set_config('app.user_id',    $2, true),
                         set_config('app.role',       $3, true)`, companyID, userID, role)
    return fn(tx) // every query of this request runs inside this transaction
})
```

The values must come from the **verified login** (JWT), never from the request body.

**Fail-safe helper functions:**

```sql
CREATE FUNCTION support.current_company_id() RETURNS uuid LANGUAGE sql STABLE AS
$$ SELECT NULLIF(current_setting('app.company_id', true), '')::uuid $$;
```
- `current_setting(name, true)` → NULL instead of an error if not set.
- `NULLIF(…, '')`: on a pooled connection that used the setting before, it comes back as `''`, not NULL.
- NULL never equals anything → **no context = no rows** (verified). Never write it so that "not set" means "all rows".

---

## 5. What's possible: pattern catalog

All of these are plain SQL expressions in `USING` / `WITH CHECK`.

| # | Pattern | Example condition | Notes |
|---|---|---|---|
| 1 | **Tenant isolation** | `company_id = current_company_id()` | RESTRICTIVE, FOR ALL. The foundation |
| 2 | **Ownership** | `owner_id = current_user_id()` | e.g. "my drafts" |
| 3 | **Role-based** | `current_app_role() = 'supervisor'` | Role from the JWT, set per transaction |
| 4 | **Assignment / queue** | `assigned_to = me OR assigned_to IS NULL` | Our agent rule |
| 5 | **Team membership** | `EXISTS (SELECT 1 FROM team_members m WHERE m.team_id = conversations.team_id AND m.user_id = current_user_id())` | Index `team_members(user_id, team_id)` |
| 6 | **Different rules per command** | read: whole team; update: only own; delete: only supervisors | One policy per command |
| 7 | **Visibility by state/time** | `published_at <= now()`, `deleted_at IS NULL` | Soft-delete hiding |
| 8 | **Prevent moving rows** | `WITH CHECK (company_id = current_company_id())` | Blocks "move this record to another company" (tested: spoofed insert → 403) |
| 9 | **Read-only role** | Only a `FOR SELECT` policy `TO reporting_role` | Plus only `GRANT SELECT` |
| 10 | **Policies per DB role** | `TO app_runtime` vs `TO support_tools` | Different services, different rules |
| 11 | **Admin / background jobs** | A separate login with `BYPASSRLS` | Use it rarely, and audit it. Prefer setting a context even for jobs |
| 12 | **Hierarchy** (company → branch) | `branch_id IN (SELECT branch_id FROM user_branches WHERE user_id = current_user_id())` | Keep subqueries indexed |

### What RLS does **not** do (use something else)

| Need | Use |
|---|---|
| Hide **columns** (e.g. customer phone) | Column privileges `GRANT SELECT (col1, col2)`, or a view |
| Mask values (`08xx-xxxx`) | A view or the app |
| Protect `TRUNCATE` | Don't grant `TRUNCATE` to the runtime role |
| Rate limits, field validation | App / `CHECK` constraints |

---

## 6. Traps and gotchas

| Trap | What happens | Fix |
|---|---|---|
| App user **owns** the tables | RLS ignored | Separate owner and runtime roles + `FORCE` |
| App user is **superuser / `BYPASSRLS`** | RLS ignored, even with `FORCE` | Runtime role must be neither. On **TencentDB**, the admin role `pg_tencentdb_superuser` has `BYPASSRLS`, so the app must not use it |
| **Views** | By default run with the **view owner's** rights → may bypass RLS (verified: plain view showed 3 rows, table 1) | PostgreSQL 15+: `CREATE VIEW … WITH (security_invoker = true)` |
| **`SECURITY DEFINER` functions** | Run as their owner → may bypass RLS | Keep them tiny; set `search_path`; review |
| Plain `SET` behind PgBouncer | Context leaks to another client | `set_config(…, true)` inside a transaction |
| "Not set" handled wrongly | Could return **all** rows | NULL-safe helpers (§4); test the "no context" case |
| Unique / foreign-key checks | Bypass RLS, so they can reveal that a value **exists** in another tenant (e.g. "email already used"). Verified | Make unique keys per tenant: `UNIQUE (company_id, email)` |
| `COPY … FROM` | Not supported on RLS tables | Use `INSERT` |
| `pg_dump` | Sets `row_security = off`, so it **errors** instead of dumping partial data | Run backups as owner/superuser |
| Hidden rows on UPDATE/DELETE | 0 rows, no error | Treat as 404 |
| pgx + PgBouncer prepared statements | Errors with transaction pooling | `QueryExecModeExec` (ours) or PgBouncer `max_prepared_statements` (1.21+) |

---

## 7. Performance

- RLS adds the policy condition to every query → **index the columns policies use**: `(company_id)`, `(company_id, assigned_to)`, membership tables.
- Mark helper functions `STABLE` (ours are), not `VOLATILE`.
- Postgres evaluates the policy conditions **before** the query's own `WHERE` conditions, unless those conditions use **LEAKPROOF** functions/operators (most built-in comparisons are). The docs warn that otherwise an index may not be usable for them. Check plans with `EXPLAIN ANALYZE` **as the runtime role** with context set.
- Subquery policies (pattern 5, 12) cost more. Keep them simple and indexed.

---

## 8. Testing RLS

```sql
BEGIN;
SET LOCAL ROLE app_runtime;                                   -- test as the app, not as superuser!
SELECT set_config('app.company_id', '1111…', true),
       set_config('app.user_id', 'a000…', true),
       set_config('app.role', 'agent', true);
SELECT count(*) FROM support.conversations;                   -- expect Ana's rows + the unassigned queue
ROLLBACK;
```

Minimum test list (all verified in M3):
1. Each role sees exactly the expected rows.
2. **No context → 0 rows.**
3. Insert into another tenant → error `42501`.
4. Update someone else's row → 0 rows.
5. Same query as the owner without/with `FORCE`.
6. After `COMMIT` through PgBouncer → context gone.

Put these in CI against a real Postgres.

---

## 9. Rollout on a live product

The team today uses **one shared login** that very likely owns the tables.

| Step | Action | Risk |
|---|---|---|
| 1 | Create `app_owner` (migrations) and `app_runtime` (app). `ALTER TABLE … OWNER TO app_owner`, grant runtime only `SELECT/INSERT/UPDATE/DELETE` | Low: permissions only |
| 2 | Switch the app to `app_runtime` (secret from OpenBao) | Medium: find missing grants in staging |
| 3 | Add `WithTenant` to every request path. Verify with logs that context is always set | Medium: code change |
| 4 | Add indexes on policy columns | Low |
| 5 | `CREATE POLICY` + `ENABLE` **one table at a time**, starting with the most sensitive | Missing context = empty results, so test in staging first |
| 6 | `FORCE ROW LEVEL SECURITY` | Low after step 1 |
| 7 | Move to OpenBao dynamic users (`IN ROLE app_runtime`) | See the deployment guide |

---

## 10. Self-hosted vs managed Postgres

| | Self-hosted on VM (today) | TencentDB for PostgreSQL |
|---|---|---|
| RLS | ✅ full | ✅ PostgreSQL feature (versions 10–18 offered) |
| Superuser | Yes | **No.** You get `pg_tencentdb_superuser` (has `CREATEROLE` and **`BYPASSRLS`**) |
| OpenBao dynamic users | ✅ (`CREATEROLE` admin) | Likely ✅ via a `CREATEROLE` account. **Test before relying on it** |
| PgBouncer `auth_query` | ✅ | Run PgBouncer on your own VM; the `SECURITY DEFINER` lookup function must be created by a role allowed to read passwords. **Test** |

Sources: [TencentDB privileges](https://intl.cloud.tencent.com/document/product/409/43241), [TencentDB versions](https://intl.cloud.tencent.com/document/product/409/44365). The `pg_tencentdb_superuser` details are from Tencent's Chinese-language docs.
