#!/bin/sh
set -eu
cd /opt/shortlong/backend
exec docker compose -f compose.yaml -f compose.local.yaml -f compose.server.yaml "$@"
