# Controlled Onboarding

Controlled onboarding is a superadmin's explicitly bounded exercise of the
existing onboarding flow using authenticated operator-controlled pool accounts.
It records `controlled_test` proof in the operator's own demo workspace.
It does not establish formal customer identity, customer ownership, a commercial
subscription or a production acceptance result.

The website actor, platform, receiving management account, claimant account and
one observed native group marked `isolated_test=true` are fixed in a persisted
scope. The receiving robot
and claimant must be different authenticated identities on the same platform;
both accounts must have controlled testing enabled. Current native evidence
must show the claimant as the selected group's owner or administrator when the
robot is already present. Alternatively, an authenticated native read may prove
the receiving robot's absence from exactly the selected group for the selected
claimant. This permits preparation before joining; failed or incomplete reads
do not prove absence. An absence-based scope stores empty initial roles and no
owner identity hash, and supplies no group ownership proof.
The browser supplies selectors, never trusted identities, roles or proof.

## Administrative API

All four routes use the existing authenticated superadmin wrapper. Requests
retain the selected `X-Workspace: wsl` or `lajiaoliao`; a platform in a query or
body cannot change that website session reference. Mutations use the existing
origin and CSRF checks. Responses have `Cache-Control: no-store`.

| Method and path | Input | Result |
| --- | --- | --- |
| `GET /api/v1/test-tools/onboarding/scopes?platform=wsl` | Matching selected platform | Current actor's scopes and platform candidates |
| `POST /api/v1/test-tools/onboarding/scopes` | `platform`, `account_id`, `claimant_account_id`, `group_id`, optional `minutes`; `Idempotency-Key` header | Persisted scope, HTTP 201 |
| `GET /api/v1/test-tools/onboarding/scopes/{id}` | Scope ID | Actor- and platform-scoped progress |
| `POST /api/v1/test-tools/onboarding/scopes/{id}/close` | `{"version": 1}` | Closed scope with a new version |

Creation defaults to six hours (360 minutes) and accepts 1–360 minutes. The idempotency
key comes only from the header; it is not a JSON field. A repeated key must
refer to the same input. Closing uses the current version fence.
An active scope can be closed at any time. The closed scope's reopen button
prefills its accounts and group but requires explicit confirmation to create a
new scope with fresh proof; it never changes an old deadline or revives old
challenges or budgets. Migration 069 widens the database limit for new scopes
only; existing scopes keep their recorded deadlines.
Each scope also retains its original request default for idempotent retries:
an omitted duration on a pre-upgrade request still retrieves its original
30-minute scope, without extending it. New requests default to 360 minutes.
Ordinary customers receive HTTP 403. Another superadmin's scope and a valid
but unknown scope ID receive HTTP 404; a malformed resource ID is rejected by
the common HTTP wrapper.

The list shape is:

```json
{
  "items": [],
  "candidates": {
    "accounts": [],
    "groups": []
  }
}
```

Candidates are same-platform authenticated pool accounts and observed groups.
They are choices for an explicit scope, not website or group authorization.
The superadmin test page also reuses these group candidates for its current-role
read and native-window preparation selector. This avoids depending on the
business `/groups` collection, which correctly hides a paused controlled proof.
Candidates stay local to the test page and its grant dialog; they do not enter
the assistant or customer group collections. Selecting a candidate neither
restores a closed scope nor satisfies a native operation's binding, proof, role
or final submission checks.
Each scope projects `id`, `platform`, `actor_id`, `tenant_id`, `account_id`,
`claimant_account_id`, `group_id`, `robot`, `claimant`, `group`, `state`,
`version`, `expires_at` and `blocked_reason`. The three identity projections
carry `id`, `name` and `native_id`. Credentials, challenge secrets and arbitrary
native error bodies are absent.

## Proof And Connection Lifetime

Create an ordinary onboarding challenge through
`POST /api/v1/group-binding/challenges` with the matching `platform` and
`controlled_scope_id`. Control resolves its account, group and operator demo
tenant from the scope. No client-selected customer tenant, claimant or group
can widen that authorization.

