"""Exercise hardened local images with a disposable database and no native accounts."""

import base64
import http.cookiejar
import ipaddress
import json
import os
import secrets
import stat
import subprocess
import tempfile
import threading
import time
from html.parser import HTMLParser
from datetime import datetime, timezone
from pathlib import Path
from urllib.error import HTTPError
from urllib.parse import urlsplit
from urllib.request import HTTPCookieProcessor, Request, build_opener, urlopen

ROOT = Path(__file__).resolve().parents[1]
API_IMAGE = os.environ.get("WSL_SMOKE_API_IMAGE", "wsl-api:local")
WEB_IMAGE = os.environ.get("WSL_SMOKE_WEB_IMAGE", "wsl-web:local")
GO_IMAGE = os.environ.get("WSL_SMOKE_GO_IMAGE", "wsl-test:ci")
EXPECT_CHILI_MEDIA = os.environ.get("WSL_SMOKE_EXPECT_CHILI_MEDIA") == "1"
EXPECT_ASSISTANT_PLATFORMS = os.environ.get("WSL_SMOKE_EXPECT_ASSISTANT_PLATFORMS") == "1"
# Optional metadata from the separate, unpublished lightweight candidate.
EXPECT_PRODUCT_SCOPE = os.environ.get("WSL_SMOKE_EXPECT_PRODUCT_SCOPE") == "1"
EXPECT_PROJECT_MONITOR = os.environ.get("WSL_SMOKE_EXPECT_PROJECT_MONITOR") == "1"
EXPECT_NATIVE_USAGE = os.environ.get("WSL_SMOKE_EXPECT_NATIVE_USAGE", "1") == "1"
EXPECT_CUSTOMER = os.environ.get("WSL_SMOKE_EXPECT_CUSTOMER") == "1"
EXPECT_OPERATOR = os.environ.get("WSL_SMOKE_EXPECT_OPERATOR") == "1"
MONITOR_SOAK_SECONDS = int(os.environ.get("WSL_SMOKE_MONITOR_SOAK_SECONDS", "0"))
PREFIX = "wsl-smoke-" + secrets.token_hex(6)
CONTAINERS = []
REDACTIONS = []


def validate_native_consumer_catalog(catalog):
    if not isinstance(catalog, dict):
        raise RuntimeError("Native consumer catalog is incomplete")
    usage = catalog.get("native_usage")
    items = catalog.get("items")
    if (not isinstance(usage, dict) or not isinstance(items, list)
            or any(not isinstance(item, dict) or not isinstance(item.get("id"), str)
                   for item in items)):
        raise RuntimeError("Native consumer catalog is incomplete")
    manual = ("account_signup", "signup_sms", "signup_submit", "account_login",
              "account_settings", "account_nickname", "account_avatar", "privacy_search",
              "privacy_friend", "privacy_invitation", "change_password", "change_phone",
              "approve_friend", "accept_friend", "reject_friend", "friend_delete",
              "onboarding_friend_request", "read_voice", "read_file")
    foundation = ("receive_message", "session_restore", "send_text", "read_quote",
                  "read_recalls", "mute_member", "set_self_card", "send_voice", "send_video")
    if (any(usage.get(key) != "manual" for key in manual)
            or any(usage.get(key) != "foundation" for key in foundation)
            or usage.get("send_file") != "excluded"
            or any(item["id"] == "friend_approval" for item in items)):
        raise RuntimeError("Native consumer boundary drifted")


def docker(*args):
    result = subprocess.run(["docker", *args], capture_output=True, text=True, timeout=150)
    if result.returncode:
        message = result.stderr.strip()
        for value in REDACTIONS:
            message = message.replace(value, "[redacted]")
        raise RuntimeError("Docker smoke command failed: " + message[:2000])
    return result.stdout.strip()


def environment(directory, name, values):
    path = directory / name
    fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
    with os.fdopen(fd, "w") as handle:
        handle.write("".join(f"{key}={value}\n" for key, value in values.items()))
    return str(path)


def run_container(name, *args):
    identifier = docker("run", "-d", "--name", name, "--label", f"wsl.smoke={PREFIX}", *args)
    CONTAINERS.append(identifier)
    return identifier


def json_response(opener, url, method="GET", body=None, origin=None, csrf=None, workspace=None,
                  idempotency=None):
    headers = {"Content-Type": "application/json"}
    if origin:
        headers["Origin"] = origin
    if csrf:
        headers["X-CSRF-Token"] = csrf
    if workspace:
        headers["X-Workspace"] = workspace
    if idempotency:
        headers["Idempotency-Key"] = idempotency
    request = Request(url, method=method, headers=headers, data=None if body is None else json.dumps(body).encode())
    with opener.open(request, timeout=5) as response:
        result = json.load(response)
        if result.get("code") != 0:
            raise RuntimeError("API did not return success")
        return result["data"]


def rejected_response(opener, url, status, message, method="GET", body=None,
                      origin=None, csrf=None):
    try:
        json_response(opener, url, method, body, origin, csrf)
    except HTTPError as error:
        with error:
            result = json.load(error)
            if (error.code != status or result.get("code") == 0
                    or result.get("msg") != message or result.get("data") is not None
                    or any(cookie.startswith(("wsl_session=", "wsl_csrf="))
                           for cookie in error.headers.get_all("Set-Cookie", []))):
                raise RuntimeError("Authentication rejection contract changed") from None
    else:
        raise RuntimeError("Authentication request was unexpectedly admitted")


def validate_captcha_challenge(challenge):
    if (set(challenge) != {"challenge_id", "image", "expires_at"}
            or not isinstance(challenge["challenge_id"], str)
            or len(challenge["challenge_id"]) != 43):
        raise RuntimeError("Captcha challenge format changed or exposed private fields")
    image = challenge["image"]
    fields = {"background", "tile", "width", "height", "tile_width", "tile_height",
              "initial_x", "initial_y"}
    if not isinstance(image, dict) or set(image) != fields:
        raise RuntimeError("Captcha image format changed or exposed an answer")
    if (any(type(image[key]) is not int for key in fields - {"background", "tile"})
            or (image["width"], image["height"]) != (300, 220)
            or not 0 < image["tile_width"] < image["width"]
            or not 0 < image["tile_height"] < image["height"]
            or not 0 <= image["initial_x"] <= image["width"] - image["tile_width"]
            or not 0 <= image["initial_y"] <= image["height"] - image["tile_height"]):
        raise RuntimeError("Captcha image dimensions or initial position changed")
    for key, mime, magic in (("background", "jpeg", b"\xff\xd8\xff"),
                             ("tile", "png", b"\x89PNG\r\n\x1a\n")):
        prefix = f"data:image/{mime};base64,"
        encoded = image[key]
        if not isinstance(encoded, str) or not encoded.startswith(prefix):
            raise RuntimeError("Captcha image is not a data URL")
        try:
            raw = base64.b64decode(encoded[len(prefix):], validate=True)
        except ValueError:
            raise RuntimeError("Captcha image is not valid base64") from None
        if not raw.startswith(magic) or len(raw) < 24:
            raise RuntimeError("Captcha image bytes do not match their declared format")
        if key == "tile" and (raw[12:16] != b"IHDR" or
                (int.from_bytes(raw[16:20]), int.from_bytes(raw[20:24])) !=
                (image["tile_width"], image["tile_height"])):
            raise RuntimeError("Captcha tile dimensions do not match its PNG")
    expires = datetime.fromisoformat(challenge["expires_at"].replace("Z", "+00:00"))
    if not 0 < (expires - datetime.now(timezone.utc)).total_seconds() <= 130:
        raise RuntimeError("Captcha expiry differs from the bounded challenge TTL")


