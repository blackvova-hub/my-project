#!/usr/bin/env python3
"""Integration test in a disposable container/volume; no production writes.

Requires Docker and the production ClickHouse image already installed. Run as
root on the server; optionally pass --image to test an explicit image ID/tag.
"""
import argparse
import json
from pathlib import Path
import subprocess
import tempfile
import time
import uuid
import xml.etree.ElementTree as ET


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image")
    args = parser.parse_args()
    source = Path(__file__).resolve().parent
    logs = ["trace_log", "text_log", "metric_log", "processors_profile_log",
            "query_metric_log", "asynchronous_metric_log", "query_log", "part_log"]
    name = "shortlong-clickhouse-budget-qa-" + uuid.uuid4().hex[:8]
    volume = name + "-data"

    def run(argv):
        return subprocess.check_output(argv, text=True).strip()

    image = args.image or run(["docker", "inspect", "-f", "{{.Image}}", "shortlong-clickhouse-1"])

    def sql(query):
        return run(["docker", "exec", name, "clickhouse-client", "--query", query])

    def ready():
        for _ in range(45):
            try:
                if sql("SELECT 1") == "1":
                    return
            except subprocess.CalledProcessError:
                pass
            time.sleep(1)
        raise RuntimeError("ClickHouse startup failed")

    with tempfile.TemporaryDirectory(prefix="shortlong-clickhouse-qa-") as directory:
        base = Path(directory)
        base.chmod(0o755)
        config = base / "budget.xml"
        manifest = base / "manifest.json"
        final = (source / "clickhouse.xml").read_text()
        # Stock log writers generate a real pre-migration fixture.
        config.write_text("<clickhouse><max_server_memory_usage>1073741824</max_server_memory_usage>"
                          "<logger><level>warning</level></logger></clickhouse>")
        volume_created = container_created = False

        def restart(contents):
            config.write_text(contents)  # Preserve the bind-mounted inode.
            run(["docker", "restart", name])
            ready()

        try:
            run(["docker", "volume", "create", volume])
            volume_created = True
            run(["docker", "run", "-d", "--name", name, "--cpus", "1", "--memory", "2g",
                 "--mount", "type=volume,source=" + volume + ",target=/var/lib/clickhouse",
                 "-v", str(config) + ":/etc/clickhouse-server/config.d/server-budget.xml:ro",
                 "-v", str(source / "clickhouse-users.xml") + ":/etc/clickhouse-server/users.d/server-budget.xml:ro", image])
            container_created = True
            ready()
            sql("CREATE DATABASE qa_archive")
            sql("CREATE TABLE qa_archive.candles_1m (time DateTime, close Float64) ENGINE=MergeTree ORDER BY time")
            sql("INSERT INTO qa_archive.candles_1m VALUES ('2026-01-01 00:00:00',100),('2026-01-01 00:01:00',101)")
            expected = sql("SELECT count(),sum(close) FROM qa_archive.candles_1m")
            for _ in range(3):
                sql("SELECT count() FROM numbers(10000)")
            sql("SYSTEM FLUSH LOGS")
            root = ET.fromstring(final)
            for table in logs:
                element = root.find(table)
                if element is not None:
                    root.remove(element)
                ET.SubElement(root, table, {"remove": "1"})
            restart(ET.tostring(root, encoding="unicode"))
            command = ["python3", str(source / "clickhouse-log-archive.py"), "--container", name,
                       "--suffix", "qa", "--manifest", str(manifest)]
            run(command)
            run(command)  # Idempotent resume, without repeated renames.
            assert sql("SELECT count(),sum(close) FROM qa_archive.candles_1m") == expected
            saved = json.loads(manifest.read_text())
            assert len(saved["tables"]) >= 3, saved
            for table, entry in saved["tables"].items():
                sql("ATTACH TABLE system." + entry["archived_name"])
                physical = sql("SELECT sum(rows),sum(bytes_on_disk) FROM system.parts WHERE active "
                               "AND database='system' AND table='" + entry["archived_name"] + "'")
                rows = sum(part["rows"] for part in entry["active_parts"])
                size = sum(part["bytes_on_disk"] for part in entry["active_parts"])
                assert physical == f"{rows}\t{size}", (table, physical, rows, size)
                sql("DETACH TABLE system." + entry["archived_name"] + " PERMANENTLY")
            restart(final)
            settings = json.loads(sql("SELECT name,value FROM system.settings WHERE name IN "
                                      "('memory_profiler_step','query_profiler_real_time_period_ns',"
                                      "'query_profiler_cpu_time_period_ns','log_processors_profiles') FORMAT JSON"))["data"]
            assert len(settings) == 4 and all(s["value"] == "0" for s in settings), settings
            sql("INSERT INTO qa_archive.candles_1m VALUES ('2026-01-01 00:02:00',102)")
            assert sql("SELECT count(),sum(close) FROM qa_archive.candles_1m") == "3\t303"
            time.sleep(16)
            sql("SYSTEM FLUSH LOGS")
            for table, days in [("metric_log", 3), ("query_log", 7), ("part_log", 7)]:
                create = sql("SHOW CREATE TABLE system." + table)
                assert f"toIntervalDay({days})" in create and "PARTITION BY event_date" in create, create
            assert int(sql("SELECT count() FROM system.metric_log")) <= 4
            assert sql("SELECT count() FROM system.tables WHERE database='system' AND startsWith(name,'archived_')") == "0"
            assert sql("SELECT count() FROM system.tables WHERE database='system' AND name IN "
                       "('trace_log','processors_profile_log','query_metric_log','asynchronous_metric_log')") == "0"
            server = json.loads(sql("SELECT name,value FROM system.server_settings WHERE name IN "
                                   "('background_pool_size','background_merges_mutations_concurrency_ratio',"
                                   "'total_memory_profiler_step') FORMAT JSON"))["data"]
            assert {s["name"]: s["value"] for s in server} == {
                "background_pool_size": "2", "background_merges_mutations_concurrency_ratio": "2",
                "total_memory_profiler_step": "0"}, server
            print(json.dumps({"version": sql("SELECT version()"), "archived_tables": list(saved["tables"]),
                              "checks": ["startup", "safe archival", "idempotent resume", "exact restored parts/rows/bytes",
                                         "archive survives restart", "candle read/write", "profilers disabled",
                                         "daily TTL", "15s metrics", "four merge contexts"], "result": "PASS"}), flush=True)
        finally:
            if container_created:
                run(["docker", "rm", "-f", name])
            if volume_created:
                run(["docker", "volume", "rm", volume])


if __name__ == "__main__":
    main()
