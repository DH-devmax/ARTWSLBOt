import copy
from contextlib import redirect_stderr, redirect_stdout
import io
import json
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import yaml

from tools.ci import security
from tools.ci.common import ROOT, load_json, write_json
from tools.ci.evidence import JOBS, record
from tools.ci.security import codeql_findings, scan_codeql, vulnerabilities


def codeql_sarif(language, findings=None):
    return {"version": "2.1.0", "runs": [{
        "tool": {"driver": {"name": "CodeQL", "semanticVersion": "2.27.4"}},
        "automationDetails": {"id": f"security/{language}/"},
        "invocations": [{"executionSuccessful": True}],
        "results": [] if findings is None else findings}]}


class ScannerTests(unittest.TestCase):
    def config(self):
        return [{"config": {
            "scanner_name": "govulncheck", "scanner_version": "v1.9.0",
            "protocol_version": "v1.0.0", "scan_level": "symbol",
            "scan_mode": "source", "db": "https://vuln.go.dev"}},
            {"SBOM": {"modules": [{"path": "synthetic"}], "roots": ["synthetic"]}}]

    def test_missing_failed_or_wrong_version_scanner_fails(self):
        for values, code in [([], 0), ([{"progress": {}}], 0),
                             ([{"config": {}}], 0), ([{"config": {}}], 2)]:
            with self.subTest(values=values, code=code), self.assertRaises(ValueError):
                vulnerabilities(values, code)

    def test_reachable_vulnerability_and_unknown_output_are_not_clean(self):
        config = self.config()
        found = {"finding": {"osv": "GO-2026-1234",
                             "trace": [{"module": "synthetic", "function": "call"}]}}
        self.assertEqual(["GO-2026-1234"], vulnerabilities([*config, found], 3))
        with self.assertRaises(ValueError):
            vulnerabilities(config, 3)

    def test_truncated_scan_and_unknown_events_are_failures(self):
        config = self.config()
        with self.assertRaises(ValueError):
            vulnerabilities(config[:1], 0)
        with self.assertRaises(ValueError):
            vulnerabilities([*config, {"unexpected": {}}], 0)
        self.assertEqual([], vulnerabilities(config, 0))

    def test_missing_codeql_language_or_failed_extraction(self):
        with tempfile.TemporaryDirectory() as temporary:
            paths = [Path(temporary) / f"{language}.sarif" for language in ("go", "javascript")]
            for path in paths:
                write_json(path, codeql_sarif(path.stem))
            self.assertEqual([], codeql_findings(paths))
            with self.assertRaises(ValueError):
                codeql_findings(paths[:1])
            value = codeql_sarif("go")
            value["runs"][0]["invocations"][0]["executionSuccessful"] = False
            write_json(paths[0], value)
            with self.assertRaises(ValueError):
                codeql_findings(paths)

    def test_new_codeql_finding_is_a_gate(self):
        with tempfile.TemporaryDirectory() as temporary:
            paths = [Path(temporary) / f"{language}.sarif" for language in ("go", "javascript")]
            for path in paths:
                write_json(path, codeql_sarif(path.stem, [{"ruleId": "synthetic/new-finding"}]))
            self.assertEqual(2, len(codeql_findings(paths)))

    def test_renamed_duplicate_or_extra_language_is_not_a_complete_scan(self):
        with tempfile.TemporaryDirectory() as temporary:
            paths = [Path(temporary) / f"{language}.sarif" for language in ("go", "javascript")]
            for path in paths:
                write_json(path, codeql_sarif("go"))
            with self.assertRaises(ValueError):
                codeql_findings(paths)
            write_json(paths[1], codeql_sarif("javascript"))
            with self.assertRaises(ValueError):
                codeql_findings([paths[0], paths[0]])
            extra = Path(temporary) / "other.sarif"
            write_json(extra, codeql_sarif("javascript"))
            with self.assertRaises(ValueError):
                codeql_findings([*paths, extra])

    def test_bad_version_missing_completion_or_error_notification_is_not_clean(self):
        with tempfile.TemporaryDirectory() as temporary:
            paths = [Path(temporary) / f"{language}.sarif" for language in ("go", "javascript")]
            write_json(paths[0], codeql_sarif("go"))
            clean = codeql_sarif("javascript")
            bad_version = copy.deepcopy(clean)
            bad_version["runs"][0]["tool"]["driver"]["semanticVersion"] = "0.0.0"
            for changes in ({"invocations": []}, {"results": None},
                            {"automationDetails": {}},
                            {"invocations": [{"executionSuccessful": True,
                             "toolExecutionNotifications": [{"level": "error"}]}]}):
                value = copy.deepcopy(clean)
                value["runs"][0].update(changes)
                write_json(paths[1], value)
                with self.subTest(changes=changes), self.assertRaises(ValueError):
                    codeql_findings(paths)
            write_json(paths[1], bad_version)
            with self.assertRaises(ValueError):
                codeql_findings(paths)

    def test_actual_cli_semantic_version_takes_precedence_with_trailing_category(self):
        with tempfile.TemporaryDirectory() as temporary:
            paths = [Path(temporary) / f"{language}.sarif" for language in ("go", "javascript")]
            write_json(paths[0], codeql_sarif("go"))
            for fields in ({"semanticVersion": "2.27.4"}, {"version": "2.27.4"},
                           {"semanticVersion": "2.27.4", "version": "CodeQL CLI build label"}):
                value = codeql_sarif("javascript")
                value["runs"][0]["tool"]["driver"] = {"name": "CodeQL", **fields}
                write_json(paths[1], value)
                with self.subTest(fields=fields):
                    self.assertEqual([], codeql_findings(paths))
            for fields in ({}, {"semanticVersion": None, "version": "2.27.4"},
                           {"semanticVersion": "0.0.0", "version": "2.27.4"}):
                value["runs"][0]["tool"]["driver"] = {"name": "CodeQL", **fields}
                write_json(paths[1], value)
                with self.subTest(fields=fields), self.assertRaises(ValueError):
                    codeql_findings(paths)


