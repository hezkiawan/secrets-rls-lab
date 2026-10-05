# OpenBao vs HashiCorp Vault: lab findings (M4)

Measured on 2026-10-05, Docker Desktop on Windows, both in **dev mode**, same lab.
Versions: **OpenBao 2.7.1** (`openbao/openbao:2.7.1`) and **Vault 2.1.1 Community** (`hashicorp/vault:2.1.1`).

## Compatibility: what worked unchanged on both

| Test | OpenBao | Vault | Notes |
|---|---|---|---|
| M1 setup script (KV secrets, policy, AppRole) | ✅ | ✅ | Same script; only `CLI=bao` vs `CLI=vault` |
| M2 setup script (dynamic Postgres credentials) | ✅ | ✅ | Same script; each created its own `v-token-...` user |
| Go API (Fiber + OpenBao Go client), no code changes | ✅ | ✅ | Pointed at Vault via env vars only; `/status` reported `2.1.1` |
| Policy results (`/demo/read`: allow own path, deny other product, deny admin path) | ✅ | ✅ | Identical behaviour |

Why it works: OpenBao is a fork of Vault 1.14 and kept the same HTTP API, paths, CLI commands and token header (`X-Vault-Token`).

## Differences observed

| Area | OpenBao 2.7.1 | Vault 2.1.1 Community |
|---|---|---|
| **Namespaces** (isolated area per product/team) | ✅ Works: `bao namespace create kouventa` | ❌ `Code: 404 … enterprise-only feature` |
| **License** | MPL 2.0 (open source) | BSL 1.1 (source-available; production use allowed, but not offering Vault to third parties in competition with HashiCorp's paid versions) |
| **Web UI** | Basic: secrets, access, policies, tools; some tasks need the console (e.g. AppRole roles) | Richer: dashboard, API explorer, usage metrics, more menus; several items labelled **Enterprise** |
| **Image size**, content / on disk | **80.4 MB / 275 MB** | 188 MB / 740 MB (≈2.3× larger) |
| **Idle memory** (dev mode, `docker stats`) | 33.7 MiB | 37.1 MiB |
| **Idle CPU** | 1.71% | 0.75% |
| Container capability | Compose file adds `IPC_LOCK` | Not needed (Vault 2.0.2+ images dropped it) |

## How to read these numbers

- **Relative, not for sizing.** Dev mode, in memory, almost no data, no traffic. Production sizing comes from official guidance (see the research doc): e.g. HashiCorp's "small" cluster is 2–4 cores, 8–16 GB RAM per node, 3–5 nodes.
- Idle memory and CPU are effectively **the same**; the CPU difference is noise from a single snapshot.
- The image-size difference is real: a smaller download for OpenBao.

## Takeaway for the team

For what the team would use first (KV secrets, AppRole, policies, dynamic DB credentials), the two are **interchangeable**: same scripts, same Go code. The deciding factors are **not technical compatibility** but:
1. **License:** the team's priority is free open source → OpenBao.
2. **Namespaces:** one isolated area per product is free in OpenBao, paid in Vault.
3. **UI and vendor support:** Vault's UI is more polished, and paid IBM/HashiCorp support exists; OpenBao relies on community support.

Switching later is low-risk: our lab moved between them by changing one variable.
