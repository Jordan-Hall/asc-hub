# Operating ASC Hub

This page is for the person who runs a hub: deploy, volumes, ports, backup and restore, upgrade, the choice of database, and how to check a deployment. Names such as `hub.example.org` are examples. For what the pieces do, read [ARCHITECTURE.md](ARCHITECTURE.md). For one concrete staging setup, read [STAGING.md](STAGING.md).


## What runs

ASC Hub is one container. The image (`ghcr.io/<owner>/asc-hub`, built from `Dockerfile.allinone`) holds the hub (the Forgejo v16 fork), the `asc` binary and `git`. Its entrypoint, `asc-hub-entrypoint` under `dumb-init`, starts both programs and keeps them together:

1. **Checks the volume and the environment before it writes anything.** It refuses to run as root; it needs `/var/lib/gitea` to be writable and every file in the top three levels of it to belong to the container user (uid 1000); it takes an exclusive lock on `/var/lib/gitea/.asc-hub.lock`, which the hub and `asc serve` inherit, so a second container on the same volume, or the backup tool, is refused while either program runs. It then validates the variables it reads (see the table below). Each refusal names the variable or path and exits with status 1.
2. On the first start it generates the secrets the two programs share: the internal token, the webhook secret and the hub's `SECRET_KEY`, all under `/var/lib/gitea/bootstrap` (mode 0400, kept across restarts). On every start it writes the `asc serve` configuration (`/var/lib/gitea/asc/asc-serve.toml`, mode 0600) from the environment, then runs `asc serve migrate`.
3. It starts the hub with the `[asc]` defaults filled in (`ENABLED`, `SERVER_URL=http://127.0.0.1:7890`, `PUBLIC_URL` from `ROOT_URL`, `WRITER=asc`, `NEW_REPOSITORIES=asc`, `BRIDGE=eager`), waits for `GET /api/healthz`, then starts `asc serve` on `127.0.0.1:7890`.
4. If `ASC_HUB_ADMIN_EMAIL` is set and no marker exists, it creates the first administrator once.
5. It waits for either program to exit. If one does, it stops the other and exits with a non-zero status (the status of the program that ended), so Docker restarts the whole container. `SIGTERM` and `SIGINT`, also during start, are forwarded to both programs and end in exit 0; the programs get 25 seconds, so give the container `stop_grace_period: 30s`.

| Program | Port | Public |
| --- | --- | --- |
| Hub, web and API | 3000 | yes, behind your TLS proxy |
| `asc serve` | 127.0.0.1:7890 in the container | no |

