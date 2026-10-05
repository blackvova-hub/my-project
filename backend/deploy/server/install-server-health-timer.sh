#!/bin/sh
set -eu

base=/opt/shortlong/backend/deploy/server
install -m 0755 "$base/shortlong-server-health" /usr/local/sbin/shortlong-server-health
install -m 0644 "$base/shortlong-server-health.service" /etc/systemd/system/shortlong-server-health.service
install -m 0644 "$base/shortlong-server-health.timer" /etc/systemd/system/shortlong-server-health.timer
systemctl daemon-reload
systemctl enable --now shortlong-server-health.timer
systemctl start shortlong-server-health.service
