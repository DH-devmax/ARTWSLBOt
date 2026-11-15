# Native Conversations And Decoder

As of 2026-10-29. Read-only projections plus a separately gated manual-command
surface; not native/AI release acceptance.

## Scope

- Superadmin-only **消息中心**, scoped by selected workspace/account. Ordinary
  users and the restricted database role cannot read shared private messages.
- **原生群组** lists current account membership snapshots. It never creates
  SaaS group authorization, ownership, member mappings or action permissions.
- **对话消息** lists authenticated app messages observed during the last seven
  days, including incoming private messages and self-sent synchronization.
  Version 7 also displays separately labeled read-only transport notifications.
  This is not the server's complete contact/session/history directory.
- Group names match only when business group ID AND transport team ID match.
  Private conversation keys contain both business and transport peer. Header
  authentication is not evidence of a current native member/admin role.
- Projections never send or authorize actions. The separate manual text-command
  surface below is acceptance-gated. There is no AI dispatch, private management,
  group mutation, signup, SMS or password login in this feature.

## Pipeline

The existing receiver persists raw ciphertext and handles ACKs independently.
It never waits for this decoder, directory HTTP, models, or frontend requests.
One coordinator interleaves bounded candidates across accounts. Four Go workers
share a 16-item queue and a separate four-connection PostgreSQL pool; decoder
queries cannot occupy receiver/control pool connections. A successful raw
ingress commit wakes the coordinator through a coalesced, nonblocking signal.
Worker completion also wakes it to refill available capacity. Startup and a
five-second recovery check reconcile durable rows and expired leases; this is
not a message-read frequency limit. A lost signal never deletes durable work.
These are settings, not latency measurements or guarantees.

PostgreSQL claims have event uniqueness, owner and a 60-second lease. Expired
claims recover after restart. Codec upgrades reproject retained inputs but
cannot steal live claims or downgrade newer results. Commit rechecks account
epoch, deployment version, owner and original retention deadline. Message
ordering follows durable event IDs, never worker completion order.

The codec ports pinned AstrBot `a672ef9`: canonical Base64URL, strict JSON
wrapper, protobuf envelope, original libsodium eight-byte-nonce
ChaCha20-Poly1305, bounded LZ4 and authenticated routing headers. Version-one
packet-signature metadata is authenticated but not used as identity/permission.
Desktop 2.8.3 protobuf `Timestamp` and historical int64 timestamps are supported.

The pinned desktop project root also authenticates app-message envelopes. New
offline profiles explicitly include `message_key`. Existing 2.8.3 profiles use
that versioned project-root contract if the optional field is absent; no
multi-key/algorithm decryption fallback is attempted.

Text, mentions and quote text are supported. Codec version 5 additionally
projects WSL images, sticker flags, embedded forwarded chat records and
sender/target avatars, personal contact cards (type 13), and video metadata
(type 3, including embedded forward entries). Click-to-load MP4/WebM previews
are bounded at 16 MiB; covers use the reviewed asset `.jpg` variant.
External video URLs are not exposed or fetched. Group avatars
come from the same account's sealed directory and require an exact business
and transport group match when decorating a conversation. The separate bounded resource reader and remaining
acceptance are documented in [native-media.md](native-media.md).
Version 7 adds bounded display-only projections for authenticated group notices,
announcements, tips, extension, game and red-envelope message classes; it adds
no game or financial operation. Images above the preview policy retain metadata
and an unavailable-preview placeholder; resource limits remain unchanged.
The pinned client's four-field aad=0 group-system notifications have no AEAD
authentication. A separate notice reader validates their exact structure and
header consistency; `OpenMessage` still refuses them as authenticated messages.
Both these records and SDK type-5 notification labels require a unique group
in the same account's sealed directory. Their projection uses a generic sender,
empty chat text and an explicit transport-only notice marker. Missing/ambiguous
directory scope remains unsupported. Such records never authorize an action,
become quote evidence, update native roles or dispatch business work.
Unknown authenticated app formats retain metadata as **未支持类型**.
Native non-app notifications do not create guessed conversations. Unknown signatures
and invalid authentication remain failures with sealed raw evidence. An app
failure does not disconnect a valid transport; unknown transport boundaries
still pause it.

Fixed-schema desktop multi-port login, offline system, directory, read-marker
and receipt notices are retained separately. They never execute contents or
advance the global recovery cursor.

The 6→7 compatibility correction reclaims only failed/unsupported version-6
projections; already decoded version-6 rows keep their payload and completion
time. Recovery uses existing claims, owner/epoch/version/retention fences and
four workers. This is not an increase in readiness or a performance release.

## Directory And Health

