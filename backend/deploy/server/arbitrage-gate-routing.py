#!/usr/bin/env python3
"""Route only Gate collector egress through server01's existing awg0 VPN.
The dedicated Docker address is stable across restarts. No DNS IP hardcoding,
VPN keys, global default route or existing application networks are modified.
"""
import fcntl
import json
import pathlib
import subprocess

PRIORITY = "32120"
TABLE = "51820"
SOURCE = "172.31.254.2"
STATE = pathlib.Path("/var/lib/shortlong/arbitrage-gate-routing.json")


def run(*args):
    return subprocess.run(args, check=True, capture_output=True, text=True).stdout


def main():
    STATE.parent.mkdir(parents=True, exist_ok=True)
    with STATE.with_suffix(".lock").open("w") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        routes = json.loads(run("ip", "-j", "-4", "route", "show", "table", TABLE))
        if not any(r.get("dev") == "awg0" and r.get("dst") == "default" for r in routes):
            raise RuntimeError("Existing awg0 policy route is unavailable")
        previous = json.loads(STATE.read_text()) if STATE.exists() else {}
        rules = json.loads(run("ip", "-j", "-4", "rule", "show"))
        present = False
        for rule in rules:
            if str(rule.get("priority")) != PRIORITY:
                continue
            if str(rule.get("table")) != TABLE:
                raise RuntimeError("Policy priority is used by an unrelated rule")
            source = rule.get("src", "").removesuffix("/32")
            if source == SOURCE and rule.get("dst", "all") == "all":
                present = True
                continue
            # Remove only destination rules owned by the earlier probe revision.
            destination = rule.get("dst", "").removesuffix("/32")
            if source in ("all", "") and destination in previous:
                run("ip", "-4", "rule", "del", "priority", PRIORITY, "to", destination + "/32", "lookup", TABLE)
            else:
                raise RuntimeError("Policy priority is used by an unrelated rule")
        if not present:
            run("ip", "-4", "rule", "add", "priority", PRIORITY, "from", SOURCE + "/32", "lookup", TABLE)
        temporary = STATE.with_suffix(".new")
        temporary.write_text(json.dumps({"source": SOURCE, "table": TABLE}))
        temporary.chmod(0o600)
        temporary.replace(STATE)
        print("Gate collector egress routed through awg0; global default route unchanged")


if __name__ == "__main__":
    main()
