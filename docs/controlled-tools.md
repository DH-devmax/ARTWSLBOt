# Controlled native acceptance tools

## Controlled Onboarding Scope

The normal onboarding wizard may use an explicit superadmin-owned demo test
scope. The account testing flag alone cannot admit a pool claimant. Scope
creation binds both authenticated accounts, platform, isolated group and a
default and maximum six-hour deadline; each challenge retains its 10-minute expiry.
See [controlled onboarding](controlled-onboarding.md). Native action grants and
source-specific business gates remain independent. Closing a scope preserves
already submitted/unknown operations for reconciliation, disables business
runtime and never deletes friends or leaves a native group.

Decision: 2026-10-29. This is a separate superadmin acceptance interface inside
the existing API. It is not registered with the tenant management assistant or
heatbot. Those runtimes retain their existing prohibitions on native actions.
No MCP daemon, per-account process, new service or arbitrary native proxy is added.
The transport is authenticated JSON Tools, not a claim of MCP protocol compatibility.

## Native contracts and gates

The frozen desktop 2.8.3 `api/core/group.proto` and desktop route definitions
provide the following fixed contracts:

| Capability | Desktop route | Current implementation |
| --- | --- | --- |
| Member mute | `/v1/group/set-member-mute` | One target, explicit minutes; Tools limited to 1–5 |
| Member unmute | `/v1/group/member-mute-cancel` | One target; full native mute-list readback when available |
| Group mute/unmute | `/v1/group/set-group-mute` | Explicit `MUTE_MEMBER` / `MUTE_NO` |
| Remove member | `/v1/group/remove-group-member` | One `groupMemberIds` entry, never disband |
| Grant administrator | `/v1/group/add-group-manage` | Current native owner; one `groupManagerIds` entry; target currently member |
| Revoke administrator | `/v1/group/del-group-manage` | Current native owner; one `userId`; target currently administrator |
| Publish notice | `/v1/group/add-notice` | `COMMON_NOTICE`, 1–600 characters |
| WSL group name | `/v1/group/set-group-name` | Explicit `group_info` grant; current owner/admin; single bounded name |
| WSL group nickname | `/v1/group/set-member-nickname` | Explicit `member_nickname` grant; original nickname lock, self/target role and digest fences |
| Read invitation window | `/v1/group/get-apply-logs` | Fixed desktop `lastId:"0"`; canonical int64 cursor including zero; verified group filtering |
| Invite one friend | `/v1/group/add-group-member` | One authorized absent friend, current owner/admin, `friends` module |
| Self departure | `/v1/group/leave-group` | Non-owner's own UID in grant; management and AI stopped |
| WSL delete one friend | `/v1/friend/del-friend` | Separate `native_debug` window; one account, one current friend, one attempt |

Chili friend deletion has its own fixed `/friend/delete_friend` contract using
the authenticated `ownerUserID` and string `friendUserID`; it does not reuse WSL
numeric identity or its acceptance. Both implementations remain pending real
deletion acceptance. See [account social operations](native-social.md).

The invitation route alone allows at most 3 MiB of authenticated decompressed
response JSON and 4096 validated rows; other business responses keep the 1 MiB
cap. AEAD binding, one-frame validation and bounded decompression remain
mandatory. A 2026-10-30 structural read found an authenticated 1,418,150-byte
server window with 2798 rows, explaining the earlier 1 MiB rejection. A bounded
local read using the repaired codec parsed it without logging record payloads.
The separate test-window/API acceptance status remains in the test report.

The gateway uses the existing per-account serial business reader and its global
concurrency bound. Reads, role proof and mutations do not run in receive/commit/ACK.
Individual mute evidence uses only the existing connection owner, SDK 8/27;
no second socket is created. Missing evidence stays unknown.

Invitation windows are shared per account for up to 30 seconds, capped at 20
cached accounts and 4096 records each. Epoch, credential, account version and
deployment changes invalidate reuse. Receiver reconciliation removes expired
entries and stop clears them. Snapshots retain their original sample time.
The returned cursor is informational: no unverified pagination is invented.
Only a mapped group's records are delivered; applicant/inviter remain separate
native IDs. Unknown enums/invalid IDs reject the response. Visibility is not a
complete history, ownership evidence or sufficient evidence for financial rewards.

