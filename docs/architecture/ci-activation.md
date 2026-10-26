# Remote architecture CI

Activation date: 2026-10-27 (Asia/Shanghai).

This change publishes architecture tooling and documentation only. It starts
from remote `main` at `f6692ec09b9f9ed9e9416aee21b09522c716371f`.
Uncommitted product changes in the local development checkout are not included.
The four generated views therefore describe the remote source baseline, not
the newer local candidate or running containers.

## Secret and execution boundaries

- The provider key is an environment secret named
  `ARCHITECTURE_OPENAI_API_KEY` in `architecture-model`; no value is committed.
- The environment admits only the `main` branch. Existing protected-branch PR
  checks remain unchanged; PR analysis is credential-free.
- The approved provider is `https://www.sevnx.lol/v1`, model
  `deepseek-v4.1-flash`. Tooling enforces per-run request, token and time bounds.
- First initialization is an explicit workflow dispatch. Default-branch
  incremental runs are controlled by `ARCHITECTURE_MODEL_ENABLED`.
- No release workflow, deployment, migration or real native-account test is
  authorized or triggered by architecture analysis.

## Validation

Local pre-publication validation passed: 14 architecture unit tests, source
snapshot consistency, existing ownership and repository guards, pinned
actionlint, and four nonempty Mermaid SVG renders. The actual provider key was
checked against all Git-candidate files and was absent.

Remote run identifiers and observed results are appended after execution;
publication alone does not count as a successful run.

## Cold-run incremental framing fix

Initial remote baseline run `40397921599` passed (one provider request,
11,909 supplier-reported tokens). Restored incremental run `40398489211`
failed before any provider request: pinned upstream bootstrap emits human
installation progress on stdout before the CLI JSON result on a fresh runner.
Warm local tests had not exercised that output shape.

The adapter now accepts exactly one terminal incremental wire result after
progress output, validates mode/output path and rejects conflicting results,
truncation and full-analysis/error signals. It does not suppress failures,
change provider limits or introduce retries. Automatic model analysis was
disabled immediately after the failed recovery check. Runner fingerprints
change, so a fresh baseline requires explicit initialization; the old trusted
cache is preserved, not relabeled or promoted.
