#!/bin/sh
# snapshot.sh — back up / restore the whole cluster (Raft snapshot).
#   docker compose -f reference/docker-compose.yml run --rm tools /scripts/snapshot.sh save
#   docker compose -f reference/docker-compose.yml run --rm tools /scripts/snapshot.sh restore
# PRODUCTION: run "save" on a schedule (cron/systemd timer), copy snapshots off the VMs
# (e.g. Tencent COS), and TEST restores regularly.
set -eu
: "${BAO_ADDR:=http://haproxy:8300}"; export BAO_ADDR
BAO_TOKEN=$(sed -n 's/.*"root_token": *"\([^"]*\)".*/\1/p' /secrets/init.json); export BAO_TOKEN
case "${1:-}" in
  save)    bao operator raft snapshot save /backups/openbao.snap && ls -l /backups/openbao.snap ;;
  restore) bao operator raft snapshot restore /backups/openbao.snap && echo "== restored" ;;
  *)       echo "usage: snapshot.sh save|restore"; exit 1 ;;
esac
