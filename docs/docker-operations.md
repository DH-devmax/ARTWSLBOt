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
6. Verify `/healthz`, website login, both workspace/account lists and project
   status. Native/AI readiness must still be false.

## Local Resource Budget

`compose.local.yaml` is an explicit resource-only overlay on the same root
Compose project, not another deployment path. It does not change images,
accounts, volumes, authentication, service topology or readiness gates.
It is deliberately not named `compose.override.yaml`, so production cannot
load the reduced limits implicitly.

| Service | Local CPU limit | Local memory limit |
| --- | --- | --- |
| PostgreSQL | 1 | 1 GiB |
| API / native receiver / decoder | 1 | 512 MiB |
| Go heatbot | 0.5 | 256 MiB |
| Web | 0.25 | 128 MiB |

The five persistent services total 1984 MiB (1.94 GiB) and 2.85 CPU units in
configured limits, not reservations or measured capacity. Migrate/bootstrap
are separately capped at 0.5 CPU / 256 MiB while running. Local swap limits
equal memory limits; Docker VM, build tools and other projects require their
own headroom. This budget is for local functional development, not the promised
production throughput, group capacity or long-duration acceptance tests.

```sh
docker compose -f compose.yaml -f compose.local.yaml config --quiet
docker compose -f compose.yaml -f compose.local.yaml up -d --no-build --wait
docker compose -f compose.yaml -f compose.local.yaml ps
```

Applying changed Compose settings may recreate containers. For the initial
resource-only adjustment of a live development stack, `docker update` can
change the existing containers' CPU/memory/swap limits without restarting
them; verify container IDs, restart counts and health afterward. That online
adjustment is not persistent Compose configuration: continue using the
explicit local overlay on subsequent local `up` commands.

Production and target-machine acceptance use only `compose.yaml` and the
reviewed production images. Restore those limits in a maintenance window;
do not accidentally apply the local overlay to production. An online
`docker update` does not update Compose's configuration-hash label: plain
`up` alone might retain the manually reduced limits. On the same adjusted
stack, explicitly recreate the existing services during that maintenance
window and inspect their limits afterward:

```sh
docker compose -f compose.yaml up -d --no-build --force-recreate --wait \
  postgres api heatbot web
```

This stops/recreates connections and must not be run as an online check.
Named volumes remain attached; no account reauthentication or native action
is part of this resource command. Never use
`down -v`, volume prune, or blanket system prune to switch resource budgets.
Disk cleanup should target verified unreferenced project images without
`--force`; retain current runtime images, data volumes and unrelated projects.
Removing image/build cache frees disk, not running-container memory.

## Opt-In Development

Frontend Docker HMR and controlled API rebuild/restart are described
in [`docker-development.md`](docker-development.md). The development overlay is
not loaded automatically. Source changes do not update release containers.
Frontend-only activation uses `--no-deps`, preserving the existing API/native
owner. Never start a second API against the same live native accounts.
An opted-in development setup uses one mutable `:dev` image per changed
application component, separate from current/previous release retention below.
It is disposable working state, not an additional rollback version; do not
accumulate dated development images or delete the release rollback to make room.

## Default Local Delivery

On 2026-10-29 the owner authorized routine synchronization and necessary service
restarts after each completed, verified change to this existing local Compose
deployment. This replaces repeated approval of individual routine API restart
windows and includes a brief interruption of native sockets when API changes
require it. It is not an automatic watcher or permission to switch environments.

- Preserve the current release/development mode and local resource overlay.
  In development, source-only frontend changes use HMR; dependency changes
  rebuild the development Web image. In release mode, build and activate the
  matching Web candidate. HMR never substitutes for backend delivery.
- Build and verify before activation; review schema compatibility, active and
  unknown submissions, credentials and connection ownership before restarting.
  Failed checks or unresolved submissions block deployment for investigation.
- Recreate only affected services with the existing Compose project. A UI-only
  update does not restart API, PostgreSQL or heatbot. New API endpoints
  require the matching API/Web deployment. Preserve data volumes and limits.
- Back up and record the current images/state before replacement, verify the
  deployed images, health, asset versions and business/session preservation,
  and retain the immediately previous running release as specified below.
- Report the actual deployment and any remaining acceptance gaps. Destructive
  data removal, incompatible migrations and a new deployment target require
  separate confirmation. Native account tests and readiness acceptance remain
  separately scoped; routine deployment does not authorize real native actions.

The development helper keeps its explicit `--confirm-api-restart` flag as an
operator safeguard. An agent may supply it under this standing authorization
after the required review; it need not ask the owner for the same window again.

## Local Application Image Retention

Keep exactly two application versions per repository: `wsl-api:local` and
`wsl-web:local` identify the current local release; a dated `rollback-*` tag
identifies the immediately previous **running** release. An unused candidate
is not the previous running release. Record full image IDs in the protected
deployment inventory before moving tags.

