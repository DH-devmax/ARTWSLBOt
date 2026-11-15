"""Reviewed business paths, backed by calls inside named Go functions.

This is not whole-program control-flow analysis. Missing reviewed anchors block
generation instead of inventing a replacement relationship.
"""


def scoped(facts, path, function, symbol, kind="calls"):
    evidence = [item for item in facts.get(kind, []) if (
        item["path"] == path and item["function"] == function and item["symbol"] == symbol)]
    if not evidence:
        raise ValueError(f"Business flow evidence disappeared: {path}:{function}:{symbol}")
    return evidence


def add_flows(facts, candidates, node, edge, has_assistant):
    if "internal/control/group_binding_scan.go" in candidates:
        add_customer_business(facts, node, edge)
    if "internal/control/manual_actions.go" in candidates:
        add_operator_demo(facts, node, edge)
    chat = "internal/control/heat_chat.go"
    if chat in candidates:
        view = "heat-chat"
        for id_, label in {
            "source": "Persisted current-group text / chat account only",
            "candidate": "Unique decision / one active and one pending per group",
            "job": "Sealed immutable job / 120-second expiry",
            "grant": "Single-use gateway redemption / separate generation deadline",
            "engine": "Text-only engine proposal / no native execution",
            "gateway": "Shared credit reservation / stable job key",
            "send": "Durable send attempt / live production scope",
            "native": "Existing account connection / one fenced write",
            "reconcile": "Unknown result / read-only original echo check",
        }.items():
            node(id_, view, "business-stage", label=label)
        edge("source", "candidate", view, "bounded persisted scan",
             scoped(facts, chat, "discover", "s.messages") + scoped(facts, chat, "discover", "s.candidate"))
        edge("candidate", "job", view, "immutable scope and unique source event",
             scoped(facts, chat, "candidate", "s.Vault.Seal"))
        edge("job", "grant", view, "one grant per stable job",
             scoped(facts, chat, "execute", "s.mintJobGrant"))
        edge("grant", "engine", view, "bounded text generation",
             scoped(facts, chat, "execute", "s.Generate"))
        edge("engine", "gateway", view, "control validates live job and wallet",
             scoped(facts, "internal/control/heat_gateway.go", "HeatGenerate", "s.generate"))
        edge("engine", "send", view, "proposal is not execution",
             scoped(facts, chat, "execute", "s.send"))
        edge("send", "native", view, "shared protocol port, independent production fence",
             scoped(facts, "internal/control/heat_send.go", "send", "s.lockSendScope")
             + scoped(facts, "internal/control/heat_send.go", "send", "s.Send"))
        edge("native", "reconcile", view, "never replay an unknown submission",
             scoped(facts, "internal/control/heat_admission.go", "Reconcile", "s.messages"))
        if "internal/control/business_rules_heat.go" in candidates:
            node("business-rules", view, "business-stage", label="Current-group rules / suppress matching heat reply")
            edge("candidate", "business-rules", view, "moderation and keyword reply take precedence",
                 scoped(facts, chat, "candidate", "s.BusinessRules.SuppressesHeat"))
    receive = "internal/gateway/receiver_connection.go"
    view = "backend-receive"
    labels = {
        "receive": "Receive consumer / bounded batch",
        "persist": "Persist frames / fenced transaction",
        "insert": "Sealed raw event insert",
        "commit": "Durable batch commit",
        "ack": "Fenced ACK chunk",
        "write": "ACK socket write",
    }
    for id_, label in labels.items():
        node(id_, view, "business-stage", label=label)
    edge("receive", "persist", view, "bounded batch call",
         scoped(facts, receive, "consume", "m.persistFrames"))
    inserts = scoped(facts, receive, "persistFrames", "m.insertEvent")
    edge("persist", "insert", view, "store raw event", inserts)
    commit = scoped(facts, receive, "persistFrames", "tx.Commit")
    ack = scoped(facts, receive, "persistFrames", "m.ackChunk")
    if (max(e["line"] for e in inserts) >= min(e["line"] for e in commit)
            or max(e["line"] for e in commit) >= min(e["line"] for e in ack)):
        raise ValueError("Reviewed insert/commit-before-ACK order changed")
    edge("insert", "commit", view, "commit before fan-out", inserts + commit)
    edge("commit", "ack", view, "after successful commit", commit + ack)
    # Newer implementations use writeTracked; older snapshots use write.
    writes = [e for e in facts.get("calls", []) if e["path"] == receive
              and e["function"] == "ackChunk" and e["symbol"] in ("t.write", "t.writeTracked")]
    if not writes:
        raise ValueError("ACK socket write evidence disappeared")
    edge("ack", "write", view, "ACK written, not business completion", writes)
    decoder = "internal/gateway/decoder.go"
    if decoder in candidates:
        wake = scoped(facts, receive, "persistFrames", "m.WakeDecoder")
        if max(e["line"] for e in commit) >= min(e["line"] for e in wake):
            raise ValueError("Decoder notification moved before durable commit")
        wiring = scoped(facts, "internal/httpapi/native_receiver.go", "RunReceiver",
                        "s.Receiver.WakeDecoder = s.Decoder.Notify", "bindings")
        node("notify", view, "business-stage", label="Nonblocking decoder notification")
        node("decoder", view, "business-stage", label="Independent bounded decoder workers")
        node("decode", view, "business-stage", label="Application decode / persisted result")
        edge("commit", "notify", view, "after successful commit", commit + wake)
        edge("notify", "decoder", view, "wake wiring, independent of ACK", wiring)
        edge("decoder", "decode", view, "worker process call",
             scoped(facts, decoder, "Run", "m.process"))
    chili = "internal/gateway/lajiaoliao.go"
    if chili in candidates:
        for id_, label in {
            "chili-receive": "LajiaoLiao push hint / recovery sweep",
            "chili-pull": "Bounded contiguous sequence pull",
            "chili-store": "Fenced sealed events and cursor transaction",
            "chili-commit": "Sequence commit only (not network ACK)",
            "chili-decode": "Scoped LajiaoLiao text projection",
        }.items():
            node(id_, view, "business-stage", label=label)
        cached_pull = any(item["path"] == chili and item["function"] == "pullChiliCached"
                          and item["symbol"] == "m.commitChili" for item in facts.get("calls", []))
        pull_function = "pullChiliCached" if cached_pull else "pullChili"
        edge("chili-receive", "chili-pull", view, "single existing account owner",
             scoped(facts, chili, "lajiaoConnection", "m.lajiaoConnectionUsing")
             + scoped(facts, chili, "lajiaoConnectionUsing", "m." + pull_function))
        edge("chili-pull", "chili-store", view, "persist verified contiguous range",
             scoped(facts, chili, pull_function, "m.commitChili"))
        commits = scoped(facts, chili, "commitChili", "tx.Commit")
        shared_observer = any(item["path"] == chili and item["function"] == "commitChili"
                              and item["symbol"] == "m.observeChiliCommit"
                              for item in facts.get("calls", []))
        observations = (scoped(facts, chili, "commitChili", "m.observeChiliCommit")
                        if shared_observer else scoped(facts, chili, "commitChili", "m.WakeDecoder"))
        wakes = (scoped(facts, chili, "observeChiliCommit", "m.WakeDecoder")
                 if shared_observer else [])
        if max(e["line"] for e in commits) >= min(e["line"] for e in observations):
            raise ValueError("LajiaoLiao decoder wake moved before cursor/event commit")
        edge("chili-store", "chili-commit", view, "raw events and cursor commit together", commits)
        edge("chili-commit", "notify", view, "nonblocking shared decoder wake", commits + observations + wakes)
        push = "internal/gateway/lajiaoliao_push.go"
        if push in candidates:
            node("chili-push-store", view, "business-stage", label="Fenced bounded zero-sequence push")
            node("chili-push-commit", view, "business-stage", label="Push commit without cursor advance")
            edge("chili-receive", "chili-push-store", view, "same owner, stable native identity",
                 scoped(facts, chili, "lajiaoConnectionUsing", "m.commitChiliPush")
                 + scoped(facts, push, "commitChiliPush", "m.commitChiliPushAttempt")
                 + scoped(facts, push, "commitChiliPushAttempt", "m.chiliFence"))
            push_commit = scoped(facts, push, "commitChiliPushAttempt", "tx.Commit")
            push_observation = scoped(facts, push, "commitChiliPushAttempt", "m.observeChiliCommit")
            if max(e["line"] for e in push_commit) >= min(e["line"] for e in push_observation):
                raise ValueError("LajiaoLiao push wake moved before durable commit")
            edge("chili-push-store", "chili-push-commit", view, "idempotent sealed receive commit", push_commit)
            edge("chili-push-commit", "notify", view, "wake after confirmed push commit",
                 push_commit + push_observation + scoped(facts, chili, "observeChiliCommit", "m.WakeDecoder"))
        measured = [item for item in facts.get("calls", []) if (
            item["path"] == decoder and item["function"] == "processMeasured"
            and item["symbol"] == "m.decodeChiliSource")]
        dispatch = (scoped(facts, decoder, "process", "m.processMeasured") + measured
                    if measured else scoped(facts, decoder, "process", "m.decodeChili"))
        edge("decoder", "chili-decode", view, "platform dispatch in same worker pool", dispatch)
    tools = "internal/httpapi/test_tools.go"
    if tools in candidates:
        view = "controlled-tools"
        for id_, label in {
            "endpoint": "Superadmin JSON Tools endpoint (not assistant / heatbot)",
            "scope": "Human window / session / package / epoch",
            "read": "Fresh native group evidence",
            "prepare": "Sealed operation / atomic attempt reservation",
            "gateway": "Existing per-account serial gateway",
            "final": "Control final submission fence",
            "native": "One fixed desktop request",
            "inspect": "Original operation query",
            "reconcile": "Read-only unknown-result reconciliation",
        }.items():
            node(id_, view, "business-stage", label=label)
        edge("endpoint", "scope", view, "validate grant before read / execution",
             scoped(facts, tools, "testToolCall", "service.TestScope"))
        edge("endpoint", "read", view, "current role / full roster / policy",
             scoped(facts, tools, "testToolCall", "s.Receiver.Moderation"))
        edge("endpoint", "prepare", view, "moderation-specific durable service",
             scoped(facts, tools, "testToolCall", "service.ExecuteModeration"))
        edge("prepare", "final", view, "revalidate before native submission",
             scoped(facts, "internal/control/native_settings.go", "execute", "s.authorize"))
        edge("endpoint", "gateway", view, "fixed gateway adapter",
             scoped(facts, tools, "testToolCall", "s.Receiver.ChangeModeration"))
        edge("gateway", "final", view, "fresh proof and submit callback",
             scoped(facts, "internal/gateway/moderation.go", "moderation", "fence"))
        edge("gateway", "native", view, "fixed moderation client, no arbitrary route",
             scoped(facts, "internal/gateway/moderation.go", "moderation", "native.ChangeModeration"))
        edge("endpoint", "inspect", view, "query original ID / key without resubmission",
             scoped(facts, tools, "testToolCall", "service.Inspect"))
        edge("endpoint", "reconcile", view, "unknown result readback only",
             scoped(facts, tools, "testToolCall", "service.Reconcile"))
        if "internal/gateway/social.go" in candidates:
            node("membership-read", view, "business-stage", label="Full directory / roster / friend evidence")
            node("membership-gateway", view, "business-stage", label="Invite one friend / non-owner self departure")
            edge("endpoint", "membership-read", view, "membership evidence, absence requires known directory",
                 scoped(facts, tools, "testToolCall", "s.Receiver.Social"))
            edge("endpoint", "prepare", view, "membership-specific durable service / same bounded grant",
                 scoped(facts, tools, "testToolCall", "service.ExecuteSocial"))
            edge("endpoint", "membership-gateway", view, "fixed social adapter",
                 scoped(facts, tools, "testToolCall", "s.Receiver.ChangeSocial"))
            edge("membership-gateway", "final", view, "fresh role / own UID / package revalidation",
                 scoped(facts, "internal/gateway/social.go", "social", "fence"))
            edge("membership-gateway", "native", view, "one fixed membership request",
                 scoped(facts, "internal/gateway/social.go", "social", "social.ChangeSocial"))
        if "internal/httpapi/claim_code.go" in candidates:
            for id_, label in {
                "admin-setup": "Separate superadmin preparation (never a model Tool)",
                "test-admission": "Test-only authenticated account / fresh receiver",
                "claim-code": "One-time hashed claim code / explicit tenant review",
                "delivery": "Website actor / explicit account / epoch delivery check",
                "history": "Actor-scoped bounded operation history (no payload / replay)",
            }.items():
                node(id_, view, "business-stage", label=label)
            edge("admin-setup", "test-admission", view, "control admission before account update",
                 scoped(facts, "internal/httpapi/control.go", "updateAccount", "control.RequireTestConnectionTx"))
            edge("admin-setup", "claim-code", view, "session-revalidated audited administrative issuance",
                 scoped(facts, "internal/httpapi/claim_code.go", "issueClaimCode", "control.IssueClaimCodeForSession"))
            edge("admin-setup", "scope", view, "persist and audit bounded human window",
                 scoped(facts, "internal/httpapi/operations.go", "createTestWindow", "auditTx"))
            edge("endpoint", "delivery", view, "revalidate explicit account after native read",
                 scoped(facts, tools, "testToolCall", "s.memberResultAccessForAccount"))
            edge("admin-setup", "history", view, "session-revalidated metadata after grant closure",
                 scoped(facts, tools, "testToolOperations", "service.TestOperations"))
            if "internal/httpapi/test_binding.go" in candidates:
                node("test-binding", view, "business-stage",
                     label="Explicit confirmed website owner / isolated binding / business disabled")
                edge("admin-setup", "test-binding", view, "separate superadmin delegated test preparation",
                     scoped(facts, "internal/httpapi/test_binding.go", "authorizeTestBinding", "control.AuthorizeTestBinding"))
                edge("test-binding", "test-admission", view, "same current account and receiver gate; no execution grant",
                     scoped(facts, "internal/control/test_binding.go", "AuthorizeTestBinding", "RequireTestConnectionTx"))
    chili_media = "internal/control/lajiaoliao_media.go"
    if chili_media in candidates:
        view = "controlled-tools"
        upload = "internal/httpapi/conversation_upload.go"
        sending = "internal/httpapi/conversation_sending.go"
        gateway = "internal/gateway/lajiaoliao_media.go"
        for id_, label in {
            "chili-media-http": "Separate superadmin message HTTP (production port absent)",
            "chili-media-stage": "Sealed Chili message intent / independent string-ID attempt",
            "chili-media-upload": "Durable upload admission / separate attempt budget",
            "chili-media-dispatch": "Current scope / reserved send / single final fence",
            "chili-media-owner": "Existing Chili connection owner / fresh native proof",
            "chili-media-native": "Fixed OpenIM text/reference/media contract / one submission",
            "chili-media-reconcile": "Bounded retained echo / exact type, peer and content",
        }.items():
            node(id_, view, "business-stage", label=label)
        edge("chili-media-http", "chili-media-upload", view, "scoped upload, no arbitrary target",
             scoped(facts, upload, "uploadConversationMedia", "service.UploadChiliMedia"))
        edge("chili-media-http", "chili-media-stage", view, "persist before dispatch",
             scoped(facts, upload, "stageNativeMedia", "service.StageChiliMedia")
             + scoped(facts, sending, "stageNativeText", "s.nativeTextService"))
        edge("chili-media-http", "chili-media-dispatch", view, "explicit human submission",
             scoped(facts, sending, "dispatchNativeText", "service.DispatchChiliMedia"))
        edge("chili-media-upload", "chili-media-owner", view, "fixed port, revalidate admission",
             scoped(facts, chili_media, "UploadChiliMedia", "port.UploadChiliMedia")
             + scoped(facts, upload, "UploadChiliMedia", "p.receiver.UploadChiliMedia"))
        edge("chili-media-dispatch", "chili-media-owner", view, "same owner, independent final fence",
             scoped(facts, chili_media, "DispatchChiliMedia", "port.SubmitChiliMedia")
             + scoped(facts, upload, "SubmitChiliMedia", "p.receiver.SubmitChiliMedia"))
        edge("chili-media-owner", "chili-media-native", view, "one fenced write, no automatic retry",
             scoped(facts, gateway, "SubmitChiliMedia", "transport.SendOutbound")
             + scoped(facts, gateway, "SubmitChiliMedia", "fence"))
        edge("chili-media-http", "chili-media-reconcile", view, "read-only unknown-result recovery",
             scoped(facts, "internal/httpapi/native_text_status.go", "reconcileAccountNativeText",
                    "service.ReconcileChiliMedia")
             + scoped(facts, chili_media, "ReconcileChiliMedia", "port.ChiliMediaEvidence"))
    if not has_assistant:
        return
    view = "agents-approval"
    runtime = "internal/assistant/runtime.go"
    adapted = "internal/assistantadapter/eino.go" in candidates
    if adapted:
        runtime = "internal/assistantadapter/eino.go"
    host = "internal/control/assistant_turns.go"
    plans = "internal/control/assistant_plans.go"
    for id_, label in {
        "runtime": "Eino ChatModelAgent (3 calls / 6 sequential tools)" if adapted else "Bounded assistant runtime",
        "check": "Server session / scope revalidation",
        "reserve": "Shared gateway budget boundary" if adapted else "Independent token reservation",
        "model": "Assistant scoped gateway adapter" if adapted else "Assistant provider generation",
        "tools": "Allowlisted Host.Call",
        "query": "Authorized safe query",
        "propose": "Suggestion-only plan",
        "human": "Separate website approval POST",
        "approve": "Approve immutable stored plan",
        "authorize": "Approval permission / epoch / role checks",
        "save": "Shared configuration transaction",
        "commit": "Commit config / execution / audit",
    }.items():
        node(id_, view, "business-stage", label=label)
    for to, target, label in (
        ("check", "host.Check", "revalidate each round / delivery"),
        ("reserve", "host.Reserve", "reserve before model request"),
        ("model", "provider.Generate", "text / tool proposals only"),
        ("tools", "host.Call", "bounded approved tool"),
    ):
        if adapted:
            function = "InvokableRun" if to == "tools" else "Generate"
            target = "s." + target
            edge("runtime", to, view, label, scoped(facts, runtime, function, target))
        else:
            edge("runtime", to, view, label, scoped(facts, runtime, "Run", target))
    for to, target, label in (
        ("check", "h.Check", "revalidate before tool"),
        ("query", "h.service.queryTool", "safe projection"),
        ("propose", "h.service.propose", "no configuration mutation"),
    ):
        edge("tools", to, view, label, scoped(facts, host, "Call", target))
    edge("human", "approve", view, "explicit approval handler",
         scoped(facts, "internal/httpapi/assistant.go", "assistantRoutes", "s.Assistant.Approve"))
    edge("approve", "authorize", view, "recheck current authorization",
         scoped(facts, plans, "Approve", "s.authorize"))
    save = scoped(facts, plans, "Approve", "SaveFeaturesTx")
    commits = scoped(facts, plans, "Approve", "tx.Commit")
    # Earlier commits may reconcile an already-committed execution.
    final = [e for e in commits if e["line"] > max(e["line"] for e in save)]
    if not final:
        raise ValueError("Approval configuration commit evidence disappeared")
    edge("approve", "save", view, "CAS / content-only validation", save)
    edge("save", "commit", view, "configuration saved, effect unverified", save + final)
    if "internal/control/assistant_actions.go" in candidates:
        add_assistant_actions(facts, node, edge)
    gateway = "internal/control/ai_gateway.go"
    if gateway in candidates:
        for id_, label in {
            "gateway": "Shared AI gateway / fair bounded queue",
            "wallet": "Atomic period wallet and sub-limit reservation",
            "vendor": "Sealed provider / pinned rates",
            "settle": "Idempotent settlement or frozen unknown usage",
            "heat": "Fixed private heatbot client (generation gated)",
            "grant": "Single-use control grant / live scope check",
        }.items():
            node(id_, view, "business-stage", label=label)
        edge("model", "gateway", view, "trusted AssistantHost identity only",
             scoped(facts, gateway, "Generate", "p.s.generate"))
        edge("gateway", "wallet", view, "short transaction, no model wait",
             scoped(facts, gateway, "generate", "s.reserve"))
        edge("gateway", "vendor", view, "one paid submission, no automatic retries",
             scoped(facts, gateway, "generate", "provider.Generate"))
        edge("gateway", "settle", view, "conservative finalization",
             scoped(facts, gateway, "generate", "s.settle"))
        edge("heat", "grant", view, "fixed Compose endpoint and token",
             scoped(facts, "internal/heatbot/gateway.go", "GenerateDetailed", "p.client.Do"))
        edge("grant", "gateway", view, "resolve tenant / group from stored grant",
             scoped(facts, "internal/control/heat_gateway.go", "HeatGenerate", "s.generate"))
        platform = "internal/control/platform_assistant.go"
        if platform in candidates:
            for id_, label in {
                "platform-human": "Superadmin website session / independent platform conversation",
                "platform-assistant": "Read-only platform assistant (one call / 45 seconds)",
                "platform-health": "Fixed aggregate platform_health projection",
                "platform-budget": "Separate platform-assistant month budget / current settings",
            }.items():
                node(id_, view, "business-stage", label=label)
            edge("platform-human", "platform-assistant", view, "separate admin endpoint; no management session IDs",
                 scoped(facts, "internal/httpapi/platform_assistant.go", "platformAssistantRoutes", "service.Generate"))
            edge("platform-assistant", "platform-health", view, "counts and gates only; no callable tools",
                 scoped(facts, platform, "Generate", "s.Snapshot"))
            edge("platform-assistant", "gateway", view, "fixed purpose / no tenant wallet / no native execution",
                 scoped(facts, platform, "Generate", "s.Gateway.generate"))
            edge("gateway", "platform-budget", view, "reserve and submit revalidate the consumer",
                 scoped(facts, gateway, "reserve", "s.platformAssistantAdmission") +
                 scoped(facts, gateway, "submit", "s.platformAssistantAdmission"))


