# Account Dashboard

Selecting a robot is a read-only view change. Its dashboard replaces the
all-account overview; it never starts, reconnects or stops an account.
The selector's “工作台总览” retains workspace totals, all robot connection
records, recent operations and acceptance targets.

`GET /api/v1/accounts/{id}/dashboard` requires both `overview:read` and
`accounts:read` for tenant users. Workspace guards continue to restrict the
chat workspace to superadmins. The response is not cacheable.

## Scope

Account metadata, group counts and operation aggregates use the restricted
tenant transaction and forced RLS for ordinary users. Shared inbox metadata
is aggregated by the platform service only for active group IDs obtained
through that transaction. The ingress query rechecks current tenant ownership,
active authorization and primary account binding. Browser tenant parameters
are ignored. It returns no message content, sender IDs, group IDs, credentials,
other tenants' totals or operation payloads.

Admins see the selected account's ingress, including unbound group messages;
tenant users see only their currently authorized groups. Revoked group
ownership can retain historical operations under existing RLS rules, while
its shared inbox metrics are no longer exposed. Authorization changes during a
request can leave earlier business counts sampled just before that change;
the response is an observation, never permission to submit a native action.

## Measurements

All measurements are database observations with a `sampled_at` timestamp.
Recent inbox windows use `(sampled_at - 60 seconds, sampled_at]` and unique
stored message rows. Throughput is that count divided by 60, not native packet
rate, ACK rate, model throughput or allowed HTTP request frequency.

Latency percentiles P50/P95/P99 measure `persisted_at - received_at` for
nonnegative samples in that window. `persisted_at` is the SQL INSERT timestamp,
before commit/WAL flush and ACK. These values must never be described as
reliable commit or ACK latency. No samples produce null percentiles, not zero;
negative clock differences are counted separately and excluded.

The chart contains 15 ordered minute buckets, including the current incomplete
minute. Empty buckets mean no matching rows were stored, not proof of an
operational native receiver. “最后入库” is the most recent matching INSERT.
Migrations 011/012 add account/time, account/group/time and partial backlog/failure
indexes. Tenant latest-message lookup uses an indexed lookup per authorized group
instead of scanning all other groups' messages in a shared account.

## Health And Backlog

Pending and unresolved inbox rows, queued/running operations, quarantined
messages and unknown results are counted separately. The oldest waiting
message/queued operation reaching 30 seconds marks backlog. Quarantined
messages or unknown operation results take precedence and require review.
Failures in the last 15 minutes are shown separately.

An empty persistent queue is labeled “无数据库积压”. It is not evidence that
in-memory queues are empty or native HTTP is unthrottled. Stored login,
connection switch and connection state are displayed as records.

There is currently no wired native receiver/heartbeat/ACK/HTTP-scheduler
observer. `runtime_observed` remains false; the UI shows those measurements
as unavailable and never manufactures a health percentage or a healthy
connection from a stored `connected` flag. These runtime measurements still
require receiver implementation and acceptance before they can be shown.
Native and AI readiness remain false.

## Refresh

The existing 15-second console refresh also refreshes the selected dashboard;
there is a separate manual refresh and a refresh on tab visibility recovery.
Requests capture website user, workspace and account scope, use cancellation
and a generation check. Account/workspace/user changes clear the previous
sample immediately. A failed refresh preserves the prior sample only in the
same scope, visibly labeled with its original sampling time. There is no
cross-account or persistent browser telemetry cache.

Only Docker Compose is used to deploy this change. Existing migrations remain
immutable and the development database is backed up before migrations 011/012.
