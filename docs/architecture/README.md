# Architecture maintenance

`../architecture.md` remains the human-approved ownership contract. Automation
must not edit that contract or decisions in `decisions/`. These documents describe
the current candidate source, including reviewed uncommitted product code; they
are not proof of deployment, readiness, capacity or native acceptance.

## Four views

| View | Evidence | Meaning |
| --- | --- | --- |
| [System](generated/system.md) | Compose dependencies, endpoint declarations and Nginx proxy | Declared deployment, not observed traffic |
| [Backend](generated/backend.md) | Go package metadata and source imports with sodium enabled | Internal package dependency, not a runtime call |
| [Frontend](generated/frontend.md) | TypeScript AST and Vue SFC scripts/templates | Product UI dependencies only, not framework demos |
| [Agents](generated/agents.md) | Explicit source symbols and interface wiring | Assistant interactions; disabled heatbot is separate |

The backend view has a [receive business path](generated/backend-receive.md):
bounded batches, sealed inserts, durable commit and fenced ACK socket writes.
When decoder wiring exists in the analyzed revision, it is a separate branch
after commit, not a dependency that delays ACK. An assistant approval detail is
generated as `generated/agents-approval.md` only when the assistant exists.
It separates model/tool proposals from the website's human approval, shared
configuration commit and control-owned native business admission. The latter
reuses durable operations and the native final fence without granting execution
to the assistant or access to controlled test windows. Each platform's production
admission remains separate. Missing components are not drawn as implemented.
These are reviewed mappings of calls in named functions and explicit bindings,
not a complete control-flow proof or observed running topology. Removed or
reordered anchors require review rather than an AI-generated substitute.

When the superadmin acceptance endpoint exists, the separate
`generated/controlled-tools.md` business view records its human grant, durable
operation, current native evidence, final fence and read-only reconciliation.
It does not draw an execution edge from assistant or heatbot to native transport.

`generated/customer-business.md` records authenticated onboarding proof, explicit
binding confirmation, source-specific production admission and the bounded
automatic-rule consumer. Invitation acceptance uses its own durable attempt;
automatic rules use the same native protocol ports with independent final fences.
Matching rules suppress a heat reply in `generated/heat-chat.md`.

`generated/graph.json` contains evidence for every edge. `generated/manifest.json`
records input hashes, extractor settings and per-view hashes. Stable identifiers
and sorting prevent layout/source timestamp churn. An unchanged view is not
rewritten. Unknown imports and incomplete coverage are reported, not inferred.