def add_customer_business(facts, node, edge):
    view = "customer-business"
    for id_, label in {
        "challenge": "Website owner / one-time scoped challenge",
        "source": "Authenticated persisted event / independent private and group proof",
        "workspace": "Own operator space / real customer space / explicit resource owner",
        "friend-target": "Exact public account / immutable target preview",
        "friend-gate": "Explicit outgoing request / account and platform gate / one attempt",
        "role": "Fresh roster and connection owner",
        "binding": "Explicit owner confirmation / administrator review",
        "invitation": "Fixed pending invitation / explicit single attempt",
        "admission": "Platform / package / operator ceiling / group runtime / native role",
        "scan": "Enabled groups only / bounded durable consumer",
        "decision": "Literal rules / exemption / fixed priority",
        "job": "Unique sealed job / 120-second expiry",
        "attempt": "Durable attempt / serial account / final live scope",
        "native": "Existing native protocol port / one fenced write",
        "unknown": "Unknown result / reconcile original record",
    }.items():
        node(id_, view, "business-stage", label=label)
    scan = "internal/control/group_binding_scan.go"
    confirm = "internal/control/group_binding_confirm.go"
    accept = "internal/control/group_binding_accept.go"
    consumer = "internal/control/business_rules_consumer.go"
    execute = "internal/control/business_rules_execute.go"
    edge("challenge", "source", view, "only authenticated sealed ingress is proof",
         scoped(facts, scan, "scanChallenge", "s.Decode"))
    edge("challenge", "workspace", view, "operator own space or explicit customer; no fake subscription",
         scoped(facts, "internal/control/group_binding_create.go", "Create", "OperatorWorkspace"))
    edge("challenge", "friend-target", view, "bounded exact lookup; preview does not send",
         scoped(facts, "internal/control/group_binding_outbound.go", "PrepareOutboundFriend", "port.LookupBindingFriend"))
    edge("friend-target", "friend-gate", view, "explicit durable send outside assistant and heatbot",
         scoped(facts, "internal/control/group_binding_outbound_submit.go", "SendOutboundFriend", "port.SendBindingFriend"))
    edge("friend-gate", "source", view, "friendship is not proof; authenticated messages must match the pinned sender",
         scoped(facts, scan, "recordBindingIdentity", "s.checkOutboundSender")
         + scoped(facts, scan, "recordProof", "s.checkOutboundSender"))
    edge("friend-gate", "unknown", view, "read-only original request reconciliation; never resend",
         scoped(facts, "internal/control/group_binding_outbound_reconcile.go", "ReconcileOutboundFriend", "port.ReconcileBindingOutbound"))
    edge("source", "role", view, "browser identity cannot establish ownership",
         scoped(facts, scan, "scanChallenge", "s.Native.BindingRoles"))
    edge("role", "binding", view, "fresh role revalidated at explicit confirmation",
         scoped(facts, confirm, "currentProof", "s.Native.BindingRoles")
         + scoped(facts, confirm, "Confirm", "s.bindProvenGroup")
         + scoped(facts, confirm, "Review", "s.bindProvenGroup")
         + scoped(facts, "internal/control/onboarding_proof_binding.go", "bindProvenGroup", "bindCustomerGroupTx"))
    edge("challenge", "invitation", view, "separate durable admission, never a test grant",
         scoped(facts, accept, "ConfirmAccept", "RequireBusinessAdmission")
         + scoped(facts, accept, "ConfirmAccept", "s.Native.AcceptBindingInvitation"))
    edge("scan", "decision", view, "bounded persisted messages, no receive/ACK wait",
         scoped(facts, consumer, "discover", "s.messages")
         + scoped(facts, consumer, "discover", "s.consumeMessages"))
    edge("decision", "job", view, "stable dedupe and fixed action",
         scoped(facts, consumer, "candidate", "features.EvaluateBusinessRules")
         + scoped(facts, consumer, "candidate", "s.recordCandidate"))
    edge("job", "attempt", view, "persist attempt before native write",
         scoped(facts, execute, "execute", "s.prepareAttempt"))
    edge("attempt", "admission", view, "revalidate the current source-specific gate",
         scoped(facts, "internal/control/business_rules_scope.go", "authorizeAction", "RequireBusinessAdmission"))
    edge("attempt", "native", view, "control fence owns mutation",
         scoped(facts, execute, "execute", "s.Execute"))
    edge("native", "unknown", view, "read-only exact echo / no retry",
         scoped(facts, "internal/control/business_rules_history.go", "Reconcile", "s.Observe"))


