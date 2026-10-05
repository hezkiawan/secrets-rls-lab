# Deploying OpenBao on VMs (Tencent Cloud) — guide

**Goal:** run OpenBao for all our products on VMs, with auto-unseal, HA, backups, dynamic DB credentials and no `.env` files.
**Working copy of everything below:** `reference/` (M5). Each container there = one VM here.
**Sources (checked 2026-10-06, OpenBao 2.7.1):** [Install](https://openbao.org/docs/install/), [Storage](https://openbao.org/docs/configuration/storage/), [Integrated storage](https://openbao.org/docs/internals/integrated-storage/), [Seals](https://openbao.org/docs/configuration/seal/), [TCP listener](https://openbao.org/docs/configuration/listener/tcp/), [Audit](https://openbao.org/docs/configuration/audit/), [Telemetry](https://openbao.org/docs/configuration/telemetry/), [HA upgrades](https://openbao.org/docs/upgrading/ha-upgrade/), [Health API](https://openbao.org/docs/api/system/health/), [2.7 release notes](https://openbao.org/community/release-notes/2-7-0/); sizing from [HashiCorp's Raft reference architecture](https://developer.hashicorp.com/well-architected-framework/zero-trust-security/raft-reference-architecture); Tencent: [CVM disks](https://intl.cloud.tencent.com/document/product/213/33000), [CLB health checks](https://intl.cloud.tencent.com/document/product/214/39251), [COS](https://intl.cloud.tencent.com/document/product/436/6222).

---

## 1. Target architecture

```
                           Tencent VPC (private subnets, 3 availability zones)
  ┌─────────────────────────────────────────────────────────────────────────────────┐
  │                                                                                 │
  │   App VMs (Go APIs) ──TLS:8200──► CLB / HAProxy ──► bao-1  bao-2  bao-3 (+4,5)  │
  │        │                         health: 200 only    AZ-1   AZ-2   AZ-3         │
  │        │                                               │ Raft :8201 (TLS)       │
  │        │                                               ▼                        │
  │        │                                         unsealer VM (transit)          │
  │        └──► PgBouncer ──► PostgreSQL  ◄── OpenBao creates/drops temp users      │
  │                                                                                 │
  │   Raft snapshots ──► COS bucket                                                 │
  └─────────────────────────────────────────────────────────────────────────────────┘
```

| Component | Count | Why |
|---|---|---|
| OpenBao nodes | **3** to start, **5** recommended by OpenBao for production | Raft needs a majority: 3 survive 1 failure, 5 survive 2 |
| Unsealer | 1 small VM (or 3 for HA) | Holds the transit key that unseals the main cluster |
| Load balancer | Tencent **CLB** or HAProxy | One address for apps, always the active node |
| Spread | Different availability zones | Survive losing a zone (with 5 nodes) |

---

## 2. Decisions to make

### 2.1 Storage: Raft or PostgreSQL?

OpenBao 2.7 offers four backends: **raft**, **postgresql**, pebbledb (single node, no HA) and in-memory (testing only). `file` was removed in 2.7.

| | Raft (integrated) — *used in our lab* | PostgreSQL backend |
|---|---|---|
| HA | ✅ leader + standbys, standby reads | ✅ since 2.7.0, standby reads |
| Extra software | None | A PostgreSQL server or cluster |
| OpenBao's advice | Needs Raft know-how; sensitive to network latency | *"If you're new to running OpenBao, we recommend PostgreSQL as an easier storage backend to safely operate than Raft."* |
| Backup | `bao operator raft snapshot save` | Normal Postgres backups |
| Catch for us | — | Do **not** reuse the product database: OpenBao would depend on the database it hands credentials for. Use a separate Postgres instance |

**Recommendation:** Raft. It's what we tested end to end, has no external dependency, and the team doesn't yet run an HA Postgres. Revisit the PostgreSQL backend if we later run a managed HA Postgres (e.g. TencentDB) that is separate from the products.

### 2.2 Unsealing on Tencent

After every restart OpenBao is **sealed** and needs its root key. The options in 2.7:

| Option | Built in? | On Tencent | Verdict |
|---|---|---|---|
| **Shamir (manual)** | ✅ default | Works. *k* of *n* people type key shares after every restart | OK for the unsealer only; painful for the main cluster |
| **Transit** (another OpenBao) | ✅ | Works | ✅ **Recommended** (tested in M5) |
| Static key in config | ✅ | Works, but the key sits on disk/env next to the data | Lab only |
| KMIP server | ✅ | Needs a KMIP-capable key server | Only if we buy one |
| PKCS#11 / HSM | Plugin (since 2.7) | Tencent Cloud HSM: **not verified** | Possible later |
| AWS / Azure / GCP / OCI / AliCloud KMS | Plugins (since 2.7) | Cross-cloud dependency | Only if we accept depending on another cloud |
| Tencent KMS | ❌ none | — | Not available. **Note:** the `tcloudpublic` seal is *T Cloud Public* (T-Systems), **not Tencent** |

**Transit in one paragraph:** a small separate OpenBao (the *unsealer*) holds an encryption key `autounseal`. Each main node has a token that may only `encrypt`/`decrypt` with that key. At startup a node asks the unsealer to decrypt its root key → unsealed with no human. In production the unsealer itself is unsealed **manually with Shamir** by key holders. (Our lab unsealer uses a `static` seal so it restarts unattended. That's lab only.) It restarts rarely, and it only needs to be reachable when a main node **starts**, not while the cluster runs.

One caveat: the nodes' transit token is **periodic** (24 h in the lab). Each node renews it while it can reach the unsealer. If the unsealer is down longer than the period, the token expires and the next node restart can't unseal. Monitor the unsealer like the cluster.

### 2.3 Sizing

OpenBao publishes no hardware table. The official reference we can cite is HashiCorp's Raft reference architecture:

| Size | CPU | RAM | Disk | IOPS | Throughput |
|---|---|---|---|---|---|
| Small | 2–4 cores | 8–16 GB | 100+ GB | 3,000+ | 75+ MB/s |
| Large | 4–8 cores | 32–64 GB | 200+ GB | 10,000+ | 250+ MB/s |

- It recommends **5 nodes across 3 availability zones** and says to **avoid burstable** CPU/storage instance types.
- **For us:** the "small" size is plenty for several products. Idle OpenBao used ~34 MiB in our lab, so start at the low end (e.g. 2 vCPU / 8 GB) and watch metrics.
- **Tencent disks:** IOPS grow with size. SSD gives `min(1800 + 30 × GiB, 26000)`, so 100 GiB SSD ≈ 4,800 IOPS ✅. Enhanced SSD goes higher. Premium Cloud Disk at 100 GiB ≈ 2,600 IOPS, below the 3,000 guideline.
- **Unsealer:** the smallest standard instance is fine: it only does a few encrypt/decrypt calls at node startup.

### 2.4 One cluster or many? (multi-cloud)

Products may run on Tencent, AWS and Azure.

| Option | Pros | Cons |
|---|---|---|
| **One central cluster on Tencent** + namespace or path per product *(recommended to start)* | One thing to run, one audit log | Apps on other clouds reach it over VPN/private link: latency, and a dependency on that link |
| One cluster per cloud | Local, independent | 2–3× the operations work; secrets duplicated |

Apps only talk to OpenBao **at startup and on renewals** (every minutes–hours), so cross-cloud latency matters little. The link must be reliable and **always TLS**.

---

## 3. Install a node (repeat on each VM)

**1. OS hardening**
- Disable swap, or encrypt it. OpenBao removed `mlock` in 2.0; the official systemd unit sets `MemorySwapMax=0`.
- Time sync (NTP).
- Disable core dumps.
- Dedicated VMs: nothing else runs on them.

**2. Install** from the official packages or the release zip. Verify checksums first:
```bash
sha256sum --ignore-missing --check checksums.txt
gpg2 --verify checksums.txt.gpgsig checksums.txt    # or cosign, see install docs
```

**3. TLS certificates** for every node: server cert with the node's DNS name + the LB name, signed by an internal CA. OpenBao *"assumes TLS by default"*; our lab's `tls_disable` is lab-only.

**4. Config** `/etc/openbao/openbao.hcl` (production version of `reference/openbao/bao-1.hcl`):
```hcl
ui            = true
cluster_name  = "company-secrets"
api_addr      = "https://bao-1.internal:8200"
cluster_addr  = "https://bao-1.internal:8201"

storage "raft" {
  path    = "/var/lib/openbao"
  node_id = "bao-1"
  retry_join {
    leader_api_addr     = "https://bao-2.internal:8200"
    leader_ca_cert_file = "/etc/openbao/tls/ca.pem"
  }
  retry_join {
    leader_api_addr     = "https://bao-3.internal:8200"
    leader_ca_cert_file = "/etc/openbao/tls/ca.pem"
  }
}

listener "tcp" {
  address         = "0.0.0.0:8200"
  cluster_address = "0.0.0.0:8201"
  tls_cert_file   = "/etc/openbao/tls/bao-1.pem"
  tls_key_file    = "/etc/openbao/tls/bao-1-key.pem"
}

seal "transit" {
  address    = "https://unsealer.internal:8200"
  key_name   = "autounseal"
  mount_path = "transit/"
  tls_ca_cert = "/etc/openbao/tls/ca.pem"
  # token: from the BAO_TOKEN environment variable (systemd EnvironmentFile, mode 0600)
}

audit "file" "main" {
  options { file_path = "/var/log/openbao/audit.log" }
}

telemetry {
  prometheus_retention_time = "30s"
  disable_hostname          = true
}
```

**5. Firewall** (Tencent security groups):

| Port | From | Purpose |
|---|---|---|
| 8200 | LB + app subnets + admin bastion | API / UI |
| 8201 | **Other OpenBao nodes only** | Raft + request forwarding |
| 8200 on unsealer | OpenBao nodes only | Transit unseal |

**6. Start:** `systemctl enable --now openbao`.

---

## 4. First-time setup (once)

| Step | Command (see `reference/scripts/`) | Notes |
|---|---|---|
| Init | `bao operator init -recovery-shares=5 -recovery-threshold=3 -recovery-pgp-keys=…` | With auto-unseal you get **recovery keys**. Give each to a different person, PGP-encrypted, never stored together |
| Configure | `configure.sh` (setup as code, idempotent) | KV, policies, AppRole, database engine |
| Admin access | Enable `userpass` or **OIDC** (team SSO) for humans, with an admin policy | Humans shouldn't use the root token |
| Revoke root | `bao token revoke <root>` | Generate a new one with recovery keys only when needed |

The UI is fine for **exploring**. Everything that defines the system belongs in scripts in git, reviewed like code (our lab proved it: dev mode lost every click on restart).

---

## 5. Connecting the products

### 5.1 Organizing secrets per product

```
secret/                       ← KV v2 mount
 ├─ kouventa/app              ← JWT key, Meta tokens
 ├─ kouventa/env              ← imported .env
 ├─ kouventa/firebase         ← Firebase service-account JSON
 ├─ productB/…
database/                     ← database secrets engine
 ├─ config/kouventa-db       ← (lab name: supportdesk)
 └─ creds/kouventa-app        ← dynamic users
auth/approle/role/kouventa-api
```
- One **policy** per service, allowing only its own paths (see the research report §5 for the path rules).
- **Namespaces** (free in OpenBao) are an option for stronger separation between products: each gets its own mounts, policies and admins. Start with paths; move to namespaces if product teams need to self-administer.

### 5.2 App login (AppRole)

- `role_id` ships with the app config (not secret).
- `secret_id` = **secret zero**, delivered by the deploy pipeline, not humans. Limit it with:
  - `secret_id_ttl`
  - `secret_id_num_uses`
  - `secret_id_bound_cidrs` (only the app VMs' IPs)
  - response wrapping
- The app renews its token and logs in again at max TTL (`KeepLoggedIn` in our Go code).

### 5.3 Moving a `.env` file

1. `import-env.sh <product> <file>` → `secret/<product>/env`.
2. Change the app to read from OpenBao at startup (our `LoadAppSecrets` pattern).
3. Deploy, verify, then **delete the `.env` from the server**. Rotate any value that was ever committed to git.

### 5.4 Database credentials (in this order)

| Phase | What | App change |
|---|---|---|
| 1. Static role | OpenBao **rotates the password of the existing shared login** on a schedule; app reads it from `database/static-creds/…` | Small: read the password from OpenBao |
| 2. Split roles | `app_owner` (migrations) vs `app_runtime` (app). Prerequisite for RLS | Grants + connection change |
| 3. Dynamic roles | Each app instance gets its own temporary user `IN ROLE app_runtime`, rotated automatically | Our `DynamicDBCreds` + pool swap |

Postgres side for dynamic users (Postgres 16+ rules):
```sql
CREATE ROLE openbao_admin LOGIN CREATEROLE PASSWORD '…';      -- then `rotate-root` in OpenBao
GRANT app_runtime TO openbao_admin WITH ADMIN OPTION;          -- needed to put new users IN ROLE app_runtime
```
PgBouncer must use **`auth_query`**: a fixed `userlist.txt` can't know users created on the fly. See `reference/pgbouncer/` and `reference/postgres/initdb/04-pgbouncer-auth.sql`.

---

## 6. Operating it

### High availability
- **Load balancer health check:** `GET /v1/sys/health`, healthy only on **200** (active node). 429 = standby, 503 = sealed, 501 = not initialized.
  - Tencent CLB: choose status class **http_2xx** only.
  - HAProxy: `http-check expect status 200` (our config).
- Standbys forward requests to the active node anyway; the LB rule just avoids the extra hop.
- Expect **10–30 s without a leader** after a full restart (we measured ~15 s). Apps must retry at startup.

### Backups
- `bao operator raft snapshot save` on a schedule (systemd timer/cron) → upload to **COS**, encrypted, with versioning.
- **Test a restore** regularly on a separate VM. A backup you never restored is a hope.
- Also back up the **unsealer** (its data, plus the Shamir keys held by people). Without it the main cluster can't unseal.

### Monitoring and audit
- Prometheus metrics at `/v1/sys/metrics?format=prometheus` (without `format` it returns JSON): needs a token with `read`/`list`, active node only by default. In the Prometheus scrape config set `params: { format: [prometheus] }`.
- Alert on:
  - a node sealed or down
  - leader changes
  - Raft peer count
  - certificate expiry
  - lease counts
  - audit log write failures
- Audit log: ship `/var/log/openbao/audit.log` to central logging. Values are HMAC-hashed.

### Upgrades
1. **Snapshot first.** Upgrades can change data formats; a downgrade may be impossible.
2. Read the release notes for every version in between. 2.7 alone removed `file` storage and moved HSM/KMS seals and LDAP auth to plugins.
3. One **standby** at a time: stop it, replace the binary, start it, check it's unsealed and joined.
4. Last, the **active** node: `bao operator step-down` (a standby takes over), then upgrade it.

Releases are roughly monthly.

---

## 7. Security checklist

- [ ] TLS everywhere (API, cluster, unsealer, Postgres)
- [ ] Swap disabled or encrypted; core dumps off; dedicated VMs; not running as root
- [ ] Security groups: 8201 only between nodes
- [ ] Root token revoked; recovery keys split across people
- [ ] Humans log in with OIDC/userpass + least-privilege policies
- [ ] One AppRole + policy per service; `secret_id` limited (TTL, uses, CIDR)
- [ ] Audit device on and shipped off the VM
- [ ] Snapshots to COS + tested restore
- [ ] Monitoring and alerts
- [ ] All configuration in git (HCL, policies, scripts)

---

## 8. Rollout plan

| Week | Step | Result |
|---|---|---|
| 1 | Staging cluster (3 nodes + unsealer + LB) from our `reference/` files, with TLS | Team practises init, failover, restore |
| 2 | **One service's `.env`** into KV + AppRole, in staging | First service without `.env` |
| 3 | Production cluster; move services' `.env` one by one | No `.env` in production |
| 4 | DB **static role** for the shared login | Password rotated by OpenBao |
| Later | Split owner/runtime roles → **RLS** (see [`rls-guide.md`](rls-guide.md)) → **dynamic DB users** | Full M3 + M5 setup |

Same order as the research report §9.

---

## 9. Open questions to check before production

1. **Tencent Cloud HSM** with the PKCS#11 plugin: possible? Would remove manual unsealing of the unsealer.
2. **TencentDB for PostgreSQL** (if we move): can `pg_tencentdb_superuser` or another `CREATEROLE` account do OpenBao dynamic users and the PgBouncer `auth_query` function? Test.
3. Network path from AWS/Azure VMs to the Tencent cluster (VPN / private connection).
4. Who holds the recovery keys and unsealer Shamir keys (at least 3 people).
5. Paid support: OpenBao lists vendors (e.g. Adfinis, ControlPlane, Kubermatic) offering support or managed OpenBao.
