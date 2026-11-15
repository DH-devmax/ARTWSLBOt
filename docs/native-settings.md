# Desktop account settings and group nicknames

Owner scope update, 2026-11-02: product editing includes native nickname,
implemented avatar upload and necessary group nicknames only. Signup, password,
phone, personal details, privacy, notifications, secondary security code and
real-name certification are excluded, not deferred. New submissions and
unsubmitted execution are closed; prior submitted operation queries and
read-only reconciliation remain available. The contracts for excluded fields
below are historical evidence; see `platform-native-scope.md`.

This is a manually confirmed control path, separate from the still-closed native
message/moderation executor. Global `native_ready` and `ai_ready` remain false.
Synthetic compatibility is not real-account acceptance.

## Evidence and scope

The fixed official desktop 2.8.3 settings/group dialogs and business route map
were reviewed locally. No desktop runtime, mobile adapter, external URL proxy,
or new account connection is introduced.

| Field | Reviewed desktop request | Current scope |
| --- | --- | --- |
| Native nickname | `settings/self-nick-name`, `nickName` | Superadmin, own logged-in WSL account |
| Group nickname | `group/set-member-nickname`, `groupId`, `userId`, `nick` | Self or permitted lower-role member in a bound authorized group |
| Search settings | `settings/p2p-search`, `searchableAccount`, `searchableMobile` | Excluded; historical reconciliation only |
| Friend approval policy | `settings/p2p-verify`, `policy` | Excluded; individual group-management friend approval is separate |
| Group invitation policy | `settings/group-verify`, `policy` | Excluded; invitation actions are separate |
| Voice/video call preference | `settings/audio-video-chat`, `audioVideoOpen` | Excluded; media sending is separate |
| Avatar | Upload line → `user/image-check`, `assetId` → `settings/avatar`, `id` | Superadmin, manually confirmed JPG/PNG upload |
| Password | `verify/sms`, `verify/verify` then `settings/password`, `verify.pwd.passwd`, `newPassword` | Excluded; use official App |
| Phone | `verify/sms` / `verify/verify`, `verify/sms-anon`, `settings/changePhone` | Excluded; use official App |
| Secondary security code | `user/get_safe_code`, `user/set_safe_code`, six-digit `code` | Excluded; no new status or setup |
| Legal identity | `finance/real-name-authentication/get_real_name_info` | Excluded; use official App |

The website robot name, original account nickname and per-group nickname are
different fields. Saving one never silently changes the others. Names use the
desktop's 20 UTF-16-unit input limit; control characters and surrounding spaces
are rejected. An empty group nickname can clear a card; a personal nickname
cannot be empty.

Basic account reads project only subject ID, nickname and avatar reference.
Chili uses the IM identity route without privacy, notification or complete
profile pre-reads. Historical excluded-operation reconciliation explicitly
selects its original field reader; it cannot submit a new change.

### Security extensions (2026-11-01 candidate)

The fixed desktop route map and the official `user_extra.proto` define
`get_safe_code` / `set_safe_code`. The desktop UI redirects first setup to mobile;
this is not evidence that its server route has passed a real website test.
The platform allows one explicitly confirmed six-digit first-time setup through
the existing durable security-operation path. A fresh native status read must
report `set=false` immediately before the final control fence. An already-set
code is refused; change/reset/enable operations are not offered. Input is
ephemeral, the stored operation contains only its keyed fingerprint, and the
completed verification flow has no sealed payload. A lost response remains
unknown and cannot be retried by the browser or recovery.
Migration 039 extends only the allowed operation kinds; previous migrations are
unchanged. Compatibility and unresolved submissions must be checked before
deployment.

The reviewed finance service supplies real certification status. The
`settings/legal` profile-document route is deliberately not used as certification.
`native-security-status?section=identity` returns only the known audit enum and
certified/expired flags. It never returns or stores name, document number,
certificate times, face limits or tokens. Unknown enums, missing or null required
fields are errors, not successful certification. `section=secondary` reads only
the three code-status flags; an unavailable identity service cannot block this
independent read. Both use the existing account read lane, current identity,
epoch, deployment and website authorization checks.

The official application flow includes identity-image upload and a native face
SDK with opaque server tickets. This candidate does not implement that flow and
does not offer a misleading identity submission form. Real setup/certification
acceptance is pending and requires the human account holder; synthetic tests
never send SMS or change a real security credential.