The optional local Docker development overlay has a separate, source-backed
system view in [`../docker-development.md`](../docker-development.md). It replaces
only the frontend process when explicitly activated; generated system inventory
here continues to describe the production Compose contract, not an additional
native connection owner.

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
node --test tools/architecture/render-cache.test.mjs
node tools/architecture/render.mjs
```

On macOS the renderer uses installed Google Chrome. Elsewhere install the locked
Puppeteer headless shell with `npm exec --prefix tools/architecture -- puppeteer browsers
install chrome-headless-shell` and set `PUPPETEER_EXECUTABLE_PATH` if needed.
SVGs are ignored under `output/architecture/rendered`; Markdown Mermaid views and
their `.mmd` sources are tracked. External package/style dependencies and runtime
component selection are recorded in `graph.json`, not silently claimed complete.
The product's explicit shared Art bar/ring chart files, lifecycle helper and
chart type file are also extracted and hashed. Framework demo routes and
unreferenced component directories remain outside this allowlist.
The renderer fingerprints source, renderer code, locked dependencies and options,
checks the actual SVG digest, and skips unchanged views without launching Chrome.
Missing/corrupt output is rendered again; fresh CI output validates every view.
Only previously recorded renderer-owned SVGs are removed when a view disappears.

```sh
"$ARCHPY" tools/architecture/provider.py configure
"$ARCHPY" tools/architecture/provider.py preflight
"$ARCHPY" tools/architecture/analyze.py --cache-path # current private namespace
"$ARCHPY" tools/architecture/analyze.py --preflight  # no key read or model call
"$ARCHPY" tools/architecture/analyze.py --initialize  # explicit first paid run
"$ARCHPY" tools/architecture/analyze.py               # incremental thereafter
"$ARCHPY" tools/architecture/architecture.py diff --base <verified-commit>
```

The provider/model are pinned in `tools/architecture/toolchain.json`. The runner
copies only allowlisted Git-candidate source into a private synthetic repository;
upstream does not receive the real checkout's history, remotes or home directory.
A loopback relay holds the real key; the analysis subprocess receives a random
local-only credential. Source reads are repository-contained in pinned upstream.
The adapter explicitly selects Chat Completions, including tool calls. This
matches the relay's sole allowed route and prevents client-library model-name
heuristics from switching to Responses. A synthetic SDK-to-relay test verifies
the route without submitting source to the provider; other routes remain denied.
Telemetry is disabled. Each run is limited to 600 seconds, 16 provider requests,
120 seconds per provider request, 4096 output tokens per request and 300,000 conservatively reserved input/output
tokens (UTF-8 bytes upper-bound inputs); these are safety bounds, not a currency
quote. Missing usage retains its estimate. Requests are never automatically
retried by the relay, and failed analysis does not replace a trusted baseline.
The adapter also sets the scope-agent and planner attempt limits to one; the
upstream middleware has a retry policy independent of the SDK setting. Private
reports retain fixed timeout/transport/HTTP-status counts without response bodies,
while failed submitted requests keep their unknown-usage reservation.
CodeBoarding's language-server view uses its upstream default build context;
the deterministic Go view explicitly uses Linux/CGO/sodium. Do not interpret
an advisory component classification as production build compatibility.

Cache and raw advisory output remain outside Git under
`~/.cache/artwslbot/architecture/analysis/v2/<repository>/<ref>/<settings>`,
mode 0700. Local worktrees share a repository identity but not a branch namespace;
CI uses the verified GitHub repository and branch. Detached local commits are
separate scopes. `--cache-path` resolves the current location. Inspect that
namespace's mode-0600 `last-run.json` for counts/usage/status, not a legacy global
report. Preflight failures write a fresh blocked report before reading credentials
or calling the model; concurrent analyzers cannot overwrite each other's status.
Do not publish private diagnostics or raw component
descriptions. The model fingerprint includes its runner, source inventory
primitives, locked Python dependencies, toolchain and ignore policy. Diagram-only
rules in `architecture.py`/`flows.py` and Vue/Mermaid extractors do not invalidate
the model baseline. Shared inventory primitives live in `common.py`; changing
their source-selection behavior still invalidates it. Product changes are the
input to incremental analysis, not a reason to allocate another namespace.

Each successful analysis records SHA-256 hashes of every baseline artifact,
including the fingerprint, static pickle and its tag. Preflight verifies these
before upstream can load executable serialization. The complete verified
baseline and state are checkpointed in the same private repository/ref/settings
namespace under `~/.local/share/artwslbot/architecture/baselines/v2/`.
Checkpoints are immutable and previous versions remain available. Only the
mode-0600 `latest.json` pointer changes atomically.

When both active baseline and state are absent, the default run or `--preflight`
automatically restores that exact namespace's checkpoint after ownership,
permissions, artifact hashes, identity and ancestor checks. Restoration does not
read the provider key, call a model, initialize a new baseline or copy one from
another branch/worktree. A partial active baseline, damaged backup, identity
mismatch or interrupted promotion remains an explicit failure, preserving the
evidence. Never delete such evidence merely to trigger restoration.
The report states `baseline_restored=true` for an actual recovery.

If toolchain/ignore/model-runner settings change, preserve the old cache for
review and explicitly initialize a separate compatible baseline; do not remove
the check or silently perform a full run. A failed promotion keeps the old
baseline; `previous-baseline` after a crash requires manual reconciliation.
There is no automatic migration of legacy unscoped caches. Upgrading the runner
can therefore intentionally require a new paid baseline. The upstream CLI
adapter also forbids a full-analysis fallback during an incremental run.
Deterministic extraction/diffs continue independently. Source hashes are checked
while copying and again before promotion; edits during analysis prevent a
stale snapshot from being published as the current source.

For a genuinely new namespace, use the explicit `--initialize` command after
owner authorization. Run `--preflight` and a default incremental analysis after
initialization to verify the continuing workflow. The owner's 2026-10-30 request
to repair the missing compatible baseline authorizes this batch's local
initialization and white-listed source disclosure through the existing bounded
provider; it is not standing permission for unrelated branches/providers or
future automatic full-analysis retries. Current validation is recorded in
[`validation-2026-10-30-baseline.md`](validation-2026-10-30-baseline.md).

## GitHub Actions

`platform-checks / architecture` checks PRs and main pushes without secrets. It compares
the PR's verified merge base against its exact head (push uses the exact prior
commit), reports node/edge additions, removals and semantic attribute changes
(including gates and interface/template attributes), and publishes freshly extracted
candidate diagrams plus evidence. Candidate generation happens in ignored output,
even if the committed documentation is stale; a final check rejects stale tracked
documents without committing them automatically. An absent baseline is labeled
`baseline-unavailable`, not a false claim that every component was newly added.
The first PR therefore establishes an inventory; subsequent PRs have real diffs.
Source-line-only evidence changes do not count as topology changes. The JSON
report includes affected views, comparison commits and coverage gaps.

`Trusted architecture advisory / model` never runs on PRs. For remote activation, the repository
owner must create the `architecture-model` environment restricted to `main`,
set its `ARCHITECTURE_OPENAI_API_KEY` secret, and explicitly dispatch the default
branch workflow once with `initialize=true`, after separate owner authorization.
Subsequent analyses are explicit default-branch dispatches with initialization
disabled. Paid analysis is not part of untrusted PR jobs.

Only this trusted default-branch job saves/restores the CodeBoarding baseline.
Cache lineage includes toolchain/runner/ignore hashes, origin branch and an
ancestor commit check in local runs and CI; PR artifacts and model transcripts
are never restored. Baseline preflight happens in a step without model credentials.
Reports must match the current run, run attempt and commit before appearing in
the job summary; a cached success cannot stand in for a failed/not-started run.
No `pull_request_target`, workflow-run privilege escalation, write token, automatic
commit, PR-source execution with secrets or overwrite of design decisions is used.
The job summary is the PR/push review surface; no comment-write permission is needed.

Local measured results are in
[`validation-2026-10-27.md`](validation-2026-10-27.md).
Remote activation and run identifiers are in [`ci-activation.md`](ci-activation.md).
PR #21 publishes the tooling against the actual remote baseline, not the
newer uncommitted local product code. The remote environment admits only `main`;
required environment reviewers may additionally gate each paid analysis.
Local and remote source fingerprints must not be conflated.
The persistent checkpoint directory above is local to each executor; this local
repair does not initialize or deploy a remote GitHub runner. Remote cache
eviction with no trusted checkpoint still requires explicit initialization,
as specified in `ci-activation.md`.

## Change Matrix

| Change | Update / validation |
| --- | --- |
| Go package imports or program entries | Backend; ownership guard and source evidence |
| Product Vue/TS imports, templates or clients | Frontend; AST coverage and product-only scope |
| Compose, Docker ownership or proxy endpoint | System; review human deployment contract |
| Agent tools, authorization, approval or binding | Agents and affected business detail; scoped call anchors |
| Implementation or line movement only | Refresh hashes/evidence; preserve unchanged Mermaid/SVGs |
| Runner, ignore or pinned analysis settings | Tooling tests; isolated baseline; explicit approval to initialize |

After a structural change: `update` -> `check` -> ownership guard -> tests and
render -> trusted incremental CodeBoarding. Record a blocked analysis explicitly
when no compatible baseline exists. Do not run a full analysis, push changes,
dispatch paid CI, or modify production merely to make this checklist green.

The latest tooling-only hardening checks and explicitly blocked model result are
in [incremental validation](validation-2026-10-27-incremental.md).
