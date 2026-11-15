"""Gate structured scanner results; scanner failure is never a clean scan."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import sys

try:
    from .common import ROOT, load_json, report, run, write_json
except ImportError:
    from common import ROOT, load_json, report, run, write_json


CODEQL_LANGUAGES = ("go", "javascript")

# These two native wire operations are reviewed against synthetic protocol tests.
# Source drift, duplicate results and other MD5 operations remain blocking.
PROTOCOL_MD5_CONTRACTS = (
    {
        "id": "lajiaoliao-login-wire-md5",
        "file": "internal/lajiaoliao/client.go",
        "function": "(*Client).Login",
        "declaration": "func (c *Client) Login(",
        "source_sha256": "4e5c149cfc9e0b4405934ad2d9fab2836f12f3ade253befbb898bc5838cc7644",
        "rule": "go/weak-sensitive-data-hashing",
        "line_offset": 22, "start_column": 18, "end_column": 34, "count": 1,
        "reason": "Desktop /pc/account/login requires passwordMD5 over HTTPS; "
                  "this operation encodes the native wire field, not stored password verification.",
        "evidence": "internal/lajiaoliao/client_test.go::TestDesktopLoginNativeContract",
    },
    {
        "id": "lajiaoliao-password-reset-wire-md5",
        "file": "internal/lajiaoliao/security.go",
        "function": "(*Client).ChangeSecurity",
        "declaration": "func (c *Client) ChangeSecurity(",
        "source_sha256": "05d710fc63db2779f455fb8339abd3e24d466927b0e7401a360c6fb34734afb8",
        "rule": "go/weak-sensitive-data-hashing",
        "line_offset": 53, "start_column": 19, "end_column": 38, "count": 1,
        "reason": "Desktop /account/password/reset requires passwordMd5 over HTTPS; "
                  "this operation encodes the native wire field, not stored password verification.",
        "evidence": "internal/lajiaoliao/security_test.go::"
                    "TestSecurityIndependentDesktopContractsAndNoReplay",
    },
)


class CodeQLStageError(ValueError):
    def __init__(self, language, stage):
        if language not in CODEQL_LANGUAGES or stage not in ("extract", "analyze"):
            raise ValueError("Invalid CodeQL failure stage")
        super().__init__("CodeQL stage failed")
        self.language = language
        self.stage = stage


def stream(text):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError("Duplicate scanner field")
            result[key] = value
        return result

    decoder = json.JSONDecoder(object_pairs_hook=unique)
    remaining = text.strip()
    values = []
    while remaining:
        value, offset = decoder.raw_decode(remaining)
        if not isinstance(value, dict):
            raise ValueError("Malformed scanner record")
        values.append(value)
        remaining = remaining[offset:].lstrip()
    return values


def vulnerabilities(values, returncode):
    if (returncode not in (0, 3) or not values or set(values[0]) != {"config"}
            or sum("config" in value for value in values) != 1
            or sum("SBOM" in value for value in values) != 1):
        raise ValueError("govulncheck did not complete")
    config = values[0]["config"]
    if (config.get("scanner_name") != "govulncheck"
            or config.get("scanner_version") != "v1.9.0"
            or config.get("protocol_version") != "v1.0.0"
            or config.get("scan_level") != "symbol"
            or config.get("scan_mode") != "source"
            or config.get("db") != "https://vuln.go.dev"):
        raise ValueError("Unexpected vulnerability scanner")
    findings = []
    for value in values:
        if (len(value) != 1 or next(iter(value)) not in {
                "config", "SBOM", "progress", "osv", "finding"}
                or not isinstance(next(iter(value.values())), dict)):
            raise ValueError("Malformed vulnerability scanner event")
        if "SBOM" in value:
            sbom = value["SBOM"]
            if (not isinstance(sbom.get("modules"), list) or not sbom["modules"]
                    or not all(isinstance(module, dict) and module.get("path")
                               for module in sbom["modules"])
                    or not isinstance(sbom.get("roots"), list) or not sbom["roots"]
                    or not all(isinstance(root, str) and root for root in sbom["roots"])):
                raise ValueError("Missing vulnerability scan scope")
        if "finding" not in value:
            continue
        finding = value["finding"]
        if not re.fullmatch(r"GO-\d{4}-\d+", finding.get("osv", "")):
            raise ValueError("Invalid vulnerability identifier")
        trace = finding.get("trace")
        if (not isinstance(trace, list) or not trace
                or not all(isinstance(frame, dict) and frame.get("module")
                           for frame in trace)):
            raise ValueError("Incomplete vulnerability reachability evidence")
        if any(frame.get("function") for frame in trace):
            findings.append(finding["osv"])
    if returncode == 3 and not findings:
        raise ValueError("Scanner and reachability evidence disagree")
    return sorted(set(findings))


def scan_codeql(executable, database_root, output):
    specification = load_json(ROOT / "tools/ci/toolchain.json")["tools"]["codeql"]
    version = run([str(executable), "version", "--format=json"], timeout=60)
    values = stream(version.stdout)
    if (version.returncode or len(values) != 1
            or values[0].get("version") != specification["version"]):
        raise ValueError("Unexpected CodeQL CLI version")
    if any(output.glob("*.sarif")):
        raise ValueError("CodeQL output must be fresh")
    database_root.mkdir(parents=True, exist_ok=False)
    environment = {**os.environ, "CGO_ENABLED": "1", "GOFLAGS": "-mod=readonly"}
    for language in CODEQL_LANGUAGES:
        database = database_root / language
        create = [str(executable), "database", "create", str(database),
                  f"--language={language}", f"--source-root={ROOT}",
                  f"--codescanning-config={ROOT / '.github/codeql.yml'}"]
        if language == "go":
            create += ["--build-mode=manual",
                       "--command=go build -a -tags sodium ./cmd/... ./internal/... ./tools/..."]
        else:
            create += ["--build-mode=none"]
        analyze = [str(executable), "database", "analyze", str(database),
                   "--format=sarifv2.1.0", f"--output={output / (language + '.sarif')}",
                   f"--sarif-category=security/{language}",
                   "--sarif-include-diagnostics", "--threads=0"]
        for stage, command in (("extract", create), ("analyze", analyze)):
            try:
                result = run(command, env=environment, timeout=900)
            except Exception:
                raise CodeQLStageError(language, stage) from None
            if result.returncode:
                raise CodeQLStageError(language, stage)


def protocol_contract_line(contract):
    try:
        path = ROOT / contract["file"]
        if path.is_symlink():
            return None
        source = path.read_bytes()
    except OSError:
        return None
    # A complete, hash-pinned Go top-level function ends at its unindented brace.
    pattern = rb"(?ms)^" + re.escape(contract["declaration"].encode()) + rb".*?^}\n"
    functions = list(re.finditer(pattern, source))
    if len(functions) != 1:
        return None
    function = functions[0]
    if hashlib.sha256(function.group()).hexdigest() != contract["source_sha256"]:
        return None
    return source[:function.start()].count(b"\n") + 1 + contract["line_offset"]


def protocol_contract_matches(language, finding, contract, line):
    if language != "go" or line is None or finding.get("ruleId") != contract["rule"]:
        return False
    locations = finding.get("locations")
    if not isinstance(locations, list) or len(locations) != 1:
        return False
    physical = locations[0].get("physicalLocation", {})
    artifact = physical.get("artifactLocation", {})
    region = physical.get("region", {})
    return (artifact.get("uri") == contract["file"] and artifact.get("uriBaseId") == "%SRCROOT%"
            and region.get("startLine") == line and region.get("endLine", line) == line
            and region.get("startColumn") == contract["start_column"]
            and region.get("endColumn") == contract["end_column"])


def codeql_findings(paths, *, protocol_audit=None):
    """Validate both complete scans; apply pinned wire contracts only with an audit sink."""
    if len(paths) != 2 or {path.name for path in paths} != {
            f"{language}.sarif" for language in CODEQL_LANGUAGES}:
        raise ValueError("Missing CodeQL language result")
    found = []
    for path in paths:
        value = load_json(path)
        if value.get("version") != "2.1.0" or not value.get("runs"):
            raise ValueError("Malformed CodeQL SARIF")
        for result in value["runs"]:
            driver = result.get("tool", {}).get("driver", {})
            version = driver.get("semanticVersion", driver.get("version"))
            if driver.get("name") != "CodeQL" or version != "2.27.4":
                raise ValueError("Unexpected CodeQL extractor version")
            category = result.get("automationDetails", {}).get("id", "")
            if not isinstance(category, str) or category.rstrip("/") != f"security/{path.stem}":
                raise ValueError("CodeQL language result does not match its category")
            invocations = result.get("invocations")
            if not isinstance(invocations, list) or not invocations:
                raise ValueError("Missing CodeQL completion evidence")
            for invocation in invocations:
                if invocation.get("executionSuccessful") is not True:
                    raise ValueError("CodeQL extraction or query failure")
                if any(item.get("level") == "error" for item in
                       invocation.get("toolExecutionNotifications", [])):
                    raise ValueError("CodeQL reported a failed extraction")
            if not isinstance(result.get("results"), list):
                raise ValueError("Missing CodeQL query results")
            for item in result["results"]:
                identifier = item.get("ruleId")
                if not identifier:
                    raise ValueError("Missing security rule identity")
                found.append((path.stem, item))
    accepted = set()
    if protocol_audit is not None:
        for contract in PROTOCOL_MD5_CONTRACTS:
            line = protocol_contract_line(contract)
            matches = [index for index, (language, finding) in enumerate(found)
                       if protocol_contract_matches(language, finding, contract, line)]
            if len(matches) == contract["count"]:
                accepted.update(matches)
                protocol_audit.append({key: contract[key] for key in
                                       ("id", "file", "function", "source_sha256",
                                        "rule", "count", "reason", "evidence")})
    return sorted({f"{language}:{finding['ruleId']}"
                   for index, (language, finding) in enumerate(found) if index not in accepted})


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("scanner", choices=("govulncheck", "codeql"))
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--codeql", type=Path)
    parser.add_argument("--database-root", type=Path)
    options = parser.parse_args()
    if bool(options.codeql) != bool(options.database_root) or (
            options.codeql and options.scanner != "codeql"):
        parser.error("--codeql and --database-root must be used together for CodeQL")
    options.output.mkdir(parents=True, exist_ok=True)
    try:
        if options.scanner == "govulncheck":
            specification = load_json(ROOT / "tools/ci/toolchain.json")["tools"]["govulncheck"]
            result = run(["govulncheck", *specification["check_args"]], timeout=900)
            if result.stderr:
                raise ValueError("Vulnerability scanner reported a failure")
            values = stream(result.stdout)
            findings = vulnerabilities(values, result.returncode)
            write_json(options.output / "scanner.json", values)
        else:
            if options.codeql:
                scan_codeql(options.codeql.resolve(), options.database_root.resolve(),
                            options.output.resolve())
            protocol_audit = []
            findings = codeql_findings(list(options.output.glob("*.sarif")),
                                      protocol_audit=protocol_audit)
        cases = [{"id": options.scanner, "status": "failed" if findings else "passed",
                  "code": "new_security_findings" if findings else "ok",
                  "findings": findings}]
        if options.scanner == "codeql":
            cases[0]["audited_protocol_findings"] = protocol_audit
    except CodeQLStageError as failure:
        cases = [{"id": options.scanner, "status": "failed",
                  "code": f"codeql_{failure.stage}_failed",
                  "language": failure.language, "stage": failure.stage}]
    except Exception:
        cases = [{"id": options.scanner, "status": "failed", "code": "scanner_or_extraction_failed"}]
    value = report("security/" + options.scanner, cases)
    write_json(options.output / "report.json", value)
    summary = {"scanner": options.scanner, "status": value["status"], "counts": value["counts"]}
    if "stage" in cases[0]:
        summary.update({key: cases[0][key] for key in ("code", "language", "stage")})
    print(json.dumps(summary))
    return int(value["status"] != "passed")


if __name__ == "__main__":
    sys.exit(main())
