# Secrets Management (OpenBao vs HashiCorp Vault) and Row Level Security — research report

**For:** production team · **By:** Hezki (technical research intern) · **Date:** 2026-10-06
**Versions checked:** OpenBao 2.7.1 (2026-10-01), Vault Community 2.1.1 (2026-09-16), PostgreSQL 18 (lab tested on 16/18), PgBouncer 1.26 latest (lab: 1.22/1.24), Fiber v3.5.0
**Evidence:** everything marked *tested* was run in this repo (`secrets-rls-lab`). Every claim from documentation links to the official source (list in §12).

| Read this… | …for |
|---|---|
| This report | Summary, comparison, how it works, recommendation |
| [`deployment-guide.md`](deployment-guide.md) | How to run OpenBao on Tencent VMs |
| [`rls-guide.md`](rls-guide.md) | Everything about PostgreSQL RLS |
| [`firebase-guide.md`](firebase-guide.md) | "RLS" in Firestore / Realtime DB |
| [`../reference/README.md`](../reference/README.md) | Hands-on: the working cluster + RLS demo |
| [`comparison-findings.md`](comparison-findings.md) | Raw measurements OpenBao vs Vault |

---

## 1. Summary

**Recommendation: adopt OpenBao,** deployed on Tencent VMs as a 3-node cluster (5 later) with **transit auto-unseal**. Roll it out in steps: `.env` files → static DB credentials → RLS → dynamic DB credentials.

| Question | Answer |
|---|---|
| Vault or OpenBao? | **OpenBao.** Same API and tooling, truly open source (MPL 2.0). Features Vault charges for are free: **namespaces**, standby reads, control groups. Vault's licence (BSL) allows our internal use, but it isn't open source and is now owned by IBM |
| Can it run on our VMs? | Yes. *Tested:* 3-node cluster + load balancer survived node loss and full restarts, with automatic unsealing |
| Tencent-specific issue? | Tencent sells a KMS, but **neither product has a seal for it**, so it can't auto-unseal OpenBao/Vault. Solution: **transit auto-unseal** (a small second OpenBao), built in and *tested* |
| Can `.env` files go away? | Yes. *Tested:* `.env` → KV import; the API loads every secret from OpenBao at startup |
| Dynamic DB credentials? | Yes, including through **PgBouncer** (needs `auth_query`). *Tested:* temporary users rotated automatically with no failed requests |
| Resources? | Small. Idle ~34 MiB in the lab. Official guidance for "small" production nodes is 2–4 cores / 8–16 GB / 3,000+ IOPS |
| RLS in PostgreSQL? | Yes, and it works with our **one shared login + PgBouncer** (per-transaction settings). *Tested.* **Blocker today:** the shared login probably owns the tables, so RLS would be ignored. Split owner/runtime roles first |
| RLS in Firebase? | For **client** reads/writes, **Security Rules** do the same job. The Go **Admin SDK bypasses them**, so backend reads need checks in Go |

---

## 2. The problem today

| Today | Risk |
|---|---|
| Secrets in `.env` files on each VM | Copied around, in backups, maybe in git history. No record of who read what. Rotation = manual edits everywhere |
| One shared Postgres login for all queries | Never rotated (would break everything). If leaked, it reaches all data. It owns the tables, so RLS can't protect anything |
| Tenant filtering only in Go `WHERE` clauses | One forgotten `WHERE company_id = …` = data leak between customers |

A secrets manager fixes the first two. RLS fixes the third.

---

## 3. Key concepts

| Term | Meaning |
|---|---|
| **Secret** | Anything that grants access: passwords, API tokens, keys |
| **Static vs dynamic secret** | Static = stored value (an API token). Dynamic = **created on request**, expires by itself (a temporary DB user) |
| **Auth method** | How a client proves who it is (AppRole for apps, OIDC/userpass for people) |
| **Token** | What you get after login. Carries **policies** and a **TTL** |
| **Policy** | Rules on **paths**: what a token may read/write. Deny by default |
| **Secrets engine** | A plugin mounted at a path: KV (store values), database (DB users), transit (encryption), PKI (certificates)… |
| **Lease** | Lifetime of a dynamic secret/token. Renew before expiry; get a new one at **max TTL** |
| **Seal / unseal** | Data on disk is encrypted. After restart OpenBao is **sealed** until given the root key (manually or **auto-unseal**) |
| **Raft** | Built-in replicated storage: one active leader, standbys copy the data |
| **Audit device** | Log of every request (values hashed) |
| **Namespace** | An isolated "mini OpenBao" inside one cluster (own secrets, policies, admins) |
| **RLS** | PostgreSQL rules that filter rows per request |

