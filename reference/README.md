# Reference stack — production-like OpenBao + PostgreSQL

This folder is what the team can copy from. Each container here = **one VM in production**, and the config files are the same files you'd put on those VMs.

```
                      ┌──────────── OpenBao cluster (Raft) ────────────┐
 Go API ──:8300──► HAProxy ──► bao-1 (active)   bao-2 (standby)   bao-3 (standby)
   │                              │  auto-unseal via transit  │
   │                              └──────────► unsealer ◄─────┘
   │
   └──:6432──► PgBouncer ──► PostgreSQL (RLS)
                 ▲ temporary users created by OpenBao (dynamic credentials)
```

| Service | Port on your machine | Role |
|---|---|---|
| `haproxy` | **8300** (OpenBao API + UI), 8404 (stats page) | The one address apps use; always routes to the active node |
| `bao-1/2/3` | 8201 / 8202 / 8203 | 3-node OpenBao cluster, Raft storage (survives 1 node failure) |
| `unsealer` | — | Small separate OpenBao that holds the key that unlocks the cluster (transit auto-unseal) |
| `postgres` | 5433 | Database with RLS |
| `pgbouncer` | 6432 | Connection pooler (transaction mode); looks up users with `auth_query` |
| `pgadmin` | 5051 | Database GUI |
| `tools` | — | Admin toolbox to run `scripts/` (init, configure, import-env, snapshot) |

## What this stack proves

| Team requirement | Where | Demo |
|---|---|---|
| 1. Deploy OpenBao on VMs | `openbao/bao-*.hcl`, `unsealer/`, `haproxy/` | Steps 1–2 |
| 2. Move `.env` files into KV | `scripts/import-env.sh` | Step 3 |
| 3. Dynamic DB credentials | `scripts/configure.sh` (database engine) | Step 4: API logs in as `v-approle-kouventa-…` |
| 4. Renewal + high availability | `api/background.go`, `api/database/database.go` (SetPool), HAProxy | Steps 5–6 |
| Backup / restart safety | `scripts/snapshot.sh`, transit seal | Steps 7–8 |

## Run it (PowerShell, from the repo root)

### 0. Clean start (once)
```powershell
docker compose -f reference/docker-compose.yml down -v
docker compose -f reference/docker-compose.yml up -d --build
```
`down -v` deletes the old database volume, so the new `postgres/initdb/06-openbao-admin.sql` runs. **It deletes all reference data.**

The `unsealer-setup` container runs once automatically: it initializes the unsealer, creates the `autounseal` transit key and the token the nodes use.
Wait about 30 s, then check: `docker compose -f reference/docker-compose.yml ps`. The three `bao-*` nodes should be running (sealed and uninitialized for now).

### 1. Initialize the cluster (once, ever)
```powershell
docker compose -f reference/docker-compose.yml run --rm tools /scripts/init.sh
```
- Saves the **recovery keys + initial root token** to `reference/.secrets/init.json` (git-ignored, LAB ONLY).
- No unseal step: the nodes unseal themselves via the unsealer. bao-2 and bao-3 join bao-1 by `retry_join`.

Open **http://localhost:8404** (HAProxy stats): exactly **one** node is green (active). The others are standbys.

### 2. Configure everything as code
```powershell
docker compose -f reference/docker-compose.yml run --rm tools /scripts/configure.sh
```
Safe to re-run. It sets up:
- KV v2 at `secret/` with the app secrets
- the `kouventa-app` policy
- AppRole `kouventa-api` → writes `api/.reference/role_id` and `secret_id`
- the database engine: connects as `openbao_admin`, then **`rotate-root`** (after this, only OpenBao knows that password)
- role `kouventa-app`: temporary users that are members of `app_runtime` (so RLS applies to them)

UI: http://localhost:8300. Log in with the `root_token` from `reference/.secrets/init.json`.

### 3. Move a `.env` file into OpenBao
```powershell
docker compose -f reference/docker-compose.yml run --rm tools /scripts/import-env.sh kouventa /env/example.env
```
Each `KEY=VALUE` line in [`scripts/example.env`](scripts/example.env) becomes one key in `secret/kouventa/env`. Check in the UI: Secrets → secret → kouventa → env.

### 4. Run the API with dynamic database credentials
```powershell
cd api
go run .
```
No env vars needed: the defaults in `api/config/config.go` point at this stack (OpenBao `127.0.0.1:8300`, PgBouncer `127.0.0.1:6432`, AppRole files in `api/.reference/`).

Expected log:
```
STEP 2: logged in to OpenBao at http://127.0.0.1:8300
STEP 3: read secrets from secret/kouventa/app
STEP 4: got temporary database user v-approle-kouventa-…
STEP 5: connected to Postgres at 127.0.0.1:6432 as v-approle-kouventa-…
STEP 6: background jobs started
STEP 7: listening on http://localhost:3000
```
- http://localhost:3000/ — the RLS demo still works, now through a user that didn't exist a minute ago.
- http://localhost:3000/status — `database_user`, `database_user_in_use`, `openbao_active_node`.

