"""Start/stop this workspace's local API and Vite without touching other services."""

import json
import os
import shutil
import signal
import socket
import subprocess
import sys
import time
from pathlib import Path
from urllib.request import urlopen

ROOT = Path(__file__).resolve().parents[1]
STATE = ROOT / "var/dev-services.json"


def stamp(pid):
    p = subprocess.run(["ps", "-p", str(pid), "-o", "lstart=", "-o", "args="], capture_output=True, text=True)
    return p.stdout.strip() if p.returncode == 0 else ""


def active(state):
    return bool(state.get("stamp")) and stamp(state["pid"]) == state["stamp"]


def load():
    return json.loads(STATE.read_text()) if STATE.exists() else {}


def stop(state):
    for item in state.values():
        if active(item):
            os.killpg(item["pid"], signal.SIGTERM)
    deadline = time.monotonic() + 10
    while any(active(item) for item in state.values()) and time.monotonic() < deadline:
        time.sleep(0.1)
    for item in state.values():
        if active(item):
            os.killpg(item["pid"], signal.SIGKILL)
    STATE.unlink(missing_ok=True)


def start():
    existing = load()
    if any(active(item) for item in existing.values()):
        raise SystemExit("Workspace services already running; use status or stop first.")
    for port in (8080, 5173):
        with socket.socket() as check:
            check.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            try:
                check.bind(("127.0.0.1", port))
            except OSError:
                raise SystemExit(f"Port {port} is occupied; existing services were not changed.")
    if not (ROOT / "bin/wsl").is_file() or not shutil.which("pnpm"):
        raise SystemExit("Run go build -tags sodium -o bin/wsl ./cmd/wsl and install the web dependencies first.")
    (ROOT / "var").mkdir(mode=0o700, exist_ok=True)
    commands = {
        "api": ([str(ROOT / "bin/wsl"), "serve"], ROOT),
        "web": ([shutil.which("pnpm"), "dev", "--host", "127.0.0.1", "--port", "5173", "--strictPort"], ROOT / "web"),
    }
    state = {}
    try:
        for name, (command, cwd) in commands.items():
            log_path = ROOT / f"var/{name}.log"
            fd = os.open(log_path, os.O_CREAT | os.O_WRONLY | os.O_APPEND, 0o600)
            with os.fdopen(fd, "ab") as log:
                proc = subprocess.Popen(command, cwd=cwd, stdout=log, stderr=log, start_new_session=True)
            time.sleep(0.1)
            state[name] = {"pid": proc.pid, "stamp": stamp(proc.pid), "log": str(log_path)}
        fd = os.open(STATE, os.O_CREAT | os.O_TRUNC | os.O_WRONLY, 0o600)
        with os.fdopen(fd, "w") as f:
            json.dump(state, f, indent=2)
        for address in ("http://127.0.0.1:8080/healthz", "http://127.0.0.1:5173"):
            deadline = time.monotonic() + 20
            while True:
                if not all(active(item) for item in state.values()):
                    raise RuntimeError("A workspace service exited; inspect var/*.log.")
                try:
                    with urlopen(address, timeout=1) as response:
                        if response.status == 200:
                            break
                except Exception:
                    if time.monotonic() > deadline:
                        raise RuntimeError("Workspace startup timed out; inspect var/*.log.")
                    time.sleep(0.2)
        print("Web: http://127.0.0.1:5173\nAPI: http://127.0.0.1:8080/healthz")
    except Exception:
        stop(state)
        raise


if __name__ == "__main__":
    command = sys.argv[1] if len(sys.argv) == 2 else ""
    if command == "start":
        raise SystemExit("Host services are retired. Use docker compose up -d --build --wait.")
    elif command == "stop":
        stop(load())
        print("Workspace services stopped.")
    elif command == "status":
        for name, item in load().items():
            print(f"{name}: {'running' if active(item) else 'stopped'}")
    else:
        raise SystemExit("Usage: python3 tools/dev_services.py start|stop|status")
