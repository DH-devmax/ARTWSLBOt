# Scoped Controlled Onboarding

Status: accepted by the owner on 2026-11-10.

The normal customer flow continues to reject pool identities as customer proof.
A separate superadmin-created `onboarding_test_scopes` record may admit exactly
one authenticated pool claimant, different from the selected receiving robot,
in the same platform and the actor's own operator demo workspace. It is bounded
to one isolated group and at most 30 minutes. Creation does not require an
existing website binding. When the robot has not joined, the native role port
must supply verified absence; fresh owner/admin proof is required after joining.

Control owns scope creation, cancellation, account-version checks, native role
proof, and all execution. The existing customer wizard and protocol adapters
remain the sole onboarding path. `controlled_test` origin is persisted on the
challenge, ownership and claim; it cannot be converted into customer identity.
The database enforces origin consistency and immutable scope identity. A
challenge still expires after 10 minutes independently of the test scope.

The same day's later owner correction exempts only the live scope's challenge
creation and sealed single-contact approval/rejection from the website slider.
The handler asks control to revalidate the authenticated actor, own demo space,
platform and persisted scope; it never trusts a browser exemption flag. The
scope projection explicitly reports `human_verification_required`. Missing
projection fields retain CAPTCHA compatibility. Expired, closed, changed or
unresolved scopes reject submission. Native final fences, proofs, budgets,
origin/CSRF and explicit human confirmation remain. A slider-free stale-request
rejection has its own confirmation dialog. Login, ordinary onboarding and
independent demo claims retain CAPTCHA; no global test-mode bypass is added.

Manual, assistant, automatic-rule and heatbot execution retain their independent
production gates. Sealed work carries the scope ID and version; checks before
generation, delivery and final submission reject stale work without waiting for
cleanup. A shared append-only reservation ledger bounds native mutations to two
and model calls to two, with at most 0.02 USDT reserved per scope. This ledger
only limits the test; existing protocol, approval and gateway ledgers still own
execution, idempotency and charges. Unknown effects retain their original IDs.

The onboarding scanner handles expiry; no service or per-group worker is added.
Closing or expiry pauses controlled ownership, disables runtime switches and
cancels unsubmitted work. Saved configuration, financial history, already
established friendships and native effects remain. Reopening requires a new
scope and authentic private/group proofs. Cross-tenant movement still requires
the versioned transfer review and sealed configuration archive.

Implementation evidence: `internal/control/controlled_onboarding.go`,
`controlled_onboarding_lifecycle.go`, `controlled_execution.go`,
`internal/database/migrations/067_controlled_onboarding.sql`,
`068_controlled_onboarding_usage.sql`,
`internal/httpapi/controlled_onboarding.go`.

The administrative endpoints are not assistant or heatbot Tools. Neither AI
consumer receives scope creation or `native_debug` authority. Controlled proof
does not count as ordinary customer onboarding or raise readiness.

Owner amendment, 2026-11-11: newly created scopes now default to six hours
(360 minutes), also the maximum, superseding the original 30-minute limit.
Migration 069 widens the database deadline constraint and records the immutable
request default for compatible idempotent retries. Old deadlines and the
independent ten-minute challenge lifetime are preserved.
The operator may close a live scope at any time and explicitly create a new
scope from a closed record, with selectors prefilled but fresh proofs and
independent usage records. No existing proof, plan or budget is revived.
