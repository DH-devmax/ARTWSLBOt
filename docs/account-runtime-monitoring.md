# Account runtime and monitoring

This is a candidate implementation, not a production capacity or native-protocol
acceptance report. `native_ready` and `ai_ready` remain false. Deployment still
uses the existing Docker Compose services and unchanged local resource limits.

## Ownership and admission

- One active gateway coordinator owns account connections and directory workers.
  Existing advisory ownership, account leases and epoch fences remain in place.
- Each workspace admits 10 accounts and 100 authorized groups. An account admits
  30 distinct active bindings. Feature/management/AI switches do not release a
  binding; inactive authorization does. Existing excess bindings may be retained
  or reduced, but cannot receive an additional group.
- Control service checks plus the forward migration's serialized capacity trigger
  cover approval, AI assignment and manual migration. The trigger protects direct
  platform writes too. Admission writers acquire lock `798423` before row locks.
- Directory discovery is not native-role proof. Recommendations require a fresh
  native role, authenticated account, live receiver lease, initial synchronization,
  recent heartbeat and no known active HTTP cooldown. Management needs admin/owner;
  independent chat assignment also permits a native member.
- Eligible candidates sort by bound groups, latest business-read wait, receive
  queue, observed commit latency, receive load and stable account ID. A missing
  mapping/role blocks selection; the system never invites an account automatically.
- `PostgresLedger` preserves secondary/unbound observations as `shadow`, not
  runnable business work. Current primary observations capture binding version and
  epoch. The native decoder remains a read-only projection and does not enable
  new ranking, finance or automation consumers.

## Business reads

`internal/gateway/read_scheduler.go` replaces hash-striped locks with explicit
account lanes. Each account executes at most one task, the coordinator executes
at most two, and each lane queues at most 16 distinct pending tasks. Renewal
precedes requested directory refresh, which precedes periodic synchronization.
Pending same-resource refreshes coalesce; result-bearing renewal calls do not.
Account order rotates independently of lane priority. Two bounded directory
workers feed the scheduler; no new process or persistent service is introduced.

Each task has a deadline no later than 15 seconds and respects its caller's
deadline/cancellation. The last waiting caller cancels its shared task. Losing
the coordinator cancels all lanes; account stop cancels its outstanding work.
Socket reception, heartbeats, durable commit and ACK never enter this scheduler.

A verified 429 persists a bounded cooldown in `account_read_state`, fenced by
account epoch. A replacement scheduler checks this state before sending HTTP.
Uncertain/mutating results are never automatically replayed. Directory failures
retain the last successful sealed snapshot and its original sampling timestamp.

## Measurements

| Metric | Meaning |
| --- | --- |
| Message receive rate | Group/private messages, including duplicates, divided by a fixed rolling 60 seconds; not HTTP rate or protocol frames |
| Unique ingress rate | Newly committed raw group/private messages in that same 60-second window |
| Business HTTP rate | Calls handed to the business client's HTTP transport in 60 seconds; an attempt is not proof the upstream received it |
| Durable P95 | Message arrival through transaction commit, not just INSERT duration |
| ACK P95 | Arrival through successful socket write; not remote delivery confirmation |
| Receive quantiles | Recent 60 seconds, at most 8192 samples per account per stage; separate commit/ACK counts; empty is null |
| Decode P50/P95/P99 per account | Latest 1000 valid `decode_ms` observations, completed within `(sampled_at - 24 hours, sampled_at]`; includes source loading and decode/sealing, before result UPDATE |
| Workspace total decode P95 | Merge the same raw observations from accounts in the current workspace, select the newest 1000, then calculate P95; never average account percentiles |
| Decoder worker P95 | Whole successful projection task, rolling 60 seconds, at most 8192 samples; includes projection persistence |
| Queue bytes | Transport queue bytes or sealed decode-job source bytes, not plaintext size |
| Pool acquisition | Process-cumulative acquisition count/duration, not a fabricated instantaneous database wait |

Local receiver/read/decoder counters aggregate in memory. Monitoring publishes a
shared snapshot every second, while database account aggregates refresh every
15 seconds using a separate one-connection pool and two-second deadlines.
Historical backlog counts are a separate query every 60 seconds on that same
pool, with their own two-second deadline. Each section carries sampling time;
database failure retains the last successful sample time. Backlogs additionally
expose `backlog_sampled_at`, `backlog_stale` and `backlog_available`: a failed
refresh preserves the number but marks it stale, and a sample older than
120 seconds is unavailable. Recent decode percentiles do not wait for a
historical scan. A fresh local lease updates receive metrics between polls.
HTTP/SSE consumers read shared snapshots and never trigger collection or native reads.
Receiver authorization/coordination still runs each second. Its 15-second
lease and safe metric snapshot refresh every five seconds; state changes are
persisted promptly. Runtime account state uses a distinct-value write gate.
Receiver observation freshness is 15 seconds and never overrides an expired
lease. In-process receive quantiles are computed at most once per second.
Account dashboards reuse this same decoder sample rather than issue another
backlog query; persisted cooldowns remain visible before the first scheduled read.
Decode summaries include actual count, limit, oldest/newest completion time,
six histogram buckets and the original sampling timestamp. Interpolated
percentiles match PostgreSQL `percentile_cont`. Null, negative and nonfinite
durations, future timestamps and the open 24-hour boundary are excluded.
At most 1000 observations per account leave PostgreSQL for the existing shared
monitor; raw event IDs/observations never appear in API or minute history.
Historical replay is included when its decode completed within the sample window;
the value is decode service duration, not old message age or end-to-end delivery.
Workspace summaries and native ingress trends are superadmin-only. An unavailable
database or a sample older than 45 seconds is marked unavailable; retained numbers
carry their original observation time. Reaching 1000 observations is not a
24-hour stability acceptance.
While the resource tab is visible, a new streamed minute refreshes its bounded
history once; two-second snapshots never trigger per-message history queries.

