"""Run only registered synthetic Python/Node checks and record complete results."""

import argparse
import ast
import hashlib
import importlib
from pathlib import Path
import sys
import time
import unittest
from xml.etree import ElementTree

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from tools.ci.common import ROOT, load_json, report, run, write_json
from tools.ci.registry import load


def flatten(suite):
    for item in suite:
        if isinstance(item, unittest.TestSuite):
            yield from flatten(item)
        else:
            yield item


class RecordedResult(unittest.TestResult):
    def __init__(self, skips=None):
        super().__init__()
        self.cases = []
        self.skips = skips or {}

    def addSuccess(self, test):
        super().addSuccess(test)
        self.cases.append({"id": test.id(), "status": "passed"})

    def addFailure(self, test, err):
        super().addFailure(test, err)
        self.cases.append({"id": test.id(), "status": "failed", "code": "assertion_failed"})

    def addError(self, test, err):
        super().addError(test, err)
        self.cases.append({"id": test.id(), "status": "failed", "code": "test_error"})

    def addSkip(self, test, reason):
        super().addSkip(test, reason)
        permitted = self.skips.get(test.id())
        self.cases.append({
            "id": test.id(), "status": "skipped" if permitted else "failed",
            "approved": bool(permitted),
            "code": "pinned_optional_artifact" if permitted else "unapproved_skip",
        })

    def addExpectedFailure(self, test, err):
        self.cases.append({"id": test.id(), "status": "failed", "code": "unapproved_expected_failure"})

    def addUnexpectedSuccess(self, test):
        self.cases.append({"id": test.id(), "status": "failed", "code": "unexpected_success"})

    def addSubTest(self, test, subtest, err):
        super().addSubTest(test, subtest, err)
        if err:
            self.cases.append({"id": subtest.id(), "status": "failed", "code": "subtest_failed"})


def optional_skips(value, entries, expected, root=ROOT):
    if value.get("schema") != 1 or not isinstance(value.get("entries"), list):
        raise ValueError("Invalid Python skip policy")
    fingerprints = {}
    for entry in entries:
        tree = ast.parse((root / entry).read_text())
        for cls in tree.body:
            if not isinstance(cls, ast.ClassDef):
                continue
            for method in cls.body:
                if isinstance(method, ast.FunctionDef):
                    name = entry[:-3].replace("/", ".") + "." + cls.name + "." + method.name
                    fingerprints[name] = hashlib.sha256(
                        ast.dump(method, include_attributes=False).encode()).hexdigest()
    approved = {}
    for item in value["entries"]:
        name = item.get("id")
        if (name in approved or name not in expected
                or not item.get("reason", "").strip()
                or fingerprints.get(name) != item.get("fingerprint")):
            raise ValueError("Missing, changed or duplicated optional test")
        approved[name] = item["reason"]
    return approved


def python_checks(entries):
    sys.path[:0] = [str(ROOT), str(ROOT / "tools"), str(ROOT / "ai"),
                    str(ROOT / "tools/architecture")]
    suites, errors = [], []
    for entry in entries:
        try:
            module = importlib.import_module(entry[:-3].replace("/", "."))
            suites.append(unittest.defaultTestLoader.loadTestsFromModule(module))
        except Exception:
            errors.append({"id": entry, "status": "failed", "code": "module_import_failed"})
    suite = unittest.TestSuite(suites)
    expected = [test.id() for test in flatten(suite)]
    if not expected or len(set(expected)) != len(expected):
        raise ValueError("Missing or repeated Python test discovery")
    skips = optional_skips(load_json(ROOT / "tools/ci/python-skips.json"),
                           entries, expected)
    result = RecordedResult(skips)
    suite.run(result)
    actual = {case["id"] for case in result.cases}
    missing = set(expected) - actual
    result.cases.extend({"id": name, "status": "failed", "code": "missing_result"}
                        for name in sorted(missing))
    return [*result.cases, *errors]


def node_results(entry, text, returncode, root=ROOT):
    document = ElementTree.fromstring(text)
    if document.tag != "testsuites":
        raise ValueError("Unexpected Node result format")
    cases, seen = [], set()
    for test in document.iter("testcase"):
        name, classname = test.get("name"), test.get("classname")
        filename = Path(test.get("file", ""))
        if (not name or not classname or not filename.is_absolute()
                or filename.resolve() != (root / entry).resolve()):
            raise ValueError("Missing Node case source")
        identifier = entry + ":" + classname + "/" + name
        if identifier in seen:
            raise ValueError("Repeated Node test")
        seen.add(identifier)
        skipped = test.find("skipped") is not None
        failed = any(test.find(state) is not None for state in ("failure", "error"))
        cases.append({"id": identifier, "status": "failed" if skipped or failed else "passed",
                      "code": "unapproved_skip" if skipped else "assertion_failed" if failed else "ok"})
    if not cases:
        raise ValueError("Node returned no test cases")
    if returncode and not any(case["status"] == "failed" for case in cases):
        cases.append({"id": entry + ":runner", "status": "failed", "code": "child_failed"})
    return cases


def node_checks(entries):
    cases = []
    for entry in entries:
        started = time.monotonic()
        try:
            completed = run(
                ["node", "--experimental-transform-types", "--experimental-vm-modules",
                 "--test", "--test-reporter=junit", entry], timeout=300,
            )
            results = node_results(entry, completed.stdout, completed.returncode)
            for case in results:
                case["duration_seconds"] = round(time.monotonic() - started, 3)
            cases.extend(results)
        except Exception:
            cases.append({"id": entry, "status": "failed", "code": "child_timeout_or_error"})
    return cases


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--profile", choices=("python", "node"), required=True)
    parser.add_argument("--output", type=Path, required=True)
    options = parser.parse_args()
    started = time.monotonic()
    entries = []
    try:
        inventory = load()
        entries = inventory[options.profile]
        cases = python_checks(entries) if options.profile == "python" else node_checks(entries)
    except Exception:
        cases = [{"id": options.profile + "/inventory", "status": "failed",
                  "code": "invalid_inventory_or_incomplete_discovery"}]
    value = report(options.profile, cases, started=started, extra={"entries": entries})
    write_json(options.output / "report.json", value)
    print(f"{options.profile}: {value['counts']}")
    return int(value["status"] != "passed")


if __name__ == "__main__":
    sys.exit(main())
