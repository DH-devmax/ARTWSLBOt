import contextlib
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import analyze
import baseline


class BaselineTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "repo"
        self.root.mkdir()
        self.git("init", "-q", "-b", "main")
        self.git("-c", "user.name=Test", "-c", "user.email=test@localhost",
                 "commit", "--allow-empty", "-qm", "baseline")
        (self.root / ".codeboarding").mkdir()
        (self.root / ".codeboarding/.codeboardingignore").write_text("fixtures\n")
        self.here = Path(self.temp.name) / "tooling"
        self.here.mkdir()
        (self.here / "toolchain.json").write_text('{"pinned":true}')
        for name in baseline.RUNNER_FILES:
            (self.here / name).write_text(name)
        self.cache = Path(self.temp.name) / "private"
        self.archive = Path(self.temp.name) / "durable"

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.root, text=True).strip()

    def context(self):
        with patch.dict(os.environ, {"GITHUB_ACTIONS": "false"}):
            return baseline.context(self.root, self.here, self.cache, self.archive)

    def seed(self, ctx):
        baseline.prepare(ctx)
        (ctx["cache"] / "baseline").mkdir()
        (ctx["cache"] / "baseline/analysis.json").write_text('{"components":[{"id":"test"}]}')
        (ctx["cache"] / "baseline/fingerprint.json").write_text('{"test.go":"synthetic"}')
        (ctx["cache"] / "baseline/static_analysis.pkl").write_bytes(b"synthetic-not-loaded")
        (ctx["cache"] / "baseline/static_analysis.sha").write_text("synthetic")
        (ctx["cache"] / "state.json").write_text(json.dumps({
            "schema_version": 2,
            **{key: ctx[key] for key in ("repository_id", "trusted_ref", "settings_hash", "commit")},
            "artifacts": baseline.artifact_hashes(ctx["cache"] / "baseline"),
        }))

    def test_namespace_separates_repositories_refs_and_runner_versions(self):
        first = self.context()
        self.git("switch", "-q", "-c", "other")
        other_ref = self.context()
        self.assertNotEqual(first["cache"], other_ref["cache"])
        (self.here / "analyze.py").write_text("changed")
        self.assertNotEqual(other_ref["cache"], self.context()["cache"])
        original = self.root
        self.root = Path(self.temp.name) / "other"
        subprocess.run(["git", "clone", "-q", str(original), str(self.root)], check=True)
        (self.root / ".codeboarding").mkdir()
        (self.root / ".codeboarding/.codeboardingignore").write_text("fixtures\n")
        self.assertNotEqual(other_ref["repository_id"], self.context()["repository_id"])

    def test_worktrees_share_repository_but_not_branch_namespace(self):
        first = self.context()
        other = Path(self.temp.name) / "worktree"
        self.git("worktree", "add", "-q", "-b", "isolated", str(other))
        (other / ".codeboarding").mkdir()
        (other / ".codeboarding/.codeboardingignore").write_text("fixtures\n")
        self.root = other
        second = self.context()
        self.assertEqual(first["repository_id"], second["repository_id"])
        self.assertNotEqual(first["cache"], second["cache"])

    def test_diagram_and_product_changes_do_not_invalidate_model_runner(self):
        first = self.context()
        for name in ("architecture.py", "flows.py", "frontend.mjs", "render.mjs"):
            (self.here / name).write_text("new diagram extraction rules")
        (self.root / "product.go").write_text("package product")
        self.assertEqual(first["cache"], self.context()["cache"])
        (self.here / "common.py").write_text("new source selection rules")
        self.assertNotEqual(first["cache"], self.context()["cache"])

    def test_ci_ref_and_repository_are_part_of_namespace(self):
        env = {"GITHUB_ACTIONS": "true", "GITHUB_REPOSITORY": "owner/repo",
               "GITHUB_REF": "refs/heads/main", "GITHUB_EVENT_NAME": "push"}
        with patch.dict(os.environ, env):
            first = baseline.context(self.root, self.here, self.cache)
            with patch.dict(os.environ, {"GITHUB_REPOSITORY": "other/repo"}):
                other = baseline.context(self.root, self.here, self.cache)
            self.assertNotEqual(first["cache"], other["cache"])
            for event in ("pull_request", "pull_request_target"):
                with patch.dict(os.environ, {"GITHUB_EVENT_NAME": event}):
                    with self.assertRaisesRegex(baseline.BaselineError, "untrusted_ci_origin"):
                        baseline.context(self.root, self.here, self.cache)

    def test_valid_baseline_and_ancestor_are_accepted(self):
        ctx = self.context()
        self.seed(ctx)
        self.git("-c", "user.name=Test", "-c", "user.email=test@localhost",
                 "commit", "--allow-empty", "-qm", "next")
        newer = self.context()
        baseline.validate(newer)
        with self.assertRaisesRegex(baseline.BaselineError, "baseline_already_exists"):
            baseline.validate(newer, initialize=True)

    def test_non_ancestor_and_identity_mismatch_are_rejected(self):
        ctx = self.context()
        self.seed(ctx)
        state_path = ctx["cache"] / "state.json"
        state = json.loads(state_path.read_text())
        state["trusted_ref"] = "refs/heads/not-main"
        state_path.write_text(json.dumps(state))
        with self.assertRaisesRegex(baseline.BaselineError, "baseline_identity_mismatch"):
            baseline.validate(ctx)
        state["trusted_ref"] = ctx["trusted_ref"]
        state["commit"] = "0" * 40
        state_path.write_text(json.dumps(state))
        with self.assertRaisesRegex(baseline.BaselineError, "baseline_not_ancestor"):
            baseline.validate(ctx)

    def test_symlink_and_writable_baselines_fail_closed(self):
        ctx = self.context()
        self.seed(ctx)
        data = ctx["cache"] / "baseline/analysis.json"
        data.chmod(0o666)
        with self.assertRaisesRegex(baseline.BaselineError, "untrusted_baseline"):
            baseline.validate(ctx)
        data.unlink()
        data.symlink_to(ctx["cache"] / "missing")
        # A dangling output is missing, and never permits automatic initialization.
        with self.assertRaises(baseline.BaselineError):
            baseline.validate(ctx)
        with self.assertRaisesRegex(baseline.BaselineError, "baseline_already_exists"):
            baseline.validate(ctx, initialize=True)

    def test_lock_rejects_concurrent_analyzer(self):
        ctx = self.context()
        baseline.prepare(ctx)
        with baseline.locked(ctx):
            with self.assertRaisesRegex(baseline.BaselineError, "analysis_in_progress"):
                with baseline.locked(ctx):
                    self.fail("second analyzer acquired the lock")

    def test_checkpoint_recovers_missing_cache_without_model_or_credentials(self):
        ctx = self.context()
        self.seed(ctx)
        baseline.checkpoint(ctx)
        state = baseline.validate(ctx)
        shutil.rmtree(ctx["cache"])
        baseline.prepare(ctx)
        with patch.object(baseline, "context", return_value=ctx), \
             patch.object(analyze, "source_manifest", return_value={"internal/a.go": "hash"}), \
             patch.object(analyze, "read_key") as key, \
             patch.object(analyze, "execute") as execute, \
             contextlib.redirect_stdout(io.StringIO()):
            analyze.analyze(preflight_only=True)
        key.assert_not_called()
        execute.assert_not_called()
        self.assertEqual(baseline.validate(ctx), state)
        report = json.loads((ctx["cache"] / "last-run.json").read_text())
        self.assertTrue(report["baseline_restored"])
        self.assertEqual(report["status"], "ready")
        with self.assertRaisesRegex(baseline.BaselineError, "baseline_already_exists"):
            baseline.validate(ctx, initialize=True)

    def test_archive_cannot_seed_different_ref_or_settings(self):
        first = self.context()
        self.seed(first)
        baseline.checkpoint(first)
        self.git("switch", "-q", "-c", "unrelated")
        other = self.context()
        baseline.prepare(other)
        self.assertFalse(baseline.restore(other))
        with self.assertRaisesRegex(baseline.BaselineError, "compatible_baseline_missing"):
            baseline.validate(other)
        self.git("switch", "-q", "main")
        (self.here / "analyze.py").write_text("different runner")
        changed = self.context()
        baseline.prepare(changed)
        self.assertFalse(baseline.restore(changed))

    def test_tampered_checkpoint_is_rejected_before_model(self):
        ctx = self.context()
        self.seed(ctx)
        baseline.checkpoint(ctx)
        pointer = json.loads((ctx["archive"] / "latest.json").read_text())["checkpoint"]
        (ctx["archive"] / pointer / "baseline/static_analysis.pkl").write_bytes(b"tampered")
        shutil.rmtree(ctx["cache"])
        baseline.prepare(ctx)
        with patch.object(baseline, "context", return_value=ctx), \
             patch.object(analyze, "read_key") as key, \
             patch.object(analyze, "execute") as execute, \
             contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaisesRegex(baseline.BaselineError, "baseline_integrity_mismatch"):
                analyze.analyze()
        key.assert_not_called()
        execute.assert_not_called()
        self.assertFalse((ctx["cache"] / "baseline").exists())

    def test_partial_cache_is_not_overwritten_by_backup(self):
        ctx = self.context()
        self.seed(ctx)
        baseline.checkpoint(ctx)
        (ctx["cache"] / "state.json").unlink()
        self.assertFalse(baseline.restore(ctx))
        with self.assertRaisesRegex(baseline.BaselineError, "compatible_baseline_missing"):
            baseline.validate(ctx)

    def test_incomplete_baseline_rejected_before_unpickling(self):
        ctx = self.context()
        self.seed(ctx)
        (ctx["cache"] / "baseline/fingerprint.json").unlink()
        with self.assertRaisesRegex(baseline.BaselineError, "baseline_incomplete"):
            baseline.validate(ctx)

    def test_archive_pointer_cannot_escape_private_namespace(self):
        ctx = self.context()
        baseline.prepare(ctx)
        archive = baseline.prepare_archive(ctx)
        baseline.atomic_json(archive / "latest.json", {"checkpoint": "../../untrusted"})
        with self.assertRaisesRegex(baseline.BaselineError, "invalid_archive_pointer"):
            baseline.restore(ctx)

    def test_checkpoint_keeps_previous_verified_versions(self):
        ctx = self.context()
        self.seed(ctx)
        baseline.checkpoint(ctx)
        first = (ctx["archive"] / "latest.json").read_text()
        state = json.loads((ctx["cache"] / "state.json").read_text())
        state["source_hash"] = "new-source"
        baseline.atomic_json(ctx["cache"] / "state.json", state)
        baseline.checkpoint(ctx)
        self.assertNotEqual(first, (ctx["archive"] / "latest.json").read_text())
        self.assertTrue((ctx["archive"] / json.loads(first)["checkpoint"]).is_dir())

    def test_missing_baseline_replaces_stale_report_without_secrets_or_paid_calls(self):
        ctx = self.context()
        baseline.prepare(ctx)
        report = ctx["cache"] / "last-run.json"
        report.write_text('{"status":"passed","commit":"stale"}')
        with patch.object(baseline, "context", return_value=ctx), \
             patch.object(analyze, "read_key") as key, \
             patch.object(analyze, "execute") as execute, \
             contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaisesRegex(baseline.BaselineError, "compatible_baseline_missing"):
                analyze.analyze()
        key.assert_not_called()
        execute.assert_not_called()
        current = json.loads(report.read_text())
        self.assertEqual(current["status"], "blocked")
        self.assertEqual(current["reason"], "compatible_baseline_missing")
        self.assertEqual(current["usage"]["requests"], 0)
        self.assertEqual(current["commit"], ctx["commit"])
        self.assertEqual(report.stat().st_mode & 0o777, 0o600)

    def test_preflight_never_reads_credentials_or_runs_model(self):
        ctx = self.context()
        self.seed(ctx)
        with patch.object(baseline, "context", return_value=ctx), \
             patch.object(analyze, "source_manifest", return_value={"internal/a.go": "hash"}), \
             patch.object(analyze, "execute") as execute, \
             contextlib.redirect_stdout(io.StringIO()):
            analyze.analyze(preflight_only=True)
        execute.assert_not_called()
        self.assertEqual(json.loads((ctx["cache"] / "last-run.json").read_text())["status"], "ready")

    def test_source_rejection_is_reported_not_inherited_from_old_success(self):
        ctx = self.context()
        self.seed(ctx)
        with patch.object(baseline, "context", return_value=ctx), \
             patch.object(analyze, "source_manifest", side_effect=ValueError("synthetic")), \
             contextlib.redirect_stdout(io.StringIO()):
            with self.assertRaises(ValueError):
                analyze.analyze()
        current = json.loads((ctx["cache"] / "last-run.json").read_text())
        self.assertEqual(current["reason"], "source_preflight_failed")
        self.assertEqual(current["usage"]["requests"], 0)


if __name__ == "__main__":
    unittest.main()