def add_assistant_actions(facts, node, edge):
    view = "agents-approval"
    actions = "internal/control/assistant_actions.go"
    admission = "internal/control/assistant_execution.go"
    for id_, label in {
        "action-proposal": "Sealed single-group action suggestion / 10 minutes",
        "action-approval": "Self approval / explicit action grant / production gate",
        "action-executor": "Trusted fixed adapter (not model-callable)",
        "business-admission": "Private business admission (no test window / native_debug)",
        "native-durable": "Shared durable native operation",
        "native-fence": "Current scope / epoch / package / role / final fence",
        "message-attempt": "Durable platform identity / shared WSL nonce allocator",
        "wsl-message-port": "Existing WSL owner / text or scoped recall",
        "chili-message-port": "Existing Chili owner / string IDs / OpenIM recall",
    }.items():
        node(id_, view, "business-stage", label=label)
    edge("tools", "action-proposal", view, "suggestion only; no transport capability",
         scoped(facts, "internal/control/assistant_turns.go", "Call", "h.service.actionTool")
         + scoped(facts, actions, "actionTool", "s.proposeAction"))
    edge("human", "action-approval", view, "separate website confirmation",
         scoped(facts, "internal/httpapi/assistant.go", "assistantRoutes", "s.Assistant.ApproveAction"))
    edge("action-approval", "authorize", view, "recheck live approval and group permissions",
         scoped(facts, actions, "ApproveAction", "s.authorize"))
    edge("action-approval", "action-executor", view, "only after approved-plan commit",
         scoped(facts, actions, "ApproveAction", "tx.Commit")
         + scoped(facts, actions, "ApproveAction", "s.ActionExecutor"))
    edge("action-executor", "business-admission", view, "fixed native settings adapter",
         scoped(facts, "internal/httpapi/assistant_execution.go", "executeHumanAction", "service.ExecuteAssistant"))
    edge("business-admission", "native-durable", view, "shared persistence, separate admission",
         scoped(facts, admission, "ExecuteAssistant", "s.execute"))
    edge("native-durable", "native-fence", view, "repeat control authorization before submission",
         scoped(facts, "internal/control/native_settings.go", "execute", "s.authorize")
         + scoped(facts, "internal/control/native_settings.go", "authorize", "req.business.authorize"))
    edge("native-durable", "message-attempt", view, "reserve identity in intent transaction",
         scoped(facts, "internal/control/native_settings.go", "execute", "req.business.prepareMessage"))
    for port, symbols in (
        ("wsl-message-port", ("s.Receiver.SubmitText", "s.Receiver.RecallAssistantWSL")),
        ("chili-message-port", ("s.Receiver.SubmitAssistantChiliMessage",)),
    ):
        evidence = []
        for symbol in symbols:
            evidence += scoped(facts, "internal/httpapi/assistant_execution.go", "executeHumanAction", symbol)
        edge("action-executor", port, view, "fixed trusted port; model cannot invoke it", evidence)
    edge("message-attempt", "native-fence", view, "current connection identity and immutable target",
         scoped(facts, "internal/control/native_settings.go", "authorize", "req.business.messageFence"))


