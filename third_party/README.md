# Pinned gopsutil Source

`gopsutil/` contains v4.26.9 at upstream commit
`81236e0193191e972dfdf1e15cc1ae53764dc1e0`.
Upstream test files and non-runtime example directories are omitted.
The upstream BSD license is retained.

The only runtime source patch removes `cpu/cpu.go`'s package initializer,
which otherwise samples system-wide CPU counters on import. Production uses
only `process.MemoryInfoWithContext` against its own PID with a fixed `/proc`
context. CPU/start time use the bounded project adapter; process enumeration,
Times/CreateTime, virtualization discovery and system CPU APIs are not called.

The local Go module replacement is shared by builds and tests. It adds no
service or collector process. Dependency updates must review this patch again.
