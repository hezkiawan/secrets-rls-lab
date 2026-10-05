#!/bin/sh
# configure.sh — everything the Kouventa API needs, as code. Safe to re-run.
#   1. KV v2 engine + the app's static secrets (what used to be in .env)
#   2. Policy kouventa-app
#   3. AppRole login for the API (writes role_id/secret_id for it)
#   4. Database engine: dynamic Postgres users that are members of app_runtime (→ RLS applies)
#   (Audit logging is configured in the node config files: OpenBao 2.7 manages it declaratively.)
set -eu
: "${BAO_ADDR:=http://haproxy:8300}"
: "${PG_ADDR:=postgres:5432}"      # how OpenBao reaches Postgres (directly, not via PgBouncer)
export BAO_ADDR
BAO_TOKEN=$(sed -n 's/.*"root_token": *"\([^"]*\)".*/\1/p' /secrets/init.json)
export BAO_TOKEN
[ -n "$BAO_TOKEN" ] || { echo "no root token found — run init.sh first"; exit 1; }
echo "== configuring via $BAO_ADDR"

# --- 1. KV v2 + static secrets ------------------------------------------------------
# (Dev mode mounted secret/ for us. A real cluster starts empty: we enable it.)
if ! bao secrets list -format=json | grep -q '"secret/"'; then
  bao secrets enable -path=secret kv-v2
fi
if ! bao kv get secret/kouventa/app >/dev/null 2>&1; then
  bao kv put secret/kouventa/app \
    jwt_signing_key="$(head -c 32 /dev/urandom | base64)" \
    meta_api_token="EAAG-fake-meta-business-token-for-the-lab"
fi
bao kv put secret/kouventa/firebase service_account_json=@/bootstrap/fake-firebase-service-account.json >/dev/null
# LAB ONLY (for the "owner bypasses RLS" demo). Never give an app the owner's login.
bao kv put secret/kouventa/db-owner username="app_owner" password="owner-dev-only" >/dev/null
bao kv put secret/kouventa/admin/break-glass note="humans only, never the app" >/dev/null

# --- 2. Policy ------------------------------------------------------------------------
bao policy write kouventa-app /policies/kouventa-app.hcl

# --- 3. AppRole -----------------------------------------------------------------------
if ! bao auth list -format=json | grep -q '"approle/"'; then
  bao auth enable approle
fi
# Short TTLs so you can WATCH the API renew and re-login. Production: e.g. 1h / 24h.
bao write auth/approle/role/kouventa-api \
  token_policies="kouventa-app" token_ttl=2m token_max_ttl=6m \
  secret_id_ttl=24h secret_id_num_uses=0
bao read  -field=role_id   auth/approle/role/kouventa-api/role-id   > /out/role_id
bao write -f -field=secret_id auth/approle/role/kouventa-api/secret-id > /out/secret_id

# --- 4. Database engine (dynamic credentials) -----------------------------------------
if ! bao secrets list -format=json | grep -q '"database/"'; then
  bao secrets enable database
fi
if ! bao read database/config/supportdesk >/dev/null 2>&1; then
  # OpenBao talks to Postgres directly (admin work), not through PgBouncer.
  bao write database/config/supportdesk \
    plugin_name="postgresql-database-plugin" \
    allowed_roles="kouventa-app" \
    connection_url="postgresql://{{username}}:{{password}}@${PG_ADDR}/supportdesk?sslmode=disable" \
    username="openbao_admin" \
    password="openbao-admin-initial" \
    password_authentication="scram-sha-256"
  # Rotate the admin password: from now on ONLY OpenBao knows it.
  bao write -f database/rotate-root/supportdesk
fi
# Each temporary user is a MEMBER of app_runtime → same grants, and RLS applies.
# Short TTLs so you can watch the API rotate credentials. Production: e.g. 1h / 24h.
bao write database/roles/kouventa-app \
  db_name="supportdesk" \
  creation_statements="CREATE ROLE \"{{name}}\" WITH LOGIN PASSWORD '{{password}}' VALID UNTIL '{{expiration}}' IN ROLE app_runtime;" \
  default_ttl="2m" \
  max_ttl="6m"

echo "== done. Start the API against the cluster (see reference/README.md)."
