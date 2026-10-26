import json
from pathlib import Path
import subprocess
import tempfile
import unittest
import urllib.error
import urllib.request
from unittest.mock import patch

import analyze
import architecture as arch
import provider


def graph():
    return {"nodes": [{"id": "internal/a", "view": "backend", "kind": "go-package"},
                      {"id": "internal/b", "view": "backend", "kind": "go-package"}],
            "edges": [{"from": "internal/a", "to": "internal/b", "view": "backend", "kind": "imports",
                       "evidence": [{"path": "internal/a/a.go", "line": 1, "symbol": "b"}]}]}


class ArchitectureTests(unittest.TestCase):
    def test_absent_assistant_is_not_invented(self):
        self.assertFalse(arch.assistant_present({"internal/heatbot/engine.go"}))

    def test_partial_assistant_wiring_fails_closed(self):
        with self.assertRaisesRegex(ValueError, "Partial assistant wiring"):
            arch.assistant_present({"internal/assistant/runtime.go"})

    def test_noop_preserves_mtime(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "backend.mmd"
            self.assertTrue(arch.write_changed(path, arch.mermaid(graph(), "backend")))
            before = path.stat().st_mtime_ns
            self.assertFalse(arch.write_changed(path, arch.mermaid(graph(), "backend")))
            self.assertEqual(before, path.stat().st_mtime_ns)

    def test_evidence_line_change_does_not_redraw(self):
        before = graph()
        after = graph()
        after["edges"][0]["evidence"][0]["line"] = 90
        self.assertEqual(arch.mermaid(before, "backend"), arch.mermaid(after, "backend"))
        self.assertEqual(arch.compare_graphs(before, after)["backend"]["added_edges"], [])

    def test_only_affected_view_changes(self):
        before = graph()
        after = graph()
        after["nodes"].append({"id": "internal/c", "view": "backend", "kind": "go-package"})
        for view in ("system", "frontend", "agents"):
            self.assertEqual(arch.mermaid(before, view), arch.mermaid(after, view))
        self.assertNotEqual(arch.mermaid(before, "backend"), arch.mermaid(after, "backend"))
        self.assertEqual(arch.compare_graphs(after, before)["backend"]["removed_nodes"], ["internal/c"])

    def test_graph_labels_cannot_inject_mermaid(self):
        value = graph()
        value["nodes"][0]["label"] = 'bad"] --> attack <script>'
        output = arch.mermaid(value, "backend")
        self.assertNotIn("<script>", output)
        self.assertEqual(output.count(" -->|"), 1)

    def test_paid_snapshot_is_an_allowlist(self):
        for name in ("internal/heatbot/engine.go", "web/src/wsl/App.vue", "go.mod"):
            self.assertTrue(analyze.allowed_source(name), name)
        for name in (".env", "web/.env", "var/session.json", "internal/gateway/a_test.go",
                     "internal/nativewire/testdata/secret.go", "web/src/views/demo.vue",
                     "internal/database/migrations/001.sql", "docs/users.md"):
            self.assertFalse(analyze.allowed_source(name), name)

    def test_incremental_exit_zero_error_is_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "analysis.json"
            path.write_text('{"components":[{"id":"1"}]}')
            for stdout in ('{"requiresFullAnalysis":true}', '{"error":"bad"}', '{}'):
                with self.assertRaises(ValueError):
                    analyze.validate_result(stdout, path, True)
            result = analyze.validate_result('{"requiresFullAnalysis":false}', path, True)
            self.assertEqual(len(result["components"]), 1)

    def test_failed_promotion_preserves_old_baseline(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            baseline, output, state = root / "baseline", root / "result", root / "state.json"
            baseline.mkdir()
            output.mkdir()
            (baseline / "analysis.json").write_text("old")
            (output / "analysis.json").write_text("new")
            state.write_text('{"old":true}\n')
            replace = analyze.os.replace
            def fail_state(source, destination):
                if Path(source).name.startswith("state-"):
                    raise OSError("synthetic disk error")
                return replace(source, destination)
            with patch.object(analyze.os, "replace", side_effect=fail_state):
                with self.assertRaises(OSError):
                    analyze.promote(output, baseline, state, {"new": True})
            self.assertEqual((baseline / "analysis.json").read_text(), "old")
            self.assertEqual(json.loads(state.read_text()), {"old": True})

    def test_model_request_budget_and_unknown_usage(self):
        with patch.object(analyze, "MAX_REQUESTS", 1), patch.object(
            analyze, "request", return_value={"choices": [], "usage": None}
        ) as upstream, analyze.relay() as (url, token, ledger):
            req = urllib.request.Request(url + "/chat/completions",
                data=json.dumps({"model": analyze.CONFIG["model"], "messages": []}).encode(),
                headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
            with urllib.request.urlopen(req, timeout=3) as response:
                self.assertEqual(response.status, 200)
            with self.assertRaises(urllib.error.HTTPError):
                urllib.request.urlopen(req, timeout=3)
            self.assertEqual(upstream.call_count, 1)
            self.assertEqual(ledger["usage_unknown"], 1)

    def test_private_key_file_permissions(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "key"
            path.write_text("synthetic-credential\n")
            path.chmod(0o644)
            with patch.object(provider, "KEY_FILE", path), patch.dict(provider.os.environ, {}, clear=True):
                with self.assertRaises(ValueError):
                    provider.read_key()
                path.chmod(0o600)
                self.assertEqual(provider.read_key(), "synthetic-credential")

    def test_provider_redirects_are_rejected(self):
        self.assertIsNone(provider.NoRedirect().redirect_request(None, None, 302, "", {}, "https://other.test"))

    def test_fresh_snapshot_has_all_evidence(self):
        payload = json.loads((arch.OUTPUT / "graph.json").read_text())
        ids = {(n["view"], n["id"]) for n in payload["nodes"]}
        for edge in payload["edges"]:
            self.assertIn((edge["view"], edge["from"]), ids)
            self.assertIn((edge["view"], edge["to"]), ids)
            self.assertTrue(edge["evidence"])
            for entry in edge["evidence"]:
                self.assertTrue((arch.ROOT / entry["path"]).is_file())
                self.assertGreater(entry["line"], 0)
        agent_edges = {(e["from"], e["to"]) for e in payload["edges"] if e["view"] == "agents"}
        self.assertNotIn(("api", "heatbot"), agent_edges)

    def test_vue_templates_types_aliases_and_dynamic_imports(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            ui = root / "web/src/wsl"
            ui.mkdir(parents=True)
            (root / "web/tsconfig.json").write_text(json.dumps({
                "compilerOptions": {"paths": {"@/*": ["src/*"]}}}))
            (ui / "Child.vue").write_text("<template><span /></template>")
            (ui / "types.ts").write_text("export type Kind = string")
            (ui / "App.vue").write_text(
                '<script setup lang="ts">\nimport Child from "@/wsl/Child.vue"\n'
                'import type {Kind} from "./types"\nconst lazy = () => import("./types")\n'
                '</script>\n<template><Child /><component :is="Child" /></template>')
            result = json.loads(subprocess.check_output(
                ["node", str(arch.HERE / "frontend.mjs"), str(root)], text=True))
            self.assertTrue(any(e["template_used"] and e["to"].endswith("Child.vue") for e in result["edges"]))
            self.assertTrue(any(e["kind"] == "type-import" for e in result["edges"]))
            self.assertTrue(any(e["kind"] == "dynamic-import" for e in result["edges"]))
            self.assertTrue(any(e["kind"] == "runtime-component-selection" for e in result["gaps"]))


if __name__ == "__main__":
    unittest.main()