2026-10-30 real acceptance: one-minute member mute and explicit unmute passed
in the first isolated round. A separate owner-directed round used one of two
attempts and observed automatic expiry between the last muted sample at
56.680 seconds and the first clear sample at 67.746 seconds after acknowledgement.
This does not establish exact duration or exactly-once semantics.
Invitation reading in the earlier round failed with `native_response_invalid`. The pinned desktop's
`getApplyLogs` sends `{lastId:"0"}` and its protobuf request declares `last_id`;
the old empty request differed. The candidate now sends that fixed request and
accepts a valid zero cursor, with signed synthetic contract coverage. The later
bounded structural probe also established the oversized authenticated window
described above; changing the parameter alone did not resolve the failure.
Do not convert an error to an empty list.
See [the test report](test-report.md) for the concrete round, cleanup and limits.

2026-10-31/2026-11-01 real acceptance: group mute and explicit unmute passed
with policy readback; one marked test notice was acknowledged and its exact
content was subsequently read back. The 300-second mutation window used 3/3
attempts and was cancelled. A later readback window used zero attempts; no notice
or moderation operation was replayed. `read_group` exposes `notices` separately
after its existing scope/package/epoch checks; generic gateway snapshots still
omit notice content. The repaired invitation route also passed through the
formal Tools API, returning two records for the isolated group with
`complete=false`, not a complete invitation history.
The authenticated owned target is currently absent with an earlier invitation
pending, so member moderation, removal, invitation completion and departure were
not repeated this round. Self card change and restoration passed through the
separate durable settings API. All current windows are cancelled and temporary
connection/package enablement is disabled again. The old subscription plan is
restored, but its historical expired timestamp cannot be restored by the current
future-expiry-only API; this cleanup limitation is recorded in the test report.
These are scoped acceptance results, not blanket native readiness.

`native_ready` and `ai_ready` remain false. Catalog entries are `partial`,
not accepted. These explicit test operations do not activate business modules.
Durable group media and WSL private text/image have separate message-center
acceptance implementations, but are not registered as model Tools or production
send ports. Chili group text and dual-platform recall now have separate durable
controlled endpoints, described in
[the client completion record](lajiaoliao-client-completion.md).
WSL nonce-history and scoped send acceptance are recorded independently in
`native-controlled-sends-20261103.md`; they do not activate Chili sends.

`POST /api/v1/test-tools/text` accepts Chili account, window and text only.
`GET /api/v1/test-tools/messages` returns at most 20 current-group candidates;
`POST /api/v1/test-tools/recall` accepts a retained event ID, resolving its native
route server-side. Both submissions share sealed `native_setting_operations`,
explicit actions/targets, original connection ownership and final submission
fences. Text uses its durable operation ID as the native client message ID.
Unknown outcomes remain query-only; generic moderation readback cannot mark
these messages successful. These endpoints are not assistant tools.

## Human grant and execution

2026-11-01 owner decision: new grants explicitly select `admission_mode`.
`package` preserves prior subscription/module checks and remains the API default
for older clients and existing grants. `native_debug` is an isolated superadmin
diagnostic scope, independent of subscription expiry and production management/AI
switches. It still requires an active tenant/group binding, authenticated test
account and enabled action admission, immutable account epochs, fresh native
evidence, explicit actions/targets, deadline and attempt budget. Creating or using
it never changes a subscription or starts production modules. Only implemented
durable moderation/membership, the separately scoped manual `friend_delete`
action, scoped WSL group editors and reviewed WSL send/upload actions are admitted
in this mode. Private sends require separate
private actions and explicit peer targets; group-send grants cannot authorize
them. This mode cannot admit registration, SMS, arbitrary routes or assistant
execution. The UI defaults
to this diagnostic mode. The action list may contain up to 32 reviewed kinds; the
mutation budget remains at most ten and the window at most 300 seconds. Self
invitation acceptance retains its separate single-action grant. Owner-only
administrator changes cannot be granted to a native administrator. Ordinary
members may receive a separate self-departure grant or a WSL self-card grant.
The latter permits only `member_nickname` with exactly their own UID as the
single target; it does not permit editing another member.

