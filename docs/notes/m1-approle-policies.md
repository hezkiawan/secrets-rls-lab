# M1 notes — AppRole, policies, and "secret zero"

Sources (OpenBao v2.7.x docs + v2.7.1 source): AppRole auth, Policies concept page, KV v2, `api/auth/approle` Go module.

## The flow

```
             (1) role_id + secret_id            (2) token (1h, policy: kouventa-app)
  Go API  ─────────────────────────────────▶  OpenBao  ─────────────────────────────▶  Go API
                                                                                          │
             (3) GET secret/data/kouventa/app  + token                                     │
          ◀───────────────────────────────────────────────────────────────────────────────┘
             (4) OpenBao checks the token's policy → returns values, or 403
```

1. **Authenticate** — prove *who* you are (AppRole: role ID + secret ID).
2. OpenBao returns a **token** with a TTL and a list of **policies**.
3. **Authorize** — every later request carries the token; the policy decides *what* it may do.
4. Every request is checkable in the audit log (M5).

## AppRole in one table

| Piece | Like… | How secret? |
|---|---|---|
| Role ID | a username | Low — identifies the role |
| Secret ID | a password | High — this is "secret zero" |
| Token | a session cookie | High, but short-lived (1h here, max 4h) |

**Secret zero**: the app needs *one* credential to fetch all the others. It can't be eliminated, only shrunk and protected: secret IDs can expire (`secret_id_ttl`), be single-use (`secret_id_num_uses=1`), be bound to IP ranges (`secret_id_bound_cidrs`), or be delivered **response-wrapped** (a one-time envelope; the Go client supports `approle.WithWrappingToken()`). In production a deploy pipeline delivers it, not a human.

## Policies ("filtering" = access control)

- **Deny by default**: no matching rule → forbidden.
- Capabilities: `create`, `read`, `update`, `patch`, `delete`, `list`, `sudo`, `deny`.
- **KV v2 paths**: values live under `secret/data/...`, names/versions under `secret/metadata/...`. Policies must use these API paths (the CLI hides the `data/` part).
- **Only the highest-priority matching rule applies** (rules aren't merged). Roughly: more specific paths win — a rule whose first `*` comes later in the path outranks one whose `*` comes earlier. That's why `kouventa/admin/*` (deny) beats `kouventa/*` (read).
- `deny` beats everything on the same path.
- Note from the docs: parameter constraints (`allowed_parameters`, `denied_parameters`, `required_parameters`) are **not supported with KV v2** — so "filtering" for KV secrets is done with paths + capabilities.

## GUI vs code (observed in OpenBao 2.7.1)

| Task | Web UI | CLI / script |
|---|---|---|
| Create / edit KV secrets | ✅ | ✅ |
| Write policies | ✅ (Policies → ACL) | ✅ |
| Enable AppRole auth method | ✅ (Access → Auth methods) | ✅ |
| Create AppRole roles, read role ID, generate secret IDs | ❌ no screen — use the UI's built-in **console** (terminal icon, top left) | ✅ |

Recommendation for the team: GUI for exploring and occasional edits; **setup as code** (like `bootstrap/m1-setup.sh`, or Terraform/OpenTofu later) for anything that must be reproducible — dev mode proved it: one restart wipes every click.

## Go concepts introduced
- Multiple packages: `internal/config`, `internal/openbao`. Exported (Capitalized) names cross package boundaries; lowercase ones don't. `internal/` can only be imported by code in this module.
- Imports of your own packages use the module path from `go.mod`: `secrets-rls-lab/api/internal/config`.
- `context.WithTimeout` + `defer cancel()` — give startup 10 seconds, then give up.
- `log.Fatal` — fail fast if secrets can't be loaded.
- Type assertion `raw.(string)` — check that an `any` value is really a string.
- `errors.As` — inspect an error to find a specific type (here: an OpenBao 403).
- Unexported struct fields + methods to keep secrets from leaking into JSON or logs.