Simon is retired. API and heatbot each collect only their own process, cgroup and
Go runtime every five seconds. The executable injects `internal/metricsproc`
(gopsutil/v4 v4.26.9 for Linux own-PID RSS only); the core monitor and heatbot packages never import
it. A pinned local source patch removes gopsutil's system CPU package initializer.
The adapter never calls its Times/CreateTime or discovery paths; own-PID stat and
CLOCK_BOOTTIME provide CPU/start time. The fixed proc root ignores environment overrides.
Unavailable cgroups/unlimited CPU quota produce null. CPU percentage is relative
to that container's quota. Container memory, process RSS and Go heap are distinct.
Collection failure marks unavailable immediately and retains the last successful
timestamp; snapshots older than 15 seconds are stale.
API reads one fixed heatbot metrics route and HEADs the fixed Web static root
every five seconds (one-second timeout, no redirects, heatbot body <=16 KiB).
Web has no process metrics. Current-database statistics are read every 60 seconds
on the existing monitoring pool with a two-second timeout; no SQL text or other
databases are queried and no role privileges are expanded. Failed database
statistics retain their timestamp and become stale after 120 seconds.
These measurements do not cover a project-total CPU/memory value.

Connection, backlog, cooldown and freshness are independent conditions, not a
health score. Heartbeat >40 seconds and oldest work >30 seconds retain the existing
thresholds; an otherwise healthy quiet group is not offline.

## Persistence and interfaces

Migration `017_account_runtime_monitoring.sql` adds capacity enforcement, shadow
classification, epoch-fenced cooldowns, platform-only monitoring tables and
assignment execution receipts. Existing migrations are unchanged.
The new time-window indexes are built inside the migration transaction. Size and
time the maintenance window against real history volume before deploying; this
is not an authorization for an online migration under active native reception.

- Numeric minute snapshots expire after 24 hours; transitions expire after 7 days.
  Names, chat text, credentials, request bodies and packet content are not stored
  in monitoring history. Account samples have a cascading account FK.
- Transition buffers are bounded at 512 entries while the database is unavailable,
  preserving observation times until recovery. Overflow reports a gap. A process
  crash can lose unflushed transitions; this is not a durable monitoring queue.
- Startup reads persisted open incidents. Process counters naturally reset;
  history must not interpret a reset or a missing minute as zero.
- Retention runs on successful minute writes, including startup. Storage failure
  is exposed as unavailable history, not successful persistence.

All monitoring endpoints are superadmin-only, including in the chat workspace:

| Endpoint | Behavior |
| --- | --- |
| `GET /api/v1/system/monitor` | Fixed `services`, `database_stats`, compatible `api` and scoped accounts/latency; no Simon fields |
| `GET /api/v1/system/monitor/events` | Full snapshot SSE at most every 2 seconds; 8 global/2 per user; session rechecked each frame |
| `GET /api/v1/system/monitor/history` | `start`, `end`, optional workspace-checked `account_id`; at most 24 hours / 1441 minutes |
| `GET /api/v1/system/monitor/incidents` | Seven-day descending transition timeline, 100 rows with `before` cursor |
| `GET /api/v1/groups/{id}/assignment` | Immutable-input preview; versions/epochs/candidate evidence |
| `POST /api/v1/groups/{id}/assignment` | CSRF + stable Idempotency-Key + source/target/binding epochs and version |

Migration `045_project_monitor_services.sql` only extends the incident component
constraint for Web and heatbot. Old Simon incidents remain historical retired
sources; their removal never creates a resolved incident. Old environment fields
in minute JSON are ignored by charts and expire through existing retention.

Migration locks accounts and binding, rechecks eligibility, package/quota and
versions, rejects running/unknown group operations, cancels old queued operations,
increments binding version/epoch and audits in the same transaction. Successful
response is `binding_saved`, `executed:false`, never a claim of native activation.
Receipt retention is seven days. An identical retry returns the original receipt;
a changed request under the same key is rejected. A COMMIT error is unknown.

The Vue confirmation panel retains uncertain request IDs/inputs per website user,
workspace and group in session storage for less than receipt retention. Reopening
can reconcile the same request, not generate a new migration. Account/workspace
switches abort old view requests. Monitoring SSE drops intermediates by sending
full latest snapshots; slow writes time out instead of accumulating messages.

## Release gates

See `test-report.md` for executed results, failed attempts and unverified gates.
Local short inbox replay is not native-protocol throughput, slow-upstream/SSE
fault-load acceptance, a 60-minute target-machine run, or a 24-hour soak.
Scheduler bounds do not prove real-platform rate allowances.
CodeBoarding must use a compatible trusted baseline; an incompatible cache is a
blocked incremental check, never permission to silently run full analysis.