Administrator changes use the existing sealed settings ledger and final fences.
Grant requires the target to be an ordinary member; revoke requires administrator.
Both exclude self, owner, absent and unknown-role targets. Each consumes one
attempt. An acknowledgement is separate from a full roster readback; unknown
results may only be reconciled from the original operation with current native
roles, even after the window closes. No transfer, batch role update, repeat on
timeout or production role automation is exposed. Chili retains its own pending
contract/control entry; WSL support does not activate a Chili role route.

Real R3 on 2026-11-01 19:11–19:13 Asia/Shanghai passed one grant followed by
one revoke using the two owned WSL accounts in the unique isolated test group.
Both complete rosters confirmed member/admin/member; the executor remained owner.
The native-debug window used 2/2 attempts and was cancelled. Production management,
AI and configuration stayed unchanged; temporary account action admission was
restored off. This does not cover stale-proof execution after revocation, native
transport faults, restart recovery or production role automation.

The superadmin enables an account's controlled-test switch and explicitly creates
a window on the existing test page. Account updates invalidate previous epochs.
The administrative account connection switch admits authenticated WSL or Chili
management accounts already in test mode, with a freshly leased, synchronized
receive connection and a recent heartbeat. Leaving test mode clears that switch.
This does not open production sends or change either readiness flag.
The window names one isolated authorized group, up to 10 accounts, up to 10
target IDs, allowed actions, at most 300 seconds and at most 10 mutation attempts.
Explicit `confirm_isolated=true` can mark that selected group for test use in the
same grant transaction; it is audited. Merely opening a page never marks a group
or creates a grant. Creating a grant requires authenticated accounts, reception
enabled, testing enabled and fresh native admin/owner observations. A window
containing only `group_leave`, or only WSL `member_nickname` targeting that
account itself, may instead use a verified ordinary member.
An acceptance-only `group_accept` window instead requires a fresh, authenticated
self invitation for that exact group and no current membership; it cannot be
combined with other actions or used to accept another identity's invitation.
Every departure account must have its own native UID explicitly listed as a
target; owners cannot obtain a departure grant. An ordinary member cannot mix
departure with invitation, moderation or send actions.

For an unbound group, the superadmin can issue a one-time claim code with
`POST /api/v1/groups/{id}/claim-code` and an `account_id`. Control revalidates the
website session, same-platform management account, connection, native role and absence of
an existing binding. Only the code hash is stored; audit records the actor and
account epoch without the code. The tenant claim and explicit identity-verified
administrative review remain necessary. This route is not a model Tool.
The owner's bounded local acceptance delegation in `AGENTS.md` permits the
development agent to materialize the specified administrative grant.

For an explicitly owner-delegated isolated test without commercial activation,
`POST /api/v1/groups/{id}/test-binding` accepts `account_id`, `tenant_id`,
`account_version`, `confirm_isolated=true` and `identity_verified=true`.
This superadmin-only preparation verifies an already confirmed, enabled website
tenant owner, same-platform authenticated management account, test and action
admission, current receiver lease/heartbeat and fresh native admin/owner evidence.
The caller explicitly selects the website tenant; a native roster never selects
it or establishes its ownership. Pending claims/codes and existing bindings block
this route. Global and per-account capacities share the normal admission lock.
It audits the real website actor and confirmed owner, marks that one group
isolated and creates a binding with management and AI disabled. It creates no
subscription, test window or native operation. A separate bounded `native_debug`
window is still required for every real test; `package` admission keeps its
subscription checks. Ordinary users, assistants and heatbot cannot call this
administrative route. This is not the ordinary tenant claim workflow.

