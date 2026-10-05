#!/bin/sh
# M1 bootstrap — "setup as code".
# Does exactly what you did by hand in the OpenBao UI, so it can be repeated,
# reviewed in git, and re-run after every dev-mode restart (dev mode forgets everything).
#
# Run from the project root:   docker compose run --rm bootstrap
set -eu
CLI="${CLI:-bao}"   # "bao" for OpenBao, "vault" for HashiCorp Vault — same commands

echo "== M1 bootstrap using: $CLI"

# --- 1. Secrets (KV v2 is already mounted at secret/ in dev mode) -------------
echo "-- writing secrets"
$CLI kv put secret/kouventa/app \
  jwt_signing_key="$(head -c 32 /dev/urandom | base64)" \
  meta_api_token="EAAG-fake-meta-business-token-for-the-lab"

$CLI kv put secret/kouventa/firebase \
  service_account_json=@/bootstrap/fake-firebase-service-account.json

$CLI kv put secret/kouventa/admin/break-glass \
  note="emergency DB superuser - humans only, never the app"

$CLI kv put secret/otherproduct/app \
  api_key="otherproduct-secret-kouventa-must-not-see-this"

# --- 2. Policy -----------------------------------------------------------------
echo "-- writing policy kouventa-app"
$CLI policy write kouventa-app /bootstrap/policies/kouventa-app.hcl

# --- 3. AppRole auth method + a role for the Kouventa API ---------------------
if ! $CLI auth list -format=json | grep -q '"approle/"'; then
  echo "-- enabling approle auth method"
  $CLI auth enable approle
fi

echo "-- writing role kouventa-api"
# token_ttl / token_max_ttl : how long a login token lives (renewable up to max)
# secret_id_ttl             : a secret ID expires after 24h if unused
# secret_id_num_uses=0      : unlimited logins per secret ID (lab convenience;
#                             production often uses 1 = single-use)
$CLI write auth/approle/role/kouventa-api \
  token_policies="kouventa-app" \
  token_ttl=1h \
  token_max_ttl=4h \
  secret_id_ttl=24h \
  secret_id_num_uses=0

# --- 4. Deliver "secret zero" to the app ---------------------------------------
# In production a deploy pipeline/orchestrator does this step, ideally with a
# short-lived, single-use or response-wrapped secret ID. Here we write two files.
echo "-- delivering role_id and secret_id (written to the folder mounted at /out)"
$CLI read -field=role_id auth/approle/role/kouventa-api/role-id > /out/role_id
$CLI write -f -field=secret_id auth/approle/role/kouventa-api/secret-id > /out/secret_id

echo "== done. Now start the API:  cd api; go run ."
