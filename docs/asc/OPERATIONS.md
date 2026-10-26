# Operating ASC Hub

This page is for the person who runs a hub: deploy, volumes, ports, backup and restore, upgrade, and the choice of database. Names such as `hub.example.org` are placeholders. For what the pieces do, read [ARCHITECTURE.md](ARCHITECTURE.md). For one concrete staging setup, read [STAGING.md](STAGING.md).


## What runs

ASC Hub is one container. The image (`ghcr.io/<owner>/asc-hub`, built from `Dockerfile.allinone`) holds the hub (the Forgejo v16 fork), the `asc` binary and `git`. Its entrypoint, `asc-hub-entrypoint` under `dumb-init`, starts both programs and keeps them together:

1. On every start it makes sure the directories exist and, on the first start, generates the secrets the two programs share: the internal token, the webhook secret and the hub's `SECRET_KEY`, all under `/var/lib/gitea/bootstrap` (mode 0400, kept across restarts).
2. It writes the `asc serve` configuration (`/var/lib/gitea/asc/asc-serve.toml`) from the environment, then runs `asc serve migrate`.
3. It starts the hub with the `[asc]` defaults filled in (`ENABLED`, `SERVER_URL=http://127.0.0.1:7890`, `PUBLIC_URL` from `ROOT_URL`, `WRITER=asc`, `NEW_REPOSITORIES=asc`, `BRIDGE=eager`), waits for `GET /api/healthz`, then starts `asc serve` on `127.0.0.1:7890`.
4. If `ASC_HUB_ADMIN_EMAIL` is set and no marker exists, it creates the first administrator once.
5. It waits for either program to exit. If one does, it stops the other and exits with a non-zero status, so Docker restarts the whole container. `SIGTERM` and `SIGINT` are forwarded to both and end in exit 0; give the container `stop_grace_period: 30s`.

| Program | Port | Public |
| --- | --- | --- |
| Hub, web and API | 3000 | yes, behind your TLS proxy |
| Hub, git over SSH (built-in server) | 2222 in the container | yes, publish it |
| `asc serve` | 127.0.0.1:7890 in the container | no |

The hub and `asc serve` meet on loopback inside the container, so the hub's client needs no TLS to reach it, and `asc serve` has no route or published port of its own. Clients reach the ASC API through the hub's address: `GET /.well-known/asc` announces `[asc] PUBLIC_URL`, which defaults to your `ROOT_URL`.

The build copies the `asc` binary from the published `asc-serve` image (`--build-arg ASC_IMAGE=ghcr.io/<owner>/asc-serve:<tag>`) into a Debian base (the label `io.asc.serve.image` records which). The hub-only image (`Dockerfile.rootless`, no `asc serve`) still builds; [STAGING.md](STAGING.md) describes a staging setup that runs `asc serve` as a separate service next to it.

Both programs run as the same user (uid 1000) and share the volume. That is simpler than two containers, and it also means that a bug in one can read the other's secrets. The configuration and secret files are mode 0600 and 0400, and the processes get separate environments: `asc serve` gets the loopback-http opt-in, the hub does not, and the hub does not get `asc serve`'s database URL or object store keys.

Status: the image is new. `Dockerfile.allinone`, the entrypoint and the health check were written and checked by reading them and with `bash -n`; the image has not been built or started yet (no container runtime was used where it was written). Treat the first CI build and the first start as the test, and report what breaks.

## Deploy with Compose

The README has the one-service file. Notes for a real deployment:

