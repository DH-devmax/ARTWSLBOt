# Third-Party Baselines

| Project | Pinned commit | License file | Use |
| --- | --- | --- | --- |
| AstrBot fork | a672ef93cb7dbfb854ae01ff131267b9e8a1e050 | LICENSE (AGPL-3.0) | Native protocol port, synthetic compatibility fixtures, behavior reference |
| Art Design Pro | f3aaf58eec1a0e988f162352c33862327a484f95 | web/LICENSE (MIT) | Imported Vue/Element Plus framework and new product entry |
| MaiBot | 062d45cf8bd4272ac57d7ec03fcf405e5a458211 | licenses/MaiBot-GPL-3.0.txt | Go behavior migration reference; Python runtime and upstream UI are not deployed; parity incomplete |
| Simon | 3778d2fc30704aa4e9f8828567c08dd7d6957c7d | licenses/Simon-MIT.txt | Pinned private monitoring container; Go metric projection only; no upstream UI/source copied |
| agent-desk | 9f211c0d799ebc57463d465fc7a34ce39a9e3754 | Upstream LICENSE (Apache-2.0) | Permission catalog, role-template and user-assignment design reference only; no source copied or runtime dependency |

The new protocol port is distributed under the root AGPL license.
Retained Art Design Pro source files keep their original notices. The live entry is
`web/src/wsl/main.ts`; original demonstration routes/authentication are not the product entry.

`go.mod`/`go.sum` and `web/pnpm-lock.yaml` pin direct and transitive dependencies.
The native protocol test image uses a controlled libsodium C binding rather than an
incompatible replacement cipher. Alpine package versions still require recording
in a release SBOM; this document is not a completed dependency-license or vulnerability audit.

Android 3.7.4 hashing uses `github.com/spaolacci/murmur3` v1.1.0, copyright
2013 Sebastien Paolacci, under BSD-3-Clause. Its full notice is retained in
`licenses/murmur3-BSD-3-Clause.txt`. The offline inspection dependencies are
pinned separately in `tools/requirements-native-client.txt`; they are not
production API dependencies. The proprietary APK, native libraries, decompiled
sources, and deployment keys are not vendored. Synthetic compatibility vectors
contain no real user credentials or provider keys.

The Simon metric client uses `github.com/gorilla/websocket` v1.5.3 under
BSD-2-Clause; the notice is retained in
`licenses/gorilla-websocket-BSD-2-Clause.txt`. Simon's MIT license is retained
even though its separately pinned container is not bundled into the Go binary.
The pinned Simon image declares revision
`699f5a0bd2902a949d5dc89c0a7b323653ed2c04`; compared with the source baseline
above, only `Cargo.lock` differs. Both identifiers are recorded in
`upstream.lock.json` so the image is not mistaken for a rebuild of the baseline.

Before SaaS publication, complete the dependency inventory, source/notice delivery
review, container package audit, MaiBot plugin/memory dependency review, and model
provider retention assessment. None of those release reviews is represented as complete.
