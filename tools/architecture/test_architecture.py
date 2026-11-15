from concurrent.futures import ThreadPoolExecutor
import json
from pathlib import Path
import subprocess
import tempfile
import threading
from types import SimpleNamespace
import unittest
import urllib.error
import urllib.request
from unittest.mock import patch

import analyze
import architecture as arch
import provider
import codeboarding_entry


def graph():
    return {"nodes": [{"id": "internal/a", "view": "backend", "kind": "go-package"},
                      {"id": "internal/b", "view": "backend", "kind": "go-package"}],
            "edges": [{"from": "internal/a", "to": "internal/b", "view": "backend", "kind": "imports",
                       "evidence": [{"path": "internal/a/a.go", "line": 1, "symbol": "b"}]}]}


def chat_request(url, token, content="synthetic"):
    return urllib.request.Request(
        url + "/chat/completions",
        data=json.dumps({"model": analyze.CONFIG["model"],
                         "messages": [{"role": "user", "content": content}]}).encode(),
        headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"},
    )


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

    def test_existing_node_state_changes_are_reported(self):
        before, after = graph(), graph()
        before["nodes"][0]["generation_gate"] = "false"
        after["nodes"][0]["generation_gate"] = "true"
        result = arch.compare_graphs(before, after)
        self.assertEqual(result["backend"]["added_nodes"], [])
        self.assertEqual(result["backend"]["changed_nodes"][0]["id"], "internal/a")
        self.assertFalse(any(result["system"].values()))

    def test_edge_attributes_but_not_evidence_are_reported(self):
        before, after = graph(), graph()
        after["edges"][0]["template_used"] = True
        result = arch.compare_graphs(before, after)["backend"]
        self.assertEqual(len(result["changed_edges"]), 1)
        del after["edges"][0]["template_used"]
        after["edges"][0]["evidence"][0]["line"] = 100
        self.assertFalse(any(arch.compare_graphs(before, after)["backend"].values()))

    def test_graph_labels_cannot_inject_mermaid(self):
        value = graph()
        value["nodes"][0]["label"] = 'bad"] --> attack <script>'
        output = arch.mermaid(value, "backend")
        self.assertNotIn("<script>", output)
        self.assertEqual(output.count(" -->|"), 1)

    def test_external_import_changes_are_reported_without_redrawing_compact_view(self):
        before, after = graph(), graph()
        after["nodes"].append({"id": "external-library", "view": "backend", "kind": "external"})
        after["edges"].append({"from": "internal/a", "to": "external-library",
                               "view": "backend", "kind": "imports", "evidence": []})
        self.assertEqual(arch.mermaid(before, "backend"), arch.mermaid(after, "backend"))
        report = arch.compare_graphs(before, after)["backend"]
        self.assertEqual(report["added_nodes"], ["external-library"])
        self.assertEqual(len(report["added_edges"]), 1)

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
            result = analyze.validate_result(json.dumps({
                "mode": "incremental", "requiresFullAnalysis": False, "analysis_path": str(path)
            }), path, True)
            self.assertEqual(len(result["components"]), 1)

    def test_incremental_adapter_forbids_upstream_full_fallback(self):
        class Generator:
            def generate_analysis(self):
                self.fail("full analysis must not run")
        codeboarding_entry.forbid_full_fallback(Generator)
        with self.assertRaisesRegex(ValueError, "explicit initialization required"):
            Generator().generate_analysis()

    def test_incremental_cold_bootstrap_progress_before_wire_result(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "analysis.json"
            path.write_text('{"components":[{"id":"1"}]}')
            wire = {"mode": "incremental", "requiresFullAnalysis": False, "analysis_path": str(path)}
            stdout = "Step: npm check started\nStep: npm check finished: success\n" + json.dumps(wire, indent=2) + "\n"
            self.assertEqual(len(analyze.validate_result(stdout, path, True)["components"]), 1)

    def test_incremental_rejects_missing_trailing_or_conflicting_results(self):
        success = json.dumps({"mode": "incremental", "requiresFullAnalysis": False})
        for stdout in ("", "Step: npm check started\n", success + "\ntruncated output",
                       '{"requiresFullAnalysis":true}\n' + success):
            with self.subTest(stdout=stdout), self.assertRaises(ValueError):
                analyze.incremental_result(stdout)

    def test_incremental_rejects_wrong_mode_or_path(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "analysis.json"
            path.write_text('{"components":[{"id":"1"}]}')
            for mode, location in (("full", str(path)), ("incremental", "/other/analysis.json")):
                wire = {"mode": mode, "requiresFullAnalysis": False, "analysis_path": location}
                with self.subTest(mode=mode, location=location), self.assertRaises(ValueError):
                    analyze.validate_result(json.dumps(wire), path, True)

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
            with self.assertRaises(urllib.error.HTTPError) as caught:
                urllib.request.urlopen(req, timeout=3)
            with caught.exception as response:
                self.assertEqual(response.code, 429)
                self.assertIn(b"Architecture analysis budget exhausted", response.read())
            self.assertEqual(upstream.call_count, 1)
            self.assertEqual(ledger["usage_unknown"], 1)
            self.assertEqual(ledger["reserved_tokens"], ledger["estimated_tokens"])
            self.assertEqual(ledger["local_rejections"], {"request_limit": 1})
            self.assertEqual(ledger["provider_failures"], {})

    def test_known_usage_settlement_allows_third_model_turn(self):
        replies = [{"choices": [], "usage": {"total_tokens": usage}}
                   for usage in (30538, 31651, 11)]
        with patch.object(analyze, "request", side_effect=replies) as upstream, (
            analyze.relay()
        ) as (url, token, ledger):
            req = chat_request(url, token, "x" * 134500)
            for _ in range(2):
                with urllib.request.urlopen(req, timeout=3) as response:
                    self.assertEqual(response.status, 200)
            self.assertEqual(ledger["reserved_tokens"], 62189)
            self.assertGreater(ledger["estimated_tokens"], 277000)
            with urllib.request.urlopen(req, timeout=3) as response:
                self.assertEqual(response.status, 200)
            self.assertEqual(upstream.call_count, 3)
            self.assertEqual(ledger["requests"], 3)
            self.assertEqual(ledger["reserved_tokens"], 62200)
            self.assertEqual(ledger["reported_tokens"], 62200)
            self.assertGreater(ledger["estimated_tokens"], analyze.MAX_RESERVED_TOKENS)
            self.assertEqual(ledger["usage_unknown"], 0)
            self.assertEqual(ledger["local_rejections"], {})

    def test_unknown_usage_keeps_token_reservation_and_rejects_locally(self):
        with patch.object(analyze, "request", return_value={
            "choices": [], "usage": None,
        }) as upstream, analyze.relay() as (url, token, ledger):
            req = chat_request(url, token, "x" * 134500)
            for _ in range(2):
                with urllib.request.urlopen(req, timeout=3) as response:
                    self.assertEqual(response.status, 200)
            with self.assertRaises(urllib.error.HTTPError) as caught:
                urllib.request.urlopen(req, timeout=3)
            with caught.exception as response:
                self.assertEqual(response.code, 429)
                self.assertIn(b"Architecture analysis budget exhausted", response.read())
            self.assertEqual(upstream.call_count, 2)
            self.assertEqual(ledger["requests"], 2)
            self.assertEqual(ledger["usage_unknown"], 2)
            self.assertEqual(ledger["reported_tokens"], 0)
            self.assertEqual(ledger["reserved_tokens"], ledger["estimated_tokens"])
            self.assertEqual(ledger["local_rejections"], {"reserved_token_limit": 1})
            self.assertEqual(ledger["provider_failures"], {})

    def test_invalid_usage_does_not_release_reservation(self):
        for usage in ({}, {"total_tokens": None}, {"total_tokens": True},
                      {"total_tokens": -1}, {"total_tokens": 1.5}, {"total_tokens": "11"}):
            with self.subTest(usage=usage), patch.object(
                analyze, "request", return_value={"choices": [], "usage": usage}
            ), analyze.relay() as (url, token, ledger):
                with urllib.request.urlopen(chat_request(url, token), timeout=3) as response:
                    self.assertEqual(response.status, 200)
                self.assertEqual(ledger["usage_unknown"], 1)
                self.assertEqual(ledger["reported_tokens"], 0)
                self.assertGreater(ledger["reserved_tokens"], 0)
                self.assertEqual(ledger["reserved_tokens"], ledger["estimated_tokens"])

    def test_inflight_reservation_cannot_be_reused_concurrently(self):
        entered = threading.Event()
        finish = threading.Event()

        def completion(*_):
            entered.set()
            if not finish.wait(5):
                raise ValueError("synthetic_wait_timeout")
            return {"choices": [], "usage": {"total_tokens": 7}}

        def submit(url, token):
            with urllib.request.urlopen(chat_request(url, token, "x" * 200000),
                                        timeout=5) as response:
                return response.status

        with patch.object(analyze, "request", side_effect=completion) as upstream, (
            analyze.relay()
        ) as (url, token, ledger), ThreadPoolExecutor(max_workers=1) as pool:
            pending = pool.submit(submit, url, token)
            try:
                self.assertTrue(entered.wait(3))
                self.assertEqual(ledger["usage_unknown"], 1)
                self.assertGreater(ledger["reserved_tokens"], 200000)
                with self.assertRaises(urllib.error.HTTPError) as caught:
                    urllib.request.urlopen(chat_request(url, token, "x" * 200000), timeout=3)
                with caught.exception as response:
                    self.assertEqual(response.code, 429)
                self.assertEqual(upstream.call_count, 1)
                self.assertEqual(ledger["local_rejections"], {"reserved_token_limit": 1})
            finally:
                finish.set()
            self.assertEqual(pending.result(timeout=3), 200)
            self.assertEqual(ledger["reserved_tokens"], 7)
            self.assertEqual(ledger["reported_tokens"], 7)
            self.assertEqual(ledger["usage_unknown"], 0)

    def test_reported_usage_above_estimate_is_fully_charged(self):
        with patch.object(analyze, "request", return_value={
            "choices": [], "usage": {"total_tokens": analyze.MAX_RESERVED_TOKENS + 1},
        }) as upstream, analyze.relay() as (url, token, ledger):
            req = chat_request(url, token)
            with urllib.request.urlopen(req, timeout=3) as response:
                self.assertEqual(response.status, 200)
            self.assertEqual(ledger["reserved_tokens"], analyze.MAX_RESERVED_TOKENS + 1)
            self.assertEqual(ledger["reported_tokens"], analyze.MAX_RESERVED_TOKENS + 1)
            with self.assertRaises(urllib.error.HTTPError) as caught:
                urllib.request.urlopen(req, timeout=3)
            with caught.exception as response:
                self.assertEqual(response.code, 429)
            self.assertEqual(upstream.call_count, 1)
            self.assertEqual(ledger["local_rejections"], {"reserved_token_limit": 1})

    def test_gpt6_tools_use_bounded_chat_relay(self):
        from langchain_openai import ChatOpenAI

        config = SimpleNamespace(extra_args={})
        codeboarding_entry.configure_chat_transport(config)
        completion = {
            "id": "synthetic-chat", "object": "chat.completion",
            "model": analyze.CONFIG["model"],
            "choices": [{"index": 0, "message": {"role": "assistant", "content": "ok"},
                         "finish_reason": "stop"}],
            "usage": {"prompt_tokens": 10, "completion_tokens": 1, "total_tokens": 11},
        }
        with patch.object(analyze, "request", return_value=completion) as upstream, analyze.relay() as (
            url, token, ledger
        ):
            model = ChatOpenAI(model=analyze.CONFIG["model"], base_url=url, api_key=token,
                               **config.extra_args)
            tool = {"type": "function", "function": {
                "name": "inspect_source", "description": "Synthetic tool",
                "parameters": {"type": "object", "properties": {}},
            }}
            result = model.bind_tools([tool]).invoke("synthetic source only")
            self.assertEqual(result.content, "ok")
            self.assertEqual(upstream.call_count, 1)
            route, payload = upstream.call_args.args
            self.assertEqual(route, "/chat/completions")
            self.assertEqual(payload["tools"], [tool])
            self.assertEqual(payload["max_tokens"], 4096)
            self.assertEqual(model.max_retries, 0)
            self.assertEqual(ledger["requests"], 1)
            self.assertEqual(ledger["usage_unknown"], 0)
            self.assertEqual(ledger["reported_tokens"], 11)

    def test_relay_rejects_responses_without_provider_submission(self):
        with patch.object(analyze, "request") as upstream, analyze.relay() as (url, token, ledger):
            req = urllib.request.Request(url + "/responses",
                data=json.dumps({"model": analyze.CONFIG["model"], "input": "synthetic"}).encode(),
                headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
            with self.assertRaises(urllib.error.HTTPError) as error:
                urllib.request.urlopen(req, timeout=3)
            self.assertEqual(error.exception.code, 403)
            upstream.assert_not_called()
            self.assertEqual(ledger["requests"], 0)
            self.assertEqual(ledger["usage_unknown"], 0)

    def test_scope_model_failure_is_not_automatically_retried(self):
        from agents import scope_analysis_agent, tree_planner_agent
        from agents.scope_analysis_agent import TransientErrorRetry

        with patch.object(scope_analysis_agent, "LLM_MAX_ATTEMPTS", 5), patch.object(
            tree_planner_agent, "LLM_MAX_ATTEMPTS", 5
        ):
            codeboarding_entry.configure_single_attempt(scope_analysis_agent, tree_planner_agent)
            self.assertEqual(tree_planner_agent.LLM_MAX_ATTEMPTS, 1)
            with patch.object(scope_analysis_agent, "classify_llm_error") as classify:
                from agents.retry import RetryAction, RetryDecision
                classify.return_value = RetryDecision(RetryAction.RETRY, 0)
                with patch.object(scope_analysis_agent, "logger"):
                    handler = unittest.mock.Mock(side_effect=ValueError("synthetic failure"))
                    with self.assertRaisesRegex(ValueError, "synthetic failure"):
                        TransientErrorRetry("root").wrap_model_call(None, handler)
                    self.assertEqual(handler.call_count, 1)

    def test_provider_timeout_is_bounded_and_body_free(self):
        with patch.object(provider, "read_key", return_value="synthetic"), patch.object(
            provider.urllib.request, "build_opener"
        ) as build:
            build.return_value.open.side_effect = TimeoutError("private transport contents")
            with self.assertRaises(provider.ProviderError) as caught:
                provider.request("/chat/completions", {"synthetic": True})
            self.assertEqual(caught.exception.reason, "timeout")
            self.assertNotIn("private", str(caught.exception))
            self.assertEqual(build.return_value.open.call_args.kwargs["timeout"], 120)

    def test_relay_retains_unknown_usage_with_safe_failure_class(self):
        for failure, category in ((provider.ProviderError("timeout"), "timeout"),
                                  (provider.ProviderError("http_error", 429), "http_429"),
                                  (provider.ProviderError("http_error", 502), "http_502"),
                                  (provider.ProviderError("transport"), "transport")):
            with self.subTest(category=category), patch.object(
                analyze, "request", side_effect=failure
            ) as upstream, analyze.relay() as (url, token, ledger):
                req = urllib.request.Request(url + "/chat/completions",
                    data=json.dumps({"model": analyze.CONFIG["model"], "messages": []}).encode(),
                    headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"})
                with self.assertRaises(urllib.error.HTTPError) as caught:
                    urllib.request.urlopen(req, timeout=3)
                with caught.exception as response:
                    self.assertEqual(response.code, 502)
                    self.assertIn(b"Architecture provider unavailable", response.read())
                self.assertEqual(upstream.call_count, 1)
                self.assertEqual(ledger["usage_unknown"], 1)
                self.assertEqual(ledger["reported_tokens"], 0)
                self.assertGreater(ledger["reserved_tokens"], 0)
                self.assertEqual(ledger["reserved_tokens"], ledger["estimated_tokens"])
                self.assertEqual(ledger["provider_failures"], {category: 1})
                self.assertEqual(ledger["local_rejections"], {})

    def test_provider_502_keeps_budget_until_local_429(self):
        with patch.object(
            analyze, "request", side_effect=provider.ProviderError("http_error", 502)
        ) as upstream, analyze.relay() as (url, token, ledger):
            req = chat_request(url, token, "x" * 134500)
            for status in (502, 502, 429):
                with self.assertRaises(urllib.error.HTTPError) as caught:
                    urllib.request.urlopen(req, timeout=3)
                with caught.exception as response:
                    self.assertEqual(response.code, status)
            self.assertEqual(upstream.call_count, 2)
            self.assertEqual(ledger["requests"], 2)
            self.assertEqual(ledger["usage_unknown"], 2)
            self.assertEqual(ledger["reported_tokens"], 0)
            self.assertEqual(ledger["reserved_tokens"], ledger["estimated_tokens"])
            self.assertEqual(ledger["provider_failures"], {"http_502": 2})
            self.assertEqual(ledger["local_rejections"], {"reserved_token_limit": 1})

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

    def test_product_shared_charts_are_scanned_without_framework_demos(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            ui = root / "web/src/wsl"
            chart = root / "web/src/components/core/charts/art-bar-chart"
            demo = root / "web/src/views/demo"
            for folder in (ui, chart, demo):
                folder.mkdir(parents=True)
            (root / "web/tsconfig.json").write_text(json.dumps({"compilerOptions": {}}))
            (ui / "App.vue").write_text(
                '<script setup lang="ts">import Chart from "../components/core/charts/art-bar-chart/index.vue"</script>'
                '<template><Chart /></template>')
            (chart / "index.vue").write_text(
                '<script setup lang="ts">import {helper} from "../useChartSurface"</script><template><div /></template>')
            (chart.parent / "useChartSurface.ts").write_text("export const helper = 1")
            (demo / "Bad.vue").write_text("not a valid demo source")
            result = json.loads(subprocess.check_output(
                ["node", str(arch.HERE / "frontend.mjs"), str(root)], text=True))
            self.assertTrue(any(e["to"].endswith("useChartSurface.ts") and not e["external"]
                                for e in result["edges"]))
            self.assertFalse(any("/views/" in n["id"] for n in result["nodes"]))


if __name__ == "__main__":
    unittest.main()