### 5. Watch renewal and rotation (just wait)
The lab uses short TTLs so you can watch it happen: a **periodic** token (`2m` period, renewed forever), DB credentials `2m` / max `6m`.
- Every 30 s the API **renews** both (`[token] renewed…`, `[db] user … renewed…`).
- At 6 minutes the DB lease can't be renewed any more → the API gets a **new user**, opens a new pool and swaps (`[db] user is near its maximum lifetime, getting a new one` → `[db] now using new user …`). Requests keep working.
- The token is **periodic**: renewed forever, no re-login while the API runs. (A token with a max TTL would be a problem: dynamic DB users are revoked when the token that requested them expires.)
- OpenBao **drops** expired `v-…` users from Postgres. Check in pgAdmin (:5051): Login/Group Roles.

Production TTLs are longer (e.g. 1h / 24h). The mechanism is the same.

### 6. High availability: kill the active node
1. Find the active node on http://localhost:8404 (or `/status` → `active_node`).
2. Stop it, e.g.:
   ```powershell
   docker compose -f reference/docker-compose.yml stop bao-1
   ```
3. Within a few seconds another node becomes active (green on :8404). Keep clicking in the demo page: it keeps working.
4. Bring it back: `docker compose -f reference/docker-compose.yml start bao-1`. It rejoins as a standby and **unseals itself**.

3 nodes survive 1 failure. 5 nodes (the OpenBao recommendation for production) survive 2.

### 7. Backup and restore (Raft snapshot)
```powershell
docker compose -f reference/docker-compose.yml run --rm tools /scripts/snapshot.sh save
# change or delete a secret in the UI, then:
docker compose -f reference/docker-compose.yml run --rm tools /scripts/snapshot.sh restore
```
The file is `reference/backups/openbao.snap` (git-ignored).

### 8. Full restart = automatic unseal
```powershell
docker compose -f reference/docker-compose.yml restart unsealer bao-1 bao-2 bao-3
```
- The unsealer unlocks itself (static seal, LAB ONLY), then the nodes unseal themselves via transit.
- **Electing a leader takes ~10–30 s.** Until then HAProxy has no healthy node and returns 503. That's expected.
- The data is still there. In dev mode (learning lab) everything would be gone.

> ⚠️ **Stack stopped for more than 24 hours?** The nodes' unseal token (`lab-transit-unseal-token`) is periodic with a 24h period. Nothing renews it while the stack is down, so it expires and the nodes can't unseal (`403 permission denied` in `docker compose -f reference/docker-compose.yml logs bao-1`). Fix:
> ```powershell
> docker compose -f reference/docker-compose.yml up -d                       # re-runs unsealer-setup → recreates the token
> docker compose -f reference/docker-compose.yml restart bao-1 bao-2 bao-3
> ```
> Do this before recording a demo.

### 9. Audit log
Every request is logged as JSON (with secret values hashed):
```powershell
docker compose -f reference/docker-compose.yml logs bao-1 --tail 20
```

## Files

| File | What it is |
|---|---|
| `openbao/bao-N.hcl` | Node config: Raft storage + `retry_join`, listener, transit seal, **audit device** |
| `openbao/policies/kouventa-app.hcl` | What the API may do (read its KV paths, get DB creds, renew leases) |
| `unsealer/unsealer.hcl`, `setup.sh` | The unseal helper: transit key `autounseal` + a token limited to encrypt/decrypt |
| `haproxy/haproxy.cfg` | Routes to the node whose `/v1/sys/health` returns **200** (active). 429 = standby, 503 = sealed, 501 = not initialized |
| `scripts/init.sh` | One-time init (recovery keys + root token) |
| `scripts/configure.sh` | All OpenBao setup as code (idempotent) |
| `scripts/import-env.sh` | `.env` → `secret/<product>/env` |
| `scripts/snapshot.sh` | Raft snapshot save / restore |
| `postgres/initdb/*.sql` | Roles, schema, RLS, PgBouncer auth function, seed, `openbao_admin` |
| `pgbouncer/pgbouncer.ini` | Transaction pooling + `auth_query` (needed because the users are created on the fly) |

## LAB ONLY: what production does differently

| Here | In production |
|---|---|
| `tls_disable = true` | TLS on every listener (8200 API, 8201 cluster) |
| Unsealer uses a **static seal** with its key in the compose file | Unsealer itself is unsealed manually (Shamir, key holders) or via HSM/KMS. Its key never sits in a file next to it |
| Fixed transit token ID in compose | Generated token, delivered to the nodes securely, periodic, renewed |
| Recovery keys + root token in `.secrets/init.json` | Recovery keys split between people (PGP-encrypted); root token **revoked** after setup |
| AppRole `secret_id` in a file, created by the admin script | Delivered by the deploy pipeline, short-lived / limited uses |
| All on one machine | 3–5 VMs across availability zones; firewall allows only 8200 (clients) and 8201 (cluster) |

The full production guide is chapter 13 of the [research report](../docs/Secrets-Management-and-RLS-Research-Report.pdf).

## OpenBao 2.7 notes (things that differ from older tutorials)
- No `file` storage backend → Raft everywhere (even the single-node unsealer).
- Audit devices are declared in the HCL. `bao audit enable` is refused.
- mlock removed → no `IPC_LOCK` / `disable_mlock`. Disable or encrypt swap on the VM instead.
- HSM (PKCS#11) and cloud-KMS seals are external plugins now. Transit is built in.
