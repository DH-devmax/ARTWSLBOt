# Docker Development

Development uses the existing root Docker Compose project only. Do not start
host Go/Vite/Python services or a second API against the same native accounts.
Run the helper from the checkout that owns the local deployment, never from a
validation worktree against another checkout's containers.

## Commands

Keep the existing `.env` private (mode 0600), credentials and data volumes.
The helper requires `.env`, `compose.yaml`, `compose.local.yaml`,
`compose.dev.yaml`, Docker Compose and an existing project. The project name is
`COMPOSE_PROJECT_NAME`, defaulting to `wsl-platform`; container working-directory
labels must match the helper's checkout. A missing or foreign project is refused.

| Command | Effect |
| --- | --- |
| `bash tools/docker_dev.sh web` | Build the development Web image, then activate only Web after checking the existing API is healthy. |
| `bash tools/docker_dev.sh web-build` | Rebuild the development Web image only; run `web` afterward to activate it. |
| `bash tools/docker_dev.sh api --confirm-api-restart` | Build `wsl-api:dev`, then recreate only API; native connections may briefly pause. |
| `bash tools/docker_dev.sh status` | Show existing Compose services; this is not native/AI acceptance evidence. |

`web` loads `compose.yaml`, `compose.local.yaml`, then `compose.dev.yaml`.
Activation uses `--no-deps --no-build --wait web`, preserving API/native
connections. `api` uses only the base/local files and the corresponding
`--no-deps --no-build --wait api`; it does not switch Web mode, migrate the
database or update heatbot.

## Frontend And Authentication

Once development Web is active, mounted product sources under `web/src/wsl`
and the explicitly mounted charts/assets/public files use frontend HMR/reload.
Source saves do not invoke the helper or restart API. Package/lockfile edits,
TypeScript configuration and other image-only files require rebuilding and
activating Web. Framework demo routes are not part of the product surface.

Web remains on `127.0.0.1:${WEB_PORT:-5173}`. Vite proxies `/api/` and `/healthz`
to the existing API; login, CAPTCHA, sessions, CSRF, roles and native admission
checks still apply. It does not load or serve `.env`. HMR, container health and
saved configuration never activate protocols or raise native/AI readiness.

## Resources And Rollback

The helper always retains the local resource overlay. Its CPU/memory limits are
PostgreSQL 1/1 GiB, API 1/512 MiB, heatbot 0.5/256 MiB and release Web 0.25/128 MiB.
Development overrides only Web to 0.75 CPU and 512 MiB; its swap limit is also
512 MiB and Node's heap limit is 384 MiB. These are limits, not measurements.
Web runs as a non-root user with read-only source mounts/root filesystem;
the named Vite cache and `/tmp` remain writable. Build/VM resources are separate.

Before API delivery, pass the required checks and review schema compatibility
and unresolved submissions. Failed checks or unresolved submissions block it;
query/reconcile unknown outcomes instead of replaying them.
`--confirm-api-restart` acknowledges this review; the helper neither performs
these checks automatically nor grants authorization.

Record the selected mode and actual running image IDs, preserve protected
configuration and the immediately previous running release before replacement.
The helper does not back up or roll back; mutable `:dev` images are not rollback
versions. A reviewed frontend rollback uses the retained compatible Web image
with base/local Compose, without the development overlay, and targets only Web
with `--no-deps`. Backend rollback needs schema/operation compatibility review.
Do not run an old migrator, restore over new writes, remove data volumes or prune
the stack. See [Docker operations](docker-operations.md) for release retention
and [architecture](architecture.md) for ownership and acceptance boundaries.
