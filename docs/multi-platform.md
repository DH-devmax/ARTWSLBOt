# Cross-platform delivery

Approved scope: 2026-10-27. This is implementation status, not native acceptance
or a declaration that every phase of the plan is complete.

## Identity and capacity

| Concept | Stored scope | Admission |
| --- | --- | --- |
| Panel | user / wsl / lajiaoliao / maibot | View only; no reconnect |
| Native account | platform + native UID; immutable purpose | 10 management + 10 chat, both platforms share each pool |
| Native group | platform + native group ID | 300 active authorizations globally |
| Account binding | same-platform management/chat purpose | 30 active groups per account, including disabled features |
| Heatbot request | platform + tenant + group + account, binding/epoch/persona version and deadline | 300 group states; 4 calls; one active/one replaceable pending per group |

Migration `020_multi_platform.sql` preserves existing WSL native IDs, sealed
credentials and bindings, derives chat purpose from the previous maibot
workspace, and adds forward-only constraints. Existing over-limit data is
retained; only additions are rejected. Old plans keep their quotas and gain
only `allowed_platforms=['wsl']`. Revoking authorization releases capacity;
turning off a feature does not. No migration has been run on live `wsl` as
part of this development.

## User and UI boundaries

The sidebar picker uses named icons. The top bar filters accounts belonging
to the selected panel; all enabled receiver owners remain independent of views.
New chat-account login chooses WSL or LajiaoLiao explicitly. Existing accounts
cannot change platform/purpose through a browser parameter.

Normal users default to the user center: package status, allowed platforms,
authorized groups, claims, operations and published persona names only.
Group preferences are versioned, package-capped and require live user grants.
Only activity, time window, published persona and group token cap are stored.
Enable/disable requests remain intent only and never enable the engine.
Prompts, native credentials, shared private messages and global model settings
are not exposed. Tenant queries use the existing restricted role and RLS.

## LajiaoLiao adapter

Pinned reference:
`sh492773746/AstrBot@e62e2ce12e7d8591afe995c56478205224294afd`.
`internal/lajiaoliao` ports only fixed official desktop HTTP and OpenIM
message-wire contracts. It has no upstream Python, SDK database, local bridge
server, file-based token store or per-account process.

Manual login uses a stable per-account device identity, sealed credentials, human Aliyun
verification when required and explicit rejection of device takeover.
Unknown submissions persist a review-required state; they are not retried.
The superadmin can explicitly acknowledge an official-client status review and
clear only the local session. A login submitted within the last minute cannot
be cleared; interrupted stale attempts require the same manual review. Resetting
never logs in automatically or clears stream recovery cursors.
Only the authenticated manual-login route has a 45-second request budget and
matching bounded write deadline for its three sequential upstream requests.
Ordinary routes retain the existing 15-second protection; cancellation still
stops the login chain, and final persistence rechecks the website session.
The verification iframe is opaque-origin and source/nonce checked; its real
vendor-browser compatibility still needs explicit isolated acceptance.

The existing gateway coordinates leases/epoch and renewal. One bounded socket
reader demultiplexes responses and coalesces push hints. A 15-second recovery
sweep shares the same stream, not one poller per page. Pulls require contiguous
sequences; initial attachment skips history, while recovery gaps stop.
Each recovery round pulls at most 50 messages from one conversation, rotates
over backlogged conversations and rechecks liveness before the next batch.
Raw events and cursor movement share one durable transaction. UI reports
sequence commit, not a fictitious network ACK. The existing fair centralized
decoder handles platform-scoped group and private text projections.
Media/quote/mention content remains unsupported or pending, not flattened.

Business HTTP uses the existing per-account serial/global-two scheduler with
persistent cooldown and coalescing. Group/member results have sampling times;
directories cannot create tenant authorization or replace fresh role evidence.

## Capability and rollout matrix

