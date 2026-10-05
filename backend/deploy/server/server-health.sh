#!/bin/sh
# Compact operational snapshot for server01. Run with sudo on the server.
set -eu

printf '%s\n' '=== Server ==='
date -Is
hostname
printf 'CPU: '
nproc
uptime
free -h

printf '%s\n' '=== CPU temperatures ==='
found=0
for d in /sys/class/hwmon/hwmon*; do
  [ -r "$d/name" ] || continue
  [ "$(cat "$d/name")" = coretemp ] || continue
  for t in "$d"/temp*_input; do
    [ -r "$t" ] || continue
    found=1
    printf '%s: ' "$(basename "$t")"
    awk '{printf "%.1f C\\n", $1 / 1000}' "$t"
  done
done
[ "$found" = 1 ] || printf '%s\n' 'No CPU temperature sensor exposed.'

printf '%s\n' '=== Storage ==='
df -h / /srv/archive
for disk in /dev/sda /dev/sdb /dev/sdc; do
  [ -b "$disk" ] || continue
  health=$(smartctl -H "$disk" 2>/dev/null | sed -n 's/.*test result: //p; s/.*status: //p' | head -n 1)
  temperature=$(smartctl -A "$disk" 2>/dev/null | awk '/Temperature_Celsius|Airflow_Temperature_Cel/ {print $NF; exit}')
  printf '%s: health=%s temperature=%s\n' "$disk" "${health:-unknown}" "${temperature:-unavailable}"
done

printf '%s\n' '=== Containers ==='
docker ps --format '{{.Names}} {{.Status}}'
printf '%s\n' '=== Container usage ==='
docker stats --no-stream --format '{{.Name}} CPU={{.CPUPerc}} RAM={{.MemUsage}}'

printf '%s\n' '=== Archive ==='
cd /opt/shortlong/backend
./deploy/server/compose.sh exec -T market_history /app/archive-sync -status

printf '%s\n' '=== PostgreSQL ==='
docker exec crypto_postgres psql -U postgres -d alerts -At -c \
  "SELECT state,count(*) FROM pg_stat_activity GROUP BY state ORDER BY state;"