Private message-center testing requires the distinct `send_private_text` or
`send_private_image` action and an explicitly listed peer target. Existing group
send actions do not authorize private messages. The peer must be a current
friend in the grant's isolated group, proven from a fresh roster and retained
sealed private conversation; callers cannot specify arbitrary native routing.
An image upload and its subsequent send each consume an attempt. These grants
do not expose private operations to tenants or the management assistant.
See `native-media.md` for the shared durable sender and remaining release gates.

Tools contain no create-grant or expand-scope operation. They use the website
session actor, never a client-supplied tenant/user/role. Every read checks the
active group authorization, persisted admission policy and current account epoch before delivery.
In `package` mode moderation also requires management enabled and the moderation module.
Both modes require a full
fresh native roster and the current account's verified native UID/NIM mapping.
Self/owner targets are protected; an admin cannot moderate another admin.
An owner may moderate a verified admin. Invitation and departure use distinct
`friends` package checks in `package` mode and persisted membership actions, including the legacy
social HTTP entry. Invitations require a full current roster and verified good
friend relationship; a native acknowledgement does not prove joining. Departure
requires management and AI disabled, and current member/admin role. Its readback
requires a successfully read complete directory; missing evidence is not absence.
An absent account's readback does not depend on fetching friends. No action
automatically invites an account or transfers tenant ownership.

Preparation atomically reserves one attempt and inserts a sealed durable intent.
Failed attempts are not refunded. Same-key retries recover the same operation
without another reservation; changing its window/action/target is rejected.
Immediately before the fixed native request, control revalidates the website
session, account, package, binding epoch, deadline, window, target and fresh proof.
Cancellation or an epoch change invalidates prepared operations. Requests already
submitted cannot be unsent by cancellation; their outcomes stay traceable.

Unknown outcomes are never automatically replayed. `inspect_operation` recovers
the original ledger record by ID/key. `reconcile_operation` performs reads only,
including after the test window expires or is cancelled, for the original
authenticated actor/account. It cannot resolve a different native login epoch.
The UI stores only the original key/account/window/time metadata in
sessionStorage, without notice text, member rosters, credentials or message bodies.
An early 404 cannot clear a request still preparing; the UI retains its key
beyond the bounded HTTP/submission deadlines before accepting an absent result.

The test page separates reception from action admission, and provides explicit
existing-session reception enable/resume and native role observation controls.
These preparation controls do not run automatically when opening the page.
The grant form starts with the selected account/group; creating a new grant still
requires explicit human submission. Before an action the UI requires a group
snapshot sampled within 30 seconds and excludes protected targets. The server
continues to collect its own fresh evidence and revalidate all authorization.
Confirmation freezes account, window and action fields, including minutes and
notice text. A successful submission triggers one read-only group check while
the grant remains valid; exhausted budgets still allow reads. Failure or mismatch
of that check never changes the acknowledged operation into a retry. Member
mute verification explicitly does not prove automatic expiration duration;
notice effects require separate evidence not exposed by this snapshot.
The page accounts for a returned durable attempt immediately and merges the
server's remaining budget monotonically, so a delayed parent-list refresh cannot
briefly re-enable a spent window. This is a UI fence; control owns the budget.

### Exact Incoming Friend Approval

`friend_approve` has a separate `native_debug` window: exactly one account and
non-self applicant, this action only, one attempt, at most 300 seconds and a
nonempty `friend_application_id`. Migration 066 enforces this independently of
the UI. Creating the window verifies that exact pending incoming application,
the receiver and current owner/admin role in its active isolated group.
The read/submit path combines current group and application evidence only when
explicitly requested; ordinary membership reads add no application request.

`run_social` supplies that same application ID in `text`; matching only the UID
cannot authorize it. All final session/binding/epoch/role/receiver/deadline fences,
account serialization and cooldowns remain. Chili rejects multiple pending
applications from the same applicant because its native approval route carries
the applicant rather than the application ID. Results distinguish native
acknowledgement from readback of the agreed application and current friendship.
Unknown outcomes only inspect/reconcile the original durable record, even after
expiry or cancellation; they never approve again.

The unscoped account-social mutation endpoint rejects approval; ordinary customer
onboarding still uses its independent dedicated confirmation and proof ledger.
This test cannot establish website identity/group ownership, bind a group,
activate business or grant assistant/heatbot access. Rejection remains a separate
manual capability and is not included in the approval grant.