## Flow

`NativeSettings.vue` → authenticated/CSRF API → control operation record →
gateway's existing serial account business lane → fresh desktop projection →
control final fence → one signed HTTP request → durable outcome.

The gateway uses the current sealed session, persisted business request-ID
allocator, account cooldown, bounded account queue and two-request global limit.
No read or write runs on receive, durable commit or ACK goroutines.

Each request contains one allowlisted change, original projection digest,
account epoch/version and an idempotency key. Control persists the sealed
request before dispatch. Under the final fence it checks the live website
session, superadmin role, immutable platform/workspace, account identity,
credentials, epoch/version, deployment version and a read less than five
seconds old. The single HTTP submission and bounded response consumption have
a two-second deadline. SHARE row locks allow ingress FK KEY SHARE acquisition;
the preliminary per-account admission transaction uses UPDATE for serialization.
The upstream desktop mutation has no reviewed conditional-version parameter.
The fresh digest prevents stale website submissions; it cannot provide a global
CAS against changes another native client makes after the final read.

Group modifications additionally require an active management binding to the
account (or same-group chat account), enabled management and account native
actions, a current WSL/cards subscription, native membership/transport mapping,
current self/target roles and an explicit group nickname-mode observation.
The current conservative policy rejects any nickname-locked group; self can
otherwise edit self, an admin can edit members, and an owner can edit nonowners.
It does not grant authorization from a native directory snapshot or enable the
existing native action switch. A missing binding or still-closed native action
gate produces an explicit refusal. Tenants do not receive shared account settings.

## Results and recovery

`preparing → submitting → succeeded / failed / unknown`; submitting is durably
marked before the final fence. A refusal before HTTP is failed. Transport loss,
bad response or failed local commit after attempted HTTP remains unknown.
Neither refresh, duplicate POST, process restart nor reconciliation resubmits.
Restarts mark unfinished preparing as failed and submitting as unknown.
Pending/unknown records block other account settings, removal and affected group
migration. Text dispatch also refuses unresolved setting operations.

Status can be queried by original key even when a POST response is lost.
The browser persists only that key under the full user/account/group view scope.
Reopening also queries the same actor/account's pending records, so losing the
browser key does not hide a durable unresolved submission.
Explicit read-only reconciliation compares current native values to the original
sealed request. Equality resolves with `desired_state_observed`, which states
that the desired state exists, not that this request executed exactly once.
Inequality remains unknown. Reconciliation revalidates current authorization.

The forward migration `021_native_settings.sql` adds a platform-only forced-RLS
operation table. Requests are encrypted and cleared after seven days. Minimal
terminal metadata is cleaned after 90 days; unresolved metadata remains for
review. Account removal clears its terminal records via FK cascade while
unresolved submissions block removal. Audits contain operation type, state and
safe code only, never nicknames or privacy values.

Real group/card/profile changes remain untested in this release; no automated
signup, login, SMS or real group action is part of the tests.

## UI and release

The selected-account dashboard's `账号资料` opens personal settings. The message
center's native group directory → members → `修改我的群昵称` / `修改群昵称`
opens a scoped group edit. Merely opening either view performs reads only.
The server remains the authority for plan, binding, role and native-action gates.

The current account dialog has profile and supported avatar tabs only. It shows
the website robot name independently from the editable native nickname, native
identity and current avatar. Avatar changes use the dedicated manually
confirmed flow below; the historical privacy and security flows are not
product controls. An expired native session offers a manual
re-login entry from the account dialog; opening this entry does not itself
submit native authentication.

Only changed, supported fields can be saved, one operation at a time, after a
plain-text old/new value confirmation. The browser checks the same UTF-16 name
limit without silently trimming input. Missing profile data and unsupported
avatar capabilities disable their corresponding controls. The server still
validates every request.

Switching the dialog's tabs keeps its in-memory draft. Closing or manually
refreshing a dirty form requires a discard confirmation; failed saves keep the
input. A successful save refreshes that field while preserving other edited
fields. Account/group/actor scope changes cancel reads and confirmations and
clear the prior view; delayed responses cannot enter the new scope.
Profile and pending-operation reads are independent. A failed profile read
cannot hide an unresolved operation, and failed pending checks prevent saves.
Only operation and opaque verification-flow IDs use session storage; neither drafts nor native settings
are persisted there. Server pending queries recover when that storage is absent.
Unknown outcomes are never resubmitted by refresh, reopening or reconciliation.