HTTPS is the only inbound port a hub needs. The image starts no SSH server and exposes only port 3000; git over SSH is an opt-in ([Git over SSH](#git-over-ssh-optional)). Clients use `asc auth login --host <this hub>` with an access token, and `git` over HTTPS through the git-compatible endpoint.

The hub and `asc serve` meet on loopback inside the container, so the hub's client needs no TLS to reach it, and `asc serve` has no route or published port of its own. Clients reach the ASC API through the hub's address: `GET /.well-known/asc` announces `[asc] PUBLIC_URL`, which defaults to your `ROOT_URL`.

The build copies the `asc` binary from the published `asc-serve` image (`--build-arg ASC_IMAGE=ghcr.io/<owner>/asc-serve:<tag>`) into a Debian base (the label `io.asc.serve.image` records which). The hub-only image (`Dockerfile.rootless`, no `asc serve`) still builds; [STAGING.md](STAGING.md) describes a staging setup that runs `asc serve` as a separate service next to it.

Both programs run as the same user (uid 1000) and share the volume. That is simpler than two containers, and it also means that a bug in one can read the other's secrets. The configuration and secret files are mode 0600 and 0400, and the processes get separate environments: `asc serve` gets only `PATH`, locale, proxy and certificate variables, the loopback-http opt-in, its own settings and the object store keys (as `AWS_*`), so not the hub's `SECRET_KEY` or database password; the hub gets everything except `ASC_DATABASE_URL`, the object store keys and the opt-in.

### What is tested

The entrypoint, the health check, the backup tool and `scripts/smoke.sh` have tests that run them against fake programs (`tests/allinone`, run by the `image-scripts` job of `.github/workflows/asc-hub-test.yml`): first start and restart, the secrets and the configuration written, the environment of each program, the first administrator, either program killed, `SIGTERM` and `SIGINT`, the volume checks, a second instance, and the backup round trip including archives built to escape the target directory. The tests do not run the real hub or the real `asc serve`, and the image itself is built only by `.github/workflows/asc-hub-image.yml`. After the first deployment of a new image tag, run the smoke test below; it is the check against the real programs.

## Deploy with Compose

The README has the one-service file. Notes for a real deployment:

1. Pin the image to the 12-character commit tag. `:asc-v16` moves with every push to `asc/v16`.
2. Put a TLS proxy in front of port 3000 and set `ROOT_URL` to the public https URL. Port 3000 itself is plain HTTP. Without `ROOT_URL` the container falls back to `http://localhost:3000/` and logs a warning; a plain `http://` address of any other host is refused, because clients send their credentials to the public address.
3. Publish no SSH port. Git over SSH is off by default; to use it, see [Git over SSH](#git-over-ssh-optional).
4. `DISABLE_REGISTRATION` is `true` and `INSTALL_LOCK` is `true` unless you set them. With the lock on there is no web installer; the installer would write a literal internal token into `app.ini`, which conflicts with the token file.
5. The volume at `/var/lib/gitea` must belong to uid 1000. A named volume does (the image creates the directory with that owner). For a bind mount run `chown -R 1000:1000 <directory>` first; on Kubernetes set `fsGroup: 1000` and `runAsUser: 1000`. The container does not fix ownership itself, because it runs without privileges; it stops and says which path is wrong.
6. A read-only root filesystem needs `/tmp` as a tmpfs (the hub's temporary directory is `/tmp/gitea`) and `/var/lib/gitea` as a volume. [STAGING.md](STAGING.md) runs the hub-only image that way with all capabilities dropped and `no-new-privileges`. The all-in-one image writes nowhere else, but it has not been run with those options by us.
7. The image's health check needs both programs: `GET /api/healthz` on the hub and `GET /readyz` on `asc serve`. `/readyz` fails when the database of `asc serve` does not answer or its outbox lags. The check runs every 30 seconds and the container is marked unhealthy after three failures; the first 120 seconds are a grace period. Docker itself does not restart an unhealthy container: `restart: unless-stopped` restarts it when a program exits, and an orchestrator that acts on health (Coolify, Swarm, Kubernetes with an exec probe of `/usr/local/bin/asc-hub-healthcheck`) does the rest.
8. The check reads the same port variables as the entrypoint. If you set `FORGEJO__server__PROTOCOL=https` for the hub's own listener, the check, which speaks plain http, fails; terminate TLS in the proxy.

### Variables read by the entrypoint

Each `FORGEJO__<section>__<KEY>` variable you set wins over the defaults above. The plain hub variables (`APP_NAME`, `ROOT_URL`, `HTTP_PORT`, `DB_TYPE`, `DB_HOST`, `DB_NAME`, `DB_USER`, `DB_PASSWD`, `INSTALL_LOCK`, `DISABLE_REGISTRATION`, `REQUIRE_SIGNIN_VIEW`, `SECRET_KEY`) fill `app.ini` on the first start only (`docker-setup.sh` writes it only when it does not exist). Afterwards use `FORGEJO__...` variables, which are applied at every start, or edit `app.ini`. `APP_NAME` defaults to `ASC Hub`.

| Variable | Default | Meaning |
| --- | --- | --- |
| `ROOT_URL` | `http://localhost:3000/` | Public URL of the hub. Also the default of `[asc] PUBLIC_URL`. Must be https unless the host is `localhost` or a loopback address. |
| `ASC_HUB_ADMIN_EMAIL` | none | Creates the first administrator once. Must look like an e-mail address. The password goes to `/var/lib/gitea/bootstrap/admin-password` (mode 0600). |
| `ASC_HUB_ADMIN_USER` | `asc-admin` | User name of that administrator: letters, digits, `-`, `_`, `.`, starting with a letter or digit, at most 39 characters. |
| `ASC_HUB_BRIDGE` | `eager` | Default of `[asc] BRIDGE` (`eager`, `lazy`, `off`). `eager` turns on `bridge_promote` and the git layer of `asc serve`. |
| `ASC_HUB_SSH_ENABLE` | `false` | `true` or `false` (anything else stops the container). `false`: no SSH server, no sshd, `[asc] SSH_ENABLED=false`, no SSH keys page and no SSH clone address in the UI. The entrypoint forces `DISABLE_SSH=true` and `START_SSH_SERVER=false` whatever else is set. `true`: see [Git over SSH](#git-over-ssh-optional). `asc serve`'s own SSH listener stays off either way. |
| `ASC_HUB_START_TIMEOUT` | `120` | Seconds to wait for the hub before giving up (1 to 9999). |
| `ASC_HUB_ASC_PORT` | `7890` | Loopback port of `asc serve`; also the default port in `[asc] SERVER_URL` and the port the health check asks. Change it only if 7890 is taken (for example with host networking). |
| `ASC_DATABASE_URL` | SQLite file `/var/lib/gitea/asc/asc.db` | Database of `asc serve`: a `sqlite://`, `postgres://` or `postgresql://` URL. |
| `ASC_S3_ENDPOINT`, `ASC_S3_BUCKET` (`asc-hub`), `ASC_S3_REGION` (`garage`), `ASC_S3_ACCESS_KEY_ID`, `ASC_S3_SECRET_ACCESS_KEY` | none | With an endpoint, the objects of `asc serve` go to an S3-compatible bucket (path-style) instead of `/var/lib/gitea/asc/objects`. The endpoint must be an `https://` URL without credentials, query or fragment; both keys are required with it. This is the operator's storage, not a per-user setting. |
| `ASC_CACHE_MEMORY`, `ASC_CACHE_DISK`, `ASC_TEXT_MEMORY` | `256MiB`, `2GiB`, `256MiB` | Cache sizes of `asc serve`: a number and a unit such as `MiB`. |
| `ASC_GIT_ENABLED` | `false` | Git layer of `asc serve` when the bridge is `lazy` or `off` (always on for `eager`): `true` or `false`. |
| `ASC_LOG`, `ASC_LOG_FORMAT` | `info`, `text` | Log filter and format (`text` or `json`) of `asc serve`. |

The values that go into the `asc serve` configuration file may not contain quotes or line breaks. A value that does not fit stops the container before anything is written, with a message that names the variable and never prints a secret. The names are read by the entrypoint script (`docker/allinone/usr/local/bin/asc-hub-entrypoint`), not by the hub program; `tests/allinone/compose_test.go` fails when this list and the script disagree.

Running the image with a command, for example `docker run IMAGE forgejo admin user list`, skips the supervisor and runs the hub's own entrypoint with that command.

### Git over SSH (optional)

Only if the host can open an extra port. With `ASC_HUB_SSH_ENABLE=true` the entrypoint starts the hub's built-in SSH server and sets `[asc] SSH_ENABLED=true`; the hub then shows the SSH keys page, SSH deploy keys and an SSH clone address. You publish the port yourself:

| Variable | Default when enabled | Meaning |
| --- | --- | --- |
| `SSH_LISTEN_PORT` | `2222` | Port of the built-in server inside the container. |
| `SSH_PORT` | `2222` | Port shown in clone addresses; it must equal the host port you publish. |
| `SSH_DOMAIN` | host of `ROOT_URL` | Host name shown in clone addresses. |

Add the published port to the Compose file (`"<host port>:2222"`, with `SSH_PORT=<host port>`) and open it in the firewall. Coolify's HTTP proxy does not carry SSH. These three variables are ignored (the entrypoint logs a warning) while `ASC_HUB_SSH_ENABLE` is not `true`. In a hub run without the image, set `[asc] SSH_ENABLED = true` (default `false`); with `[asc] ENABLED = true` and `SSH_ENABLED = false` the hub forces `[server] DISABLE_SSH = true`.

## Other shapes

- **Hub image and `asc serve` as two services.** Build `Dockerfile.rootless` (no `asc serve` inside), run `asc serve` from the ASC distribution as a second service in the hub's network namespace (`network_mode: "service:hub"`), and set the `[asc]` keys, the internal token and the shared repository directory yourself. The contract: `[asc] SERVER_URL` is where the hub dials; both read the same internal token (`[security] INTERNAL_TOKEN_URI`, a `file://` URI); `asc serve` asks the hub at `/api/internal/asc/authz` (and `/ssh-key`, `/request-merge`) what a credential may do; with `BRIDGE = eager`, `asc serve` needs the hub's `git/repositories` directory read-write at the same path and the same user id. The configuration of `asc serve` itself is documented with `asc serve`. The backup tool and the lock below belong to the all-in-one image only.
- **Separate hosts.** Use `https` for `[asc] SERVER_URL`; the hub's client refuses plain `http` to a host that is not loopback.

## Coolify

The ASC repository ships the Compose file for this image in `deploy/coolify/asc-hub-allinone.compose.yml`. Create a **Docker Compose** resource from it, or from the one-service file in the README:

1. Set the resource's domain on the `hub` service, port 3000, so Coolify's proxy terminates TLS and routes to it. Coolify's magic variable for that is `SERVICE_FQDN_<SERVICE>_3000` (the file uses `SERVICE_FQDN_HUB_3000` for the service `hub`). Set `ROOT_URL` to the same https address.
2. Publish no SSH port: Coolify's HTTP proxy carries HTTPS only, and the file does not publish one. Clients use `asc auth login --host <hub>` and HTTPS clones. (Git over SSH is optional; see [Git over SSH](#git-over-ssh-optional).)
3. Keep all state in the one named volume at `/var/lib/gitea`, so redeploys keep the data, the secrets and the `asc serve` state.
4. Set the image tag in an environment variable that the compose file requires (`${VAR:?message}`), and change it per deploy. Do not use a moving tag.
5. Set `ASC_HUB_ADMIN_EMAIL` for the first deploy, read the password file once from the Coolify terminal, change the password, delete the file.
6. Keep every secret (S3 keys, database URL) in Coolify's environment, never in the file. `tests/allinone/compose_test.go` checks a Compose file against the image: set `ASC_ALLINONE_COMPOSE_FILES` to its path and run `go test ./tests/allinone/`. It fails for an unpinned or unguarded image tag, a variable the image does not read, a literal value for a variable that looks like a secret, a missing `ROOT_URL`, a published SSH port without `ASC_HUB_SSH_ENABLE: "true"` (or a published one that does not match `SSH_PORT`), a `stop_grace_period` under 30 seconds, no restart policy, a state path that is not a named volume, and privileged mode.

[STAGING.md](STAGING.md) describes the older staging resource, with the hub-only image and SQLite; its compose file belongs to the ASC repository, not this one.

## Volumes

One volume holds all state of the all-in-one image.

| Path in the container | Content | Back up |
| --- | --- | --- |
| `/var/lib/gitea` | `custom/conf/app.ini`; `data/` (the SQLite file `data/gitea.db` when used, sessions, avatars, attachments, logs); `git/repositories` (the bare repositories, which for ASC repositories are the git bridge); `git/lfs`; `bootstrap/` (the internal token, webhook secret, `SECRET_KEY`, the admin password file until you delete it); `asc/` (`asc-serve.toml`, `asc.db`, `objects/`, `cache/`, `gitview/`, `home/`); `asc-bridge/` (workspaces of a lazy bridge); `.asc-hub.lock` | yes, except `asc/cache` and the lock file |
| `/tmp` | scratch space (`/tmp/gitea`) | no |

The path `/var/lib/gitea` comes from `GITEA_WORK_DIR` in `Dockerfile.allinone`; the sub-paths of the hub come from `docker/rootless/etc/templates/app.ini`, those of `asc serve` from the entrypoint. The volume must be owned by uid 1000 (see Deploy, point 5).

## Database: SQLite and Postgres

| | SQLite | Postgres |
| --- | --- | --- |
| Use for | staging, a trial, a small single-node hub | production |
| Hub: setup | none: `DB_TYPE=sqlite3` is the default, the file is `data/gitea.db` in the data volume | a separate server and database; first start: `DB_TYPE=postgres`, `DB_HOST=db:5432`, `DB_NAME`, `DB_USER`, `DB_PASSWD`; later: `FORGEJO__database__*` |
| `asc serve`: setup | none: `/var/lib/gitea/asc/asc.db` | `ASC_DATABASE_URL=postgres://user:password@db:5432/asc` (percent-encode the password); the role needs to create tables in its schema, because `asc serve migrate` runs at every start |
| Backup | stop the container and back up the volume with the backup tool (below) | `pg_dump` of each database, taken while the container is stopped, together with the volume |
| Tradeoff | one writer, lives and dies with the volume | one more service to run, back up and patch |

Choose before the first repository exists. The hub's database holds only the hub's data. `asc serve` has its own database, and the two refer to each other, so choose both the same way and back them up together. A hub on SQLite can move to Postgres: `forgejo dump --database postgres` writes the hub's database as Postgres SQL inside the dump file, which you load into an empty database before the first start on Postgres. Try it on a copy first. This page documents no conversion of the `asc serve` database; decide it before first use.

## Backup and restore

The hub and `asc serve` each hold half of the truth, and each refers to the other (pull requests point at asc changes; asc refs feed the bridge). With SQLite for both and the files on the one volume, a copy of the volume taken while the container is stopped is a consistent backup. With Postgres or an S3 bucket in use, take those backups at the same point.

The image carries a tool for the volume, `asc-hub-backup`. It takes the volume's lock, so it refuses while a hub runs on the volume (including a container that is still stopping), writes an archive that has a manifest as its first entry, leaves out `asc/cache` (rebuilt on start), and checks the archive it wrote. It tells you which parts live outside the volume.

```sh
docker compose stop hub
docker volume ls                       # the volume is named <project>_hub-data
IMAGE=ghcr.io/<owner>/asc-hub:<sha12>   # the image you run

# Back up. The archive goes to standard output, so no directory on the host needs permissions.
docker run --rm -v <project>_hub-data:/var/lib/gitea --entrypoint asc-hub-backup "$IMAGE" \
  create --data /var/lib/gitea --out - > hub-backup-$(date +%F).tar.gz
sha256sum hub-backup-*.tar.gz > hub-backup.sha256

# Check the archive (complete, manifest first, only files and directories, safe names).
# From standard input the archive is copied to the container's /tmp for the check.
docker run --rm -i --entrypoint asc-hub-backup "$IMAGE" verify --in - < hub-backup-2026-10-26.tar.gz

docker compose start hub
```

`create` stops with an error, and writes no archive, when the volume holds a link or a special file (the hub does not create any), when the output file exists, or when the directory is not an ASC Hub volume. With `--out FILE` instead of `-` (for example with a bind mount at `/backup` owned by uid 1000) it also writes `FILE.sha256`, and `verify` and `restore` check it when it sits next to the archive.

Restore never merges into a volume and never deletes anything: it unpacks onto an empty volume.

```sh
docker compose stop hub
docker volume create <project>_hub-data-restored
docker run --rm -i -v <project>_hub-data-restored:/var/lib/gitea --entrypoint asc-hub-backup "$IMAGE" \
  restore --data /var/lib/gitea --in - < hub-backup-2026-10-26.tar.gz
# point the compose file at the new volume, start the container, check it (below),
# and delete the old volume only after that
```

Restore refuses an archive that is incomplete, has a failing checksum, lacks the manifest, or holds a link, device, absolute name, `..` part or backslash in a name, before it writes a byte; it refuses a target that is not empty. Reading the archive from standard input needs free space on the new volume for one more copy of the archive, which it deletes afterwards. File modes (0400 for the secrets) are kept, and the owner becomes the user who runs the tool (uid 1000 in the image).

The volume holds the secrets in `bootstrap/`. Without the same `SECRET_KEY` the stored secrets of the hub (two-factor seeds, webhook secrets) cannot be read, and without the same internal token the two programs cannot talk. Keep the archive as private as the volume.

With Postgres or S3 in use, the archive does not hold that data, and `create` and `restore` say so. Take those at the same moment as the archive, with the container stopped:

```sh
pg_dump -Fc -h db -U hub -f hub-db.dump hub_database      # the hub's database
pg_dump -Fc -h db -U asc -f asc-db.dump asc_database      # asc serve's database
# restore into empty databases, before the first start:
pg_restore --no-owner -h db -U hub -d hub_database hub-db.dump
```

The bucket needs your storage provider's snapshot or versioning; the archive does not copy it.

After a restore the entrypoint starts `asc serve` after the hub. If the git bridge directories are older than asc's refs, `BRIDGE = eager` catches them up from asc's outbox. A `lazy` bridge rebuilds itself when the fingerprint of asc's refs differs from the one it last recorded. After a restore, run the repository health check (the `repo_health_check` cron task, in the admin monitor). For an ASC repository it asks asc to verify that every ref's closure exists and that the git id map round-trips, instead of running `git fsck` (`services/repository/check_asc.go`).

`forgejo dump --file <path>` (run in the container as the container user, with the file on a writable mount) writes the hub's own zip of files and database; it does not contain the state of `asc serve`, so it is not a replacement for the archive.

Test a restore before you need one: restore the latest archive into a scratch volume, start a second container on it with another `ROOT_URL` and port, and run the smoke test against it. The backup tool's own tests are `tests/allinone/backup_test.sh`.

## Upgrade and rollback

1. Take the backup above. A rollback after a schema change needs it.
2. Set the image tag to the new commit tag and redeploy (`docker compose pull && docker compose up -d`). On start `asc serve migrate` runs first, then the hub applies its database migrations; the ASC tables are `v17b` to `v17h` (without `v17f`) in `models/forgejo_migrations`. Migrations only go forward.
3. The hub and `asc serve` are upgraded together because they ship in one image: the `asc` binary comes from the `asc-serve` image named by the build argument `ASC_IMAGE`, and the generated `asc.v1` code the hub speaks is in `modules/ascproto`, produced from the ASC repository's `proto` directory. Never combine the hub of one tag with the `asc` of another.
4. Read the container log: the lines that begin with `asc-hub:` name each step (`asc serve migrate`, `starting the hub`, `starting asc serve`), and `asc-hub: error:` lines name what stopped the start. Then run the smoke test.
5. Roll back by setting the previous tag. If the older image refuses to start because the schema moved forward, restore the backup of step 1 onto a new volume.

Upstream releases, security ones first, are merged into `asc/v16` before an image is built; the procedure, with its checks, is in [FORK.md](FORK.md).

## First administrator

Set `ASC_HUB_ADMIN_EMAIL` (and optionally `ASC_HUB_ADMIN_USER`, default `asc-admin`) on the first start. After the hub answers, the entrypoint creates the administrator with a generated password and writes it to `/var/lib/gitea/bootstrap/admin-password` (mode 0600), then leaves a marker so it does not run again:

```sh
docker compose exec hub cat /var/lib/gitea/bootstrap/admin-password
```

Sign in, change the password (the account must), and delete the file. If a user of that name already exists, the entrypoint only leaves the marker. If creating the user fails, the container logs a warning, keeps running, and tries again at the next start. Without `ASC_HUB_ADMIN_EMAIL`, run once, as the container user:

```sh
forgejo admin user create --admin --username asc-admin --email admin@example.org \
  --random-password --must-change-password=true
```

The command prints a generated password once. It also accepts `--password`, but a password in an argument is visible in `ps` and shell history, so use `--random-password`. To get an access token for `asc auth login` at the same time the command has `--access-token`, `--access-token-name` and `--access-token-scopes`.

## Checks after a deploy

Run the smoke test from any machine that can reach the hub. It needs `curl`, `git` 2.31 or later (plus `ssh` and `ssh-keygen` only when you check SSH), and an access token of a test user with the scopes `write:repository` and `write:user` (Settings, Applications):

```sh
printf '%s\n' "$TOKEN" > ~/.asc-smoke-token && chmod 600 ~/.asc-smoke-token
ASC_SMOKE_URL=https://hub.example.org ASC_SMOKE_USER=alice \
  ASC_SMOKE_TOKEN_FILE=~/.asc-smoke-token scripts/smoke.sh
```

In order it checks: `GET /api/healthz`; `GET /.well-known/asc` (it must announce an https address, or a loopback one); the sign-in page; that the API refuses a request without a token; that the token signs in as the named user; creating a public repository `asc-smoke-<random>` under that user (and that the answer describes that repository, under that user, at the address you gave); `git clone`, a commit and `git push` over https; that the API shows the branch and serves the file unchanged; a second clone whose head equals the pushed commit; the repository's web page; then, only with `ASC_HUB_SSH_ENABLE=true` in the script's environment (the hub offers git over SSH and its port is reachable), with a temporary ssh key, a clone over ssh with the same head and a push of a second branch over ssh. It finishes by deleting the repository and the key, and the run fails if that fails (the message names what to delete by hand). It prints the number of checks passed and skipped. A skip is printed as `skip` and never counted as a pass: the ssh checks skip, with the reason, unless `ASC_HUB_SSH_ENABLE=true` (and also when you set `ASC_SMOKE_SKIP_SSH=1`), and the web page check skips when the hub requires sign-in to view.

Safety of the script: the token goes to `curl` through a file in a private temporary directory and to `git` through its environment, never on a command line or into the output; redirects are not followed; plain http is accepted only for a loopback host with `ASC_SMOKE_ALLOW_HTTP=1`; the ssh clone address must name the host of `ASC_SMOKE_URL` (or `ASC_SMOKE_SSH_HOST`) and the test connects to no other host; it deletes only what it made, and only after the hub's answer named that repository under the user you gave. The ssh host key is accepted at first sight, into a known-hosts file that is deleted at the end. The settings are listed at the top of the script. Use a test user, not an administrator: an administrator's token could do far more than the test needs.

By hand:

- `docker inspect --format '{{.State.Health.Status}}' <container>` is `healthy` only when both the hub and `asc serve` answer.
- `curl -fsS https://hub.example.org/.well-known/asc` returns `{"server":"...","repos":"*"}`; a 404 means `[asc] ENABLED` is false or `SERVER_URL` is empty.
- Look in the container log for `asc-hub:` lines and for `[asc]` validation errors; an invalid `[asc]` value stops the start.
- `asc clone https://hub.example.org/<owner>/<repo>` with the `asc` CLI, for the ASC-native path that the smoke test does not use.
