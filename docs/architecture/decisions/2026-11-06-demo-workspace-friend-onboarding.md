# Operator Spaces And Friend Onboarding

Owner approval: 2026-11-06, dual-platform friend verification and shared user center.

`tenants.space_kind` distinguishes customer resources from an operator's own demo
space. The real platform account remains a superadmin with no tenant membership.
Migration and user creation provision one space transactionally; GET endpoints
only read it. Existing customer ownership, subscriptions and historical charges
remain unchanged.

The system-only plan is permanent and commercially unlimited. It is not an order,
paid period, role template or a grant to bypass source admission. Physical pool
capacity, native role, binding, version, deadline, idempotency and rate fences
still apply. Demo bindings start with automatic business and chat disabled.

Human use retains `operator_demo / platform_demo`. Explicit service execution for
a demo-owned group uses `workspace_demo / platform_workspace`, counts toward the
platform total budget and never debits a customer wallet or gateway test budget.
Customer background work continues to require its subscription, wallet and
runtime switch. No background consumer borrows a website session.

Friend onboarding is separate from business tools. A human clicks verification,
one incoming application must match a short random challenge, and control
persists a single approval attempt before submitting it. Unknown submission is
reconciled, never repeated. Its proof references a dedicated record rather than
a fabricated receive event. Friend identity alone cannot establish group
ownership: an independent group message and current owner/admin evidence remain
mandatory. Existing friendships are retained and may use private-message proof.

Adding-account projections come from authenticated sealed sessions. Internal
native UID and searchable account are separate fields. Arbitrary application
notes, credentials and challenge payloads never enter general friend lists,
audits or logs.
