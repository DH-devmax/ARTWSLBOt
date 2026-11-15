"""Private, repository/ref/toolchain-scoped advisory baselines. No model calls."""
from contextlib import contextmanager
from datetime import datetime, timezone
import fcntl
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import tempfile

from common import digest, json_text, run
from provider import private_dir

CACHE_ROOT = Path.home() / ".cache/artwslbot/architecture/analysis"
ARCHIVE_ROOT = Path.home() / ".local/share/artwslbot/architecture/baselines"
RUNNER_FILES = ("analyze.py", "baseline.py", "codeboarding_entry.py", "provider.py",
                "common.py", "requirements.lock")


class BaselineError(ValueError):
    def __init__(self, code):
        self.code = code
        super().__init__(code + "; preserve existing caches and review the trusted baseline")


def context(root, here, cache_root=None, archive_root=None):
    ci = os.environ.get("GITHUB_ACTIONS") == "true"
    if ci:
        repository = os.environ.get("GITHUB_REPOSITORY", "")
        ref = os.environ.get("GITHUB_REF", "")
        if not repository or not ref.startswith("refs/heads/") or os.environ.get(
            "GITHUB_EVENT_NAME", ""
        ) in ("pull_request", "pull_request_target"):
            raise BaselineError("untrusted_ci_origin")
    else:
        common = Path(run(["git", "rev-parse", "--git-common-dir"], root).strip())
        repository = str((root / common).resolve())
        branch = subprocess.run(["git", "symbolic-ref", "-q", "HEAD"], cwd=root,
                                capture_output=True, text=True)
        ref = branch.stdout.strip() if branch.returncode == 0 else (
            "detached:" + run(["git", "rev-parse", "HEAD"], root).strip())
    settings = digest({
        "toolchain": json.loads((here / "toolchain.json").read_text()),
        "ignore": (root / ".codeboarding/.codeboardingignore").read_text(),
        "runner": [hashlib.sha256((here / name).read_bytes()).hexdigest() for name in RUNNER_FILES],
    })
    parent = cache_root or CACHE_ROOT
    repository_id, ref_id = digest(repository)[:24], digest(ref)[:24]
    scope = Path("v2") / repository_id / ref_id / settings
    return {"cache": parent / scope,
            "archive": (archive_root or ARCHIVE_ROOT) / scope,
            "archive_root": archive_root or ARCHIVE_ROOT,
            "cache_root": parent, "root": root, "repository_id": repository_id,
            "trusted_ref": ref, "settings_hash": settings,
            "commit": run(["git", "rev-parse", "HEAD"], root).strip()}


def prepare(ctx):
    path = ctx["cache_root"]
    private_dir(path)
    for part in ctx["cache"].relative_to(path).parts:
        path = path / part
        private_dir(path)
        if path.stat().st_uid != os.getuid():
            raise BaselineError("untrusted_cache_owner")
    return path


def prepare_archive(ctx):
    path = ctx["archive_root"]
    private_dir(path)
    for part in ctx["archive"].relative_to(path).parts:
        path = path / part
        private_dir(path)
        if path.stat().st_uid != os.getuid():
            raise BaselineError("untrusted_archive_owner")
    return path


@contextmanager
def locked(ctx):
    fd = os.open(ctx["cache"] / "run.lock", os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW, 0o600)
    try:
        metadata = os.fstat(fd)
        if (not stat.S_ISREG(metadata.st_mode) or metadata.st_uid != os.getuid()
                or metadata.st_mode & 0o077):
            raise BaselineError("untrusted_lock")
        try:
            fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise BaselineError("analysis_in_progress") from None
        yield
    finally:
        os.close(fd)


def validate(ctx, initialize=False):
    cache = ctx["cache"]
    metadata = cache.lstat()
    if (not stat.S_ISDIR(metadata.st_mode) or metadata.st_uid != os.getuid()
            or metadata.st_mode & 0o077):
        raise BaselineError("untrusted_baseline_root")
    baseline, state_path = cache / "baseline", cache / "state.json"
    if os.path.lexists(cache / "previous-baseline"):
        raise BaselineError("interrupted_promotion")
    if initialize:
        if (os.path.lexists(baseline) or os.path.lexists(state_path)
                or (ctx["archive"] / "latest.json").exists()):
            raise BaselineError("baseline_already_exists")
        return
    if baseline.is_symlink() or state_path.is_symlink():
        raise BaselineError("untrusted_baseline")
    if not state_path.is_file() or not (baseline / "analysis.json").is_file():
        raise BaselineError("compatible_baseline_missing")
    for path in (state_path, baseline, *baseline.rglob("*")):
        metadata = path.lstat()
        if (not (stat.S_ISREG(metadata.st_mode) or stat.S_ISDIR(metadata.st_mode))
                or metadata.st_uid != os.getuid() or metadata.st_mode & 0o022):
            raise BaselineError("untrusted_baseline")
    try:
        state = json.loads(state_path.read_text())
    except (ValueError, OSError):
        raise BaselineError("invalid_baseline_state") from None
    if not isinstance(state, dict) or state.get("schema_version") != 2:
        raise BaselineError("incompatible_baseline_schema")
    for key in ("repository_id", "trusted_ref", "settings_hash"):
        if state.get(key) != ctx[key]:
            raise BaselineError("baseline_identity_mismatch")
    commit = state.get("commit", "")
    if not isinstance(commit, str) or len(commit) != 40 or any(c not in "0123456789abcdef" for c in commit):
        raise BaselineError("invalid_baseline_commit")
    result = subprocess.run(["git", "merge-base", "--is-ancestor", commit, ctx["commit"]],
                            cwd=ctx["root"], capture_output=True)
    if result.returncode:
        raise BaselineError("baseline_not_ancestor")
    expected = state.get("artifacts")
    if not isinstance(expected, dict) or not expected:
        raise BaselineError("baseline_integrity_missing")
    if expected != artifact_hashes(baseline):
        raise BaselineError("baseline_integrity_mismatch")
    return state