def protocol_md5_results():
    return [{"ruleId": "go/weak-sensitive-data-hashing", "locations": [{
        "physicalLocation": {
            "artifactLocation": {"uri": path, "uriBaseId": "%SRCROOT%"},
            "region": {"startLine": line, "startColumn": start, "endColumn": end}}}]}
        for path, line, start, end in (
            ("internal/lajiaoliao/client.go", 299, 18, 34),
            ("internal/lajiaoliao/security.go", 110, 19, 38))]


class ProtocolContractTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.folder = Path(self.temporary.name) / "security/codeql"
        self.folder.mkdir(parents=True)
        self.paths = [self.folder / f"{language}.sarif" for language in ("go", "javascript")]
        write_json(self.paths[0], codeql_sarif("go", protocol_md5_results()))
        write_json(self.paths[1], codeql_sarif("javascript"))

    def test_only_two_reviewed_wire_operations_can_pass_with_a_complete_audit(self):
        self.assertEqual(["go:go/weak-sensitive-data-hashing"], codeql_findings(self.paths))
        audit = []
        self.assertEqual([], codeql_findings(self.paths, protocol_audit=audit))
        self.assertEqual(2, len(audit))
        for expected, item in zip(security.PROTOCOL_MD5_CONTRACTS, audit):
            for key in ("file", "function", "source_sha256", "rule", "reason", "evidence"):
                self.assertEqual(expected[key], item[key])
            self.assertEqual(1, item["count"])

    def test_rule_path_region_language_and_count_near_misses_still_block(self):
        for index in range(2):
            for change in ("rule", "path", "base", "line", "column", "span", "locations", "duplicate"):
                findings = protocol_md5_results()
                finding = findings[index]
                location = finding["locations"][0]["physicalLocation"]
                if change == "rule":
                    finding["ruleId"] = "go/log-injection"
                elif change == "path":
                    location["artifactLocation"]["uri"] = "internal/other/password.go"
                elif change == "base":
                    location["artifactLocation"]["uriBaseId"] = "%OTHER%"
                elif change == "line":
                    location["region"]["startLine"] += 1
                elif change == "column":
                    location["region"]["startColumn"] += 1
                elif change == "span":
                    location["region"]["endLine"] = location["region"]["startLine"] + 1
                elif change == "locations":
                    finding["locations"].append(copy.deepcopy(finding["locations"][0]))
                else:
                    findings.append(copy.deepcopy(finding))
                write_json(self.paths[0], codeql_sarif("go", findings))
                audit = []
                with self.subTest(index=index, change=change):
                    self.assertTrue(codeql_findings(self.paths, protocol_audit=audit))
                    self.assertEqual(1, len(audit))
        write_json(self.paths[0], codeql_sarif("go"))
        write_json(self.paths[1], codeql_sarif("javascript", protocol_md5_results()))
        self.assertEqual(["javascript:go/weak-sensitive-data-hashing"],
                         codeql_findings(self.paths, protocol_audit=[]))

    def test_function_source_drift_disables_the_exception(self):
        root = self.folder / "changed-source"
        for contract in security.PROTOCOL_MD5_CONTRACTS:
            path = root / contract["file"]
            path.parent.mkdir(parents=True, exist_ok=True)
            source = (ROOT / contract["file"]).read_text()
            path.write_text(source.replace("md5.Sum(", "md5.Sum( /* changed */ ", 1))
        with patch.object(security, "ROOT", root):
            audit = []
            self.assertTrue(codeql_findings(self.paths, protocol_audit=audit))
            self.assertEqual([], audit)

    def test_vendor_and_other_first_party_findings_are_never_exempted(self):
        for path, rule in (("third_party/gopsutil/cpu/cpu_linux.go", "go/incorrect-integer-conversion"),
                           ("internal/httpapi/server.go", "go/log-injection"),
                           ("internal/other/password.go", "go/weak-sensitive-data-hashing")):
            findings = protocol_md5_results()
            extra = copy.deepcopy(findings[0])
            extra["ruleId"] = rule
            extra["locations"][0]["physicalLocation"]["artifactLocation"]["uri"] = path
            write_json(self.paths[0], codeql_sarif("go", [*findings, extra]))
            audit = []
            with self.subTest(path=path, rule=rule):
                self.assertEqual([f"go:{rule}"], codeql_findings(self.paths, protocol_audit=audit))
                self.assertEqual(2, len(audit))

    def test_report_audits_exceptions_without_modifying_raw_sarif_and_new_issue_blocks_receipt(self):
        original = [path.read_bytes() for path in self.paths]
        for extra in (False, True):
            findings = protocol_md5_results()
            if extra:
                findings.append({"ruleId": "go/log-injection"})
            write_json(self.paths[0], codeql_sarif("go", findings))
            with patch("sys.argv", ["security.py", "codeql", "--output", str(self.folder)]), \
                    patch("tools.ci.common.revision", return_value="a" * 40), \
                    redirect_stdout(io.StringIO()):
                self.assertEqual(int(extra), security.main())
            value = load_json(self.folder / "report.json")
            case = value["cases"][0]
            self.assertEqual("new_security_findings" if extra else "ok", case["code"])
            self.assertEqual(2, len(case["audited_protocol_findings"]))
            self.assertEqual("failed" if extra else "passed", value["status"])
            write_json(self.folder.parent / "vulnerabilities/report.json",
                       {"sha": "a" * 40, "status": "passed"})
            if extra:
                with self.assertRaises(ValueError):
                    record("security", self.folder.parent, "a" * 40)
            else:
                receipt = record("security", self.folder.parent, "a" * 40)
                self.assertIn("codeql/go.sarif", receipt["files"])
                self.assertIn("codeql/javascript.sarif", receipt["files"])
            if not extra:
                self.assertEqual(original, [path.read_bytes() for path in self.paths])


class CodeQLExecutionTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.output = self.root / "security/codeql"
        self.output.mkdir(parents=True)
        self.database_root = self.root / "databases"
        self.executable = self.root / "bundle with spaces/codeql"

    def command(self, command, **options):
        if command[1] == "version":
            stdout = '{"version":"2.27.4"}'
        else:
            self.assertEqual("1", options["env"]["CGO_ENABLED"])
            self.assertEqual("-mod=readonly", options["env"]["GOFLAGS"])
            if command[2] == "analyze":
                output = Path(next(arg.split("=", 1)[1] for arg in command
                                   if arg.startswith("--output=")))
                write_json(output, codeql_sarif(output.stem))
            stdout = ""
        return subprocess.CompletedProcess(command, 0, stdout, "CodeQL progress")

    def test_manual_go_and_build_free_javascript_produce_one_complete_gate(self):
        with patch.object(security, "run", side_effect=self.command) as command:
            scan_codeql(self.executable, self.database_root, self.output)
        calls = [call.args[0] for call in command.call_args_list]
        creates = [call for call in calls if call[1:3] == ["database", "create"]]
        self.assertEqual(2, len(creates))
        self.assertEqual(str(self.database_root / "go"), creates[0][3])
        self.assertIn("--language=go", creates[0])
        self.assertIn("--build-mode=manual", creates[0])
        self.assertIn("--command=go build -a -tags sodium ./cmd/... ./internal/... ./tools/...", creates[0])
        self.assertEqual(str(self.database_root / "javascript"), creates[1][3])
        self.assertIn("--language=javascript", creates[1])
        self.assertIn("--build-mode=none", creates[1])
        self.assertFalse(any(arg.startswith("--command") for arg in creates[1]))
        for create in creates:
            self.assertIn(f"--source-root={ROOT}", create)
            self.assertIn(f"--codescanning-config={ROOT / '.github/codeql.yml'}", create)
            self.assertNotIn("--skip-empty", create)
        for analyze in (call for call in calls if call[1:3] == ["database", "analyze"]):
            self.assertIn("--sarif-include-diagnostics", analyze)
        self.assertEqual([], codeql_findings(list(self.output.glob("*.sarif"))))

    def test_each_extraction_or_analysis_failure_blocks_report_and_receipt(self):
        for failed_stage in range(1, 5):
            for failure in ("exit", "timeout", "oserror"):
                with self.subTest(stage=failed_stage, failure=failure), \
                        tempfile.TemporaryDirectory() as temporary:
                    folder = Path(temporary)
                    arguments = ["security.py", "codeql", "--codeql", str(self.executable),
                                 "--database-root", str(folder / "databases"),
                                 "--output", str(folder / "codeql")]
                    stage = 0
                    raw = "SYNTHETIC_PRIVATE_TOOL_OUTPUT"

                    def fail(command, **options):
                        nonlocal stage
                        if command[1] != "version":
                            stage += 1
                        if stage == failed_stage:
                            if failure == "timeout":
                                raise subprocess.TimeoutExpired(command, 900, raw, raw)
                            if failure == "oserror":
                                raise OSError(raw)
                            return subprocess.CompletedProcess(command, 2, raw, raw)
                        return self.command(command, **options)

                    output, errors = io.StringIO(), io.StringIO()
                    with patch.object(security, "run", side_effect=fail), \
                            patch("sys.argv", arguments), \
                            patch("tools.ci.common.revision", return_value="a" * 40), \
                            redirect_stdout(output), redirect_stderr(errors):
                        self.assertEqual(1, security.main())
                    value = load_json(folder / "codeql/report.json")
                    self.assertEqual("failed", value["status"])
                    expected_stage = "extract" if failed_stage % 2 else "analyze"
                    expected = {"code": f"codeql_{expected_stage}_failed",
                                "stage": expected_stage,
                                "language": "go" if failed_stage <= 2 else "javascript"}
                    for key, expected_value in expected.items():
                        self.assertEqual(expected_value, value["cases"][0][key])
                        self.assertEqual(expected_value, json.loads(output.getvalue())[key])
                    self.assertEqual(failed_stage, stage)
                    self.assertEqual("", errors.getvalue())
                    self.assertNotIn(raw, output.getvalue() + json.dumps(value))
                    write_json(folder / "vulnerabilities/report.json",
                               {"sha": "a" * 40, "status": "passed"})
                    with self.assertRaises(ValueError):
                        record("security", folder, "a" * 40)

    def test_wrong_cli_version_never_starts_extraction(self):
        for stdout, returncode in [('{"version":"0.0.0"}', 0), ('{"version":"2.27.4"}', 2)]:
            with self.subTest(stdout=stdout, code=returncode), \
                    patch.object(security, "run", return_value=subprocess.CompletedProcess(
                        [], returncode, stdout, "")) as command:
                with self.assertRaises(ValueError):
                    scan_codeql(self.executable, self.database_root, self.output)
                self.assertEqual(1, command.call_count)
                self.assertFalse(self.database_root.exists())

    def test_stale_database_or_sarif_cannot_be_reused(self):
        with patch.object(security, "run", side_effect=self.command) as command:
            write_json(self.output / "go.sarif", codeql_sarif("go"))
            with self.assertRaises(ValueError):
                scan_codeql(self.executable, self.database_root, self.output)
            self.output.joinpath("go.sarif").unlink()
            self.database_root.mkdir()
            with self.assertRaises(FileExistsError):
                scan_codeql(self.executable, self.database_root, self.output)
            self.assertEqual(2, command.call_count)

    def test_success_requires_both_sarif_files_before_security_receipt(self):
        folder = self.output.parent
        with patch.object(security, "run", side_effect=self.command), \
                patch("sys.argv", ["security.py", "codeql", "--codeql", str(self.executable),
                                   "--database-root", str(self.database_root),
                                   "--output", str(self.output)]), \
                patch("tools.ci.common.revision", return_value="a" * 40), \
                redirect_stdout(io.StringIO()):
            self.assertEqual(0, security.main())
        write_json(folder / "vulnerabilities/report.json", {"sha": "a" * 40, "status": "passed"})
        receipt = record("security", folder, "a" * 40)
        self.assertTrue({"codeql/go.sarif", "codeql/javascript.sarif",
                         "codeql/report.json", "vulnerabilities/report.json"}.issubset(receipt["files"]))
        (self.output / "javascript.sarif").unlink()
        with self.assertRaises(ValueError):
            record("security", folder, "a" * 40)

    def test_workflow_keeps_one_security_job_and_the_nine_required_receipts(self):
        workflow = yaml.safe_load((ROOT / ".github/workflows/ci.yml").read_text())
        self.assertEqual(set(JOBS), set(workflow["jobs"]) - {"foundation"})
        self.assertEqual(set(JOBS), set(workflow["jobs"]["foundation"]["needs"]))
        job = workflow["jobs"]["security"]
        self.assertNotIn("strategy", job)
        commands = [step.get("run", "") for step in job["steps"]]
        scan = next(index for index, command in enumerate(commands) if "security.py codeql" in command)
        receipt = next(index for index, command in enumerate(commands) if "evidence.py --job security" in command)
        self.assertLess(scan, receipt)
        self.assertIn('--codeql "$RUNNER_TEMP/codeql/codeql"', commands[scan])
        self.assertIn("--database-root", commands[scan])
        self.assertIn("--output output/ci/security/codeql", commands[scan])
        self.assertTrue(any("security.py govulncheck" in command for command in commands[:scan]))
        for step in job["steps"]:
            self.assertFalse(step.get("continue-on-error", False))
