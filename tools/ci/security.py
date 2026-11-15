"""Gate structured scanner results; scanner failure is never a clean scan."""

import argparse
import json
from pathlib import Path
import re
import sys

try:
    from .common import ROOT, load_json, report, run, write_json
except ImportError:
    from common import ROOT, load_json, report, run, write_json


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


def codeql_findings(paths):
    if len(paths) != 2 or {path.stem for path in paths} != {"go", "javascript"}:
        raise ValueError("Missing CodeQL language result")
    found = []
    for path in paths:
        value = load_json(path)
        if value.get("version") != "2.1.0" or not value.get("runs"):
            raise ValueError("Malformed CodeQL SARIF")
        for result in value["runs"]:
            driver = result.get("tool", {}).get("driver", {})
            if driver.get("name") != "CodeQL" or driver.get("version") != "2.27.4":
                raise ValueError("Unexpected CodeQL extractor version")
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
                found.append(f"{path.stem}:{identifier}")
    return sorted(set(found))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("scanner", choices=("govulncheck", "codeql"))
    parser.add_argument("--output", type=Path, required=True)
    options = parser.parse_args()
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
            findings = codeql_findings(list(options.output.glob("*.sarif")))
        cases = [{"id": options.scanner, "status": "failed" if findings else "passed",
                  "code": "new_security_findings" if findings else "ok",
                  "findings": findings}]
    except Exception:
        cases = [{"id": options.scanner, "status": "failed", "code": "scanner_or_extraction_failed"}]
    value = report("security/" + options.scanner, cases)
    write_json(options.output / "report.json", value)
    print(json.dumps({"scanner": options.scanner, "status": value["status"], "counts": value["counts"]}))
    return int(value["status"] != "passed")


if __name__ == "__main__":
    sys.exit(main())