### Manual friend deletion

The 2026-11-09 owner decision permits temporary deletion of the explicitly
authorized test relationship in this round. It does not authorize automatic
deletion to manufacture onboarding samples. Customer cancellation, expiry,
restart and retry continue to preserve established friendships.

`friend_delete` requires a separate persisted `native_debug` window with exactly
one account, one non-self target, only this action and `budget=1`, lasting at most
300 seconds. The selected isolated group must remain actively bound and the
authenticated testing account must have a fresh enabled receive connection and
current native owner/admin role. The target is selected from a complete current
friend list; group membership is not the deletion effect or proof of target
ownership. Both platforms verify their own identities and friend contracts.
No mixed-action, package, assistant or heatbot grant may authorize this action.

The existing `run_social` entry prepares one sealed native-setting operation,
reserves the attempt and revalidates the grant, website actor, account epoch,
binding, receiver, role, target and deadline at final submission. Other pending
account writes and existing cooldowns remain effective. The legacy account
social mutation route rejects deletion; a direct control call without the
persisted test window also fails closed.

The test page explicitly reads friend/group evidence and requires a separate
delete confirmation. Viewing or closing it never submits. Native acknowledgement
is distinct from a later complete friend-list readback. Unknown submissions
remain locked to the original operation and can only be inspected/reconciled,
including after cancellation or expiry; they are never replayed. Deleting the
friendship does not leave a group, remove a member, blacklist the peer, erase
onboarding evidence or automatically add the friend again.

Forward migration 065 extends the existing operation-kind and single-use window
constraints; applied migrations and prior capability IDs remain unchanged.
WSL and Chili real deletion acceptance are both pending. This manual account
capability is excluded from group foundation progress and business/assistant
dependencies, even after a later controlled result passes.

`GET /api/v1/test-tools/operations?account_id=<uuid>` is a separate superadmin
administrative history route, not an additional model Tool. It revalidates the
website actor, requires a WSL management account and returns at most 50 of that
actor's moderation and membership operations, with unresolved entries first. Actor/window/group
scope must agree. Expired or cancelled grants remain queryable. Results contain
IDs, kind, status, result code and timestamps, without sealed request content,
targets, idempotency keys or notice text; responses are `no-store`.
The UI can recover by original operation ID after sessionStorage loss. It blocks
new submissions while history cannot be checked or any listed operation is
unresolved, and discards responses after account/workspace changes.

`succeeded / native_moderation_acknowledged` means the upstream acknowledged the
operation, not that every client applied it. `desired_state_observed` means a
subsequent read saw the desired state, not proof of exactly-once execution.
A mute list proves a Boolean, not the requested duration; an ambiguous timed
mute remains unknown. A matching notice text is likewise an observed state,
not proof that this request created the notice.

## Tools transport

`GET /api/v1/test-tools` returns seven fixed JSON Schemas:
`read_group`, `read_invitations`, `read_membership`, `run_social`, `run_moderation`, `inspect_operation`,
`reconcile_operation`. Additional properties are rejected.

`run_social` accepts the separately scoped `friend_delete` plus its one
authorized current-friend `target_id`, `group_invite` plus one authorized `target_id`,
`group_accept` plus the authenticated account's own `target_id` and canonical
invitation ID in `text`, or `group_leave` without a caller-supplied target.
An acceptance grant contains only `group_accept`. Creating it requires a fresh
native pending invitation for the exact account and group, current sealed-session
and deployment fingerprints, enabled receiver and account switches. It does not
require or manufacture membership for an account that has not joined yet.
The gateway re-reads the pending record at submission; control retains the
package/binding/epoch/deadline/budget and final session fences. Read-only membership
verification distinguishes acknowledgement, pending administrator approval and
actual membership. Global invitation privacy remains unchanged.
Control resolves the departing
account's own native UID and checks it against the persisted target grant.
`read_membership` requires a membership action and `friends` module, remains
read-only after budget exhaustion, and returns directory/roster/friend evidence
flags. The UI separates the two capabilities and freezes confirmation parameters.

