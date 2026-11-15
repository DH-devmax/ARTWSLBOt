import base64
import copy
import io
import json
import stat
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from email.message import Message
from pathlib import Path
from unittest.mock import patch
from urllib.error import HTTPError
from urllib.request import HTTPCookieProcessor

import container_smoke as smoke


def challenge():
    # Only the byte format is exercised here; the container uses real GoCaptcha images.
    png = (b"\x89PNG\r\n\x1a\n" + (13).to_bytes(4) + b"IHDR"
           + (64).to_bytes(4) * 2)
    return {
        "challenge_id": "c" * 43,
        "image": {
            "background": "data:image/jpeg;base64," + base64.b64encode(b"\xff\xd8\xff" + b"x" * 24).decode(),
            "tile": "data:image/png;base64," + base64.b64encode(png).decode(),
            "width": 300, "height": 220, "tile_width": 64, "tile_height": 64,
            "initial_x": 20, "initial_y": 80,
        },
        "expires_at": (datetime.now(timezone.utc) + timedelta(seconds=120)).isoformat(),
    }


def rejection(status=401, msg="authentication_required", headers=None):
    return HTTPError("http://127.0.0.1/api/v1/me", status, "", headers or Message(),
                     io.BytesIO(json.dumps({"code": 1, "msg": msg}).encode()))


class CaptchaSmokeTests(unittest.TestCase):
    def test_image_contract_and_private_field_rejection(self):
        value = challenge()
        smoke.validate_captcha_challenge(value)
        for where, key, value in (
                ("top", "answer", 148), ("image", "x", 148),
                ("image", "tile", "not-an-image"), ("image", "width", 1),
                ("image", "initial_y", 220), ("image", "tile_width", 63),
                ("top", "expires_at", (datetime.now(timezone.utc) - timedelta(seconds=1)).isoformat())):
            candidate = challenge()
            (candidate if where == "top" else candidate["image"])[key] = value
            with self.subTest(field=key), self.assertRaises((RuntimeError, ValueError)):
                smoke.validate_captcha_challenge(candidate)

    def test_rejection_checks_status_envelope_and_no_session_cookie(self):
        with patch.object(smoke, "json_response", side_effect=rejection()):
            smoke.rejected_response(None, "synthetic", 401, "authentication_required")
        for error in (rejection(503), rejection(msg="other_failure")):
            with patch.object(smoke, "json_response", side_effect=error):
                with self.assertRaises(RuntimeError):
                    smoke.rejected_response(None, "synthetic", 401, "authentication_required")
        headers = Message()
        headers["Set-Cookie"] = "wsl_session=synthetic; Path=/"
        with patch.object(smoke, "json_response", side_effect=rejection(headers=headers)):
            with self.assertRaises(RuntimeError):
                smoke.rejected_response(None, "synthetic", 401, "authentication_required")
        with patch.object(smoke, "json_response", return_value={}):
            with self.assertRaises(RuntimeError):
                smoke.rejected_response(None, "synthetic", 401, "authentication_required")

    def test_captcha_smoke_never_solves_or_attempts_password_login_without_rejection(self):
        with patch.object(smoke, "json_response", return_value=challenge()) as success:
            with patch.object(smoke, "rejected_response") as rejected:
                smoke.captcha_smoke("http://127.0.0.1:1234", "synthetic-smoke-admin", "synthetic-password")
        self.assertEqual(success.call_count, 1)
        self.assertTrue(success.call_args.args[1].endswith("/auth/captcha/challenge"))
        verifies = [call for call in rejected.call_args_list if call.args[1].endswith("/captcha/verify")]
        self.assertEqual(len(verifies), 2)
        self.assertEqual(verifies[0].args[5]["x"], 300)
        self.assertEqual(verifies[0].args[5]["y"], 220)
        self.assertEqual(verifies[0].args[5], verifies[1].args[5])
        logins = [call for call in rejected.call_args_list if call.args[1].endswith("/auth/login")]
        self.assertEqual([call.args[2:4] for call in logins],
                         [(400, "captcha_required"), (422, "captcha_invalid")])