API/Web and forward migration 021 were deployed in the explicitly authorized
2026-10-28 maintenance window; see `docs/test-report.md`. Existing credentials
were preserved. The observed account already had `im_kicked` before deployment
and the new profile read returned `native_reauth_required`; the release did not
attempt a native login or treat saved authentication as an available connection.

## Avatar and account security (deployed 2026-10-29)

The owner explicitly approved the API restart and matching Web/022 deployment.
Container health, migrated isolation and preserved business/session fingerprints
were verified; no native upload, SMS, password/phone change or login was executed.
Real-account acceptance remains pending. See `docs/test-report.md`.

### Current avatar preview (deployed 2026-10-29)

The settings dialog previously displayed a generic user icon and “已设置”
instead of loading the native avatar. The deployed dialog shows the current avatar
in the identity header and avatar form; selecting a replacement adds a separate
“待上传的新头像 / 尚未保存” preview. Only explicit confirmed submission writes
the avatar. Missing settings or unsupported asset references never invent one.

The existing settings GET returns an `avatar_url`, derived from that same
snapshot without another native settings read. It is an encrypted reference
valid for ten minutes, scoped to the website actor, workspace and account.
The superadmin-only `GET /accounts/{id}/native-avatar/{reference}` revalidates
the live website session, account identity/epoch/version, sealed credential
fingerprint and deployment before download and again before delivery.
The fixed credential-free image line, existing two-download limit, ten-second
deadline, 1 MiB avatar limit and image validation are reused. Arbitrary URLs,
redirects and browser-provided asset IDs remain prohibited.

Images are read into bounded memory with `no-store`; no image, new settings
snapshot or reference is persisted. The frontend reuses `MessageImage`'s scoped,
cancelable blob lifecycle. Scope changes clear previews and revoke blob URLs.
Expired references require rereading settings. Synthetic fixtures verify this
implementation; no real account avatar was read, uploaded or changed by its tests.
The owner authorized default verified local delivery on 2026-10-29. Matching
API/Web images were deployed, health and preserved session/business fingerprints
verified; see `docs/test-report.md`. Real-avatar acceptance is recorded per
platform and per operation in the feature catalog, separately from these
synthetic fixtures.

`NativeSecurity.vue` uses `POST /accounts/{id}/native-security-operations`;
`GET /accounts/{id}/native-security-flows/{flow}` recovers safe step/expiry only.
Both are superadmin-only, account/workspace-scoped Cookie/CSRF endpoints.
The old generic setting endpoint cannot invoke security steps. Passwords,
entered SMS codes, captcha answers and image bytes are request-local only.
Control stores a keyed, account-scoped fingerprint for idempotency, never a
plain password/OTP digest or sensitive operation body.

Avatar input is capped at 1 MiB JPG/PNG, 4096 pixels per side and 4,194,304
pixels total. Go decodes and center-crops a metadata-free 256×256 JPEG in
bounded memory. Multipart fields are `file`, `thumbnail`, `asset_type=1`,
`use_for=3`, `width=256`, `height=256`, with the saved JWT in the `token`
header. The reviewed signed desktop bootstrap field 8 fixes the upload line
to `https://authupapi01.hfjtqy.com/v1/assets/userupload`; no browser URL,
redirect, cookie jar or alternate destination is accepted. Upload, image-check
and final assignment each use a fresh submission fence, at most three calls.
There is no disk media cache or automatic retry. Only the returned asset ID
is retained for a possible read-only unknown-result comparison.

Password stages are `password_sms → password_verify → password`: SMS purpose
`VERIFY_FOR_RESET_PASSWORD`, verification `{ty,key,answer}`, then the desktop
form's old password plus new password. Phone stages are
`phone_old_sms → phone_old_verify → phone_new_sms → phone`: old-phone purpose
`VERIFY_FOR_CHANGE_PHONE`, verification certificate, new-phone anonymous SMS
purpose `VERIFY_FOR_RESET_PHONE`, and final `{phone,newDeviceKey,newDeviceCode,cert}`.
Certificates/keys/phone come from the sealed server flow, never final browser
parameters. Old-phone validation is required even though an old API comment
describes a temporary exemption. The desktop 2.8.3 page has no phone-edit UI;
this request is sourced from the official published API schema and remains
unaccepted on a real desktop-authenticated account.

