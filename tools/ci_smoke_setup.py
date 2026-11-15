"""Prepare the existing Compose CI stack with an isolated synthetic database."""

import json
import os
import re
import stat
import subprocess
import sys
import tempfile
from pathlib import Path
from urllib.parse import urlsplit, urlunsplit

ROOT = Path(__file__).resolve().parents[1]

OVERLAY = """services:
  postgres:
    environment:
      POSTGRES_DB: wsl_test
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U wsl_owner -d wsl_test"]
  migrate:
    environment:
      DATABASE_URL: postgres://wsl_platform:${WSL_PLATFORM_PASSWORD}@postgres:5432/wsl_test?sslmode=disable
      TENANT_DATABASE_URL: postgres://wsl_tenant:${WSL_TENANT_PASSWORD}@postgres:5432/wsl_test?sslmode=disable
      MIGRATION_DATABASE_URL: postgres://wsl_owner:${POSTGRES_OWNER_PASSWORD}@postgres:5432/wsl_test?sslmode=disable
  bootstrap:
    environment:
      DATABASE_URL: postgres://wsl_platform:${WSL_PLATFORM_PASSWORD}@postgres:5432/wsl_test?sslmode=disable
      TENANT_DATABASE_URL: postgres://wsl_tenant:${WSL_TENANT_PASSWORD}@postgres:5432/wsl_test?sslmode=disable
      BOOTSTRAP_USERNAME: synthetic-smoke-admin
  api:
    environment:
      DATABASE_URL: postgres://wsl_platform:${WSL_PLATFORM_PASSWORD}@postgres:5432/wsl_test?sslmode=disable
      TENANT_DATABASE_URL: postgres://wsl_tenant:${WSL_TENANT_PASSWORD}@postgres:5432/wsl_test?sslmode=disable
"""


def private_write(path, text):
    with open(path, "x", opener=lambda name, flags: os.open(name, flags, 0o600)) as handle:
        handle.write(text)


def prepare():
    isolate_fixture_database(ROOT / ".env")
    folder = ROOT / "var"
    folder.mkdir(mode=0o700, exist_ok=True)
    overlay = folder / "ci-test.compose.yaml"
    private_write(overlay, OVERLAY)
    with open(os.environ["GITHUB_ENV"], "a") as handle:
        handle.write(f"COMPOSE_FILE={ROOT / 'compose.yaml'}:{overlay}\n")


def isolate_fixture_database(path):
    """Only newly generated GitHub fixtures may be redirected to wsl_test."""
    if os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("CI") != "true":
        raise RuntimeError("This setup is confined to GitHub Actions CI")
    if path.is_symlink() or stat.S_IMODE(path.stat().st_mode) != 0o600:
        raise RuntimeError("Expected a private generated fixture")
    keys = {"DATABASE_URL", "TENANT_DATABASE_URL", "MIGRATION_DATABASE_URL"}
    lines, found = [], set()
    for line in path.read_text().splitlines():
        key, separator, value = line.partition("=")
        if separator and key in keys:
            if key in found:
                raise RuntimeError("Duplicate fixture database URL")
            found.add(key)
            parsed = urlsplit(value)
            if (parsed.scheme not in {"postgres", "postgresql"} or
                    parsed.hostname != "127.0.0.1" or
                    parsed.path not in {"/wsl", "/wsl_test"} or
                    parsed.query != "sslmode=disable" or parsed.fragment):
                raise RuntimeError("Invalid generated database fixture")
            line = key + "=" + urlunsplit(parsed._replace(path="/wsl_test"))
        lines.append(line)
    if found != keys:
        raise RuntimeError("Missing generated database fixture")
    path.write_text("\n".join(lines) + "\n")


def docker(*args):
    result = subprocess.run(["docker", *args], capture_output=True, text=True, timeout=120)
    if result.returncode:
        raise RuntimeError("Isolated CI fixture command failed")
    return result.stdout.strip()


def create_session():
    values = dict(line.split("=", 1) for line in (ROOT / ".env").read_text().splitlines()
                  if "=" in line and not line.startswith("#"))
    # No submitted role, grant or identity is created here.
    identifier = docker("exec", "wsl-platform-postgres-1", "psql", "-U", "wsl_owner",
                        "-d", "wsl_test", "-Atc",
                        "SELECT id FROM users WHERE username='synthetic-smoke-admin'"
                        " AND role='superadmin' AND NOT disabled")
    if not re.fullmatch(r"[0-9a-f-]{36}", identifier):
        raise RuntimeError("Expected synthetic CI bootstrap identity is absent")
    parsed = urlsplit(values["DATABASE_URL"])
    if parsed.hostname != "127.0.0.1" or parsed.username != "wsl_platform":
        raise RuntimeError("CI connection template is invalid")
    dsn = urlunsplit(parsed._replace(netloc=f"{parsed.username}:{parsed.password}@postgres:5432",
                                    path="/wsl_test"))
    output = ROOT / "var/ci-website-session.json"
    if output.exists() or output.is_symlink():
        raise RuntimeError("Refusing to overwrite a CI session")
    with tempfile.TemporaryDirectory(prefix="wsl-ci-session-") as temporary:
        environment = Path(temporary) / "fixture.env"
        private_write(environment, f"DATABASE_URL={dsn}\n"
                      f"WSL_SMOKE_SESSION_USERNAME=synthetic-smoke-admin\n"
                      f"WSL_SMOKE_SESSION_USER_ID={identifier}\n")
        docker("run", "--rm", "--network", "wsl-platform_default", "--read-only",
               "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true",
               "--tmpfs", "/tmp:rw,exec,nosuid,nodev,size=256m",
               "--env-file", str(environment), "-e", "GOCACHE=/tmp/go-build",
               "-v", f"{ROOT}:/src:ro", "-v", f"{output.parent}:/smoke-output",
               "wsl-test:ci", "run", "-mod=readonly", "./tools/smoke_session",
               "/smoke-output/" + output.name)
    if stat.S_IMODE(output.stat().st_mode) != 0o600:
        raise RuntimeError("CI session output is not private")
    session = json.loads(output.read_text())
    if session["username"] != "synthetic-smoke-admin" or session["user_id"] != identifier:
        raise RuntimeError("CI fixture identity changed")


def main():
    if os.environ.get("GITHUB_ACTIONS") != "true" or os.environ.get("CI") != "true":
        raise RuntimeError("This setup is confined to GitHub Actions CI")
    if len(sys.argv) != 2 or sys.argv[1] not in ("prepare", "session"):
        raise RuntimeError("Expected prepare or session")
    {"prepare": prepare, "session": create_session}[sys.argv[1]]()
    print(json.dumps({"passed": True, "phase": sys.argv[1], "database": "wsl_test"}))


if __name__ == "__main__":
    try:
        main()
    except Exception:
        print("Isolated CI fixture setup failed", file=sys.stderr)
        sys.exit(1)
