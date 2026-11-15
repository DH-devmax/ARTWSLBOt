"""Shared source inventory primitives; independent of diagram extraction rules."""
import hashlib
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent


def run(args, root=ROOT, **kwargs):
    return subprocess.check_output(args, cwd=root, text=True, **kwargs)


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def json_text(value):
    return json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + "\n"


def candidate_files(root=ROOT):
    raw = run(["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"], root)
    result = []
    for name in sorted(set(filter(None, raw.split("\0")))):
        path = root / name
        if path.is_symlink():
            if name.startswith(("cmd/", "internal/", "web/src/wsl/", "web/src/components/core/charts/")):
                raise ValueError("Product source symlink rejected: " + name)
            continue
        if path.is_file():
            if not path.resolve().is_relative_to(root.resolve()):
                raise ValueError("Source escapes repository: " + name)
            result.append(name)
    return result
