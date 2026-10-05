#!/bin/sh
# init.sh — initialize the cluster ONCE (like formatting a new disk).
#
# With auto-unseal, init returns RECOVERY keys (not unseal keys) plus the initial root token.
#   Recovery keys: needed for rare operations (e.g. generating a new root token). 3 shares, any 2 work.
#   Root token: use it ONLY for initial setup (configure.sh), then revoke it.
#
# LAB ONLY: everything is saved to reference/.secrets/init.json (git-ignored).
# PRODUCTION: give each recovery key to a different person (or use -recovery-pgp-keys),
#             never store them together, never on the server.
set -eu
: "${BAO_ADDR:=http://bao-1:8200}"   # talk to ONE node directly: uninitialized nodes are not in the load balancer
export BAO_ADDR

if bao status -format=json 2>/dev/null | grep -q '"initialized": true'; then
  echo "== already initialized"; exit 0
fi

echo "== initializing via $BAO_ADDR"
bao operator init -recovery-shares=3 -recovery-threshold=2 -format=json > /secrets/init.json
echo "== done. Recovery keys + root token saved to reference/.secrets/init.json (LAB ONLY)"
echo "   The other nodes join automatically (retry_join) and auto-unseal (transit)."