1. Pin the image to the 12-character commit tag. `:asc-v16` moves with every push to `asc/v16`.
2. Put a TLS proxy in front of port 3000 and set `ROOT_URL` to the public https URL. Port 3000 itself is plain HTTP. Without `ROOT_URL` the container falls back to `http://localhost:3000/` and logs a warning.
3. Publish the SSH port. `SSH_PORT` is the port shown in clone URLs, `SSH_LISTEN_PORT` is the port the built-in server listens on in the container (the entrypoint sets it to 2222), and `SSH_DOMAIN` is the host name shown (default: the host of `ROOT_URL`). On the shared server use host port 22223 (`"22223:2222"` with `SSH_PORT=22223`), because 22222 is taken.
4. `DISABLE_REGISTRATION` is `true` and `INSTALL_LOCK` is `true` unless you set them. With the lock on there is no web installer; the installer would write a literal internal token into `app.ini`, which conflicts with the token file.
5. A read-only root filesystem should work if `/tmp` is a tmpfs (the temp directory is `/tmp/gitea`) and `/var/lib/gitea` is a volume. [STAGING.md](STAGING.md) runs the hub-only image that way with all capabilities dropped and `no-new-privileges`; this image has not been run with those options yet.
6. The image's health check needs both programs: `GET /api/healthz` on the hub and `GET /readyz` on `asc serve`. `/readyz` fails when the database of `asc serve` does not answer or its outbox lags.

### Variables read by the entrypoint

Each `FORGEJO__<section>__<KEY>` variable you set wins over the defaults above. The plain hub variables (`APP_NAME`, `ROOT_URL`, `SSH_DOMAIN`, `SSH_PORT`, `SSH_LISTEN_PORT`, `HTTP_PORT`, `DB_TYPE`, `DB_HOST`, `DB_NAME`, `DB_USER`, `DB_PASSWD`, `INSTALL_LOCK`, `DISABLE_REGISTRATION`, `REQUIRE_SIGNIN_VIEW`, `SECRET_KEY`) fill `app.ini` on the first start only (`docker-setup.sh` writes it only when it does not exist). Afterwards use `FORGEJO__...` variables, which are applied at every start, or edit `app.ini`. `APP_NAME` defaults to `ASC Hub`.

| Variable | Default | Meaning |
| --- | --- | --- |
| `ROOT_URL` | `http://localhost:3000/` | Public URL of the hub. Also the default of `[asc] PUBLIC_URL`. |
| `ASC_HUB_ADMIN_EMAIL` | none | Creates the first administrator once. The password goes to `/var/lib/gitea/bootstrap/admin-password` (mode 0600). |
| `ASC_HUB_ADMIN_USER` | `asc-admin` | User name of that administrator. |
| `ASC_HUB_BRIDGE` | `eager` | Default of `[asc] BRIDGE` (`eager`, `lazy`, `off`). `eager` turns on `bridge_promote` and the git layer of `asc serve`. |
| `ASC_HUB_START_TIMEOUT` | `120` | Seconds to wait for the hub before giving up. |
| `ASC_DATABASE_URL` | SQLite file `/var/lib/gitea/asc/asc.db` | Database of `asc serve`, for example a Postgres URL. |
| `ASC_S3_ENDPOINT`, `ASC_S3_BUCKET` (`asc-hub`), `ASC_S3_REGION` (`garage`), `ASC_S3_ACCESS_KEY_ID`, `ASC_S3_SECRET_ACCESS_KEY` | none | With an endpoint, the objects of `asc serve` go to an S3-compatible bucket (path-style) instead of `/var/lib/gitea/asc/objects`. This is the operator's storage, not a per-user setting. |
| `ASC_CACHE_MEMORY`, `ASC_CACHE_DISK`, `ASC_TEXT_MEMORY` | `256MiB`, `2GiB`, `256MiB` | Cache sizes of `asc serve`. |
| `ASC_GIT_ENABLED` | `false` | Git layer of `asc serve` when the bridge is `lazy` or `off` (always on for `eager`). |
| `ASC_LOG`, `ASC_LOG_FORMAT` | `info`, `text` | Log level and format of `asc serve`. |

The values that go into the `asc serve` configuration file may not contain quotes or line breaks; the entrypoint refuses them. These names are read by the entrypoint script (`docker/allinone/usr/local/bin/asc-hub-entrypoint`), not by the hub program.

Running the image with a command, for example `docker run IMAGE forgejo admin user list`, skips the supervisor and runs the hub's own entrypoint with that command.

## Other shapes

