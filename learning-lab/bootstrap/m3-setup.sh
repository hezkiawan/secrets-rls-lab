#!/bin/sh
# M3 bootstrap — store the API's database logins in OpenBao.
# The API reads them at startup (never from a config file).
#
# Run from learning-lab/:   docker compose run --rm bootstrap /bootstrap/m3-setup.sh
# Requires M1 (policy kouventa-app already allows reading secret/kouventa/*).
set -eu
CLI="${CLI:-bao}"

echo "== M3 bootstrap using: $CLI"

# The runtime login: RLS applies to it. This is what the API uses for every request.
# (In M5 this static login is replaced by dynamic credentials from the database engine.)
$CLI kv put secret/kouventa/db \
  username="app_runtime" \
  password="runtime-dev-only"

# LAB ONLY: the table OWNER's login, so the API can demonstrate the "owner bypasses RLS"
# trap at /demo/as-owner. A real application must never be given the owner's credentials.
$CLI kv put secret/kouventa/db-owner \
  username="app_owner" \
  password="owner-dev-only"

echo "== done. Start the reference database, then the API:  cd api; go run ."
