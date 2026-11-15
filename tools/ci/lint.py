#!/usr/bin/env python3
"""Product lint with counted, source-bound legacy findings and fail-closed scanners."""

from __future__ import annotations

import argparse
from collections import Counter
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys
from typing import Any

try:
    from tools.ci.common import revision, run as run_process
except ModuleNotFoundError:
    from common import revision, run as run_process

ROOT = Path(__file__).resolve().parents[2]
CHECKS = {
    "format": {"gofmt", "prettier"},
    "eslint": {"eslint"},
    "staticcheck": {"staticcheck"},
    "ruff": {"ruff"},
}
CHECKS["all"] = set().union(*CHECKS.values(), {"govulncheck"})
BASELINE_TOOLS = CHECKS["all"] - {"govulncheck"}
KEY_FIELDS = ("tool", "file", "rule", "code_fingerprint")
MAX_ENTRIES = 2000
MAX_DIAGNOSTICS = 4000
TIMEOUT = 900


class ToolFailure(RuntimeError):
    """An incomplete check, never a baselinable diagnostic."""


def unique_object(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON key: {key}")
        result[key] = value
    return result


def reject_constant(value: str) -> Any:
    raise ValueError(f"non-JSON numeric constant: {value}")


def load_json(text: str) -> Any:
    return json.loads(
        text, object_pairs_hook=unique_object, parse_constant=reject_constant
    )


def json_stream(text: str) -> list[dict[str, Any]]:
    decoder = json.JSONDecoder(
        object_pairs_hook=unique_object, parse_constant=reject_constant
    )
    values = []
    remaining = text.strip()
    while remaining:
        value, offset = decoder.raw_decode(remaining)
        if not isinstance(value, dict):
            raise ValueError("expected a JSON object")
        values.append(value)
        remaining = remaining[offset:].lstrip()
    return values


def digest(code: bytes) -> str:
    return hashlib.sha256(code).hexdigest()


def key(entry: dict[str, Any]) -> tuple[str, ...]:
    return tuple(entry[field] for field in KEY_FIELDS)


def baseline_allowed(tool: str, rule: str) -> bool:
    if tool in {"gofmt", "prettier"}:
        return rule == "format"
    if tool == "staticcheck":
        return bool(re.fullmatch(r"(?:SA|ST|S|U)\d{4}", rule))
    if tool == "ruff":
        return bool(re.fullmatch(r"(?:E[47]\d{2}|E9(?!99)\d{2}|F\d{3})", rule))
    if tool == "eslint":
        # Parser failures and security-sensitive sinks do not become legacy style.
        return bool(
            re.fullmatch(r"(?:@[a-z0-9-]+/)?[a-z][a-z0-9-]*(?:/[a-z][a-z0-9-]*)?", rule)
        ) and not (
            rule.startswith(("security/", "no-unsanitized/", "scan/"))
            or rule in {"parse-error", "vue/no-v-html", "no-eval", "no-implied-eval"}
        )
    return False


def legacy_eligible(entry: dict[str, Any]) -> bool:
    return baseline_allowed(entry["tool"], entry["rule"]) and not entry[
        "file"
    ].startswith("tools/ci/")


def validate_baseline(data: Any) -> list[dict[str, Any]]:
    if not isinstance(data, dict) or set(data) != {"schema_version", "entries"}:
        raise ValueError("baseline must contain schema_version and entries")
    if data["schema_version"] != 1 or not isinstance(data["entries"], list):
        raise ValueError("unsupported baseline schema")
    entries = data["entries"]
    seen = set()
    for entry in entries:
        if not isinstance(entry, dict) or set(entry) != {*KEY_FIELDS, "count"}:
            raise ValueError(
                "baseline entries require exact tool/file/rule/fingerprint/count"
            )
        if not all(isinstance(entry[field], str) for field in KEY_FIELDS):
            raise ValueError("baseline key fields must be strings")
        filename = PurePosixPath(entry["file"])
        if (
            filename.is_absolute()
            or ".." in filename.parts
            or "\\" in entry["file"]
            or filename.as_posix() != entry["file"]
            or not filename.suffix
            or not legacy_eligible(entry)
            or not re.fullmatch(r"[0-9a-f]{64}", entry["code_fingerprint"])
            or type(entry["count"]) is not int
            or entry["count"] < 1
        ):
            raise ValueError(f"invalid or non-baselinable entry: {entry}")
        if key(entry) in seen:
            raise ValueError(f"duplicate baseline entry: {key(entry)}")
        seen.add(key(entry))
    if (
        len(entries) > MAX_ENTRIES
        or sum(entry["count"] for entry in entries) > MAX_DIAGNOSTICS
    ):
        raise ValueError("baseline exceeds the manual legacy budget")
    return entries


def compare(
    diagnostics: list[dict[str, Any]],
    baseline: list[dict[str, Any]],
    selected: set[str],
) -> tuple[list[dict[str, Any]], list[dict[str, Any]], dict[str, int]]:
    available = Counter({key(entry): entry["count"] for entry in baseline})
    observed: Counter[tuple[str, ...]] = Counter()
    annotated = []
    for diagnostic in diagnostics:
        item = dict(diagnostic)
        identity = key(item)
        observed[identity] += 1
        if item["classification"] == "security":
            item["status"] = "security"
        elif legacy_eligible(item) and observed[identity] <= available[identity]:
            item["status"] = "legacy"
        else:
            item["status"] = "new"
        annotated.append(item)
    resolved = []
    for entry in baseline:
        missing = entry["count"] - observed[key(entry)]
        if entry["tool"] in selected and missing > 0:
            resolved.append({**entry, "count": missing})
    counts = {
        "total": len(annotated),
        "legacy": sum(item["status"] == "legacy" for item in annotated),
        "new": sum(item["status"] == "new" for item in annotated),
        "security": sum(item["status"] == "security" for item in annotated),
        "resolved": sum(entry["count"] for entry in resolved),
    }
    return annotated, resolved, counts


def update_baseline(
    baseline: list[dict[str, Any]],
    diagnostics: list[dict[str, Any]],
    selected: set[str],
    *,
    ci: bool,
) -> list[dict[str, Any]]:
    if ci:
        raise ValueError("--write-baseline is a manual operation and is disabled in CI")
    if any(
        item["classification"] == "security"
        or not baseline_allowed(item["tool"], item["rule"])
        for item in diagnostics
    ):
        raise ValueError("security or scanner failures cannot be baselined")
    entries = [entry for entry in baseline if entry["tool"] not in selected]
    counts = Counter(key(item) for item in diagnostics if legacy_eligible(item))
    entries.extend(
        {**dict(zip(KEY_FIELDS, identity)), "count": count}
        for identity, count in sorted(counts.items())
    )
    entries.sort(key=key)
    return validate_baseline({"schema_version": 1, "entries": entries})


INVENTORY_JS = """
const { productFiles, eslintFiles } = await import(configURL);
console.log(JSON.stringify({ productFiles, eslintFiles }));
"""
PRETTIER_JS = """
const prettier = require('prettier');
const { productFiles } = await import(configURL);
const diagnostics = [];
for (const file of productFiles) {
  const filename = path.resolve(web, file);
  const source = fs.readFileSync(filename, 'utf8');
  const options = await prettier.resolveConfig(filename);
  if (!(await prettier.check(source, { ...options, filepath: filename })))
    diagnostics.push({ file: filename, line: 1, column: 1, rule: 'format',
      message: 'Prettier formatting differs' });
}
console.log(JSON.stringify(diagnostics));
"""
ESLINT_JS = """
const { ESLint } = require('eslint');
const { eslintFiles } = await import(configURL);
const eslint = new ESLint({ cwd: web, overrideConfigFile: path.join(web, 'eslint.wsl.config.mjs'),
  ignore: false, errorOnUnmatchedPattern: true });
const results = await eslint.lintFiles(eslintFiles);
console.log(JSON.stringify(results));
"""
NODE_PRELUDE = """
import fs from 'node:fs';
import path from 'node:path';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
const web = process.cwd();
const configURL = pathToFileURL(path.join(web, 'eslint.wsl.config.mjs')).href;
const require = createRequire(path.join(web, 'package.json'));
"""


class Runner:
    def __init__(self, root: Path, output: Path, manifest: dict[str, Any]):
        self.root = root.resolve()
        self.output = output
        self.manifest = manifest
        self.diagnostics: list[dict[str, Any]] = []
        self.failures: list[dict[str, str]] = []
        self.runs: list[dict[str, Any]] = []
        self.sources: dict[str, str] = {}
        self.scope: dict[str, list[str]] = {}
        self.repository_files: list[Path] | None = None
        self.product: dict[str, Any] | None = None

    def command(
        self,
        tool: str,
        args: list[str],
        *,
        cwd: Path | None = None,
        env: dict[str, str] | None = None,
        accepted: tuple[int, ...] = (0,),
    ) -> subprocess.CompletedProcess[str]:
        try:
            result = run_process(
                args,
                cwd=cwd or self.root,
                env={**os.environ, **(env or {})},
                timeout=TIMEOUT,
            )
        except (OSError, subprocess.TimeoutExpired, UnicodeError) as error:
            raise ToolFailure(f"{tool} could not complete: {error}") from error
        self.runs.append({"tool": tool, "exit_code": result.returncode})
        if result.returncode not in accepted:
            raise ToolFailure(f"{tool} exited {result.returncode}; check incomplete")
        return result

    def files(self) -> list[Path]:
        if self.repository_files is None:
            result = self.command(
                "inventory",
                ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
            )
            self.repository_files = sorted(
                {
                    self.root / filename
                    for filename in result.stdout.split("\0")
                    if filename and (self.root / filename).is_file()
                }
            )
        return self.repository_files

    def relative(self, filename: str | Path) -> str:
        candidate = Path(filename)
        if not candidate.is_absolute():
            candidate = self.root / candidate
        try:
            return candidate.resolve(strict=True).relative_to(self.root).as_posix()
        except (OSError, ValueError) as error:
            raise ToolFailure(f"diagnostic outside repository: {filename}") from error

    def freeze(self, tool: str, files: list[Path]) -> None:
        self.scope[tool] = sorted({self.relative(filename) for filename in files})
        for filename in self.scope[tool]:
            current = digest((self.root / filename).read_bytes())
            if filename in self.sources and self.sources[filename] != current:
                raise ToolFailure(f"source changed during checks: {filename}")
            self.sources[filename] = current

    def unchanged(self) -> None:
        for filename, expected in self.sources.items():
            candidate = self.root / filename
            if not candidate.is_file() or digest(candidate.read_bytes()) != expected:
                raise ToolFailure(
                    f"source changed during checks: {filename}; retry on frozen sources"
                )

    def diagnostic(
        self,
        tool: str,
        filename: str,
        rule: str,
        message: str,
        line: int = 1,
        column: int = 1,
        end_line: int | None = None,
        *,
        whole_file: bool = False,
        security: bool = False,
    ) -> dict[str, Any]:
        relative = self.relative(filename)
        if relative not in self.scope[tool]:
            raise ToolFailure(
                f"{tool} reported a file outside its selected source scope: {relative}"
            )
        if (
            not isinstance(rule, str)
            or not rule
            or not isinstance(message, str)
            or not message
            or type(line) is not int
            or type(column) is not int
            or line < 1
            or column < 1
            or (end_line is not None and (type(end_line) is not int or end_line < line))
        ):
            raise ToolFailure(f"malformed {tool} diagnostic")
        source = (self.root / relative).read_bytes()
        lines = source.splitlines(keepends=True)
        if not whole_file and (line > len(lines) or (end_line or line) > len(lines)):
            raise ToolFailure(
                f"{tool} diagnostic has an invalid source range: {relative}:{line}"
            )
        code = source if whole_file else b"".join(lines[line - 1 : end_line or line])
        if not security and not baseline_allowed(tool, rule):
            raise ToolFailure(
                f"{tool} returned a non-baselinable parser/scanner diagnostic: {rule}"
            )
        return {
            "tool": tool,
            "file": relative,
            "rule": rule,
            "code_fingerprint": digest(code),
            "line": line,
            "column": column,
            "end_line": end_line or line,
            "message": message,
            "classification": "security"
            if security
            else (
                "formatter"
                if tool in {"gofmt", "prettier"}
                else ("static" if relative.startswith("tools/ci/") else "static_legacy")
            ),
        }

    def node(self, tool: str, script: str) -> Any:
        result = self.command(
            tool,
            ["node", "--input-type=module", "-e", NODE_PRELUDE + script],
            cwd=self.root / "web",
        )
        if result.stderr:
            raise ToolFailure(f"{tool} wrote unexpected stderr; check incomplete")
        return load_json(result.stdout)

    def frontend(self) -> dict[str, Any]:
        if self.product is None:
            versions = self.node(
                "frontend-version",
                """const versions = {};
for (const name of Object.keys(JSON.parse(process.env.WSL_LINT_FRONTEND_VERSIONS)))
  versions[name] = require(name + '/package.json').version;
console.log(JSON.stringify(versions));""".replace(
                    "JSON.parse(process.env.WSL_LINT_FRONTEND_VERSIONS)",
                    json.dumps(self.manifest["frontend_versions"]),
                ),
            )
            if versions != self.manifest["frontend_versions"]:
                raise ToolFailure(
                    f"frontend tool versions differ from toolchain.json: {versions}"
                )
            inventory = self.node("product-inventory", INVENTORY_JS)
            if (
                not isinstance(inventory, dict)
                or set(inventory) != {"productFiles", "eslintFiles"}
                or not all(
                    isinstance(value, list) and value for value in inventory.values()
                )
                or not all(
                    isinstance(filename, str) and filename.startswith("src/")
                    for value in inventory.values()
                    for filename in value
                )
                or not set(inventory["eslintFiles"]).issubset(inventory["productFiles"])
            ):
                raise ToolFailure("malformed product dependency inventory")
            self.product = inventory
        return self.product

    def version(self, tool: str) -> None:
        definition = self.manifest["tools"][tool]
        result = self.command(tool + "-version", [tool, *definition["version_args"]])
        if result.stderr or not re.search(definition["version_pattern"], result.stdout):
            raise ToolFailure(f"{tool} version does not match toolchain.json")
        if tool in {"staticcheck", "govulncheck"}:
            build = self.command(
                tool + "-buildinfo", ["go", "version", "-m", self.executable(tool)]
            )
            expected = f"\tmod\t{definition['module']}\t{definition['module_version']}\t{definition['module_sum']}"
            if expected not in build.stdout.splitlines():
                raise ToolFailure(
                    f"{tool} Go build provenance does not match verified module sum"
                )

    @staticmethod
    def executable(tool: str) -> str:
        import shutil

        filename = shutil.which(tool)
        if not filename:
            raise ToolFailure(
                f"{tool} missing; install the pin in tools/ci/toolchain.json"
            )
        return filename

    def format(self) -> None:
        go_files = [
            filename
            for filename in self.files()
            if filename.suffix == ".go"
            and filename.relative_to(self.root).parts[0]
            in {"cmd", "internal", "ai", "tools"}
        ]
        if not go_files:
            raise ToolFailure("empty first-party Go format scope")
        self.freeze("gofmt", go_files)
        # Keep argv below platform limits and never let gofmt rewrite sources.
        for offset in range(0, len(go_files), 100):
            batch = go_files[offset : offset + 100]
            result = self.command("gofmt", ["gofmt", "-l", *map(str, batch)])
            if result.stderr:
                raise ToolFailure("gofmt parser failure; check incomplete")
            for filename in result.stdout.splitlines():
                self.diagnostics.append(
                    self.diagnostic(
                        "gofmt",
                        filename,
                        "format",
                        "gofmt formatting differs",
                        whole_file=True,
                    )
                )
        product = self.frontend()
        self.freeze(
            "prettier", [self.root / "web" / name for name in product["productFiles"]]
        )
        findings = self.node("prettier", PRETTIER_JS)
        if not isinstance(findings, list):
            raise ToolFailure("malformed Prettier result")
        for item in findings:
            self.diagnostics.append(
                self.diagnostic(
                    "prettier",
                    item["file"],
                    item["rule"],
                    item["message"],
                    whole_file=True,
                )
            )

    def eslint(self) -> None:
        product = self.frontend()
        self.freeze(
            "eslint", [self.root / "web" / name for name in product["eslintFiles"]]
        )
        findings = self.node("eslint", ESLINT_JS)
        if not isinstance(findings, list) or len(findings) != len(
            product["eslintFiles"]
        ):
            raise ToolFailure("incomplete or malformed ESLint result")
        seen = set()
        for file_result in findings:
            filename = file_result["filePath"]
            relative = self.relative(filename)
            if relative in seen or relative not in self.scope["eslint"]:
                raise ToolFailure("ESLint returned duplicate or unexpected files")
            seen.add(relative)
            messages = file_result["messages"]
            if (
                not isinstance(messages, list)
                or file_result.get("fatalErrorCount", 0)
                or type(file_result["errorCount"]) is not int
                or type(file_result["warningCount"]) is not int
                or file_result["errorCount"] + file_result["warningCount"]
                != len(messages)
            ):
                raise ToolFailure(
                    f"ESLint parser failure or inconsistent counts: {relative}"
                )
            for item in messages:
                rule = item["ruleId"]
                if (
                    item.get("fatal")
                    or not rule
                    or type(item.get("severity")) is not int
                    or item["severity"] not in (1, 2)
                ):
                    raise ToolFailure(
                        f"ESLint parser/configuration failure: {relative}"
                    )
                security = not baseline_allowed("eslint", rule)
                self.diagnostics.append(
                    self.diagnostic(
                        "eslint",
                        filename,
                        rule,
                        item["message"],
                        item["line"],
                        item["column"],
                        item.get("endLine"),
                        security=security,
                    )
                )

    def go_environment(self) -> dict[str, str]:
        return self.manifest["runner"]["go_environment"]

    def freeze_go(self, tool: str) -> None:
        files = [
            filename
            for filename in self.files()
            if filename.suffix == ".go" or filename.name in {"go.mod", "go.sum"}
        ]
        self.freeze(tool, files)

    def staticcheck(self) -> None:
        self.version("staticcheck")
        self.freeze_go("staticcheck")
        result = self.command(
            "staticcheck",
            ["staticcheck", *self.manifest["tools"]["staticcheck"]["check_args"]],
            env=self.go_environment(),
            accepted=(0, 1),
        )
        findings = json_stream(result.stdout)
        if result.stderr or (result.returncode == 1) != bool(findings):
            raise ToolFailure(
                "staticcheck failed without valid diagnostics; check incomplete"
            )
        for item in findings:
            location = item["location"]
            if item.get("severity") not in {"warning", "error"}:
                raise ToolFailure("malformed staticcheck severity")
            if not baseline_allowed("staticcheck", item["code"]):
                raise ToolFailure(f"staticcheck {item['code']}: {item['message']}")
            self.diagnostics.append(
                self.diagnostic(
                    "staticcheck",
                    location["file"],
                    item["code"],
                    item["message"],
                    location["line"],
                    location["column"],
                )
            )

    def ruff(self) -> None:
        self.version("ruff")
        files = [
            filename
            for filename in self.files()
            if filename.suffix == ".py"
            and filename.relative_to(self.root).parts[0] == "tools"
        ]
        if not files:
            raise ToolFailure("empty Python tools scope")
        self.freeze("ruff", files)
        result = self.command(
            "ruff",
            ["ruff", *self.manifest["tools"]["ruff"]["check_args"], *map(str, files)],
            accepted=(0, 1),
        )
        findings = load_json(result.stdout)
        if (
            result.stderr
            or not isinstance(findings, list)
            or (result.returncode == 1) != bool(findings)
        ):
            raise ToolFailure("Ruff failed or returned inconsistent findings")
        for item in findings:
            location, end = item["location"], item["end_location"]
            self.diagnostics.append(
                self.diagnostic(
                    "ruff",
                    item["filename"],
                    item["code"],
                    item["message"],
                    location["row"],
                    location["column"],
                    end["row"],
                )
            )

    def govulncheck(self) -> None:
        self.version("govulncheck")
        self.freeze_go("govulncheck")
        result = self.command(
            "govulncheck",
            ["govulncheck", *self.manifest["tools"]["govulncheck"]["check_args"]],
            env={
                **self.go_environment(),
                "GOVULNDB": self.manifest["tools"]["govulncheck"]["database"],
            },
        )
        events = json_stream(result.stdout)
        if result.stderr:
            raise ToolFailure("govulncheck scanner failure; check incomplete")
        names = []
        for event in events:
            if len(event) != 1 or next(iter(event)) not in {
                "config",
                "SBOM",
                "progress",
                "osv",
                "finding",
            }:
                raise ToolFailure("malformed govulncheck event")
            name, value = next(iter(event.items()))
            if not isinstance(value, dict):
                raise ToolFailure("malformed govulncheck payload")
            names.append(name)
            if name == "config" and (
                value.get("protocol_version") != "v1.0.0"
                or value.get("scanner_name") != "govulncheck"
                or value.get("scanner_version")
                != self.manifest["tools"]["govulncheck"]["version"]
                or value.get("db") != self.manifest["tools"]["govulncheck"]["database"]
                or value.get("scan_level") != "symbol"
                or value.get("scan_mode") != "source"
            ):
                raise ToolFailure(
                    "govulncheck scan provenance differs from the pinned source scan"
                )
            if name == "SBOM" and (
                not isinstance(value.get("modules"), list)
                or not value["modules"]
                or not all(
                    isinstance(module, dict)
                    and isinstance(module.get("path"), str)
                    and module["path"]
                    for module in value["modules"]
                )
                or not isinstance(value.get("roots"), list)
                or not value["roots"]
                or not all(isinstance(root, str) and root for root in value["roots"])
            ):
                raise ToolFailure("govulncheck returned an empty SBOM")
            if name == "progress" and not isinstance(value.get("message"), str):
                raise ToolFailure("malformed govulncheck progress")
            if name == "osv" and (
                not isinstance(value.get("id"), str)
                or not isinstance(value.get("affected"), list)
            ):
                raise ToolFailure("malformed govulncheck vulnerability")
            if name == "finding":
                if (
                    not re.fullmatch(r"GO-\d{4}-\d+", value.get("osv", ""))
                    or not isinstance(value.get("trace"), list)
                    or not value["trace"]
                    or not all(
                        isinstance(frame, dict)
                        and isinstance(frame.get("module"), str)
                        and frame["module"]
                        for frame in value["trace"]
                    )
                ):
                    raise ToolFailure("malformed govulncheck finding")
                self.diagnostics.append(
                    self.diagnostic(
                        "govulncheck",
                        "go.mod",
                        value["osv"],
                        json.dumps(value, sort_keys=True, separators=(",", ":")),
                        whole_file=True,
                        security=True,
                    )
                )
        if (
            not names
            or names[0] != "config"
            or names.count("config") != 1
            or names.count("SBOM") != 1
        ):
            raise ToolFailure("incomplete govulncheck source scan")

    def run(self, check: str) -> None:
        self.freeze(
            "configuration",
            [
                self.root / name
                for name in (
                    "tools/ci/toolchain.json",
                    "web/eslint.wsl.config.mjs",
                    "web/tsconfig.json",
                    "web/.prettierrc",
                    "web/pnpm-lock.yaml",
                )
                if (self.root / name).is_file()
            ],
        )
        methods = (
            ["format", "eslint", "staticcheck", "ruff", "govulncheck"]
            if check == "all"
            else [check]
        )
        for method in methods:
            try:
                getattr(self, method)()
                self.unchanged()
            except (
                ToolFailure,
                ValueError,
                KeyError,
                TypeError,
                AttributeError,
                IndexError,
                OSError,
                re.error,
            ) as error:
                self.failures.append({"tool": method, "error": str(error)})


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", choices=CHECKS, default="all")
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument(
        "--write-baseline",
        action="store_true",
        help="manual bounded legacy refresh; disabled in CI",
    )
    args = parser.parse_args(argv)
    args.output.mkdir(parents=True, exist_ok=True)
    baseline_file = ROOT / "tools/ci/lint-baseline.json"
    runner = Runner(ROOT, args.output, {})
    sha = None
    try:
        sha = revision(ROOT)
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        runner.failures.append({"tool": "evidence", "error": str(error)})
    try:
        manifest = load_json(
            (ROOT / "tools/ci/toolchain.json").read_text(encoding="utf-8")
        )
        if (
            not isinstance(manifest, dict)
            or manifest.get("schema_version") != 1
            or not {"staticcheck", "govulncheck", "ruff"}.issubset(manifest["tools"])
        ):
            raise ValueError("unsupported toolchain schema")
        runner.manifest = manifest
    except (OSError, ValueError, KeyError, TypeError) as error:
        runner.failures.append({"tool": "toolchain", "error": str(error)})
    baseline = []
    try:
        baseline = validate_baseline(
            load_json(baseline_file.read_text(encoding="utf-8"))
        )
    except (OSError, ValueError) as error:
        runner.failures.append({"tool": "baseline", "error": str(error)})
    if not runner.failures:
        runner.run(args.check)
    if sha:
        try:
            if revision(ROOT) != sha:
                raise ValueError("Git HEAD changed during lint checks")
        except (OSError, ValueError, subprocess.CalledProcessError) as error:
            runner.failures.append({"tool": "evidence", "error": str(error)})
    if args.write_baseline and not runner.failures:
        try:
            baseline = update_baseline(
                baseline,
                runner.diagnostics,
                CHECKS[args.check],
                ci=os.environ.get("CI", "").lower() not in {"", "0", "false"},
            )
            baseline_file.write_text(
                json.dumps({"schema_version": 1, "entries": baseline}, indent=2) + "\n",
                encoding="utf-8",
            )
        except ValueError as error:
            runner.failures.append({"tool": "baseline", "error": str(error)})
    diagnostics, resolved, counts = compare(
        runner.diagnostics, baseline, CHECKS[args.check]
    )
    counts["failures"] = len(runner.failures)
    passed = not any(
        counts[name] for name in ("new", "security", "resolved", "failures")
    )
    report = {
        "schema_version": 1,
        "sha": sha,
        "status": "passed" if passed else "failed",
        "profile": "lint/" + args.check,
        "check": args.check,
        "passed": passed,
        "counts": counts,
        "diagnostics": sorted(
            diagnostics, key=lambda item: (*key(item), item["line"], item["column"])
        ),
        "resolved_baseline": sorted(resolved, key=key),
        "failures": runner.failures,
        "runs": runner.runs,
        "scope": runner.scope,
        "source_fingerprints": dict(sorted(runner.sources.items())),
    }
    (args.output / "lint.json").write_text(
        json.dumps(report, indent=2) + "\n", encoding="utf-8"
    )
    print(
        json.dumps(
            {
                "check": args.check,
                "passed": passed,
                "counts": counts,
                "report": str(args.output / "lint.json"),
            }
        )
    )
    return 0 if passed else 1


if __name__ == "__main__":
    sys.exit(main())
