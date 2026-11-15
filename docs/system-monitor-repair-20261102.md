# System monitor repair

## Scope

This change starts from the exact deployed decoder-repair v16 source. It does not
include the unverified lightweight, parser or media candidates in the workspace.
CodecVersion 7, native readiness, AI readiness, schema, receiver workers, database
connections and container resource limits are unchanged.

## Metrics and interaction

- `GET /system/monitor` and its SSE snapshots now expose `decode_latency` for the
  selected workspace. The newest 1000 valid raw observations in the previous
  24 hours are merged before calculating percentiles and six histogram buckets.
  No account-percentile averaging or new database query is introduced.
- Database observation time controls availability. Another account's timestamp
  cannot make an old database sample appear current. Missing, future or expired
  samples do not become zero.
- The overview separates current connection/backlog issues from retained failed
  decode rows in the last seven days and process-cumulative quarantine counters.
  Historical failures remain visible and are not marked repaired.
- Memory trends use independent API and Simon environment axes. Simon describes
  its container-visible environment, not total project consumption.
- Snapshot scope checks apply to both HTTP and SSE. Older snapshots cannot replace
  newer ones. Leaving the page aborts requests and disposes charts.
- Refresh reloads the visible history/events as well as the current snapshot.
  Each panel preserves its own errors; an incoming SSE snapshot cannot erase a
  history/event error. Range changes cancel previous history requests. Event
  pagination is deduplicated and bounded to 1000 rows.

## Verification

Results are recorded in `test-report.md`. Browser fixtures make no native calls.
This change does not establish production capacity or repair native account login.
