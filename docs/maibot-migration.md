# MaiBot Go Migration

Controlled demo ownership now carries a persistent onboarding scope and version.
Heatbot generation and sending recheck that scope through Go control; the engine
receives neither scope management nor native-debug tools. Scope closure, expiry,
account changes and paused ownership stop new model and send submissions even
before scanner cleanup. Shared scope reservations count failed/unknown model
calls and native attempts. This change adds no chat consumer, long-lived group
worker, memory, learning or plugin. Synthetic expiry/concurrency coverage and
real low-frequency platform acceptance must be reported separately.

Reference: MaiBot `062d45cf8bd4272ac57d7ec03fcf405e5a458211`, specifically
`src/webui/routes.py` and its registered router modules. The new UI is Vue/
Element Plus with a standalone product theme; control and chat computation are Go.
No Python runtime is used.

2026-11-06 operator-owned resource spaces add a distinct background service
entitlement in control. A real superadmin owns one private `operator_demo`
space; its manually enabled heat tasks use `workspace_demo / platform_workspace`
attribution, a durable task and single-use grant, with actual and frozen costs
counted against the platform total budget. Customer spaces continue to use
their own subscriptions and wallets. Human `operator_demo / platform_demo`
records retain their meaning. No website session is forged for a consumer,
and the text engine receives no new tools, billing access or native authority.
Runtime, scope, source admission, cancellation and final-send checks remain
mandatory. Real friend and group onboarding is accepted separately.

2026-11-05 human operator demo adds a server-derived entitlement for page and
management-assistant use. It does not apply to heat consumers. Basic-chat
generation and native sending recheck the customer runtime, package and
source-specific formal admission; disabling customer business prevents later
model submission and native sends. No engine tools, memory, learning or plugins
are added. Scope, cancellation and final-fence regressions are recorded
separately from real chat-account acceptance in the current test report.

2026-11-05 customer/business control adds a separate durable automatic-rule
consumer and shares its pure matching decision with basic-chat suppression.
Challenge messages, moderation and keyword matches cannot trigger a competing
heat reply. Disabled business does not create rule tasks or native role reads.
This does not extend `internal/heatbot`: generation still returns text only.
Source/platform/action pilot admission, version cancellation, unknown results
and real group effects are checked independently. See
[customer onboarding](customer-onboarding.md) and the current test report.

2026-11-04 basic chat v51, deployed with both platform admissions closed: dual-platform bounded consumption, immutable
job scope, single-use shared-gateway redemption, separate durable native send
attempts and unknown-result reconciliation are owned by control. The engine's
`/internal/v1/chat/generate` accepts text-only scoped jobs without memory,
learning, plugins or native execution. See [cache/basic-chat contract](cache-and-basic-chat.md).
Platform admission starts closed; synthetic scope, cancellation and recovery
checks do not replace separately authenticated chat-account/group acceptance.

2026-10-31 owner decision: the platform supplies the complete managed robot service.
Web management conversation is a core configuration experience; independent
superadmin AI chat is retired from product navigation. Learning, people, memory,
media memory and reviewed tools are optional first-party extensions. The
[decision](architecture/decisions/2026-10-31-managed-service-and-extensions.md)
defines disabled-runtime and post-projection boundaries; its target scheduler
is not yet implemented.