The scope's six-hour maximum is separate from the challenge's 10-minute
lifetime. An unexpired scope cannot extend an expired challenge. If the robot
is already in the selected group, a fresh authenticated group message from the
selected claimant may directly prove identity and the current owner/admin role.
If the robot must first accept an invitation, the selected claimant must provide
fresh authenticated private proof before that acceptance, then independent group
proof after joining. Name matching, directory membership, creation of the scope
and merely viewing progress supply neither proof. The final confirmation rechecks the scope and current native
roles; resulting ownership and claim records retain `controlled_test` origin.

An active scope explicitly projects `human_verification_required=false`.
Only challenge creation and confirmation of its sealed incoming friend approval
or stale-request rejection may omit the website slider. Control resolves the
scope again from the website session and persisted challenge; browser flags,
account testing switches alone and an ordinary operator-demo claim confer no
exemption. Unknown, closed, expired, revoked or changed scopes reject submission.
The actual operation repeats its existing final checks. Login and normal
customer/operator onboarding still require their scoped slider tickets.
Approval retains its explicit confirmation. Rejection uses a separate
confirmation dialog when exempt, instead of treating candidate lookup as consent.
Private/group proofs, source admission, attempt limits, rate limits, origin,
CSRF and idempotency are unchanged. A missing projection field keeps the slider
for compatibility; there is no global disable switch or automatic fallback retry.

The incoming contact flow pins its fresh applicant to the selected pooled
claimant. Human approval establishes friendship only. Private proof from another
sender and group proof from another sender or group remain invalid. An expired
application is a separate rejection-only decision: a confirmed rejection ends
that challenge and requires a new application, without establishing identity.
Unknown approvals and rejections retain their original sealed operation for
read-only reconciliation even after scope closure.

An invitation requires independent private proof before preparation or an
attempt reservation. The incoming-friend gate and the onboarding `group_accept`
source gate remain independent. Invitation previews spend no attempt and
acknowledged acceptance does not establish group ownership. For an initially
absent robot, a fresh authenticated group message must subsequently prove the
selected claimant's current owner/admin role; final confirmation rereads those
roles. Closing the scope before the invitation's final submission fence prevents
the native acceptance and retains the failed attempt's ID and sealed history.

The persisted account epochs and versions fence both the receiving robot and
claimant. Reauthentication, credential replacement or another connection change
that invalidates those snapshots requires fresh authorization and fresh proof.
An expired or closed scope cannot be reopened by refresh, polling, another
actor or a replacement request key. Reauthenticate through the normal native
account flow when necessary, then explicitly create a new scope and challenge.
Never edit production account roles, epochs, proof rows or authorization with SQL
to make an old scope pass.

Ordinary customer challenges retain the non-pool sender restriction. A
`controlled_scope_id` is not a customer exemption and cannot be borrowed by
another actor, platform, group or claimant. Existing authenticated-message
challenges cannot acquire a scope later or be relabeled `controlled_test`.

An observed group may already exist in `native_groups` and `account_groups`
without a website binding. Its existing directory ID is reused by the proof.
If the same native group belongs to another website tenant, controlled proof
still requires the existing separate transfer review, including the source
tenant and binding version. The approved destination is this actor's own demo
workspace, with `controlled_test` origin. The transfer archives the old settings,
keeps source financial/history rows under their old tenant and starts the new
binding with clean, disabled settings.

## Closure And Independent Gates

Closing or expiry stops further use of the scope and invalidates its execution
authority. Saved group configuration, proof and operation history remain.
Restoring a paused binding in the same operator workspace requires a new scope
and fresh authenticated proof through the applicable path above. It retains saved settings, advances the binding
fences and records the new controlled proof without relabeling the old challenge.
Unresolved or unknown submissions keep their original operation IDs and sealed
requests for read-only reconciliation; they are never replayed under a new scope
or silently removed to free capacity. While an operation is submitting or
unknown, closing its scope preserves the challenge's persisted state and blocks
further proof and execution through the inactive scope. The pending operation
continues to retain its capacity reservation. Progress may display the scope as
unknown until observation resolves the original operation; that display does
not restore execution authority. Terminal challenge cleanup follows confirmed
reconciliation and never clears an unknown result to permit closure.
Closure does not delete friendships,
remove group members, reset the database or erase financial history.

