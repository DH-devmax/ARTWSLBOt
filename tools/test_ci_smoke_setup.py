import importlib.util
import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import yaml

SPEC = importlib.util.spec_from_file_location("ci_smoke_setup", Path(__file__).with_name("ci_smoke_setup.py"))
SETUP = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SETUP)


class CISetupTests(unittest.TestCase):
    def test_overlay_only_changes_test_database_and_synthetic_bootstrap(self):
        services = yaml.safe_load(SETUP.OVERLAY)["services"]
        self.assertEqual(set(services), {"postgres", "migrate", "bootstrap", "api"})
        self.assertEqual(services["postgres"]["environment"]["POSTGRES_DB"], "wsl_test")
        for name in ("migrate", "bootstrap", "api"):
            self.assertEqual(set(services[name]), {"environment"})
            for key, value in services[name]["environment"].items():
                if key.endswith("DATABASE_URL"):
                    self.assertIn("@postgres:5432/wsl_test?", value)
                else:
                    self.assertEqual((name, key, value),
                                     ("bootstrap", "BOOTSTRAP_USERNAME", "synthetic-smoke-admin"))

    def test_main_rejects_local_execution_before_writes(self):
        with patch.dict(os.environ, {"CI": "false", "GITHUB_ACTIONS": "false"}):
            with self.assertRaisesRegex(RuntimeError, "confined"):
                SETUP.main()

    def test_private_write_refuses_overwrite_and_symlink(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "fixture"
            SETUP.private_write(path, "synthetic")
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            with self.assertRaises(FileExistsError):
                SETUP.private_write(path, "replacement")
            link = Path(folder) / "link"
            link.symlink_to(path)
            with self.assertRaises(FileExistsError):
                SETUP.private_write(link, "replacement")
            self.assertEqual(path.read_text(), "synthetic")

    def test_isolation_requires_ci_and_exact_generated_urls(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / ".env"
            keys = ("DATABASE_URL", "TENANT_DATABASE_URL", "MIGRATION_DATABASE_URL")
            body = "".join(f"{key}=postgres://synthetic:fixture@127.0.0.1:55439/wsl?sslmode=disable\n"
                           for key in keys)
            SETUP.private_write(path, body)
            with patch.dict(os.environ, {"CI": "false", "GITHUB_ACTIONS": "false"}):
                with self.assertRaises(RuntimeError):
                    SETUP.isolate_fixture_database(path)
            self.assertEqual(path.read_text(), body)
            with patch.dict(os.environ, {"CI": "true", "GITHUB_ACTIONS": "true"}):
                SETUP.isolate_fixture_database(path)
            self.assertEqual(path.read_text().count("/wsl_test?"), 3)
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)

    def test_nonlocal_or_duplicate_database_fixture_refused(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / ".env"
            body = "DATABASE_URL=postgres://synthetic@other-project/wsl?sslmode=disable\n"
            SETUP.private_write(path, body)
            with patch.dict(os.environ, {"CI": "true", "GITHUB_ACTIONS": "true"}):
                with self.assertRaises(RuntimeError):
                    SETUP.isolate_fixture_database(path)
            self.assertEqual(path.read_text(), body)

if __name__ == "__main__":
    unittest.main()
