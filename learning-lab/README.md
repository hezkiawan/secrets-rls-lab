# learning-lab — the first lab (dev mode)

> **You don't need this folder to run or understand the system.** Use [`../reference/`](../reference/README.md) instead.

This is where the research started: OpenBao and HashiCorp Vault in **dev mode** (in-memory, auto-unsealed, root token `root`), side by side, with the same setup scripts run against both. It is kept as the evidence behind one claim in the report: **our setup scripts work on OpenBao and Vault unchanged** (chapter 2.4).

| File | What it is |
|---|---|
| `docker-compose.yml` | OpenBao (`:8200`), Postgres (`:5432`), pgAdmin, and Vault (`:8210`, profile `vault`) |
| `bootstrap/m1-setup.sh` | KV secrets, policies, AppRole login |
| `bootstrap/m2-setup.sh` | Database secrets engine (static + dynamic roles) |
| `bootstrap/m3-setup.sh` | Stores the API database logins in OpenBao |

Run it (PowerShell, from the repo root):

```powershell
cd learning-lab
docker compose up -d
docker compose run --rm bootstrap                    # the scripts against OpenBao

docker compose --profile vault up -d vault
docker compose run --rm bootstrap-vault              # the same scripts against Vault
```

Dev mode forgets everything on restart. Never use dev mode outside a laptop.
