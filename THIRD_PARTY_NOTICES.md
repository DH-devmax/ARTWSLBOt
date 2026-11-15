# Third-Party Baselines

| Project | Pinned commit | License file | Use |
| --- | --- | --- | --- |
| AstrBot fork | a672ef93cb7dbfb854ae01ff131267b9e8a1e050 | LICENSE (AGPL-3.0) | Native protocol port, synthetic compatibility fixtures, behavior reference |
| AstrBot LajiaoLiao reference | e62e2ce12e7d8591afe995c56478205224294afd | LICENSE (AGPL-3.0) | Scoped Go wire adaptation; no Python, bridge process or upstream UI |
| AstrBot media reference | 5ff5113074304e6c2fe89483d3497f727620176d | LICENSE (AGPL-3.0) | Supplementary image metadata / bounded fixed-line read reference; the original login / crypto baseline is unchanged |
| Art Design Pro | f3aaf58eec1a0e988f162352c33862327a484f95 | web/LICENSE (MIT) | Imported Vue/Element Plus framework and new product entry |
| Element Plus X | npm vue-element-plus-x 2.0.3 | licenses/Element-Plus-X-MIT.txt | Product assistant bubbles and conversation list; scoped product UI, no upstream demo routes or request hooks |
| markdown-it | npm markdown-it 15.0.3 | licenses/markdown-it-MIT.txt | Assistant Markdown, lists and tables; raw HTML and images disabled |
| markdown-it runtime dependencies | argparse 3.0.4, entities 8.1.0, linkify-it 6.1.0, mdurl 2.1.0, punycode.js 2.3.1, uc.micro 3.0.0 | licenses/markdown-it-dependencies/ | PSF-2.0, BSD-2-Clause and MIT notices for bundled renderer dependencies |
| MaiBot | 062d45cf8bd4272ac57d7ec03fcf405e5a458211 | licenses/MaiBot-GPL-3.0.txt | Go behavior migration reference and original dashboard icon; Python runtime and upstream pages are not deployed; parity incomplete |
| Simon (retired) | 3778d2fc30704aa4e9f8828567c08dd7d6957c7d | licenses/Simon-MIT.txt | Historical notice only; removed from runtime 2026-11-03 |
| gopsutil | Go module v4.26.9 | licenses/gopsutil-BSD-3-Clause.txt | Linux own-process RSS only; CPU/start avoid its virtualization discovery |
| go-sysconf | Go module v0.4.0 | licenses/go-sysconf-BSD-3-Clause.txt | Linux SC_CLK_TCK constant in own-process metrics adapter |
| GoCaptcha | Go module v2.0.5 | licenses/GoCaptcha-Apache-2.0.txt | Website slider puzzle generation in internal/webcaptcha |
| GoCaptcha assets | Go module v1.0.7 | licenses/GoCaptcha-Assets-Apache-2.0.txt | Fixed embedded slider backgrounds and tile images |
| Freetype-Go | Go module v0.0.0-20170609003504-e2365dfdc4a0 | licenses/Freetype-Go-LICENSE.txt and licenses/Freetype-Go-FTL.txt | GoCaptcha indirect dependency; FreeType License (FTL) selected |
| agent-desk | 9f211c0d799ebc57463d465fc7a34ce39a9e3754 | Upstream LICENSE (Apache-2.0) | Permission catalog, role-template and user-assignment design reference only; no source copied or runtime dependency |
| Eino | v0.9.22 / 0a5ca53b9d3bf7b661dccc863465ec568a421eea | licenses/Eino-Apache-2.0.txt | Go ChatModelAgent Runtime adapter only, with explicit iteration, tool and deadline bounds |
| Bifrost | 6aae0e790012f0e93c495ec967d9f5cd48dae4df | Upstream license | Routing, budget and usage design reference only; no source, service, UI or runtime dependency |

