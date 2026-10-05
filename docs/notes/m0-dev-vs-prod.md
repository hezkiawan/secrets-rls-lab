# M0 notes — Dev mode vs production

Source: OpenBao docs, "Dev server mode" (v2.7.x) — https://openbao.org/docs/concepts/dev-server/

## What dev mode does for you (and why each is unsafe in production)

| Dev mode shortcut | Production reality |
|---|---|
| **Auto-initialized and unsealed** | Someone must initialize once, then unseal after every restart (by hand with key shares, or auto-unseal via a cloud KMS). |
| **In-memory storage** — data lost on restart | Persistent storage (Integrated Storage / Raft) on disk, replicated across nodes. |
| **No TLS**, listens on HTTP | TLS required — secrets would otherwise cross the network in plain text. |
| **Root token is known** (`root` in our compose file) | Root token used only for initial setup, then revoked. Apps and humans log in with limited identities. |
| **Single unseal key** | Key split into shares (e.g. 5 shares, 3 needed), held by different people. |
| **KV v2 already mounted at `secret/`** | Secrets engines enabled deliberately, as part of setup-as-code. |

The docs are blunt: never run a dev-mode server in production.

## Things to try (and what they teach)

1. **Data disappears on restart.** In the UI, create a secret under `secret/`. Then run `docker compose restart openbao`, log in again → the secret is gone. That's "in-memory storage".
2. **Readiness vs liveness.** Run `docker compose stop openbao`, then open `/livez` (still 200) and `/readyz` (503). The API process is alive but can't do its job. Load balancers and monitoring use exactly this difference. `docker compose start openbao` → `/readyz` back to 200.
3. **Unauthenticated health endpoint.** `/v1/sys/health` needs no token (it's for load balancers). Open http://localhost:8200/v1/sys/health in the browser. Default status codes: 200 active · 429 standby · 503 sealed · 501 not initialized.

## Go concepts introduced
- `go.mod` = the project (module) and its dependencies; `go.sum` = checksums so dependencies can't be swapped.
- `package main` + `func main()` = an executable. All `.go` files in one folder share a package, so `main.go` can call `loadConfig()` from `config.go` directly.
- Errors are values: functions return `(result, error)` and every caller checks `if err != nil`.
- `fmt.Errorf("...: %w", err)` wraps an error with context, building a readable chain.
- Structs + JSON tags map JSON fields onto Go fields.
- `defer` schedules cleanup (closing the response body) for when the function returns.
- Capitalized names are exported (visible outside the package); lowercase names are private.
