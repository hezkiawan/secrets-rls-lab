# M5 notes — a production-like OpenBao cluster

**Goal:** show that what the team asked for works outside dev mode:
1. OpenBao deployed like on VMs
2. `.env` files moved into KV
3. dynamic database credentials
4. renewal and high availability

## Concepts in one line each

| Concept | Meaning |
|---|---|
| **Sealed** | OpenBao's data on disk is encrypted. After every start it is *sealed*: it can't read its own data until it gets the root key. |
| **Unseal** | Giving it that key. Manual (Shamir: 3 of 5 people type key shares) or **auto-unseal** (ask a key service). |
| **Transit auto-unseal** | A second, small OpenBao (the *unsealer*) holds an encryption key. Nodes ask it to decrypt their root key at startup. Our choice for Tencent because there is no Tencent KMS seal. |
| **Recovery keys** | With auto-unseal, `init` returns recovery keys instead of unseal keys. Needed only for rare admin operations. |
| **Raft (integrated storage)** | Each node keeps a full copy of the data. One **leader** (active) writes; the others are **standbys**. A majority must agree: 3 nodes survive 1 failure, 5 survive 2. |
| **`retry_join`** | New nodes find the leader by themselves. No manual "join" step. |
| **Health codes** | `/v1/sys/health`: 200 active, 429 standby, 503 sealed, 501 not initialized. HAProxy sends traffic only to 200. |
| **Lease** | Every dynamic secret (and token) has a TTL. Renew it before it ends; at **max TTL** you must get a new one. |
| **Dynamic DB user** | OpenBao runs `CREATE ROLE "v-…" … VALID UNTIL … IN ROLE app_runtime` and drops it when the lease ends. Being a member of `app_runtime` means RLS applies. |
| **`rotate-root`** | OpenBao changes its own admin DB password. Afterwards no human knows it. |
| **Raft snapshot** | One file containing the whole cluster's data: the backup. |
| **Audit device** | A log of every request; secret values are hashed. |

## How the API keeps working forever (`api/internal/openbao/lifecycle.go`)

```
login (AppRole) ─► token ─► renew … renew … max TTL ─► login again
get DB creds ─► open pool ─► renew lease … max TTL ─► get NEW creds ─► new pool ─► swap ─► old pool closes
```
- `KeepLoggedIn` and `WatchLease` use the official client's `LifetimeWatcher`.
- `db.Pools` swaps the pool atomically, so requests never see a closed pool.
- The `kouventa-app` policy needs `update` on `sys/leases/renew` so the API can renew its DB lease.

## What we measured (local test, short TTLs)
- 40/40 requests OK while the DB user rotated twice, the token re-logged-in twice, and the **active node was killed** (another took over).
- Expired `v-…` users were removed from Postgres.
- Killed node restarted, rejoined and unsealed itself.
- Snapshot save + restore worked.
- Full restart of everything: data intact, auto-unsealed; **leader election took ~15 s** (HAProxy returns 503 until then).

## Surprises (OpenBao 2.7)
- `file` storage removed → Raft for everything.
- `bao audit enable` refused → audit devices go in the HCL config.
- mlock removed → no `IPC_LOCK`; harden swap on the OS instead.
- HSM / cloud-KMS seals are now external plugins; transit is built in.
- After a restart, logs show `no TLS config found for ALPN req_fw_sb-act_v1` for a few seconds. That is request-forwarding noise before the leader is ready; it stops by itself.

## Dev mode (M0–M2) vs this
| | Dev mode | Reference cluster |
|---|---|---|
| Storage | In memory: restart = everything gone | Raft on disk, 3 copies |
| Unseal | Automatic, fake | Transit auto-unseal |
| Root token | `root` | Random, used once by `configure.sh`, then should be revoked |
| Setup | Re-run bootstrap after every restart | `init.sh` once, `configure.sh` idempotent |
| HA | None | Leader + standbys behind HAProxy |