Forward migration 022 extends the operation kind constraint and adds
`native_security_flows`, forced RLS/platform-only. Flow ownership binds actor,
account, workspace and account epoch. Successful steps, expected stage and
expiry are rechecked; a restart never resends. Challenges expire at the smaller
of ten minutes and the bounded native expiry (five-minute fallback if absent).
Sealed flow keys/certificates are cleared on terminal results and expired rows
are removed by the existing retention task. Starting SMS is rate-limited per
account for 60 seconds. Full phone values exist only inside the short-lived
sealed verification flow, not history, audit, browser storage or logs.

Successful password/phone modification changes the account to
`reauth_required`, increments epoch/version, disables `connection_enabled`,
and disables/revises its receiver in the same outcome transaction. Its old
business/receive session cannot keep being used; a user must explicitly log in
and enable reception again. Unknown
security outcomes remain blocked for manual review. The app does not probe a
password by logging in, compare a masked phone as proof, or auto-retry a write.
The global native/AI readiness gates remain false.

The proxy's larger body limit is restricted to the new security POST path.
Other endpoints retain their 64-KiB and 15-second limits. API/Web must be
deployed together after migration/restart review; frontend HMR alone cannot
activate the new API.

## Chili Basic Profile (2026-11-04)

The manual account dialog edits the native nickname and avatar only. Basic
reads use `/user/get_users_info` for the authenticated self ID. The fixed SDK's
`GetDesignateUsersResp.usersInfo` must contain exactly that one user; the
different `/user/find/full` response retains its own `users` contract.
Malformed or unavailable privacy, full-profile and push settings no longer
block this form.
Historical non-basic operations retain their existing reconciliation reads.

Desktop 1.0.3 `App-24552bef.js` uploads a cropped image with `uploadFile`,
then sends `faceURL` and the self `userID` through `/user/update`. OpenIM SDK
3.8.3 `internal/third/file/upload.go` supplies the multipart contract. Our
avatar path normalizes a bounded JPG/PNG to a 256-pixel JPEG and permits one
part, with no upload resume or automatic retry. Upload initiation, signed
transfer/completion and final profile update use at most three existing
control fences. Credentials are never attached to the signed storage request.

The actual avatar URL contributes to the settings digest but is private in
the snapshot. The browser receives a ten-minute sealed reference bound to
actor, platform, account, epoch, version and credential digest. Resource
access checks permission both before and after fetching and shares the
existing two media slots. No background image task or database migration
is added. Unknown results retain the operation ID and sealed upload receipt;
reconciliation compares the actual current avatar URL, never the generic
`configured` marker.

Gender, birthday, signature, region, notification, privacy and account
security editing are absent from this basic dialog. Historical IDs and
audit records remain intact. Only a superadmin may edit pool-account
profiles; the tenant assistant has no account-profile tool. Real native
write acceptance is recorded separately from synthetic regression results.

Chili avatar control fences allow eight seconds per phase, with an extra
second of operation/session validity required before submission. Other
settings retain their two-second fences. A final profile update can start
only after the completed upload reference has been sealed into the original
operation. If upload ends without that reference, the result is
`lajiaoliao_avatar_upload_unconfirmed`, with no final profile update.
It is never retried automatically.

The Compose one-shot `reconcile-avatar-upload` command settles only an old
`unknown` record whose sealed request still holds the original upload UUID,
original actor/account/epoch and exact avatar kind/digest. It checks the
current authenticated website session and current account scope separately.
Testing-switch epoch rotation does not invent a successful update.
Interrupted submissions, changed identities and an already recorded final
avatar reference remain unresolved. This command makes no native request;
it cannot retry an upload or profile mutation.

On 2026-11-04 the owner's current image was uploaded and set successfully.
The new JPEG was read through the scoped endpoint and decoded by the browser
at 256 by 256 pixels, with the nickname unchanged. The exact read-only
IM-object-to-CDN hop is documented in `lajiaoliao-outbound-20261104.md`.
