# api/ — the Go API

A small support desk. It gets **all its secrets from OpenBao** and lets **PostgreSQL RLS** decide which rows each user sees.

Run it (reference stack running, see [../reference/README.md](../reference/README.md)):

```powershell
cd api
go mod tidy
go run .
```

Then open http://localhost:3000/.

## Folders

| Folder | Job | Talks to |
|---|---|---|
| `main.go` | Startup, STEP 1–7, plus the list of routes | everything |
| `background.go` | Two jobs that keep the OpenBao token and the DB user alive | `openbao/`, `database/` |
| `config/` | Reads settings from env vars (addresses only, no secrets) | — |
| `openbao/` | **The only package that talks to OpenBao**: login, secrets, DB user, renew | OpenBao (via HAProxy) |
| `database/` | Opens the Postgres pool; swaps it when the DB user changes; `BeginWithTenant` (RLS context) | PgBouncer → Postgres |
| `models/` | Plain structs: `User`, `Conversation` | — |
| `repository/` | **SQL only**: one function per query | `database/` |
| `services/` | **Rules** between HTTP and SQL (thin on purpose) | `repository/`, `auth/` |
| `handlers/` | **HTTP only**: read the request, call a service, write JSON | `services/` |
| `middleware/` | `RequireLogin`: checks the JWT before a handler runs | `auth/` |
| `auth/` | Creates and checks JWTs with the key from OpenBao | — |
| `web/` | The demo page | — |
| `cmd/simple/` | The whole OpenBao + RLS idea in **one file**, no web server. `go run ./cmd/simple` | |

## One request, top to bottom

```
GET /conversations  (Authorization: Bearer <JWT>)
   │
   ▼
middleware.RequireLogin   check JWT → User{company, id, role}
   │
   ▼
handlers.ListConversations      HTTP in / JSON out
   │
   ▼
services.ListConversations      rules (here: none, it just passes through)
   │
   ▼
repository.ListConversations    BeginWithTenant → SELECT … (no WHERE) → Commit
   │
   ▼
Postgres RLS                    adds "only rows of this company / this agent"
```

OpenBao is **not** called during a request. It is used at startup and by the background jobs.
