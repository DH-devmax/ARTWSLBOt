# Local patch notes

Base: `github.com/shirou/gopsutil/v4 v4.26.9`, BSD-3-Clause; retain `LICENSE`
and the upstream source notices.

The project uses a local replacement to remove the system CPU initializer.
Production monitoring remains restricted to the current process through
`internal/metricsproc`; these patches do not expand its collection methods.

The 2026-11-15 CI fixes additionally:

- Parse CPU fields at their destination's 32-bit width and reject overflow.
- Format unsigned user IDs without narrowing them to a platform-sized integer.
- Subtract the memory watermark after accumulating available pages, clamping
  deficits to zero instead of allowing unsigned underflow.

The CPU and memory regression files use temporary proc fixtures only. The
mandatory CI unit job executes these local tests separately from application
coverage. No third-party diagnostic is waived to make CodeQL pass.
