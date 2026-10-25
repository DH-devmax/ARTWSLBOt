"""Exercise hardened local images with a disposable database and no native accounts."""

import base64
import http.cookiejar
import json
import os
import secrets
import subprocess
import tempfile
import time
from html.parser import HTMLParser
from pathlib import Path
from urllib.request import HTTPCookieProcessor, Request, build_opener, urlopen

ROOT = Path(__file__).resolve().parents[1]
PREFIX = "wsl-smoke-" + secrets.token_hex(6)
CONTAINERS = []
REDACTIONS = []


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


def json_response(opener, url, method="GET", body=None, origin=None, csrf=None):
    headers = {"Content-Type": "application/json"}
    if origin:
        headers["Origin"] = origin
    if csrf:
        headers["X-CSRF-Token"] = csrf
    request = Request(url, method=method, headers=headers, data=None if body is None else json.dumps(body).encode())
    with opener.open(request, timeout=5) as response:
        result = json.load(response)
        if result.get("code") != 0:
            raise RuntimeError("API did not return success")
        return result["data"]


class Scripts(HTMLParser):
    def __init__(self):
        super().__init__()
        self.sources = []

    def handle_starttag(self, tag, attrs):
        values = dict(attrs)
        if tag == "script" and values.get("src", "").startswith("/assets/"):
            self.sources.append(values["src"])