def artifact_hashes(directory):
    if directory.is_symlink() or not directory.is_dir():
        raise BaselineError("untrusted_baseline")
    files = {}
    for path in sorted(directory.rglob("*")):
        metadata = path.lstat()
        if (not (stat.S_ISREG(metadata.st_mode) or stat.S_ISDIR(metadata.st_mode))
                or metadata.st_uid != os.getuid() or metadata.st_mode & 0o022):
            raise BaselineError("untrusted_baseline")
        if path.is_file():
            files[str(path.relative_to(directory))] = hashlib.sha256(path.read_bytes()).hexdigest()
    # Upstream needs all three to run incrementally, not just the rendered JSON.
    if not {"analysis.json", "fingerprint.json", "static_analysis.pkl", "static_analysis.sha"} <= files.keys():
        raise BaselineError("baseline_incomplete")
    return files


def atomic_json(path, value):
    fd, name = tempfile.mkstemp(prefix="state-", dir=path.parent)
    try:
        with os.fdopen(fd, "w") as out:
            out.write(json_text(value))
            out.flush()
            os.fsync(out.fileno())
        os.replace(name, path)
    finally:
        if Path(name).exists():
            Path(name).unlink()


def checkpoint(ctx):
    """Keep immutable private checkpoints outside disposable cache storage."""
    state = validate(ctx)
    archive = prepare_archive(ctx)
    identifier = digest(state)
    destination = archive / identifier
    if not destination.exists():
        temporary = Path(tempfile.mkdtemp(prefix="checkpoint-", dir=archive))
        try:
            shutil.copytree(ctx["cache"] / "baseline", temporary / "baseline")
            atomic_json(temporary / "state.json", state)
            validate({**ctx, "cache": temporary})
            os.replace(temporary, destination)
        finally:
            if temporary.exists():
                shutil.rmtree(temporary)
    validate({**ctx, "cache": destination})
    atomic_json(archive / "latest.json", {"checkpoint": identifier})


def restore(ctx):
    """Restore only an exact, intact trusted scope; never initialize or relabel."""
    cache = ctx["cache"]
    if any(os.path.lexists(cache / name) for name in ("baseline", "state.json", "previous-baseline")):
        return False
    archive = prepare_archive(ctx)
    pointer = archive / "latest.json"
    if not pointer.exists():
        return False
    metadata = pointer.lstat()
    if (not stat.S_ISREG(metadata.st_mode) or metadata.st_uid != os.getuid()
            or metadata.st_mode & 0o077):
        raise BaselineError("untrusted_archive_pointer")
    try:
        identifier = json.loads(pointer.read_text())["checkpoint"]
    except (ValueError, KeyError, TypeError):
        raise BaselineError("invalid_archive_pointer") from None
    if (not isinstance(identifier, str) or len(identifier) != 64
            or any(c not in "0123456789abcdef" for c in identifier)):
        raise BaselineError("invalid_archive_pointer")
    source = archive / identifier
    state = validate({**ctx, "cache": source})
    if digest(state) != identifier:
        raise BaselineError("archive_identity_mismatch")
    temporary = Path(tempfile.mkdtemp(prefix="restore-", dir=cache))
    try:
        shutil.copytree(source / "baseline", temporary / "baseline")
        atomic_json(temporary / "state.json", state)
        validate({**ctx, "cache": temporary})
        os.replace(temporary / "baseline", cache / "baseline")
        try:
            os.replace(temporary / "state.json", cache / "state.json")
        except BaseException:
            shutil.rmtree(cache / "baseline")
            raise
    finally:
        shutil.rmtree(temporary)
    return True


def report(ctx, value):
    value = {**value, "schema_version": 2, "repository_id": ctx["repository_id"],
             "trusted_ref": ctx["trusted_ref"], "settings_hash": ctx["settings_hash"],
             "commit": ctx["commit"], "ci_run_id": os.environ.get("GITHUB_RUN_ID"),
             "ci_run_attempt": os.environ.get("GITHUB_RUN_ATTEMPT"),
             "reported_at": datetime.now(timezone.utc).isoformat(),
             "legacy_cache_present": (ctx["cache_root"] / "state.json").exists()}
    fd, name = tempfile.mkstemp(prefix="report-", dir=ctx["cache"])
    try:
        with os.fdopen(fd, "w") as out:
            out.write(json_text(value))
            out.flush()
            os.fsync(out.fileno())
        os.replace(name, ctx["cache"] / "last-run.json")
    finally:
        if Path(name).exists():
            Path(name).unlink()
    print(json.dumps(value))