Before each upgrade, tag the actual running API/heatbot and web image IDs for
rollback, stop the application writers, and back up the database and separately
protected configuration. Validate the forward migrations on an isolated restored
copy without starting the API or any native receiver. Then move the local tags,
run the new migrator, and recreate only API/heatbot/web with the local overlay.
Verify health, exact images, resource limits and business/receiver fingerprints.

Only after successful verification, remove obsolete `wsl-api`/`wsl-web`
references and the now-redundant candidate aliases using explicit
`docker image rm` commands without `--force`. On the next successful release,
the former current release becomes the sole previous release; remove the older
rollback image only after the new deployment passes these checks. Never infer
ownership of untagged images, delete shared database/Simon base images, or use
global image/system/volume pruning to enforce this policy.

After the 2026-10-29 avatar-preview deployment under standing local authorization,
both retained previous tags are `rollback-20261029-preavatar`, under `wsl-api` and
`wsl-web` respectively. They identify the immediately previous running images,
not unused candidates. Migration 022 was already present; the latest update
added no migration and recreated only API/Web. PostgreSQL, Simon and heatbot
retained their container IDs, images, mounts and resource limits. The avatar
preview and account-security APIs and matching Web are deployed, with real
native modification acceptance still pending.
The older API image still running heatbot is retained as
`wsl-api:heatbot-runtime`; it is an active component dependency, not another
rollback version. Never remove an active image to satisfy a version count.
Do not recreate heatbot as part of a frontend-only update. Older unused rollback
images and redundant candidate aliases were removed after health, state and
receiver verification. Current IDs and isolated restore/forward-migration
verification are recorded in `docs/test-report.md`. The `.env` file and its
MASTER_KEY were not changed.
Do not rebuild or retag the release images during a rollback investigation.

A rollback is a maintenance operation, not a health check. Freeze writes and
inspect unknown operations and forward-schema compatibility first. An approved
frontend-only rollback of the latest Web update can use this explicit override,
without restarting the API/native owner:

```sh
WSL_WEB_IMAGE=wsl-web:rollback-20261029-preavatar \
docker compose -f compose.yaml -f compose.local.yaml up -d --no-build \
  --no-deps --force-recreate --wait web
```

Backend rollback requires a separately reviewed compatible frontend/backend
combination and the standing local authorization or an agreed window. Do not combine the retained
component tags blindly. Do not run the old migrator or restore the pre-upgrade
database over new writes.
Keeping the old images is not proof that every new business state is compatible
with the old binary. Migration 020 changes native identity uniqueness and adds
platform/purpose constraints; migration 021 adds durable manual settings
operations. Migration 022 adds short-lived sealed account-security flows and
new operation kinds. The retained previous API understands 022 and its
verification flows but lacks the current-avatar resource route. Do not combine
the new Web with that API without reviewing the missing preview capability.
Freeze and reconcile any
preparing/submitting/unknown settings before reviewing an API rollback; an old
binary must not bypass their submission barriers. Include verification-flow
state and any changed account authentication/epoch in that review; do not run
the old migrator or downgrade the schema. Review schema and business
state before authorizing a code-only rollback. Do not run this example merely
to check health. No live rollback was executed during this deployment.

## Release

CI: repository and immutable migration checks, architecture imports/Compose
boundaries, Go race tests with libsodium, isolated PostgreSQL integration,
frontend build, hardened container smoke and full Compose startup.
Browser checks also run inside Docker, with synthetic writes and no native
signup/login/model calls. Clean product builds use `web/tsconfig.wsl.json`.
If Docker Desktop has exhausted its default network pools, the disposable
`tools/container_smoke.py` network accepts `WSL_SMOKE_SUBNET` as an unused private
IPv4 subnet between /24 and /28. Check existing networks first. This changes only
the temporary acceptance network; do not prune other projects or change the
production Compose network to make a smoke test pass.

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

Migration 014 switches authentication to desktop-only. Stop the API before its
consistent backup and migration so an old binary cannot restore a mobile
session during the transition. It fences saved credentials and enabled receivers,
without deleting accounts, group bindings or business evidence. Import an
independently verified desktop profile and authenticate manually after upgrade;
never relabel an Android profile or resume its old tickets. A code rollback
alone must not re-enable those sessions or reset epochs.

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

Simon and its private network are removed. API and heatbot cache their own
Linux process/cgroup/Go metrics every five seconds. API reads heatbot's fixed
internal snapshot and the Web static root; it never proxies arbitrary routes.
PostgreSQL observation is restricted to `current_database()` every 60 seconds.
There are no host mounts/PID namespace, Docker socket, process enumeration or
environment/command-line reads. Monitoring remains superadmin-only.
The resource page reports service-specific values, not host or project totals.