`POST /api/v1/test-tools/call` uses the existing website cookie, CSRF/Origin
protection and the selected `X-Workspace: wsl` or `lajiaoliao`. Mutations require a stable `Idempotency-Key`.
It accepts no endpoint, script, SQL, trusted identity, raw credential or plugin.

```json
{
  "name": "run_moderation",
  "arguments": {
    "account_id": "<website-account-uuid>",
    "test_window_id": "<human-created-window-uuid>",
    "action": "mute_member",
    "target_id": "<explicitly-authorized-native-uid>",
    "minutes": 1
  }
}
```

The existing same-permission CLI adds `wslctl tools` and
`wslctl tool-call <stable-key>` with bounded JSON on stdin. See `wslctl --help`
for the website cookie file and fixed API URL options. The cookie file remains
mode 0600 outside Git. Agents must receive an explicitly authorized account,
group, targets, actions and deadline before real calls; general project
development authorization does not authorize real group effects.

Website CLI login uses two explicit steps. `login-challenge --username NAME`
prints only the server's public challenge JSON: challenge ID, expiry, image data,
dimensions and initial tile position. A human supplies the final tile coordinates
in the original image's pixels. The CLI neither solves the image nor starts a
browser, callback or local HTTP server.

```sh
wslctl -url http://127.0.0.1:5173 -session /private/path/site-cookie.json \
  login-challenge --username <website-username>
wslctl -url http://127.0.0.1:5173 -session /private/path/site-cookie.json \
  login --username <website-username> --password-stdin --captcha-x <human-x> --captcha-y <human-y>
```

The password is read only from standard input. Challenge state is a separate
0600 file, defaulting to the session path plus `.challenge.json`; global
`--challenge-state /private/path/challenge.json` overrides it for both steps.
It stores only the exact API origin, username, challenge ID, server expiry and
`wsl_human` browser cookie, outside Git. It contains no password, submitted
coordinates, answer, image or login ticket. Do not use the session file itself
as the challenge-state path.

`login` claims and removes this state before submitting verification, checks its
origin, exact username and expiry, restores the `/api/v1` browser cookie, then
calls verify and login sequentially in the same cookie jar. An invalid/expired
state, rejected verification, malformed response or unknown transport result
stops the attempt; neither request is retried. Request a fresh challenge after
failure. Successful login saves the existing website session format; already
authenticated `Load`, `tools` and `tool-call` usage is unchanged. The optional
ticket argument on `apiclient.Login` preserves existing Go call sites; it grants
no exemption from the server's mandatory captcha validation.

Example command shape, using an already prepared request file:

```sh
wslctl -url http://127.0.0.1:5173 -session /private/path/site-cookie.json tools
wslctl -url http://127.0.0.1:5173 -session /private/path/site-cookie.json \
  tool-call <stable-request-key> < /private/path/authorized-tool-request.json
```

No credentials or example real identities belong in this document. Tools are
not automatically advertised to paid models. AI orchestration may choose among
these fixed tools only inside the human grant; it cannot add capabilities.
Manual `friend_delete` is never registered with AI orchestration, assistants or
heatbot; its test authorization cannot be inherited by customer onboarding.
The existing `test-create` CLI remains a direct superadmin administrative
command, not a model-exposed tool.

## Persistence and retention

Forward migration 024 extends the existing sealed native-setting operation
ledger and adds a test-window reference. Previously applied migrations remain
unchanged. Payloads follow the existing seven-day sealed retention policy;
audit contains only safe kind/status/code metadata, never notice text or secrets.
Window account/target IDs retain the existing controlled-test evidence policy.
No tenant table privileges or RLS policies are relaxed.

Verification results, limitations and deployment status are recorded separately
in `test-report.md`. Synthetic tests never sign up, log in, send SMS or operate
real accounts. Real native acceptance requires a separately bounded grant.

## WSL send and recall acceptance, November 3, 2026

