# WSL Controlled Outbound Acceptance

This document records the earlier v25 upload-only run. The subsequent sender
and recall work is tracked separately in
[controlled group sends](native-controlled-sends-20261103.md).

This run addresses the owner's request to test the website's own upload,
send and recall paths in the isolated test group. Mobile demonstrations are
retained as read evidence and do not count as website sends.

## Frozen Scope

The candidate starts from the running `native-demonstrations-v24-20261103`
source and API image `57e983aeef74...`. It registers only the existing WSL
media upload adapter. The text sender remains unregistered. Decoder code,
message sealing, migrations, production admission and the Web image are unchanged.

The new explicit WSL `native_debug` actions are `upload_image`,
`upload_voice`, `upload_video` and `upload_private_image`. They are separate
from send actions. Their grant must contain only upload actions, and cannot
stage a message or dispatch a send. Package grants retain their existing
subscription and module checks. Chili does not inherit these actions.

Uploads retain live website authorization, authenticated account, current
connection owner, epoch, native group role, active isolated binding, deadline,
attempt budget and the existing final upload fence. A successful upload
persists only a sealed receipt and hashes. Inspection never uploads again.
No subscription, business module or assistant permission is enabled for testing.

## Checks

The first PostgreSQL regression exposed an invalid expired-window fixture.
Its timestamps were corrected without changing the production constraint.
The first container smoke expected an unregistered upload adapter; it was
updated to require a scoped `conversation_not_found` rejection for a synthetic
account with no conversation. Neither probe submitted a native operation.
The first temporary database later lost connections; its partial run is retained
and is not counted as a pass. The rerun uses an independent temporary database
with bounded WAL and more temporary storage.

The full Go race/sodium regression and the corrected container smoke passed.
The serial isolated PostgreSQL HTTP race rerun passed in 109.066 seconds.
Architecture extraction/check, repository and boundary guards, 52 tooling
tests, three renderer cache tests and seven unchanged Mermaid renders passed.
The exact-source trusted DeepSeek incremental analysis passed with one request,
27,601 reported tokens and zero unknown usage; its source hash is
`a301b1052aa6563fc9af6fec4e2284ac4c0a5f348277bad5e34b2e1c6102e178`.
This advisory model check is separate from real native acceptance.

## Native Boundary

The production database had one nonce allocator row, zero text attempts,
zero upload attempts and no active test windows at preflight. This proves
the current website has no recorded outbound attempts; it does not establish
whether the shared application key was used by an older sender or before a
backup restore. Historical nonce safety remains unresolved. No timestamp
or random nonce is introduced as a substitute for that history.

Actual test scope was at most one image, one short PCM voice clip and one short
MP4 upload, submitted sequentially with a total budget of three. The first
300-second grant used two attempts and was cancelled after the voice rejection.
The final, previously unused video attempt had a separate upload-video-only
300-second grant with budget one. Total usage remained three, with no retries.
Uploading is independent of sending.

## Real Results

The verified API image `29c84d47bb2e...` was delivered to existing Compose.
Only API restarted; Web, PostgreSQL and heatbot kept their container identities.
Environment hashes, resource limits, mounts and networks matched before/after,
and the immediately previous API image remains tagged for rollback.

| Attempt | Input | Native result |
| --- | --- | --- |
| Image | Original 128x128 PNG, 380 bytes | Succeeded; persisted `native_upload_received` and matching read-only inspection |
| Voice | Original one-second mono PCM WAV, 32,078 bytes | Failed; explicit `native_upload_rejected`, matching inspection, no retry |
| Video | Original one-second H.264 MP4, 160x96, 1,878 bytes, PNG cover | Succeeded; persisted `native_upload_received` and matching read-only inspection |

Uploads ran between 09:13 and 09:16 UTC on November 3, 2026. Both grants were
cancelled and the account action switch was restored to false. Testing mode
and the receive connection were retained. No subscription or group module
was activated. Three submitted attempts were consumed, including the rejection.

The current uploader collapses HTTP/business/rate-limit rejections into the
safe persistent `native_upload_rejected` code. This record cannot recover the
discarded upstream number, nor prove the specific reason the WAV was rejected.
The reviewed desktop uploader is not evidence that its voice-upload format
has passed; that contract still needs investigation. Do not mark voice sending
or upload compatibility as accepted.

No website text, mention, quote, media send, private send or recall occurred.
Zero new send operations were recorded. Historical nonce safety and the
independent controlled recall execution entry remain unresolved. The feature
catalog's complete-send acceptance is therefore unchanged. Native and AI
readiness remain false; this run does not establish production or scale readiness.

At 09:19 UTC the final live API/browser check passed: all four WSL/Chili
receivers were connected with fresh observations, all account action switches
were false, no active test window remained and both bindings kept management
and AI disabled. Catalog controlled counts remained WSL 27 / Chili 4.
The browser showed the existing video quote and manual-operation separation,
with zero page errors and zero native writes.
