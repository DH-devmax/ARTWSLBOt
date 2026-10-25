# Docker Operations

The only supported deployment is the root Compose project. Do not start the
legacy host Go/Vite helper. Do not run `docker compose down -v` on a real stack.

## Initialize

1. Build `wsl-api:local` with `deploy/api.Dockerfile`.
2. Run `wsl init` inside the image with the workspace mounted and the current
   numeric host UID/GID. It refuses to overwrite `.env`.
3. Set `PUBLIC_ORIGIN` to the actual HTTPS origin and `COOKIE_SECURE=true` for
   an external deployment. Keep `.env` mode 0600, outside Git and image contexts.
4. Run `docker compose up -d --build --wait`. The migration service holds the
   owner credential; the long-running API does not.
5. Run `docker compose --profile ops run --rm bootstrap` only for a fresh
   database. It refuses to create another superadmin.
6. Verify `/healthz`, website login, both workspace/account lists and Simon
   status. Native/AI readiness must still be false.

## Release

CI: repository and immutable migration checks, architecture imports/Compose
boundaries, Go race tests with libsodium, isolated PostgreSQL integration,
frontend build, hardened container smoke and full Compose startup.
Browser checks also run inside Docker, with synthetic writes and no native
signup/login/model calls. Clean product builds use `web/tsconfig.wsl.json`.

Main-branch protection requires a pull request and the successful `foundation`
check against the up-to-date branch, resolves review conversations, and rejects
force pushes/deletion. The rule includes administrators. A sole maintainer
does not need to approve their own pull request; CODEOWNERS still identifies
the reviewer. Do not bypass checks by changing the workflow or migration lock.

`docker-release` runs the checks again before publishing API/web images to
`ghcr.io/dh-devmax/artwslbot-{api,web}:sha-<commit>`. It records immutable
digests in its job summary. Set `WSL_API_IMAGE` / `WSL_WEB_IMAGE` to those exact
digests; heatbot shares the API image but uses its own unprivileged executable.
No `latest` deployment, server SSH or in-app update is supported.

Before upgrading, freeze native owners and business writes, create a consistent
database backup plus separately protected MASTER_KEY/configuration inventory,
review migrations and check the candidate on a restored retention-compliant copy.
Run `docker compose pull`, then the maintenance-window migration and
`docker compose up -d --no-build --wait`. Preserve unknown native actions.

## Backup And Restore

For a maintenance-window PostgreSQL backup, run `pg_dump -Fc -U wsl_owner -d wsl`
inside the postgres container and stream to a mode-0600 secure backup path.
Snapshot metadata must include migration names, image digests, SHA-256, counts,
financial totals, binding versions and the consistency point.

Do not include `.env` in source releases. Store encrypted secrets separately and
preserve MASTER_KEY or sealed values cannot be opened. Backup retention and
restore filtering must enforce the same seven-day raw-message policy. The
database dump is not evidence that the retention-aware restore is implemented.

Restore into an isolated new Compose project/volume, recreate restricted roles,
then use `pg_restore` under owner authority. Validate RLS and evidence totals
before handing over a native connection. A forward migration requires a
compatible rollback or a reviewed data conversion; never restore an old
snapshot over post-cutover writes.

## Monitoring Boundary

Simon 0.3.7 is pinned by OCI digest. It has no published port, host mount,
host PID namespace, Docker socket or allowed file roots. Its network is internal
and only the API is attached. The API reads only `/ws/g`, with bounded compressed
and decompressed frames, projects CPU/memory/load/uptime, caches five seconds,
and preserves a marked stale snapshot when unavailable. Only superadmins can
read the projected API. The browser never receives the upstream address/token.

These metrics describe the container-visible system. Docker Desktop metrics
are not measurements of the production Linux host; there is no claim of
per-container Docker metrics without a separately reviewed read-only gateway.
Simon notification endpoints are not exposed and no notifications are sent.
