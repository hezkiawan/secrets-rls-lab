# secrets-rls-lab

Research and a working lab for the production team, answering two questions:

1. **Where should our secrets live?** OpenBao vs HashiCorp Vault, and how to run it on our VMs.
2. **How can the database itself keep each customer's data apart?** PostgreSQL Row Level Security (RLS), plus Firebase Security Rules.

Everything runs on one laptop with Docker, using our own stack: Go + Fiber v3, PostgreSQL and PgBouncer.

## The answer in short

- **Use OpenBao** (open source, MPL 2.0). It's the same API, CLI and Go client as Vault, and the features Vault charges for (namespaces, standby reads, control groups) are free.
- **Run it as a 3-node cluster** behind a load balancer, with **transit auto-unseal** (a small separate OpenBao holds the unlock key). Tencent's KMS has no OpenBao seal, so transit is the built-in option that works on our VMs.
- **Apps log in with AppRole** and read their secrets at startup, so `.env` files go away. OpenBao can also create **temporary database users** that expire, instead of one shared, never-rotated password.
- **Turn on PostgreSQL RLS**, so every query is filtered by company, user and role, even if a developer forgets a `WHERE`. It works through PgBouncer. First split the table **owner** login from the **app** login, because the owner bypasses RLS.
- **Firebase:** Security Rules do the same job for the client SDKs. The Admin SDK used by our Go backend **bypasses them**, so the backend must check access itself.

## Start here

| If you want to… | Open |
|---|---|
| Understand the findings and the recommendation | **The research report:** [PDF](docs/Secrets-Management-and-RLS-Research-Report.pdf) · [Word (editable)](docs/Secrets-Management-and-RLS-Research-Report.docx). Read the summary (2 pages) and the "In one paragraph" box of each chapter for a quick overview. |
| Run the cluster and the demos yourself | [`reference/README.md`](reference/README.md): step-by-step guide |
| Read how the Go API uses OpenBao and RLS | [`api/README.md`](api/README.md), or the whole idea in one file: [`api/cmd/simple/main.go`](api/cmd/simple/main.go) |

## What the lab runs

```
                       ┌──────────── OpenBao cluster (Raft) ────────────┐
 Go API ──:8300──► HAProxy ──► bao-1 (leader)   bao-2 (standby)   bao-3 (standby)
   │                               │      auto-unseal via transit      │
   │                               └────────────► unsealer ◄───────────┘
   │
   └──:6432──► PgBouncer ──► PostgreSQL (RLS filters every row)
                                  ▲ temporary DB users, created by OpenBao
```

Each container stands for **one VM in production**, and its config files are the ones you'd put on that VM.

## Quick start (Windows PowerShell, from the repo root)

Requirements: Docker Desktop (Compose v2) and Go 1.25+.

```powershell
# 1. Start the stack (the first time takes a few minutes to build)
docker compose -f reference/docker-compose.yml up -d --build

# 2. Once per new cluster: initialize it, then configure it (secrets, policy, AppRole, DB engine)
docker compose -f reference/docker-compose.yml run --rm tools /scripts/init.sh
docker compose -f reference/docker-compose.yml run --rm tools /scripts/configure.sh

# 3. Run the API: no .env file, no database password, it gets everything from OpenBao
cd api
go run .
```

Then open:

| URL | What you see |
|---|---|
| http://localhost:3000 | **RLS demo page:** switch between users and try to read or write another company's data |
| http://localhost:3000/status | The temporary database user in use right now, and the active OpenBao node |
| http://localhost:8404 | HAProxy stats: the green row is the current leader |
| http://localhost:8300 | OpenBao UI (lab login: `root_token` in `reference/.secrets/init.json`) |
| http://localhost:5051 | pgAdmin |

Things to try (all explained in [`reference/README.md`](reference/README.md)):

- **Failover:** `docker compose -f reference/docker-compose.yml stop bao-1` (or whichever node is the leader). Another node takes over within seconds and the API keeps working.
- **Full restart:** restart every container. The nodes unseal themselves; nobody types a key.
- **Credential rotation:** leave the API running. Every few minutes it swaps to a new temporary database user without a failed request.
- **The app's permissions:** `docker compose -f reference/docker-compose.yml run --rm tools /scripts/app-check.sh` logs in *as the app* and shows what its policy allows and refuses.

> ⚠️ **Stopped the stack for more than 24 hours?** The nodes' unseal token expires while nothing renews it, and the nodes stay sealed (`403` in their logs). Fix: run `docker compose -f reference/docker-compose.yml up -d` again, then `docker compose -f reference/docker-compose.yml restart bao-1 bao-2 bao-3`.

## Repository map

| Path | What it is |
|---|---|
| `docs/` | The research report (PDF and editable Word) |
| `reference/` | **The production-like stack:** OpenBao cluster, unsealer, HAProxy, PostgreSQL with RLS, PgBouncer, setup scripts |
| `reference/openbao/` | Node configs (`bao-1/2/3.hcl`) and the app's policy |
| `reference/unsealer/` | The unsealer's config and setup script |
| `reference/postgres/initdb/` | Roles, tables, **RLS policies** (`03-rls.sql`), PgBouncer auth function, demo data |
| `reference/scripts/` | `init.sh`, `configure.sh`, `import-env.sh` (.env → OpenBao), `snapshot.sh` (backup), `app-check.sh`, `demo.ps1` (PowerShell shortcuts for demos) |
| `reference/firebase/` | Firestore and Realtime Database rules for the same support desk, with emulator tests |
| `api/` | Go + Fiber API: AppRole login, secrets from OpenBao, temporary DB users, RLS-protected endpoints, demo page |
| `learning-lab/` | The first dev-mode lab: OpenBao and Vault side by side. Not needed to run anything; kept as the compatibility evidence |

## Lab only: not a production template

This lab turns off TLS, keeps the root token and recovery keys in a file, uses fixed passwords, and uses very short lifetimes (minutes) so you can watch renewal happen. **Production differs:** TLS everywhere, 5 nodes across zones, keys split between people, root token revoked after setup. See the report, chapter 13, and the "LAB ONLY" table in [`reference/README.md`](reference/README.md).

## Versions

Checked October 2026: OpenBao 2.7.1 · Vault Community 2.1.1 (for the comparison) · PostgreSQL 18 · PgBouncer 1.24 · HAProxy 3.4 · Go + Fiber v3.5.0 · pgx v5.11

Research and lab by Hezki (technical research intern, production team), October 2026.
