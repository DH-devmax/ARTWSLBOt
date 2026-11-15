import json
from pathlib import Path
import subprocess
import tempfile
import unittest

import architecture as arch
import flows


def fixture(decoder=False):
    path = "internal/gateway/receiver_connection.go"
    calls = [
        {"path": path, "function": function, "symbol": target, "line": line}
        for function, target, line in (
            ("consume", "m.persistFrames", 1),
            ("persistFrames", "m.insertEvent", 5),
            ("persistFrames", "tx.Commit", 10),
            ("persistFrames", "m.ackChunk", 20),
            ("ackChunk", "t.writeTracked", 30),
        )
    ]
    bindings = []
    if decoder:
        calls.extend([
            {"path": path, "function": "persistFrames", "symbol": "m.WakeDecoder", "line": 12},
            {"path": "internal/gateway/decoder.go", "function": "Run", "symbol": "m.process", "line": 40},
        ])
        bindings.append({"path": "internal/httpapi/native_receiver.go", "function": "RunReceiver",
                         "symbol": "s.Receiver.WakeDecoder = s.Decoder.Notify", "line": 3})
    return {"calls": calls, "bindings": bindings}


class FlowTests(unittest.TestCase):
    def test_customer_business_keeps_proof_confirmation_and_execution_separate(self):
        facts = {"calls": []}
        for path, function, symbols in (
            ("internal/control/group_binding_create.go", "Create", ("OperatorWorkspace",)),
            ("internal/control/group_binding_outbound.go", "PrepareOutboundFriend",
             ("port.LookupBindingFriend",)),
            ("internal/control/group_binding_outbound_submit.go", "SendOutboundFriend",
             ("port.SendBindingFriend",)),
            ("internal/control/group_binding_outbound_reconcile.go", "ReconcileOutboundFriend",
             ("port.ReconcileBindingOutbound",)),
            ("internal/control/group_binding_scan.go", "scanChallenge", ("s.Decode", "s.Native.BindingRoles")),
            ("internal/control/group_binding_scan.go", "recordBindingIdentity", ("s.checkOutboundSender",)),
            ("internal/control/group_binding_scan.go", "recordProof", ("s.checkOutboundSender",)),
            ("internal/control/group_binding_confirm.go", "currentProof", ("s.Native.BindingRoles",)),
            ("internal/control/group_binding_confirm.go", "Confirm", ("s.bindProvenGroup",)),
            ("internal/control/group_binding_confirm.go", "Review", ("s.bindProvenGroup",)),
            ("internal/control/onboarding_proof_binding.go", "bindProvenGroup", ("bindCustomerGroupTx",)),
            ("internal/control/group_binding_accept.go", "ConfirmAccept",
             ("RequireBusinessAdmission", "s.Native.AcceptBindingInvitation")),
            ("internal/control/business_rules_consumer.go", "discover", ("s.messages", "s.consumeMessages")),
            ("internal/control/business_rules_consumer.go", "candidate",
             ("features.EvaluateBusinessRules", "s.recordCandidate")),
            ("internal/control/business_rules_execute.go", "execute", ("s.prepareAttempt", "s.Execute")),
            ("internal/control/business_rules_scope.go", "authorizeAction", ("RequireBusinessAdmission",)),
            ("internal/control/business_rules_history.go", "Reconcile", ("s.Observe",)),
        ):
            facts["calls"].extend({"path": path, "function": function, "symbol": symbol, "line": 1}
                                 for symbol in symbols)
        edges = []
        flows.add_customer_business(facts, lambda *args, **kwargs: None,
            lambda a, b, view, kind, evidence: edges.append((a, b, evidence)))
        self.assertTrue(all(evidence for _, _, evidence in edges))
        self.assertIn(("role", "binding"), [(a, b) for a, b, _ in edges])
        self.assertIn(("attempt", "admission"), [(a, b) for a, b, _ in edges])
        self.assertIn(("challenge", "workspace"), [(a, b) for a, b, _ in edges])
        self.assertIn(("friend-target", "friend-gate"), [(a, b) for a, b, _ in edges])
        self.assertIn(("friend-gate", "unknown"), [(a, b) for a, b, _ in edges])
        self.assertNotIn(("decision", "native"), [(a, b) for a, b, _ in edges])
        for symbol in ("s.Decode", "s.bindProvenGroup", "bindCustomerGroupTx", "RequireBusinessAdmission", "s.prepareAttempt",
                       "OperatorWorkspace", "port.LookupBindingFriend", "port.SendBindingFriend",
                       "s.checkOutboundSender", "port.ReconcileBindingOutbound"):
            missing = {"calls": [c for c in facts["calls"] if c["symbol"] != symbol]}
            with self.assertRaisesRegex(ValueError, "evidence disappeared"):
                flows.add_customer_business(missing, lambda *a, **kw: None, lambda *a, **kw: None)

    def test_assistant_actions_require_separate_human_and_business_admission(self):
        facts = {"calls": []}
        for path, function, symbols in (
            ("internal/control/assistant_turns.go", "Call", ("h.service.actionTool",)),
            ("internal/control/assistant_actions.go", "actionTool", ("s.proposeAction",)),
            ("internal/control/assistant_actions.go", "ApproveAction", ("s.authorize", "tx.Commit", "s.ActionExecutor")),
            ("internal/httpapi/assistant.go", "assistantRoutes", ("s.Assistant.ApproveAction",)),
            ("internal/httpapi/assistant_execution.go", "executeHumanAction",
             ("service.ExecuteAssistant", "s.Receiver.SubmitText", "s.Receiver.RecallAssistantWSL", "s.Receiver.SubmitAssistantChiliMessage")),
            ("internal/control/assistant_execution.go", "ExecuteAssistant", ("s.execute",)),
            ("internal/control/native_settings.go", "execute", ("s.authorize", "req.business.prepareMessage")),
            ("internal/control/native_settings.go", "authorize", ("req.business.authorize", "req.business.messageFence")),
        ):
            facts["calls"].extend({"path": path, "function": function, "symbol": symbol, "line": 1}
                                 for symbol in symbols)
        edges = []
        flows.add_assistant_actions(facts, lambda *a, **kw: None,
            lambda a, b, view, kind, evidence: edges.append((a, b, evidence)))
        self.assertIn(("human", "action-approval"), [(a, b) for a, b, _ in edges])
        self.assertNotIn(("tools", "action-executor"), [(a, b) for a, b, _ in edges])
        for required in ("req.business.authorize", "s.Assistant.ApproveAction", "s.ActionExecutor"):
            missing = {"calls": [c for c in facts["calls"] if c["symbol"] != required]}
            with self.assertRaisesRegex(ValueError, "evidence disappeared"):
                flows.add_assistant_actions(missing, lambda *a, **kw: None, lambda *a, **kw: None)

    def collect(self, facts, decoder=False, chili=False, tools=False, preparation=False, membership=False, media=False, test_binding=False, chili_push=False):
        nodes, edges = [], []
        def node(id_, view, kind, **extra):
            nodes.append({"id": id_, "view": view, "kind": kind, **extra})
        def edge(from_, to, view, kind, evidence, **extra):
            edges.append({"from": from_, "to": to, "view": view, "kind": kind,
                          "evidence": evidence, **extra})
        candidates = {"internal/gateway/decoder.go"} if decoder else set()
        if chili:
            candidates.add("internal/gateway/lajiaoliao.go")
        if chili_push:
            candidates.add("internal/gateway/lajiaoliao_push.go")
        if tools:
            candidates.add("internal/httpapi/test_tools.go")
        if preparation:
            candidates.add("internal/httpapi/claim_code.go")
        if test_binding:
            candidates.add("internal/httpapi/test_binding.go")
        if membership:
            candidates.add("internal/gateway/social.go")
        if media:
            candidates.add("internal/control/lajiaoliao_media.go")
        flows.add_flows(facts, candidates,
                        node, edge, False)
        return {"nodes": nodes, "edges": edges}

    def test_chili_media_keeps_independent_fence_and_readonly_recovery(self):
        facts = fixture()
        for path, function, symbols in (
            ("internal/httpapi/conversation_upload.go", "uploadConversationMedia", ("service.UploadChiliMedia",)),
            ("internal/httpapi/conversation_upload.go", "stageNativeMedia", ("service.StageChiliMedia",)),
            ("internal/httpapi/conversation_upload.go", "UploadChiliMedia", ("p.receiver.UploadChiliMedia",)),
            ("internal/httpapi/conversation_upload.go", "SubmitChiliMedia", ("p.receiver.SubmitChiliMedia",)),
            ("internal/httpapi/conversation_sending.go", "dispatchNativeText", ("service.DispatchChiliMedia",)),
            ("internal/httpapi/conversation_sending.go", "stageNativeText", ("s.nativeTextService",)),
            ("internal/control/lajiaoliao_media.go", "UploadChiliMedia", ("port.UploadChiliMedia",)),
            ("internal/control/lajiaoliao_media.go", "DispatchChiliMedia", ("port.SubmitChiliMedia",)),
            ("internal/control/lajiaoliao_media.go", "ReconcileChiliMedia", ("port.ChiliMediaEvidence",)),
            ("internal/gateway/lajiaoliao_media.go", "SubmitChiliMedia", ("transport.SendOutbound", "fence")),
            ("internal/httpapi/native_text_status.go", "reconcileAccountNativeText", ("service.ReconcileChiliMedia",)),
        ):
            for symbol in symbols:
                facts["calls"].append({"path": path, "function": function, "symbol": symbol, "line": 1})
        graph = self.collect(facts, media=True)
        edges = [edge for edge in graph["edges"] if edge["view"] == "controlled-tools"]
        self.assertEqual(len(edges), 7)
        self.assertFalse(any(edge["from"] in {"heatbot", "assistant", "runtime"} for edge in edges))
        for required in ("fence", "port.ChiliMediaEvidence", "service.UploadChiliMedia"):
            missing = {**facts, "calls": [call for call in facts["calls"] if call["symbol"] != required]}
            with self.assertRaisesRegex(ValueError, "evidence disappeared"):
                self.collect(missing, media=True)

    def test_acceptance_tools_are_separate_and_require_final_fence_evidence(self):
        facts = fixture()
        endpoint = "internal/httpapi/test_tools.go"
        for path, function, symbols in (
            (endpoint, "testToolCall", ("service.TestScope", "s.Receiver.Moderation",
                "service.ExecuteModeration", "s.Receiver.ChangeModeration", "service.Inspect", "service.Reconcile")),
            ("internal/control/native_settings.go", "execute", ("s.authorize",)),
            ("internal/gateway/moderation.go", "moderation", ("fence", "native.ChangeModeration")),
        ):
            facts["calls"].extend({"path": path, "function": function, "symbol": symbol, "line": n + 1}
                                 for n, symbol in enumerate(symbols))
        graph = self.collect(facts, tools=True)
        edges = [e for e in graph["edges"] if e["view"] == "controlled-tools"]
        self.assertEqual(len(edges), 9)
        self.assertTrue(all(e["evidence"] for e in edges))
        self.assertFalse(any("assistant" in e["from"] or "heatbot" in e["from"] for e in edges))
        for path, function, symbol in (
            ("internal/httpapi/control.go", "updateAccount", "control.RequireTestConnectionTx"),
            ("internal/httpapi/claim_code.go", "issueClaimCode", "control.IssueClaimCodeForSession"),
            ("internal/httpapi/operations.go", "createTestWindow", "auditTx"),
            (endpoint, "testToolCall", "s.memberResultAccessForAccount"),
            (endpoint, "testToolOperations", "service.TestOperations"),
        ):
            facts["calls"].append({"path": path, "function": function, "symbol": symbol, "line": 1})
        current = self.collect(facts, tools=True, preparation=True)
        current_edges = [e for e in current["edges"] if e["view"] == "controlled-tools"]
        self.assertEqual(len(current_edges), 14)
        for path, function, symbol in (
            ("internal/httpapi/test_binding.go", "authorizeTestBinding", "control.AuthorizeTestBinding"),
            ("internal/control/test_binding.go", "AuthorizeTestBinding", "RequireTestConnectionTx"),
        ):
            facts["calls"].append({"path": path, "function": function, "symbol": symbol, "line": 1})
        bound = self.collect(facts, tools=True, preparation=True, test_binding=True)
        bound_edges = [e for e in bound["edges"] if e["view"] == "controlled-tools"]
        self.assertEqual(len(bound_edges), 16)
        self.assertFalse(any(e["from"] in {"endpoint", "assistant", "heatbot"}
                             and e["to"] == "test-binding" for e in bound_edges))
        missing_binding_gate = {**facts, "calls": [c for c in facts["calls"]
            if not (c["path"] == "internal/control/test_binding.go" and c["symbol"] == "RequireTestConnectionTx")]}
        with self.assertRaisesRegex(ValueError, "evidence disappeared"):
            self.collect(missing_binding_gate, tools=True, preparation=True, test_binding=True)
        for path, function, symbols in (
            (endpoint, "testToolCall", ("s.Receiver.Social", "service.ExecuteSocial", "s.Receiver.ChangeSocial")),
            ("internal/gateway/social.go", "social", ("fence", "social.ChangeSocial")),
        ):
            facts["calls"].extend({"path": path, "function": function, "symbol": symbol, "line": 1}
                                 for symbol in symbols)
        membership = self.collect(facts, tools=True, preparation=True, membership=True)
        self.assertEqual(len([e for e in membership["edges"] if e["view"] == "controlled-tools"]), 19)
        no_social_fence = {**facts, "calls": [c for c in facts["calls"]
            if not (c["path"] == "internal/gateway/social.go" and c["symbol"] == "fence")]}
        with self.assertRaisesRegex(ValueError, "evidence disappeared"):
            self.collect(no_social_fence, tools=True, membership=True)
        self.assertFalse(any(e["from"] == "endpoint" and e["to"] in {"claim-code", "admin-setup", "test-admission"}
                             for e in current_edges))
        without_delivery = {**facts, "calls": [c for c in facts["calls"] if c["symbol"] != "s.memberResultAccessForAccount"]}
        with self.assertRaisesRegex(ValueError, "evidence disappeared"):
            self.collect(without_delivery, tools=True, preparation=True)
        facts["calls"] = [c for c in facts["calls"] if c["symbol"] != "fence"]
        with self.assertRaisesRegex(ValueError, "evidence disappeared"):
            self.collect(facts, tools=True)

    def test_chili_cursor_commit_is_not_ack(self):
        facts = fixture(True)
        path = "internal/gateway/lajiaoliao.go"
        facts["calls"].extend([
            {"path": path, "function": function, "symbol": target, "line": line}
            for function, target, line in (
                ("lajiaoConnection", "m.lajiaoConnectionUsing", 45),
                ("lajiaoConnectionUsing", "m.pullChili", 50),
                ("pullChili", "m.commitChili", 60),
                ("commitChili", "tx.Commit", 70),
                ("commitChili", "m.WakeDecoder", 80),
            )
        ])
        facts["calls"].append({"path":"internal/gateway/decoder.go",
                               "function":"process","symbol":"m.decodeChili","line":90})
        graph = self.collect(facts, True, True)
        edges = {(e["from"], e["to"]) for e in graph["edges"]}
        self.assertIn(("chili-store", "chili-commit"), edges)
        self.assertIn(("chili-commit", "notify"), edges)
        self.assertNotIn(("chili-commit", "ack"), edges)
        receive = next(e for e in graph["edges"] if e["from"] == "chili-receive")
        self.assertEqual(len(receive["evidence"]), 2)
        cached = {**facts, "calls": [
            {**call, "symbol": "m.pullChiliCached"} if call["symbol"] == "m.pullChili" else
            {**call, "function": "pullChiliCached"} if call["function"] == "pullChili" else call
            for call in facts["calls"]
        ]}
        cached_graph = self.collect(cached, True, True)
        store = next(e for e in cached_graph["edges"] if e["from"] == "chili-pull")
        self.assertEqual(store["evidence"][0]["function"], "pullChiliCached")
        observed = {**cached, "calls": [
            {**call, "symbol": "m.observeChiliCommit"} if call["symbol"] == "m.WakeDecoder"
            and call["path"] == path else call for call in cached["calls"]
        ] + [{"path": path, "function": "observeChiliCommit", "symbol": "m.WakeDecoder", "line": 100}]}
        self.collect(observed, True, True)
        push_path = "internal/gateway/lajiaoliao_push.go"
        pushed = {**observed, "calls": observed["calls"] + [
            {"path": path, "function": "lajiaoConnectionUsing", "symbol": "m.commitChiliPush", "line": 55},
            *({"path": push_path, "function": function, "symbol": symbol, "line": line}
              for function, symbol, line in (
                  ("commitChiliPush", "m.commitChiliPushAttempt", 10),
                  ("commitChiliPushAttempt", "m.chiliFence", 20),
                  ("commitChiliPushAttempt", "tx.Commit", 30),
                  ("commitChiliPushAttempt", "m.observeChiliCommit", 40),
              ))
        ]}
        pushed_graph = self.collect(pushed, True, True, chili_push=True)
        push_edges = {(e["from"], e["to"]) for e in pushed_graph["edges"]}
        self.assertIn(("chili-push-commit", "notify"), push_edges)
        self.assertNotIn(("chili-push-commit", "ack"), push_edges)
        self.assertNotIn(("chili-push-commit", "chili-commit"), push_edges)
        early = {**pushed, "calls": [
            {**call, "line": 25} if call["path"] == push_path
            and call["symbol"] == "m.observeChiliCommit" else call for call in pushed["calls"]
        ]}
        with self.assertRaisesRegex(ValueError, "push wake"):
            self.collect(early, True, True, chili_push=True)
        no_wake = {**pushed, "calls": [
            call for call in pushed["calls"] if call["function"] != "observeChiliCommit"
        ]}
        with self.assertRaisesRegex(ValueError, "evidence disappeared"):
            self.collect(no_wake, True, True, chili_push=True)
        missing_cached_call = {**cached, "calls": [
            call for call in cached["calls"] if call["symbol"] != "m.pullChiliCached"
        ]}
        with self.assertRaisesRegex(ValueError, "evidence disappeared"):
            self.collect(missing_cached_call, True, True)
        no_connection = {**facts, "calls": [c for c in facts["calls"] if c["symbol"] != "m.lajiaoConnectionUsing"]}
        with self.assertRaisesRegex(ValueError, "evidence disappeared"):
            self.collect(no_connection, True, True)
        wrapped = {**facts, "calls": [c for c in facts["calls"] if c["symbol"] != "m.decodeChili"]}
        wrapped["calls"].extend([
            {"path": "internal/gateway/decoder.go", "function": "process",
             "symbol": "m.processMeasured", "line": 90},
            {"path": "internal/gateway/decoder.go", "function": "processMeasured",
             "symbol": "m.decodeChiliSource", "line": 100},
        ])
        measured = self.collect(wrapped, True, True)
        dispatch = next(e for e in measured["edges"] if e["to"] == "chili-decode")
        self.assertEqual(len(dispatch["evidence"]), 2)
        wrapped["calls"] = [c for c in wrapped["calls"] if c["symbol"] != "m.processMeasured"]
        with self.assertRaisesRegex(ValueError, "evidence disappeared"):
            self.collect(wrapped, True, True)
        facts["calls"][-2]["line"] = 65
        with self.assertRaisesRegex(ValueError, "cursor/event commit"):
            self.collect(facts, True, True)

    def test_decoder_is_conditional_and_does_not_gate_ack(self):
        old = self.collect(fixture())
        self.assertNotIn("decoder", {n["id"] for n in old["nodes"]})
        current = self.collect(fixture(True), True)
        relations = {(e["from"], e["to"]) for e in current["edges"]}
        self.assertIn(("insert", "commit"), relations)
        self.assertNotIn(("persist", "commit"), relations)
        self.assertIn(("commit", "ack"), relations)
        self.assertIn(("commit", "notify"), relations)
        self.assertNotIn(("decoder", "ack"), relations)
        self.assertNotIn(("ack", "decoder"), relations)

    def test_missing_scope_or_reordered_commit_fails_closed(self):
        facts = fixture()
        facts["calls"][2]["function"] = "unrelated"
        with self.assertRaisesRegex(ValueError, "evidence disappeared"):
            self.collect(facts)
        facts = fixture()
        facts["calls"][2]["line"] = 21
        with self.assertRaisesRegex(ValueError, "commit-before-ACK"):
            self.collect(facts)
        facts = fixture(True)
        facts["bindings"] = []
        with self.assertRaisesRegex(ValueError, "evidence disappeared"):
            self.collect(facts, True)

    def test_call_ast_preserves_function_scope_without_arguments(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "fixture.go").write_text(
                'package fixture\nfunc persistFrames() { tx.Commit(ctx); m.WakeDecoder() }\n'
                'func unrelated() { tx.Commit(ctx); Factory("PRIVATE_LITERAL").Do() }\n'
                'func RunReceiver() { s.Receiver.WakeDecoder = s.Decoder.Notify }\n')
            raw = subprocess.check_output(["go", "run", str(arch.HERE / "gofacts.go"), str(root)],
                                          input='["fixture.go"]', text=True)
            facts = json.loads(raw)
            self.assertEqual(len(flows.scoped(facts, "fixture.go", "persistFrames", "tx.Commit")), 1)
            self.assertEqual(len(flows.scoped(facts, "fixture.go", "RunReceiver",
                                 "s.Receiver.WakeDecoder = s.Decoder.Notify", "bindings")), 1)
            self.assertNotIn("PRIVATE_LITERAL", raw)
            with self.assertRaises(ValueError):
                flows.scoped(facts, "fixture.go", "persistFrames", "s.Assistant.Approve")


if __name__ == "__main__":
    unittest.main()
