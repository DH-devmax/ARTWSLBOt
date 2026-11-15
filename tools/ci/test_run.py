import ast
import hashlib
from pathlib import Path
import tempfile
import unittest

from tools.ci.common import report
from tools.ci.run import RecordedResult, node_results, optional_skips


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