def captcha_smoke(base, username, password):
    opener = build_opener(HTTPCookieProcessor(http.cookiejar.CookieJar()))
    rejected_response(opener, base + "/api/v1/me", 401, "authentication_required")
    rejected_response(opener, base + "/api/v1/auth/login", 400, "captcha_required", "POST",
                      {"username": username, "password": password}, "http://smoke.local")
    challenge = json_response(opener, base + "/api/v1/auth/captcha/challenge", "POST",
                              {"username": username}, "http://smoke.local")
    validate_captcha_challenge(challenge)
    # Outside both image edges: no answer extraction, guessing or solving.
    verification = {"username": username, "challenge_id": challenge["challenge_id"],
                    "x": challenge["image"]["width"], "y": challenge["image"]["height"]}
    for _ in range(2):
        rejected_response(opener, base + "/api/v1/auth/captcha/verify", 422, "captcha_invalid",
                          "POST", verification, "http://smoke.local")
    rejected_response(opener, base + "/api/v1/auth/login", 422, "captcha_invalid", "POST",
                      {"username": username, "password": password,
                       "captcha_ticket": secrets.token_urlsafe(32)}, "http://smoke.local")
    rejected_response(opener, base + "/api/v1/me", 401, "authentication_required")


def build_session_helper(directory):
    # Reuse the test image's locked module cache, never the production image.
    docker("run", "--rm", "--network", "none", "--read-only", "--cap-drop", "ALL",
           "--security-opt", "no-new-privileges:true", "--user", f"{os.getuid()}:{os.getgid()}",
           "--mount", f"type=bind,source={ROOT},target=/src,readonly",
           "--mount", f"type=bind,source={directory},target=/smoke-output",
           "--tmpfs", "/tmp:rw,size=512m,nosuid", "--workdir", "/src",
           "--env", "CGO_ENABLED=0", "--env", "GOTOOLCHAIN=local", "--env", "GOPROXY=off",
           "--env", "GOSUMDB=off", "--env", "GOCACHE=/tmp/go-build",
           "--env", "GOMODCACHE=/go/pkg/mod", "--entrypoint", "go", GO_IMAGE,
           "build", "-mod=readonly", "-trimpath", "-o", "/smoke-output/smoke-session",
           "./tools/smoke_session")


def synthetic_session(directory, db, platform_password, username):
    if username not in ("synthetic-smoke-admin", "synthetic-customer"):
        raise RuntimeError("Unsupported synthetic smoke identity")
    if docker("exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
              "SELECT current_database()") != "wsl_test":
        raise RuntimeError("Session fixtures require the fresh wsl_test database")
    user_id = docker("exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
                     f"SELECT id::text FROM users WHERE username='{username}'")
    env = environment(directory, username + ".env", {
        "DATABASE_URL": f"postgres://wsl_platform:{platform_password}@postgres:5432/wsl_test?sslmode=disable",
        "WSL_SMOKE_SESSION_USERNAME": username,
        "WSL_SMOKE_SESSION_USER_ID": user_id,
    })
    output = directory / (username + ".json")
    docker("run", "--rm", "--network", PREFIX, "--env-file", env, "--read-only",
           "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
           "--user", f"{os.getuid()}:{os.getgid()}",
           "--mount", f"type=bind,source={directory},target=/smoke-output",
           "--entrypoint", "/smoke-output/smoke-session", GO_IMAGE,
           "/smoke-output/" + output.name)
    if stat.S_IMODE(output.stat().st_mode) != 0o600:
        raise RuntimeError("Synthetic session output must be private")
    session = json.loads(output.read_text())
    output.unlink()
    if (session.get("username") != username or session.get("user_id") != user_id
            or any(not isinstance(session.get(key), str) or len(session[key]) != 43
                   for key in ("session_token", "csrf_token"))):
        raise RuntimeError("Synthetic session identity or token format changed")
    REDACTIONS.extend((session["session_token"], session["csrf_token"]))
    return session


def session_opener(base, session):
    jar = http.cookiejar.CookieJar()
    for name, key in (("wsl_session", "session_token"), ("wsl_csrf", "csrf_token")):
        rest = {"SameSite": "Strict"}
        if name == "wsl_session":
            rest["HttpOnly"] = None
        jar.set_cookie(http.cookiejar.Cookie(
            version=0, name=name, value=session[key], port=None, port_specified=False,
            domain=urlsplit(base).hostname, domain_specified=False, domain_initial_dot=False,
            path="/", path_specified=True, secure=False,
            expires=int(datetime.fromisoformat(session["expires_at"].replace("Z", "+00:00")).timestamp()),
            discard=False, comment=None, comment_url=None, rest=rest, rfc2109=False))
    return build_opener(HTTPCookieProcessor(jar))


def session_auth_smoke(opener, base, session, role):
    me = json_response(opener, base + "/api/v1/me")
    if any(me["user"].get(key) != value for key, value in (
            ("id", session["user_id"]), ("username", session["username"]), ("role", role))):
        raise RuntimeError("Synthetic session resolved another website identity or role")
    for origin, csrf in (("http://smoke.local", None),
                         ("http://smoke.local", "synthetic-invalid-csrf"),
                         ("http://untrusted.invalid", session["csrf_token"])):
        rejected_response(opener, base + "/api/v1/auth/logout", 403, "csrf_rejected",
                          "POST", {}, origin, csrf)
    return me


def logout_smoke(opener, base, session):
    replay = session_opener(base, session)
    result = json_response(opener, base + "/api/v1/auth/logout", "POST", {},
                           "http://smoke.local", session["csrf_token"])
    if result != {"logged_out": True}:
        raise RuntimeError("Website logout contract changed")
    for client in (opener, replay):
        rejected_response(client, base + "/api/v1/me", 401, "authentication_required")


class Scripts(HTMLParser):
    def __init__(self):
        super().__init__()
        self.sources = []

    def handle_starttag(self, tag, attrs):
        values = dict(attrs)
        if tag == "script" and values.get("src", "").startswith("/assets/"):
            self.sources.append(values["src"])


