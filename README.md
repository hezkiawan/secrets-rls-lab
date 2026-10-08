# secrets-rls-lab

Research lab for the production team: **secrets management (OpenBao vs HashiCorp Vault)** and **PostgreSQL Row Level Security**, using the team's stack (Go + Fiber v3, PostgreSQL, PgBouncer).

| Folder | What it is |
|---|---|
| `docker-compose.yml`, `bootstrap/` | **Learning lab** (dev mode): OpenBao + Vault side by side, setup-as-code scripts for M1–M3 |
| `reference/` | **Production-like reference stack**: PostgreSQL with RLS + PgBouncer (M3); 3-node OpenBao cluster + HAProxy, auto-unseal, dynamic DB credentials (M5). Walkthrough: [reference/README.md](reference/README.md) |
| `api/` | Go + Fiber v3 API: AppRole login, secrets from OpenBao, dynamic DB users, RLS-protected conversations API, demo page. Layers: `handlers/` → `services/` → `repository/` (see [api/README.md](api/README.md)) |
| `api/cmd/simple/` | **Start here to read the code:** the whole OpenBao + RLS integration in ONE file, top to bottom (6 numbered steps, no web server). `go run ./cmd/simple` |
| `docs/` | **Start here:** [`research-report.md`](docs/research-report.md) (summary + recommendation), [`deployment-guide.md`](docs/deployment-guide.md), [`rls-guide.md`](docs/rls-guide.md), [`firebase-guide.md`](docs/firebase-guide.md), [`comparison-findings.md`](docs/comparison-findings.md), milestone notes in `notes/` |
| `reference/firebase/` | Firestore + Realtime DB Security Rules for the same support desk, with emulator tests |

See [PLAN.md](PLAN.md) for milestones.

## Requirements
Docker Desktop (Compose v2) · Go 1.25+

## Run everything (PowerShell, from this folder)

The API runs against the **reference stack** (3-node OpenBao cluster + PostgreSQL with RLS + PgBouncer). Full walkthrough: [reference/README.md](reference/README.md). Short version:

```powershell
# 1. Reference stack (first time: follow reference/README.md steps 0–2: init + configure)
docker compose -f reference/docker-compose.yml up -d --build

# 2. API (defaults already point at the reference stack: no env vars needed)
cd api
go mod tidy
go run .
```

Open **http://localhost:3000/** — the Conversation Desk demo page.

| URL | What |
|---|---|
| http://localhost:3000/ | RLS demo page: switch users, try to break it |
| http://localhost:3000/status | Which temporary DB user is in use + which OpenBao node is active |
| http://localhost:8300 | OpenBao UI (through HAProxy; root token in `reference/.secrets/init.json`) |
| http://localhost:8404 | HAProxy stats: which node is active |
| http://localhost:5051 | pgAdmin (reference database) |

> ⚠️ **Stopped the stack for more than 24 hours?** The token the nodes use to auto-unseal is periodic (24h) and expires while nothing renews it. The nodes then fail to unseal (`403` in their logs). Fix: run `docker compose -f reference/docker-compose.yml up -d` again. The one-shot `unsealer-setup` service recreates the token, and the nodes unseal on their next restart (`docker compose -f reference/docker-compose.yml restart bao-1 bao-2 bao-3`).

**Learning lab (M1–M4, dev mode):** the root `docker-compose.yml` + `bootstrap/` scripts show OpenBao and Vault side by side (`docker compose up -d`, `docker compose run --rm bootstrap`; Vault: `docker compose --profile vault up -d vault`, then `docker compose run --rm bootstrap-vault`). The API no longer uses the learning lab.

## Versions (checked 2026-10-05)
OpenBao 2.7.1 · Vault 2.1.1 · HAProxy 3.4 · PostgreSQL 18 · PgBouncer 1.24 (Alpine) · pgAdmin 9.18 · Fiber v3.5.0 · pgx v5.11 · golang-jwt v5.3

> ⚠️ Dev-only passwords and OpenBao dev mode throughout. This is a lab, not a deployment template. Production guidance lives in `docs/`.
