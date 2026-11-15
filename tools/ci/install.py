"""Install pinned verification binaries outside production dependency manifests."""

import argparse
import hashlib
import os
from pathlib import Path
import platform
import re
import shutil
import sys
import tarfile
import tempfile
from urllib.request import urlopen

try:
    from .common import ROOT, load_json, run
except ImportError:
    from common import ROOT, load_json, run

NAMES = {"ruff", "staticcheck", "govulncheck", "codeql"}


def download(asset, destination):
    expected = asset["sha256"]
    if not re.fullmatch(r"[a-f0-9]{64}", expected) or not asset["url"].startswith(
            "https://github.com/"):
        raise ValueError("Untrusted tool asset")
    digest = hashlib.sha256()
    size = 0
    with urlopen(asset["url"], timeout=60) as response, destination.open("xb") as handle:
        while block := response.read(1024 * 1024):
            size += len(block)
            if size > 1500 * 1024 * 1024:
                raise ValueError("Tool asset exceeds its bound")
            digest.update(block)
            handle.write(block)
    if digest.hexdigest() != expected:
        destination.unlink()
        raise ValueError("Tool asset checksum mismatch")


def ruff_binary(archive, destination):
    with tarfile.open(archive, "r:gz") as handle:
        matches = [entry for entry in handle.getmembers()
                   if Path(entry.name).name == "ruff"]
        if (len(matches) != 1 or not matches[0].isfile()
                or matches[0].size > 200 * 1024 * 1024):
            raise ValueError("Untrusted Ruff archive layout")
        with destination.open("xb") as output:
            shutil.copyfileobj(handle.extractfile(matches[0]), output)
    destination.chmod(0o755)


def go_tool(name, specification, folder, environment):
    target = f"{specification['module']}@{specification['module_version']}"
    completed = run(["go", "mod", "download", "-json", target], cwd=folder,
                    timeout=300, env=environment)
    if completed.returncode:
        raise ValueError("Pinned Go module download failed")
    import json
    metadata = json.loads(completed.stdout)
    if (metadata.get("Sum") != specification["module_sum"]
            or metadata.get("GoModSum") != specification["go_mod_sum"]):
        raise ValueError("Pinned Go module checksum mismatch")
    completed = run(["go", "install", f"{specification['package']}@{specification['module_version']}"],
                    cwd=folder, timeout=900, env=environment)
    if completed.returncode:
        raise ValueError(f"Pinned {name} build failed")


def install(names):
    manifest = load_json(ROOT / "tools/ci/toolchain.json")
    if manifest.get("schema_version") != 1 or set(names) - NAMES or not names:
        raise ValueError("Invalid tool request")
    temporary_root = Path(os.environ.get("RUNNER_TEMP", ROOT / "var/ci-tools")).resolve()
    binary = temporary_root / "ci-bin"
    binary.mkdir(parents=True, exist_ok=True)
    environment = {**os.environ, **manifest["go_tools_environment"], "GOBIN": str(binary)}
    for name in names:
        specification = manifest["tools"][name]
        if name in {"staticcheck", "govulncheck"}:
            with tempfile.TemporaryDirectory(prefix="wsl-ci-go-tool-") as folder:
                go_tool(name, specification, Path(folder), environment)
        elif name == "ruff":
            architecture = {"arm64": "aarch64", "aarch64": "aarch64",
                            "x86_64": "x86_64", "AMD64": "x86_64"}.get(platform.machine())
            system = {"darwin": "apple-darwin", "linux": "unknown-linux-gnu"}.get(sys.platform)
            key = f"{architecture}-{system}"
            if key not in specification["assets"]:
                raise ValueError("Unsupported verification platform")
            with tempfile.TemporaryDirectory(prefix="wsl-ci-ruff-") as folder:
                archive = Path(folder) / "ruff.tar.gz"
                download(specification["assets"][key], archive)
                staged = Path(folder) / "ruff"
                ruff_binary(archive, staged)
                staged.replace(binary / "ruff")
        else:
            if sys.platform != "linux" or platform.machine() not in {"x86_64", "AMD64"}:
                raise ValueError("CodeQL bundle is Linux amd64 only")
            destination = temporary_root / "codeql-bundle.tar.gz"
            if destination.exists():
                destination.unlink()
            download(specification["assets"]["linux64"], destination)
            continue
        checked = run([str(binary / name), *specification["version_args"]], timeout=60)
        if (checked.returncode or not re.search(specification["version_pattern"],
                (checked.stdout or "") + (checked.stderr or ""))):
            raise ValueError("Installed verification tool version mismatch")
        print(f"Verified {name} {specification['version']}")
    if os.environ.get("GITHUB_PATH"):
        with Path(os.environ["GITHUB_PATH"]).open("a") as handle:
            handle.write(str(binary) + "\n")
    else:
        print(f"Verification binaries: {binary}")
    return 0


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--tools", nargs="+", required=True, choices=sorted(NAMES))
    arguments = parser.parse_args()
    try:
        sys.exit(install(arguments.tools))
    except Exception:
        print("Pinned verification tool installation failed.", file=sys.stderr)
        sys.exit(1)
