import copy
import unittest

from container_smoke import validate_monitor_observation


def observation():
    rows = []
    for offset in range(0, 1801, 5):
        resource = {
            "process_rss_bytes": (160 if offset < 180 else 28)*1024**2,
            "memory_limit_bytes": 512*1024**2,
            "go_goroutines": 35,
            "collection_count": offset//5 + 1,
        }
        rows.append({"elapsed": offset, "api": resource,
                     "heatbot": {"resources": {**resource, "process_rss_bytes": 8*1024**2}},
                     "monitor_pool": {"maximum": 1},
                     "database_stats": {"sampled_at": str(offset//60)}})
    return {"seconds": 1800, "rows": rows, "stream_errors": [],
            "stream_frames": [891, 891], "stream_max_gap_seconds": [3.1, 3.1]}


class MonitorObservationTests(unittest.TestCase):
    def test_startup_memory_release_is_not_growth(self):
        validate_monitor_observation(observation())

    def test_late_growth_or_quota_breach_fails(self):
        result = observation()
        for row in result["rows"]:
            if row["elapsed"] >= 1500:
                row["api"]["process_rss_bytes"] = 230*1024**2
        with self.assertRaisesRegex(RuntimeError, "memory"):
            validate_monitor_observation(result)
        result["rows"][0]["api"]["process_rss_bytes"] = 600*1024**2
        with self.assertRaisesRegex(RuntimeError, "memory"):
            validate_monitor_observation(result)

    def test_readers_cannot_increase_collection_or_database_sampling(self):
        original = observation()
        result = copy.deepcopy(original)
        result["rows"][-1]["api"]["collection_count"] = 1000
        with self.assertRaisesRegex(RuntimeError, "frequency"):
            validate_monitor_observation(result)
        for row in original["rows"]:
            row["database_stats"]["sampled_at"] = str(row["elapsed"])
        with self.assertRaisesRegex(RuntimeError, "statistics frequency"):
            validate_monitor_observation(original)

    def test_stopped_stream_and_goroutine_growth_fail(self):
        result = observation()
        result["stream_frames"][1] = 50
        with self.assertRaisesRegex(RuntimeError, "stream stopped"):
            validate_monitor_observation(result)
        result["stream_frames"][1] = 891
        result["rows"][-1]["heatbot"]["resources"]["go_goroutines"] = 70
        with self.assertRaisesRegex(RuntimeError, "goroutines"):
            validate_monitor_observation(result)


if __name__ == "__main__":
    unittest.main()