def main():
    # Fresh credentials never leave this disposable network or the temporary 0600 files.
    owner, platform, tenant, password = [secrets.token_urlsafe(32) for _ in range(4)]
    master = base64.b64encode(secrets.token_bytes(32)).decode()
    REDACTIONS.extend((owner, platform, tenant, password, master))
    for image in ("wsl-api:local", "wsl-web:local", "postgres:17.6-alpine"):
        docker("image", "inspect", image, "--format", "{{.Id}}")
    docker("network", "create", "--label", f"wsl.smoke={PREFIX}", PREFIX)
    try:
        with tempfile.TemporaryDirectory(prefix="wsl-container-smoke-") as temporary:
            directory = Path(temporary)
            db_env = environment(directory, "database.env", {
                "POSTGRES_DB": "wsl",
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
                ready = subprocess.run(["docker", "exec", db, "pg_isready", "-h", "127.0.0.1", "-U", "wsl_owner", "-d", "wsl"],
                                       capture_output=True, timeout=5)
                if ready.returncode == 0:
                    break
                if time.monotonic() > deadline:
                    raise RuntimeError("Fresh PostgreSQL startup timed out")
                time.sleep(0.5)
            roles = docker("exec", db, "psql", "-U", "wsl_owner", "-d", "wsl", "-Atc",
                           "SELECT rolname||':'||rolsuper FROM pg_roles WHERE rolname IN ('wsl_platform','wsl_tenant') ORDER BY rolname")
            if roles.splitlines() != ["wsl_platform:false", "wsl_tenant:false"]:
                raise RuntimeError("Fresh database did not create restricted runtime roles")
            owner_env = environment(directory, "owner.env", {
                "MIGRATION_DATABASE_URL": f"postgres://wsl_owner:{owner}@postgres:5432/wsl?sslmode=disable",
                "DATABASE_URL": f"postgres://wsl_platform:{platform}@postgres:5432/wsl?sslmode=disable",
                "TENANT_DATABASE_URL": f"postgres://wsl_tenant:{tenant}@postgres:5432/wsl?sslmode=disable",
                "MASTER_KEY": master,
                "BOOTSTRAP_USERNAME": "admin",
                "BOOTSTRAP_PASSWORD": password,
            })
            for command in ("migrate", "bootstrap"):
                docker("run", "--rm", "--network", PREFIX, "--env-file", owner_env,
                       "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
                       "wsl-api:local", command)
            signup_schema = docker("exec", db, "psql", "-U", "wsl_owner", "-d", "wsl", "-Atc",
                                   "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='007_native_signup.sql')"
                                   " AND NOT has_table_privilege('wsl_tenant','native_signup_attempts','SELECT')")
            if signup_schema != "t":
                raise RuntimeError("Native signup migration or tenant isolation is missing")
            # Docker allocates an unused localhost port; no other project's server is replaced.
            web_name = PREFIX + "-web"
            api_env = environment(directory, "api.env", {
                "DATABASE_URL": f"postgres://wsl_platform:{platform}@postgres:5432/wsl?sslmode=disable",
                "TENANT_DATABASE_URL": f"postgres://wsl_tenant:{tenant}@postgres:5432/wsl?sslmode=disable",
                "MASTER_KEY": master,
                "HTTP_ADDR": ":8080",
                "PUBLIC_ORIGIN": "http://smoke.local",
                "COOKIE_SECURE": "false",
                "ENABLE_NATIVE": "false",
            })
            api = run_container(PREFIX + "-api", "--network", PREFIX, "--network-alias", "api",
                                "--env-file", api_env, "--read-only", "--cap-drop", "ALL",
                                "--security-opt", "no-new-privileges:true", "wsl-api:local")
            web = run_container(web_name, "--network", PREFIX, "--read-only", "--cap-drop", "ALL",
                                "--security-opt", "no-new-privileges:true",
                                "--tmpfs", "/tmp:rw,size=64m,noexec,nosuid",
                                "-p", "127.0.0.1::8080", "wsl-web:local")
            address = docker("port", web, "8080/tcp")
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
            for identifier in (api, web):
                info = json.loads(docker("inspect", identifier))[0]
                if info["Config"]["User"] in ("", "0", "root") or not info["HostConfig"]["ReadonlyRootfs"]:
                    raise RuntimeError("Runtime user/read-only configuration is not hardened")
            with urlopen(base, timeout=5) as response:
                parser = Scripts()
                parser.feed(response.read().decode())
            if not parser.sources:
                raise RuntimeError("Built product script is missing")
            with urlopen(base + "/wsl-logo.png", timeout=5) as response:
                if response.status != 200 or not response.read().startswith(b"\x89PNG\r\n\x1a\n"):
                    raise RuntimeError("User-provided brand asset is not readable")
            for path in parser.sources:
                with urlopen(base + path, timeout=5) as response:
                    if response.status != 200 or "javascript" not in response.headers.get("Content-Type", ""):
                        raise RuntimeError("Built script did not load")
            opener = build_opener(HTTPCookieProcessor(http.cookiejar.CookieJar()))
            login = json_response(opener, base + "/api/v1/auth/login", "POST",
                                  {"username": "admin", "password": password}, "http://smoke.local")
            me = json_response(opener, base + "/api/v1/me")
            if me["user"]["role"] != "superadmin":
                raise RuntimeError("Bootstrap login did not get the expected role")
            native = json_response(opener, base + "/api/v1/native-deployment")
            if native.get("crypto_ready") is not True or native.get("configured") is not False:
                raise RuntimeError("Native auth runtime must link libsodium without preloaded secrets")
            json_response(opener, base + "/api/v1/auth/logout", "POST", {}, "http://smoke.local",
                          login["csrf_token"])
            print(json.dumps({
                "passed": True, "fresh_database_roles": True, "migrations_and_bootstrap": True,
                "non_root_read_only_runtime": True, "built_assets": True, "login_and_logout": True,
                "native_auth_crypto_ready": True, "native_deployment_preloaded": False,
                "native_signup_migration": True, "native_signup_tenant_isolation": True,
                "native_ready": False, "ai_ready": False, "production_data_used": False,
            }, indent=2))
    finally:
        for identifier in reversed(CONTAINERS):
            subprocess.run(["docker", "rm", "-f", identifier], capture_output=True, timeout=20)
        subprocess.run(["docker", "network", "rm", PREFIX], capture_output=True, timeout=20)


if __name__ == "__main__":
    main()