Explicit persisted native_debug grants can exercise the WSL durable group sender;
upload and send consume separate action budgets. A secondary owned ordinary
member can receive a text-only grant without replacing the primary binding.
The control layer still rejects production/package admission for that shortcut.
The isolated recall endpoint resolves a retained event into an authenticated
route and uses the existing durable settings ledger and final write fences.
Actual text, mention, quote, image, MP3, MP4 and recall effects were verified in
the owner's single test group; windows were cancelled and temporary account
action switches restored. Delayed recall notices were inspected without replay.
See `native-controlled-sends-20261103.md`; these grants are unavailable to product
assistants and do not raise readiness or enable group business modules.

The private sender's debug whitelist now includes `send_private_text` and
`send_private_image`; `upload_private_image` remains separately budgeted.
The peer still comes from an authenticated retained private projection and
fresh native friend/member evidence. Missing private source evidence blocks
preparation without creating a grant or issuing a send. Real private acceptance
and synthetic recovery results are tracked independently in
`native-private-acceptance-20261103.md`.

## Chili native admission bans (2026-11-04)

The fixed desktop 1.0.3 bundle independently exposes permanent group admission
bans. Ordinary `kick_member` explicitly sends `permanentBan=false`;
`kick_member_ban` sends true and requires that separate persisted action.
`read_group_bans` is a role-checked bounded read. `remove_group_ban` requires
its own action grant, a complete current native ban list and the exact banned
target; it does not invite or accept anyone. These are distinct from the
business blacklist and friend blacklist. They are not assistant or heatbot tools.

Both writes reuse sealed durable intent, final authorization, expiry, budget,
single-attempt and unknown-result reconciliation. WSL grants cannot admit these
Chili-only kinds. Forward migration 047 expands the operation kind constraint
without editing earlier migrations. Mobile kick notifications do not expose the
permanent flag, so they cannot approve a permanent-ban execution result.
See `lajiaoliao-membership-20261104.md` for fixed-source and actual-read evidence.

## Controlled friend lookup and outbound requests (2026-11-08)

Customer onboarding now asks the user to add the allocated robot; it never
searches for a user or sends an outbound application. The retained fixed native
contracts are available only through the superadmin controlled-test surface.
Ordinary `friend-target` and `friend-send` return 410, including for superadmins.
Existing outgoing records retain their original read-only reconciliation.

The dedicated endpoints are:

| Endpoint | Effect |
| --- | --- |
| `POST /api/v1/test-tools/onboarding-friend/windows` | Persist an explicit bounded native_debug window |
| `POST /api/v1/test-tools/onboarding-friend/challenges/{id}/friend-target` | Look up the one authorized public account and seal its exact native target |
| `POST /api/v1/test-tools/onboarding-friend/challenges/{id}/friend-send` | Explicitly submit that one sealed request at most once |

The window binds the website superadmin, an owned demo challenge, platform,
management account, isolated bound group, binding epoch, public account, native
target, challenge version, deadline and attempt budget. It requires the existing
authenticated connection and fresh native owner/admin evidence. The server
constructs a private execution context after validating the persisted window;
request fields and customer challenges cannot assert that context.
The final transaction rechecks the same scope, role evidence, admission, budget
and write cooldown. Expired, cancelled, exhausted or substituted windows cannot
submit, and a window cannot admit assistant or heatbot tools.
Lookup also revalidates the persisted window after acquiring the account read
queue. The dispatch fence holds the grant and scope locks for at most three
seconds, within the original window deadline; cancellation or expiry while
waiting prevents the native lookup.

The API's existing controlled-test panel provides a scoped friend-debug dialog.
Opening or reading it does not search or send. A lookup and an outbound request
are separate explicit actions; confirmation shows the robot and exact target.
The original operation remains the only reconciliation target after an unknown
result. Neither contact evidence nor a successful request becomes website
identity or group ownership. Window target IDs retain the existing controlled
evidence policy; credentials and arbitrary upstream responses never enter audit.

Forward064 adds only the controlled-window reference and scope constraints to
the existing outgoing ledger. Applied migrations and historical records stay
unchanged. Real acceptance, synthetic checks and deployment are reported
separately in `test-report.md`; no real search or friend request is implied here.
