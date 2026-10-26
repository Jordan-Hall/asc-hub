# Operating ASC Hub

This page is for the person who runs a hub: deploy, volumes, ports, backup and restore, upgrade, and the choice of database. Names such as `hub.example.org` are placeholders. For what the pieces do, read [ARCHITECTURE.md](ARCHITECTURE.md). For one concrete staging setup, read [STAGING.md](STAGING.md).

## What runs

A hub deployment has two programs that talk over gRPC:

| Program | Image or binary | Port | Public |
| --- | --- | --- | --- |
| Hub (the Forgejo v16 fork) | `ghcr.io/<owner>/asc-hub:<sha12>`, built from `Dockerfile.rootless` | 3000 web, 2222 git over SSH | yes |
| `asc serve` | from the ASC distribution, not from this repository | 7890 (the default port of the hub's client) | no |

The hub image contains the hub, `git`, and an SSH server, and nothing else: it does not contain `asc serve` or the `asc` CLI. Run `asc serve` as a second service of the same compose project. The standard layout puts it in the hub's network namespace (`network_mode: "service:hub"` in Compose), so the two meet on `127.0.0.1:7890`. That layout is the default for two reasons: the hub's client refuses plain `http` to a host that is not loopback, and `[asc] PUBLIC_URL` is the only address clients ever see, so `asc serve` needs no route of its own. If you put the two on separate hosts, use `https` for `[asc] SERVER_URL`.

### The contract between the two

- **Address.** `[asc] SERVER_URL` is where the hub dials `asc serve`. `[asc] PUBLIC_URL` is what `/.well-known/asc` tells clients. If clients reach `asc serve` through the hub's own domain, set it to the hub's root URL (for example `https://hub.example.org`) and have your proxy route asc's paths to `asc serve`.
- **Shared secret.** Both read the same internal token. Create it once (for example 32 random bytes in hex, mode 0400, in a file both containers mount read-only) and give the hub `FORGEJO__security__INTERNAL_TOKEN_URI=file:///run/secrets/asc-internal-token`. The hub refuses a `file://` token URI together with a literal `INTERNAL_TOKEN` in `app.ini`; start from a fresh data volume or remove the literal.
- **Authorization.** `asc serve` must ask the hub, at `/api/internal/asc/authz` (and `/api/internal/asc/ssh-key` for SSH keys, `/api/internal/asc/request-merge` for the merge queue), what a credential may do. The hub accepts requests on that API only with the internal token.
- **Git bridge.** With `[asc] BRIDGE = eager` (default) `asc serve` writes the objects and refs of every change into the hub's bare repository directory (`/var/lib/gitea/git/repositories` in the image), so asc needs that directory mounted read-write at the same path, and the same user id as the hub (1000 in the image). `asc serve` also reads push quarantines from that directory when a git push is imported. With `lazy` the hub builds the directory itself and needs an `asc` binary in its own container (`[asc] ASC_BINARY`); the stock image does not have one, so `lazy` needs a derived image. With `off` there is no git access to ASC repositories.
- **Configuration of `asc serve` itself** (its database, its object store, its listener, TLS) is documented with `asc serve`, not here.

The `asc serve` side of this contract is owned by the ASC repository; the hub side is what `modules/setting/asc.go` and `routers/private/asc*.go` implement.

## Deploy with Compose

The README has a minimal hub service. Notes for a real deployment:

1. Pin the image to the 12-character commit tag. `:asc-v16` moves with every push to `asc/v16`.
2. Put a TLS proxy in front of port 3000 and set `ROOT_URL` to the public https URL. Port 3000 itself is plain HTTP.
3. Publish the SSH port. `SSH_PORT` is the port shown in clone URLs, `SSH_LISTEN_PORT` is the port the built-in server listens on in the container (it defaults to `SSH_PORT`), and `SSH_DOMAIN` is the host name shown. The image runs as user 1000, so the listen port must be above 1024; the image's default is 2222.
4. Set the secrets: `SECRET_KEY` (first start), and the internal token file.
5. Keep registration closed (`DISABLE_REGISTRATION=true`) unless you want open sign-up, and `INSTALL_LOCK=true` to skip the web installer.
6. The root filesystem can be read-only if `/tmp` is a tmpfs (the image's temp directory is `/tmp/gitea`) and `/var/lib/gitea` is a volume. [STAGING.md](STAGING.md) runs that way with all capabilities dropped and `no-new-privileges`.
7. Health check: `GET /api/healthz` on port 3000.

### Environment variables

`docker-setup.sh` fills `app.ini` from a template **only when the file does not exist**. The plain variables (`APP_NAME`, `ROOT_URL`, `SSH_DOMAIN`, `SSH_PORT`, `SSH_LISTEN_PORT`, `HTTP_PORT`, `DISABLE_SSH`, `DB_TYPE`, `DB_HOST`, `DB_NAME`, `DB_USER`, `DB_PASSWD`, `INSTALL_LOCK`, `DISABLE_REGISTRATION`, `REQUIRE_SIGNIN_VIEW`, `SECRET_KEY`, `LFS_START_SERVER`) matter on the first start. Afterwards, change settings with `FORGEJO__<section>__<KEY>` variables, which are applied to `app.ini` at every start, or edit `app.ini` in the volume. Notably, without `APP_NAME` the first-start template writes `Forgejo: Beyond coding. We forge.` Set `APP_NAME` before the first start, or fix it later with `FORGEJO____APP_NAME` (four underscores: a key in the unnamed top section).

The `[asc]` keys and their `FORGEJO__asc__*` variables are in the README table. The hub reads no `ASC_*` environment variable of its own.

## Coolify

Create a **Docker Compose** resource from your compose file, with the hub and `asc serve` as services of one project:

1. Set the resource's domain on the `hub` service, port 3000, so Coolify's proxy terminates TLS and routes to it. Coolify's magic variable for that is `SERVICE_FQDN_<SERVICE>_3000` (staging uses `SERVICE_FQDN_FORGEJO_3000` for a service named `forgejo`). Set `ROOT_URL` and `[asc] PUBLIC_URL` to the same https address.
2. Publish the SSH port on the host (`2222:2222` or another host port, with `SSH_PORT` set to the host port) and open it in the firewall. Coolify's HTTP proxy does not carry SSH.
3. Keep all state in named volumes, so redeploys keep the data.
4. Put the secrets in Coolify's environment, and write the token file into a volume that both services mount read-only, owned by uid 1000, with a one-shot init service.
5. Set the image tag in an environment variable that the compose file requires, and change it per deploy. Do not use a moving tag.

The staging resource, with SQLite, is described in [STAGING.md](STAGING.md); the compose file it names belongs to the ASC repository, not this one.

## Volumes

| Mount | Content | Back up |
| --- | --- | --- |
| `/var/lib/gitea` (hub) | `custom/conf/app.ini`; `data/` (SQLite file `data/gitea.db` when used, sessions, avatars, attachments, logs); `git/repositories` (the bare repositories, which for ASC repositories are the git bridge); `git/lfs`; `asc-bridge/` (workspaces of a lazy bridge) | yes |
| `/tmp` (hub) | scratch space (`/tmp/gitea`) | no |
| the secrets directory | the internal token | yes, in your secret store |
| `asc serve` state | its database and object store, wherever you configured them | yes, together with the hub |

The path `/var/lib/gitea` comes from `GITEA_WORK_DIR` in `Dockerfile.rootless`; the sub-paths come from `docker/rootless/etc/templates/app.ini`.

## Database: SQLite and Postgres

| | SQLite | Postgres |
| --- | --- | --- |
| Use for | staging, a trial, a small single-node hub | production |
| Setup | none: `DB_TYPE=sqlite3` is the default, the file is `data/gitea.db` in the data volume | a separate server and database; first start: `DB_TYPE=postgres`, `DB_HOST=db:5432`, `DB_NAME`, `DB_USER`, `DB_PASSWD`; later: `FORGEJO__database__*` |
| Backup | stop the hub or use `forgejo dump`; the file is in the data volume | `pg_dump`, or your database's snapshots |
| Tradeoff | one writer, lives and dies with the volume | one more service to run, back up and patch |

The hub's database holds only the hub's data. `asc serve` has its own database, which can also be SQLite or Postgres; choose it with the same reasoning and keep the two backed up together.

## Backup and restore

The hub and `asc serve` each hold half of the truth, and each refers to the other (pull requests point at asc changes; asc refs feed the bridge). Take both backups at the same point, and restore both from the same point.

Backup:

1. Quiesce writes: stop the hub and `asc serve`, or take a consistent snapshot of both volumes and databases.
2. Hub: the `/var/lib/gitea` volume plus the database. `forgejo dump --file <path>` (run as the container user, in the container, with the file on a writable mount) writes a zip of the files and a database dump; add `--skip-repository` if you back the repositories up separately.
3. `asc serve`: its database and object store, by the method its documentation gives.
4. The internal token and `SECRET_KEY`. Without the same `SECRET_KEY` the stored secrets of the hub (two-factor seeds, webhook secrets) cannot be read.

Restore: restore both sets onto empty volumes and databases, start `asc serve` first, then the hub. If the git bridge directories are older than asc's refs, `BRIDGE = eager` catches them up from asc's outbox. A `lazy` bridge rebuilds itself when the fingerprint of asc's refs differs from the one it last recorded. After a restore, run the repository health check (the `repo_health_check` cron task, in the admin monitor). For an ASC repository it asks asc to verify that every ref's closure exists and that the git id map round-trips, instead of running `git fsck` (`services/repository/check_asc.go`).

Test a restore before you need one.

## Upgrade and rollback

1. Take the backup above.
2. Set the image tag to the new commit tag and redeploy. The hub applies its database migrations at start; the ASC tables are `v17b` to `v17h` in `models/forgejo_migrations`. Migrations only go forward.
3. Upgrade `asc serve` and the hub as a compatible pair; the `asc.v1` stubs the hub speaks are in `modules/ascproto`, generated from the ASC repository's `proto` directory.
4. Roll back by setting the previous tag. If the older image refuses to start because the schema moved forward, restore the backup of step 1.

Upstream security releases are merged into `asc/v16` first; see [FORK.md](FORK.md).

## First administrator

After the first healthy start, as the container user:

```sh
forgejo admin user create --admin --username asc-admin --email admin@example.org \
  --random-password --must-change-password=true
```

The command prints a generated password once; sign in and change it. The command also accepts `--password`, but a password in an argument is visible in `ps` and shell history, so use `--random-password`. To get an access token for `asc auth login` at the same time the command has `--access-token`, `--access-token-name` and `--access-token-scopes`.

## Checks after a deploy

- `curl -fsS https://hub.example.org/api/healthz`
- `curl -fsS https://hub.example.org/.well-known/asc` returns `{"server":"...","repos":"*"}`; a 404 means `[asc] ENABLED` is false or `SERVER_URL` is empty.
- Create a repository, then `asc clone https://hub.example.org/<owner>/<repo>` and `git clone https://hub.example.org/<owner>/<repo>.git`.
- Look in the hub log for `[asc]` validation errors; an invalid `[asc]` value stops the start.
