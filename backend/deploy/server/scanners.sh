#!/bin/sh
# Temporarily pause/resume all live market scanners without touching the
# archive, databases, web application or persisted scanner state.
set -eu

cd /opt/shortlong/backend
services='scanner_bybit_shard0 scanner_bybit_shard1 scanner_binance scanner_binance_shard1 scanner_okx_liquidations scanner_bitget_liquidations scanner_gateio_liquidations'

case "${1:-}" in
  pause)
    for service in $services; do
      id=$(./deploy/server/compose.sh ps -q "$service")
      [ -n "$id" ] || continue
      docker update --restart=no "$id" >/dev/null
    done
    # A manual compose stop keeps scanner state/volumes intact. Restart policy
    # was disabled first, so a host or Docker restart cannot bring them back.
    ./deploy/server/compose.sh stop $services
    ;;
  resume)
    for service in $services; do
      id=$(./deploy/server/compose.sh ps -aq "$service")
      [ -n "$id" ] || continue
      docker update --restart=unless-stopped "$id" >/dev/null
    done
    ./deploy/server/compose.sh up -d $services
    ;;
  *)
    echo "Usage: $0 pause|resume" >&2
    exit 2
    ;;
esac
