# kouventa-app — what the Kouventa API's token may do in the reference cluster.
# Deny by default: anything not listed is forbidden.

# Read the app's own static secrets (and list their names).
path "secret/data/kouventa/*"     { capabilities = ["read"] }
path "secret/metadata/kouventa/*" { capabilities = ["list"] }

# Human-only secrets under kouventa/ (more specific path → wins over the rule above).
path "secret/data/kouventa/admin/*" { capabilities = ["deny"] }

# Get short-lived Postgres credentials (dynamic secrets) — only this role.
path "database/creds/kouventa-app" { capabilities = ["read"] }

# Keep those credentials alive: renew the leases it was given.
path "sys/leases/renew" { capabilities = ["update"] }
