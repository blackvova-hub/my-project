#!/bin/sh
set -eu
cd /opt/shortlong/backend
./deploy/server/compose.sh ps
./deploy/server/compose.sh exec -T market_history /app/archive-sync -status
./deploy/server/compose.sh exec -T clickhouse sh -c 'clickhouse-client --user "$CLICKHOUSE_USER" --password "$CLICKHOUSE_PASSWORD" --query "SELECT table,sum(rows) AS physical_rows,formatReadableSize(sum(bytes_on_disk)) AS disk FROM system.parts WHERE active AND database = '\''shortlong_backtest'\'' GROUP BY table"'
df -h / /srv/archive
