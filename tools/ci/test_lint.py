"""Synthetic lint contracts; no services, native connections, or tool downloads."""

from collections import Counter
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location(
    "wsl_ci_lint", Path(__file__).with_name("lint.py")
)
lint = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(lint)
MANIFEST = json.loads(
    Path(__file__).with_name("toolchain.json").read_text(encoding="utf-8")
)


def finding(**changes):
    return {
        "tool": "ruff",
        "file": "tools/example.py",
        "rule": "F401",
        "code_fingerprint": "a" * 64,
        "line": 1,
        "column": 1,
        "message": "unused import",
        "classification": "static_legacy",
        **changes,
    }


def entry(diagnostic=None, count=1):
    diagnostic = diagnostic or finding()
    return {field: diagnostic[field] for field in lint.KEY_FIELDS} | {"count": count}


class BaselineTests(unittest.TestCase):
    def test_exact_existing_finding_passes(self):
        _, resolved, counts = lint.compare([finding()], [entry()], {"ruff"})
        self.assertEqual(
            counts, {"total": 1, "legacy": 1, "new": 0, "security": 0, "resolved": 0}
        )
        self.assertEqual(resolved, [])

    def test_moved_code_retains_identity(self):
        result, _, counts = lint.compare([finding(line=57)], [entry()], {"ruff"})
        self.assertEqual(counts["legacy"], 1)
        self.assertEqual(result[0]["line"], 57)

    def test_new_file_rule_or_code_cannot_borrow_baseline(self):
        for change in [
            {"file": "tools/other.py"},
            {"rule": "F821"},
            {"code_fingerprint": "b" * 64},
        ]:
            with self.subTest(change=change):
                _, _, counts = lint.compare([finding(**change)], [entry()], {"ruff"})
                self.assertEqual((counts["new"], counts["resolved"]), (1, 1))

    def test_duplicate_count_is_not_a_blanket_ignore(self):
        _, _, counts = lint.compare([finding(), finding(line=8)], [entry()], {"ruff"})
        self.assertEqual((counts["legacy"], counts["new"]), (1, 1))
        _, _, counts = lint.compare([finding()], [entry(count=2)], {"ruff"})
        self.assertEqual(counts["resolved"], 1)

    def test_resolved_baseline_requires_cleanup(self):
        _, resolved, counts = lint.compare([], [entry()], {"ruff"})
        self.assertEqual(counts["resolved"], 1)
        self.assertEqual(resolved, [entry()])

    def test_partial_check_keeps_other_tools(self):
        old = entry(
            finding(tool="staticcheck", file="internal/example.go", rule="SA1019")
        )
        _, resolved, _ = lint.compare([], [old], {"ruff"})
        self.assertEqual(resolved, [])
        updated = lint.update_baseline([old], [finding()], {"ruff"}, ci=False)
        self.assertIn(old, updated)

    def test_security_is_never_legacy_even_with_forged_entry(self):
        security = finding(
            tool="govulncheck",
            file="go.mod",
            rule="GO-2026-1234",
            classification="security",
        )
        _, _, counts = lint.compare([security], [entry(security)], {"govulncheck"})
        self.assertEqual((counts["security"], counts["legacy"]), (1, 0))
        with self.assertRaisesRegex(ValueError, "security"):
            lint.update_baseline([], [security], {"govulncheck"}, ci=False)

    def test_manual_write_is_disabled_in_ci(self):
        with self.assertRaisesRegex(ValueError, "disabled in CI"):
            lint.update_baseline([], [finding()], {"ruff"}, ci=True)

    def test_new_ci_sources_remain_new_after_manual_legacy_refresh(self):
        current = finding(file="tools/ci/new_check.py", classification="static")
        baseline = lint.update_baseline([], [finding(), current], {"ruff"}, ci=False)
        self.assertEqual(baseline, [entry()])
        _, _, counts = lint.compare([finding(), current], baseline, {"ruff"})
        self.assertEqual((counts["legacy"], counts["new"]), (1, 1))
        with self.assertRaises(ValueError):
            lint.validate_baseline({"schema_version": 1, "entries": [entry(current)]})

    def test_manual_budget_is_bounded(self):
        with (
            patch.object(lint, "MAX_ENTRIES", 1),
            self.assertRaisesRegex(ValueError, "budget"),
        ):
            lint.update_baseline(
                [], [finding(), finding(rule="F821")], {"ruff"}, ci=False
            )
        with (
            patch.object(lint, "MAX_DIAGNOSTICS", 1),
            self.assertRaisesRegex(ValueError, "budget"),
        ):
            lint.update_baseline([], [finding(), finding()], {"ruff"}, ci=False)

    def test_rejects_wildcards_traversal_duplicates_and_scanner_entries(self):
        for change in [
            {"file": "../outside.py"},
            {"file": "/tmp/outside.py"},
            {"file": "tools/./example.py"},
            {"file": "tools\\example.py"},
            {"rule": "*"},
            {"code_fingerprint": "*"},
            {"count": True},
            {"count": 0},
            {"tool": "govulncheck", "rule": "GO-2026-1234"},
            {"tool": "staticcheck", "rule": "compile"},
            {"tool": "ruff", "rule": "invalid-syntax"},
            {"tool": "eslint", "rule": "security/detect-eval-with-expression"},
        ]:
            with self.subTest(change=change), self.assertRaises(ValueError):
                lint.validate_baseline(
                    {"schema_version": 1, "entries": [{**entry(), **change}]}
                )
        with self.assertRaisesRegex(ValueError, "duplicate baseline"):
            lint.validate_baseline({"schema_version": 1, "entries": [entry(), entry()]})

    def test_json_rejects_duplicate_keys_truncation_and_noise(self):
        for text in [
            '{"code":1,"code":2}',
            '{"code":',
            '{"code":1} garbage',
            "[]",
            '{"code":NaN}',
            '{"code":Infinity}',
        ]:
            with self.subTest(text=text), self.assertRaises(ValueError):
                lint.json_stream(text)
        self.assertEqual(
            lint.json_stream('{\n"config":{}\n}\n{"SBOM":{}}'),
            [{"config": {}}, {"SBOM": {}}],
        )


class RunnerTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        (self.root / "tools/ci").mkdir(parents=True)
        (self.root / "internal").mkdir()
        (self.root / "tools/example.py").write_text(
            "import os\nprint(1)\n", encoding="utf-8"
        )
        (self.root / "internal/example.go").write_text(
            "package example\nvar unused = 1\n", encoding="utf-8"
        )
        (self.root / "go.mod").write_text(
            "module example\n\ngo 1.27.0\n", encoding="utf-8"
        )
        self.runner = lint.Runner(self.root, self.root / "out", MANIFEST)
        revision_patch = patch.object(lint, "revision", return_value="1" * 40)
        revision_patch.start()
        self.addCleanup(revision_patch.stop)
        self.runner.repository_files = [
            self.root / "tools/example.py",
            self.root / "internal/example.go",
            self.root / "go.mod",
        ]

    @staticmethod
    def completed(stdout, code=0, stderr=""):
        return subprocess.CompletedProcess(["synthetic"], code, stdout, stderr)

    def ruff_result(self):
        return [
            {
                "filename": str(self.root / "tools/example.py"),
                "code": "F401",
                "message": "unused import",
                "location": {"row": 1, "column": 1},
                "end_location": {"row": 1, "column": 10},
            }
        ]

    def test_ruff_valid_diagnostic_exit_one_is_static(self):
        with (
            patch.object(self.runner, "version"),
            patch.object(
                self.runner,
                "command",
                return_value=self.completed(json.dumps(self.ruff_result()), 1),
            ),
        ):
            self.runner.run("ruff")
        self.assertEqual(self.runner.failures, [])
        self.assertEqual(len(self.runner.diagnostics), 1)
        self.assertEqual(self.runner.diagnostics[0]["classification"], "static_legacy")

    def test_ruff_empty_malformed_or_inconsistent_findings_fail(self):
        for result in [
            self.completed("", 1),
            self.completed("[]", 1),
            self.completed("null"),
            self.completed("[]", 0, "panic"),
            self.completed(json.dumps(self.ruff_result()), 0),
            self.completed('[{"filename": 42}]', 1),
        ]:
            with self.subTest(result=result):
                self.runner.failures.clear()
                with (
                    patch.object(self.runner, "version"),
                    patch.object(self.runner, "command", return_value=result),
                ):
                    self.runner.run("ruff")
                self.assertTrue(self.runner.failures)

    def test_staticcheck_json_and_compile_failure(self):
        diagnostic = {
            "code": "U1000",
            "severity": "warning",
            "message": "unused",
            "location": {
                "file": str(self.root / "internal/example.go"),
                "line": 2,
                "column": 5,
            },
        }
        with (
            patch.object(self.runner, "version"),
            patch.object(
                self.runner,
                "command",
                return_value=self.completed(json.dumps(diagnostic), 1),
            ),
        ):
            self.runner.run("staticcheck")
        self.assertEqual(self.runner.failures, [])
        self.assertEqual(self.runner.diagnostics[0]["rule"], "U1000")
        diagnostic["code"] = "compile"
        with (
            patch.object(self.runner, "version"),
            patch.object(
                self.runner,
                "command",
                return_value=self.completed(json.dumps(diagnostic), 1),
            ),
        ):
            self.runner.run("staticcheck")
        self.assertTrue(self.runner.failures)

    def test_source_fingerprint_survives_move_and_changes_with_code(self):
        self.runner.freeze("ruff", [self.root / "tools/example.py"])
        original = self.runner.diagnostic("ruff", "tools/example.py", "F401", "unused")
        (self.root / "tools/example.py").write_text(
            "\nimport os\nprint(1)\n", encoding="utf-8"
        )
        moved = self.runner.diagnostic("ruff", "tools/example.py", "F401", "unused", 2)
        self.assertEqual(original["code_fingerprint"], moved["code_fingerprint"])
        (self.root / "tools/example.py").write_text(
            "import sys\nprint(1)\n", encoding="utf-8"
        )
        changed = self.runner.diagnostic("ruff", "tools/example.py", "F401", "unused")
        self.assertNotEqual(original["code_fingerprint"], changed["code_fingerprint"])
        with self.assertRaisesRegex(lint.ToolFailure, "source changed"):
            self.runner.unchanged()

    def test_outside_scope_and_invalid_ranges_fail(self):
        self.runner.freeze("ruff", [self.root / "tools/example.py"])
        for filename, line in [
            ("internal/example.go", 1),
            ("tools/example.py", 99),
            ("../outside.py", 1),
        ]:
            with self.subTest(filename=filename), self.assertRaises(lint.ToolFailure):
                self.runner.diagnostic("ruff", filename, "F401", "unused", line)

    def scan_events(self, finding=None):
        events = [
            {
                "config": {
                    "protocol_version": "v1.0.0",
                    "scanner_name": "govulncheck",
                    "scanner_version": "v1.9.0",
                    "db": "https://vuln.go.dev",
                    "scan_level": "symbol",
                    "scan_mode": "source",
                }
            },
            {"SBOM": {"modules": [{"path": "example"}], "roots": ["example/main"]}},
        ]
        if finding:
            events.append({"finding": finding})
        return "\n".join(json.dumps(event) for event in events)

    def test_govulncheck_clean_and_security_findings(self):
        for vulnerable in [False, True]:
            with self.subTest(vulnerable=vulnerable):
                self.runner.diagnostics.clear()
                text = self.scan_events(
                    {"osv": "GO-2026-1234", "trace": [{"module": "example"}]}
                    if vulnerable
                    else None
                )
                with (
                    patch.object(self.runner, "version"),
                    patch.object(
                        self.runner, "command", return_value=self.completed(text)
                    ),
                ):
                    self.runner.govulncheck()
                self.assertEqual(len(self.runner.diagnostics), int(vulnerable))
                if vulnerable:
                    self.assertEqual(
                        self.runner.diagnostics[0]["classification"], "security"
                    )

    def test_govulncheck_rejects_incomplete_and_malformed_protocol(self):
        for text in [
            "",
            "{}",
            '{"config":{}}',
            "[]",
            self.scan_events() + '\n{"error":{"text":"failed"}}',
        ]:
            with (
                self.subTest(text=text),
                patch.object(self.runner, "version"),
                patch.object(self.runner, "command", return_value=self.completed(text)),
                self.assertRaises((lint.ToolFailure, ValueError)),
            ):
                self.runner.govulncheck()

    def test_govulncheck_rejects_wrong_provenance_or_empty_scan(self):
        for old, new in [
            ('"v1.0.0"', '"v999"'),
            ('"v1.9.0"', '"v1.0.0"'),
            ('"symbol"', '"module"'),
            ('"https://vuln.go.dev"', '"https://untrusted.invalid"'),
            ('"roots": ["example/main"]', '"roots": []'),
            ('"modules": [{"path": "example"}]', '"modules": {}'),
        ]:
            with (
                self.subTest(old=old),
                patch.object(self.runner, "version"),
                patch.object(
                    self.runner,
                    "command",
                    return_value=self.completed(self.scan_events().replace(old, new)),
                ),
                self.assertRaises(lint.ToolFailure),
            ):
                self.runner.govulncheck()

    def test_scanner_nonzero_and_tool_crash_fail(self):
        self.runner.output.mkdir()
        for returncode in (1, 2, 3, -11):
            with (
                self.subTest(returncode=returncode),
                patch.object(
                    lint,
                    "run_process",
                    return_value=self.completed(self.scan_events(), returncode),
                ),
                self.assertRaises(lint.ToolFailure),
            ):
                self.runner.command("govulncheck", ["govulncheck", "-json"])
        with (
            patch.object(lint, "run_process", side_effect=FileNotFoundError("missing")),
            self.assertRaises(lint.ToolFailure),
        ):
            self.runner.command("ruff", ["ruff"])
        with (
            patch.object(
                lint, "run_process", side_effect=subprocess.TimeoutExpired(["ruff"], 1)
            ),
            self.assertRaises(lint.ToolFailure),
        ):
            self.runner.command("ruff", ["ruff"])

    def test_wrong_tool_version_or_build_module_sum_fails(self):
        with (
            patch.object(
                self.runner, "command", return_value=self.completed("ruff 0.0.1")
            ),
            self.assertRaises(lint.ToolFailure),
        ):
            self.runner.version("ruff")
        with (
            patch.object(self.runner, "executable", return_value="/tmp/staticcheck"),
            patch.object(
                self.runner,
                "command",
                side_effect=[
                    self.completed("staticcheck 2026.2.2 (0.8.2)\n"),
                    self.completed("\tmod\thonnef.co/go/tools\tv0.8.2\th1:wrong\n"),
                ],
            ),
            self.assertRaisesRegex(lint.ToolFailure, "provenance"),
        ):
            self.runner.version("staticcheck")

    def test_main_writes_failure_report_and_never_refreshes_on_failure(self):
        baseline_path = self.root / "tools/ci/lint-baseline.json"
        baseline = json.dumps({"schema_version": 1, "entries": [entry()]})
        baseline_path.write_text(baseline, encoding="utf-8")
        (self.root / "tools/ci/toolchain.json").write_text(
            json.dumps(MANIFEST), encoding="utf-8"
        )

        def fail(runner, _):
            runner.failures.append({"tool": "ruff", "error": "synthetic scanner crash"})

        with (
            patch.object(lint, "ROOT", self.root),
            patch.object(lint.Runner, "run", fail),
            contextlib.redirect_stdout(io.StringIO()),
        ):
            result = lint.main(
                [
                    "--check",
                    "ruff",
                    "--write-baseline",
                    "--output",
                    str(self.root / "report"),
                ]
            )
        self.assertEqual(result, 1)
        self.assertEqual(baseline_path.read_text(encoding="utf-8"), baseline)
        report = json.loads(
            (self.root / "report/lint.json").read_text(encoding="utf-8")
        )
        self.assertEqual(report["counts"]["failures"], 1)
        self.assertEqual(report["counts"]["resolved"], 1)
        self.assertFalse(report["passed"])
        self.assertEqual(report["sha"], "1" * 40)
        self.assertEqual(report["status"], "failed")
        self.assertEqual(report["profile"], "lint/ruff")

    def test_main_malformed_toolchain_still_writes_json(self):
        (self.root / "tools/ci/toolchain.json").write_text("{", encoding="utf-8")
        (self.root / "tools/ci/lint-baseline.json").write_text(
            '{"schema_version":1,"entries":[]}', encoding="utf-8"
        )
        with (
            patch.object(lint, "ROOT", self.root),
            contextlib.redirect_stdout(io.StringIO()),
        ):
            result = lint.main(
                ["--check", "ruff", "--output", str(self.root / "report")]
            )
        self.assertEqual(result, 1)
        report = json.loads(
            (self.root / "report/lint.json").read_text(encoding="utf-8")
        )
        self.assertEqual(report["failures"][0]["tool"], "toolchain")

    def test_main_new_and_resolved_findings_fail_without_changing_baseline(self):
        baseline_path = self.root / "tools/ci/lint-baseline.json"
        before = json.dumps({"schema_version": 1, "entries": [entry()]})
        baseline_path.write_text(before, encoding="utf-8")
        (self.root / "tools/ci/toolchain.json").write_text(
            json.dumps(MANIFEST), encoding="utf-8"
        )

        def collect(runner, _):
            runner.diagnostics.append(finding(code_fingerprint="b" * 64))

        with (
            patch.object(lint, "ROOT", self.root),
            patch.object(lint.Runner, "run", collect),
            contextlib.redirect_stdout(io.StringIO()),
        ):
            result = lint.main(
                ["--check", "ruff", "--output", str(self.root / "report")]
            )
        self.assertEqual(result, 1)
        report = json.loads(
            (self.root / "report/lint.json").read_text(encoding="utf-8")
        )
        self.assertEqual(
            (report["counts"]["new"], report["counts"]["resolved"]), (1, 1)
        )
        self.assertEqual(baseline_path.read_text(encoding="utf-8"), before)

    def test_main_ci_write_cannot_accept_new_findings(self):
        baseline_path = self.root / "tools/ci/lint-baseline.json"
        before = '{"schema_version":1,"entries":[]}'
        baseline_path.write_text(before, encoding="utf-8")
        (self.root / "tools/ci/toolchain.json").write_text(
            json.dumps(MANIFEST), encoding="utf-8"
        )

        def collect(runner, _):
            runner.diagnostics.append(finding())

        with (
            patch.object(lint, "ROOT", self.root),
            patch.object(lint.Runner, "run", collect),
            patch.dict(os.environ, {"CI": "true"}),
            contextlib.redirect_stdout(io.StringIO()),
        ):
            result = lint.main(
                [
                    "--check",
                    "ruff",
                    "--write-baseline",
                    "--output",
                    str(self.root / "report"),
                ]
            )
        self.assertEqual(result, 1)
        self.assertEqual(baseline_path.read_text(encoding="utf-8"), before)
        report = json.loads(
            (self.root / "report/lint.json").read_text(encoding="utf-8")
        )
        self.assertEqual(report["counts"]["new"], 1)
        self.assertIn("disabled in CI", report["failures"][0]["error"])

    def test_changed_git_revision_fails_evidence_binding(self):
        (self.root / "tools/ci/toolchain.json").write_text(
            json.dumps(MANIFEST), encoding="utf-8"
        )
        (self.root / "tools/ci/lint-baseline.json").write_text(
            '{"schema_version":1,"entries":[]}', encoding="utf-8"
        )
        with (
            patch.object(lint, "ROOT", self.root),
            patch.object(lint.Runner, "run"),
            patch.object(lint, "revision", side_effect=["1" * 40, "2" * 40]),
            contextlib.redirect_stdout(io.StringIO()),
        ):
            result = lint.main(
                ["--check", "ruff", "--output", str(self.root / "report")]
            )
        self.assertEqual(result, 1)
        report = json.loads(
            (self.root / "report/lint.json").read_text(encoding="utf-8")
        )
        self.assertEqual(report["failures"][0]["tool"], "evidence")


