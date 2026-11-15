import copy
import tempfile
import unittest
from pathlib import Path

from tools.ci.common import digest, load_json, write_json
from tools.ci.evidence import JOBS, REQUIRED, record
from tools.ci.foundation import validate

SHA = "a" * 40


class FoundationTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.needs = {job: {"result": "success"} for job in JOBS}
        for job in JOBS:
            folder = self.root / job
            self.fixture(job, folder)
            record(job, folder, SHA)

    def fixture(self, job, folder):
        for filename in REQUIRED[job]:
            path = folder / filename
            path.parent.mkdir(parents=True, exist_ok=True)
            if path.suffix == ".json":
                value = {"sha": SHA, "status": "passed"}
                if filename == "gopsutil-boundaries.json":
                    value.update(schema=1, check="gopsutil-boundaries")
                write_json(path, value)
            else:
                path.write_text("synthetic")
        if job == "architecture":
            path = folder / "rendered/system.svg"
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("<svg/>")

    def test_all_jobs_with_intact_artifacts(self):
        self.assertEqual("passed", validate(self.needs, self.root, SHA)["status"])

    def test_failed_cancelled_skipped_jobs_never_pass(self):
        for state in ("failure", "cancelled", "skipped", None):
            with self.subTest(state=state):
                needs = copy.deepcopy(self.needs)
                needs["database"]["result"] = state
                with self.assertRaises(ValueError):
                    validate(needs, self.root, SHA)

    def test_missing_job_or_report_fails(self):
        needs = copy.deepcopy(self.needs)
        del needs["database"]
        with self.assertRaises(ValueError):
            validate(needs, self.root, SHA)
        (self.root / "database/job-report.json").unlink()
        with self.assertRaises(ValueError):
            validate(self.needs, self.root, SHA)

    def test_local_metrics_dependency_regression_is_required(self):
        (self.root / "unit/gopsutil-boundaries.json").unlink()
        with self.assertRaises(ValueError):
            record("unit", self.root / "unit", SHA)

    def test_invalid_metrics_dependency_receipt_cannot_be_recorded(self):
        path = self.root / "unit/gopsutil-boundaries.json"
        valid = load_json(path)
        for change in ({"sha": "b" * 40}, {"status": "failed"}, {"schema": 0},
                       {"check": "unrelated"}, None):
            with self.subTest(change=change):
                write_json(path, {} if change is None else valid | change)
                with self.assertRaises(ValueError):
                    record("unit", self.root / "unit", SHA)

    def test_rehashed_invalid_metrics_dependency_receipt_cannot_pass(self):
        folder = self.root / "unit"
        path = folder / "gopsutil-boundaries.json"
        valid = load_json(path)
        receipt_path = folder / "job-report.json"
        for change in ({"sha": "b" * 40}, {"status": "failed"}, {"schema": 0},
                       {"check": "unrelated"}, None):
            with self.subTest(change=change):
                write_json(path, {} if change is None else valid | change)
                receipt = load_json(receipt_path)
                receipt["files"][path.name] = digest(path)
                write_json(receipt_path, receipt)
                with self.assertRaises(ValueError):
                    validate(self.needs, self.root, SHA)

    def test_stale_source_and_tampered_report_fail(self):
        with self.assertRaises(ValueError):
            validate(self.needs, self.root, "b" * 40)
        (self.root / "unit/report.json").write_text("{}")
        with self.assertRaises(ValueError):
            validate(self.needs, self.root, SHA)

    def test_duplicate_job_evidence_fails(self):
        folder = self.root / "duplicate"
        self.fixture("unit", folder)
        record("unit", folder, SHA)
        with self.assertRaises(ValueError):
            validate(self.needs, self.root, SHA)

    def test_private_artifacts_refused(self):
        (self.root / "unit/.env").write_text("synthetic")
        with self.assertRaises(ValueError):
            record("unit", self.root / "unit", SHA)

    def test_incomplete_and_failed_lint_evidence_cannot_pass(self):
        (self.root / "unit/unit.cover").unlink()
        with self.assertRaises(ValueError):
            record("unit", self.root / "unit", SHA)
        write_json(self.root / "frontend/eslint/lint.json",
                   {"sha": SHA, "status": "failed"})
        with self.assertRaises(ValueError):
            record("frontend", self.root / "frontend", SHA)
