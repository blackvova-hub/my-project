import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("archive", Path(__file__).with_name("clickhouse-log-archive.py"))
archive = importlib.util.module_from_spec(spec)
spec.loader.exec_module(archive)


class ArchiveSafetyTest(unittest.TestCase):
    def test_application_tables_cannot_be_archived(self):
        for table in ("candles_1m", "shortlong_backtest.candles_1m", "trace_log; DROP DATABASE system"):
            with self.assertRaises(ValueError):
                archive.archive_name(table, "20261006")

    def test_suffix_cannot_inject_sql(self):
        for suffix in ("", "x; DROP TABLE t", "../backup", "x" * 33):
            with self.assertRaises(ValueError):
                archive.archive_name("text_log", suffix)

    def test_active_log_writers_block_archival(self):
        for table in archive.LOG_TABLES:
            with self.assertRaises(ValueError):
                archive.require_disabled("<clickhouse><" + table + "/></clickhouse>")

    def test_disabled_preprocessed_config_permits_archival(self):
        archive.require_disabled("<clickhouse><logger><level>warning</level></logger></clickhouse>")
        self.assertEqual(archive.archive_name("trace_log", "20261006"), "archived_trace_log_20261006")


if __name__ == "__main__":
    unittest.main()