def add_operator_demo(facts, node, edge):
    view = "customer-business"
    manual = "internal/control/manual_actions.go"
    admission = "internal/control/assistant_execution.go"
    for id_, label in {
        "human-entitlement": "Live website actor / customer or operator demo",
        "manual-plan": "Independent sealed page plan / no assistant session",
        "manual-approval": "Same actor confirmation / digest / version / stable key",
        "human-gate": "Manual or assistant gate / binding / ownership / epoch / native role",
        "demo-cost": "Operator demo platform cost / no customer wallet",
        "customer-cost": "Automatic and heat use customer runtime and wallet",
    }.items():
        node(id_, view, "business-stage", label=label)
    edge("human-entitlement", "manual-plan", view, "server-derived mode and immutable scope",
         scoped(facts, manual, "scope", "ResolveHumanEntitlement")
         + scoped(facts, manual, "Create", "s.scope"))
    edge("manual-plan", "manual-approval", view, "explicit page confirmation, not model output",
         scoped(facts, "internal/httpapi/manual_actions.go", "manualActionRoutes", "s.ManualActions.Approve"))
    edge("manual-approval", "human-gate", view, "repeat live human and source admission",
         scoped(facts, manual, "Approve", "RequireHumanBusinessAdmission")
         + scoped(facts, admission, "authorizeManual", "RequireHumanBusinessAdmission"))
    edge("human-gate", "native", view, "existing durable operation and final fence",
         scoped(facts, admission, "ExecuteManual", "s.execute")
         + scoped(facts, "internal/control/native_settings.go", "authorize", "req.business.authorizeOwnership")
         + scoped(facts, "internal/control/native_settings.go", "authorize", "req.business.roleFence"))
    edge("human-entitlement", "demo-cost", view, "mode and payer frozen at reservation",
         scoped(facts, "internal/control/ai_gateway.go", "reserve", "gatewayAccounting"))
    edge("customer-cost", "admission", view, "heat never inherits human demo entitlement",
         scoped(facts, "internal/control/heat_settings.go", "heatScope", "RequireBusinessAdmission")
         + scoped(facts, "internal/control/business_rules_scope.go", "authorizeAction", "RequireBusinessAdmission"))
