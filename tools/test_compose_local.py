"""Protect the explicit local resource overlay from changing deployment behavior."""

import unittest
from pathlib import Path

import yaml


ROOT = Path(__file__).resolve().parents[1]
PERSISTENT = {"postgres", "api", "heatbot", "web"}


def memory_bytes(value):
    value = str(value).lower()
    units = {"g": 1024**3, "m": 1024**2}
    return int(value[:-1]) * units[value[-1]]


class LocalComposeTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.base = yaml.safe_load((ROOT / "compose.yaml").read_text())
        cls.local = yaml.safe_load((ROOT / "compose.local.yaml").read_text())

    def test_only_resource_fields_are_overridden(self):
        self.assertEqual(set(self.local), {"services"})
        self.assertEqual(set(self.local["services"]), set(self.base["services"]))
        for service in self.local["services"].values():
            self.assertEqual(set(service), {"cpus", "mem_limit", "memswap_limit"})

    def test_persistent_memory_budget_is_below_two_gib(self):
        total = sum(
            memory_bytes(self.local["services"][name]["mem_limit"])
            for name in PERSISTENT
        )
        self.assertEqual(total, 1920 * 1024**2)
        self.assertLess(total, 2 * 1024**3)

    def test_persistent_limits_do_not_increase_production_limits(self):
        for name in PERSISTENT:
            local = self.local["services"][name]
            base = self.base["services"][name]
            self.assertGreater(local["cpus"], 0)
            self.assertLessEqual(local["cpus"], base["cpus"])
            self.assertLessEqual(
                memory_bytes(local["mem_limit"]), memory_bytes(base["mem_limit"])
            )

    def test_local_swap_budget_cannot_exceed_memory(self):
        for service in self.local["services"].values():
            self.assertEqual(service["memswap_limit"], service["mem_limit"])

    def test_production_readiness_gates_stay_closed(self):
        self.assertEqual(
            self.base["services"]["api"]["environment"]["ENABLE_NATIVE"], "false"
        )
        self.assertEqual(
            self.base["services"]["heatbot"]["environment"]["AI_ALLOW_TEST_GENERATION"],
            "false",
        )


if __name__ == "__main__":
    unittest.main()
