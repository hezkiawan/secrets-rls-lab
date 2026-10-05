# secrets-rls-lab

A hands-on lab for the production team's research on **secrets management (OpenBao / HashiCorp Vault)** and **PostgreSQL Row Level Security**, using the team's stack: Go + Fiber v3, PostgreSQL, Docker.

See [PLAN.md](PLAN.md) for the milestones and [docs/notes/](docs/notes/) for what each milestone teaches.

## Requirements

- Docker Desktop (Compose v2)
- Go 1.25+ (Fiber v3 requires it)

## Current milestone: M1 — AppRole login + secrets from OpenBao

| Service | URL | Login |
|---|---|---|
| OpenBao UI + API | http://localhost:8200 | Token: `root` (dev mode only) |
| pgAdmin | http://localhost:5050 | No login. Postgres password: `postgres-dev-only` |
| PostgreSQL | localhost:5432 | `postgres` / `postgres-dev-only`, DB `supportdesk` |
| Lab API | http://localhost:3000 | none |

### Run it (PowerShell, from this folder)

```powershell
docker compose up -d               # start OpenBao, Postgres, pgAdmin
docker compose ps                  # all should become "healthy"
docker compose run --rm bootstrap  # load secrets, policy, AppRole into OpenBao
                                   # (re-run after EVERY OpenBao restart — dev mode forgets)

cd api
go mod tidy                        # after each milestone: downloads new dependencies
go run .                           # start the API (Ctrl+C to stop)
```

Then open in the browser:

- http://localhost:3000/livez  — is the API process alive?
- http://localhost:3000/readyz — can it do real work (is OpenBao ready)?
- http://localhost:3000/status — token policies + TTL, secret fingerprints (never values)
- http://localhost:3000/demo/read?path=kouventa/app — allowed by the policy
- http://localhost:3000/demo/read?path=otherproduct/app — denied (another product)
- http://localhost:3000/demo/read?path=kouventa/admin/break-glass — denied (explicit deny)

### Stop / reset

```powershell
docker compose down      # stop everything (Postgres data kept)
docker compose down -v   # stop and delete Postgres data
```

## Versions (checked 2026-10-02)

| Component | Version | Source |
|---|---|---|
| OpenBao | 2.7.1 | github.com/openbao/openbao releases |
| PostgreSQL | 18 | 19 is still in beta |
| pgAdmin | 9.18 | github.com/pgadmin-org/pgadmin4 |
| Fiber | v3.5.0 | github.com/gofiber/fiber |

> ⚠️ Everything in this repo uses **dev-only** passwords and OpenBao **dev mode**. It is a lab, not a deployment template. Production setup is covered in M5.
