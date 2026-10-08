#!/bin/sh
# app-check.sh — log in AS THE APP (AppRole, like the Go API does) and test what its policy allows.
#   docker compose -f reference/docker-compose.yml run --rm tools /scripts/app-check.sh
# Reads api/.reference/role_id + secret_id (mounted at /out). No root token involved.
set -u

export BAO_TOKEN=$(bao write -field=token auth/approle/login \
  role_id="$(cat /out/role_id)" secret_id="$(cat /out/secret_id)")

echo "== 1. who am I (expect: policies [default kouventa-app], period 2m)"
bao token lookup | grep -E "^(policies|period)"

echo; echo "== 2. read own secret (expect: the value)"
bao kv get -field=meta_api_token secret/kouventa/app; echo

echo; echo "== 3. read admin secret (expect: 403 permission denied)"
bao kv get secret/kouventa/admin/break-glass

echo; echo "== 4. read another product's secret (expect: 403)"
bao kv get secret/otherproduct/app

echo; echo "== 5. WRITE own secret (expect: 403, the app may only read)"
bao kv put secret/kouventa/app hacked=yes

echo; echo "== 6. get a temporary DB user (expect: username v-approle-kouventa-...)"
bao read database/creds/kouventa-app
