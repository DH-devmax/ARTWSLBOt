import importlib.util
import json
import os
import shlex
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import yaml

from tools import container_smoke as SMOKE

SPEC = importlib.util.spec_from_file_location("ci_smoke_setup", Path(__file__).with_name("ci_smoke_setup.py"))
SETUP = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SETUP)


class CISetupTests(unittest.TestCase):
    def test_overlay_only_changes_test_database_and_synthetic_bootstrap(self):
        services = yaml.safe_load(SETUP.OVERLAY)["services"]
        self.assertEqual(set(services), {"postgres", "migrate", "bootstrap", "api"})
        self.assertEqual(services["postgres"]["environment"]["POSTGRES_DB"], "wsl_test")
        self.assertEqual(services["postgres"]["healthcheck"]["test"],
                         ["CMD-SHELL", "pg_isready -U wsl_owner -d wsl_test"])
        for name in ("migrate", "bootstrap", "api"):
            self.assertEqual(set(services[name]), {"environment"})
            for key, value in services[name]["environment"].items():
                if key.endswith("DATABASE_URL"):
                    self.assertIn("@postgres:5432/wsl_test?", value)
                else:
                    self.assertEqual((name, key, value),
                                     ("bootstrap", "BOOTSTRAP_USERNAME", "synthetic-smoke-admin"))

    def test_prepare_exports_private_overlay_for_later_steps_only(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            fixture = root / ".env"
            keys = ("DATABASE_URL", "TENANT_DATABASE_URL", "MIGRATION_DATABASE_URL")
            SETUP.private_write(fixture, "".join(
                f"{key}=postgres://synthetic:fixture@127.0.0.1:55439/wsl?sslmode=disable\n"
                for key in keys))
            github_env = root / "github-env"
            with patch.object(SETUP, "ROOT", root), patch.dict(os.environ, {
                "CI": "true", "GITHUB_ACTIONS": "true", "GITHUB_ENV": str(github_env),
            }, clear=True):
                SETUP.prepare()
                self.assertNotIn("COMPOSE_FILE", os.environ)
            overlay = root / "var/ci-test.compose.yaml"
            self.assertEqual(github_env.read_text(),
                             f"COMPOSE_FILE={root / 'compose.yaml'}:{overlay}\n")
            self.assertEqual(overlay.read_text(), SETUP.OVERLAY)
            self.assertEqual(overlay.stat().st_mode & 0o777, 0o600)
            self.assertEqual(fixture.read_text().count("/wsl_test?"), 3)
            self.assertEqual(fixture.stat().st_mode & 0o777, 0o600)

    def test_workflow_consumes_isolated_overlay_after_prepare_step(self):
        jobs = yaml.safe_load((SETUP.ROOT / ".github/workflows/ci.yml").read_text())["jobs"]
        for name in ("database", "browser"):
            with self.subTest(job=name):
                job = jobs[name]
                steps = job["steps"]
                commands = [
                    ((step_index, line_index), shlex.split(line, comments=True))
                    for step_index, step in enumerate(steps)
                    for line_index, line in enumerate(step.get("run", "").splitlines())
                ]
                prepare = [position for position, command in commands
                           if command == ["python3", "tools/ci_smoke_setup.py", "prepare"]]
                self.assertEqual(len(prepare), 1)
                self.assertNotIn("COMPOSE_FILE", job.get("env", {}))
                for step in steps:
                    self.assertNotIn("COMPOSE_FILE", step.get("env", {}))
                for step in steps[:prepare[0][0] + 1]:
                    self.assertNotRegex(step.get("run", ""), r"\bdocker\s+compose\b")
                start = [position for position, command in commands
                         if command[:3] == ["docker", "compose", "up"]]
                self.assertEqual(len(start), 1)
                self.assertGreater(start[0][0], prepare[0][0])
                if name == "database":
                    self.assertIn((start[0], ["docker", "compose", "up", "-d", "--wait", "postgres"]),
                                  commands)
                else:
                    bootstrap = [position for position, command in commands
                                 if command == ["docker", "compose", "--profile", "ops",
                                                "run", "--rm", "bootstrap"]]
                    session = [position for position, command in commands
                               if command == ["python3", "tools/ci_smoke_setup.py", "session"]]
                    browser = [position for position, command in commands
                               if command[:2] == ["docker", "run"]
                               and command[-1:] == ["wsl-browser-test:ci"]]
                    self.assertEqual(len(bootstrap), 1)
                    self.assertEqual(len(session), 1)
                    self.assertEqual(len(browser), 1)
                    self.assertLess(start[0], bootstrap[0])
                    self.assertLess(bootstrap[0], session[0])
                    self.assertLess(session[0], browser[0])

    def test_browser_preflight_pulls_postgres_and_selects_media_stage(self):
        job = yaml.safe_load((SETUP.ROOT / ".github/workflows/ci.yml").read_text())["jobs"]["browser"]
        self.assertEqual(job["env"]["WSL_SMOKE_EXPECT_CHILI_MEDIA"], "1")
        commands = [
            shlex.split(line, comments=True)
            for step in job["steps"]
            for line in step.get("run", "").splitlines()
        ]
        pull = ["docker", "pull", "postgres:17.6-alpine"]
        smoke = ["python3", "tools/container_smoke.py"]
        self.assertIn(pull, commands)
        self.assertIn(smoke, commands)
        self.assertLess(commands.index(pull), commands.index(smoke))

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

    def test_session_helper_keeps_private_output_owned_by_the_runner(self):
        identifier = "b7000000-0000-4000-8000-000000000001"
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / "var").mkdir()
            SETUP.private_write(root / ".env",
                                "DATABASE_URL=postgres://wsl_platform:synthetic@127.0.0.1/"
                                "wsl_test?sslmode=disable\n")
            calls = []

            def docker(*arguments):
                calls.append(arguments)
                if arguments[0] == "exec":
                    return identifier
                self.assertEqual(arguments[arguments.index("--user") + 1],
                                 f"{os.getuid()}:{os.getgid()}")
                self.assertEqual(arguments[arguments.index("--tmpfs") + 1],
                                 "/tmp:rw,exec,nosuid,nodev,size=512m")
                for setting in ("GOCACHE=/tmp/go-build", "CGO_ENABLED=0",
                                "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off",
                                "GOMODCACHE=/go/pkg/mod"):
                    self.assertIn(setting, arguments)
                environment = Path(arguments[arguments.index("--env-file") + 1])
                self.assertEqual(environment.stat().st_mode & 0o777, 0o600)
                SETUP.private_write(root / "var/ci-website-session.json", json.dumps({
                    "username": "synthetic-smoke-admin", "user_id": identifier}))
                return ""

            with patch.object(SETUP, "ROOT", root), patch.object(SETUP, "docker", docker):
                SETUP.create_session()
            self.assertEqual(len(calls), 2)
            output = root / "var/ci-website-session.json"
            self.assertEqual(output.stat().st_mode & 0o777, 0o600)
            self.assertEqual(output.stat().st_uid, os.getuid())


