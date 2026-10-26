# Architecture maintenance

`../architecture.md` remains the human-approved ownership contract. Automation
must not edit that contract or decisions in `decisions/`. These documents describe
the source revision on which the extractor runs; they
are not proof of deployment, readiness, capacity or native acceptance.

## Four views

| View | Evidence | Meaning |
| --- | --- | --- |
| [System](generated/system.md) | Compose dependencies, endpoint declarations and Nginx proxy | Declared deployment, not observed traffic |
| [Backend](generated/backend.md) | Go package metadata and source imports with sodium enabled | Internal package dependency, not a runtime call |
| [Frontend](generated/frontend.md) | TypeScript AST and Vue SFC scripts/templates | Product UI dependencies only, not framework demos |
| [Agents](generated/agents.md) | Explicit source symbols and interface wiring | Only present components; heatbot generation stays disabled |

`generated/graph.json` contains evidence for every edge. `generated/manifest.json`
records input hashes, extractor settings and per-view hashes. Stable identifiers
and sorting prevent layout/source timestamp churn. An unchanged view is not
rewritten. Unknown imports and incomplete coverage are reported, not inferred.

## Trust and workflow

Deterministic extraction is authoritative for code dependencies. CodeBoarding
0.15.2 supplies advisory component analysis, not permissions or source truth.
It does not natively analyze Vue SFCs; the separate TypeScript/Vue extractor is
mandatory. Its first analysis is explicit; incremental failures never silently
trigger a costly full analysis.

Only Git-candidate Go and product TypeScript/Vue files are sent to the configured
provider. Ignored files, runtime data, credentials, logs, attachments, database
contents and third-party framework demos are excluded. The provider receives
source excerpts, so repository owners must approve that disclosure.

Credentials belong in a mode-0600 file outside the repository or in the
`ARCHITECTURE_OPENAI_API_KEY` environment variable. They are separate from
business assistant and heatbot settings. Never commit the key or model transcripts.
CodeBoarding caches contain executable Python serialization: only this runner's
local cache or a verified trusted default-branch artifact may be used. PR caches
must not be loaded.

CI runs credential-free extraction and diagram checks on PRs/pushes. Paid model
analysis is a separately enabled trusted job. PRs cannot modify the analyzer
and then execute it with secrets. CI reports drift and publishes review artifacts;
it does not push source changes or overwrite human decisions.

## Maintainer commands

These are development/CI dependencies, not a second deployment path. Nothing is
added to production Compose or the application images. Python 3.12 is required
by CodeBoarding; Node 22+, Go matching `go.mod`, and a Chromium browser are used
only for analysis/rendering. The existing production service is not restarted.

```sh
uv venv --python 3.12 "$HOME/.cache/artwslbot/architecture/venv"
uv pip sync --python "$HOME/.cache/artwslbot/architecture/venv/bin/python" \
  --require-hashes tools/architecture/requirements.lock
PUPPETEER_SKIP_DOWNLOAD=true npm ci --prefix tools/architecture --ignore-scripts
ARCHPY="$HOME/.cache/artwslbot/architecture/venv/bin/python"
"$ARCHPY" tools/architecture/architecture.py update
"$ARCHPY" tools/architecture/architecture.py check
"$ARCHPY" tools/check_architecture.py
"$ARCHPY" -m unittest discover -s tools/architecture -p 'test_*.py'
node tools/architecture/render.mjs
```

On macOS the renderer uses installed Google Chrome. Elsewhere install the locked
Puppeteer headless shell with `npm exec --prefix tools/architecture -- puppeteer browsers
install chrome-headless-shell` and set `PUPPETEER_EXECUTABLE_PATH` if needed.
SVGs are ignored under `output/architecture/rendered`; Markdown Mermaid views and
their `.mmd` sources are tracked. External package/style dependencies and runtime
component selection are recorded in `graph.json`, not silently claimed complete.

```sh
"$ARCHPY" tools/architecture/provider.py configure
"$ARCHPY" tools/architecture/provider.py preflight
"$ARCHPY" tools/architecture/analyze.py --initialize  # explicit first paid run
"$ARCHPY" tools/architecture/analyze.py               # incremental thereafter
"$ARCHPY" tools/architecture/architecture.py diff --base <verified-commit>
```

The provider/model are pinned in `tools/architecture/toolchain.json`. The runner
copies only allowlisted Git-candidate source into a private synthetic repository;
upstream does not receive the real checkout's history, remotes or home directory.
A loopback relay holds the real key; the analysis subprocess receives a random
local-only credential. Source reads are repository-contained in pinned upstream.
Telemetry is disabled. Each run is limited to 600 seconds, 16 provider requests,
4096 output tokens per request and 300,000 conservatively reserved input/output
tokens (UTF-8 bytes upper-bound inputs); these are safety bounds, not a currency
quote. Missing usage retains its estimate. Requests are never automatically
retried by the relay, and failed analysis does not replace a trusted baseline.
CodeBoarding's language-server view uses its upstream default build context;
the deterministic Go view explicitly uses Linux/CGO/sodium. Do not interpret
an advisory component classification as production build compatibility.

Cache and raw advisory output remain outside Git under
`~/.cache/artwslbot/architecture/analysis`, mode 0700. Inspect `last-run.json`
for counts/usage/status. Do not publish private diagnostics or raw component
descriptions. If toolchain/ignore settings change, preserve the old cache for
review and explicitly initialize a separate compatible baseline; do not remove
the check or silently perform a full run. A failed promotion keeps the old
baseline; `previous-baseline` after a crash requires manual reconciliation.

## GitHub Actions

`Architecture / evidence` checks every push/PR without secrets. It compares
the PR's verified merge base against its exact head (push uses the exact prior
commit), reports node/edge additions and removals, and publishes freshly extracted
candidate diagrams plus evidence. Candidate generation happens in ignored output,
even if the committed documentation is stale; a final check rejects stale tracked
documents without committing them automatically. An absent baseline is labeled
`baseline-unavailable`, not a false claim that every component was newly added.
The first PR therefore establishes an inventory; subsequent PRs have real diffs.

`Architecture / model` never runs on PRs. For remote activation, the repository
owner must create the `architecture-model` environment restricted to `main`,
set its `ARCHITECTURE_OPENAI_API_KEY` secret, and explicitly dispatch the default
branch workflow once with `initialize=true`. Enable subsequent default-branch
push analysis using the repository variable `ARCHITECTURE_MODEL_ENABLED=true`.
Leave that variable unset to keep paid analysis manual. The existing protected
`main` PR checks gate trusted code; no PR receives the environment secret.
Required environment reviewers can additionally gate each paid run when desired.

Only this trusted default-branch job saves/restores the CodeBoarding baseline.
Cache lineage includes toolchain/runner/ignore hashes, origin branch and an
ancestor commit check; PR artifacts and model transcripts are never restored.
No `pull_request_target`, workflow-run privilege escalation, write token, automatic
commit, PR-source execution with secrets or overwrite of design decisions is used.
The job summary is the PR/push review surface; no comment-write permission is needed.

Remote activation results and exact run identifiers are recorded separately in
[`ci-activation.md`](ci-activation.md). Local installation alone is not evidence
that a remote analysis has run.
