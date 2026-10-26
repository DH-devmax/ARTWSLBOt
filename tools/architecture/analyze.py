"""Run CodeBoarding on an allowlisted snapshot, through a bounded private relay."""
import argparse
from contextlib import contextmanager
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import signal
import subprocess
import sys
import tempfile
import threading

from architecture import ROOT, HERE, candidate_files, digest, json_text, run, write_changed
from provider import CONFIG, PRIVATE, private_dir, read_key, request

MAX_REQUESTS = 16
MAX_RESERVED_TOKENS = 300_000
TOOLCHAIN = json.loads((HERE / "toolchain.json").read_text())
CACHE = Path.home() / ".cache/artwslbot/architecture/analysis"


def allowed_source(name):
    path = Path(name)
    if path.name.startswith(".env") or any(p in path.parts for p in ("testdata", "migrations", "vendor")):
        return False
    return ((name.startswith(("cmd/", "internal/")) and name.endswith(".go") and not name.endswith("_test.go"))
            or (name.startswith("web/src/wsl/") and path.suffix in (".ts", ".vue"))
            or name in ("go.mod", "go.sum", "web/tsconfig.json", "web/tsconfig.wsl.json"))


def source_manifest():
    result = {}
    for name in candidate_files():
        if not allowed_source(name):
            continue
        content = (ROOT / name).read_bytes()
        if len(content) > 1_000_000:
            raise ValueError("Oversized source rejected: " + name)
        if re.search(rb"\b(?:sk-[A-Za-z0-9_-]{20,}|gh[pousr]_[A-Za-z0-9]{25,})", content):
            raise ValueError("Potential hardcoded credential; snapshot rejected: " + name)
        result[name] = hashlib.sha256(content).hexdigest()
    if not result:
        raise ValueError("Empty analysis source is not permitted")
    return result


def incremental_result(stdout):
    """Upstream cold bootstrap prints progress before its final JSON wire result."""
    decoder = json.JSONDecoder()
    results = []
    for match in re.finditer(r"(?m)^[ \t]*\{", stdout):
        start = match.end() - 1
        try:
            payload, end = decoder.raw_decode(stdout[start:])
        except json.JSONDecodeError:
            continue
        if isinstance(payload, dict) and (
            "requiresFullAnalysis" in payload or payload.get("mode") == "incremental"
        ):
            results.append((payload, start + end))
    if len(results) != 1 or stdout[results[0][1]:].strip():
        raise ValueError("Missing, conflicting or truncated terminal incremental JSON result")
    return results[0][0]


def validate_result(stdout, analysis_path, incremental):
    if incremental:
        # Upstream may report a failure in JSON with process exit status zero.
        payload = incremental_result(stdout)
        if payload.get("requiresFullAnalysis") or "error" in payload:
            raise ValueError("Incremental analysis unavailable; explicit baseline review required")
        if payload.get("requiresFullAnalysis") is not False:
            raise ValueError("Incomplete incremental result contract")
        if payload.get("mode") != "incremental" or payload.get("analysis_path") != str(analysis_path):
            raise ValueError("Incremental result mode or output path mismatch")
    payload = json.loads(analysis_path.read_text())
    if payload.get("error") or not payload.get("components"):
        raise ValueError("Analysis lacks verified component output")
    return payload


def promote(output, baseline, state_path, state):
    """Keep a recoverable prior baseline until both validated data and state exist."""
    previous = baseline.with_name("previous-baseline")
    if previous.exists():
        raise ValueError("An interrupted promotion needs review; previous baseline preserved")
    old_state = state_path.read_text() if state_path.exists() else None
    if baseline.exists():
        os.replace(baseline, previous)
    try:
        os.replace(output, baseline)
        fd, name = tempfile.mkstemp(prefix="state-", dir=state_path.parent)
        with os.fdopen(fd, "w") as out:
            out.write(json_text(state))
            out.flush()
            os.fsync(out.fileno())
        os.replace(name, state_path)
    except BaseException:
        if baseline.exists():
            shutil.rmtree(baseline)
        if previous.exists():
            os.replace(previous, baseline)
        if old_state is not None:
            state_path.write_text(old_state)
        raise
    if previous.exists():
        shutil.rmtree(previous)


