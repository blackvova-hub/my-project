#!/bin/sh
set -eu
cd /opt/shortlong/backend
vpn_override=""
voice_override=""
if [ -f deploy/server/compose.arbitrage-gate-vpn.yaml ]; then
  vpn_override="-f deploy/server/compose.arbitrage-gate-vpn.yaml"
fi
if [ -f deploy/server/compose.voice.yaml ] && [ -f /var/lib/shortlong/voice/livekit.yaml ]; then
  voice_override="-f deploy/server/compose.voice.yaml"
fi
# These two variables contain only fixed flag/path pairs, never user input.
# shellcheck disable=SC2086
exec docker compose -f compose.yaml -f compose.local.yaml -f compose.server.yaml $vpn_override $voice_override "$@"
