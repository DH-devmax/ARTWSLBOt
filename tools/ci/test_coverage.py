"""Synthetic coverage parser, provenance and merger regressions."""

import copy
import importlib.util
import io
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from pathlib import Path
from unittest.mock import patch


NAME = "_wsl_ci_coverage"
if NAME not in sys.modules:
    SPEC = importlib.util.spec_from_file_location(NAME, Path(__file__).with_name("coverage.py"))
    module = importlib.util.module_from_spec(SPEC)
    sys.modules[NAME] = module
    SPEC.loader.exec_module(module)
coverage = sys.modules[NAME]

SHA = "a" * 40
TOOLCHAIN = {"GOVERSION": "go1.27.0", "GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "1"}


class ProfileTests(unittest.TestCase):
    def test_atomic_profile_round_trip_and_duplicates_sum(self):
        blocks = coverage.parse_profile("mode: atomic\n"
                                        "example/a.go:2.1,3.2 2 1\n"
                                        "example/a.go:2.1,3.2 2 4\n"
                                        "example/a.go:4.1,4.2 0 0\n")
        self.assertEqual(len(blocks), 2)
        self.assertEqual(next(iter(blocks.values())).count, 5)
        self.assertEqual(coverage.parse_profile(coverage.profile_text(blocks)), blocks)

    def test_invalid_header_coordinates_counts_and_paths(self):
        rows = [
            "", "mode: set\n", "mode: count\n", "mode: atomic\n\n",
            "mode: atomic\nmode: atomic\n", "mode: atomic\nnot-a-block\n",
        ]
        rows += ["mode: atomic\n" + row + "\n" for row in [
            "example/a.go:0.1,2.1 1 0", "example/a.go:1.0,2.1 1 0",
            "example/a.go:2.1,1.1 1 0", "example/a.go:2.1,2.1 1 0",
            "example/a.go:1.1,2.1 -1 0", "example/a.go:1.1,2.1 1 -1",
            "example/a.go:1.1,2.1 1 1.5", "example/a.go:1.1,2.1 1 1 extra",
            "/absolute.go:1.1,2.1 1 0", "../escape.go:1.1,2.1 1 0",
            "example/../escape.go:1.1,2.1 1 0", "example//a.go:1.1,2.1 1 0",
            r"example\a.go:1.1,2.1 1 0",
            f"example/a.go:1.1,2.1 1 {coverage.MAX_COUNTER + 1}",
        ]]
        for text in rows:
            with self.subTest(text=text), self.assertRaises(coverage.CoverageError):
                coverage.parse_profile(text)

    def test_conflicting_statement_counts_and_overflow_rejected(self):
        for other in ("example/a.go:1.1,2.1 2 1",
                      f"example/a.go:1.1,2.1 1 {coverage.MAX_COUNTER}"):
            with self.subTest(other=other), self.assertRaises(coverage.CoverageError):
                coverage.parse_profile("mode: atomic\nexample/a.go:1.1,2.1 1 1\n" + other + "\n")

    def test_empty_atomic_profile_has_no_percentage(self):
        blocks = coverage.parse_profile("mode: atomic\n")
        self.assertEqual(coverage.totals(list(blocks.values())),
                         {"statements": 0, "covered": 0, "percent": None})

    def test_real_go_empty_block_coordinates_can_be_equal_and_hit(self):
        blocks = coverage.parse_profile(
            "mode: atomic\nexample/internal/control/a.go:143.23,143.23 0 50\n")
        self.assertEqual(len(blocks), 1)
        self.assertEqual(next(iter(blocks.values())).count, 50)
        self.assertEqual(coverage.totals(list(blocks.values()))["statements"], 0)

    def test_generated_malformed_lines_never_accepted(self):
        valid = "example/a.go:1.1,2.1 1 0"
        for position in range(len(valid)):
            with self.subTest(position=position), self.assertRaises(coverage.CoverageError):
                coverage.parse_profile("mode: atomic\n" + valid[:position] + "\0"
                                       + valid[position + 1:] + "\n")


class BundleTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        (self.root / "go.mod").write_text("module example\n")
        self.snapshot = {"go.mod": coverage.file_hash(self.root / "go.mod")}
        for directory in ("internal/control", "internal/httpapi", "internal/gateway"):
            path = self.root / directory / "a.go"
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("package " + path.parent.name + "\n")
            self.snapshot[path.relative_to(self.root).as_posix()] = coverage.file_hash(path)
        self.digest = coverage.source_digest(self.snapshot)

    def metadata(self, profile="unit", package="example/internal/control", count=1):
        inventory = {
            relative: {"package": "example/" + str(Path(relative).parent),
                       "instrumented": True}
            for relative in self.snapshot if relative.endswith(".go")
        }
        packages = sorted({item["package"] for item in inventory.values()}) \
            if profile == "unit" else [package]
        files = {
            item["package"] + "/a.go": {
                "path": relative, "sha256": self.snapshot[relative], "package": item["package"]}
            for relative, item in inventory.items() if item["package"] in packages
        }
        text = "mode: atomic\n" + "".join(
            f"{name}:1.1,2.1 2 {count}\n" for name in files)
        blocks = coverage.parse_profile(text)
        scope = {"profile": profile, "packages": packages}
        if profile == "database":
            scope["database"] = "wsl_test"
        meta = {"schema": 1, "sha": SHA, "module": "example", "source_digest": self.digest,
                "source_files": self.snapshot, "toolchain": TOOLCHAIN, "tags": ["sodium"],
                "race": True, "mode": "atomic", "status": "passed", "scope": scope,
                "coverage_inventory": inventory, "files": files}
        return copy.deepcopy(meta), blocks

    def bundle(self, name="unit", profile="unit"):
        directory = self.root / name
        directory.mkdir()
        profiles = [self.metadata(profile, "example/" + relative)
                    for relative in ("internal/control", "internal/httpapi", "internal/gateway")
                    ] if profile == "database" else [self.metadata()]
        report = {"schema": 1, "status": "passed", "profile": profile, "sha": SHA,
                  "toolchain": TOOLCHAIN, "source_digest": self.digest,
                  "coverage_inventory": profiles[0][0]["coverage_inventory"], "coverage": []}
        for index, (meta, blocks) in enumerate(profiles):
            file = f"{index}.cover"
            path = directory / file
            path.write_text(coverage.profile_text(blocks))
            meta["cover_sha256"] = coverage.file_hash(path)
            coverage.write_json(directory / (file + ".meta.json"), meta)
            report["coverage"].append({"file": file, "metadata": file + ".meta.json"})
        coverage.write_json(directory / "report.json", report)
        return directory

    def test_valid_unit_and_database_bundles(self):
        self.assertEqual(len(coverage.load_bundle(self.bundle(), self.root)), 1)
        self.assertEqual(len(coverage.load_bundle(self.bundle("database", "database"), self.root)), 3)

    def test_merge_compatible_unit_database_counters_and_package_file_summaries(self):
        unit = self.bundle()
        database = self.bundle("database", "database")
        summary = coverage.merge_directories([unit, database], self.root / "result", self.root)
        self.assertEqual(summary["status"], "passed")
        self.assertEqual(summary["total"], {"statements": 6, "covered": 6, "percent": 100.0})
        self.assertEqual(len(summary["packages"]), 3)
        self.assertEqual(len(summary["files"]), 3)
        merged = coverage.parse_profile((self.root / "result/merged.cover").read_text())
        self.assertEqual(merged[("example/internal/control/a.go", 1, 1, 2, 1)].count, 2)
        self.assertEqual(len(coverage.read_json(self.root / "result/merged.cover.meta.json")["scopes"]), 4)

    def test_low_coverage_is_a_summary_not_a_gate(self):
        meta, blocks = self.metadata(count=0)
        summary = coverage.summarize(blocks, meta["files"])
        self.assertEqual(summary["total"]["percent"], 0)

    def test_changed_file_summary_marks_uninstrumented_files(self):
        meta, blocks = self.metadata()
        summary = coverage.summarize(blocks, meta["files"],
                                     ["internal/control/a.go", "internal/control/new.go"])
        self.assertTrue(summary["changed_files"]["internal/control/a.go"]["instrumented"])
        self.assertFalse(summary["changed_files"]["internal/control/new.go"]["instrumented"])
        self.assertIsNone(summary["changed_files"]["internal/control/new.go"]["percent"])
        self.assertEqual(summary["changed_total"]["statements"], 2)

    def test_rejects_different_source_sha_toolchain_build_settings(self):
        left, blocks = self.metadata()
        for key, value in (("sha", "b" * 40),
                           ("toolchain", dict(TOOLCHAIN, GOVERSION="go1.28.0")),
                           ("tags", []), ("race", False), ("mode", "count"),
                           ("source_digest", "c" * 64)):
            right = copy.deepcopy(left)
            right[key] = value
            with self.subTest(key=key), self.assertRaises(coverage.CoverageError):
                coverage.merge_profiles([(left, blocks), (right, blocks)])

    def test_rejects_layout_and_statement_disagreement(self):
        left, blocks = self.metadata()
        for row in ("example/internal/control/a.go:1.1,2.1 3 1",
                    "example/internal/control/a.go:1.1,3.1 2 1"):
            other = coverage.parse_profile("mode: atomic\n" + row + "\n")
            with self.subTest(row=row), self.assertRaises(coverage.CoverageError):
                coverage.merge_profiles([(left, blocks), (copy.deepcopy(left), other)])

    def test_rejects_aggregate_overflow(self):
        meta, blocks = self.metadata(count=coverage.MAX_COUNTER)
        _, other = self.metadata(count=1)
        with self.assertRaises(coverage.CoverageError):
            coverage.merge_profiles([(meta, blocks), (meta, other)])

    def test_empty_and_missing_whole_file_profiles_cannot_be_resealed(self):
        for profile in ("unit", "database"):
            meta, blocks = self.metadata(profile)
            for empty in (False, True):
                with self.subTest(profile=profile, empty=empty):
                    changed = copy.deepcopy(meta)
                    dropped = next(iter(changed["files"]))
                    kept = {} if empty else {
                        key: block for key, block in blocks.items() if block.file != dropped}
                    changed["files"] = {
                        name: item for name, item in changed["files"].items()
                        if any(block.file == name for block in kept.values())}
                    with self.assertRaises(coverage.CoverageError):
                        coverage.validate_scope(changed, kept)

    def test_unit_cannot_drop_a_package_from_both_scope_and_coverage(self):
        meta, blocks = self.metadata()
        dropped = meta["scope"]["packages"].pop()
        meta["files"] = {name: item for name, item in meta["files"].items()
                         if item["package"] != dropped}
        kept = {key: block for key, block in blocks.items() if block.file in meta["files"]}
        with self.assertRaises(coverage.CoverageError):
            coverage.validate_scope(meta, kept)

    def test_compiled_manifest_is_required_and_source_bound(self):
        meta, _ = self.metadata()
        for mutation in (
            lambda value: value.pop("coverage_inventory"),
            lambda value: value.update(coverage_inventory={}),
            lambda value: value["coverage_inventory"].update({
                "internal/control/missing.go": {"package": "example/internal/control",
                                                "instrumented": True}}),
            lambda value: value["coverage_inventory"]["internal/control/a.go"].update(
                instrumented="true"),
            lambda value: value["coverage_inventory"]["internal/control/a.go"].update(
                package="example/internal/gateway"),
        ):
            changed = copy.deepcopy(meta)
            mutation(changed)
            with self.subTest(mutation=mutation), self.assertRaises(coverage.CoverageError):
                coverage.validate_identity(changed)

    def test_rehashed_whole_file_loss_is_rejected_when_loading_a_bundle(self):
        directory = self.bundle()
        path = directory / "0.cover"
        meta_path = Path(str(path) + ".meta.json")
        meta = coverage.read_json(meta_path)
        dropped = "example/internal/gateway/a.go"
        blocks = coverage.parse_profile(path.read_text())
        path.write_text(coverage.profile_text({
            key: block for key, block in blocks.items() if block.file != dropped}))
        del meta["files"][dropped]
        meta["cover_sha256"] = coverage.file_hash(path)
        coverage.write_json(meta_path, meta)
        with self.assertRaises(coverage.CoverageError):
            coverage.load_bundle(directory, self.root)
        meta["coverage_inventory"]["internal/gateway/a.go"]["instrumented"] = False
        coverage.write_json(meta_path, meta)
        with self.assertRaises(coverage.CoverageError):
            coverage.load_bundle(directory, self.root)

    def test_header_only_profiles_cannot_produce_a_passing_zero_statement_merge(self):
        directories = [self.bundle(), self.bundle("database", "database")]
        for directory in directories:
            report_path = directory / "report.json"
            report = coverage.read_json(report_path)
            for item in report["coverage_inventory"].values():
                item["instrumented"] = False
            coverage.write_json(report_path, report)
            for artifact in report["coverage"]:
                path = directory / artifact["file"]
                path.write_text("mode: atomic\n")
                meta_path = directory / artifact["metadata"]
                meta = coverage.read_json(meta_path)
                meta.update(files={}, cover_sha256=coverage.file_hash(path),
                            coverage_inventory=report["coverage_inventory"])
                coverage.write_json(meta_path, meta)
        with self.assertRaises(coverage.CoverageError) as caught:
            coverage.merge_directories(directories, self.root / "empty", self.root)
        self.assertEqual(str(caught.exception), "merged coverage has no statements")

    def test_source_checksums_and_scope_are_enforced(self):
        meta, blocks = self.metadata()
        mutations = [
            lambda m: m["scope"].update(packages=["example/internal/httpapi"]),
            lambda m: m["scope"].update(profile="unknown"),
            lambda m: m["scope"].update(packages=["example/var/frozen"]),
            lambda m: m["files"]["example/internal/control/a.go"].update(sha256="d" * 64),
            lambda m: m["files"]["example/internal/control/a.go"].update(path="internal/httpapi/a.go"),
            lambda m: m["files"].clear(),
        ]
        for mutate in mutations:
            changed = copy.deepcopy(meta)
            mutate(changed)
            with self.subTest(mutate=mutate), self.assertRaises(coverage.CoverageError):
                coverage.validate_scope(changed, blocks)

    def test_database_scope_requires_exact_test_database_and_one_package(self):
        meta, blocks = self.metadata("database")
        for database in ("wsl", "", None):
            changed = copy.deepcopy(meta)
            changed["scope"]["database"] = database
            with self.subTest(database=database), self.assertRaises(coverage.CoverageError):
                coverage.validate_scope(changed, blocks)

    def test_current_source_change_rejected(self):
        directory = self.bundle()
        (self.root / "internal/control/a.go").write_text("package changed\n")
        with self.assertRaises(coverage.CoverageError):
            coverage.load_bundle(directory, self.root)

    def test_cover_tampering_rejected(self):
        directory = self.bundle()
        path = directory / "0.cover"
        path.write_text(path.read_text().replace("2 1", "2 99"))
        with self.assertRaises(coverage.CoverageError):
            coverage.load_bundle(directory, self.root)

    def test_report_failure_or_provenance_mismatch_rejected(self):
        directory = self.bundle()
        path = directory / "report.json"
        baseline = coverage.read_json(path)
        for key, value in (("status", "failed"), ("sha", "b" * 40),
                           ("source_digest", "c" * 64), ("schema", True)):
            report = copy.deepcopy(baseline)
            report[key] = value
            coverage.write_json(path, report)
            with self.subTest(key=key), self.assertRaises(coverage.CoverageError):
                coverage.load_bundle(directory, self.root)

    def test_database_bundle_cannot_omit_or_duplicate_a_package(self):
        directory = self.bundle("database", "database")
        path = directory / "report.json"
        baseline = coverage.read_json(path)
        for artifacts in (baseline["coverage"][:2],
                          baseline["coverage"][:2] + baseline["coverage"][:1]):
            report = dict(baseline, coverage=artifacts)
            coverage.write_json(path, report)
            with self.subTest(artifacts=artifacts), self.assertRaises(coverage.CoverageError):
                coverage.load_bundle(directory, self.root)

    def test_traversal_and_symlinks_cannot_escape_bundle_or_source(self):
        directory = self.bundle()
        report_path = directory / "report.json"
        report = coverage.read_json(report_path)
        report["coverage"][0]["file"] = "../outside.cover"
        coverage.write_json(report_path, report)
        with self.assertRaises(coverage.CoverageError):
            coverage.load_bundle(directory, self.root)
        outside = self.root.parent / "outside"
        (directory / "escape").symlink_to(outside)
        with self.assertRaises(coverage.CoverageError):
            coverage.inside(directory, "escape/a.go")

    def test_duplicate_inputs_output_reuse_and_empty_inputs_rejected(self):
        directory = self.bundle()
        database = self.bundle("database", "database")
        with self.assertRaises(coverage.CoverageError):
            coverage.merge_directories([directory, directory], self.root / "out", self.root)
        with self.assertRaises(coverage.CoverageError):
            coverage.merge_directories([directory], directory, self.root)
        with self.assertRaises(coverage.CoverageError):
            coverage.merge_directories([], self.root / "empty", self.root)
        coverage.merge_directories([directory, database], self.root / "out", self.root)
        with self.assertRaises(coverage.CoverageError):
            coverage.merge_directories([directory, database], self.root / "out", self.root)

    def test_unit_only_or_database_only_cannot_satisfy_merge(self):
        for profile in ("unit", "database"):
            directory = self.bundle(profile, profile)
            with self.subTest(profile=profile), self.assertRaises(coverage.CoverageError):
                coverage.merge_directories([directory], self.root / ("out-" + profile), self.root)

    def test_duplicate_json_keys_and_invalid_metadata_fail_closed(self):
        path = self.root / "bad.json"
        path.write_text('{"schema":1,"schema":1}')
        with self.assertRaises(coverage.CoverageError):
            coverage.read_json(path)
        meta, _ = self.metadata()
        for key, value in (("sha", None), ("toolchain", {}), ("schema", True),
                           ("source_files", {}), ("module", "../escape")):
            changed = copy.deepcopy(meta)
            changed[key] = value
            with self.subTest(key=key), self.assertRaises(coverage.CoverageError):
                coverage.validate_identity(changed)
        meta["source_files"]["var/frozen/a.go"] = "d" * 64
        meta["source_digest"] = coverage.source_digest(meta["source_files"])
        with self.assertRaises(coverage.CoverageError):
            coverage.validate_identity(meta)

    def test_local_dependency_changes_cannot_share_coverage_provenance(self):
        meta, _ = self.metadata()
        meta["source_files"]["third_party/metrics/a.go"] = "d" * 64
        meta["source_digest"] = coverage.source_digest(meta["source_files"])
        coverage.validate_identity(meta)
        changed = copy.deepcopy(meta)
        changed["source_files"]["third_party/metrics/a.go"] = "e" * 64
        changed["source_digest"] = coverage.source_digest(changed["source_files"])
        with self.assertRaises(coverage.CoverageError):
            coverage.compatible(meta, changed)

    def test_changed_file_discovery_is_nul_delimited_and_no_raw_git_output(self):
        responses = [subprocess.CompletedProcess([], 0, SHA + "\n"),
                     subprocess.CompletedProcess(
                         [], 0, b"internal/control/a.go\0docs/a.md\0var/frozen/a.go\0vendor/x.go\0")]
        with patch.object(coverage.subprocess, "run", side_effect=responses) as run:
            self.assertEqual(coverage.changed_files(self.root, "main"), ["internal/control/a.go"])
        self.assertIn("--end-of-options", run.call_args_list[0].args[0])
        with patch.object(coverage.subprocess, "run", side_effect=subprocess.TimeoutExpired(
                ["git"], 1, output="SECRET VALUE")):
            with self.assertRaises(coverage.CoverageError) as caught:
                coverage.changed_files(self.root, "main")
        self.assertNotIn("SECRET", str(caught.exception))

    def test_cli_success_and_failure_are_redacted(self):
        directory = self.bundle()
        database = self.bundle("database", "database")
        output = io.StringIO()
        with redirect_stdout(output), redirect_stderr(output):
            result = coverage.main(["--input", str(directory), "--input", str(database),
                                    "--output", str(self.root / "out"),
                                    "--root", str(self.root)])
        self.assertEqual(result, 0)
        with patch.object(coverage, "merge_directories",
                          side_effect=coverage.CoverageError("SECRET VALUE")):
            with redirect_stdout(output), redirect_stderr(output):
                result = coverage.main(["--input", str(directory), "--output", str(self.root / "bad")])
        self.assertEqual(result, 1)
        self.assertNotIn("SECRET", output.getvalue())


if __name__ == "__main__":
    unittest.main()