| Upstream capability | New location | Current delivery | Remaining acceptance |
| --- | --- | --- | --- |
| Bot accounts / identity | Topbar + account-scoped native login | Independent workspace panels and preserved view state | Native receive/reconnect and real login acceptance |
| Persona / reply style | AI 人设 + `internal/heatbot/snapshot.go` | Versioned configuration and immutable generation snapshots; basic chat selects same-platform published group persona or account default | Real chat-account/group acceptance and upstream behavior comparison |
| Chat settings | 聊天配置 | Validation, sealed TTL storage, CAS, scoped drafts and immutable basic-chat settings; account enable, time intersection and group overrides enforced | Real platform acceptance; admission remains closed |
| Model configuration | AI 网关 + 模型服务 | Shared control-owned gateway; fixed upstream inspection, safe rate drafts, explicit immutable USD/USDT accounting policy, capped/idempotent superadmin test; alias/version and single-use scoped grant replace production vendor keys | Complete verified provider pricing/usage acceptance, native inbox-to-engine routing, vision/embedding execution |
| Platform assistant (legacy local extension) | Legacy control endpoints; no product chat entry | Separate sessions and financial evidence retained; old UI links resolve to assistant audit or overview | Close new-call admission through versioned settings; do not erase history or unknown reservations; no new platform-chat development |
| Management conversation UI (local extension) | Web 群管助手 / control / Vue | Dual-platform context pagination; server-resolved single-group scope; native nickname mapping; Element Plus X bubbles/history, ten-round retention and singleton empty conversation; scoped drafts and cancelled stale reads | Separate from group heatbot and platform assistant; explicit grants, package, group authorization and human confirmation; no open Agent tools |
| Management action plans (local extension) | Web / control / existing native ports | Sealed single-group plans; group-name, text and recall adapters; bounded recent-message choices with explicit human selection; independent durable attempts and final connection fence | Models cannot approve or execute; production admission stays closed per platform; cancellation never replays an unknown attempt; synthetic scope, cancellation and recovery tests only |
| Human-confirmed action suggestions (local extension, 2026-11-02 candidate) | control / sealed plans / fixed native adapter | Scoped capability whitelist; at most20 member candidates with human disambiguation; immutable10-minute plans, actor/platform RLS, epoch/version and idempotent original-operation query | Production admission is platform-specific and closed; no test grants, native_debug, financial actions or autonomous dispatch; cancellation/authorization/300-group synthetic checks recorded in test report |
| Passive reply / probability participation | `internal/control/heat_chat.go` + `internal/heatbot` | Durable bounded inbox decisions, verified mention/reply priority, four active jobs, one pending per group, expiry/cancellation, single-use gateway and final native fence; no idle proactive chat | Both platform admissions closed; independently authenticated chat accounts and real group/fee acceptance |
| Statistics / reasoning | 运行统计 + private proposal contract | Shared period credit reservation/settlement and frozen unknown costs; reasoning/tools never returned as replies | Real provider usage verification, runtime telemetry and safe traces |
| Native decode observation (local extension) | Account dashboard / workspace overview / shared monitor | Account and workspace P95, latest 1000 valid observations within 24 hours, histogram and coverage; bounded parser value arena and canonical Base64 validation preserve authenticated routing | Synthetic codec benchmarks are separate from native throughput, ACK latency and 24-hour stability acceptance; no additional engine or transport capability |
| People / impressions | 人物与印象 | Migration status, no global read path | Go group-scoped storage, deletion, retention and retrieval |
| Expressions / jargon / text learning | 表达与黑话 | Account defaults stored; cross-group settings rejected | Go extraction, group-scoped storage and compare fixtures |
| Long-term memory / graph / search | 长期记忆 | Retention/default validation; scoped request contract | Go group-only persistence, vector index, graph and isolation tests |
| Emoji / avatar / user emoji / media | 表情与媒体 | Migration status, no unscoped media API | Scoped object storage, vision pipeline and expiry |
| Optional extension directory / reviewed tools | 扩展中心 | Learning/memory defaults linked; unimplemented runtime switches disabled; other extension details removed from primary navigation | Group-scoped registration, versioned activation, separate bounded scheduling, recovery and disabled-path tests; arbitrary MCP/code execution remains disabled |
| Logs / websocket events | 引擎操作记录 | Safe configuration audit, per-account filtering | Scoped runtime event stream and redaction |
| Data transfer / backup | 备份与迁移 | Docker database backup and migration runbook | Scoped derived-data export/import and retention-aware restore |
| System / self update | 引擎与升级 | Docker release workflow; no in-app shell/update | Release/rollback rehearsal and complete parity approval |
| News / upstream search service | No automatic external feed | Not connected | Decide whether necessary for the product; never expose tenant prompts |

Settings-only panels save real versioned records but explicitly return
`applied=false`. Learning and memory configuration does not create a learning
pipeline or weaken group isolation. Engine health reports `parity_verified=false`.
Normal platform operation cannot enable test generation through WebUI.

2026-10-27 cross-platform update: requests/proposals now require an explicit
`wsl` or `lajiaoliao` platform. Queue keys include platform/tenant/group,
the bounded state count is 300 and concurrency remains four globally.
Published persona names may be selected from the user center without exposing
prompts. Account purpose/platform, same-platform bindings and package platforms
are separately enforced. Native inbox routing, heatbot token reservations,
memory/learning parity and final text submission are still not activated.
See `multi-platform.md`; these structural changes are not native/AI acceptance.