def validate_monitor_observation(result):
    rows, seconds = result["rows"], result["seconds"]
    if result.get("stream_errors"):
        raise RuntimeError("Monitor stream failed during observation")
    if seconds < 30:
        return
    counts = rows[-1]["api"]["collection_count"] - rows[0]["api"]["collection_count"]
    if abs(counts - seconds/5) > 3:
        raise RuntimeError("Shared sampler frequency drifted")
    for service in ("api", "heatbot"):
        resources = [row["api"] if service == "api" else row["heatbot"]["resources"] for row in rows]
        memories = [sample["process_rss_bytes"] for sample in resources]
        limit = min(sample["memory_limit_bytes"] for sample in resources)
        # Startup allocation release is not growth; check the full-run quota and
        # post-startup growth separately.
        stable = [sample for row, sample in zip(rows, resources)
                  if seconds < 300 or row["elapsed"] >= 120]
        if max(memories) >= limit or max(sample["process_rss_bytes"] for sample in stable) > stable[0]["process_rss_bytes"] + 64*1024**2:
            raise RuntimeError("Monitoring memory exceeded observation bounds")
        goroutines = [sample["go_goroutines"] for sample in resources]
        if max(goroutines) > min(goroutines) + 30:
            raise RuntimeError("Monitoring goroutines exceeded observation bounds")
    if any(row["monitor_pool"]["maximum"] != 1 for row in rows):
        raise RuntimeError("Monitoring pool grew")
    if len({row["database_stats"]["sampled_at"] for row in rows}) > seconds//60 + 2:
        raise RuntimeError("Database statistics frequency grew")
    if any(frames < seconds/2 - seconds/100 - 4 for frames in result["stream_frames"]):
        raise RuntimeError("Monitor stream stopped delivering")
    if any(gap > 5 for gap in result["stream_max_gap_seconds"]):
        raise RuntimeError("Monitor reconnect gap exceeded limit")


