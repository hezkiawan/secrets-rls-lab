#!/bin/sh
# import-env.sh — move an existing .env file into OpenBao (KV v2), one product at a time.
#
#   docker compose -f reference/docker-compose.yml run --rm tools /scripts/import-env.sh kouventa /env/example.env
#
# Each KEY=VALUE line becomes one key in secret/<product>/env. Comments and blank lines are skipped.
# After importing: change the app to read from OpenBao, then DELETE the .env from the server.
set -eu
[ $# -eq 2 ] || { echo "usage: import-env.sh <product> <file.env>"; exit 1; }
product=$1; file=$2
: "${BAO_ADDR:=http://haproxy:8300}"; export BAO_ADDR
BAO_TOKEN=$(sed -n 's/.*"root_token": *"\([^"]*\)".*/\1/p' /secrets/init.json); export BAO_TOKEN

set --
count=0
while IFS= read -r line || [ -n "$line" ]; do
  line=$(printf '%s' "$line" | tr -d '\r')            # tolerate Windows line endings
  case "$line" in ''|\#*) continue ;; esac
  line=${line#export }
  key=${line%%=*}; val=${line#*=}
  case "$val" in \"*\") val=${val#\"}; val=${val%\"} ;; \'*\') val=${val#\'}; val=${val%\'} ;; esac
  case "$val" in @*) val="\\$val" ;; esac              # a leading @ would mean "read a file" to the CLI
  set -- "$@" "$key=$val"
  count=$((count + 1))
done < "$file"

bao kv put "secret/$product/env" "$@" >/dev/null
echo "== imported $count keys from $file into secret/$product/env"
bao kv get -format=json "secret/$product/env" | sed -n 's/^ *"\([A-Z_][A-Z0-9_]*\)": .*/   - \1/p'