The new protocol port is distributed under the root AGPL license.
Retained Art Design Pro source files keep their original notices. The live entry is
`web/src/wsl/main.ts`; original demonstration routes/authentication are not the product entry.

`web/public/maibot-logo.png` is a PNG conversion of MaiBot's
`dashboard/public/maimai.ico` at the pinned commit above. Its GPL-3.0 license
is retained; only this original icon is reused, not the upstream UI.
`web/public/lajiaoliao-logo.png` is the logo supplied by the project owner
for the LajiaoLiao panel. These assets do not indicate protocol acceptance.

`web/public/robot.svg` uses Lucide 0.468.0's `icons/bot.svg`, with a fixed
product stroke color for the offline image. Its ISC notice is retained in
`licenses/Lucide-ISC.txt`. It identifies the management purpose, not a platform.

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

Native transport uses `github.com/gorilla/websocket` v1.5.3 under
BSD-2-Clause; the notice is retained in
`licenses/gorilla-websocket-BSD-2-Clause.txt`. Simon's MIT license is retained
as historical provenance; its container and client have been retired.
The pinned Simon image declares revision
`699f5a0bd2902a949d5dc89c0a7b323653ed2c04`; compared with the source baseline
above, only `Cargo.lock` differs. Both identifiers are recorded in
`upstream.lock.json` so the image is not mistaken for a rebuild of the baseline.

Before SaaS publication, complete the dependency inventory, source/notice delivery
review, container package audit, MaiBot plugin/memory dependency review, and model
provider retention assessment. None of those release reviews is represented as complete.

Eino's module-supplied `LICENSE-APACHE` is retained byte for byte. This pinned
module has no separate NOTICE file. The adapter also uses
`github.com/eino-contrib/jsonschema` v1.0.3 (Apache-2.0); all transitive module
versions and hashes remain pinned by Go. This is an added dependency boundary,
not a claim of a complete transitive license or security audit. On the local
Go 1.27 toolchain Sonic emits its compatibility warning and falls back to JSON;
synthetic checks pass, but no performance claim relies on this fallback.

The bounded application-message decoder uses `github.com/pierrec/lz4/v4`
v4.1.22 under BSD-3-Clause. Its full upstream notice is retained in
`licenses/lz4-BSD-3-Clause.txt`. This is block decompression only; native AEAD
continues to use the controlled libsodium binding.

Image header validation additionally uses `golang.org/x/image/webp` v0.32.0
under BSD-3-Clause. Its full notice is retained in
`licenses/Go-Image-BSD-3-Clause.txt`. Standard Go codecs validate PNG, JPEG and GIF.
No proprietary desktop icons, sticker packs, SDKs, Java sources or JavaScript
bundles are shipped with the media reader; protocol field evidence is documented
in `docs/native-media.md`. This is not a completed dependency vulnerability audit.

Website slider generation uses `github.com/wenlng/go-captcha/v2` v2.0.5 and
`github.com/wenlng/go-captcha-assets` v1.0.7, both declaring Apache-2.0.
Their original `LICENSE` files are retained byte for byte from these exact
versions in the local Go module cache as `licenses/GoCaptcha-Apache-2.0.txt`
and `licenses/GoCaptcha-Assets-Apache-2.0.txt`. GoCaptcha's attribution is
`Copyright [2021] [Awen <wengaolng@gmail.com>]`; the assets attribution is
`Copyright (c) 2024 Awen <wengaolng@gmail.com>`. Neither pinned module supplies
a separate NOTICE file. The assets LICENSE is its original short Apache-2.0
notice; the complete Apache-2.0 text is retained in the GoCaptcha license file.

