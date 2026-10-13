#!/usr/bin/env python3
"""Idempotent root-only LAN setup. Does not print keys or change client trust."""
import ipaddress
import json
import os
from pathlib import Path
import secrets
import subprocess

if os.geteuid() != 0:
    raise SystemExit("Run on server01 as root")
env_file = Path("/opt/shortlong/backend/.env")
content = env_file.read_text()
values = {}
for line in content.splitlines():
    if "=" in line and not line.startswith("#"):
        key, value = line.split("=", 1)
        values[key] = value.strip().strip('"\'')
for key in ("LIVEKIT_API_KEY", "LIVEKIT_API_SECRET"):
    if not values.get(key):
        values[key] = secrets.token_hex(16 if key.endswith("KEY") else 32)
        content += f"\n{key}={values[key]}\n"
lan = str(ipaddress.ip_address(values.get("SERVER_LAN_IP", "192.168.0.130")))
bridge = json.loads(subprocess.check_output(["docker", "network", "inspect", "bridge"]))[0]["IPAM"]["Config"][0]["Gateway"]
ipaddress.ip_address(bridge)
directory = Path("/var/lib/shortlong/voice")
directory.mkdir(mode=0o700, parents=True, exist_ok=True)
cache_directory = directory / "catalogue"
cache_directory.mkdir(mode=0o750, exist_ok=True)
app_uid = int(subprocess.check_output(["docker", "exec", "crypto_backend", "id", "-u"]))
app_gid = int(subprocess.check_output(["docker", "exec", "crypto_backend", "id", "-g"]))
os.chown(cache_directory, app_uid, app_gid)
public_ca = directory / "caddy-data/caddy/pki/authorities/local/root.crt"
if public_ca.is_file():
    # Only the public CA certificate is shared with nginx. Keys stay private.
    public_ca.chmod(0o644)
config = f"""port: 7880
bind_addresses:
  - 127.0.0.1
  - {bridge}
rtc:
  node_ip: {lan}
  use_external_ip: false
  tcp_port: 7881
  udp_port: 7882
keys:
  {values['LIVEKIT_API_KEY']}: {values['LIVEKIT_API_SECRET']}
room:
  auto_create: true
  empty_timeout: 1
  departure_timeout: 1
  max_participants: 50
logging:
  level: info
"""
env_file.write_text(content)
env_file.chmod(0o600)
(directory / "livekit.yaml").write_text(config)
(directory / "livekit.yaml").chmod(0o600)
print(f"LiveKit LAN configuration ready for {lan}; credentials kept on server")
if not public_ca.is_file():
    print("Start Caddy, wait for root.crt, then rerun setup to enable public certificate download")
