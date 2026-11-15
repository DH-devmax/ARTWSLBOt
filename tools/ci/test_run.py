import ast
import hashlib
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from tools.ci.common import report
from tools.ci.run import RecordedResult, node_checks, node_results, optional_skips


class RunnerPolicyTests(unittest.TestCase):
    def fixture(self, root):
        filename = root / "test_example.py"
        source = "class Example:\n    def test_optional(self):\n        pass\n"
        filename.write_text(source)
        method = ast.parse(source).body[0].body[0]
        return {"id": "test_example.Example.test_optional",
                "fingerprint": hashlib.sha256(
                    ast.dump(method, include_attributes=False).encode()).hexdigest(),
                "reason": "Optional local artifact"}

    def test_changed_missing_and_duplicate_exceptions_fail(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            entry = self.fixture(root)
            policy = {"schema": 1, "entries": [entry]}
            self.assertIn(entry["id"], optional_skips(
                policy, ["test_example.py"], [entry["id"]], root))
            with self.assertRaises(ValueError):
                optional_skips(policy, ["test_example.py"], [], root)
            with self.assertRaises(ValueError):
                optional_skips({"schema": 1, "entries": [entry, entry]},
                               ["test_example.py"], [entry["id"]], root)
            (root / "test_example.py").write_text(
                "class Example:\n    def test_optional(self):\n        return 1\n")
            with self.assertRaises(ValueError):
                optional_skips(policy, ["test_example.py"], [entry["id"]], root)

    def test_unapproved_skip_is_failure(self):
        case = unittest.FunctionTestCase(lambda: None)
        result = RecordedResult()
        result.addSkip(case, "temporary")
        self.assertEqual("failed", report("python", result.cases)["status"])

    def test_approved_skip_is_visible_with_reason(self):
        case = unittest.FunctionTestCase(lambda: None)
        result = RecordedResult({case.id(): "Optional local artifact"})
        result.addSkip(case, "optional")
        value = report("python", result.cases)
        self.assertEqual("passed", value["status"])
        self.assertEqual(1, value["counts"]["skipped"])
        self.assertTrue(value["cases"][0]["approved"])

    def test_node_requires_individual_nonempty_unique_source_results(self):
        root = Path("/synthetic")
        case = '<testcase name="case" classname="test" file="/synthetic/example.mjs"/>'
        self.assertEqual("passed", node_results(
            "example.mjs", "<testsuites>" + case + "</testsuites>", 0, root)[0]["status"])
        for body in ("", case + case, case.replace("/synthetic/", "/foreign/")):
            with self.subTest(body=body), self.assertRaises(ValueError):
                node_results("example.mjs", "<testsuites>" + body + "</testsuites>", 0, root)

    def test_node_skip_failure_and_nonzero_exit_cannot_pass(self):
        root = Path("/synthetic")
        for state in ("skipped", "failure", "error"):
            text = ('<testsuites><testcase name="case" classname="test" '
                    f'file="/synthetic/example.mjs"><{state}/></testcase></testsuites>')
            self.assertEqual("failed", node_results("example.mjs", text, 0, root)[0]["status"])
        text = ('<testsuites><testcase name="case" classname="test" '
                'file="/synthetic/example.mjs"/></testsuites>')
        self.assertEqual("failed", node_results("example.mjs", text, 7, root)[-1]["status"])

    def test_node22_junit_binds_missing_files_only_to_explicit_invocation(self):
        root = Path("/synthetic")
        text = ('<?xml version="1.0" encoding="utf-8"?><testsuites>'
                '<testcase name="first" time="0.001" classname="test"/>'
                '<testcase name="second &amp; third" time="0.002" classname="test"/>'
                '<!-- tests 2 --><!-- pass 2 --></testsuites>')
        with self.assertRaises(ValueError):
            node_results("example.mjs", text, 0, root)
        for source in ("example.mjs", "/foreign/example.mjs"):
            with self.subTest(source=source), self.assertRaises(ValueError):
                node_results("example.mjs", text, 0, root, invoked_file=source)
        cases = node_results("example.mjs", text, 0, root,
                             invoked_file=root / "example.mjs")
        self.assertEqual(
            ["example.mjs:test/first", "example.mjs:test/second & third"],
            [case["id"] for case in cases],
        )
        self.assertEqual(["passed", "passed"], [case["status"] for case in cases])

    def test_invocation_never_overrides_reported_or_missing_case_identity(self):
        root = Path("/synthetic")
        for attributes in (
            'name="case" classname="test" file="/foreign/example.mjs"',
            'name="case" classname="test" file="example.mjs"',
            'name="case" classname="test" file=""',
            'name="" classname="test"',
            'name="case" classname=""',
            'classname="test"',
            'name="case"',
        ):
            text = f"<testsuites><testcase {attributes}/></testsuites>"
            with self.subTest(attributes=attributes), self.assertRaises(ValueError):
                node_results("example.mjs", text, 0, root,
                             invoked_file=root / "example.mjs")

    def test_node22_binding_preserves_empty_duplicate_skip_and_failure_checks(self):
        root = Path("/synthetic")
        case = '<testcase name="case" classname="test"/>'
        for body in ("", case + case):
            with self.subTest(body=body), self.assertRaises(ValueError):
                node_results("example.mjs", f"<testsuites>{body}</testsuites>", 0,
                             root, invoked_file=root / "example.mjs")
        for state, code in (("skipped", "unapproved_skip"),
                            ("failure", "assertion_failed"),
                            ("error", "assertion_failed")):
            text = ('<testsuites><testcase name="case" classname="test">'
                    f'<{state}/></testcase></testsuites>')
            with self.subTest(state=state):
                cases = node_results("example.mjs", text, 0, root,
                                     invoked_file=root / "example.mjs")
                self.assertEqual([("failed", code)],
                                 [(item["status"], item["code"]) for item in cases])
        cases = node_results("example.mjs", f"<testsuites>{case}</testsuites>",
                             7, root, invoked_file=root / "example.mjs")
        self.assertEqual(("failed", "child_failed"),
                         (cases[-1]["status"], cases[-1]["code"]))

    def test_node_runner_uses_one_entry_per_process_and_continues_after_errors(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            entries = ["timeout.mjs", "missing.mjs", "execution.mjs",
                       "malformed.mjs", "empty.mjs", "passing.mjs"]
            passing = ('<testsuites><testcase name="case" classname="test"/>'
                       '</testsuites>')
            outcomes = [
                subprocess.TimeoutExpired(["node"], 300, output="private output"),
                FileNotFoundError("private path"),
                RuntimeError("private error"),
                subprocess.CompletedProcess(["node"], 7, "not XML", "private stderr"),
                subprocess.CompletedProcess(["node"], 0, "<testsuites/>", ""),
                subprocess.CompletedProcess(["node"], 0, passing, ""),
            ]
            with patch("tools.ci.run.run", side_effect=outcomes) as execute:
                cases = node_checks(entries, root)
            self.assertEqual(
                ["child_timeout", "child_start_failed", "child_execution_error",
                 "invalid_node_report", "invalid_node_report", "ok"],
                [case["code"] for case in cases],
            )
            self.assertEqual(["failed"] * 5 + ["passed"],
                             [case["status"] for case in cases])
            self.assertEqual(7, cases[3]["returncode"])
            self.assertEqual(0, cases[4]["returncode"])
            self.assertEqual(len(entries), execute.call_count)
            for entry, call in zip(entries, execute.call_args_list):
                self.assertEqual(
                    (["node", "--experimental-transform-types",
                      "--experimental-vm-modules", "--test",
                      "--test-reporter=junit", entry],), call.args,
                )
                self.assertEqual({"timeout": 300, "cwd": root}, call.kwargs)
            self.assertTrue(all(case["duration_seconds"] >= 0 for case in cases))
            self.assertNotIn("private", str(cases))

    def test_real_node_reports_all_named_cases_and_top_level_assertions(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            (root / "named.mjs").write_text(
                "import test from 'node:test';\n"
                "import assert from 'node:assert/strict';\n"
                "test('first & second', () => assert.equal(1, 1));\n"
                "test('third', () => assert.equal(2, 2));\n"
            )
            (root / "script.mjs").write_text(
                "import assert from 'node:assert/strict';\nassert.equal(3, 3);\n"
            )
            cases = node_checks(["named.mjs", "script.mjs"], root)
            self.assertEqual(
                ["named.mjs:test/first & second", "named.mjs:test/third",
                 "script.mjs:test/script.mjs"],
                [case["id"] for case in cases],
            )
            self.assertEqual(["passed"] * 3, [case["status"] for case in cases])

    def test_real_node_failure_and_skip_cannot_pass(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            (root / "failure.mjs").write_text(
                "import test from 'node:test';\n"
                "test('failing', () => { throw new Error('synthetic failure'); });\n"
            )
            (root / "skip.mjs").write_text(
                "import test from 'node:test';\ntest.skip('skipped', () => {});\n"
            )
            cases = node_checks(["failure.mjs", "skip.mjs"], root)
            self.assertEqual(["failure.mjs:test/failing", "skip.mjs:test/skipped"],
                             [case["id"] for case in cases])
            self.assertEqual(["failed", "failed"], [case["status"] for case in cases])
            self.assertEqual(["assertion_failed", "unapproved_skip"],
                             [case["code"] for case in cases])
