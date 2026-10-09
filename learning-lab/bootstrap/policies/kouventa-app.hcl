# Policy: kouventa-app
# Attached to the Kouventa API's AppRole. Policies are DENY BY DEFAULT:
# anything not listed below is forbidden — including every other product's secrets.
#
# KV v2 note: the CLI path `secret/kouventa/app` maps to two API paths:
#   secret/data/kouventa/app      ← the secret VALUES
#   secret/metadata/kouventa/app  ← names, versions, timestamps (no values)
# Policies must use the API paths.

# 1. Read Kouventa's own secrets.
path "secret/data/kouventa/*" {
  capabilities = ["read"]
}

# 2. List which Kouventa secrets exist (names only — values stay protected).
path "secret/metadata/kouventa/*" {
  capabilities = ["list"]
}

# 3. Explicit deny: admin / break-glass secrets live under kouventa/ too, but the
#    app must never read them. When several rules match a path, OpenBao uses only
#    the HIGHEST-PRIORITY match; this more specific path outranks rule 1.
#    "deny" also overrides any other capability on the same path.
path "secret/data/kouventa/admin/*" {
  capabilities = ["deny"]
}

# 4. (M2) Get short-lived Postgres credentials for the Kouventa role — and nothing
#    else under database/ (not the connection config, not other roles).
path "database/creds/kouventa-app" {
  capabilities = ["read"]
}

# Not listed = not allowed:
#   - secret/data/otherproduct/*     (another product's secrets)
#   - writing/deleting anything      (the app is read-only)
#   - sys/*, auth/*                  (no admin powers)