class SessionSmokeTests(unittest.TestCase):
    def session(self):
        return {"user_id": "c1640000-0000-4000-8000-000000000001",
                "username": "synthetic-smoke-admin",
                "session_token": "s" * 43, "csrf_token": "c" * 43,
                "expires_at": (datetime.now(timezone.utc) + timedelta(hours=12)).isoformat()}

    def test_fixture_uses_private_files_isolated_network_and_existing_go_image(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            commands = []
            session = self.session()

            def docker(*args):
                commands.append(args)
                if "SELECT current_database()" in args:
                    return "wsl_test"
                if any("SELECT id::text" in arg for arg in args):
                    return session["user_id"]
                if args[0] == "run":
                    path = directory / "synthetic-smoke-admin.json"
                    path.touch(mode=0o600)
                    path.write_text(json.dumps(session))
                return ""

            with patch.object(smoke, "docker", side_effect=docker):
                actual = smoke.synthetic_session(directory, "synthetic-db", "synthetic-password", session["username"])
            self.assertEqual(actual, session)
            self.assertFalse((directory / "synthetic-smoke-admin.json").exists())
            env = directory / "synthetic-smoke-admin.env"
            self.assertEqual(stat.S_IMODE(env.stat().st_mode), 0o600)
            self.assertIn("@postgres:5432/wsl_test?", env.read_text())
            command = commands[-1]
            self.assertEqual(command[command.index("--network") + 1], smoke.PREFIX)
            self.assertIn(smoke.GO_IMAGE, command)
            self.assertNotIn(smoke.API_IMAGE, command)
            self.assertNotIn(session["session_token"], " ".join(command))
            self.assertNotIn("synthetic-password", " ".join(command))

    def test_fixture_refuses_unknown_identities_or_other_databases(self):
        with patch.object(smoke, "docker") as docker:
            with self.assertRaises(RuntimeError):
                smoke.synthetic_session(Path("unused"), "db", "unused", "admin")
            docker.assert_not_called()
        with patch.object(smoke, "docker", return_value="wsl") as docker:
            with self.assertRaises(RuntimeError):
                smoke.synthetic_session(Path("unused"), "db", "unused", "synthetic-smoke-admin")
            self.assertEqual(docker.call_count, 1)

    def test_helper_build_is_offline_and_does_not_change_production_images(self):
        with patch.object(smoke, "docker") as docker:
            smoke.build_session_helper(Path("/synthetic-private"))
        command = docker.call_args.args
        self.assertEqual(command[command.index("--network") + 1], "none")
        self.assertIn("GOPROXY=off", command)
        self.assertIn("-mod=readonly", command)
        self.assertIn("GOMODCACHE=/go/pkg/mod", command)
        self.assertIn("GOCACHE=/tmp/go-build", command)
        self.assertIn("--read-only", command)
        self.assertIn("./tools/smoke_session", command)
        self.assertIn(smoke.GO_IMAGE, command)
        self.assertNotIn(smoke.API_IMAGE, command)

    def test_cookie_scope_identity_csrf_rejection_and_logout_replay(self):
        session = self.session()
        opener = smoke.session_opener("http://127.0.0.1:1234", session)
        processor = next(h for h in opener.handlers if isinstance(h, HTTPCookieProcessor))
        cookies = {cookie.name: cookie for cookie in processor.cookiejar}
        self.assertEqual(set(cookies), {"wsl_session", "wsl_csrf"})
        self.assertEqual(cookies["wsl_session"].domain, "127.0.0.1")
        self.assertEqual(cookies["wsl_session"].path, "/")
        self.assertTrue(cookies["wsl_session"].has_nonstandard_attr("HttpOnly"))
        self.assertFalse(cookies["wsl_csrf"].has_nonstandard_attr("HttpOnly"))
        me = {"user": {"id": session["user_id"], "username": session["username"], "role": "superadmin"}}
        with patch.object(smoke, "json_response", return_value=me):
            with patch.object(smoke, "rejected_response") as reject:
                smoke.session_auth_smoke(opener, "synthetic", session, "superadmin")
                self.assertEqual(reject.call_count, 3)
        wrong = copy.deepcopy(me)
        wrong["user"]["id"] = "another-user"
        with patch.object(smoke, "json_response", return_value=wrong):
            with self.assertRaises(RuntimeError):
                smoke.session_auth_smoke(opener, "synthetic", session, "superadmin")
        with patch.object(smoke, "json_response", return_value={"logged_out": True}):
            with patch.object(smoke, "rejected_response") as reject:
                smoke.logout_smoke(opener, "http://127.0.0.1:1234", session)
                self.assertEqual(reject.call_count, 2)
                self.assertIsNot(reject.call_args_list[0].args[0], reject.call_args_list[1].args[0])


if __name__ == "__main__":
    unittest.main()
