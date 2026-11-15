# Native recall notice reads

This is a superadmin, explicit read-only message-center dialog for the selected
account and conversation. It does not provide a recall mutation or enable a
business consumer. Read coverage and controlled/production acceptance remain
separate in the native capability catalog.

## Reviewed inputs

- WSL: fixed desktop 2.8.3 business SDK `7_14` (`sysMsg`) and `7_15`
  (`sysMsgs`, timetag, offline/roaming type). Only message-delete types 7 and 8
  are read. The original client/server message identifiers, operator, sender
  and time are projected. `7/3` responses and other delete types are not
  interpreted as recall notices. Existing known push envelopes may be unwrapped
  once; unsupported layouts fail closed.
- Lajiaoliao: the owner-produced 2101/`MsgFrom=200` notification matches the
  pinned OpenIM `sdkws.RevokeMsgTips` definition, including its original
  `sesstionType` spelling. The content wrapper contains a JSON string `detail`.
  Duplicate/unknown fields, invalid UTF-8, wrong session type and incomplete
  identity/sequence/time are rejected. App evidence confirms the native format,
  not this new reader's real acceptance.

`internal/nativewire/recall.go` and `internal/lajiaoliao/recall.go` own these pure,
bounded readers. They do not import control, storage or native action code.
The former limits a collection to 128 notices; the latter uses the existing
protobuf dependency and copies projected strings before clearing its buffer.
No generated type fork, unsafe string or plaintext pool is added.

## Scope and bounds

`GET /accounts/{id}/conversations/{conversation}/recalls` requires the existing
authenticated superadmin and immutable account workspace. A recent sealed
projection supplies the trusted native group target or both private endpoints.
Notification actors never supply group ownership or action authorization.

The query examines only the selected account's sealed source events within the
seven-day retention window, newest first. Each page scans at most 1000 source
records, stops at a four-MiB scan budget (one current record may cross it), and
rejects individual sealed records larger than 256 KiB. It stops after an event
brings matches to 100; a single WSL event may contain up to 128 entries and is
never split or silently lost across an event-ID cursor. An explicit next-page
button advances by source ID. The browser retains at most 500 notices.

The response reports scan count, sampling time, window and cursor. Empty pages
do not claim that no earlier notices exist. No source event, decoded state,
message text, ACK or recovery cursor is written. Original messages stay
available as retained history; a notice links by identifier and does not claim
to prove deletion on every client. Raw bodies, sealed credentials and unrelated
conversations never leave the server.

## Verification boundary

Synthetic protocol and isolated database regressions verify layout, malformed
input, source-buffer ownership, group/account filtering and retained message
history. Mocked browser checks verify explicit reads, escaped identifiers,
mobile layout and absence of native writes. On 2026-11-01 a manual read-only
check of retained events also exercised both new readers: WSL group notices
parsed successfully, and the Lajiaoliao test-group notice parsed and matched.
This confirms native support and retained-input compatibility. The bounded WSL
page contained no notice matching the isolated test group; it does not establish
that the group never produced a notice. Real account-scoped HTTP/dialog display
remains a separate owner-directed read acceptance after delivery.
`native_ready` and `ai_ready` remain false.

`go run -tags sodium tools/native_recall_probe.go` is a manual local diagnostic,
not an automated regression or deployed service. It uses a read-only database
transaction, a seven-day window, at most 1000 events/four MiB per platform, and
existing sealed directory mappings for the owner's named test groups. Output
contains aggregate counts and known route labels only. It never prints raw
notices, identities, message identifiers or credentials, and does not perform
any native operation. App support, candidate integration, retained-input checks,
live dialog acceptance and production acceptance are separate evidence.
