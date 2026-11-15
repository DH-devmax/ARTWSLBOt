"""Synthetic runner tests: no real Go tests, Docker, database or native calls."""

import importlib.util
import io
import json
import re
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from unittest.mock import patch


SPEC = importlib.util.spec_from_file_location("_wsl_go_tests", Path(__file__).with_name("go_tests.py"))
runner = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = runner
SPEC.loader.exec_module(runner)
coverage = runner.coverage

SHA = "a" * 40
TOOLCHAIN = {"GOVERSION": "go1.27.0", "GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "1"}
PACKAGE = "example/internal/control"
SOURCE = {"file": "internal/control/a_test.go", "line": 3, "end_line": 5,
          "sha256": "b" * 64, "fingerprint": "c" * 64}


def event(action, test=None, package=PACKAGE, **values):
    result = {"Action": action, "Package": package, **values}
    if test is not None:
        result["Test"] = test
    return result


def policy_entry(profile="unit", name="TestA", **values):
    return {"package": PACKAGE, "test": name, "profile": profile,
            "fingerprint": SOURCE["fingerprint"], "reason": "isolated fixture",
            **values}


class DatabaseSafetyTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.write_env()

    def write_env(self, database="wsl_test"):
        (self.root / ".env").write_text("".join(
            f"{key}=postgres://test:secret@127.0.0.1:5432/{database}?sslmode=disable\n"
            for key in runner.DB_KEYS), encoding="utf-8")

    def test_accepts_exact_fixture_and_environment(self):
        environment = {"PGDATABASE": "wsl_test",
                       "DATABASE_URL": "postgresql://test:secret@postgres/wsl_test"}
        self.assertEqual(len(runner.validate_database(self.root, environment)), 64)

    def test_fixture_and_environment_are_both_checked(self):
        self.write_env("wsl")
        with self.assertRaises(runner.RunnerError):
            runner.validate_database(self.root, {})
        self.write_env()
        for key in runner.DB_KEYS:
            with self.subTest(key=key), self.assertRaises(runner.RunnerError):
                runner.validate_database(self.root, {key: "postgres://x@localhost/wsl"})

    def test_rejects_environment_database_and_service_options(self):
        for environment in ({"PGDATABASE": "wsl"}, {"PGSERVICE": "production"},
                            {"PGSERVICEFILE": "/outside"}, {"PGOPTIONS": "-c dbname=wsl"}):
            with self.subTest(environment=environment), self.assertRaises(runner.RunnerError):
                runner.validate_database(self.root, environment)

    def test_rejects_overrides_malformed_urls_and_wrong_names(self):
        values = [
            "postgres://x@127.0.0.1/wsl", "postgres://x@127.0.0.1/wsl_test?dbname=wsl",
            "postgres://x@127.0.0.1/wsl_test?database=wsl",
            "postgres://x@127.0.0.1/wsl_test?options=-c%20dbname%3Dwsl",
            "postgres://x@127.0.0.1/wsl_test?host=elsewhere",
            "postgres://x@127.0.0.1/wsl_test?service=other",
            "postgres://x@127.0.0.1/wsl_test?dbname=wsl_test&dbname=wsl",
            "postgres://x@127.0.0.1/%77sl", "postgres://x@127.0.0.1/wsl_test/extra",
            "postgres://x@127.0.0.1:bad/wsl_test", "postgres://x@[broken/wsl_test",
            "postgres://x@127.0.0.1/wsl_test#fragment", "dbname=wsl_test",
            "postgres://x@127.0.0.1/wsl_test?sslmode", "", None,
        ]
        for value in values:
            with self.subTest(value=value), self.assertRaises(runner.RunnerError):
                runner.validate_dsn(value)

    def test_accepts_encoded_exact_database_and_default_port(self):
        runner.validate_dsn("postgres://x@127.0.0.1/%77sl_test?dbname=wsl_test&sslmode=disable")

    def test_duplicate_fixture_values_rejected(self):
        with (self.root / ".env").open("a") as handle:
            handle.write("DATABASE_URL=postgres://x@127.0.0.1/wsl_test\n")
        with self.assertRaises(runner.RunnerError):
            runner.validate_database(self.root, {})

    def test_unsafe_database_fails_before_any_subprocess(self):
        self.write_env("wsl")
        with patch.object(runner, "capture") as capture:
            report = runner.run_profile("database", self.root / "output", self.root, environment={})
        capture.assert_not_called()
        self.assertEqual(report["status"], "failed")
        text = (self.root / "output/report.json").read_text()
        self.assertNotIn("secret", text)
        self.assertNotIn("postgres://", text)

    def test_unit_disables_all_integration_and_performance_switches(self):
        original = {key: "1" for key in (*runner.OPT_INS, "WSL_INTEGRATION",
                                        "WSL_GATEWAY_INTEGRATION")}
        result = runner.build_environment(original, "unit")
        self.assertEqual(original["WSL_INTEGRATION"], "1")
        self.assertEqual(result["WSL_INTEGRATION"], "0")
        self.assertEqual(result["WSL_GATEWAY_INTEGRATION"], "0")
        self.assertTrue(all(result[key] != "1" for key in runner.OPT_INS))
        self.assertEqual(result["GOWORK"], "off")

    def test_database_flags_are_package_specific(self):
        for package in runner.DB_PACKAGES:
            with self.subTest(package=package):
                env = runner.build_environment({}, "database", package)
                gateway = package.endswith("gateway")
                self.assertEqual(env["WSL_INTEGRATION"], "0" if gateway else "1")
                self.assertEqual(env["WSL_GATEWAY_INTEGRATION"], "1" if gateway else "0")


class InventoryTests(unittest.TestCase):
    def collect(self, events, expected=None, policy=None, code=0):
        inventory = runner.Inventory(expected or {(PACKAGE, "TestA"): SOURCE},
                                     [PACKAGE], policy or {}, "unit")
        for item in [event("start"), *events, event("pass")]:
            inventory.consume(item)
        return inventory.finish(code)

    def test_parent_pass(self):
        result = self.collect([event("run", "TestA"), event("pass", "TestA", Elapsed=0.25)])
        self.assertEqual(result["status"], "passed")
        self.assertEqual(result["counts"]["expected"], 1)
        self.assertEqual(result["counts"]["passed"], 1)
        self.assertEqual(result["cases"][0]["source"], SOURCE)

    def test_approved_skip_records_exact_reason_and_fingerprint(self):
        entry = policy_entry()
        result = self.collect([event("run", "TestA"), event("skip", "TestA")],
                              policy={(PACKAGE, "TestA"): entry})
        self.assertEqual(result["status"], "passed")
        self.assertEqual(result["counts"]["skipped"], 1)
        self.assertEqual(result["cases"][0]["skip"]["fingerprint"], SOURCE["fingerprint"])

    def test_unapproved_skip_rejected_without_name_heuristics(self):
        for name in ("TestA", "TestBenchmark", "TestPerformance", "TestPostgres"):
            with self.subTest(name=name):
                result = self.collect([event("run", name), event("skip", name)],
                                      expected={(PACKAGE, name): SOURCE})
                self.assertIn("unapproved_skip", result["errors"])

    def test_child_failure_never_hidden_by_parent_pass(self):
        result = self.collect([event("run", "TestA"), event("run", "TestA/child"),
                               event("fail", "TestA/child"), event("pass", "TestA")])
        self.assertEqual(result["status"], "failed")
        self.assertEqual(result["counts"]["child_failures"], 1)
        self.assertIn("child_failure", result["errors"])

    def test_parent_skip_policy_does_not_authorize_child_skip(self):
        result = self.collect([event("run", "TestA"), event("run", "TestA/child"),
                               event("skip", "TestA/child"), event("pass", "TestA")],
                              policy={(PACKAGE, "TestA"): policy_entry()})
        self.assertIn("unapproved_skip", result["errors"])

    def test_exact_child_policy_is_supported(self):
        result = self.collect([event("run", "TestA"), event("run", "TestA/child"),
                               event("skip", "TestA/child"), event("pass", "TestA")],
                              policy={(PACKAGE, "TestA/child"): policy_entry(name="TestA/child")})
        self.assertEqual(result["status"], "passed")

    def test_missing_parent_and_unfinished_child_rejected(self):
        result = self.collect([event("run", "TestA/child")])
        self.assertEqual(result["counts"]["missing"], 1)
        self.assertIn("missing_parent", result["errors"])
        self.assertIn("missing_test_terminal", result["errors"])

    def test_unfinished_report_distinguishes_started_from_unstarted(self):
        result = self.collect([event("run", "TestA"), event("run", "TestA/child")],
                              expected={(PACKAGE, "TestA"): SOURCE,
                                        (PACKAGE, "TestB"): SOURCE}, code=1)
        self.assertEqual([case["started"] for case in result["cases"]], [True, False])
        self.assertEqual(result["unfinished"], [
            {"package": PACKAGE, "name": "TestA"},
            {"package": PACKAGE, "name": "TestA/child"},
        ])
        self.assertEqual(result["exit_code"], 1)

    def test_fixed_failure_markers_never_store_raw_output(self):
        result = self.collect([
            event("run", "TestA"),
            event("output", Output="panic: test timed out after 15m0s\nPRIVATE PAYLOAD"),
            event("output", Output="panic: SECRET VALUE\nfatal error: PRIVATE DATA"),
            event("build-output", Output="/private/build: no space left on device"),
        ], code=1)
        self.assertEqual(result["diagnostics"], [
            "package_timeout", "runtime_fatal", "storage_exhausted", "test_panic",
        ])
        for forbidden in ("PRIVATE", "SECRET", "/private/build", "15m0s"):
            self.assertNotIn(forbidden, json.dumps(result))

    def test_printed_marker_cannot_replace_test_completion(self):
        result = self.collect([event("run", "TestA"),
                               event("output", Output="panic: synthetic diagnostic"),
                               event("pass", "TestA")])
        self.assertEqual(result["status"], "passed")
        self.assertEqual(result["diagnostics"], ["test_panic"])
        self.assertEqual(result["unfinished"], [])

    def test_duplicate_run_terminal_and_terminal_without_run(self):
        for events, problem in [
            ([event("run", "TestA"), event("run", "TestA"), event("pass", "TestA")],
             "duplicate_test_run"),
            ([event("run", "TestA"), event("pass", "TestA"), event("pass", "TestA")],
             "duplicate_test_terminal"),
            ([event("pass", "TestA")], "test_terminal_without_run"),
        ]:
            with self.subTest(problem=problem):
                self.assertIn(problem, self.collect(events)["errors"])

    def test_unknown_parent_and_package_are_errors(self):
        result = self.collect([event("run", "TestOther"), event("pass", "TestOther"),
                               event("pass", "TestA", package="unknown")])
        self.assertIn("unexpected_test", result["errors"])
        self.assertIn("unknown_package", result["errors"])

    def test_package_failure_and_exit_code_are_errors(self):
        result = self.collect([event("run", "TestA"), event("pass", "TestA"), event("fail")], code=1)
        self.assertIn("package_failure", result["errors"])
        self.assertIn("go_test_exit", result["errors"])
        self.assertIn("duplicate_package_terminal", result["errors"])

    def test_missing_package_terminal(self):
        inventory = runner.Inventory({}, [PACKAGE], {}, "unit")
        inventory.consume(event("start"))
        self.assertIn("missing_package_terminal", inventory.finish(0)["errors"])

    def test_empty_package_can_pass_without_tests(self):
        inventory = runner.Inventory({}, [PACKAGE], {}, "unit")
        inventory.consume(event("start"))
        inventory.consume(event("skip"))
        self.assertEqual(inventory.finish(0)["status"], "passed")

    def test_nonfinite_elapsed_rejected_and_output_discarded(self):
        for elapsed in (float("nan"), float("inf"), -1, True, "invalid"):
            with self.subTest(elapsed=elapsed):
                result = self.collect([event("output", "TestA", Output="SENSITIVE SECRET"),
                                       event("run", "TestA"),
                                       event("pass", "TestA", Elapsed=elapsed)])
                self.assertIn("invalid_test_elapsed", result["errors"])
                self.assertNotIn("SENSITIVE", json.dumps(result))

    def test_parallel_event_interleaving_and_fuzz_parent(self):
        expected = {(PACKAGE, "TestA"): SOURCE, (PACKAGE, "FuzzA"): SOURCE}
        result = self.collect([event("run", "TestA"), event("pause", "TestA"),
                               event("run", "FuzzA"), event("run", "FuzzA/seed#0"),
                               event("pass", "FuzzA/seed#0"), event("pass", "FuzzA"),
                               event("cont", "TestA"), event("pass", "TestA")],
                              expected=expected)
        self.assertEqual(result["status"], "passed")
        self.assertEqual(result["counts"]["actual"], 2)


class SourceAndPolicyTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.directory = self.root / "internal/control"
        self.directory.mkdir(parents=True)
        self.path = self.directory / "a_test.go"
        self.package = {"Dir": str(self.directory), "ImportPath": PACKAGE,
                        "TestGoFiles": ["a_test.go"], "XTestGoFiles": []}

    def test_function_fingerprint_handles_comments_literals_and_external_tests(self):
        self.path.write_text('''package control
// func TestFake(t *testing.T) {}
var ignored = "func TestFake2(t *testing.T) {}"
func TestA(t *testing.T) {
    text := `raw { } /* comment */`
    _ = "{\\\"}"
    // }
    /* { */
    if true { _ = text }
}
func helper() {}
''')
        first = runner.test_sources(self.root, self.package)
        self.assertEqual(set(first), {"TestA"})
        self.assertEqual(first["TestA"]["line"], 4)
        self.path.write_text(self.path.read_text().replace("func helper() {}", "func helper() { println(1) }"))
        second = runner.test_sources(self.root, self.package)
        self.assertEqual(first["TestA"]["fingerprint"], second["TestA"]["fingerprint"])
        self.path.write_text(self.path.read_text().replace("if true", "if false"))
        third = runner.test_sources(self.root, self.package)
        self.assertNotEqual(first["TestA"]["fingerprint"], third["TestA"]["fingerprint"])
        self.assertNotEqual(first["TestA"]["sha256"], second["TestA"]["sha256"])
        self.package["XTestGoFiles"] = self.package.pop("TestGoFiles")
        self.package["TestGoFiles"] = []
        self.assertIn("TestA", runner.test_sources(self.root, self.package))

    def test_valid_whitespace_and_crlf_preserve_function_source(self):
        self.path.write_bytes(b"package control\r\nfunc\r\nTestA(t *testing.T) {}\r\n")
        sources = runner.test_sources(self.root, self.package)
        self.assertEqual(sources["TestA"]["line"], 2)
        first = sources["TestA"]["fingerprint"]
        self.path.write_bytes(self.path.read_bytes().replace(b"\r\n", b"\n"))
        self.assertNotEqual(first, runner.test_sources(self.root, self.package)["TestA"]["fingerprint"])

    def test_policy_exact_fingerprint_profile_and_no_unknown_fields(self):
        path = self.root / "policy.json"
        expected = {(PACKAGE, "TestA"): SOURCE}
        path.write_text(json.dumps({"schema": 1, "entries": [policy_entry()]}))
        self.assertEqual(len(runner.load_policy(path, expected, "unit")), 1)
        self.assertEqual(runner.load_policy(path, expected, "database"), {})
        cases = [
            [policy_entry(fingerprint="d" * 64)], [policy_entry(reason=" ")],
            [policy_entry(), policy_entry()], [policy_entry(name="TestMissing")],
            [policy_entry(unknown=True)], [policy_entry(performance="true")],
        ]
        for entries in cases:
            path.write_text(json.dumps({"schema": 1, "entries": entries}))
            with self.subTest(entries=entries), self.assertRaises(runner.RunnerError):
                runner.load_policy(path, expected, "unit")

    def test_database_policy_requires_explicit_performance_flag(self):
        path = self.root / "policy.json"
        expected = {(PACKAGE, "TestA"): SOURCE}
        for performance in (False, True):
            path.write_text(json.dumps({"schema": 1, "entries": [
                policy_entry(profile="database", performance=performance)]}))
            if performance:
                self.assertEqual(len(runner.load_policy(path, expected, "database")), 1)
            else:
                with self.assertRaises(runner.RunnerError):
                    runner.load_policy(path, expected, "database")

    def test_duplicate_json_keys_and_malformed_go_json_rejected(self):
        for text in ('{"Action":"run","Action":"pass"}', '{"Action":', '[]', '{} junk'):
            with self.subTest(text=text), self.assertRaises((runner.RunnerError, coverage.CoverageError)):
                list(runner.json_objects(text))

    def test_json_stream_handles_pretty_printed_and_ndjson_objects(self):
        self.assertEqual(list(runner.json_objects(' {\n"a":1\n}\n{"b":2}  ')),
                         [{"a": 1}, {"b": 2}])

    def test_discovery_uses_sodium_race_and_list_inventory(self):
        self.path.write_text("package control\nfunc TestA(t *testing.T) {}\n")
        (self.directory / "a.go").write_text("package control\n")
        package = dict(self.package, GoFiles=["a.go"])
        dependency = {"ImportPath": "fmt", "Dir": "/outside/stdlib/fmt", "DepOnly": True}
        listing = "\n".join(json.dumps(e) for e in (
            event("start"), event("output", Output="TestA\n"),
            event("output", Output="BenchmarkA\n"), event("pass")))
        responses = [json.dumps(dependency) + "\n" + json.dumps(package), listing]
        with patch.object(runner, "checked", side_effect=responses) as checked:
            packages, expected, files = runner.discover(self.root, {}, list(runner.OWNED_ROOTS))
        self.assertEqual(set(packages), {PACKAGE})
        self.assertEqual(set(expected), {(PACKAGE, "TestA")})
        self.assertIn("internal/control/a.go", files)
        for call in checked.call_args_list:
            command = call.args[0]
            self.assertIn("-race", command)
            self.assertEqual(command[command.index("-tags") + 1], "sodium")
            self.assertEqual(command[-3:], list(runner.OWNED_ROOTS))
            self.assertNotIn("./...", command)
        self.assertIn("-list", checked.call_args_list[1].args[0])
        self.assertIn("-deps", checked.call_args_list[0].args[0])
        self.assertIn("-test", checked.call_args_list[0].args[0])

    def dependency(self, relative="third_party/metrics"):
        directory = self.root / relative
        directory.mkdir(parents=True)
        (directory / "go.mod").write_text("module synthetic/metrics\n")
        (directory / "metrics.go").write_text("package metrics\n")
        return {"ImportPath": "synthetic/metrics", "Dir": str(directory),
                "DepOnly": True, "GoFiles": ["metrics.go"],
                "Module": {"Path": "synthetic/metrics",
                           "Replace": {"Path": "./" + relative, "Dir": str(directory)}}}

    def test_local_replacement_and_module_lock_are_bound_to_source(self):
        dependency = self.dependency()
        original = dict(self.package, GoFiles=[])
        self.path.write_text("package control\n")
        synthetic = dict(original, ImportPath=PACKAGE + ".test", Name="main")
        raw = "\n".join(json.dumps(value) for value in (original, dependency, synthetic))
        with patch.object(runner, "checked", return_value=raw):
            packages, first = runner.discover_packages(self.root, {}, runner.OWNED_ROOTS)
            self.assertEqual({PACKAGE}, set(packages))
            self.assertIn("third_party/metrics/go.mod", first)
            self.assertIn("third_party/metrics/metrics.go", first)
            (self.root / "third_party/metrics/metrics.go").write_text("package metrics\nvar Changed = true\n")
            _, second = runner.discover_packages(self.root, {}, runner.OWNED_ROOTS)
        self.assertNotEqual(coverage.source_digest(first), coverage.source_digest(second))

    def test_local_replacement_outside_approved_root_is_rejected(self):
        for relative in ("var/metrics", "internal/metrics"):
            dependency = self.dependency(relative)
            with self.subTest(relative=relative), \
                    patch.object(runner, "checked", return_value=json.dumps(dependency)), \
                    self.assertRaises(runner.RunnerError):
                runner.discover_packages(self.root, {}, runner.OWNED_ROOTS)
        dependency["Module"]["Replace"]["Dir"] = str(self.root.parent)
        with patch.object(runner, "checked", return_value=json.dumps(dependency)), \
                self.assertRaises(runner.RunnerError):
            runner.discover_packages(self.root, {}, runner.OWNED_ROOTS)

    def test_versioned_cached_replacement_does_not_read_external_source(self):
        self.path.write_text("package control\n")
        dependency = {"ImportPath": "synthetic/metrics", "Dir": "/outside/cache",
                      "DepOnly": True, "Module": {"Replace": {
                          "Path": "synthetic/metrics", "Version": "v1.0.0",
                          "Dir": "/outside/cache"}}}
        raw = json.dumps(dependency) + "\n" + json.dumps(self.package)
        with patch.object(runner, "checked", return_value=raw):
            packages, files = runner.discover_packages(self.root, {}, runner.OWNED_ROOTS)
        self.assertEqual({PACKAGE}, set(packages))
        self.assertEqual({"internal/control/a_test.go"}, set(files))


class RunnerExecutionTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        (self.root / "go.mod").write_text("module example\n\ngo 1.27.0\n")
        self.packages = {}
        self.expected = {}
        self.snapshot = {"go.mod": coverage.file_hash(self.root / "go.mod")}
        for relative in runner.DB_PACKAGES:
            directory = self.root / relative
            directory.mkdir(parents=True)
            (directory / "a.go").write_text("package " + directory.name + "\n")
            (directory / "a_test.go").write_text("package " + directory.name + "\n")
            package = "example/" + relative
            self.packages[package] = {"Dir": str(directory), "ImportPath": package,
                                      "Module": {"Path": "example"}, "GoFiles": ["a.go"]}
            source = dict(SOURCE, file=relative + "/a_test.go")
            self.expected[(package, "TestA")] = source
            for name in ("a.go", "a_test.go"):
                self.snapshot[relative + "/" + name] = coverage.file_hash(directory / name)
        (self.root / ".env").write_text("".join(
            f"{key}=postgres://test:secret@127.0.0.1:5432/wsl_test\n" for key in runner.DB_KEYS))
        self.calls = []

    def fake_capture(self, command, root, environment, timeout=1800):
        self.calls.append((list(command), dict(environment)))
        names = list(self.packages) if command[-1] == "./tools/..." else [
            "example/" + command[-1].removeprefix("./")]
        expression = next((value.removeprefix("-run=") for value in command
                           if value.startswith("-run=")), None)
        events = []
        counts = {}
        for name in names:
            events.append(event("start", package=name))
            parents = [test for package, test in self.expected if package == name
                       and (expression is None or re.search(expression, test))]
            counts[name] = len(parents)
            for test in parents:
                events.extend([event("run", test, package=name),
                               event("output", test, package=name, Output="SECRET MUST NOT ESCAPE")])
                if test.startswith("Fuzz"):
                    events.extend([event("run", test + "/seed#0", package=name),
                                   event("pass", test + "/seed#0", package=name)])
                events.append(event("pass", test, package=name))
            events.append(event("pass", package=name))
        path = Path(next(value.split("=", 1)[1] for value in command
                         if value.startswith("-coverprofile=")))
        path.write_text("mode: atomic\n" + "".join(
            name + f"/a.go:1.1,2.1 1 {counts[name]}\n" for name in names))
        return subprocess.CompletedProcess(command, 0, "\n".join(json.dumps(e) for e in events))

    def set_parents(self, package, names):
        source = next(source for key, source in self.expected.items() if key[0] == package)
        self.expected = {key: value for key, value in self.expected.items() if key[0] != package}
        self.expected.update({(package, name): source for name in names})

    def run_fixture(self, profile, side_effect=None, output="output"):
        with patch.object(runner, "source_sha", return_value=SHA), \
                patch.object(runner, "toolchain", return_value=TOOLCHAIN), \
                patch.object(runner, "discover",
                             return_value=(self.packages, self.expected, self.snapshot)), \
                patch.object(runner, "discover_packages",
                             return_value=(self.packages, self.snapshot)), \
                patch.object(runner, "capture", side_effect=side_effect or self.fake_capture):
            return runner.run_profile(profile, self.root / output, self.root, environment={})

    def test_unit_executes_all_once_and_delivers_validated_artifacts(self):
        report = self.run_fixture("unit")
        self.assertEqual(report["status"], "passed")
        self.assertEqual(report["counts"]["passed"], 3)
        self.assertEqual(len(self.calls), 1)
        command, env = self.calls[0]
        for flag in ("-race", "-count=1", "-json", "-covermode=atomic"):
            self.assertIn(flag, command)
        self.assertFalse(any(value.startswith("-run=") for value in command))
        self.assertNotIn("-p=1", command)
        self.assertNotIn("-parallel=1", command)
        self.assertEqual(command[-3:], list(runner.OWNED_ROOTS))
        self.assertNotIn("./...", command)
        self.assertEqual(env["WSL_INTEGRATION"], "0")
        self.assertNotIn("SECRET", json.dumps(report))
        bundle = coverage.load_bundle(self.root / "output", self.root)
        self.assertEqual(len(bundle), 1)
        self.assertEqual(len(bundle[0][1]), 3)

    def test_database_runs_three_packages_serially_with_separate_profiles(self):
        report = self.run_fixture("database")
        self.assertEqual(report["status"], "passed")
        self.assertEqual(len(self.calls), 3)
        self.assertEqual(len(report["coverage"]), 3)
        for (command, environment), relative in zip(self.calls, runner.DB_PACKAGES):
            self.assertEqual(command[-1], "./" + relative)
            self.assertIn("-p=1", command)
            self.assertIn("-parallel=1", command)
            self.assertEqual(environment["WSL_INTEGRATION"], "0" if relative.endswith("gateway") else "1")
            self.assertEqual(environment["WSL_GATEWAY_INTEGRATION"], "1" if relative.endswith("gateway") else "0")
        self.assertEqual(len(coverage.load_bundle(self.root / "output", self.root)), 3)

    def test_database_batches_sort_and_cover_every_parent_once_with_package_artifacts(self):
        parents = [
            ["ExampleA", "FuzzA", "TestA", "TestAB"] + [f"Test{i:03d}" for i in range(61)],
            [f"Test{i:03d}" for i in range(33)],
            [f"Test{i:03d}" for i in range(32)],
        ]
        for package, names in zip(self.packages, parents):
            self.set_parents(package, reversed(names))
        report = self.run_fixture("database")
        self.assertEqual(report["status"], "passed")
        self.assertEqual(report["counts"]["expected"], 130)
        self.assertEqual(report["counts"]["passed"], 130)
        self.assertEqual(report["counts"]["missing"], 0)
        self.assertEqual(report["counts"]["children"], 1)
        self.assertEqual(len(self.calls), 6)
        self.assertEqual(len(report["runs"]), 6)
        self.assertEqual({(case["package"], case["name"]) for case in report["cases"]},
                         set(self.expected))
        self.assertEqual(report["inventory"], [
            {"package": key[0], "name": key[1], "source": source}
            for key, source in sorted(self.expected.items())])
        plan = [(relative, sorted(names)[start:start + 32])
                for relative, names in zip(runner.DB_PACKAGES, parents)
                for start in range(0, len(names), 32)]
        for (command, environment), run, (relative, names) in zip(
                self.calls, report["runs"], plan):
            self.assertEqual(command[-1], "./" + relative)
            for flag in ("-race", "-count=1", "-json", "-covermode=atomic",
                         "-p=1", "-parallel=1", "-timeout=15m"):
                self.assertIn(flag, command)
            self.assertEqual(command[command.index("-tags") + 1], "sodium")
            expression = next(value.removeprefix("-run=") for value in command
                              if value.startswith("-run="))
            self.assertEqual(expression, "^(" + "|".join(names) + ")$")
            for name in names:
                self.assertIsNotNone(re.search(expression, name))
                self.assertIsNone(re.search(expression, "Prefix" + name))
                self.assertIsNone(re.search(expression, name + "Suffix"))
            self.assertEqual([case["name"] for case in run["cases"]], names)
            gateway = relative.endswith("gateway")
            self.assertEqual(environment["WSL_INTEGRATION"], "0" if gateway else "1")
            self.assertEqual(environment["WSL_GATEWAY_INTEGRATION"], "1" if gateway else "0")
        self.assertEqual([item["file"] for item in report["coverage"]], [
            "database-control.cover", "database-httpapi.cover", "database-gateway.cover"])
        bundle = coverage.load_bundle(self.root / "output", self.root)
        self.assertEqual(len(bundle), 3)
        for (meta, blocks), names in zip(bundle, parents):
            self.assertEqual([block.count for block in blocks.values()], [len(names)])
            self.assertEqual(meta["sha"], SHA)
            self.assertEqual(meta["toolchain"], TOOLCHAIN)
            self.assertEqual(meta["source_files"], self.snapshot)
        self.assertNotIn("SECRET", json.dumps(report))
        self.assertEqual(self.run_fixture("unit", output="unit")["status"], "passed")
        summary = coverage.merge_directories(
            [self.root / "unit", self.root / "output"], self.root / "merged", self.root)
        self.assertEqual(summary["status"], "passed")
        self.assertEqual(len(summary["scopes"]), 4)
        merged = coverage.parse_profile((self.root / "merged/merged.cover").read_text())
        self.assertEqual({block.file: block.count for block in merged.values()}, {
            package + "/a.go": 2 * len(names)
            for package, names in zip(self.packages, parents)})

    def test_unit_does_not_batch_large_inventories(self):
        self.set_parents(PACKAGE, [f"Test{i:03d}" for i in range(65)])
        report = self.run_fixture("unit")
        self.assertEqual(report["status"], "passed")
        self.assertEqual(report["counts"]["passed"], 67)
        self.assertEqual(len(self.calls), 1)
        self.assertFalse(any(value.startswith("-run=") for value in self.calls[0][0]))
        self.assertEqual([item["file"] for item in report["coverage"]], ["unit.cover"])
        self.assertEqual(len(coverage.load_bundle(self.root / "output", self.root)), 1)

    def test_failed_batch_stops_before_later_batches_and_packages(self):
        self.set_parents(PACKAGE, [f"Test{i:03d}" for i in range(65)])

        def fail(command, root, environment, timeout=1800):
            result = self.fake_capture(command, root, environment, timeout)
            if len(self.calls) != 2:
                return result
            events = list(runner.json_objects(result.stdout))
            for item in events:
                if item["Action"] == "pass" and item.get("Test") in ("Test032", None):
                    item["Action"] = "fail"
            return subprocess.CompletedProcess(command, 1, "\n".join(json.dumps(e) for e in events))

        report = self.run_fixture("database", fail)
        self.assertEqual(report["status"], "failed")
        self.assertEqual(len(self.calls), 2)
        self.assertEqual(report["counts"]["expected"], 67)
        self.assertEqual(report["counts"]["failed"], 1)
        self.assertEqual(report["counts"]["missing"], 3)
        self.assertEqual(report["coverage"], [])
        self.assertFalse((self.root / "output/database-control.cover").exists())

    def test_incomplete_batch_cannot_pass_with_a_zero_exit_code(self):
        self.set_parents(PACKAGE, [f"Test{i:03d}" for i in range(33)])

        def omit_parent(command, root, environment, timeout=1800):
            result = self.fake_capture(command, root, environment, timeout)
            if len(self.calls) != 2:
                return result
            events = [item for item in runner.json_objects(result.stdout)
                      if item.get("Test") != "Test032"]
            return subprocess.CompletedProcess(command, 0, "\n".join(json.dumps(e) for e in events))

        report = self.run_fixture("database", omit_parent)
        self.assertEqual(report["status"], "failed")
        self.assertEqual(len(self.calls), 2)
        self.assertIn("missing_parent", report["errors"])
        self.assertEqual(report["counts"]["expected"], 35)
        self.assertEqual(report["counts"]["missing"], 3)
        self.assertEqual(report["coverage"], [])

    def test_database_fixture_change_between_batches_stops_before_the_next_command(self):
        self.set_parents(PACKAGE, [f"Test{i:03d}" for i in range(33)])

        def change_fixture(command, root, environment, timeout=1800):
            result = self.fake_capture(command, root, environment, timeout)
            with (self.root / ".env").open("a") as handle:
                handle.write("# synthetic fixture changed\n")
            return result

        report = self.run_fixture("database", change_fixture)
        self.assertEqual(report["status"], "failed")
        self.assertEqual(len(self.calls), 1)
        self.assertIn("runner_validation_failed", report["errors"])
        self.assertEqual(report["counts"]["expected"], 35)
        self.assertEqual(report["counts"]["missing"], 3)
        self.assertEqual(report["coverage"], [])

    def test_batch_timeout_retains_full_inventory_and_safe_unfinished_records(self):
        self.set_parents(PACKAGE, [f"Test{i:03d}" for i in range(65)])

        def timeout(command, root, environment, timeout=1800):
            result = self.fake_capture(command, root, environment, timeout)
            if len(self.calls) != 2:
                return result
            events = [event("start"), event("run", "Test032"),
                      event("run", "Test032/child"),
                      event("output", Output="panic: test timed out after 15m0s\nSECRET PAYLOAD"),
                      event("fail")]
            return subprocess.CompletedProcess(command, 1, "\n".join(json.dumps(e) for e in events))

        report = self.run_fixture("database", timeout)
        self.assertEqual(report["status"], "failed")
        self.assertEqual(len(self.calls), 2)
        self.assertEqual(report["counts"]["expected"], 67)
        self.assertEqual(report["counts"]["passed"], 32)
        self.assertEqual(report["counts"]["missing"], 35)
        started = [case["name"] for case in report["cases"] if case.get("started")]
        self.assertEqual(started, ["Test032"])
        self.assertEqual(report["runs"][1]["diagnostics"], ["package_timeout"])
        self.assertEqual(report["runs"][1]["unfinished"], [
            {"package": PACKAGE, "name": "Test032"},
            {"package": PACKAGE, "name": "Test032/child"},
        ])
        self.assertEqual(report["coverage"], [])
        self.assertNotIn("SECRET", json.dumps(report))

    def test_database_batch_coverage_layout_change_fails_closed(self):
        self.set_parents(PACKAGE, [f"Test{i:03d}" for i in range(33)])

        def change_layout(command, root, environment, timeout=1800):
            result = self.fake_capture(command, root, environment, timeout)
            if len(self.calls) == 2:
                path = Path(next(value.split("=", 1)[1] for value in command
                                 if value.startswith("-coverprofile=")))
                path.write_text(path.read_text().replace("1.1,2.1", "1.1,3.1"))
            return result

        report = self.run_fixture("database", change_layout)
        self.assertEqual(report["status"], "failed")
        self.assertEqual(len(self.calls), 2)
        self.assertIn("runner_validation_failed", report["errors"])
        self.assertEqual(report["coverage"], [])
        with self.assertRaises(coverage.CoverageError):
            coverage.load_bundle(self.root / "output", self.root)

    def test_database_batch_coverage_provenance_change_fails_closed(self):
        self.set_parents(PACKAGE, [f"Test{i:03d}" for i in range(33)])
        original = runner.coverage_metadata

        def change_provenance(root, path, *args):
            meta = original(root, path, *args)
            if path.name.endswith("-batch-002.cover"):
                meta["toolchain"] = dict(TOOLCHAIN, GOVERSION="go1.28.0")
            return meta

        with patch.object(runner, "coverage_metadata", side_effect=change_provenance):
            report = self.run_fixture("database")
        self.assertEqual(report["status"], "failed")
        self.assertEqual(len(self.calls), 2)
        self.assertIn("runner_validation_failed", report["errors"])
        self.assertEqual(report["coverage"], [])

    def test_database_failure_stops_before_later_package(self):
        def fail(command, root, environment, timeout=1800):
            result = self.fake_capture(command, root, environment, timeout)
            return subprocess.CompletedProcess(command, 1, result.stdout.replace(
                '"Action": "pass", "Package": "example/internal/control", "Test": "TestA"',
                '"Action": "fail", "Package": "example/internal/control", "Test": "TestA"'))
        report = self.run_fixture("database", fail)
        self.assertEqual(report["status"], "failed")
        self.assertEqual(len(self.calls), 1)
        self.assertEqual(report["counts"]["missing"], 2)
        self.assertEqual(report["coverage"], [])

    def test_source_mutation_is_detected(self):
        def mutate(command, root, environment, timeout=1800):
            result = self.fake_capture(command, root, environment, timeout)
            (self.root / "internal/control/a.go").write_text("package changed\n")
            return result
        report = self.run_fixture("unit", mutate)
        self.assertEqual(report["status"], "failed")
        self.assertIn("runner_validation_failed", report["errors"])
        with self.assertRaises(coverage.CoverageError):
            coverage.load_bundle(self.root / "output", self.root)

    def test_malformed_or_truncated_stream_fails_with_missing_inventory(self):
        def truncate(command, root, environment, timeout=1800):
            result = self.fake_capture(command, root, environment, timeout)
            return subprocess.CompletedProcess(command, 0, result.stdout[:30])
        report = self.run_fixture("unit", truncate)
        self.assertEqual(report["status"], "failed")
        self.assertEqual(report["counts"]["missing"], 3)
        self.assertIn("invalid_go_test_json", report["errors"])

    def test_no_raw_subprocess_output_or_timeout_in_diagnostics(self):
        process = unittest.mock.Mock(pid=1234)
        process.communicate.side_effect = [
            subprocess.TimeoutExpired(["go"], 1, output="SECRET VALUE"), ("", None)]
        with patch.object(runner.subprocess, "Popen", return_value=process), \
                patch.object(runner.os, "killpg") as kill:
            with self.assertRaises(runner.RunnerError) as caught:
                runner.capture(["go"], self.root, {})
        kill.assert_called_once_with(1234, runner.signal.SIGKILL)
        self.assertNotIn("SECRET", str(caught.exception))
        with patch.object(runner, "run_profile", return_value={
                "schema": 1, "profile": "unit", "status": "failed", "counts": {},
                "errors": ["SECRET VALUE"]}):
            output = io.StringIO()
            with redirect_stdout(output), redirect_stderr(output):
                code = runner.main(["--profile", "unit", "--output", str(self.root / "unused")])
        self.assertEqual(code, 1)
        self.assertNotIn("SECRET", output.getvalue())

    def test_cancellation_kills_group_and_is_recorded(self):
        process = unittest.mock.Mock(pid=1234)
        process.communicate.side_effect = [KeyboardInterrupt(), ("", None)]
        with patch.object(runner.subprocess, "Popen", return_value=process), \
                patch.object(runner.os, "killpg") as kill:
            with self.assertRaises(KeyboardInterrupt):
                runner.capture(["go"], self.root, {})
        kill.assert_called_once_with(1234, runner.signal.SIGKILL)
        report = self.run_fixture("unit", lambda *args: (_ for _ in ()).throw(KeyboardInterrupt()))
        self.assertEqual(report["status"], "failed")
        self.assertIn("runner_cancelled", report["errors"])
        self.assertEqual(report["counts"]["expected"], 3)
        self.assertEqual(report["counts"]["missing"], 3)

    def test_timeout_keeps_expected_inventory(self):
        def timeout(*args):
            raise runner.RunnerError("command timed out")
        report = self.run_fixture("database", timeout)
        self.assertEqual(report["status"], "failed")
        self.assertEqual(report["counts"]["expected"], 3)
        self.assertEqual(report["counts"]["missing"], 3)

    def test_added_source_file_and_toolchain_drift_fail(self):
        with patch.object(runner, "source_sha", return_value=SHA), \
                patch.object(runner, "toolchain", return_value=TOOLCHAIN), \
                patch.object(runner, "discover",
                             return_value=(self.packages, self.expected, self.snapshot)), \
                patch.object(runner, "discover_packages",
                             return_value=(self.packages, dict(self.snapshot, new="b" * 64))), \
                patch.object(runner, "capture", side_effect=self.fake_capture):
            report = runner.run_profile("unit", self.root / "new-source", self.root, environment={})
        self.assertEqual(report["status"], "failed")
        with patch.object(runner, "source_sha", return_value=SHA), \
                patch.object(runner, "toolchain", side_effect=[
                    TOOLCHAIN, dict(TOOLCHAIN, GOVERSION="go1.28.0")]), \
                patch.object(runner, "discover",
                             return_value=(self.packages, self.expected, self.snapshot)), \
                patch.object(runner, "discover_packages",
                             return_value=(self.packages, self.snapshot)), \
                patch.object(runner, "capture", side_effect=self.fake_capture):
            report = runner.run_profile("unit", self.root / "new-toolchain", self.root, environment={})
        self.assertEqual(report["status"], "failed")

    def test_output_reuse_is_rejected(self):
        self.run_fixture("unit")
        with self.assertRaises(runner.RunnerError):
            runner.run_profile("unit", self.root / "output", self.root, environment={})

    def test_source_only_image_sha_and_head_mismatch(self):
        result = subprocess.CompletedProcess([], 128, "")
        with patch.object(runner, "capture", return_value=result):
            self.assertEqual(runner.source_sha(self.root, {"GITHUB_SHA": SHA}), SHA)
            with self.assertRaises(runner.RunnerError):
                runner.source_sha(self.root, {})
        with patch.object(runner, "capture", return_value=subprocess.CompletedProcess([], 0, SHA + "\n")):
            with self.assertRaises(runner.RunnerError):
                runner.source_sha(self.root, {}, "b" * 40)


if __name__ == "__main__":
    unittest.main()