Only fixed native route `/v1/group/get-group-list`, `{"v":"0"}`, is added.
A background worker coalesces reads per account, with a ten-minute refresh
window, thirty-second manual debounce, at least sixty-second retry delay and
`Retry-After` handling. Pages never repeatedly scan upstream/member rosters.
Reads share an account gate with IM-token renewal, including session load and
credential commit. Failure retains the previous sealed, timestamped snapshot.
An old snapshot never authorizes a native action.

Superadmin/account/workspace guards cover:

- `GET /api/v1/accounts/{id}/conversations`: scene filter, 50 conversations/page
  (maximum 100), native directory separately paginated at 100 groups/page.
- `GET /api/v1/accounts/{id}/conversations/{key}/messages`: 50 messages/page,
  opaque conversation key and string event-ID cursor. Optional `ids` retrieves
  at most 64 exact event IDs in this conversation; it cannot combine with
  `before`, broaden account scope or return expired projections.
- `GET /api/v1/accounts/{id}/decoder-health`: account backlog, failure stages,
  oldest input, latest completion, per-account P95/latest 1000 successful finite
  nonnegative samples within 24 hours from the same shared monitor as the
  dashboard (including its observation timestamp and freshness), plus
  shared pool occupancy and separate receiver/directory state.
  Failure totals explicitly use seven days; separate recent-hour counts use
  original receipt times, and transport-notice counts are display-only.
- `POST /api/v1/accounts/{id}/directory-refresh`: durable merged read request,
  returns 202, no arbitrary native route parameter.
- `GET /api/v1/accounts/{id}/events?view=messages`: scoped SSE notifications,
  decoded event IDs and safe health metadata, never message bodies.

## Live Delivery And Bounds

Native WebSocket receive remains continuous. It is not driven by browser polling.
Decoder completion emits a process-local notification only after its projection
write succeeds. The shared bus carries account/event IDs, not ciphertext,
plaintext or credentials. It admits 16 subscriptions globally, two per website
user, with a 64-item nonblocking queue per subscriber. Slow browsers cannot
backpressure native ingress or the decoder. These are initial resource limits,
not a multi-user capacity acceptance result.

SSE coalesces notifications over 500 ms, light receiver updates follow its
existing one-second observer, and decoder aggregates refresh at most once per
five seconds when dirty. The client coalesces conversation-list reads over
three seconds and fetches exact IDs for the selected conversation. It deduplicates
and orders by durable ID, including late completion of smaller IDs; it never
uses the maximum completed ID as a loss-prone cursor. Health windows and
timestamped metrics still require aggregation; streaming does not mean
recomputing all database counts for every packet.

Both initial subscription and queue overflow request an explicit snapshot.
Each stream reconciles against the database every 30 seconds and reconnects
after at most 100 seconds; the browser uses bounded 1–30 second retry backoff.
Frame sequence numbers are connection-local, not durable resume cursors or
an exactly-once delivery guarantee. Missed notifications recover through
authorized HTTP snapshots/pagination, not native re-login or replay.

Each delivery rechecks the live website session, user, workspace and resource
access before and after reading. Revoked or unavailable access closes the
stream with no resource result; the client clears displayed content and aborts
late reads. Account changes abort the old stream and guard late responses.
Hidden tabs suspend subscriptions. Proxies disable buffering only for the
specific SSE routes; ordinary API timeouts are unchanged.

The selected thread retains at most 500 messages in the DOM. Loading older
history switches to a stable history view; further pages retain the oldest
500 loaded rows. “返回最新消息” returns to live view. All content remains
available through authorized seven-day pagination, not an unbounded browser
cache. No full desktop client, browser automation or new runtime service is
required by this path.

Successful historical decoding never makes a blocked socket appear healthy.
Decode P95 includes sealed-source reading and app decoding but excludes final
projection commit; shared worker samples include commit. Neither metric is
receive-to-durable or model latency.

Preview limits: 200 Unicode characters per conversation; displayed message
8192, quote 1024, mentions 32, with explicit truncation marking. Original
encrypted evidence is unchanged. Vue renders plain text, not `v-html`; remote
images/stickers load on explicit click through scoped resource routes; visible
avatars load lazily. No caller-specified remote URL is loaded.
Draft/cache keys contain no message bodies.
Manual drafts remain in component-owned memory, not browser persistent storage.

## Manual Text Commands

The message center has a scoped composer, but production text sending is still
closed. `Server.nativeTextTestPort` is nil in all production assembly; only
synthetic acceptance tests inject a sender. There is no environment switch,
public route or browser parameter that registers a real sender.

All following routes are superadmin-only and account/workspace scoped, under
`/api/v1/accounts/{id}/conversations/{conversation}`:

- `GET /sending`: safe capability, test-window and own recent-operation state.
  An unbound observed group is not a send target. WSL private text/image use a
  separate explicitly targeted test window; Lajiaoliao/chat-pool remain unavailable.
