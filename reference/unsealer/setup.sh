#!/bin/sh
# unsealer/setup.sh — prepares the small "unsealer" OpenBao that auto-unseals the main cluster.
#
# Transit auto-unseal: the main cluster's master key is encrypted by a key that lives in
# ANOTHER OpenBao (its "transit" engine). On start, each node asks the unsealer to decrypt
# it — no human needs to type unseal keys after a restart.
# Why this matters for the team: OpenBao has no Tencent Cloud KMS seal, and Transit works anywhere.
#
# Docs: https://openbao.org/docs/configuration/seal/transit/
# PRODUCTION: the unsealer is its own small, hardened OpenBao on a separate VM
# (or use a cloud KMS / HSM instead of an unsealer).
set -eu

# First start only: initialize the unsealer and keep its root token (LAB ONLY: in a file).
if [ ! -s /secrets/unsealer-init.json ]; then
  bao operator init -recovery-shares=1 -recovery-threshold=1 -format=json > /secrets/unsealer-init.json
  echo "== unsealer initialized"
  sleep 2
fi
BAO_TOKEN=$(sed -n 's/.*"root_token": *"\([^"]*\)".*/\1/p' /secrets/unsealer-init.json)
export BAO_TOKEN

if ! bao secrets list -format=json | grep -q '"transit/"'; then
  bao secrets enable transit
fi
bao write -f transit/keys/autounseal

# Least privilege: the token may ONLY encrypt/decrypt with this one key (from the docs).
bao policy write autounseal - <<'POLICY'
path "transit/encrypt/autounseal" { capabilities = ["update"] }
path "transit/decrypt/autounseal" { capabilities = ["update"] }
POLICY

# A periodic, orphan token with a fixed ID so the nodes' compose file can reference it.
# Periodic = renewable forever as long as it's renewed within 24h (the seal renews it).
if ! bao token lookup "$UNSEAL_TOKEN" >/dev/null 2>&1; then
  bao token create -id="$UNSEAL_TOKEN" -policy=autounseal -orphan -period=24h >/dev/null
fi
echo "== unsealer ready: transit key 'autounseal' + token for the cluster"