The fixed slider resource scope in `internal/webcaptcha/generate.go` is
`go-captcha-assets/resources/imagesv2` and `resources/tiles`. These import
generated Go bindata for 16 JPEG backgrounds at
`sourcedata/images/image-v2-{1..16}/image.jpg` and four tile asset groups at
`sourcedata/tiles/tile-{1..4}/`, each containing `tile.png`, `tile-mask.png`
and `tile-shadow.png` (12 PNG assets). The pinned assets README attributes
the source resources to `github.com/wenlng/go-captcha-resources`; it is not
an additional imported module or separately fetched resource checkout.
The slider reads the embedded bytes, without runtime resource downloads.
Its package dependency closure excludes the assets module's font, character,
thumbnail, shape and older background packages, and Freetype-Go's separately
licensed Luxi test fonts. No resource outside the pinned assets packages is
introduced; the upstream README's `@latest` installation example is not used.

Both pinned GoCaptcha modules require
`github.com/golang/freetype` v0.0.0-20170609003504-e2365dfdc4a0 and
`golang.org/x/image` v0.16.0. The application continues to select its existing
`golang.org/x/image` v0.32.0 and `golang.org/x/text` v0.30.0; the existing
`licenses/Go-Image-BSD-3-Clause.txt` matches the selected image module's LICENSE.
The older `x/image` v0.16.0 and its `x/text` v0.15.0 requirement appear as
module-graph metadata and `/go.mod` entries in `go.sum`, not additional selected
runtime versions. Offline, read-only package inspection on 2026-11-06 found
only the two pinned GoCaptcha modules, the pinned Freetype-Go module and the
existing `x/image` module in the slider's external package dependency closure.
Freetype-Go remains a dependency through the assets helper package even though
the slider does not import font assets.

Freetype-Go's original LICENSE offers a choice of the FreeType License (FTL)
or GPL version 2 or later; this integration selects FTL. Its original
`LICENSE` and complete `licenses/ftl.txt` are retained byte for byte from the
same pinned local module as `licenses/Freetype-Go-LICENSE.txt` and
`licenses/Freetype-Go-FTL.txt`. Attribution: Copyright 2010 The Freetype-Go
Authors. All rights reserved. Its AUTHORS file credits the original FreeType
work to David Turner, Robert Wilhelm and Werner Lemberg, copyright 1996-2010.
This software is based in part on the work of the FreeType Team.

Development-only architecture tooling is isolated under `tools/architecture`:
CodeBoarding 0.15.2 (MIT), reviewed source
`80199edc78b837cdea4b6cbecd631743ffe6b8a2`, and Mermaid CLI 11.12.0
(MIT). Python dependencies are hash-locked in `requirements.lock`; Node
dependencies and their license metadata are locked in `package-lock.json`.
TypeScript 5.6.3 (Apache-2.0), Vue compiler 3.5.22 (MIT) and Puppeteer
23.11.1 (Apache-2.0) support source extraction/rendering only. None is added
to the production Go binary or Compose services. Upstream package notices
remain with the installed packages; this is not a completed transitive audit.

The lightweight LajiaoLiao adapter imports only the message definitions from
`github.com/openimsdk/protocol` v0.0.73-alpha.17 (Apache-2.0), not OpenIM's
full SDK, storage or service processes. Its upstream `LICENSE` is retained as
`licenses/OpenIM-protocol-Apache-2.0.txt` (this pinned module has no separate
NOTICE file); generated protobuf messages retain their
upstream notices. Go module hashes pin the exact definitions. The desktop
wire's MD5 password field is an interoperability requirement; it is not the
website password hash and is never stored or returned by this adapter.

# Product Design Adaptations

The current interface adapts Lumenwave Finance, Copyright 2026 Uiverse, from the
owner's local web-design-templates pack and the referenced multi-tenant workspace.
Original attribution and Uiverse Design System License v1 are retained in
`web/src/wsl/theme/lumenwave/DESIGN.md` and `LICENSE.md`. The adaptation uses local
fonts and existing icons; no template font or icon CDN is loaded.

The historical Fairweather resources are Copyright 2026 Uiverse Design.
The original attribution and Uiverse Design System License v1 are preserved in
`web/src/wsl/theme/fairweather/DESIGN.md` and `LICENSE.md`. No remote fonts or
icon dependencies from the template are used.
