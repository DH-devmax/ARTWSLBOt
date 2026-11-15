"""Offline mobile-search configuration tests; no provider or login calls."""

import base64
import contextlib
import io
import json
import os
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch
import uuid

import native_mobile_search_config as mobile


class NativeMobileSearchConfigTests(unittest.TestCase):
    def fixture(self):
        return {
            "origins": ["https://first.example.invalid", "https://second.example.invalid:8443"],
            "body_key": bytes(range(32)),
            "server_key": bytes(range(32, 64)),
            "app_hash": ":".join(["AB"] * 32) + "=" + "CD" * 32,
        }

    def test_deployment_contract(self):
        client = self.fixture()
        profile = mobile.deployment(client)
        self.assertEqual(set(profile), {
            "origin", "headers", "metadata", "signing_seed", "body_key", "server_key",
            "captcha_id", "app_key", "device_id", "protocol_version", "app_version", "app_hash",
        })
        self.assertEqual(profile["protocol_version"], "3.7.4")
        self.assertEqual(profile["app_version"], 337)
        self.assertEqual(profile["app_hash"], client["app_hash"])
        self.assertEqual(profile["origin"], client["origins"][0])
        self.assertEqual(profile["headers"], {
            "accept": "application/json", "content-type": "application/json",
        })
        self.assertEqual(profile["metadata"], [0] * 14)
        self.assertTrue(all(type(value) is int for value in profile["metadata"]))
        for name in ("signing_seed", "body_key", "server_key"):
            self.assertEqual(len(base64.b64decode(profile[name], validate=True)), 32)
        for name in ("body_key", "server_key"):
            self.assertEqual(base64.b64decode(profile[name]), client[name])
        self.assertEqual(profile["captcha_id"], mobile.native_client_config.CAPTCHA_ID)
        self.assertEqual(profile["app_key"], "")
        device = uuid.UUID(profile["device_id"])
        self.assertEqual(device.version, 4)
        self.assertEqual(str(device), profile["device_id"])

    def test_each_profile_has_fresh_seed_device_and_metadata(self):
        client = self.fixture()
        first, second = mobile.deployment(client), mobile.deployment(client)
        self.assertNotEqual(first["signing_seed"], second["signing_seed"])
        self.assertNotEqual(first["device_id"], second["device_id"])
        first["metadata"][0] = 123
        first["headers"]["authorization"] = "synthetic"
        self.assertEqual(second["metadata"], [0] * 14)
        self.assertNotIn("authorization", second["headers"])

    def test_signed_alternate_origin(self):
        client = self.fixture()
        self.assertEqual(mobile.deployment(client, client["origins"][1])["origin"],
                         client["origins"][1])
        with self.assertRaises(ValueError):
            mobile.deployment(client, "https://unsigned.example.invalid")

    def test_origins_are_bounded_even_when_reader_is_mocked(self):
        invalid = [
            "", "http://example.invalid", "https://user:pass@example.invalid",
            "https://example.invalid/", "https://example.invalid/path",
            "https://example.invalid?q=1", "https://example.invalid?",
            "https://example.invalid#part", "https://example.invalid#",
            "https://example.invalid\\path", "https://example.invalid\n",
            " https://example.invalid", "https://example.invalid:0",
            "https://example.invalid:65536", "https://example.invalid:bad",
            "https://", "https://[broken", "https://" + "a" * 254,
            "https://" + "a" * (mobile.MAX_ORIGIN_LENGTH + 1),
            "https://ex\u00e4mple.invalid", None, 123,
        ]
        for origin in invalid:
            with self.subTest(origin=origin):
                client = self.fixture()
                client["origins"] = [origin]
                with self.assertRaises(ValueError):
                    mobile.deployment(client)
        for origins in ([], None, "https://example.invalid", ["https://example.invalid"] * 17):
            with self.subTest(origins=origins):
                client = self.fixture()
                client["origins"] = origins
                with self.assertRaises(ValueError):
                    mobile.deployment(client)
        with self.assertRaises(ValueError):
            mobile.deployment(self.fixture(), "")

    def test_invalid_material_leaves_no_output(self):
        changes = [
            ("body_key", b"x" * 31), ("body_key", b"x" * 33), ("body_key", "x" * 32),
            ("server_key", b"x" * 31), ("server_key", None),
            ("app_hash", ""), ("app_hash", "x" * 160), ("app_hash", None),
        ]
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "private.json"
            for field, value in changes:
                with self.subTest(field=field, value=value):
                    client = self.fixture()
                    client[field] = value
                    with patch.object(mobile.native_client_config, "read_client", return_value=client):
                        printed = io.StringIO()
                        with contextlib.redirect_stdout(printed), self.assertRaises(SystemExit) as error:
                            mobile.main(["synthetic.apk", "--output", str(output)])
                    self.assertEqual(str(error.exception), mobile.FAILURE_MESSAGE)
                    self.assertEqual(printed.getvalue(), "")
                    self.assertFalse(output.exists())

    def test_cli_requires_output_before_reading_apk(self):
        with patch.object(mobile.native_client_config, "read_client") as reader:
            with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit) as error:
                mobile.main(["synthetic.apk"])
        self.assertEqual(error.exception.code, 2)
        reader.assert_not_called()

    def test_cli_writes_only_private_file_and_safe_summary(self):
        client = self.fixture()
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "private.json"
            printed, errors = io.StringIO(), io.StringIO()
            with patch.object(mobile.native_client_config, "read_client", return_value=client) as reader:
                with contextlib.redirect_stdout(printed), contextlib.redirect_stderr(errors):
                    mobile.main(["synthetic.apk", "--output", str(output),
                                 "--origin", client["origins"][1]])
            reader.assert_called_once_with(Path("synthetic.apk"))
            profile = json.loads(output.read_text())
            self.assertEqual(profile["origin"], client["origins"][1])
            self.assertEqual(stat.S_IMODE(output.stat().st_mode), 0o600)
            self.assertEqual(json.loads(printed.getvalue()), {
                "protocol_version": "3.7.4", "signed_bootstrap_verified": True,
                "private_configuration_written": True, "network_requests": 0,
            })
            self.assertEqual(errors.getvalue(), "")
            for name in ("signing_seed", "body_key", "server_key", "device_id", "app_hash", "origin"):
                self.assertNotIn(profile[name], printed.getvalue())
            self.assertNotIn(str(output), printed.getvalue())

    def test_existing_files_directories_and_symlinks_are_never_replaced(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            target = root / "existing.json"
            original = b"existing-private-configuration"
            target.write_bytes(original)
            target.chmod(0o644)
            link, dangling = root / "link.json", root / "dangling.json"
            link.symlink_to(target)
            dangling.symlink_to(root / "missing.json")
            for output in (target, link, dangling, root):
                with self.subTest(output=output):
                    with self.assertRaises(FileExistsError):
                        mobile.private_output(output, {"replacement": True})
                    self.assertEqual(target.read_bytes(), original)
                    self.assertEqual(stat.S_IMODE(target.stat().st_mode), 0o644)
                    self.assertTrue(link.is_symlink())
                    self.assertTrue(dangling.is_symlink())
                    self.assertFalse((root / "missing.json").exists())

    def test_private_permissions_do_not_depend_on_umask(self):
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "private.json"
            previous = os.umask(0o777)
            try:
                mobile.private_output(target, {"synthetic": True})
            finally:
                os.umask(previous)
            self.assertEqual(stat.S_IMODE(target.stat().st_mode), 0o600)

    def test_errors_never_print_reader_details_or_leave_partial_file(self):
        marker = "SYNTHETIC-SECRET-NOT-FOR-LOGS"
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "private.json"
            for failure in ("reader", "write"):
                with self.subTest(failure=failure):
                    printed, errors = io.StringIO(), io.StringIO()
                    with contextlib.ExitStack() as stack:
                        reader = stack.enter_context(patch.object(
                            mobile.native_client_config, "read_client", return_value=self.fixture()))
                        if failure == "reader":
                            reader.side_effect = ValueError(marker)
                        else:
                            stack.enter_context(patch.object(mobile.os, "fsync", side_effect=OSError(marker)))
                        stack.enter_context(contextlib.redirect_stdout(printed))
                        stack.enter_context(contextlib.redirect_stderr(errors))
                        with self.assertRaises(SystemExit) as error:
                            mobile.main(["synthetic.apk", "--output", str(target)])
                    self.assertEqual(str(error.exception), mobile.FAILURE_MESSAGE)
                    self.assertNotIn(marker, str(error.exception))
                    self.assertEqual(printed.getvalue() + errors.getvalue(), "")
                    self.assertFalse(target.exists())

    def test_serialization_error_creates_no_file(self):
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "private.json"
            with self.assertRaises(TypeError):
                mobile.private_output(target, {"invalid": object()})
            self.assertFalse(target.exists())

    def test_partial_writes_are_completed(self):
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "private.json"
            original_write = os.write
            with patch.object(mobile.os, "write",
                              side_effect=lambda descriptor, data: original_write(descriptor, data[:3])):
                mobile.private_output(target, {"synthetic": True})
            self.assertEqual(json.loads(target.read_text()), {"synthetic": True})

    def test_generator_never_opens_network_or_executes_client(self):
        with tempfile.TemporaryDirectory() as directory, contextlib.ExitStack() as stack:
            for name in ("socket.socket", "socket.create_connection", "subprocess.Popen", "os.system"):
                stack.enter_context(patch(name, side_effect=AssertionError("unexpected external activity")))
            stack.enter_context(patch.object(mobile.native_client_config, "read_client",
                                            return_value=self.fixture()))
            stack.enter_context(contextlib.redirect_stdout(io.StringIO()))
            output = Path(directory) / "private.json"
            mobile.main(["synthetic.apk", "--output", str(output)])
            self.assertTrue(output.is_file())

    @unittest.skipUnless(os.environ.get("WSL_CLIENT_APK"), "optional pinned APK check")
    def test_pinned_artifact_in_memory_only(self):
        client = mobile.native_client_config.read_client(os.environ["WSL_CLIENT_APK"])
        profile = mobile.deployment(client)
        self.assertEqual(profile["app_hash"], client["app_hash"])
        self.assertEqual(base64.b64decode(profile["body_key"]), client["body_key"])
        self.assertEqual(base64.b64decode(profile["server_key"]), client["server_key"])
        self.assertNotIn("message_key", profile)
        self.assertNotIn("desktop_platform", profile)


if __name__ == "__main__":
    unittest.main()