- **Hub image and `asc serve` as two services.** Build `Dockerfile.rootless` (no `asc serve` inside), run `asc serve` from the ASC distribution as a second service in the hub's network namespace (`network_mode: "service:hub"`), and set the `[asc]` keys, the internal token and the shared repository directory yourself. The contract: `[asc] SERVER_URL` is where the hub dials; both read the same internal token (`[security] INTERNAL_TOKEN_URI`, a `file://` URI); `asc serve` asks the hub at `/api/internal/asc/authz` (and `/ssh-key`, `/request-merge`) what a credential may do; with `BRIDGE = eager`, `asc serve` needs the hub's `git/repositories` directory read-write at the same path and the same user id. The configuration of `asc serve` itself is documented with `asc serve`.
- **Separate hosts.** Use `https` for `[asc] SERVER_URL`; the hub's client refuses plain `http` to a host that is not loopback.

## Coolify

Create a **Docker Compose** resource from the one-service file:

1. Set the resource's domain on the `hub` service, port 3000, so Coolify's proxy terminates TLS and routes to it. Coolify's magic variable for that is `SERVICE_FQDN_<SERVICE>_3000` (staging uses `SERVICE_FQDN_FORGEJO_3000` for a service named `forgejo`). Set `ROOT_URL` to the same https address.
2. Publish the SSH port on the host (`22223:2222` on the shared server, with `SSH_PORT=22223`) and open it in the firewall. Coolify's HTTP proxy does not carry SSH.
3. Keep all state in the one named volume at `/var/lib/gitea`, so redeploys keep the data, the secrets and the `asc serve` state.
4. Set the image tag in an environment variable that the compose file requires, and change it per deploy. Do not use a moving tag.
5. Set `ASC_HUB_ADMIN_EMAIL` for the first deploy, read the password file once from the Coolify terminal, change the password, delete the file.

[STAGING.md](STAGING.md) describes the older staging resource, with the hub-only image and SQLite; its compose file belongs to the ASC repository, not this one.

## Volumes

One volume holds all state of the all-in-one image.

| Path in the container | Content | Back up |
| --- | --- | --- |
| `/var/lib/gitea` | `custom/conf/app.ini`; `data/` (the SQLite file `data/gitea.db` when used, sessions, avatars, attachments, logs); `git/repositories` (the bare repositories, which for ASC repositories are the git bridge); `git/lfs`; `bootstrap/` (the internal token, webhook secret, `SECRET_KEY`, the admin password file until you delete it); `asc/` (`asc-serve.toml`, `asc.db`, `objects/`, `cache/`, `gitview/`, `home/`); `asc-bridge/` (workspaces of a lazy bridge) | yes |
| `/tmp` | scratch space (`/tmp/gitea`) | no |

The path `/var/lib/gitea` comes from `GITEA_WORK_DIR` in `Dockerfile.allinone`; the sub-paths of the hub come from `docker/rootless/etc/templates/app.ini`, those of `asc serve` from the entrypoint. With a bind mount instead of a named volume, the host directory must be owned by uid 1000.

## Database: SQLite and Postgres

| | SQLite | Postgres |
| --- | --- | --- |
| Use for | staging, a trial, a small single-node hub | production |
| Setup | none: `DB_TYPE=sqlite3` is the default, the file is `data/gitea.db` in the data volume | a separate server and database; first start: `DB_TYPE=postgres`, `DB_HOST=db:5432`, `DB_NAME`, `DB_USER`, `DB_PASSWD`; later: `FORGEJO__database__*` |
| Backup | stop the hub or use `forgejo dump`; the file is in the data volume | `pg_dump`, or your database's snapshots |
| Tradeoff | one writer, lives and dies with the volume | one more service to run, back up and patch |

The hub's database holds only the hub's data. `asc serve` has its own database: SQLite (`/var/lib/gitea/asc/asc.db`) by default, or Postgres through `ASC_DATABASE_URL`. Choose it with the same reasoning and keep the two backed up together.

## Backup and restore

