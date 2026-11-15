import json
import tempfile
import unittest
from pathlib import Path

from tools.ci.security import codeql_findings, vulnerabilities


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
            value = {"version": "2.1.0", "runs": [{
                "tool": {"driver": {"name": "CodeQL", "version": "2.27.4"}}, "results": [],
                "invocations": [{"executionSuccessful": True}]}]}
            for path in paths:
                path.write_text(json.dumps(value))
            self.assertEqual([], codeql_findings(paths))
            with self.assertRaises(ValueError):
                codeql_findings(paths[:1])
            value["runs"][0]["invocations"][0]["executionSuccessful"] = False
            paths[0].write_text(json.dumps(value))
            with self.assertRaises(ValueError):
                codeql_findings(paths)

    def test_new_codeql_finding_is_a_gate(self):
        with tempfile.TemporaryDirectory() as temporary:
            paths = [Path(temporary) / f"{language}.sarif" for language in ("go", "javascript")]
            for path in paths:
                path.write_text(json.dumps({"version": "2.1.0", "runs": [{
                    "tool": {"driver": {"name": "CodeQL", "version": "2.27.4"}},
                    "invocations": [{"executionSuccessful": True}],
                    "results": [{"ruleId": "synthetic/new-finding"}]}]}))
            self.assertEqual(2, len(codeql_findings(paths)))
