# bao-2.hcl — config for node 2 of the 3-node OpenBao cluster.
# On a real VM this file is /etc/openbao/openbao.hcl and the service runs:
#   bao server -config=/etc/openbao/openbao.hcl
# Docs: https://openbao.org/docs/configuration/  ·  storage: .../configuration/storage/raft/

ui           = true
cluster_name = "kouventa-secrets"

# How OTHER machines reach this node: api_addr for clients, cluster_addr for node-to-node.
# On VMs these are the VM's private DNS name or IP.
api_addr     = "http://bao-2:8200"     # PRODUCTION: https://
cluster_addr = "https://bao-2:8201"    # node-to-node traffic is always TLS (automatic)

# Integrated Storage (Raft): every node keeps a full, replicated copy of the data.
# 3 nodes tolerate 1 failure; the docs recommend 5 nodes (tolerate 2) for production.
storage "raft" {
  path    = "/openbao/file"   # on a VM: /opt/openbao/data, on a fast local SSD
  node_id = "bao-2"
  retry_join {
    leader_api_addr = "http://bao-1:8200"
  }
  retry_join {
    leader_api_addr = "http://bao-3:8200"
  }
}

listener "tcp" {
  address         = "0.0.0.0:8200"
  cluster_address = "0.0.0.0:8201"
  # LAB ONLY. PRODUCTION: remove this line and add
  #   tls_cert_file = "/etc/openbao/tls/openbao.crt"
  #   tls_key_file  = "/etc/openbao/tls/openbao.key"
  tls_disable = true
}

# Transit auto-unseal: ask the "unsealer" OpenBao to decrypt our master key at startup.
# The token is NOT in this file: the docs strongly recommend the BAO_TOKEN environment variable.
seal "transit" {
  address    = "http://unsealer:8200"
  key_name   = "autounseal"
  mount_path = "transit/"
}

# Audit log: every request and response, with secret values HMAC-hashed.
# OpenBao 2.7 manages audit devices DECLARATIVELY (here), not via the API/CLI.
# Docs: https://openbao.org/docs/configuration/audit/  — keep this identical on all nodes.
# Lab: written to stdout → `docker compose logs bao-1`. Production: a file shipped to your log system.
audit "file" "to-stdout" {
  description = "Write audit information to standard output."
  options {
    file_path = "stdout"
  }
}

# (No disable_mlock: OpenBao removed mlock in 2.0.)
