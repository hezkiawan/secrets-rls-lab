# secrets-rls-lab

Research lab for the production team: **secrets management (OpenBao vs HashiCorp Vault)** and **PostgreSQL Row Level Security**, using the team's stack (Go + Fiber v3, PostgreSQL, PgBouncer).

| Folder | What it is |
|---|---|
| `docker-compose.yml`, `bootstrap/` | **Learning lab** (dev mode): OpenBao + Vault side by side, setup-as-code scripts for M1–M3 |
| `reference/` | **Production-like reference stack**: PostgreSQL with RLS + PgBouncer (M3); 3-node OpenBao cluster + HAProxy, auto-unseal, dynamic DB credentials (M5). Walkthrough: [reference/README.md](reference/README.md) |
| `api/` | Go + Fiber v3 API: AppRole login, secrets from OpenBao, RLS-protected conversations API, demo page |
| `docs/` | **Start here:** [`research-report.md`](docs/research-report.md) (summary + recommendation), [`deployment-guide.md`](docs/deployment-guide.md), [`rls-guide.md`](docs/rls-guide.md), [`firebase-guide.md`](docs/firebase-guide.md), [`comparison-findings.md`](docs/comparison-findings.md), milestone notes in `notes/` |
| `reference/firebase/` | Firestore + Realtime DB Security Rules for the same support desk, with emulator tests |

See [PLAN.md](PLAN.md) for milestones.

## Requirements
Docker Desktop (Compose v2) · Go 1.25+

## Run everything (PowerShell, from this folder)

```powershell
# 1. Learning lab: OpenBao (dev mode) — holds the API's secrets
docker compose up -d
docker compose run --rm bootstrap                         # M1: secrets, policy, AppRole
docker compose run --rm bootstrap /bootstrap/m3-setup.sh  # M3: DB logins in OpenBao
#   (re-run both after every OpenBao restart: dev mode forgets everything)

# 2. Reference stack: PostgreSQL (RLS) + PgBouncer
docker compose -f reference/docker-compose.yml up -d --build

# 3. API
cd api
go mod tidy
go run .
```

Open **http://localhost:3000/** — the Conversation Desk demo page.

| URL | What |
|---|---|
| http://localhost:3000/ | RLS demo page: switch users, try to break it |
| http://localhost:3000/status | OpenBao token + secret fingerprints (M1) |
| http://localhost:3000/demo/read?path=otherproduct/app | OpenBao policy demo (M1) |
| http://localhost:8200 / :8210 | OpenBao / Vault UI (token `root`) |
| http://localhost:5050 / :5051 | pgAdmin: learning lab / reference database |

**Production-like cluster with dynamic DB credentials (M5):** see [reference/README.md](reference/README.md).

Optional comparison (M4): `docker compose --profile vault up -d vault`, then `docker compose run --rm bootstrap-vault`.

## Versions (checked 2026-10-05)
OpenBao 2.7.1 · Vault 2.1.1 · HAProxy 3.4 · PostgreSQL 18 · PgBouncer 1.24 (Alpine) · pgAdmin 9.18 · Fiber v3.5.0 · pgx v5.11 · golang-jwt v5.3

> ⚠️ Dev-only passwords and OpenBao dev mode throughout. This is a lab, not a deployment template. Production guidance lives in `docs/`.
