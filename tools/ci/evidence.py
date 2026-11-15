"""A small artifact allowlist; never archive the checkout or private fixtures."""

import argparse
from pathlib import Path
import re
import sys

try:
    from .common import digest, load_json, revision, write_json
except ImportError:
    from common import digest, load_json, revision, write_json

JOBS = ("quality", "unit", "database", "frontend", "browser", "security",
        "architecture", "coverage", "images")
SUFFIXES = {".json", ".cover", ".md", ".mmd", ".sarif", ".svg"}
REQUIRED = {
    "quality": {"format/lint.json", "staticcheck/lint.json", "ruff/lint.json",
                "python/report.json"},
    "unit": {"report.json", "unit.cover", "unit.cover.meta.json",
             "gopsutil-boundaries.json"},
    "database": {"report.json", *(
        f"database-{package}{suffix}" for package in ("control", "httpapi", "gateway")
        for suffix in (".cover", ".cover.meta.json"))},
    "frontend": {"eslint/lint.json", "node/report.json", "build.json"},
    "browser": {"synthetic/report.json", "container.json", "authenticated.json"},
    "security": {"vulnerabilities/report.json", "codeql/report.json",
                 "codeql/go.sarif", "codeql/javascript.sarif"},
    "architecture": {"check.json", "candidate/graph.json"},
    "coverage": {"summary.json", "merged.cover", "merged.cover.meta.json"},
    "images": {"api.json", "web.json"},
}


def complete(job, files):
    if REQUIRED[job] - set(files):
        raise ValueError("Missing required job evidence")
    if job == "architecture" and not any(
            name.startswith("rendered/") and name.endswith(".svg") for name in files):
        raise ValueError("Missing rendered architecture evidence")


def validate_content(path, sha):
    if path.name in {"report.json", "lint.json"}:
        report = load_json(path)
        if not isinstance(report, dict) or report.get("sha") != sha or report.get("status") != "passed":
            raise ValueError("Failed or stale profile evidence")
    elif path.name == "gopsutil-boundaries.json":
        if load_json(path) != {"schema": 1, "sha": sha,
                              "check": "gopsutil-boundaries", "status": "passed"}:
            raise ValueError("Failed or stale metrics dependency evidence")


def record(job, folder, sha):
    files = {}
    for path in sorted(folder.rglob("*")):
        if not path.is_file() or path.name == "job-report.json":
            continue
        relative = path.relative_to(folder)
        if (path.is_symlink() or path.suffix not in SUFFIXES
                or any(part.startswith(".") for part in relative.parts)):
            raise ValueError("Unapproved report artifact")
        validate_content(path, sha)
        files[relative.as_posix()] = digest(path)
    if not files:
        raise ValueError("Missing job evidence")
    complete(job, files)
    value = {"schema": 1, "sha": sha, "job": job, "status": "passed", "files": files}
    write_json(folder / "job-report.json", value)
    return value


def verify(value, folder, sha):
    if (value.get("schema") != 1 or value.get("sha") != sha
            or value.get("job") not in JOBS or value.get("status") != "passed"
            or not value.get("files")):
        raise ValueError("Invalid, failed or stale job receipt")
    complete(value["job"], value["files"])
    for name, expected in value["files"].items():
        path = Path(name)
        if path.is_absolute() or ".." in path.parts or path.suffix not in SUFFIXES:
            raise ValueError("Invalid artifact path")
        actual = folder / path
        if actual.is_symlink() or not re.fullmatch(r"[a-f0-9]{64}", expected):
            raise ValueError("Invalid artifact integrity metadata")
        if digest(actual) != expected:
            raise ValueError("Artifact integrity failure")
        validate_content(actual, sha)
    return value["job"]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--job", choices=JOBS, required=True)
    parser.add_argument("--directory", required=True, type=Path)
    options = parser.parse_args()
    record(options.job, options.directory, revision())
    return 0


if __name__ == "__main__":
    sys.exit(main())