def main():
    # Fresh credentials never leave this disposable network or the temporary 0600 files.
    owner, platform, tenant, password = [secrets.token_urlsafe(32) for _ in range(4)]
    master = base64.b64encode(secrets.token_bytes(32)).decode()
    REDACTIONS.extend((owner, platform, tenant, password, master))
    for image in (API_IMAGE, WEB_IMAGE, GO_IMAGE, "postgres:17.6-alpine"):
        docker("image", "inspect", image, "--format", "{{.Id}}")
    network_options = []
    if subnet := os.environ.get("WSL_SMOKE_SUBNET"):
        network = ipaddress.ip_network(subnet, strict=True)
        if network.version != 4 or not network.is_private or not 24 <= network.prefixlen <= 28:
            raise RuntimeError("WSL_SMOKE_SUBNET must be a private IPv4 /24 to /28")
        network_options = ["--subnet", str(network)]
    docker("network", "create", *network_options, "--label", f"wsl.smoke={PREFIX}", PREFIX)
    try:
        with tempfile.TemporaryDirectory(prefix="wsl-container-smoke-") as temporary:
            directory = Path(temporary)
            build_session_helper(directory)
            db_env = environment(directory, "database.env", {
                "POSTGRES_DB": "wsl_test",
                "POSTGRES_USER": "wsl_owner",
                "POSTGRES_PASSWORD": owner,
                "WSL_PLATFORM_PASSWORD": platform,
                "WSL_TENANT_PASSWORD": tenant,
            })
            db = run_container(PREFIX + "-db", "--network", PREFIX, "--network-alias", "postgres",
                               "--env-file", db_env, "--tmpfs", "/var/lib/postgresql/data:rw,size=512m",
                               "--mount", f"type=bind,source={ROOT / 'deploy/init-db.sql'},target=/docker-entrypoint-initdb.d/10-roles.sql,readonly",
                               "postgres:17.6-alpine")
            deadline = time.monotonic() + 40
            while True:
                ready = subprocess.run(["docker", "exec", db, "pg_isready", "-h", "127.0.0.1", "-U", "wsl_owner", "-d", "wsl_test"],
                                       capture_output=True, timeout=5)
                if ready.returncode == 0:
                    break
                if time.monotonic() > deadline:
                    raise RuntimeError("Fresh PostgreSQL startup timed out")
                time.sleep(0.5)
            roles = docker("exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
                           "SELECT rolname||':'||rolsuper FROM pg_roles WHERE rolname IN ('wsl_platform','wsl_tenant') ORDER BY rolname")
            if roles.splitlines() != ["wsl_platform:false", "wsl_tenant:false"]:
                raise RuntimeError("Fresh database did not create restricted runtime roles")
            owner_env = environment(directory, "owner.env", {
                "MIGRATION_DATABASE_URL": f"postgres://wsl_owner:{owner}@postgres:5432/wsl_test?sslmode=disable",
                "DATABASE_URL": f"postgres://wsl_platform:{platform}@postgres:5432/wsl_test?sslmode=disable",
                "TENANT_DATABASE_URL": f"postgres://wsl_tenant:{tenant}@postgres:5432/wsl_test?sslmode=disable",
                "MASTER_KEY": master,
                "BOOTSTRAP_USERNAME": "synthetic-smoke-admin",
                "BOOTSTRAP_PASSWORD": password,
            })
            for command in ("migrate", "bootstrap"):
                docker("run", "--rm", "--network", PREFIX, "--env-file", owner_env,
                       "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
                       API_IMAGE, command)
            signup_schema = docker("exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
                                   "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='007_native_signup.sql')"
                                   " AND NOT has_table_privilege('wsl_tenant','native_signup_attempts','SELECT')")
            if signup_schema != "t":
                raise RuntimeError("Native signup migration or tenant isolation is missing")
            text_schema = docker("exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
                                 "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='019_native_text_routes.sql')"
                                 " AND NOT has_table_privilege('wsl_tenant','native_text_routes','SELECT')")
            if text_schema != "t":
                raise RuntimeError("Native text routing evidence migration or tenant isolation is missing")
            settings_schema = docker("exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
                                     "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='021_native_settings.sql')"
                                     " AND NOT has_table_privilege('wsl_tenant','native_setting_operations','SELECT')")
            if settings_schema != "t":
                raise RuntimeError("Native settings operation migration or tenant isolation is missing")
            security_schema = docker("exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
                                     "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='022_native_security.sql')"
                                     " AND NOT has_table_privilege('wsl_tenant','native_security_flows','SELECT')"
                                     " AND (SELECT relrowsecurity AND relforcerowsecurity FROM pg_class"
                                     " WHERE oid='native_security_flows'::regclass)")
            if security_schema != "t":
                raise RuntimeError("Native security flow migration or forced tenant isolation is missing")
            media_schema = docker("exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
                                  "SELECT EXISTS(SELECT 1 FROM schema_migrations"
                                  " WHERE name IN('041_lajiaoliao_media_attempts.sql','046_chili_outbound_attempts.sql'))")
            if (media_schema == "t") != EXPECT_CHILI_MEDIA:
                raise RuntimeError("Chili media migration differs from the selected release stage")
            if EXPECT_CHILI_MEDIA:
                media_isolation = docker("exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
                                         "SELECT NOT has_table_privilege('wsl_tenant','lajiaoliao_media_attempts','SELECT')"
                                         " AND (SELECT relrowsecurity AND relforcerowsecurity FROM pg_class"
                                         " WHERE oid='lajiaoliao_media_attempts'::regclass)")
                if media_isolation != "t":
                    raise RuntimeError("Chili media attempt isolation is missing")
            social_schema = docker("exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
                                   "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='023_native_social.sql')"
                                   " AND NOT has_table_privilege('wsl_tenant','native_setting_operations','SELECT')"
                                   " AND (SELECT relrowsecurity AND relforcerowsecurity FROM pg_class"
                                   " WHERE oid='native_setting_operations'::regclass)"
                                   " AND (SELECT pg_get_constraintdef(oid) LIKE '%friend_approve%'"
                                   " AND pg_get_constraintdef(oid) LIKE '%group_leave%' FROM pg_constraint"
                                   " WHERE conname='native_setting_operations_kind_check')")
            if social_schema != "t":
                raise RuntimeError("Native social migration or forced tenant isolation is missing")
            tools_schema = docker("exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
                                  "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='024_native_test_tools.sql')"
                                  " AND NOT has_table_privilege('wsl_tenant','native_setting_operations','SELECT')"
                                  " AND EXISTS(SELECT 1 FROM information_schema.columns"
                                  " WHERE table_name='native_setting_operations' AND column_name='test_window_id')"
                                  " AND (SELECT pg_get_constraintdef(oid) LIKE '%mute_member%'"
                                  " AND pg_get_constraintdef(oid) LIKE '%publish_notice%' FROM pg_constraint"
                                  " WHERE conname='native_setting_operations_kind_check')")
            if tools_schema != "t":
                raise RuntimeError("Controlled Tools migration or isolation is missing")
            commercial_schema = docker("exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
                                       "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='027_ai_gateway.sql')"
                                       " AND NOT has_table_privilege('wsl_tenant','ai_calls','SELECT')"
                                       " AND NOT has_table_privilege('wsl_tenant','member_groups','SELECT')"
                                       " AND (SELECT count(*)=2 FROM commercial_plans)"
                                       " AND EXISTS(SELECT 1 FROM schema_migrations WHERE name='028_heat_gateway_grant_lifecycle.sql')"
                                       " AND EXISTS(SELECT 1 FROM schema_migrations WHERE name='029_native_message_media.sql')"
                                       " AND EXISTS(SELECT 1 FROM schema_migrations WHERE name='030_gateway_unpriced_drafts.sql')"
                                       " AND EXISTS(SELECT 1 FROM schema_migrations WHERE name='034_platform_assistant.sql')"
                                       " AND NOT has_table_privilege('wsl_tenant','platform_assistant_turns','SELECT')"
                                       " AND (SELECT relrowsecurity AND relforcerowsecurity FROM pg_class"
                                       " WHERE oid='platform_assistant_turns'::regclass)"
                                       " AND NOT has_table_privilege('wsl_tenant','native_media_uploads','SELECT')"
                                       " AND (SELECT relrowsecurity AND relforcerowsecurity FROM pg_class"
                                       " WHERE oid='native_media_uploads'::regclass)"
                                       " AND (SELECT confdeltype='c' FROM pg_constraint"
                                       " WHERE conname='heat_gateway_grants_account_id_fkey')"
                                       " AND (SELECT relrowsecurity AND relforcerowsecurity FROM pg_class"
                                       " WHERE oid='ai_wallets'::regclass)")
            if commercial_schema != "t":
                raise RuntimeError("Commercial gateway migration or forced isolation is missing")
            private_schema = docker("exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
                                    "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='031_native_private_messages.sql')"
                                    " AND EXISTS(SELECT 1 FROM pg_policy WHERE polrelid='operations'::regclass"
                                    " AND polname='private_operation_scope' AND NOT polpermissive)"
                                    " AND EXISTS(SELECT 1 FROM information_schema.columns"
                                    " WHERE table_name='native_media_uploads' AND column_name='conversation_key')"
                                    " AND EXISTS(SELECT 1 FROM information_schema.columns"
                                    " WHERE table_name='native_text_routes' AND column_name='private_peer')"
                                    " AND NOT has_table_privilege('wsl_tenant','native_text_routes','SELECT')")
            if private_schema != "t":
                raise RuntimeError("Private message scope migration or restrictive isolation is missing")
            platform_schema = docker("exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
                                     "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='020_multi_platform.sql')"
                                     " AND NOT has_table_privilege('wsl_tenant','lajiaoliao_auth_state','SELECT')"
                                     " AND NOT has_table_privilege('wsl_tenant','lajiaoliao_cursors','SELECT')")
            if platform_schema != "t":
                raise RuntimeError("Platform migration or session/cursor isolation is missing")
            chili_signup_schema = docker(
                "exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
                "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='032_lajiaoliao_signup.sql')"
                " AND (SELECT count(*)=7 FROM information_schema.columns"
                " WHERE table_schema='public' AND table_name='lajiaoliao_auth_state' AND column_name IN"
                " ('sms_phone_sealed','sms_actor_id','sms_session_hash','sms_expires_at',"
                " 'sms_resend_at','sms_attempts','sms_phone_fingerprint'))"
                " AND (SELECT relrowsecurity AND relforcerowsecurity FROM pg_class"
                " WHERE oid='lajiaoliao_auth_state'::regclass)"
                " AND NOT has_table_privilege('wsl_tenant','lajiaoliao_auth_state','SELECT')")
            if chili_signup_schema != "t":
                raise RuntimeError("Chili SMS flow migration or forced isolation is missing")
            if EXPECT_ASSISTANT_PLATFORMS:
                assistant_schema = docker(
                    "exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
                    "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='042_assistant_platforms.sql')"
                    " AND EXISTS(SELECT 1 FROM schema_migrations WHERE name='043_assistant_action_plans.sql')"
                    " AND EXISTS(SELECT 1 FROM schema_migrations WHERE name='044_assistant_message_attempts.sql')"
                    " AND NOT has_table_privilege('wsl_tenant','assistant_message_attempts','SELECT')"
                    " AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='assistant_message_attempts'::regclass"
                    " AND tgname='assistant_attempt_immutable' AND NOT tgisinternal)"
                    " AND (SELECT relrowsecurity AND relforcerowsecurity FROM pg_class"
                    " WHERE oid='assistant_action_plans'::regclass)"
                    " AND (SELECT relrowsecurity AND relforcerowsecurity FROM pg_class"
                    " WHERE oid='assistant_target_choices'::regclass)"
                    " AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgrelid='assistant_sessions'::regclass"
                    " AND NOT tgisinternal)")
                if assistant_schema != "t":
                    raise RuntimeError("Assistant platform migration or forced plan isolation is missing")
            if EXPECT_CUSTOMER:
                customer_schema = docker(
                    "exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
                    "SELECT count(*)=3 FROM schema_migrations WHERE name LIKE '050_%'"
                    " OR name LIKE '051_%' OR name LIKE '052_%'")
                isolation = docker(
                    "exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
                    "SELECT count(*)=7 AND bool_and(c.relrowsecurity AND c.relforcerowsecurity"
                    " AND NOT has_table_privilege('wsl_tenant',c.oid,'SELECT'))"
                    " FROM pg_class c WHERE c.oid=ANY(ARRAY["
                    "'customer_invitations'::regclass,'group_binding_challenges'::regclass,"
                    "'customer_group_accept_attempts'::regclass,'tenant_capability_ceilings'::regclass,"
                    "'business_capability_gates'::regclass,'group_business_runtime'::regclass,"
                    "'business_rule_jobs'::regclass])")
                if customer_schema != "t" or isolation != "t":
                    raise RuntimeError("Customer migrations or control-owned table isolation is missing")
            if EXPECT_OPERATOR:
                operator_schema = docker(
                    "exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
                    "SELECT EXISTS(SELECT 1 FROM schema_migrations"
                    " WHERE name='053_operator_demo_and_manual_plans.sql')"
                    " AND (SELECT count(*)=2 AND bool_and(c.relrowsecurity AND c.relforcerowsecurity"
                    " AND NOT has_table_privilege('wsl_tenant',c.oid,'SELECT'))"
                    " FROM pg_class c WHERE c.oid=ANY(ARRAY["
                    "'manual_action_plans'::regclass,'manual_message_attempts'::regclass]))")
                if operator_schema != "t":
                    raise RuntimeError("Operator migration or manual-plan isolation is missing")
            # Docker allocates an unused localhost port; no other project's server is replaced.
            web_name = PREFIX + "-web"
            api_env = environment(directory, "api.env", {
                "DATABASE_URL": f"postgres://wsl_platform:{platform}@postgres:5432/wsl_test?sslmode=disable",
                "TENANT_DATABASE_URL": f"postgres://wsl_tenant:{tenant}@postgres:5432/wsl_test?sslmode=disable",
                "MASTER_KEY": master,
                "HTTP_ADDR": ":8080",
                "PUBLIC_ORIGIN": "http://smoke.local",
                "COOKIE_SECURE": "false",
                "ENABLE_NATIVE": "false",
            })
            api = run_container(PREFIX + "-api", "--network", PREFIX, "--network-alias", "api",
                                "--env-file", api_env, "--read-only", "--cap-drop", "ALL",
                                "--security-opt", "no-new-privileges:true",
                                "--cpus", "1", "--memory", "512m", "--memory-swap", "512m", API_IMAGE)
            bridge = secrets.token_urlsafe(32)
            REDACTIONS.append(bridge)
            heat_env = environment(directory, "heatbot.env", {
                "AI_BRIDGE_TOKEN": bridge,
                "AI_ALLOW_TEST_GENERATION": "false",
                "AI_PROVIDER_ORIGINS": "",
            })
            heat = run_container(PREFIX + "-heatbot", "--network", PREFIX,
                                 "--network-alias", "heatbot",
                                 "--env-file", heat_env, "--read-only", "--cap-drop", "ALL",
                                 "--security-opt", "no-new-privileges:true",
                                 "--cpus", "0.5", "--memory", "256m", "--memory-swap", "256m",
                                 "--entrypoint", "heatbot", API_IMAGE)
            web = run_container(web_name, "--network", PREFIX, "--read-only", "--cap-drop", "ALL",
                                "--network-alias", "web",
                                "--security-opt", "no-new-privileges:true",
                                "--tmpfs", "/tmp:rw,size=64m,noexec,nosuid",
                                "-p", "127.0.0.1::8080", WEB_IMAGE)
            # Docker Desktop may report the container before publishing its port.
            address = ""
            for _ in range(30):
                ports = json.loads(docker("inspect", "--format", "{{json .NetworkSettings.Ports}}", web)) or {}
                bindings = ports.get("8080/tcp") or []
                if len(bindings) == 1:
                    address = bindings[0]["HostIp"] + ":" + bindings[0]["HostPort"]
                if address:
                    break
                time.sleep(0.1)
            if not address.startswith("127.0.0.1:") or "\n" in address:
                raise RuntimeError("Unexpected smoke port mapping")
            base = "http://" + address
            deadline = time.monotonic() + 30
            while True:
                try:
                    with urlopen(base + "/healthz", timeout=2) as response:
                        result = json.load(response)
                        health = result.get("data") if result.get("code") == 0 else None
                    if health != {"status": "ok", "native_ready": False, "ai_ready": False}:
                        raise RuntimeError("Compatibility gates changed unexpectedly")
                    break
                except (OSError, ValueError):
                    if time.monotonic() > deadline:
                        raise RuntimeError("Hardened API/Web startup timed out")
                    time.sleep(0.3)
            for identifier in (api, web, heat):
                info = json.loads(docker("inspect", identifier))[0]
                if info["Config"]["User"] in ("", "0", "root") or not info["HostConfig"]["ReadonlyRootfs"]:
                    raise RuntimeError("Runtime user/read-only configuration is not hardened")
            engine_health = json.loads(docker("exec", heat, "wget", "-q", "-O", "-",
                                             "http://127.0.0.1:8090/healthz"))
            if engine_health != {"engine": "go", "parity_verified": False, "native_sender": False}:
                raise RuntimeError("Go text engine raised parity or native execution gates")
            with urlopen(base, timeout=5) as response:
                parser = Scripts()
                parser.feed(response.read().decode())
            if not parser.sources:
                raise RuntimeError("Built product script is missing")
            for asset in ("/wsl-logo.png", "/lajiaoliao-logo.png", "/maibot-logo.png"):
                with urlopen(base + asset, timeout=5) as response:
                    if response.status != 200 or not response.read().startswith(b"\x89PNG\r\n\x1a\n"):
                        raise RuntimeError("Workspace brand asset is not readable: " + asset)
            for path in parser.sources:
                with urlopen(base + path, timeout=5) as response:
                    if response.status != 200 or "javascript" not in response.headers.get("Content-Type", ""):
                        raise RuntimeError("Built script did not load")
            captcha_smoke(base, "synthetic-smoke-admin", password)
            login = synthetic_session(directory, db, platform, "synthetic-smoke-admin")
            opener = session_opener(base, login)
            me = session_auth_smoke(opener, base, login, "superadmin")
            for workspace in ("wsl", "lajiaoliao") if EXPECT_NATIVE_USAGE else ():
                catalog = json_response(opener, base + "/api/v1/features/catalog", workspace=workspace)
                validate_native_consumer_catalog(catalog)
            for workspace in ("wsl", "lajiaoliao") if EXPECT_PRODUCT_SCOPE else ():
                catalog = json_response(opener, base + "/api/v1/features/catalog", workspace=workspace)
                entries = {item["id"]: item for item in
                           catalog["native_capabilities"] + catalog["native_details"]}
                excluded = ["account_signup", "send_file", "privacy_friend"]
                if workspace == "lajiaoliao":
                    excluded.append("profile_signature")
                for capability in excluded:
                    if entries[capability]["product_scope"] != "excluded" or not entries[capability]["scope_reason"]:
                        raise RuntimeError("Retired native capability entered the product scope")
                for capability in ("send_voice", "send_video"):
                    if entries[capability]["product_scope"] != "included":
                        raise RuntimeError("Requested native media capability was retired")
            platform_assistant = json_response(opener, base + "/api/v1/platform-assistant")
            if (platform_assistant["settings"]["enabled"]
                    or platform_assistant["settings"]["monthly_limit_micro"] != 0
                    or platform_assistant["consumer"] != "platform_assistant"
                    or platform_assistant["tools"] != ["platform_health"]):
                raise RuntimeError("Platform assistant default gates or allowlist changed")
            tools = json_response(opener, base + "/api/v1/test-tools")
            if ({tool["name"] for tool in tools["tools"]} != {
                    "read_group", "read_invitations", "read_membership", "run_social",
                    "run_moderation", "inspect_operation", "reconcile_operation"}
                    or tools["limits"] != {"seconds": 300, "actions": 10, "mute_minutes": 5}
                    or tools["native_ready"] or tools["ai_ready"]):
                raise RuntimeError("Controlled Tools catalog widened authorization or readiness")
            panels = json_response(opener, base + "/api/v1/workspaces")
            if ({p["id"] for p in panels["items"]} != {"user", "wsl", "lajiaoliao", "maibot"}
                    or any(p["limits"]["native_connections"] != 20 or p["limits"]["groups"] != 300
                           for p in panels["items"] if p["id"] != "user")):
                raise RuntimeError("Four-panel admission contract is inconsistent")
            center = json_response(opener, base + "/api/v1/user-center")
            if center.get("platform_admin") is not True:
                raise RuntimeError("Admin user-center projection changed")
            if EXPECT_OPERATOR:
                entitlement = center["entitlement"]
                if (me.get("default_console") != "admin"
                        or {c["id"] for c in panels.get("consoles", [])} != {"admin", "user"}
                        or entitlement.get("mode") != "operator_demo" or not entitlement.get("unlimited")
                        or entitlement.get("expires_at") is not None or center.get("subscription") is not None):
                    raise RuntimeError("Server-derived operator workspace or demo entitlement changed")
                demo_usage = json_response(opener, base + "/api/v1/user-center/usage")
                group_page = json_response(opener, base + "/api/v1/user-center/groups?platform=wsl")
                if (demo_usage.get("billing_source") != "platform_demo" or demo_usage.get("limit") is not None
                        or demo_usage.get("currency_scale") != 1000000000 or group_page.get("limit") != 20
                        or group_page.get("items") != []):
                    raise RuntimeError("Operator costs or bounded group projection changed")
            if EXPECT_CUSTOMER:
                invitation = json_response(
                    opener, base + "/api/v1/customer-invitations", "POST",
                    {"mode": "new", "tenant_name": "synthetic-customer"},
                    "http://smoke.local", login["csrf_token"],
                    idempotency="synthetic-customer-invitation")
                token = invitation.get("token")
                if not token or invitation.get("state") != "pending":
                    raise RuntimeError("Customer invitation was not created")
                REDACTIONS.append(token)
                customer = build_opener(HTTPCookieProcessor(http.cookiejar.CookieJar()))
                preview = json_response(
                    customer, base + "/api/v1/customer-invitations/preview", "POST",
                    {"token": token}, "http://smoke.local")
                if preview.get("state") != "pending" or preview.get("token"):
                    raise RuntimeError("Invitation preview leaked its token or lost its state")
                activation = {"token": token, "username": "synthetic-customer", "password": password}
                accepted = json_response(
                    customer, base + "/api/v1/customer-invitations/activate", "POST",
                    activation, "http://smoke.local")
                if accepted.get("state") != "accepted":
                    raise RuntimeError("Customer activation failed")
                try:
                    json_response(customer, base + "/api/v1/customer-invitations/activate",
                                  "POST", activation, "http://smoke.local")
                except HTTPError as error:
                    with error:
                        if error.code != 409 or json.load(error).get("msg") != "customer_invitation_not_pending":
                            raise RuntimeError("Consumed invitation rejection changed")
                else:
                    raise RuntimeError("Consumed invitation was replayed")
                captcha_smoke(base, "synthetic-customer", password)
                customer_login = synthetic_session(directory, db, platform, "synthetic-customer")
                customer = session_opener(base, customer_login)
                session_auth_smoke(customer, base, customer_login, "user")
                rejected_response(customer, base + "/api/v1/test-tools", 403, "superadmin_required")
                customer_center = json_response(customer, base + "/api/v1/user-center")
                if (customer_center.get("groups") != [] or customer_center.get("subscription") is not None
                        or customer_center.get("native_ready") or customer_center.get("ai_ready")):
                    raise RuntimeError("New customer inherited a group, package or readiness")
                logout_smoke(customer, base, customer_login)
                closed = docker(
                    "exec", db, "psql", "-U", "wsl_owner", "-d", "wsl_test", "-Atc",
                    "SELECT NOT EXISTS(SELECT 1 FROM business_capability_gates WHERE state<>'closed')"
                    " AND NOT EXISTS(SELECT 1 FROM group_business_runtime WHERE enabled)"
                    " AND NOT EXISTS(SELECT 1 FROM business_rule_jobs)"
                    " AND NOT EXISTS(SELECT 1 FROM customer_group_accept_attempts)")
                if closed != "t":
                    raise RuntimeError("Website activation opened a native business path")
            for workspace, platform_id in (("lajiaoliao", "lajiaoliao"), ("maibot", "lajiaoliao")):
                panel = json_response(opener, base + "/api/v1/accounts", "POST",
                                      {"name": "synthetic-" + workspace, "platform": platform_id},
                                      "http://smoke.local", login["csrf_token"], workspace)
                rows = json_response(opener, base + "/api/v1/accounts", workspace=workspace)
                if not any(row["id"] == panel["id"] and row["platform"] == platform_id
                           and row["purpose"] == ("chat" if workspace == "maibot" else "management")
                           for row in rows):
                    raise RuntimeError("Platform/purpose account projection changed")
                sms = json_response(
                    opener, base + f"/api/v1/accounts/{panel['id']}/lajiaoliao-signup",
                    workspace=workspace)
                if sms != {"sms_ready": False, "review_required": False, "expires_at": None,
                           "resend_at": None, "native_ready": False}:
                    raise RuntimeError("An empty panel restored a native SMS flow")
            native = json_response(opener, base + "/api/v1/native-deployment")
            if native.get("crypto_ready") is not True or native.get("configured") is not False:
                raise RuntimeError("Native auth runtime must link libsodium without preloaded secrets")
            assistant = json_response(opener, base + "/api/v1/assistant/settings")
            if (assistant.get("enabled") or assistant.get("tenant_access") or assistant.get("approvals_enabled")
                    or assistant.get("configured") or assistant.get("monthly_tokens") != 0 or assistant.get("api_key")):
                raise RuntimeError("Assistant defaults or secret projection are unsafe")
            json_response(opener, base + "/api/v1/assistant/health")
            ai_gateway = json_response(opener, base + "/api/v1/ai-gateway")
            if (ai_gateway["settings"]["enabled"] or ai_gateway["settings"]["platform_limit_micro"]
                    or ai_gateway["runtime"]["routes"]):
                raise RuntimeError("Shared AI gateway must start disabled and unconfigured")
            account = json_response(opener, base + "/api/v1/accounts", "POST",
                                    {"name": "synthetic-stream-only"}, "http://smoke.local",
                                    login["csrf_token"])
            # An unsupported operation cannot reach a native port. Its large
            # synthetic body checks the narrowly scoped proxy upload allowance.
            upload_probe = json.dumps({"kind": "unsupported", "input": {
                "image": base64.b64encode(bytes(80 * 1024)).decode()}}).encode()
            for path, status, code in (
                    (f"/api/v1/accounts/{account['id']}/native-security-operations", 400,
                     "native_setting_unsupported"),
                    (f"/api/v1/accounts/{account['id']}/conversations/{'a' * 64}/media-uploads",
                     404, "conversation_not_found"),
                    ("/api/v1/accounts", 413, None)):
                request = Request(base + path, data=upload_probe, method="POST", headers={
                    "Content-Type": "application/json", "Origin": "http://smoke.local",
                    "X-CSRF-Token": login["csrf_token"]})
                try:
                    opener.open(request, timeout=5).close()
                except HTTPError as error:
                    with error:
                        if error.code != status or code and json.load(error).get("msg") != code:
                            api_state = json.loads(docker("inspect", api))[0]["State"]
                            proxy_result = subprocess.run(["docker", "logs", "--tail", "30", web],
                                                          capture_output=True, text=True, timeout=10)
                            proxy_log = proxy_result.stdout + proxy_result.stderr
                            diagnostic = {
                                "expected_status": status, "actual_status": error.code,
                                "api_running": api_state["Running"], "api_oom_killed": api_state["OOMKilled"],
                                "upstream_reset": "Connection reset by peer" in proxy_log,
                                "upstream_closed": "upstream prematurely closed" in proxy_log,
                                "upstream_refused": "Connection refused" in proxy_log,
                            }
                            raise RuntimeError("Security upload proxy boundary changed: " + json.dumps(diagnostic))
                else:
                    raise RuntimeError("Unsupported security upload was accepted")
            for kind in ("password_sms", "phone_old_sms", "secondary_security_code", "legal_identity") if EXPECT_PRODUCT_SCOPE else ():
                request = Request(
                    base + f"/api/v1/accounts/{account['id']}/native-security-operations",
                    data=json.dumps({"kind": kind, "input": {}}).encode(), method="POST",
                    headers={"Content-Type": "application/json", "Origin": "http://smoke.local",
                             "X-CSRF-Token": login["csrf_token"]})
                try:
                    opener.open(request, timeout=5).close()
                except HTTPError as error:
                    with error:
                        if error.code != 409 or json.load(error).get("msg") != "native_capability_not_in_product":
                            raise RuntimeError("Excluded native account operation changed its rejection")
                else:
                    raise RuntimeError("Excluded native account operation was accepted")
            # Only a local panel record; never native signup, login or reception.
            with opener.open(base + f"/api/v1/accounts/{account['id']}/events?view=messages", timeout=5) as response:
                if not response.headers.get("Content-Type", "").startswith("text/event-stream"):
                    raise RuntimeError("Account SSE proxy did not stream")
                event = None
                for _ in range(8):
                    line = response.readline(65537)
                    if len(line) > 65536:
                        raise RuntimeError("Account SSE frame exceeded limit")
                    if line.startswith(b"data: "):
                        event = json.loads(line[6:])
                        break
                if not event or event.get("account_id") != account["id"] or event.get("reset") is not True:
                    raise RuntimeError("Account SSE did not emit its initial scoped snapshot")
            with opener.open(base + "/api/v1/system/monitor/events", timeout=5) as response:
                if not response.headers.get("Content-Type", "").startswith("text/event-stream"):
                    raise RuntimeError("Monitor SSE proxy did not stream")
                event = None
                for _ in range(8):
                    line = response.readline(65537)
                    if len(line) > 65536:
                        raise RuntimeError("Monitor SSE frame exceeded limit")
                    if line.startswith(b"data: "):
                        event = json.loads(line[6:])
                        break
                if (not event or event.get("workspace") != "wsl"
                        or event.get("monitor_scope") != "platform_workspace"
                        or event.get("api", {}).get("source") != "api_self"):
                    raise RuntimeError("Monitor SSE did not emit its scoped source projection")
            json_response(opener, base + "/api/v1/system/monitor/history")
            json_response(opener, base + "/api/v1/system/monitor/incidents")
            monitor_result = None
            if EXPECT_PROJECT_MONITOR:
                deadline = time.monotonic() + 20
                while True:
                    view = json_response(opener, base + "/api/v1/system/monitor")
                    if (all(view.get("services", {}).get(name, {}).get("available")
                            for name in ("api", "heatbot", "web", "postgres"))
                            and view.get("database_stats", {}).get("available")):
                        break
                    if time.monotonic() > deadline:
                        raise RuntimeError("Project monitoring did not become available")
                    time.sleep(1)
                for name, quota in (("api", 1), ("heatbot", 0.5)):
                    resource = view["services"][name]["resources"]
                    if resource["cpu_quota_cores"] != quota or not resource["process_rss_bytes"]:
                        raise RuntimeError("Process/container resource contract failed")
                if any(key in view for key in ("environment", "cpu_count", "cpu_usage", "used_memory_bytes")):
                    raise RuntimeError("Retired environment fields remain exposed")
                # Only disposable containers are stopped; production is untouched.
                docker("stop", "-t", "2", heat)
                time.sleep(6)
                failed = json_response(opener, base + "/api/v1/system/monitor")
                if failed["services"]["heatbot"]["available"] or not failed["services"]["api"]["available"]:
                    raise RuntimeError("Heatbot fault was not independently detected")
                docker("start", heat)
                time.sleep(6)
                rows = []
                streams = []
                stream_threads = []
                stream_stop = threading.Event()
                stream_frames = [0, 0]
                stream_reconnects = [0, 0]
                stream_lifetimes = [[], []]
                stream_max_gap = [0, 0]
                stream_last_frame = [None, None]
                stream_errors = []
                def drain_stream(index, stream):
                    try:
                        while not stream_stop.is_set():
                            opened = time.monotonic()
                            try:
                                while not stream_stop.is_set():
                                    line = stream.readline(65537)
                                    if not line:
                                        lifetime = time.monotonic() - opened
                                        # The shared endpoint deliberately ends at 100s.
                                        if lifetime < 90:
                                            raise RuntimeError("Premature monitor stream end")
                                        stream_lifetimes[index].append(round(lifetime, 3))
                                        break
                                    if len(line) > 65536:
                                        raise RuntimeError("Monitor frame exceeded limit")
                                    if line.startswith(b"data: "):
                                        now = time.monotonic()
                                        if stream_last_frame[index] is not None:
                                            gap = now - stream_last_frame[index]
                                            stream_max_gap[index] = max(stream_max_gap[index], gap)
                                            if gap > 5:
                                                raise RuntimeError("Monitor reconnect gap exceeded limit")
                                        stream_last_frame[index] = now
                                        stream_frames[index] += 1
                            finally:
                                stream.close()
                            if stream_stop.wait(1):
                                return
                            stream = opener.open(base + "/api/v1/system/monitor/events", timeout=5)
                            streams[index] = stream
                            stream_reconnects[index] += 1
                    except Exception as error:
                        if not stream_stop.is_set():
                            stream_errors.append({"stream": index, "type": type(error).__name__,
                                                  "frames": stream_frames[index]})
                    finally:
                        stream.close()
                try:
                    for index in range(2):
                        streams.append(opener.open(base + "/api/v1/system/monitor/events", timeout=5))
                        thread = threading.Thread(target=drain_stream, args=(index, streams[-1]), daemon=True)
                        thread.start()
                        stream_threads.append(thread)
                    start = time.monotonic()
                    for offset in range(0, MONITOR_SOAK_SECONDS + 1, 5):
                        delay = start + offset - time.monotonic()
                        if delay > 0:
                            time.sleep(delay)
                        snapshot = json_response(opener, base + "/api/v1/system/monitor")
                        for _ in range(10):
                            duplicate = json_response(opener, base + "/api/v1/system/monitor")
                            if duplicate["api"]["collection_count"] > snapshot["api"]["collection_count"] + 1:
                                raise RuntimeError("Browser reads increased collection frequency")
                        rows.append({"elapsed": time.monotonic()-start, "api": snapshot["api"],
                                     "heatbot": snapshot["services"]["heatbot"],
                                     "database_stats": snapshot["database_stats"],
                                     "monitor_pool": snapshot["database_pools"]["monitor"]})
                        if len(rows) % 12 == 0:
                            print(json.dumps({"monitor_soak_elapsed": offset, "samples": len(rows),
                                              "stream_frames": stream_frames,
                                              "reconnects": stream_reconnects}), flush=True)
                        if stream_errors:
                            raise RuntimeError("Monitor stream failed during observation")
                finally:
                    stream_stop.set()
                    for thread in stream_threads:
                        thread.join(timeout=5)
                    if any(thread.is_alive() for thread in stream_threads):
                        stream_errors.append({"type": "StreamDidNotStop"})
                        for stream in streams:
                            stream.close()
                    monitor_result = {"seconds": MONITOR_SOAK_SECONDS, "samples": len(rows), "rows": rows,
                                      "two_streams_ten_reads_per_tick": True, "stream_frames": stream_frames,
                                      "stream_reconnects": stream_reconnects, "stream_lifetimes": stream_lifetimes,
                                      "stream_max_gap_seconds": stream_max_gap, "stream_errors": stream_errors,
                                      "production_data_used": False}
                    monitor_result["images"] = {
                        name: docker("inspect", container, "--format", "{{.Image}}")
                        for name, container in (("api", api), ("heatbot", heat), ("web", web))
                    }
                    if report := os.environ.get("WSL_SMOKE_MONITOR_REPORT"):
                        Path(report).write_text(json.dumps(monitor_result, indent=2))
                validate_monitor_observation(monitor_result)
            logout_smoke(opener, base, login)
            print(json.dumps({
                "passed": True, "fresh_database_roles": True, "migrations_and_bootstrap": True,
                "non_root_read_only_runtime": True, "built_assets": True,
                "synthetic_session_me_csrf_role_logout": True,
                "captcha_image_contract": True, "login_ticket_required": True,
                "captcha_misalignment_and_replay_rejected": True,
                "positive_password_login": "separate_synthetic_http_postgres_tests",
                "native_auth_crypto_ready": True, "native_deployment_preloaded": False,
                "native_signup_migration": True, "native_signup_tenant_isolation": True,
                "native_security_flow_isolation": True, "security_upload_proxy_boundary": True,
                "media_upload_proxy_boundary": True, "media_sender_default_closed": True,
                "native_ready": False, "ai_ready": False, "production_data_used": False,
                "assistant_defaults_closed": True,
                "account_sse_proxy": True,
                "monitor_sse_proxy": True, "monitor_history_and_incidents": True,
                "project_monitor": EXPECT_PROJECT_MONITOR, "monitor_soak_seconds": MONITOR_SOAK_SECONDS,
                "multi_platform_migration": True, "four_panels": True,
                "platform_purpose_accounts": True, "lajiaoliao_credentials_tenant_isolated": True,
                "lajiaoliao_signup_migration": True, "lajiaoliao_sms_default_closed": True,
                "go_text_engine_closed": True, "workspace_logos": True,
                "native_product_scope": EXPECT_PRODUCT_SCOPE,
                "native_consumer_boundary": EXPECT_NATIVE_USAGE,
                "retired_new_submissions_closed": EXPECT_PRODUCT_SCOPE,
                "assistant_platform_migrations": EXPECT_ASSISTANT_PLATFORMS,
                "customer_invitation_activation": EXPECT_CUSTOMER,
                "customer_defaults_closed": EXPECT_CUSTOMER,
                "operator_demo_and_manual_plans": EXPECT_OPERATOR,
                "chili_media_migration": EXPECT_CHILI_MEDIA, "database": "wsl_test",
            }, indent=2))
    finally:
        for identifier in reversed(CONTAINERS):
            subprocess.run(["docker", "rm", "-f", identifier], capture_output=True, timeout=20)
        subprocess.run(["docker", "network", "rm", PREFIX], capture_output=True, timeout=20)


if __name__ == "__main__":
    main()
