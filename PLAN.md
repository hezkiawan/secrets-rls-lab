# Plan — Secrets Management + RLS Lab

**Goal:** one small, working system that teaches the concepts, shows the team how they'd work in our stack (Go + Fiber v3, PostgreSQL, VMs), and feeds the research doc and presentation. Every milestone ends with a demo moment that works on its own.

## Deliverables
1. **This lab repo** — `docker compose up` + a Go/Fiber API.
2. **Research doc** — comparison, deployment guide, RLS guide, Firebase section, recommendation, rollout plan.
3. **Slides + live demo.**

## Design choices
- Infrastructure runs in Docker; the Go API runs on the host with `go run` (fast edit-run, debugger).
- Every setup step is done twice: **GUI first** (OpenBao UI / pgAdmin), then **as code** in `bootstrap/` → honest "GUI vs setup-as-code" recommendation.
- Demo domain: a mini multi-tenant support desk (companies, agents, tickets).

## Team answers (2026-10-02) and what they changed

| # | Team answer | Impact on the plan |
|---|---|---|
| 1 | "Filtering" = access control | M1 gets a **policy showcase** (read/list/write, deny, parameter constraints) + **namespaces per product** (free in OpenBao, Enterprise-only in Vault) |
| 2 | Deploy to **Tencent Cloud VMs** | No native Tencent KMS auto-unseal in OpenBao or Vault (note: OpenBao's `tcloudpublic` seal is **T Cloud Public / T-Systems**, not Tencent). → **Transit auto-unseal becomes required in M5**; doc gets a Tencent deployment section (manual vs Transit vs HSM/PKCS#11 vs another cloud's KMS). No Tencent auth method → **AppRole** for apps. |
| 3 | Priority: free open source; also research Enterprise | Vault Community is free but **not open source (BSL)** → OpenBao is the main candidate. M4 = compatibility + measurement. Doc: Vault Enterprise features vs OpenBao free equivalents. |
| 4 | RLS: "all" — show what's possible | M3 covers **per-company, per-user, per-role**, permissive vs restrictive policies, limits (column privileges). |
| 5 | They use the Go pool + **PgBouncer** | M3 runs **through PgBouncer** (transaction mode) to prove `SET LOCAL` is pooler-safe. M2/doc: dynamic credentials need PgBouncer **`auth_query`** (a static `auth_file` can't know new users). Pool mode / auth method not asked → doc states "check `pgbouncer.ini`: if X, do Y". |
| 6 | **Firestore + Realtime DB**, read from FE and BE | Doc covers Rules for both; FE reads protected by Rules, BE (Admin SDK) bypasses Rules → authorization in Go. Firebase service-account key stored in OpenBao (M1). |

| 7 | **One shared DB login for all queries** ("for now") | This is the "before" state the lab starts from (M0 uses one superuser login). M2 adds a **migration path**: step 1 = OpenBao **static role** rotates the existing shared password (same username, no app redesign); step 2 = **dynamic roles**, one short-lived login per service. M3: that shared login very likely **owns the tables → it bypasses RLS** → safe rollout must **split an owner/migration role from the runtime role** (or use `FORCE ROW LEVEL SECURITY`). It also confirms RLS must identify users via **per-request session settings**, not per-user DB logins. |
| 8 | Postgres is **self-hosted on a VM**; curious about managed for the future | Good news: they control superuser and `pgbouncer.ini`, so dynamic credentials and `auth_query` are both possible. Doc gets an appendix **"Self-hosted vs managed Postgres"** and how each choice affects RLS, dynamic credentials and PgBouncer (TencentDB specifics to be verified from Tencent docs). |

All scoping questions are answered.

## Milestones

| # | Milestone | Concepts | Go |
|---|---|---|---|
| M0 ✅ | Skeleton: Compose (OpenBao dev, Postgres, pgAdmin), Fiber `/livez` `/readyz` `/status` | Dev mode vs production | modules, packages, errors, structs/JSON |
| M1 core ✅ (code) | API logs in with **AppRole**, loads JWT signing key + API key + Firebase service-account key from OpenBao. **Policy showcase** (filtering) + **namespaces per product** | auth methods, tokens, policies, KV v2, namespaces, least privilege | client library, config, packages |
| M2-lite ✅ | **Database credentials from OpenBao**, in the order the team would adopt them: (1) **static role** — OpenBao rotates the password of the existing shared login; (2) **dynamic roles** — temporary per-service users (members of a non-owner role), renewal, pool rotation. PgBouncer `auth_query` note | static vs dynamic secrets, leases, TTL, revoke | goroutines, `context`, `pgx` pool |
| M3 ✅ | **RLS through PgBouncer**: per-company, per-user, per-role; permissive vs restrictive; buggy query still safe; cross-tenant insert blocked; fail-safe when unset; **owner-bypass trap (= the team's current shared login) + fix: separate owner vs runtime role, `FORCE`** | policies, `USING`/`WITH CHECK`, `SET LOCAL`, pooler safety, indexes, safe rollout | middleware, transactions, `defer` |
| M4-lite ✅ | *(smaller)* Same code + scripts against **HashiCorp Vault**; measure image size / idle memory | compatibility, migration risk | — |
| M5 ✅ | **Production-like** (`reference/`): 3-node Raft cluster + HAProxy, **Transit auto-unseal** (the Tencent answer), init/configure as code, `.env` → KV import, **dynamic DB credentials through PgBouncer with automatic renewal/rotation**, kill leader (HA), full restart → auto-unseal, Raft snapshot backup/restore, declarative audit log | deployment operations, leases | goroutines, `LifetimeWatcher`, atomic pool swap |
| M6 | *(optional)* Firebase emulator: Firestore + Realtime DB Rules vs Admin SDK | Firebase access control | — |

## Research doc outline
1. Summary + recommendation
2. Secrets management concepts
3. Vault vs OpenBao: features, license, enterprise, resources, filtering, support/risk
4. How it works + Go/Fiber integration (M1–M2)
5. Deploying on VMs: HA, unseal options per cloud, backups, TLS, multi-cloud, GUI vs setup-as-code (M5)
6. Rollout plan for live products (from today's one shared DB login → static role → dynamic roles; RLS: split owner/runtime role first)
7. RLS in PostgreSQL (M3)
8. Firebase: Firestore + Realtime DB Rules vs Admin SDK, SQL Connect
9. Open questions & risks
- Appendix A: Self-hosted vs managed PostgreSQL (TencentDB) — effect on RLS, dynamic credentials, PgBouncer

**Resources section** combines: official sizing (Vault reference architecture; OpenBao recommends 5-node Raft) + our lab measurement (relative only) + real-world reference (GitLab's OpenBao sizing) + ops effort.

## Schedule
| When | Work |
|---|---|
| Day 1 (evening) | M1 core: AppRole login + KV secrets + policy showcase · Claude drafts comparison research |
| Day 2 | M1 namespaces (AM) → M2 → M3 |
| Day 3 | M5 + quick M4 (AM) · doc + slides (PM) · rehearsal |

**Cut order if behind:** M6 → M4 live swap (keep measurement) → M5 load balancer.
**Never cut:** M1, M2, M3, M5 unseal + Transit auto-unseal + backup.