Controlled bindings appear in `/user-center/groups` and `/groups` only for
their scope's actor while its execution authority remains active. Another
superadmin cannot expose them through an all-workspaces view or a tenant filter.
Closure removes them from those collections while retaining the owner's scope
progress, saved configuration and proof history.

Controlled proof does not open any source-specific production gate.
Manual, automatic, assistant and heatbot admission remain independent, as do
each platform's acceptance and usage budgets. This administrative surface is
not registered as a model Tool and grants no HTTP, Shell, SQL or native
execution authority to either AI consumer. `native_ready` and `ai_ready`
remain subject to their documented acceptance criteria.

See [customer onboarding](customer-onboarding.md) for the ordinary customer
proof path and [controlled Tools](controlled-tools.md) for separately admitted
native actions.

## Recorded Live Acceptance

### Controlled Applicant Fixtures

The administrative outbound surface may act as the selected applicant of an
active controlled onboarding scope. The parent challenge remains an ordinary
`incoming_contact` challenge for the receiving robot. The source identity comes
from the live scope, not a browser account parameter. Both accounts must retain
their authenticated management identity, current versions, connected receivers
and native role evidence in the one isolated group. The one-attempt window
targets only the receiving robot; the source account also needs its independent
outbound admission. This creates a real incoming sample for the normal wizard
and does not prove website identity or group ownership.

Private and group proof samples use separate one-attempt, at-most-five-minute
windows tied to the exact normal challenge text. The private step requires an
actual private conversation and event; the group step requires the normal
private proof to have been observed first. These windows use the existing
durable text ledgers and final submission fences. They do not authorize arbitrary
messages, media, a different target, another group or an extra attempt. Closing
the onboarding scope immediately blocks unused windows. Unknown sends retain
their original operation for reconciliation.

Normal incoming approval, authenticated receive events, independent private and
group tokens, current roles, human confirmation and transfer review still form
the claim. A generated test window or an outbound success receipt alone cannot
complete it. These fixture routes are superadmin-only and are not registered
with the assistant, heatbot or ordinary customer tools.

On November 11, 2026 (Asia/Shanghai), the v99 deployment completed the Chili
`already in group` branch in the isolated test group: one authenticated owner
message, normal proof association, the actor's confirmation and a versioned
cross-tenant transfer into that actor's demo workspace. The original settings
were sealed and archived; automatic management and chat remained disabled.

The actor then closed the six-hour scope early. Its proof became paused,
binding fences advanced, and the group disappeared from user-center
collections while its transferred ownership and evidence remained stored.
Both temporary account switches and native action windows were closed;
native reception continued. This was controlled-test proof, not ordinary
customer acceptance. New friend requests, independent private proof and WSL
were not exercised in this round. See the v99 live-claim section at the top of
[the test report](test-report.md) for operation IDs, timestamps and boundaries.

## Synthetic Validation

`internal/httpapi/controlled_onboarding_test.go` uses the existing
`newOnboardingFixture`, sealed synthetic decoded/source records and synthetic
native role port. Database cases use the existing serial `wsl_test` fixture;
they do not authenticate native accounts or operate real groups.

This candidate's HTTP wiring and synthetic tests do not constitute deployment
or real native acceptance. PostgreSQL integration execution must be coordinated
because each fixture rebuilds `wsl_test`. Run the scoped suite with
`WSL_INTEGRATION=1` only after the coordinator grants exclusive use.
Record actual executed checks separately from skipped database cases.
The additional contact/absence cases use the focused selector
`-run '^TestControlledOnboardingContact'`; they reuse the incoming-contact
fixture and add a synthetic port that explicitly distinguishes verified absence
from an unavailable role observation.