---

## 4. OpenBao vs HashiCorp Vault

### 4.1 Background

| | OpenBao | HashiCorp Vault |
|---|---|---|
| Origin | Fork of Vault, taken just before the licence change (between 1.14.8 and 1.14.9) | The original |
| Licence | **MPL 2.0** (open source) | **BSL 1.1** since 1.15 (Aug 2023): production use allowed, *"Hosting or using the Licensed Work(s) for internal purposes within an organization is not considered a competitive offering."* Each version becomes MPL 2.0 four years after release |
| Owner / governance | Community project in the **OpenSSF** (Linux Foundation) since May 2025 | **IBM** (acquisition completed 2025-02-27) |
| Latest | 2.7.1 (2026-10-01), releases roughly monthly | 2.1.1 Community (2026-09-16). Vault jumped from 1.21 to 2.0 in April 2026 with IBM's versioning |
| Paid version | None. Vendors offer support/managed OpenBao (e.g. Adfinis, ControlPlane, Kubermatic) | Vault Enterprise (self-managed) and HCP Vault Dedicated (managed) |
| API | Same HTTP API (`/v1/...`, `X-Vault-Token` header) | — |

### 4.2 Features: what's free where

✅ = available free · 💲 = Vault Enterprise only · ❌ = not available

| Feature | OpenBao | Vault Community | Vault Enterprise |
|---|---|---|---|
| KV, database, transit, PKI, SSH, TOTP engines | ✅ | ✅ | ✅ |
| AppRole, JWT/OIDC, Kubernetes, userpass, cert auth | ✅ | ✅ | ✅ |
| LDAP auth, cloud (AWS/GCP/Azure) auth & engines | Plugins (separate install) | ✅ | ✅ |
| Dynamic + static DB credentials (Postgres, MySQL, …) | ✅ | ✅ | ✅ |
| Raft HA cluster | ✅ | ✅ | ✅ |
| PostgreSQL as storage (HA) | ✅ (2.7+, officially recommended for newcomers) | Community-supported only | Community-supported only |
| **Namespaces** (per product isolation) | ✅ (*tested*) | 💲 (*tested*: `404 enterprise-only feature`) | ✅ |
| **Standby nodes serving reads** | ✅ (2.5+, like Vault's "performance standbys") | ❌ | 💲 |
| **Control groups** (second person must approve) | ✅ (2.7+) | ❌ | 💲 |
| Login MFA (TOTP etc.) | ✅ | ✅ | ✅ (+ step-up MFA 💲) |
| Transit auto-unseal | ✅ built in | ✅ | ✅ |
| Cloud KMS auto-unseal (AWS/Azure/GCP/OCI/Ali) | Plugins (2.7+) | ✅ | ✅ |
| HSM (PKCS#11) auto-unseal | Plugin (2.7+) | ❌ | 💲 |
| Manual Raft snapshots | ✅ | ✅ | ✅ |
| Automated snapshots | ❌ (use cron/systemd timer) | ❌ | 💲 |
| Replication between clusters (DR / performance) | ❌ | ❌ | 💲 |
| Sentinel (policy as code), Transform / tokenization, KMIP engine, Secrets Sync | ❌ | ❌ | 💲 |
| Self-initialization from the config file | ✅ (2.4+) | ❌ | ❌ |
| UI | Basic (some tasks need the built-in console) | Richer | Richest |

**What we'd miss from Vault Enterprise:** cross-cluster replication, Sentinel, automated snapshots, vendor support from IBM. None is needed for our current scale. Replication matters only if we run several clusters across clouds.

### 4.3 Resources

| | OpenBao 2.7.1 | Vault 2.1.1 | Source |
|---|---|---|---|
| Container image (compressed / on disk) | **80 MB / 275 MB** | 188 MB / 740 MB | *tested* (`docker images`) |
| Idle memory | 34 MiB | 37 MiB | *tested* (`docker stats`, dev mode) |
| Production node (small) | 2–4 cores, 8–16 GB RAM, 100+ GB disk, 3,000+ IOPS | same | HashiCorp reference architecture (OpenBao publishes none) |
| Nodes | **5** recommended (3 minimum for HA) | 5 across 3 AZs | Official docs |
| Plus | 1 small **unsealer** VM, a load balancer | same | — |

Resource needs are effectively identical. The real cost is **operations time**: certificates, backups, upgrades, key holders.

### 4.4 "Filtering" = access control

How OpenBao decides who can do what:

1. **Authenticate** → token with policies (AppRole for each service, OIDC for people).
2. **Policies** on paths, deny by default:
   ```hcl
   path "secret/data/kouventa/*"       { capabilities = ["read"] }
   path "secret/data/kouventa/admin/*" { capabilities = ["deny"] }   # more specific → wins
   path "database/creds/kouventa-app"  { capabilities = ["read"] }
   ```
   Only the **most specific matching path** rule applies. If several of the token's policies have a rule for that same path, their capabilities are combined, and `deny` overrides the rest.
   *Tested:* the Kouventa API reads its own secrets, gets 403 for `otherproduct/*` and `kouventa/admin/*`.
3. **Namespaces**: a separate area per product, with its own admins.
4. Also available:
   - parameter constraints on writes (not supported for KV v2)
   - **control groups** (approval by a second person)
   - CEL expressions (JWT auth, PKI)
   - limits on AppRole secret IDs (TTL, number of uses, source IP)

### 4.5 Compatibility (*tested*, M4)

The same scripts and the same Go code ran against both. Only the environment variable names (`BAO_*` vs `VAULT_*`) and the CLI name changed; only namespaces failed on Vault. Switching later in either direction is low-risk.

---

## 5. How OpenBao works

### 5.1 The request path

```
Go API ──(1) login: role_id + secret_id──► auth/approle/login ──► token (policies, TTL)
       ──(2) GET /v1/secret/data/kouventa/app  + token ──► policy check ──► value or 403
       ──(3) GET /v1/database/creds/kouventa-app ──► OpenBao runs CREATE ROLE in Postgres ──► user + password + lease
       ──(4) renew token (periodic, forever) and DB lease … at the lease's max TTL: get a new DB user
every request ──► audit log
```

### 5.2 Paths and API addresses (how to read any OpenBao path)

Everything in OpenBao (secrets, logins, settings) is a **path**, and every path is also an **HTTP address**: `https://<server>:8200/v1/<path>`.

A path has three parts:

```
   secret  /  data  /  kouventa/app
   └─┬──┘    └─┬─┘    └────┬─────┘
   MOUNT    ENGINE      YOUR NAME
            ROUTE
```

| Part | Who decides | Examples |
|---|---|---|
| **Mount** | You, when enabling an engine (`bao secrets enable -path=secret kv-v2`) | `secret/`, `database/`, `transit/`, `auth/approle/` |
| **Engine route** | The engine's fixed API (see its docs or `bao path-help`) | KV v2: `data/`, `metadata/`. Database: `config/`, `roles/`, `creds/`, `static-creds/`. AppRole: `role/`, `login` |
| **Your name** | You | `kouventa/app`, `kouventa-app` |

Paths used in this project:

| Path | Mount | Route | Name | What it is |
|---|---|---|---|---|
| `secret/data/kouventa/app` | secret | data | kouventa/app | Secret values |
| `secret/metadata/kouventa/app` | secret | metadata | kouventa/app | Versions, key names (for `list`) |
| `database/creds/kouventa-app` | database | creds | kouventa-app | Get a new temporary DB user |
| `database/config/supportdesk` | database | config | supportdesk | Connection to Postgres |
| `auth/approle/role/kouventa-api/role-id` | auth/approle | role/…/role-id | kouventa-api | The AppRole's role ID |
| `auth/approle/login` | auth/approle | login | — | Where apps log in |
| `sys/policies/acl/kouventa-app` | sys (built in) | policies/acl | kouventa-app | A policy |
| `sys/health`, `sys/leases/renew` | sys | — | — | Status, lease renewal |

**OpenBao paths are not file paths.** `bootstrap/policies/kouventa-app.hcl` is a file in our repo. `sys/policies/acl/kouventa-app` is where OpenBao *stores* it after `bao policy write kouventa-app <file>`. Same for `/bootstrap/...` and `/secrets/...` in our scripts: those are folders mounted into a container, not OpenBao paths.

**CLI shortcut vs real path:**

| You type | Real API path | Note |
|---|---|---|
| `bao kv get secret/kouventa/app` | `GET /v1/secret/data/kouventa/app` | `kv` helper adds `data/` for you |
| `bao read database/creds/kouventa-app` | `GET /v1/database/creds/kouventa-app` | Generic command: path as is |
| `bao policy write …` | `PUT /v1/sys/policies/acl/…` | Helper |

**Policies always use the real path**, which is why ours say `secret/data/kouventa/*`, not `secret/kouventa/*`.

**How to find paths yourself:**
- `bao path-help secret/` lists a mount's routes.
- Add `-output-curl-string` to any CLI command to see the real HTTP call.
- Add `-output-policy` to see the policy that command would need.
- The API docs of each engine at openbao.org.

### 5.3 Inside a cluster

- One **active** node handles writes; **standbys** replicate via Raft and can serve reads.
- The load balancer sends traffic to the node whose `/v1/sys/health` returns **200**. Standbys return 429, sealed nodes 503, uninitialized 501.
- At startup every node is **sealed**. With transit auto-unseal it asks the unsealer to decrypt its root key.

### 5.4 Go/Fiber integration (what the team's code would look like)

Official client: `github.com/openbao/openbao/api/v2` (+ `api/auth/approle/v2`). It works for Vault too. Pattern from `api/` in this repo:

| Step | Code | When |
|---|---|---|
| Log in | `approle.NewAppRoleAuth` + `client.Auth().Login` | Startup (fail fast) |
| Read secrets | `client.KVv2("secret").Get(ctx, "kouventa/app")` | Startup |
| DB user | `client.Logical().Read("database/creds/kouventa-app")` → open `pgxpool` | Startup + on rotation |
| Stay alive | Two background jobs (`api/background.go`) renew the **periodic** token and the DB lease every 30 s; near the lease's max TTL: new DB user, new pool, swap it in. A DB user is revoked when its token expires, hence the periodic token | Background goroutines |
| Never log secrets | Logs and `/status` show usernames and node addresses, never passwords or keys | Always |

---

## 6. Lab results

| Milestone | What we proved | Status |
|---|---|---|
| M0 | Dev mode vs production; API health checks | ✅ |
| M1 | AppRole login, secrets from KV (no `.env`), policy "filtering", namespaces | ✅ |
| M2 | DB credentials from OpenBao: static role (rotates existing login) and dynamic users | ✅ |
| M3 | **RLS through PgBouncer**: per company / user / role; spoofed insert blocked; no context → 0 rows; owner trap + `FORCE` fix | ✅ |
| M4 | Same code on Vault 2.1.1; image/memory measurements; namespaces = Enterprise in Vault | ✅ |
| M5 | **3-node cluster + HAProxy + transit auto-unseal**; `.env` import; dynamic DB users with renewal/rotation; leader killed with no failed requests (40/40); full restart → auto-unseal, data intact; snapshot restore; audit log | ✅ Tested with the real OpenBao, Postgres, PgBouncer and HAProxy programs using the same config files. The `docker compose` packaging of `reference/` still needs one full run |

**OpenBao 2.7 surprises** (old tutorials are wrong here):
- `file` storage removed.
- Audit devices are configured in the HCL; the API refuses `audit enable`.
- `mlock` / `IPC_LOCK` gone. Harden swap instead.
- HSM / cloud-KMS seals and LDAP auth are now plugins.

---

## 7. Row Level Security — summary

Full guide: [`rls-guide.md`](rls-guide.md).

- Policies on a table filter rows for **every** query. Missing `WHERE` clauses can't leak.
- **How the API says who is asking:** per transaction, `set_config('app.company_id', …, true)`. Pooler-safe with PgBouncer transaction mode (*tested*), works with one DB login.
- **Possible patterns:** tenant isolation, ownership, roles, assignment, team membership, time/state visibility, per-command rules, read-only roles. Columns need GRANTs or views instead.
- **Combining:** tenant isolation as one **restrictive** policy (a wall); access rules as **permissive** policies (doors).
- **Must-do first:** the app must not log in as the table **owner**, a superuser or a `BYPASSRLS` role. Add `FORCE ROW LEVEL SECURITY`.
- **Managed option:** TencentDB has no real superuser; its admin role has `BYPASSRLS`, so the app needs its own role there too.

## 8. Firebase — summary

Full guide: [`firebase-guide.md`](firebase-guide.md).

- **Security Rules** (Firestore and Realtime DB) = RLS for the **frontend**. Rules files + tests are in `reference/firebase/`.
- Tenant and role come from **custom claims** set by our backend.
- **Rules are not filters:** client queries must include the same conditions.
- **Admin SDK (Go backend) bypasses all rules.** Backend endpoints must authorize in Go.

---

## 9. Rollout plan

| Phase | Work | Depends on |
|---|---|---|
| 1 | Staging OpenBao cluster on Tencent from `reference/` (+ TLS); team practises failover, restore | — |
| 2 | First service in staging: `.env` → KV, AppRole login at startup | 1 |
| 3 | Production cluster; migrate services' `.env` one by one | 2 |
| 4 | DB **static role**: OpenBao rotates the shared login's password | 3 |
| 5 | Split DB roles: `app_owner` (migrations) / `app_runtime` (app) | — (can start now) |
| 6 | **RLS** table by table (staging first) + `FORCE` | 5 |
| 7 | **Dynamic DB users** (`IN ROLE app_runtime`) + PgBouncer `auth_query` | 4, 5 |
| 8 | Firebase: claims + rules tests in CI; review Admin SDK endpoints | — (can start now) |

---

## 10. Risks and open questions

| Risk | Mitigation |
|---|---|
| OpenBao is down → services can't **start** (running ones keep working until leases expire) | HA cluster, longer TTLs in production, alerts |
| Lose the unsealer / its keys → cluster can't unseal | Back up the unsealer; Shamir keys with ≥3 people |
| Lose recovery keys + root token | Split recovery keys; documented break-glass procedure |
| Community-only support for OpenBao | Vendors exist; API compatibility keeps Vault as a fallback |
| Version churn (2.7 moved several features to plugins) | Read release notes; staging first; snapshot before upgrades |
| RLS: missing context → empty results | Rollout one table at a time in staging; tests in CI |
| Firebase Admin SDK bypasses rules | Authorization in Go; code review checklist |

**To verify:**
- Tencent Cloud HSM with the PKCS#11 plugin.
- TencentDB support for `CREATEROLE`-based dynamic users and PgBouncer `auth_query`.
- Network path from AWS/Azure VMs to a Tencent cluster.
- Run the Firebase rules tests (`npm test`) and the Docker version of `reference/` once.

---

## 11. Glossary of commands used

| Command | Does |
|---|---|
| `bao operator init` / `unseal` / `step-down` / `raft snapshot save` | Cluster lifecycle |
| `bao secrets enable -path=secret kv-v2` | Mount an engine |
| `bao kv put/get secret/kouventa/app` | KV secrets |
| `bao policy write <name> <file>` | Store a policy |
| `bao auth enable approle`, `bao write auth/approle/role/<r> …` | App login setup |
| `bao read database/creds/<role>` | Get a temporary DB user |
| `bao path-help <path>`, `-output-curl-string`, `-output-policy` | Discover paths and needed permissions |

---

## 12. Sources (official, checked 2026-10-06)

**OpenBao**
- [Docs home](https://openbao.org/docs/), [FAQ / fork origin](https://openbao.org/), [Licence](https://github.com/openbao/openbao/blob/main/LICENSE), [Governance (OpenSSF)](https://github.com/openbao/openbao/blob/main/GOVERNANCE.md)
- [2.7 release notes](https://openbao.org/community/release-notes/2-7-0/), [Changelog](https://github.com/openbao/openbao/blob/main/CHANGELOG.md), [Support policy](https://openbao.org/community/policies/support/)
- [Storage backends](https://openbao.org/docs/configuration/storage/), [PostgreSQL storage](https://openbao.org/docs/configuration/storage/postgresql/), [Integrated storage](https://openbao.org/docs/internals/integrated-storage/), [HA](https://openbao.org/docs/concepts/ha/)
- [Seals](https://openbao.org/docs/configuration/seal/), [Transit seal](https://openbao.org/docs/configuration/seal/transit/), [tcloudpublic seal](https://openbao.org/docs/configuration/seal/tcloudpublic/)
- [Audit config](https://openbao.org/docs/configuration/audit/), [Deprecations](https://openbao.org/community/deprecation/), [mlock RFC](https://openbao.org/community/rfcs/mlock-removal/), [Install & hardening](https://openbao.org/docs/install/)
- [Login MFA](https://openbao.org/docs/auth/login-mfa/), [Self-init](https://openbao.org/docs/configuration/self-init/), [Limits (namespaces)](https://openbao.org/docs/internals/limits/), [Database engine](https://openbao.org/docs/secrets/databases/), [Health API](https://openbao.org/docs/api/system/health/), [Vendors](https://openbao.org/ecosystem/vendors/)

**HashiCorp Vault**
- [Release notes](https://developer.hashicorp.com/vault/docs/updates/release-notes), [Licence (BSL)](https://github.com/hashicorp/vault/blob/main/LICENSE), [BSL announcement](https://www.hashicorp.com/en/blog/hashicorp-adopts-business-source-license)
- [Enterprise features](https://developer.hashicorp.com/vault/docs/enterprise), [Login MFA](https://developer.hashicorp.com/vault/docs/auth/login-mfa), [PKCS#11 seal](https://developer.hashicorp.com/vault/docs/configuration/seal/pkcs11), [Snapshots](https://developer.hashicorp.com/vault/docs/sysadmin/snapshots), [PostgreSQL storage](https://developer.hashicorp.com/vault/docs/configuration/storage/postgresql)
- [Production hardening](https://developer.hashicorp.com/vault/docs/concepts/production-hardening), [Raft reference architecture](https://developer.hashicorp.com/well-architected-framework/zero-trust-security/raft-reference-architecture), [Important changes](https://developer.hashicorp.com/vault/docs/updates/important-changes)
- [IBM completes HashiCorp acquisition](https://newsroom.ibm.com/2025-02-27-ibm-completes-acquisition-of-hashicorp,-creates-comprehensive,-end-to-end-hybrid-cloud-platform), [Vault 2.0 announcement](https://www.hashicorp.com/en/blog/vault-enterprise-20-modernizes-identity-security-at-scale)

**PostgreSQL / PgBouncer**
- [Row security policies](https://www.postgresql.org/docs/current/ddl-rowsecurity.html), [CREATE POLICY](https://www.postgresql.org/docs/current/sql-createpolicy.html), [CREATE VIEW](https://www.postgresql.org/docs/current/sql-createview.html), [Admin functions](https://www.postgresql.org/docs/current/functions-admin.html), [Role attributes](https://www.postgresql.org/docs/current/role-attributes.html), [PG 16 release notes](https://www.postgresql.org/docs/release/16.0/)
- [PgBouncer features](https://www.pgbouncer.org/features.html), [PgBouncer config](https://www.pgbouncer.org/config.html)

**Firebase / Tencent:** see the source lists in `firebase-guide.md` and `deployment-guide.md`.