@contextmanager
def relay():
    ledger = {"requests": 0, "reserved_tokens": 0, "reported_tokens": 0, "usage_unknown": 0}
    lock = threading.Lock()
    token = secrets.token_urlsafe(32)

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass

        def do_POST(self):
            if self.path != "/v1/chat/completions" or self.headers.get("Authorization") != "Bearer " + token:
                self.send_error(403)
                return
            try:
                size = int(self.headers.get("Content-Length", "0"))
                if not 0 < size <= 1_000_000:
                    raise ValueError("request_size")
                data = json.loads(self.rfile.read(size))
                if data.get("model") != CONFIG["model"] or data.get("stream"):
                    raise ValueError("unsupported_model_or_stream")
                data["max_tokens"] = min(int(data.get("max_tokens") or 4096), CONFIG["max_output_tokens"])
                # UTF-8 bytes are a deliberately conservative input token upper bound.
                reserve = size + data["max_tokens"]
                with lock:
                    if ledger["requests"] >= MAX_REQUESTS or ledger["reserved_tokens"] + reserve > MAX_RESERVED_TOKENS:
                        raise ValueError("analysis_budget_exceeded")
                    ledger["requests"] += 1
                    ledger["reserved_tokens"] += reserve
                    ledger["usage_unknown"] += 1
                response = request("/chat/completions", data)
                usage_data = response.get("usage")
                usage = usage_data.get("total_tokens") if isinstance(usage_data, dict) else None
                with lock:
                    if type(usage) is int and usage >= 0:
                        ledger["reported_tokens"] += usage
                        ledger["usage_unknown"] -= 1
                body = json.dumps(response).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
            except (ValueError, OSError, TypeError):
                # No raw provider errors, prompts, credential or source in HTTP errors.
                self.send_error(502, "Architecture provider unavailable or run budget exhausted")

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    server.daemon_threads = True
    worker = threading.Thread(target=server.serve_forever, daemon=True)
    worker.start()
    try:
        yield f"http://127.0.0.1:{server.server_port}/v1", token, ledger
    finally:
        server.shutdown()
        server.server_close()