class NativeCatalogSmokeTests(unittest.TestCase):
    def catalog(self):
        manual = ("account_signup", "signup_sms", "signup_submit", "account_login",
                  "account_settings", "account_nickname", "account_avatar", "privacy_search",
                  "privacy_friend", "privacy_invitation", "change_password", "change_phone",
                  "approve_friend", "accept_friend", "reject_friend", "friend_delete",
                  "onboarding_friend_request", "read_voice", "read_file")
        foundation = ("receive_message", "session_restore", "send_text", "read_quote",
                      "read_recalls", "mute_member", "set_self_card", "send_voice", "send_video")
        return {"native_usage": {**dict.fromkeys(manual, "manual"),
                                 **dict.fromkeys(foundation, "foundation"),
                                 "send_file": "excluded"}, "items": [{"id": "blocked_words"}]}

    def test_frozen_catalog_without_candidate_metadata_is_verified(self):
        SMOKE.validate_native_consumer_catalog(self.catalog())

    def test_promoted_manual_excluded_or_retired_consumer_is_rejected(self):
        for capability, replacement in (("privacy_friend", "foundation"),
                                        ("account_nickname", "foundation"),
                                        ("send_file", "foundation"), ("send_voice", "excluded")):
            with self.subTest(capability=capability):
                catalog = self.catalog()
                catalog["native_usage"][capability] = replacement
                with self.assertRaises(RuntimeError):
                    SMOKE.validate_native_consumer_catalog(catalog)
        catalog = self.catalog()
        catalog["items"].append({"id": "friend_approval"})
        with self.assertRaises(RuntimeError):
            SMOKE.validate_native_consumer_catalog(catalog)

    def test_missing_or_malformed_consumer_evidence_is_rejected(self):
        for catalog in (None, {}, {"native_usage": [], "items": []},
                        {"native_usage": {}, "items": [None]}, {"native_usage": {}, "items": []}):
            with self.subTest(catalog=catalog):
                with self.assertRaises(RuntimeError):
                    SMOKE.validate_native_consumer_catalog(catalog)

    def test_default_smoke_checks_release_contract_not_unpublished_metadata(self):
        spec = importlib.util.spec_from_file_location(
            "synthetic_container_smoke", Path(SMOKE.__file__))
        module = importlib.util.module_from_spec(spec)
        with patch.dict(os.environ, {}, clear=True):
            spec.loader.exec_module(module)
        self.assertTrue(module.EXPECT_NATIVE_USAGE)
        self.assertFalse(module.EXPECT_PRODUCT_SCOPE)


if __name__ == "__main__":
    unittest.main()
