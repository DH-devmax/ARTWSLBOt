"""Small, fail-closed report and subprocess contracts shared by CI tools."""

import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import threading
import time

ROOT = Path(__file__).resolve().parents[2]
SCHEMA = 1
SHA = re.compile(r"[0-9a-f]{40}")


def revision(root=ROOT):
    value = subprocess.check_output(
        ["git", "rev-parse", "HEAD"], cwd=root, text=True
    ).strip()
    if not SHA.fullmatch(value):
        raise ValueError("Invalid source revision")
    return value


def digest(path):
    value = hashlib.sha256()
    with Path(path).open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def load_json(path):
    def unique(items):
        result = {}
        for key, value in items:
            if key in result:
                raise ValueError("Duplicate JSON key")
            result[key] = value
        return result
    def reject_constant(value):
        raise ValueError("Non-JSON numeric constant")
    return json.loads(Path(path).read_text(), object_pairs_hook=unique,
                      parse_constant=reject_constant)


def write_json(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(value, indent=2, ensure_ascii=True) + "\n")
    temporary.replace(path)


def run(command, *, timeout=120, env=None, cwd=ROOT, capture=True):
    """Kill the whole child group on timeout or cancellation; never echo output."""
    process = subprocess.Popen(
        command, cwd=cwd, env=env, text=True, start_new_session=True,
        stdout=subprocess.PIPE if capture else None,
        stderr=subprocess.PIPE if capture else None,
    )
    previous = None
    if threading.current_thread() is threading.main_thread():
        previous = signal.getsignal(signal.SIGTERM)

        def terminate(number, frame):
            raise SystemExit(128 + number)

        signal.signal(signal.SIGTERM, terminate)
    try:
        stdout, stderr = process.communicate(timeout=timeout)
    except BaseException:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.communicate()
        raise
    finally:
        if previous is not None:
            signal.signal(signal.SIGTERM, previous)
    return subprocess.CompletedProcess(command, process.returncode, stdout, stderr)


def report(profile, cases, *, started=None, extra=None):
    identifiers = [case["id"] for case in cases]
    if not cases or len(set(identifiers)) != len(identifiers):
        raise ValueError("Missing or duplicate test result")
    if any(case["status"] not in {"passed", "failed", "skipped"} for case in cases):
        raise ValueError("Invalid case status")
    counts = {state: sum(case["status"] == state for case in cases)
              for state in ("passed", "failed", "skipped")}
    return {
        "schema": SCHEMA, "sha": revision(), "profile": profile,
        "status": "failed" if counts["failed"] or any(
            case["status"] == "skipped" and case.get("approved") is not True
            for case in cases) else "passed",
        "counts": counts, "cases": cases,
        "duration_seconds": round(time.monotonic() - started, 3) if started else 0,
        **(extra or {}),
    }
