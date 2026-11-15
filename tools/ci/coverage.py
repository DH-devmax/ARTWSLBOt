"""Validate and merge provenance-bound Go atomic coverage (standard library only).

Each input directory contains report.json and the .cover/.cover.meta.json pairs
listed by its coverage array. Both unit and all three database package profiles
are required. No percentage is used as a pass/fail threshold.
"""

import argparse
import hashlib
import json
import re
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path, PurePosixPath


ROOT = Path(__file__).resolve().parents[2]
HEX = re.compile(r"[0-9a-f]{64}\Z")
COMMIT = re.compile(r"(?:[0-9a-f]{40}|[0-9a-f]{64})\Z")
ROW = re.compile(r"([^\s]+):([1-9][0-9]*)\.([1-9][0-9]*),"
                 r"([1-9][0-9]*)\.([1-9][0-9]*) ([0-9]+) ([0-9]+)\Z")
MAX_COUNTER = (1 << 63) - 1
BUILD_KEYS = ("GOVERSION", "GOOS", "GOARCH", "CGO_ENABLED")


class CoverageError(ValueError):
    """A safe diagnostic; never contains subprocess output or file contents."""


def require(condition, message):
    if not condition:
        raise CoverageError(message)


def object_pairs(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate JSON key")
        result[key] = value
    return result


def read_json(path):
    try:
        return json.loads(Path(path).read_text(encoding="utf-8"),
                          object_pairs_hook=object_pairs)
    except (OSError, UnicodeError, ValueError):
        raise CoverageError("invalid or unavailable JSON artifact") from None


def write_json(path, value):
    Path(path).write_text(json.dumps(value, indent=2, sort_keys=True) + "\n",
                          encoding="utf-8")


def file_hash(path):
    try:
        return hashlib.sha256(Path(path).read_bytes()).hexdigest()
    except OSError:
        raise CoverageError("source file unavailable") from None


def source_digest(files):
    return hashlib.sha256(json.dumps(files, sort_keys=True, separators=(",", ":"))
                          .encode("utf-8")).hexdigest()


def relative_path(value):
    require(isinstance(value, str) and value != "", "invalid relative path")
    path = PurePosixPath(value)
    require(not path.is_absolute() and "\\" not in value
            and not any(ord(character) < 32 or ord(character) == 127 for character in value)
            and all(part not in ("", ".", "..") for part in value.split("/")),
            "invalid relative path")
    return path


def inside(root, relative):
    relative_path(relative)
    root = Path(root).resolve()
    path = (root / relative).resolve()
    require(path.is_relative_to(root), "artifact or source escapes its directory")
    return path


@dataclass(frozen=True, order=True)
class Block:
    file: str
    start_line: int
    start_column: int
    end_line: int
    end_column: int
    statements: int
    count: int

    @property
    def location(self):
        return (self.file, self.start_line, self.start_column,
                self.end_line, self.end_column)

    def row(self):
        return (f"{self.file}:{self.start_line}.{self.start_column},"
                f"{self.end_line}.{self.end_column} {self.statements} {self.count}")


def parse_profile(text):
    require(isinstance(text, str), "coverage must be text")
    lines = text.splitlines()
    require(lines and lines[0] == "mode: atomic", "coverage mode must be atomic")
    result = {}
    for line in lines[1:]:
        match = ROW.fullmatch(line)
        require(match is not None, "invalid coverage block")
        name = match[1]
        relative_path(name)
        try:
            values = [int(value) for value in match.groups()[1:]]
        except ValueError:
            raise CoverageError("invalid coverage integer") from None
        sl, sc, el, ec, statements, count = values
        require((el, ec) > (sl, sc) or ((el, ec) == (sl, sc) and statements == 0),
                "invalid coverage coordinates")
        require(all(value <= MAX_COUNTER for value in values),
                "coverage integer exceeds Go int64")
        block = Block(name, sl, sc, el, ec, statements, count)
        old = result.get(block.location)
        if old:
            require(old.statements == statements, "conflicting statement counts")
            count += old.count
            require(count <= MAX_COUNTER, "coverage counter overflow")
            block = Block(name, sl, sc, el, ec, statements, count)
        result[block.location] = block
    return result


def profile_text(blocks):
    return "mode: atomic\n" + "".join(block.row() + "\n"
                                      for block in sorted(blocks.values()))


def validate_identity(meta):
    require(isinstance(meta, dict) and type(meta.get("schema")) is int
            and meta["schema"] == 1, "unsupported coverage metadata schema")
    require(isinstance(meta.get("sha"), str) and COMMIT.fullmatch(meta["sha"]) is not None,
            "invalid source commit")
    files = meta.get("source_files")
    require(isinstance(files, dict) and bool(files), "source inventory absent")
    for path, digest in files.items():
        parsed = relative_path(path)
        require(path in ("go.mod", "go.sum") or parsed.parts[0]
                in ("cmd", "internal", "tools", "third_party"),
                "source inventory outside owned roots")
        require(isinstance(digest, str) and HEX.fullmatch(digest) is not None,
                "invalid source checksum")
    require(meta.get("source_digest") == source_digest(files),
            "source inventory digest mismatch")
    require("go.mod" in files, "module source absent from inventory")
    toolchain = meta.get("toolchain")
    require(isinstance(toolchain, dict) and set(toolchain) == set(BUILD_KEYS),
            "incomplete toolchain")
    require(all(isinstance(toolchain[key], str) and toolchain[key]
                for key in BUILD_KEYS), "invalid toolchain")
    require(toolchain["CGO_ENABLED"] == "1", "race/sodium requires CGO")
    require(meta.get("tags") == ["sodium"] and meta.get("race") is True
            and meta.get("mode") == "atomic", "incompatible build settings")
    require(meta.get("status") == "passed", "coverage run did not pass")
    require(isinstance(meta.get("module"), str), "source module absent")
    relative_path(meta["module"])
    inventory = meta.get("coverage_inventory")
    require(isinstance(inventory, dict) and bool(inventory),
            "compiled coverage source inventory absent")
    for name, item in inventory.items():
        path = relative_path(name)
        require(path.parts[0] in ("cmd", "internal", "tools") and path.suffix == ".go"
                and not path.name.endswith("_test.go") and name in files,
                "compiled coverage source outside inventory")
        require(isinstance(item, dict) and set(item) == {"package", "instrumented"}
                and type(item["instrumented"]) is bool
                and item["package"] == meta["module"] + "/" + str(path.parent),
                "invalid compiled coverage source")


def validate_scope(meta, blocks):
    scope = meta.get("scope")
    require(isinstance(scope, dict) and scope.get("profile") in ("unit", "database"),
            "invalid coverage scope")
    packages = scope.get("packages")
    require(isinstance(packages, list) and bool(packages)
            and all(isinstance(p, str) and p for p in packages)
            and len(packages) == len(set(packages)), "invalid package scope")
    for package in packages:
        relative_path(package)
        require(any(package.startswith(meta["module"] + "/" + name + "/")
                    or package == meta["module"] + "/" + name
                    for name in ("cmd", "internal", "tools")),
                "package outside owned roots")
    if scope["profile"] == "database":
        require(scope.get("database") == "wsl_test" and len(packages) == 1
                and packages[0] in [meta["module"] + "/internal/" + name
                                    for name in ("control", "httpapi", "gateway")],
                "invalid database coverage scope")
    else:
        require(set(packages) == {item["package"] for item in meta["coverage_inventory"].values()},
                "unit package coverage incomplete")
    mapping = meta.get("files")
    require(isinstance(mapping, dict), "coverage source mapping absent")
    require(set(mapping) == {block.file for block in blocks.values()},
            "coverage source mapping mismatch")
    expected = {
        item["package"] + "/" + PurePosixPath(path).name
        for path, item in meta["coverage_inventory"].items()
        if item["package"] in packages and item["instrumented"]
    }
    require(set(mapping) == expected, "compiled coverage files missing or unexpected")
    paths = set()
    for name, source in mapping.items():
        require(isinstance(source, dict)
                and set(source) == {"path", "sha256", "package"},
                "invalid coverage source mapping")
        relative_path(name)
        path = relative_path(source["path"])
        require(source["path"] not in paths, "duplicate coverage source alias")
        paths.add(source["path"])
        require(source["package"] in packages
                and name == source["package"] + "/" + path.name
                and source["package"] == "/".join(
                    part for part in (meta["module"], str(path.parent))
                    if part != "."),
                "coverage file outside package scope")
        require(meta["source_files"].get(source["path"]) == source["sha256"],
                "coverage file source checksum mismatch")


def compatible(left, right):
    for key in ("sha", "source_digest", "source_files", "coverage_inventory",
                "toolchain", "tags", "race", "mode", "module"):
        require(left[key] == right[key], "incompatible coverage provenance")


def validate_sources(root, meta):
    for name, expected in meta["source_files"].items():
        require(file_hash(inside(root, name)) == expected,
                "current source differs from coverage source")
    try:
        module = re.search(r"(?m)^module\s+(\S+)\s*$",
                           inside(root, "go.mod").read_text(encoding="utf-8"))
    except (OSError, UnicodeError):
        raise CoverageError("module source unavailable") from None
    require(module is not None and module[1] == meta["module"], "source module mismatch")


def load_bundle(directory, root):
    directory = Path(directory).resolve()
    report = read_json(directory / "report.json")
    require(isinstance(report, dict) and type(report.get("schema")) is int and report["schema"] == 1
            and report.get("status") == "passed", "test report did not pass")
    require(report.get("profile") in ("unit", "database"), "invalid report profile")
    artifacts = report.get("coverage")
    require(isinstance(artifacts, list) and bool(artifacts), "coverage artifacts absent")
    seen = set()
    result = []
    for artifact in artifacts:
        require(isinstance(artifact, dict) and set(artifact) == {"file", "metadata"},
                "invalid coverage artifact reference")
        path = inside(directory, artifact["file"])
        sidecar = inside(directory, artifact["metadata"])
        require(path.suffix == ".cover" and sidecar == Path(str(path) + ".meta.json"),
                "invalid coverage sidecar")
        require(path not in seen, "duplicate coverage artifact")
        seen.add(path)
        meta = read_json(sidecar)
        validate_identity(meta)
        require(meta.get("cover_sha256") == file_hash(path),
                "coverage artifact checksum mismatch")
        try:
            blocks = parse_profile(path.read_text(encoding="utf-8"))
        except (OSError, UnicodeError):
            raise CoverageError("coverage artifact unavailable") from None
        validate_scope(meta, blocks)
        require(report["profile"] == meta["scope"]["profile"],
                "report and artifact profile mismatch")
        for key in ("sha", "source_digest", "toolchain", "coverage_inventory"):
            require(report.get(key) == meta[key], "report and artifact provenance mismatch")
        validate_sources(root, meta)
        result.append((meta, blocks))
    if report["profile"] == "database":
        require(len(result) == 3 and
                {meta["scope"]["packages"][0].rsplit("/", 1)[-1] for meta, _ in result}
                == {"control", "httpapi", "gateway"}, "database package coverage incomplete")
    else:
        require(len(result) == 1, "unit coverage must have one artifact")
    return result


def merge_profiles(profiles):
    require(bool(profiles), "no coverage profiles")
    baseline = profiles[0][0]
    merged = {}
    files = {}
    layouts = {}
    package_layouts = {}
    scopes = []
    for meta, blocks in profiles:
        validate_identity(meta)
        validate_scope(meta, blocks)
        compatible(baseline, meta)
        layout = {}
        for block in blocks.values():
            layout.setdefault(block.file, set()).add(block.location[1:] + (block.statements,))
        for package in meta["scope"]["packages"]:
            package_layout = {name: coordinates for name, coordinates in layout.items()
                              if meta["files"][name]["package"] == package}
            if package in package_layouts:
                require(package_layouts[package] == package_layout,
                        "coverage package layout changed")
            package_layouts[package] = package_layout
        for name, coordinates in layout.items():
            if name in layouts:
                require(layouts[name] == coordinates, "coverage block layout changed")
                require(files[name] == meta["files"][name], "coverage source mapping changed")
            layouts[name] = coordinates
            files[name] = meta["files"][name]
        for location, block in blocks.items():
            old = merged.get(location)
            if old:
                count = old.count + block.count
                require(count <= MAX_COUNTER, "coverage counter overflow")
                block = Block(*location, block.statements, count)
            merged[location] = block
        scopes.append(meta["scope"])
    return baseline, merged, files, scopes


def totals(blocks):
    statements = sum(b.statements for b in blocks)
    covered = sum(b.statements for b in blocks if b.count > 0)
    return {"statements": statements, "covered": covered,
            "percent": round(100 * covered / statements, 4) if statements else None}


def summarize(blocks, files, changed=None):
    by_file = {}
    by_package = {}
    for block in blocks.values():
        source = files[block.file]
        by_file.setdefault(source["path"], []).append(block)
        by_package.setdefault(source["package"], []).append(block)
    summary = {"total": totals(list(blocks.values())),
               "packages": {name: totals(items) for name, items in sorted(by_package.items())},
               "files": {name: totals(items) for name, items in sorted(by_file.items())}}
    if changed is not None:
        summary["changed_files"] = {
            name: dict(totals(by_file.get(name, [])), instrumented=name in by_file)
            for name in sorted(set(changed))
        }
        selected = [block for name in set(changed) for block in by_file.get(name, [])]
        summary["changed_total"] = totals(selected)
    return summary


def changed_files(root, base):
    try:
        commit = subprocess.run(["git", "rev-parse", "--verify", "--end-of-options",
                                 base + "^{commit}"], cwd=root, stdout=subprocess.PIPE,
                                stderr=subprocess.DEVNULL, text=True, timeout=30)
        require(commit.returncode == 0 and COMMIT.fullmatch(commit.stdout.strip()) is not None,
                "invalid coverage base")
        result = subprocess.run(["git", "diff", "--name-only", "-z", "--diff-filter=ACMR",
                                 commit.stdout.strip(), "--"], cwd=root,
                                stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                                timeout=30)
        require(result.returncode == 0, "changed-file discovery failed")
        names = result.stdout.decode("utf-8").split("\0")
    except (OSError, UnicodeError, subprocess.SubprocessError):
        raise CoverageError("changed-file discovery failed") from None
    return [name for name in names if name.endswith(".go")
            and relative_path(name).parts[0] in ("cmd", "internal", "tools")]


def merge_directories(inputs, output, root=ROOT, base=None):
    directories = [Path(path).resolve() for path in inputs]
    require(len(set(directories)) == len(directories), "duplicate input directory")
    output = Path(output).resolve()
    require(output not in directories, "output must differ from inputs")
    profiles = [profile for directory in directories for profile in load_bundle(directory, root)]
    require({meta["scope"]["profile"] for meta, _ in profiles} == {"unit", "database"},
            "both unit and database coverage are required")
    baseline, blocks, files, scopes = merge_profiles(profiles)
    require(any(block.statements > 0 for block in blocks.values()),
            "merged coverage has no statements")
    changed = changed_files(root, base) if base else None
    summary = summarize(blocks, files, changed)
    summary.update({"schema": 1, "sha": baseline["sha"], "status": "passed",
                    "source_digest": baseline["source_digest"],
                    "toolchain": baseline["toolchain"], "scopes": scopes})
    output.mkdir(parents=True, exist_ok=True)
    require(not any((output / name).exists() for name in
                    ("merged.cover", "merged.cover.meta.json", "summary.json")),
            "coverage output already exists")
    cover = output / "merged.cover"
    cover.write_text(profile_text(blocks), encoding="utf-8")
    metadata = {key: baseline[key] for key in
                ("schema", "sha", "source_digest", "source_files", "coverage_inventory",
                 "toolchain", "tags",
                 "race", "mode", "status", "module")}
    metadata.update({"files": files, "scopes": scopes, "cover_sha256": file_hash(cover)})
    write_json(output / "merged.cover.meta.json", metadata)
    write_json(output / "summary.json", summary)
    return summary


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", action="append", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--base", help="Optional Git base for changed Go file summaries")
    args = parser.parse_args(argv)
    try:
        summary = merge_directories(args.input, args.output, args.root, args.base)
    except (CoverageError, OSError, TypeError, KeyError, ValueError):
        print("Coverage validation or merge failed", file=sys.stderr)
        return 1
    print(json.dumps({"schema": 1, "status": "passed", "total": summary["total"]}))
    return 0


if __name__ == "__main__":
    sys.exit(main())
