#!/usr/bin/env python3
"""Preserve old system logs as permanently detached tables, with a manifest.

First restart ClickHouse with these eight log writers disabled. This command
never drops tables or removes files and refuses to run with any writer enabled.
The archived tables retain their data and metadata in the ClickHouse volume.
"""
import argparse
import json
from pathlib import Path
import re
import subprocess
import xml.etree.ElementTree as ET

LOG_TABLES = (
    "trace_log", "text_log", "metric_log", "processors_profile_log",
    "query_metric_log", "asynchronous_metric_log", "query_log", "part_log",
)


def archive_name(table, suffix):
    if table not in LOG_TABLES or not re.fullmatch(r"[a-z0-9_]{1,32}", suffix):
        raise ValueError("Only the explicit system log allowlist and a safe suffix are permitted")
    return "archived_" + table + "_" + suffix


def require_disabled(config):
    root = ET.fromstring(config)
    enabled = [name for name in LOG_TABLES if root.find(name) is not None]
    if enabled:
        raise ValueError("Restart with log writers disabled first: " + ", ".join(enabled))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--container", default="shortlong-clickhouse-1")
    parser.add_argument("--suffix", required=True)
    parser.add_argument("--manifest", type=Path, required=True)
    args = parser.parse_args()
    for table in LOG_TABLES:
        archive_name(table, args.suffix)

    def run(argv):
        return subprocess.check_output(argv, text=True).strip()

    def sql(query):
        # Secrets expand inside the container, never in host argv or the report.
        return run(["docker", "exec", args.container, "sh", "-c",
                    'exec clickhouse-client --user "${CLICKHOUSE_USER:-default}" '
                    '--password "${CLICKHOUSE_PASSWORD:-}" --query "$1"', "sh", query])

    config = run(["docker", "exec", args.container, "cat",
                  "/var/lib/clickhouse/preprocessed_configs/config.xml"])
    require_disabled(config)
    data_path = Path(ET.fromstring(config).findtext("path", "/var/lib/clickhouse"))
    manifest = json.loads(args.manifest.read_text()) if args.manifest.exists() else {
        "container": args.container, "suffix": args.suffix, "tables": {},
    }
    if manifest["container"] != args.container or manifest["suffix"] != args.suffix:
        raise ValueError("Manifest belongs to a different migration")
    args.manifest.parent.mkdir(parents=True, exist_ok=True)

    def save():
        # A durable record exists before any rename/detach; reruns resume it.
        temporary = args.manifest.with_suffix(".tmp")
        temporary.write_text(json.dumps(manifest, indent=2) + "\n")
        temporary.replace(args.manifest)

    for table in LOG_TABLES:
        archived = archive_name(table, args.suffix)
        original_exists = sql("EXISTS TABLE system." + table) == "1"
        archived_exists = sql("EXISTS TABLE system." + archived) == "1"
        if table not in manifest["tables"]:
            if archived_exists:
                raise ValueError("Archive name already in use: " + archived)
            if not original_exists:
                continue
            manifest["tables"][table] = {
                "archived_name": archived,
                "create_query": sql("SHOW CREATE TABLE system." + table + " FORMAT TSVRaw"),
                "detached_metadata": str((data_path / Path(sql(
                    "SELECT metadata_path FROM system.tables WHERE database='system' AND name='" + table + "'"
                ))).with_name(archived + ".sql.detached")),
                "active_parts": json.loads(sql(
                    "SELECT name, rows, bytes_on_disk, path FROM system.parts "
                    "WHERE active AND database='system' AND table='" + table + "' FORMAT JSON"))["data"],
                "status": "recorded",
            }
            save()
        entry = manifest["tables"][table]
        if entry["status"] == "detached":
            if original_exists or archived_exists:
                raise ValueError("Completed archive unexpectedly attached: " + table)
            run(["docker", "exec", args.container, "test", "-f", entry["detached_metadata"]])
            continue
        if original_exists and archived_exists:
            raise ValueError("Both original and archive exist: " + table)
        if original_exists:
            sql("SYSTEM STOP MERGES system." + table)
            sql("RENAME TABLE system." + table + " TO system." + archived)
            entry["status"] = "renamed"
            save()
            archived_exists = True
        if archived_exists:
            sql("DETACH TABLE system." + archived + " PERMANENTLY")
        # DETACH is recoverable with ATTACH TABLE system.<archived_name>.
        if sql("EXISTS TABLE system." + archived) != "0":
            raise RuntimeError("Archive is still attached: " + archived)
        run(["docker", "exec", args.container, "test", "-f", entry["detached_metadata"]])
        entry["status"] = "detached"
        save()
        print(table + " -> " + archived + " (data preserved)", flush=True)


if __name__ == "__main__":
    main()