def analyze(initialize=False):
    os.umask(0o077)
    private_dir(PRIVATE)
    private_dir(CACHE)
    read_key()  # Fail before copying source or installing language servers if unconfigured.
    baseline = CACHE / "baseline"
    state_path = CACHE / "state.json"
    source = source_manifest()
    settings = digest({"toolchain": TOOLCHAIN,
                       "ignore": (ROOT / ".codeboarding/.codeboardingignore").read_text(),
                       "runner": [hashlib.sha256((HERE / n).read_bytes()).hexdigest()
                                  for n in ("analyze.py", "codeboarding_entry.py", "requirements.lock")]})
    if initialize:
        if state_path.exists() or baseline.exists():
            raise ValueError("A baseline already exists; use incremental, not automatic full replacement")
    else:
        if not state_path.is_file() or not (baseline / "analysis.json").is_file():
            raise ValueError("No trusted baseline; explicitly run --initialize once")
        state = json.loads(state_path.read_text())
        if state["settings_hash"] != settings:
            raise ValueError("Baseline toolchain differs; review and archive it before explicit initialization")
        if os.getenv("GITHUB_ACTIONS") == "true":
            current_branch = os.environ.get("GITHUB_REF", "")
            if state.get("trusted_ref") != current_branch or not current_branch.startswith("refs/heads/"):
                raise ValueError("Untrusted CI baseline origin")
            subprocess.run(["git", "merge-base", "--is-ancestor", state["commit"], "HEAD"],
                           cwd=ROOT, check=True, capture_output=True)

    # Only analysis artifacts are deserialized. Upstream's separately installed
    # language servers contain legitimate npm executable symlinks.
    trusted_paths = ([state_path, baseline, *baseline.rglob("*")] if baseline.exists() else [])
    for path in trusted_paths:
        if path.is_symlink() or path.stat().st_uid != os.getuid() or path.stat().st_mode & 0o022:
            raise ValueError("Untrusted analysis cache permissions")
    snapshot = CACHE / "source"
    if snapshot.exists():
        shutil.rmtree(snapshot)
    snapshot.mkdir(mode=0o700)
    for name in source:
        destination = snapshot / name
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(ROOT / name, destination)
    (snapshot / ".codeboarding").mkdir()
    shutil.copyfile(ROOT / ".codeboarding/.codeboardingignore", snapshot / ".codeboarding/.codeboardingignore")
    # The snapshot is its own synthetic Git repository: upstream cannot access
    # historical source, upstream remotes or credentials from the actual checkout.
    run(["git", "init", "-q"], snapshot)
    run(["git", "add", "."], snapshot)
    run(["git", "-c", "user.name=Architecture Analyzer", "-c", "user.email=architecture@localhost",
         "-c", "commit.gpgsign=false", "commit", "-qm", "Allowlisted architecture snapshot"], snapshot)
    attempt = Path(tempfile.mkdtemp(prefix="attempt-", dir=CACHE))
    output = attempt / "result"
    if initialize:
        output.mkdir()
    else:
        shutil.copytree(baseline, output)
    shutil.copyfile(ROOT / ".codeboarding/.codeboardingignore", output / ".codeboardingignore")
    home = CACHE / "home"
    home.mkdir(exist_ok=True)
    mode = "full" if initialize else "incremental"
    command = [sys.executable, str(HERE / "codeboarding_entry.py"), mode,
               "--local", str(snapshot), "--output-dir", str(output), "--project-name", "ARTWSLBOt"]
    if initialize:
        command += ["--depth-cap", "1"]
    status = "failed"
    ledger = {}
    try:
        with relay() as (base_url, relay_token, ledger):
            env = {key: os.environ[key] for key in ("PATH", "SYSTEMROOT", "TMPDIR") if key in os.environ}
            env.update({"HOME": str(home), "OPENAI_BASE_URL": base_url, "OPENAI_API_KEY": relay_token,
                        "AGENT_MODEL": CONFIG["model"], "CODEBOARDING_TELEMETRY": "false",
                        "DO_NOT_TRACK": "1", "CODEBOARDING_MAX_CONCURRENT_ENGINES": "1",
                        "CODEBOARDING_LSP_MEMORY_BUDGET_MB": "2048", "CODEBOARDING_LSP_REQUEST_TIMEOUT": "30",
                        "GOPATH": str(CACHE / "go"), "GOCACHE": str(CACHE / "go-build"),
                        "GIT_CONFIG_NOSYSTEM": "1", "PYTHONUNBUFFERED": "1"})
            with (attempt / "stderr.log").open("w") as err, (attempt / "stdout.log").open("w+") as out:
                process = subprocess.Popen(command, cwd=snapshot, env=env, stdout=out, stderr=err,
                                           start_new_session=True)
                try:
                    returncode = process.wait(timeout=CONFIG["max_analysis_seconds"])
                except subprocess.TimeoutExpired:
                    os.killpg(process.pid, signal.SIGTERM)
                    try:
                        process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        os.killpg(process.pid, signal.SIGKILL)
                        process.wait()
                    raise ValueError("Analysis timed out; no baseline promotion or full fallback") from None
                if returncode:
                    raise ValueError(f"CodeBoarding exited {returncode}; private diagnostics: {attempt.name}")
                out.seek(0)
                analysis = validate_result(out.read(), output / "analysis.json", not initialize)
            state = {"schema_version": 1, "settings_hash": settings, "source_hash": digest(source),
                     "source_files": len(source), "commit": run(["git", "rev-parse", "HEAD"]).strip(),
                     "trusted_ref": os.environ.get("GITHUB_REF", "local-owner"),
                     "component_count": len(analysis["components"])}
            promote(output, baseline, state_path, state)
            status = "passed"
    finally:
        report = {"mode": mode, "status": status, "provider": CONFIG["base_url"], "model": CONFIG["model"],
                  "source_files": len(source), "source_hash": digest(source), "usage": ledger,
                  "business_settings_changed": False}
        write_changed(CACHE / "last-run.json", json_text(report))
        print(json.dumps(report))
        if status == "passed":
            shutil.rmtree(attempt)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--initialize", action="store_true", help="Explicit first full analysis (paid)")
    args = parser.parse_args()
    analyze(args.initialize)


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        raise SystemExit(str(error)) from None
