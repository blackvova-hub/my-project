#!/bin/sh
set -eu
cd /opt/shortlong/backend
if [ -f deploy/server/compose.arbitrage-gate-vpn.yaml ]; then
  exec docker compose -f compose.yaml -f compose.local.yaml -f compose.server.yaml -f deploy/server/compose.arbitrage-gate-vpn.yaml "$@"
fi
exec docker compose -f compose.yaml -f compose.local.yaml -f compose.server.yaml "$@"
