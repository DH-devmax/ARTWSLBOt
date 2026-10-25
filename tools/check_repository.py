"""Review the Git candidate manifest without reading or printing private values."""
import hashlib
import json
import os
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def main():
    candidate = subprocess.run(["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
                               cwd=ROOT, check=True, capture_output=True).stdout.decode().split("\0")
    forbidden = {"node_modules", "var", "bin", ".venv", "__pycache__", ".playwright-cli"}
    for name in filter(None, candidate):
        path = Path(name)
        if forbidden.intersection(path.parts) or path.suffix in {".apk", ".har", ".db", ".pem", ".key"}:
            raise SystemExit(f"Forbidden artifact in repository: {name}")
        if path.name.startswith(".env") and name not in {
                ".env.example", "web/.env", "web/.env.development", "web/.env.production"}:
            raise SystemExit(f"Private environment artifact: {name}")
    lock = json.loads((ROOT / "upstream.lock.json").read_text())
    for source in lock.values():
        assert re.fullmatch("[0-9a-f]{40}", source["commit"]), "Unpinned upstream"
    baseline = json.loads((ROOT / "docs/migrations.lock.json").read_text())
    for name, digest in baseline.items():
        actual = hashlib.sha256((ROOT / "internal/database/migrations" / name).read_bytes()).hexdigest()
        assert actual == digest, f"Applied migration changed: {name}"
    base = os.environ.get("WSL_BASE_SHA", "")
    if re.fullmatch("[0-9a-f]{40}", base) and base != "0" * 40:
        changes = subprocess.run(["git", "diff", "--name-status", base, "HEAD", "--",
                                  "internal/database/migrations"],
                                 cwd=ROOT, check=True, capture_output=True, text=True)
        for line in changes.stdout.splitlines():
            if not line.startswith("A\t"):
                raise SystemExit("Existing migrations are immutable; add a new migration.")
    print("Repository artifacts, upstream pins and migration baseline passed.")


if __name__ == "__main__":
    main()