class RepositoryContractTests(unittest.TestCase):
    def test_tool_pins_have_integrity_and_scanner_cannot_be_baselined(self):
        for name in ("staticcheck", "govulncheck"):
            tool = MANIFEST["tools"][name]
            self.assertTrue(tool["module_sum"].startswith("h1:"))
            self.assertTrue(tool["go_mod_sum"].startswith("h1:"))
            self.assertEqual(
                tool["install"],
                ["go", "install", f"{tool['package']}@{tool['module_version']}"],
            )
            self.assertIn("sum.golang.org", tool["sumdb_url"])
        self.assertEqual(MANIFEST["go_tools_environment"]["GOSUMDB"], "sum.golang.org")
        self.assertFalse(MANIFEST["tools"]["govulncheck"]["baseline_allowed"])
        for asset in MANIFEST["tools"]["ruff"]["assets"].values():
            self.assertRegex(asset["sha256"], r"^[0-9a-f]{64}$")
            self.assertTrue(
                asset["url"].startswith(
                    "https://github.com/astral-sh/ruff/releases/download/0.17.5/"
                )
            )
        self.assertIn("govulncheck", lint.CHECKS["all"])
        for name in ("staticcheck", "govulncheck"):
            arguments = MANIFEST["tools"][name]["check_args"]
            self.assertNotIn("./...", arguments)
            self.assertEqual(
                arguments[-3:], ["./cmd/...", "./internal/...", "./tools/..."]
            )

    def test_product_config_has_no_generated_globals_or_demo_glob(self):
        config = (
            Path(__file__).resolve().parents[2] / "web/eslint.wsl.config.mjs"
        ).read_text(encoding="utf-8")
        self.assertNotIn(".auto-import.json", config)
        self.assertNotIn("src/views/**", config)
        self.assertNotIn("src/**", config)
        self.assertIn("ts.createSourceFile", config)
        self.assertIn("ts.resolveModuleName", config)
        self.assertIn("noInlineConfig: true", config)

    def test_checked_in_baseline_is_exact_and_bounded(self):
        baseline = lint.validate_baseline(
            json.loads(
                Path(__file__)
                .with_name("lint-baseline.json")
                .read_text(encoding="utf-8")
            )
        )
        counts = Counter(entry["tool"] for entry in baseline)
        self.assertNotIn("govulncheck", counts)


if __name__ == "__main__":
    unittest.main()
