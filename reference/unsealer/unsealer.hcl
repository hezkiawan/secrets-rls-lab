# unsealer.hcl — the small OpenBao that holds the key which auto-unseals the main cluster.
#
# It must survive restarts (if it loses its key, the main cluster can't unseal), so:
#   - persistent storage (single-node Raft — OpenBao 2.x has no "file" backend), and
#   - it unseals ITSELF with a static key (seal "static", built into OpenBao).
# Docs: https://openbao.org/docs/configuration/seal/static/
#   "only recommended when an existing source of trust ... already exists" — in production
#   that source of trust is your KMS/HSM or a hardened, separately-operated OpenBao.
# LAB ONLY: the static key comes from an environment variable set in docker-compose.yml.

ui           = true
api_addr     = "http://unsealer:8200"
cluster_addr = "https://unsealer:8201"

storage "raft" {
  path    = "/openbao/file"
  node_id = "unsealer"
}

listener "tcp" {
  address     = "0.0.0.0:8200"
  tls_disable = true   # LAB ONLY
}

seal "static" {
  current_key_id = "lab-unsealer-key-1"
  current_key    = "env://UNSEALER_STATIC_KEY"   # 32 bytes, base64
}
