"""Policy ownership tests: never execute host networking commands."""
import importlib.util
import json
import pathlib
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

if sys.platform == "win32":
    sys.modules.setdefault("fcntl", types.SimpleNamespace(LOCK_EX=2, flock=lambda *args: None))
spec = importlib.util.spec_from_file_location("routing", pathlib.Path(__file__).with_name("arbitrage-gate-routing.py"))
routing = importlib.util.module_from_spec(spec)
spec.loader.exec_module(routing)


class RoutingOwnershipTests(unittest.TestCase):
    def exercise(self, rules):
        calls = []

        def run(*args):
            if args[3:5] == ("route", "show"):
                return json.dumps([{"dev": "awg0", "dst": "default"}])
            if args[3:5] == ("rule", "show"):
                return json.dumps(rules)
            calls.append(args)
            return ""

        with tempfile.TemporaryDirectory() as directory, patch.object(routing, "STATE", pathlib.Path(directory) / "state.json"), patch.object(routing, "run", run):
            routing.main()
        return calls

    def test_adds_only_two_owned_source_rules(self):
        calls = self.exercise([])
        self.assertEqual(len(calls), 2)
        self.assertTrue(all("add" in call and "from" in call and "51820" in call for call in calls))
        self.assertEqual({call[call.index("from") + 1] for call in calls}, {"172.31.254.2/32", "172.31.254.3/32"})

    def test_existing_rules_are_idempotent(self):
        rules = [{"priority": p, "table": 51820, "src": source} for p, source in [(32120, "172.31.254.2"), (32121, "172.31.254.3")]]
        self.assertEqual(self.exercise(rules), [])

    def test_unrelated_priority_is_rejected(self):
        with self.assertRaisesRegex(RuntimeError, "unrelated"):
            self.exercise([{"priority": 32121, "table": 99, "src": "172.31.254.3"}])


if __name__ == "__main__":
    unittest.main()