Each platform owns its capability and sub-item definitions. Lajiaoliao no longer
fills its inventory by iterating WSL definitions. Existing unreviewed IDs remain
investigation entries; their support and permissions require Lajiaoliao evidence.
Secondary security-code setup and real-name certification are deferred on both
platforms. See `platform-native-scope.md` and the generated `feature-catalog.md`.

| Capability | Code status | Remaining |
| --- | --- | --- |
| Four panels and safe user center | Implemented; synthetic browser/role regression passed | Production migration and tenant gray release |
| Account platform/purpose and quotas | Implemented, forward migration | Production snapshot/migration rehearsal |
| Manual LajiaoLiao password/challenge/session | Implemented, synthetic fixtures | Explicit real isolated login/device coexistence |
| Receive/recovery/cursor and directory | Implemented; synthetic persistence/fault tests passed | Native replay-window and role-enumeration acceptance |
| Shared text decoder and health projection | Implemented | Real protocol samples and full retained component coverage |
| Text send/receipt, mute/unmute primitives | Implemented, not registered as production actions | Unified durable control submission, current speech-role evidence and unknown-result reconciliation |
| LajiaoLiao authorization-code issuance and group claim | Not registered on the native receive path | Durable role-verified code issuance and end-to-end approval acceptance |
| Rules/lists/keyword/admin-only configuration | Reuses shared feature transaction | Native event-to-rule execution remains closed |
| Scheduled text publishing | Not implemented | Bounded schedule persistence/executor and submission fences |
| Cross-platform heatbot scope/fair queue | Implemented; generation gate unchanged | Native inbox routing, atomic heatbot metering and final control send |
| Memory/learning and complete MaiBot parity | Not implemented by this change | See `maibot-migration.md`; no shared-scope fallback |

The platform-specific capability table is the user-visible source of truth.
LajiaoLiao finance, invitations, lottery, private AI and full media are excluded
from this batch without removing existing WSL features. Native readiness and
AI readiness remain false. Do not enable a disabled runtime merely because
an upstream plugin, saved form or synthetic test succeeds.

## Release boundaries

Docker Compose remains the only deployment path; no services or resource
limits are added. Development validation uses synthetic HTTP/WebSocket fixtures,
isolated `wsl_test`, candidate images and browser mocks. No automatic native
signup, login, invitations, sends or moderation is allowed.

The current batch must not be described as the complete five-stage plan.
Production requires the remaining execution/metering work, explicitly scoped
native tests, migration rehearsal, target-machine 20-connection/300-group
fairness and resource tests, then per-platform acceptance. See `test-report.md`
for actual results and CodeBoarding baseline blockers.

## Workbench presentation (2026-10-28)

The panel chooser keeps the server-authorized icon/name grid in a compact,
centered two-column dialog. Selecting a panel closes mobile navigation and
changes only the view; it never logs in, saves a configuration or restarts
reception. The chat-console client also requires the superadmin role and the
explicit workspace grant; server authorization remains authoritative.

`RobotWorkbench.vue` presents the existing account/group/overview/persona
projections for the LajiaoLiao and chat overviews. It is a presentational component,
not a second store, API or runtime. LajiaoLiao links to authorization, native
capabilities and management modules; chat links to cross-platform bindings,
personas, chat/model configuration and capability acceptance. Connection records
are labeled as records, not current health evidence. Missing model telemetry
remains unavailable. Per-account link health still comes from `AccountDashboard`.
Group counts in the chat overview may include pending unassigned requests; the
group list contains existing account bindings, not inferred memberships.

The user center adds owner-scoped group search, platform/authorization filters,
compact mobile rows, package/module/action summaries and explicit expired-package
states. Action grants do not replace field-level, package or native-role checks.
Revoked groups and expired packages cannot open chat editing. Administrators see
links to user/package/claim administration, not another tenant's personal
subscription. The user center displays its own synchronization time; other
workbenches preserve their own last successful view synchronization time.

These are UI and permission-affordance changes only. No native credential,
protocol, receiver, engine, migration, runtime quota or readiness gate is changed.
Native LajiaoLiao sending and live heatbot generation remain subject to the
acceptance and execution blockers above.