The hub and `asc serve` each hold half of the truth, and each refers to the other (pull requests point at asc changes; asc refs feed the bridge). With SQLite for both and the files on the one volume, a consistent copy of the volume is a consistent backup. With Postgres or an S3 bucket in use, take those backups at the same point as the volume.

Backup:

1. Quiesce writes: stop the container, or take a consistent snapshot of the volume and of any external database.
2. The `/var/lib/gitea` volume. It includes the secrets in `bootstrap/`; without the same `SECRET_KEY` the stored secrets of the hub (two-factor seeds, webhook secrets) cannot be read, and without the same internal token the two programs cannot talk (the entrypoint would generate new ones, which is also fine as long as both are regenerated together).
3. An external hub database (`pg_dump` or snapshots), an external `asc serve` database and an S3 bucket, if you configured them.
4. Optionally `forgejo dump --file <path>` (run in the container as the container user, with the file on a writable mount) writes a zip of the hub's files and a database dump; add `--skip-repository` if you back the repositories up separately. It does not contain `asc serve`'s state.

Restore: restore the volume (and any external database or bucket) onto empty storage and start the container. The entrypoint starts `asc serve` after the hub. If the git bridge directories are older than asc's refs, `BRIDGE = eager` catches them up from asc's outbox. A `lazy` bridge rebuilds itself when the fingerprint of asc's refs differs from the one it last recorded. After a restore, run the repository health check (the `repo_health_check` cron task, in the admin monitor). For an ASC repository it asks asc to verify that every ref's closure exists and that the git id map round-trips, instead of running `git fsck` (`services/repository/check_asc.go`).

Test a restore before you need one.

## Upgrade and rollback

1. Take the backup above.
2. Set the image tag to the new commit tag and redeploy. The hub applies its database migrations at start; the ASC tables are `v17b` to `v17h` in `models/forgejo_migrations`. Migrations only go forward.
3. The hub and `asc serve` are upgraded together because they ship in one image: the `asc` binary comes from the `asc-serve` image named by the build argument `ASC_IMAGE`, and the `asc.v1` stubs the hub speaks are in `modules/ascproto`, generated from the ASC repository's `proto` directory. `asc serve migrate` runs at every start, before the hub.
4. Roll back by setting the previous tag. If the older image refuses to start because the schema moved forward, restore the backup of step 1.

Upstream security releases are merged into `asc/v16` first; see [FORK.md](FORK.md).

## First administrator

Set `ASC_HUB_ADMIN_EMAIL` (and optionally `ASC_HUB_ADMIN_USER`, default `asc-admin`) on the first start. After the hub answers, the entrypoint creates the administrator with a generated password and writes it to `/var/lib/gitea/bootstrap/admin-password` (mode 0600), then leaves a marker so it does not run again:

```sh
docker compose exec hub cat /var/lib/gitea/bootstrap/admin-password
```

Sign in, change the password (the account must), and delete the file. Without `ASC_HUB_ADMIN_EMAIL`, run once, as the container user:

```sh
forgejo admin user create --admin --username asc-admin --email admin@example.org \
  --random-password --must-change-password=true
```

The command prints a generated password once. It also accepts `--password`, but a password in an argument is visible in `ps` and shell history, so use `--random-password`. To get an access token for `asc auth login` at the same time the command has `--access-token`, `--access-token-name` and `--access-token-scopes`.

## Checks after a deploy

- `curl -fsS https://hub.example.org/api/healthz`
- `curl -fsS https://hub.example.org/.well-known/asc` returns `{"server":"...","repos":"*"}`; a 404 means `[asc] ENABLED` is false or `SERVER_URL` is empty.
- Create a repository, then `asc clone https://hub.example.org/<owner>/<repo>` and `git clone https://hub.example.org/<owner>/<repo>.git`.
- `docker inspect --format '{{.State.Health.Status}}' <container>` is `healthy` only when both the hub and `asc serve` answer.
- Look in the container log for `asc-hub:` lines (they name each step) and for `[asc]` validation errors; an invalid `[asc]` value stops the start.