- `POST /text-operations`: persist a sealed text intent, no socket write.
  JSON permits `text`, `test_window_id`, `account_epoch`, `binding_epoch` and
  optional `reply_to` / `mentions`. A 3000-byte UTF-8 body limit leaves room
  inside the existing 4096-byte application envelope; components must also fit.
- `GET /text-operations?key=...` or `GET /text-operations/{operation}`: recover
  own state after an HTTP response loss, never resubmit or expose sealed content.
- `POST /text-operations/{operation}/dispatch`: call the existing control service
  once; it rechecks session, subscription, group, epochs, native role, receiver
  lease and isolated test window. An unknown result remains unknown.
- `POST /text-operations/{operation}/reconcile`: query existing exact echo
  evidence; no native write, login, directory refresh or resend.

Recovery also has account-scoped routes under
`/api/v1/accounts/{id}/native-text-operations`: GET by original `?key=...` or
`/{operation}`, and POST `/{operation}/reconcile`. The composer now uses these
for inspection/reconciliation. They recheck the website session, superadmin,
account/workspace and original actor, but do not need the current binding or a
retained conversation projection. Unbinding, migration or seven-day message
expiry therefore cannot hide the minimal operation status. These routes expose
no body, route or credentials and have no stage/dispatch endpoint. Missing
retained echo evidence remains unknown; recovery never extends message retention.

Targets come from server-decrypted retained projections and actual platform/group
bindings, not a browser-supplied native ID, tenant or route. Historical projections
are lookup inputs only, never current permission evidence. The generic production
operation endpoint stays closed. `native_ready` and `ai_ready` stay false.

`reply_to` is a canonical positive event-ID string, not a native server ID or
caller-provided quote body. It must resolve to a decoded, retained text message
from this account, group and deployment. Stage seals its ID and source digests;
dispatch and the final locked write fence recheck retention and exact evidence.
The current connection independently authenticates the raw source, group route
and original sender against the fresh complete roster. Missing, changed, expired,
cross-scope or oversized quotes fail closed, never fall back to plain text.
Original quote text is not duplicated into another long-lived record.

`mentions` contains at most 32 existing typed components (`uid`, `name`, `start`,
`end`). UTF-16 ranges must match the exact body, identity and name must match
the current complete member roster, and each UID must be explicitly allowed in
the isolated test window's targets. These restrictions are checked again at
submission. The production UI still offers only pure text; this batch supplies
the durable component contract, not a completed quote/member-picker interface
or permission to send to real accounts.

The composer confirms the group before staging/dispatching. Enter adds a newline;
Ctrl/Meta+Enter uses the same confirmation and ignores IME composition. Drafts
are keyed by website user, tenant, workspace, account and conversation, held only
in a bounded 32-entry in-memory map and cleared when the website user changes.
View changes/unmount invalidate continuation even if the user returns to the
same account before a delayed response arrives. Already-submitted effects cannot
be cancelled by switching views. No optimistic bubble pretends delivery.

Lost responses keep their original request key and require explicit inspection.
Queued recovery requires another confirmation; unknown/running results block a
new send. Inspection/reconciliation never resubmits. Success says only
“原生服务端已接受，不代表成员已送达或已读”. The UI cannot open real sending until the
nonce-history, trusted current native permissions and isolated acceptance gates
in `native-foundation.md` are satisfied and a reviewed production port is wired.

For private text/image, the retained sealed conversation identifies only a
candidate peer. The selected human grant supplies the independent isolated
group and binding epoch; it requires `send_private_text` / `send_private_image`
and that peer's explicit target ID. The current owner rechecks the peer against
a fresh full roster and friend list, uses session 1 / transport 7/1 and matches
private echo evidence. Group grants cannot authorize private sends. Private
quotes, mentions and other media are rejected. Upload receipts, histories and
status lookups are conversation-scoped; tenant RLS and the group assistant
exclude private operations. See `native-media.md` for migration 031 and bounds.
Production ports remain nil for both scenes.

## Retention And Remaining Work

Migration 015 is additive. Tables are platform-only; projections cascade from
raw events and accounts. Both workers and queries exclude content older than
seven days before the minute-level cleanup runs. Late workers cannot recreate
expired content. Removal previews count both new tables; account deletion
cleans them transactionally. Directory metadata follows account lifecycle.

Local diagnostic/backup artifacts are confidential, mode 0600 and Git-ignored.
Remove them before their source retention deadline; backup restoration must
not reintroduce expired plaintext.

Not yet accepted: full image/audio/video/file/sticker schemas, unknown
signature variants, complete server private-session/history synchronization,
target Linux 8-core/32-GB 100/s sustained/burst/24-hour/fault tests. No fallback
accepts unauthenticated text. Native and AI readiness remain **false**.