Acceptance order: scoped message/model contract and queue; explicit persistent
memory boundaries; learning/people/media; usage and traces; plugin safe subset;
native event routing and control submission; parity and resource soak tests.
The Go replacement must not be described as a completed MaiBot port until all
required rows have executable comparison fixtures and recorded results.

## Text Core Batch, 2026-10-28

The reviewed local reference remains the pinned MaiBot source above. This batch
ports the text-generation boundary into Go; it does not deploy the Python
runtime or claim parity for persistent learning, people, memory or media.

- `Settings` snapshots carry their own version, reply style, history count,
  output-token bound and optional temperature. `Submit` copies caller-owned
  slices and pointers before queuing. Concurrent settings changes cannot
  overwrite another generation. Version-zero legacy inputs use fixed defaults
  only: 40 recent messages, 1024 output tokens and no custom style/temperature.
- An optional `ContextSnapshot` requires the exact platform/tenant/group/account,
  a positive version and an expiry. It accepts at most 16 entries, 2048 bytes
  each and 8192 bytes total, of impression/memory/expression/jargon/summary data.
  Empty or mismatched scope fails closed. These are supplied, already-authorized
  fragments, not a new storage/search/learning implementation.
- Context is JSON-encoded into an explicitly untrusted user message; only
  the immutable persona/style use the system role. This framing cannot guarantee
  model resistance to prompt injection; authority still stays with Go control.
- Model execution is bounded to 90 seconds and the earlier request/context
  expiry. Pending cancellation/expiry completes without waiting for a model
  slot. Cancellation after a model starts rejects even a late successful result.
  The existing four-call, one-per-group, one-pending and 300-state bounds remain.
- The HTTP adapter sends no tools and rejects returned tool/function calls,
  refusals, non-assistant or incomplete text, invalid UTF-8 and oversized replies.
  It never returns reasoning content or vendor error bodies and never retries a
  failed paid request automatically.
- Proposals echo settings/context versions and the effective expiry. Usage is
  `reported` only for a complete consistent vendor token triple; missing or
  malformed usage is `unknown` with absent numeric fields. Work removed before
  a provider call is `not_requested`. Known usage survives failed output
  validation; private HTTP failure responses preserve its safe metadata.
  The engine does not charge or settle tokens. Control must retain unknown
  reservations and revalidate authorization, all versions and expiry before send.

Scope, concurrent mutation, queued/active cancellation, expiry, bounded context,
provider payload, malformed usage, no-retry and private endpoint gate fixtures
are in `internal/heatbot/*_test.go` and `cmd/heatbot/main_test.go`. All providers
are synthetic; test generation and both readiness gates remain disabled.
Actual command results and deployment scope are recorded separately in
`docs/test-report.md`.

## Shared Gateway, 2026-10-29

`cmd/heatbot` now constructs only `GatewayProvider`, with the fixed internal
Compose API destination. Production requests carry an alias, route version and
30-second single-use control grant; vendor URLs, keys and names are rejected.
The legacy HTTP provider remains a synthetic comparison oracle, not a runtime
fallback. Public Nginx rejects `/internal/`.

Control binds grants to the confirmed tenant owner, platform, group, chat
account, binding/account epoch, persona version, current native membership and
deadline. It rechecks these before reservation, submission and result delivery.
Native sending still requires separate final control and acceptance gates.
The gateway shares four global/two tenant model slots and period credits with
the management assistant. Heatbot retains group serialization and merged pending
candidates. Unknown submitted costs remain frozen; recovery does not resend.

Synthetic coverage includes token replay, persona changes, owner revocation
during provider construction, same-wallet concurrency, cancellation, rate/cache
arithmetic, unknown usage, process recovery and append-only cost corrections.
Expired grant cleanup and account-removal cascade are covered separately;
neither cleanup deletes credit history or treats an unknown charge as zero.
It does not complete event-to-engine routing, learning/memory/media parity,
real provider qualification or real chat acceptance. Both readiness gates remain
false; legacy token history is not converted or charged a second time.

## Native Self Invitation, 2026-11-01

The control-owned acceptance surface adds `group_accept`, using the pinned
desktop invitation handler. It reads a fresh pending invitation for the current
authenticated account and exact group, then uses a persisted single-action grant,
sealed intent, idempotency ledger and final authorization fence. It does not
grant group roles or change global invitation privacy. Tests cover foreign scope,
session changes, grant cancellation, budget exhaustion and unknown-result recovery.
Assistant, heatbot and plugin tools do not receive this capability. Membership
must be observed independently; readiness remains false.
