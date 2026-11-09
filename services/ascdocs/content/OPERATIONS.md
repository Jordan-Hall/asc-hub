# Operating ASC Hub

This page is for the person who runs a hub: deploy, volumes, ports, backup and restore, upgrade, the choice of database, and how to check a deployment. Names such as `hub.example.org` are examples. For what the pieces do, read [ARCHITECTURE.md](ARCHITECTURE.md). For one concrete staging setup, read [STAGING.md](STAGING.md).


## What runs

ASC Hub is one container. The image (`ghcr.io/<owner>/asc-hub`, built from `Dockerfile.allinone`) holds the hub (the Forgejo v16 fork), the `asc` binary and `git`. Its entrypoint, `asc-hub-entrypoint` under `dumb-init`, starts both programs and keeps them together:

1. **Checks the volume and the environment before it writes anything.** It refuses to run as root; it needs `/var/lib/gitea` to be writable and every file in the top three levels of it to belong to the container user (uid 1000); it takes an exclusive lock on `/var/lib/gitea/.asc-hub.lock`, which the hub and `asc serve` inherit, so a second container on the same volume, or the backup tool, is refused while either program runs. It then validates the variables it reads (see the table below). Each refusal names the variable or path and exits with status 1.
2. On the first start it generates the secrets the two programs share: the internal token, the webhook secret, the forwarded key (32 bytes as 64 hex digits, the key of the claims the hub signs about a signed-in person, see ARCHITECTURE.md "Forwarded identity"; `[asc] FORWARDED_KEY_URI` on the hub, `[auth] forwarded_key_file` in the `asc serve` configuration) and the hub's `SECRET_KEY`, all under `/var/lib/gitea/bootstrap` (mode 0400, kept across restarts). On every start it writes the `asc serve` configuration (`/var/lib/gitea/asc/asc-serve.toml`, mode 0600) from the environment, then runs `asc serve migrate`.
3. It starts the hub with the `[asc]` defaults filled in (`ENABLED`, `SERVER_URL=http://127.0.0.1:7890`, `PUBLIC_URL` from `ROOT_URL`, `WRITER=asc`, `NEW_REPOSITORIES=asc`, `BRIDGE=eager`), waits for `GET /api/healthz`, then starts `asc serve` on `127.0.0.1:7890`.
4. If `ASC_HUB_ADMIN_EMAIL` is set and no marker exists, it creates the first administrator once.
5. It waits for either program to exit. If one does, it stops the other and exits with a non-zero status (the status of the program that ended), so Docker restarts the whole container. `SIGTERM` and `SIGINT`, also during start, are forwarded to both programs and end in exit 0; the programs get 25 seconds, so give the container `stop_grace_period: 30s`.

| Program | Port | Public |
| --- | --- | --- |
| Hub, web and API | 3000 | yes, behind your TLS proxy |
| `asc serve` | 127.0.0.1:7890 in the container | no |

HTTPS is the only inbound port a hub needs. The image starts no SSH server and exposes only port 3000; git over SSH is an opt-in ([Git over SSH](#git-over-ssh-optional)). Clients use `asc auth login --host <this hub>` with an access token, and `git` over HTTPS through the git-compatible endpoint. If a `git clone` with `https://TOKEN@<hub>/...` answers 401 (seen with git 2.55 and libcurl 8.5), put a colon after the token: `https://TOKEN:@<hub>/<owner>/<repo>.git`. Or use a credential helper, or `git -c http.proactiveAuth=basic clone ...`.

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
6. A read-only root filesystem needs `/tmp` as a tmpfs (the hub's temporary directory is `/tmp/gitea`) and `/var/lib/gitea` as a volume. The all-in-one image writes nowhere else.
7. The image's health check needs both programs: `GET /api/healthz` on the hub and `GET /readyz` on `asc serve`. `/readyz` fails when the database of `asc serve` does not answer or its outbox lags. The check runs every 30 seconds and the container is marked unhealthy after three failures; the first 120 seconds are a grace period. Docker itself does not restart an unhealthy container: `restart: unless-stopped` restarts it when a program exits, and an orchestrator that acts on health (Swarm, Kubernetes with an exec probe of `/usr/local/bin/asc-hub-healthcheck`) does the rest.
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
| `ASC_HUB_DOWNLOAD_SOURCE_URL` | none | An https address the hub fetches the signed `asc` release from, for the page `/download`: the API address of a (private) GitHub release, such as `https://api.github.com/repos/OWNER/REPO/releases/latest` (also `.../releases/tags/TAG`), or a directory that holds `manifest.json` and the files. It sets `[asc] DOWNLOAD_SOURCE_URL` and `[asc] DOWNLOAD_DIR` (`/var/lib/gitea/asc-releases`). See "Release downloads from a private source". |
| `ASC_HUB_DOWNLOAD_PUBLIC_KEY` | none | Required with the source: the minisign public key (the key line of the `.pub` file, it starts with `RW`) that the checksums and the files must be signed with. |
| `ASC_HUB_DOWNLOAD_SOURCE_TOKEN_FILE` or `ASC_HUB_DOWNLOAD_SOURCE_TOKEN` | none | A token that may read a private release (for GitHub a fine-grained token with read access to Contents of that one repository). The file form is best; the plain form is written to `/var/lib/gitea/bootstrap/download-source-token` (mode 0600) and removed from the environment, so it does not land in `app.ini`. Not both. |
| `ASC_HUB_DOWNLOAD_SYNC_INTERVAL` | `1h` | How often the source is looked at: minutes or hours such as `30m` or `6h`, at least `1m`. |
| `ASC_HUB_START_TIMEOUT` | `120` | Seconds to wait for the hub before giving up (1 to 9999). |
| `ASC_HUB_ASC_PORT` | `7890` | Loopback port of `asc serve`; also the default port in `[asc] SERVER_URL` and the port the health check asks. Change it only if 7890 is taken (for example with host networking). |
| `ASC_DATABASE_URL` | SQLite file `/var/lib/gitea/asc/asc.db` | Database of `asc serve`: a `sqlite://`, `postgres://` or `postgresql://` URL. |
| `ASC_S3_ENDPOINT`, `ASC_S3_BUCKET` (`asc-hub`), `ASC_S3_REGION` (`garage`), `ASC_S3_ACCESS_KEY_ID`, `ASC_S3_SECRET_ACCESS_KEY` | none | With an endpoint, the objects of `asc serve` go to an S3-compatible bucket (path-style) instead of `/var/lib/gitea/asc/objects`. The endpoint must be an `https://` URL without credentials, query or fragment; both keys are required with it. This is the operator's storage, not a per-user setting. |
| `ASC_CACHE_MEMORY`, `ASC_CACHE_DISK`, `ASC_TEXT_MEMORY` | `256MiB`, `2GiB`, `256MiB` | Cache sizes of `asc serve`: a number and a unit such as `MiB`. |
| `ASC_GIT_ENABLED` | `false` | Git layer of `asc serve` when the bridge is `lazy` or `off` (always on for `eager`): `true` or `false`. |
| `ASC_LOG`, `ASC_LOG_FORMAT` | `info`, `text` | Log filter and format (`text` or `json`) of `asc serve`. |

The values that go into the `asc serve` configuration file may not contain quotes or line breaks. A value that does not fit stops the container before anything is written, with a message that names the variable and never prints a secret. The names are read by the entrypoint script (`docker/allinone/usr/local/bin/asc-hub-entrypoint`), not by the hub program; `tests/allinone/compose_test.go` fails when this list and the script disagree.

Running the image with a command, for example `docker run IMAGE forgejo admin user list`, skips the supervisor and runs the hub's own entrypoint with that command.

### Release downloads from a private source

The page `/download` offers the `asc` command line from the directory `[asc] DOWNLOAD_DIR`. A hub cannot link to the assets of a private GitHub release, so it can fetch them itself (`services/ascdownloadsync`). Set `[asc] DOWNLOAD_SOURCE_URL`, `[asc] DOWNLOAD_PUBLIC_KEY` and, for a private release, `[asc] DOWNLOAD_SOURCE_TOKEN` or `DOWNLOAD_SOURCE_TOKEN_URI` (a `file://` address); in the all-in-one image use the `ASC_HUB_DOWNLOAD_*` variables above.

What the hub does, at start and then every `[asc] DOWNLOAD_SYNC_INTERVAL` (one hour by default), in a goroutine that never holds up the start:

1. Fetches `manifest.json`, the checksum file and its signature that the manifest names. The checksum file must verify with `[asc] DOWNLOAD_PUBLIC_KEY`; a key named in the manifest must be the same one. A source cannot choose its own key.
2. Requires the signed checksums to list `manifest.json` with the hash of the manifest that was fetched, and every file of the manifest with the hash and size the manifest says. A file with a detached signature must verify with the key too (prehashed `ED` and legacy `Ed` minisign signatures are read).
3. Downloads every file into a staging directory below the download directory, with caps: 1 MiB for the manifest, 1 GiB for a file and 4 GiB for a release. Only when all of it has verified are the files moved in, each by an atomic rename and `manifest.json` last. Files of the release that was replaced, and only those, are removed.
4. On any failure the last good release stays and a warning goes to the log. An administrator who opens `/download` while nothing is published sees the last error.

The token goes only to the host of the source URL; GitHub redirects the download to its storage on another host and the token is not sent there. Every connection is made through a dialer that refuses private, loopback and link-local addresses, so the source address cannot point inside your network. Sign the release with `minisign -Sm SHA256SUMS` (and each file you want signed) and publish `manifest.json` in the release; the format of the manifest is in `modules/ascdownload`.

While no release is published `/download` says so and shows how to build `asc` from source and how to connect it to the hub.

### The asc API through the hub

`asc clone https://<hub>/<owner>/<repo>` finds the server through `GET /.well-known/asc`, which announces `[asc] PUBLIC_URL` (the hub's address), and sends its gRPC calls there. The hub forwards them to `asc serve` (`services/ascforward`, mounted by `routers/init.go` when `[asc] ENABLED`). It is a narrow proxy, not an open one.

- **Who decides.** The `Authorization` header of the client goes on unchanged: a platform personal access token from `/user/settings/applications` (`asc auth login` stores it). `asc serve` validates it by asking the hub, which applies its own permission model (repository units, collaborators, teams, visibility, restricted and blocked users, token scopes). The hub adds no credential and never uses the internal token on this path, so the proxy cannot be used to act as the hub. The `x-asc-forwarded-*` and `x-asc-hub-*` headers of a client are removed, so a client cannot make the hub vouch for it.
- **What is forwarded.** Only `POST /asc.v1.<Service>/<Method>` with a gRPC or gRPC-Web content type, for a service of the allowlist: `Repo`, `Changes`, `Stacks`, `Review`, `Cloud`, `Projects`, `CodeIntel`, `CrossSearch`, `Inbox`, `MergeQueue`, `Policy`, `Workflows`, `Agents`, `AgentTokens`, `Ci`, `CiInsights`, `SecretScan`, `Signing`, `Audit`, `DepsService`. Never forwarded: the services `Workspace` (local to a machine), `Bridge` (the hub's own), `Vault` (key material), `Storage` and `AiProviders` (set on the hub's pages), and the methods `Repo/RepoAdmin` (repairs and renames are the hub's) and `Repo/ImportGitPush` (names directories of the machine). Every other path, method or content type is refused before asc is asked: a gRPC answer with a status when the request was gRPC, else HTTP 404, 405 or 415. A test compares the list with the compiled protocol, so a service added later is refused until it is allowed (`services/ascforward/forward.go`, `services`).
- **Limits.** A call with one request message may send 20 MiB and take 10 minutes; a streaming call may send 4 GiB and take an hour; at most 512 calls are in flight, the rest are refused with `RESOURCE_EXHAUSTED`. gRPC-Web and gRPC streams are passed both ways with their trailers.
- **Headers.** Cookies, hop-by-hop headers, `Forwarded`, `X-Forwarded-*`, `Origin` and `Referer` are removed from the request; `X-Forwarded-For` is set to the address the hub saw. `Set-Cookie` and every CORS header are removed from the answer and none is added, so a page of another origin cannot use the path.
- **HTTP/2 without TLS (h2c).** gRPC needs HTTP/2, and the TLS proxy in front of the hub may speak it in clear text to port 3000. The hub accepts only prior-knowledge HTTP/2 on its plain listener, and a connection that begins that way serves nothing but `POST /asc.v1.*` with a gRPC content type (anything else gets 421). A request with `Upgrade: h2c` or `HTTP2-Settings`, the upgrade form that can be used to get around a front proxy's path rules, is refused with 400 on every route. HTTP/1.1 traffic goes through the ordinary router unchanged (`services/ascforward/h2c.go`, `cmd/web.go`).
- **Front proxy.** Pass HTTP/2 (or gRPC) to the hub for the path `/asc.v1.*` and do not buffer the body or cut the idle time of a long call. Keep the hub's address the only public one: `asc serve` has no published port.

### Git over SSH (optional)

Only if the host can open an extra port. With `ASC_HUB_SSH_ENABLE=true` the entrypoint starts the hub's built-in SSH server and sets `[asc] SSH_ENABLED=true`; the hub then shows the SSH keys page, SSH deploy keys and an SSH clone address. You publish the port yourself:

| Variable | Default when enabled | Meaning |
| --- | --- | --- |
| `SSH_LISTEN_PORT` | `2222` | Port of the built-in server inside the container. |
| `SSH_PORT` | `2222` | Port shown in clone addresses; it must equal the host port you publish. |
| `SSH_DOMAIN` | host of `ROOT_URL` | Host name shown in clone addresses. |

Add the published port to the Compose file (`"<host port>:2222"`, with `SSH_PORT=<host port>`) and open it in the firewall. An HTTP-only reverse proxy does not carry SSH. These three variables are ignored (the entrypoint logs a warning) while `ASC_HUB_SSH_ENABLE` is not `true`. In a hub run without the image, set `[asc] SSH_ENABLED = true` (default `false`); with `[asc] ENABLED = true` and `SSH_ENABLED = false` the hub forces `[server] DISABLE_SSH = true`.

## Events from asc serve to the hub

`asc serve` tells the hub when something happens that a person has to know about, and the hub decides who is told and how: the inbox keeps everything, and each person chose on **Settings, Notifications** what also reaches them by email, in the browser (Web Push) and in the daily summary.

- **Call.** `POST /api/internal/asc/event` on the hub, with the internal token as a Bearer token and the header `x-asc-event-id` (the same value for every retry of one event). Body: `event`, `repo` (`owner/name`), `users` (logins, at most 50), `title` (one line, at most 200 bytes), `detail` (at most 500), `path` (a path of the hub). `event` is one of `review_requested`, `mentioned`, `agent_question`, `agent_finished`, `checks_failed`, `restack_conflict`, `merge_queue`, `policy_changed`.
- **Who is told.** Only active people who can read the repository. A login that does not exist, has no access or is an organization is skipped, and the answer (`{"notified": N}`) never says which. A person who was told about an event id is not told again, so retries are safe. An answer 4xx is final (do not retry); 5xx and a dropped connection may be retried.
- **The bell.** Every event a person was told about is also listed in the notifications popover of the header bell, with the notifications of the hub (issues, pull requests, transfers), newest first. The unread number of the bell counts both, and updates live. Opening an event in the popover marks it read; **Mark all read** marks every hub notification and every event read. Events stored before the popover existed count as read. On a phone the popover is a bottom sheet: the **Inbox** tab of the bottom navigation opens it, and **Open inbox** at its foot goes to the inbox. On the inbox itself, and in a browser without script, the tab is a plain link to the inbox. Esc or a tap on the dimmed page closes the sheet and gives the focus back to the tab.
- **Where a notification leads.** The hub gives the links of `asc serve` their real address and keeps every link inside the repository; a path it does not know goes to the repository.

  | `asc serve` writes | The hub page |
  | --- | --- |
  | `/{repo}/changes/{change}` | `/{repo}/pulls/{number}` (the pull request of the change; `/{repo}/pulls` when it has none yet) |
  | `/{repo}/agents` and `/{repo}/agents/runs/{id}` | `/{repo}/asc/agents` and `/{repo}/asc/agents/runs/{id}` |
  | `/{repo}/merge-queue` | `/{repo}/asc/queue` |
  | `/{repo}/settings/policy` | `/{repo}/settings/asc/policy` |
  | `/{repo}`, `/{repo}/pulls/{n}`, `/{repo}/issues/{n}`, `/{repo}/asc/...`, `/{repo}/settings/asc/...` | unchanged |
  | another repository, `..`, a doubled slash, any other page | `/{repo}` |

### The internal token

The call is the same trust as the hooks and `authz`: both programs read **one** secret, and anyone who has it can act as `asc serve`. Keep it out of images, logs and the `app.ini` you commit.

- **All-in-one image.** Nothing to set. The entrypoint makes the token once under `/var/lib/gitea/bootstrap/internal-token` (mode 0400), sets `[security] INTERNAL_TOKEN_URI=file:///var/lib/gitea/bootstrap/internal-token` for the hub and `[forgejo] internal_token_file` in `asc-serve.toml` for `asc serve`. A backup holds it; a restore on another host keeps the two in step.
- **Two services (hub image and `asc serve`).** Make the token once, `docker run --rm IMAGE forgejo generate secret INTERNAL_TOKEN`, and store it as a secret file that both containers mount read-only:

  | Side | Setting | Environment form |
  | --- | --- | --- |
  | Hub | `[security] INTERNAL_TOKEN_URI = file:///run/secrets/asc_internal_token` | `FORGEJO__security__INTERNAL_TOKEN_URI=file:///run/secrets/asc_internal_token` |
  | Hub | `[asc] ENABLED = true`, `SERVER_URL = http://asc:7890` | `FORGEJO__asc__ENABLED=true`, `FORGEJO__asc__SERVER_URL=http://asc:7890` |
  | `asc serve` | `[forgejo] internal_token_file = "/run/secrets/asc_internal_token"`, `url = "http://hub:3000"` | the file is written from your configuration; the secret is mounted, not passed as a variable |

  `[forgejo] url` is the address of the hub inside the private network, not the public one. With the variable form, `INTERNAL_TOKEN` itself in the environment works for the hub but puts the secret in `docker inspect`; use the file.
- **Compose.** Declare a top-level `secrets: asc_internal_token: { file: ./secrets/asc_internal_token }` and list it under `secrets:` of both services; the file is then `/run/secrets/asc_internal_token` in each.
- **Behind a proxy.** The public domain of the hub exposes `/api/internal/...` too; every call there needs the token, but block the prefix at the proxy anyway when the proxy offers a path rule (`/api/internal` is only for the private network).
- **Rotation.** Make a new token, replace the file for both, restart both. A call with the old token answers 403, which is a final answer for `asc serve` (it does not retry a 4xx), so events sent between the change of the two files are lost; rotate in a quiet moment.
- **Check.** With the token in `$T`: `curl -sS -X POST -H "Authorization: Bearer $T" -H 'content-type: application/json' -d '{"event":"merge_queue","repo":"OWNER/NAME","users":["LOGIN"],"title":"test"}' http://hub:3000/api/internal/asc/event` answers `{"notified":1}` when LOGIN can read the repository, and 403 without the token. A 404 means `[asc] ENABLED` is off.

## Other shapes

- **Hub image and `asc serve` as two services.** Build `Dockerfile.rootless` (no `asc serve` inside), run `asc serve` from the ASC distribution as a second service in the hub's network namespace (`network_mode: "service:hub"`), and set the `[asc]` keys, the internal token and the shared repository directory yourself. The contract: `[asc] SERVER_URL` is where the hub dials; both read the same internal token (`[security] INTERNAL_TOKEN_URI`, a `file://` URI); `asc serve` asks the hub at `/api/internal/asc/authz` (and `/ssh-key`, `/request-merge`) what a credential may do; with `BRIDGE = eager`, `asc serve` needs the hub's `git/repositories` directory read-write at the same path and the same user id. The configuration of `asc serve` itself is documented with `asc serve`. The backup tool and the lock below belong to the all-in-one image only.
- **Separate hosts.** Use `https` for `[asc] SERVER_URL`; the hub's client refuses plain `http` to a host that is not loopback.

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

### Scheduled backups of asc serve

asc serve backs itself up on a schedule when `[backups]` names a target in its configuration (see its runbook). Site
administrators manage the schedule, the history, checks and restores on **Site administration → Backups**. When the
backups are overdue (no success within one and a half intervals) or the last one failed, the dashboard's **Needs
attention** card lists them with the date of the newest good backup and a **Back up now** button, which starts a
backup to the schedule's target and opens the Backups page (while a backup runs, the row offers **Open** instead).
The dashboard asks asc only for site administrators, waits at most three seconds and shows no row when asc does not
answer. Code: `routers/web/admin/overview.go` (backupAttention), `routers/web/admin/asc_backups.go`. Tests:
TestTheDashboardOffersABackupWhenBackupsAreOverdue, TestAscBackups*.

## Upgrade and rollback

> [!WARNING]
> Migrations only go forward. Without the backup of step 1, an upgrade that changed the schema cannot be rolled back.

1. Take the backup above. A rollback after a schema change needs it.
2. Set the image tag to the new commit tag and redeploy (`docker compose pull && docker compose up -d`). On start `asc serve migrate` runs first, then the hub applies its database migrations; the ASC tables are `v17b` to `v17h` (without `v17f`) in `models/forgejo_migrations`. Migrations only go forward.
3. The hub and `asc serve` are upgraded together because they ship in one image: the `asc` binary comes from the `asc-serve` image named by the build argument `ASC_IMAGE`, and the generated `asc.v1` code the hub speaks is in `modules/ascproto`, produced from the ASC repository's `proto` directory. Never combine the hub of one tag with the `asc` of another.
4. Read the container log: the lines that begin with `asc-hub:` name each step (`asc serve migrate`, `starting the hub`, `starting asc serve`), and `asc-hub: error:` lines name what stopped the start. Then run the smoke test.
5. Roll back by setting the previous tag. If the older image refuses to start because the schema moved forward, restore the backup of step 1 onto a new volume.

Upstream releases, security ones first, are merged into `asc/v16` before an image is built; the procedure, with its checks, is in [FORK.md](FORK.md).

## Setting up in the browser

A hub without `[security] INSTALL_LOCK = true` opens the first run form. When the form is sent the page shows Setting up your hub, Step n of 5: database tables created, repository root checked, creating the administrator account, starting the merge queue, opening your hub. The page asks `GET /-/install/status` (`{"done": n, "total": 5}`, never cached, no session) for the first three; the post-install page then waits for the hub to answer on its own and shows ASC Hub is ready. The route exists only while the installer runs: a hub that is set up answers it 404.

The administrator account is optional. Leave its four fields empty and the first person to register on the hub becomes its administrator; everyone after registers as a normal account. A skipped administrator needs self-registration open, so the form refuses Disable self-registration (invite only) without one. Require email confirmation for new accounts needs the email settings of the form, or nobody can confirm.

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

## Get started for new accounts

A signed-in person who owns no repository, collaborates on none and belongs to no organization with one gets the Get started checklist at `/` instead of the dashboard. Hide (the cross beside the heading) shows the dashboard until they sign in again; the checklist returns while they still have no repository. Once a repository exists, `/` is the dashboard. The checklist stays at `/-/get-started` for everyone.

While step 2 waits for the first sign-in of the command line, the open page asks `GET /-/get-started/status` every three seconds (only while the tab is shown). The answer is JSON `{"connected", "done", "total"}`, never cached; `connected` is true once the person has an access token. When it turns true the page puts the new steps, progress and health card in place without a reload, and a screen reader hears "The command line signed in. Step 2 is done." The route needs a sign-in.

The card "Prefer to look first?" offers Open the sample project, a button that posts to `POST /-/get-started/sample` (sign-in needed). The first time anyone presses it the hub makes the sample: a bot account `asc-sample` that cannot sign in, and its public repository `sample-project` with a README and three more commits on `main` (a small Rust service and a docs page), then archives it so nobody can push, review or open issues in it. Every later press opens the same repository. It shows in Explore like any public repository and does not count as a repository of the person who opened it, so their checklist stays. An admin can unarchive or delete it like any repository; after a delete, the next press makes it again. If making it fails, half-made parts are deleted and the person lands on Explore with a message that names the request id (look for `ascsample` in the log).

| Setting | Default | Meaning |
|---|---|---|
| `[asc] SAMPLE_PROJECT` | `true` | `false`: no sample; the card links to the public projects (`/explore/repos`) and the route goes there. |

If an account named `asc-sample` already exists and is not a bot (someone registered the name), the sample is not made and the card falls back to the public projects with the message; rename that account or turn the setting off.

Step 3 imports a repository in place. Import opens a form with three choices: From a Git URL (the form), From GitHub or GitLab (the full migration page, for issues and other data) and Start empty. The form posts `clone_addr` to `POST /-/get-started/import`, which queues the same task as `/repo/migrate` with history only: the repository is private, owned by the person and named after the last part of the address. Only `https` addresses are taken, and the address must pass the `[migrations]` allow and block lists (`ALLOWED_DOMAINS`, `BLOCKED_DOMAINS`, `ALLOW_LOCALNETWORKS`). With `DISABLE_MIGRATIONS`, or when the person is over their repository quota, the form says so and nothing is queued. While the task runs the page asks `GET /-/get-started/import/status?repo=<name>` every 1.5 seconds and shows "Reading history"; the answer is JSON `{"status", "message", "commits", "count", "link"}` (status `queued`, `running`, `failed` or `done`), never cached, and only for a task the person started. When it is done the page shows the commits read on the default branch and a link to the repository; a failed task shows its reason with any address credentials removed. After three failed answers in a row the page stops asking and links to the repository.

## The wait after wrong passwords

After five wrong passwords for one name from one address, the hub refuses that name from that address for five minutes and the sign-in page names the wait (429 with `Retry-After`). The name counts as typed, in any case, and is not looked up, so the count does not tell a stranger which email belongs to which account; a username and an email of the same person count apart. One address that gets ten times as many wrong across names waits too, which bounds the tries across the names of one person. Both counts are taken before the password is checked, so tries sent at the same time cannot pass them. The counts live in memory, at most 10000 keys each. A full table drops the key whose count is oldest among those not waiting; when every key waits, it drops the wait that ends soonest. Every key keeps its own count, so a flood never refuses or frees another key, and a try is only given back to the count that took it. A right password before the limit clears the count. The same count covers linking an account, OpenID connect and basic authentication. Set it in `[security]`:

```ini
LOGIN_MAX_ATTEMPTS = 5 ; 0 turns the wait off
LOGIN_WAIT = 5m
```

The counts are kept in memory by each hub process and are lost on a restart.

## Passkeys

With ASC on, the sign in page has Sign in with a passkey under the form. The browser offers the passkeys it holds for the hub, the person unlocks one (fingerprint, face or the PIN of the device) and is signed in with no name, no password and no second step: the key must verify the person, which stands for both. A key that only shows presence is refused, and so is an account that is inactive or may not sign in, or whose sign in source (LDAP, PAM, SMTP, OAuth2 and the like) an administrator turned off. A passkey does not ask the outside source again: an account removed there keeps signing in with its passkey until the source sync turns the account off or an administrator removes the key. Each challenge takes one answer. Keep me signed in applies as it does to the password.

Security keys added under Settings, Security while ASC is on are asked to be passkeys, so the device keeps one for the account; a device that cannot registers a plain security key, which still works as the second step but not on this button. A key added before needs to be added again to sign in with it. Passkeys are bound to `[server] DOMAIN`: changing the domain makes every key stop working.

## Passwords found in breaches

With `[security] PASSWORD_CHECK_PWN = true` the hub refuses a new password that appears in the Have-I-Been-Pwned list, and the strength meter of the sign up, reset and link forms shows the rule "Not found in known data breaches" while the person types. The browser hashes the password with SHA-1 and asks the hub for `GET /-/password/range/{prefix}`, the first five hexadecimal digits only; the hub asks `https://api.pwnedpasswords.com/range/` with padding and answers the suffixes of that range, and the browser looks for its own. Neither the password nor its full hash leaves the browser, and the outside service sees only the prefix and the address of the hub. A breached password shows as Weak. The route answers 404 with the setting off, 400 for a prefix that is not five hexadecimal digits, 502 when the service cannot be reached, and 429 after 30 ranges a minute from one address; answers are never cached.

The setting is off by default, because with it on a hub that cannot reach `api.pwnedpasswords.com` refuses every new password. Turn it on when the hub has internet access:

```ini
[security]
PASSWORD_CHECK_PWN = true
```

## Contact and policies on the About page

The footer links Security, Contact, Privacy, Terms, Acceptable use, Subprocessors and Cookies to sections of `/-/about`. The page lists the cookies the hub sets (with the names in `[session] COOKIE_NAME` and `[security] COOKIE_REMEMBER_NAME`) and what the configuration hands to outside services: object storage on this server or in an S3-compatible store, and a mail server. The page is public, so it never shows a host or address of the configuration; name your providers with `SUBPROCESSORS`. These settings make the rest yours:

| Setting | Default | Effect |
|---|---|---|
| `[asc] CONTACT_EMAIL` | empty | The Contact button mails this address. Empty: it opens the issues of ASC Hub. |
| `[asc] SECURITY_EMAIL` | `CONTACT_EMAIL` | The Security section asks for reports at this address. With both empty, reports go to the private advisories of ASC Hub. |
| `[asc] POLICIES_URL` | empty | An `https` page with the full privacy policy, terms and acceptable use policy of your hub, linked next to the summary. |
| `[asc] SUBPROCESSORS` | empty | The outside services you use, as `Name: purpose` entries separated by commas (at most 20, each part at most 100 characters), listed first under Subprocessors. |

The addresses must be plain (`security@example.org`, no display name), the URL `https` without credentials and the entries without control characters; anything else stops the hub at start.

The sign up form (and the form that makes an account while linking an outside sign-in) has the box "I agree to the Terms and the Privacy policy". Its links go to `POLICIES_URL` when it is set, else to the `#terms` and `#privacy` sections of `/-/about`. An account is only made with the box ticked, and the hub keeps when each person agreed in the table `asc_terms_acceptance` (removed with the account). Accounts made by an administrator, by automatic registration from an outside sign-in or before this box existed have no record.

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

## The System card of site administration

The **System** card on the site administration dashboard has a row for each part of the site: database, cache,
queues, runners (when Actions is on), mail and object storage.

- **Mail** reads the outgoing mail queue. It says how many mails wait and how long the oldest has waited, and warns
  when that is over five minutes. It is **Down** when the last attempt to send failed and no mail was sent after it
  (check `[mailer]`), and **None** when mail is not set up. The age is known for mail this hub process queued: mail a
  persistent queue kept across a restart is counted without an age until it is sent.
- **Object storage** is what asc serve stores for the whole server this month (Usage, `usage.v1`) against the
  server's storage limit: **Nearly full** from 90%, **Full** over the limit, with no limit only the amount. asc is
  asked as the site administrator, for at most three seconds; when it does not answer the row says so, and when asc
  is off or has no usage figures there is no row. The hub's own files (attachments, packages) are not counted here.

Code: `routers/web/admin/overview.go` (mailRow, storageRow), `services/mailer/queue_watch.go`. Tests:
TestTheSystemCardShowsTheMailQueue, TestTheSystemCardShowsObjectStorage, TestTheQueueWatch*.

### The figures at the top of the dashboard

- **People** counts the accounts of people; under it is how many were created in the last seven days. This is the
  hub's own data.
- **Changesets today** and **Agent runs today** come from asc serve (Usage.GetActivity, `usage-activity.v1`, asked as
  the site administrator) and count the whole server since 00:00 UTC. A change counts once, on the day its first
  version was published; it is an agent's when an agent published it or an agent run holds it. Under the runs is the
  share of the ended runs that succeeded (failed, over budget, expired and cancelled runs are the rest), or how many
  are running when none has ended. The figures are kept for a minute, and asked again when the UTC day changes.
- When asc is off, has no activity figures or does not answer in three seconds, the dashboard shows Organizations and
  Issues in their places.

Test: TestTheDashboardShowsTodaysActivity, TestTodaysActivityIsKeptForAMinute, TestNewPeopleThisWeek.

## The health page of the site administration

`/admin/self_check` runs its checks one after the other; "Run again" runs them again, each row changing when its own
check is done. A check that does not apply to this hub has no row. Each row is passed, warning, failed, or could not run
(the question could not be asked, so it neither passed nor failed).

| Check | Answered by | Warning | Failed |
|---|---|---|---|
| Database connection, Database collation, Cache | the hub | slow cache, case-insensitive collation | no answer, wrong collation |
| Disk usage | the hub: the file system of the data directory (`APP_DATA_PATH`) | 85% full, or full within 14 days | 95% full, or full within 2 days |
| Outgoing mail | the hub's mail queue (only when mail is set up) | the oldest mail has waited over 5 minutes | the last mail could not be sent |
| TLS certificate | the hub, only when it serves HTTPS itself: the first certificate in `CERT_FILE`; with ACME it is renewed automatically | expires within 14 days | expired, or the file cannot be read |
| Clock | the host kernel (Linux: whether NTP keeps it in time) and asc serve's clock | not kept in time, or over 2 seconds from asc serve's | over 30 seconds from asc serve's: signed calls fail |
| Object storage | asc serve (Maintenance.CheckHealth, `health.v1`): writes, reads back and deletes a probe object in its bucket | - | the object store refused or did not answer in 10 seconds |
| Signing key | asc serve: its server key signs a fixed message and the signature verifies | no server key is set, so the commits it makes are not signed | the key cannot sign |
| Runners (with Actions), System notices | the hub | no runner, notices | a runner offline |

- **How soon the disk fills** is the rate the hub saw over the last day (at least an hour of it): it samples the used
  bytes every 10 minutes from its start, so a hub that just started shows only how full the volume is.
- **The clocks** are compared at the middle of the call, and a difference smaller than half the call's time is not
  counted.
- **asc serve's probes** need the site administrator and asc on. asc serve makes them at most once every 5 seconds, so
  reloading the page does not write many objects; the probe object lives under `<root>/.asc-probe/` and is always
  deleted. When asc serve does not answer, Object storage and Signing key could not run.
- Behind a proxy that ends TLS there is no TLS certificate check: check the proxy's certificate there.
- **The gauges** above the checks are the hub host's CPU (one-minute load per core), memory and disk, and Object
  storage: what asc serve stores for the whole server against its storage limit, or the size alone when no limit is
  set. The Object storage gauge is drawn only when asc answers.

Code: `routers/web/admin/asc_health.go`, `asc_health_more.go`, `services/asctrust/health.go`. Tests:
TestTheDiskCheckSaysHowSoonTheVolumeFills, TestTheTLSCheckReadsTheCertificateTheHubServes, TestTheClockCheck,
TestTheAscChecksAskAscServeToProbeItself, TestLoadHealthMeasuresTheClocks, TestAscHealthSteps.

## Agent runners on the runners page

Site administration > Runners lists the hub's Actions runners and, under them, the agent runners: the asc serve
processes that execute agent runs. The section is shown to the site administrator when asc is on and runs agents
(`[agents] enabled`, feature `agent-runners.v1`).

- Every asc serve process with agents on is one runner. It reports every 10 seconds and stamps the runs it takes with
  its name.
- Name: `[agents] runner_name` in asc serve's configuration, by default the host name. Give each process its own
  name: two processes with one name show as one runner.
- Slots: `[agents] max_concurrent_runs` (4 by default), shown as "2 of 4 slots in use". vCPU is the number of CPUs the
  process may use.
- State: Running an agent (it has a live run), Idle, or Offline (no report for 90 seconds; the row says when it was
  last seen). An offline runner stays listed for 7 days, then is removed.
- Each live run (running or waiting for an answer) shows as "Agent run #214" and links to the run page of its
  repository.
- When asc serve does not answer within 3 seconds, the section says so and the rest of the page draws as usual.

Code: `routers/web/shared/actions/asc_agent_runners.go`, `services/asctrust/runners.go`; asc serve
`Maintenance.ListAgentRunners`. Tests: TestTheAdminRunnersPageListsTheAgentRunners, TestLoadAgentRunnersRefusesABadAnswer.

## Usage and agents on a person's page

The page of a person in site administration (Overview tab) has a **Usage** card (storage and agent minutes this
month) and an **Agents** card. Both ask asc serve as the site administrator; the usage read is written to asc's audit
log. The Agents card lists the agents that ran in the asc repositories the person can see, counting only the runs the
person started (Agents.SummarizeAgents with `created_by`): the handle links to the agent's runs, with Active when a
run is going, the number of runs and the first repository. At most five are shown, then "And N more". The card shows
only when the site administrator has turned agent features on for their own account; when asc cannot answer, the card
is left out.

Code: `routers/web/admin/user_tabs.go` (loadUserUsage, loadUserAgents). Test:
TestTheAdminPersonPageHasTheAgentsThePersonRuns.

## The default branch asc protects

asc protects one branch of each repository (path rules, secret scanning, review, signing): the trunk it holds. The hub
tells asc the trunk when a repository is made, when a migration finishes importing, and whenever the default branch
changes in the hub (settings or a rename). A repository imported by an older hub, or whose default branch changed
before the hub told asc, can have a different branch protected than the one people push to, and the real default
branch is then unscanned.

Find and repair them (needs an asc that has `RepoAdmin set_default_branch`; the check changes nothing without `--fix`):

```sh
forgejo doctor check --run asc-default-branch            # lists the repositories that protect the wrong branch (or says asc does not report it)
forgejo doctor check --run asc-default-branch --fix      # tells asc the hub's default branch for each of them
```

The check is idempotent: a repository whose trunk is right is not touched, and every change asc makes is in its audit
log as `repo.default_branch` (with `from` and `to`). Empty repositories and repositories still being migrated are
skipped. Run it once after updating the hub and asc together; new repositories need nothing.

## Who can install a package

A package is as visible as its owner, and the package lists show it as a chip next to the type and version:

- **Public**: the owner is public. Anyone can install the package.
- **Internal**: the owner is limited, or the hub has `[service] REQUIRE_SIGNIN_VIEW = true`. Only people signed in to
  the hub can install it.
- **Private**: the owner is private. Only members of the organization (or the user) can install it.

The hover text of the chip names the owner. There is no visibility per package. The registries serve indexes of all the
packages of an owner of one type (Debian, RPM, Alpine, Arch, Helm, Conda, CRAN), so a private package of a public owner
would be listed in them. To keep a package private, publish it from a private organization.

The chip is `VisibilityOf` in `routers/web/shared/packages/asc_list.go`, which follows the access check of the
registries (`DeterminePackageAccessMode`). Tests: TestAPackageIsAsVisibleAsItsOwner,
TestThePackageListShowsTheVisibilityOfEachPackage.

## Requests for access

A signed-in person who is refused on a page of a repository (the 403 page) can ask its admins for more access with
**Request access** and an optional message of up to 500 characters. A reader asks for write access, a writer asks to
be an admin, and an admin is not offered the form. A restricted account is never offered it. The page then says the
request was sent and when, and the person can withdraw it. A person the owner has blocked is told the same and nothing
is stored, so the request does not tell them about the block.

The admins of the repository see the requests at the top of **Settings > Collaborators**, oldest first, with the role
asked for, the message and the date. **Grant** adds the person as a collaborator with that role (or raises the role of
a collaborator) and mails them; a person who is no longer active, an owner of the organization or blocked is not
added, and the request stays so it can be dismissed. **Dismiss** removes the request without a change. A person has at
most one request per repository; a second one replaces the first. Requests go with the repository or the account when
either is deleted.

**Switch account**, on every 403 page for a signed-in person, signs out and opens the sign-in page, which comes back to
the refused page once another account is signed in. Only a path of this hub is kept as the way back.

The requests are the table `asc_access_request` (`models/repo/asc_access_request.go`); the 403 page data is
`ascForbiddenActions` in `services/context/context_response.go`, the handlers are
`routers/web/repo/asc_access_request.go`, `routers/web/repo/setting/asc_access_request.go` and
`routers/web/auth/asc_switch_account.go`. Tests: TestAscAccessRequests, Test_addAscAccessRequest,
TestAscAccessRequestFromTheForbiddenPage, TestAscSwitchAccount.

## Presence on profiles

The head of a profile has a pill that says **Online**, **Online, local time 14:20** or **Local time 14:20**, and the
avatar has a green dot while the person is online. Online means a request in the last ten minutes; the time of the
last request is stored at most once every five minutes (`asc.last_seen`). The local time is the time in the zone the
person chose on their profile page, drawn by the server.

The person decides on **Settings > Privacy**:

- **Show when I am online** (on by default). Off, neither the dot nor the pill shows, local time included.
- **Show my local time on my profile** (off by default), because the time tells others the time zone. On, the pill
  shows the time even while the person is offline.

Neither shows to someone who may not see the person's activity (activity kept private, or a private profile seen by
someone else). The pill is `ascpresence.Of` (`services/ascpresence`), filled in `routers/web/user/profile.go` and drawn
in `templates/shared/user/profile_big_avatar.tmpl`. Tests: TestThePillSaysOnlineAndTheLocalTime,
TestTheProfileShowsAPresencePill.

## The agents pill in the header

While a signed-in person has an agent run that is queued, running or waiting, the header (beside Inbox) and the phone
top bar show a pill with a cyan dot: **3 agents running** on a wide header, the number alone on a phone, with the full
words as its accessible name. It links to Your agents (`/-/asc/agents`). The page script asks
`GET /-/asc/agents/count` (JSON `{"count"}`, never cached by the browser, a sign-in needed) once and keeps the answer
for 30 seconds in the session storage of the tab; the hub keeps it 30 seconds per person in its cache.

The count covers the person's own runs only (`created_by` is the signed-in name) over the asc repositories of Your
agents (the 30 most recently updated that they own or collaborate on), asked with one `Agents.CountRuns` call to
`asc serve`, which checks read on each repository. It is 0 and the pill stays hidden while the person's agent features
are off, when they have no asc repository, and when `asc serve` cannot be asked (logged with the request id; a failure
is not cached). The number shown is at most 999.

## The agent activity chip of a repository row

For a signed-in person with agent features on, each row of Explore, a profile, an organization and Watching that is an
asc repository whose code they can read shows **N agent runs**: the runs started there in the last 7 days (the window
of the Agent activity quick chip), any state and anyone's. The chip links to the agent runs of the repository
(`/{owner}/{repo}/asc/agents`); hovering or a screen reader gives the full words (**3 agent runs in the last 7 days**).
A row without runs, a git repository, a visitor and a person with agent features off get no chip.

The hub asks `asc serve` with one `Agents.CountRuns` call (`since`, answer `per_repo`) per 50 rows, with the system
credential, naming only the rows the viewer can read; asc checks read on each. The counts of a set of repositories
are kept 60 seconds in the hub cache. The number shown is at most 999. When `asc serve` cannot be asked the rows show
no chip and the failure is logged with the request id (not cached).

## The changeset count of a repository row

Each row of Explore, a profile, an organization and Watching whose code the viewer can read shows **N changesets**: how
many changesets the head of the default branch reaches. It has no icon and links to the changesets of the default
branch. An empty repository and a private repository the viewer cannot read show no count.

The count is kept in the table `asc_repo_changesets` (repository, head, count) by the stats indexer, the queue
`repo_stats_update` that also keeps the language bar. The indexer counts on a push, a mirror sync, a migration and a
change of the default branch, and only when the head moved. A git repository is counted with `git rev-list --count`;
an asc repository with `History.Log` and `count_only`, one page after another, without loading a changeset. A row shows
the stored count even when the head has moved since, and queues the repository to be counted again; a repository that
was never counted shows no count until the indexer ran. The table row goes when the repository is deleted.

## The agent directory

An agent is a name that asc runs under in the repositories of one owner, written `@owner/name`: `@acme/review` is the
agent `review` in the repositories of acme. The same name in the repositories of two owners is two agents. The hub
reads the directory from asc with one `Agents.SummarizeAgents` call over at most 50 asc repositories (the newest
activity first) whose code the viewer can read, so a page never names an agent of a repository the viewer may not see.
Only a signed-in viewer with agent features on gets agents. The answer of asc is kept 60 seconds per set of
repositories (and person, for a profile); a failure is logged and the page shows no agent information. A name that is
not a valid agent name is left out. Code: `services/ascagentdir`.

## Members and agents on organization rows

Each row of Explore organizations shows a member stack: up to four member avatars and **+N** for the rest, with the
number of members as its label. A visitor and a viewer who is not a member see only the public members; a member and
an administrator see all of them. For a viewer who gets agents (see "The agent directory") the row also shows **N
agents**: the agents that ran in the asc repositories of the organization the viewer can read. The repositories of
the organizations of the page are taken in page order, at most 50 for the whole page, so an organization far down a
page with many repositories can show fewer agents than it has. A failed lookup leaves that part out of the row.

## Teams in path rules and policies

A repository's path rules and its policy (`.asc/policy.toml`) may name `team:<name>`, a team of the organization that
owns the repository. asc admits the members it holds for that team, so the hub copies its teams there
(`PathRules.SetTeamMembers`, as the hub itself): a few seconds after a team is created, renamed or deleted, or a
member is added or removed, and every 10 minutes for every organization with asc repositories (cron task
**Copy organization teams to asc for path rules and policies**, `sync_asc_teams`), which also catches renamed and
deleted accounts. The hub is the authority: a team asc holds that the organization no longer has is cleared, and a
team's members are replaced by the hub's list. Team and member names go lower case; asc compares them ignoring case.
Removals are sent first, and one team failing does not stop the others (the run reports every failure). A team whose
name is longer than asc takes (128 characters) is not sent and is logged; no rule can name it. asc keeps names, not
account ids, so when an organization is renamed or deleted the hub clears every team asc holds under the old name,
and when a person is renamed the teams of every organization they are in are sent again under the new login; a later
owner of an old name inherits none of its teams.
When asc is off, unreachable or has no `pathrules.v1`, nothing is sent and the next run repairs it; a rule naming a
team then admits nobody through it, never more.

The team detail on the organization's **Teams** page has a **Policy owner** part when a repository policy names the
selected team: each repository with the roles that list the team and the paths whose writers name the team or one of
those roles (`*` is not counted). The repository links to its policy file. Only repositories the viewer may read the
code of are listed; at most the first 50 asc repositories of the organization are checked and policies are kept for a
minute. A repository whose policy could not be read is counted under the list. Code: `services/ascteams`,
`services/asctrust/teams.go`, `routers/web/org/teams.go`, `templates/org/team/teams.tmpl`. Tests: TestReconcile*,
TestATeamChangeIsSentAfterItCommits, TestReconcileGoesOnPastAFailingTeam, TestARenamedOrDeletedAccountMovesItsTeams, TestOwnershipShowsWhatPoliciesGiveATeam, TestTheTeamPageShowsWhatPoliciesGiveTheTeam.

### Agents in teams

An organization's agents (its asc agent identities, see the People page) can be members of its teams. Owners add one
in the **Agents** part of the team detail on the **Teams** page and remove it there; members see the team's agents
with the paths each may write (**Any path** when the agent has no path limit) and whether it is paused. A team holds
at most 64 agents. Only agents asc lists for the organization can be added. The hub keeps the memberships
(`asc_team_agent`) and sends each as `agent:<name>` with the team's people, so path rules and policies that name the
team admit the agent too. The team does not widen an agent: asc still refuses a draft from an agent that is paused,
kept to other repositories or writing outside its own paths (`AGENT_SCOPE_DENIED`), whatever its teams allow.
When the organization deletes an agent in asc, the next sync drops its memberships, so a later agent with the same
name inherits none of them; until then the team detail marks it **No longer an agent of the organization** and asc is
not sent it. When the agents cannot be read, the sync sends the team's people without its agents (a team then admits
fewer, never more) and the Agents part says the agents could not be read. Deleting a team or the organization deletes
its agent memberships. Code: `models/organization/asc_team_agent.go`, `services/ascteams/agents.go`,
`routers/web/org/teams.go`. Tests: TestAgentsAreTeamMembers, TestTheTeamPageManagesAgentMembers,
Test_addAscTeamAgent.

## Agents and the merge policy on the organization page

The head of an organization page counts its agents beside its people, and the side of the page has an **Agents** card.
A member sees the agents the organization keeps in asc (its organization settings, with their names, `@org/name` and
Paused), linked to the agents on the People page. Anyone else who is signed in with agent features on sees the agents
that ran in the asc repositories of the organization they can read (see "The agent directory"), each linked to its runs
with the number of runs and how many are active; the count in the head then links to the card. A visitor sees neither.

A member also sees a **Merge policy** card with the merge defaults of the organization read from asc: the approvals
required, the required checks, Only people approve, Changes must be signed, Land through the merge queue and the merge
strategy, or "Not set yet". **Read the policy** opens the merge defaults in the organization settings, which a member
who is not an owner reads without changing. When asc cannot be asked the card keeps its lead and the link.

## The merge queue card, on the dashboard

**Merge queue** on the dashboard lists the person's changes that wait in a merge queue (the ones they queued or
wrote, at most 5). Each row says its place ("Number 2 in the queue", "Landing now", "Waits for a new head"), the checks
of its head while they run or fail ("2 of 3 checks"), the estimate asc gives ("lands in about 4 min") and **Open**,
the queue page of its branch. The header shows **Running** while a queue of the rows is landing a change and the
median wait asc measures for it ("Median wait 9 min"). The estimate and the median come from asc's merge queue (one
`MergeQueue.List` for each queue, as the internal credential, after the inbox has checked that the person may read
the pull requests); when asc does not answer they are left out and the rest of the card shows.

## What each row of Needs you says, on the dashboard

Past its section, each row of **Needs you** says what it is about:

- a change that came back to the person quotes the newest reply on it: the newest review asking for changes that has
  text, else the newest comment, never one of the person's own. The row shows the first line with text, at most 140
  letters; the pull request has the rest;
- a change ready to land through the asc merge queue says the place it would take ("Joins the merge queue as number
  2") and, when asc measures one, the median wait of that queue ("· about 9 min");
- a change of a stack that asc restacked after the trunk moved has a **Trunk moved** chip and, when the row has no
  other note, how many changes of the stack asc restacked in the last 24 hours ("asc restacked 2 of 3"). The hub reads
  the newest operation of the branch of each change (one `ListRefLog` call each, as the person, for stacks of 2 to 10
  changes in one repository, each stack once per dashboard); it needs the `stacks.undo.v1` feature;
- an agent run in **Agents working now** that waits for the person shows the question it asks, its current step on
  one line, above the answer box.

A fact that cannot be read (asc off or not answering, a pull request gone) leaves the row as it was; the rest of the
dashboard shows.

## Snoozing an item of the inbox

**Snooze** (key `s`) on the chosen item of the inbox hides it until a time and counts it in "N snoozed pull requests
are hidden". The menu offers:

- **Later today 17:00**, off after 17:00;
- **Tomorrow 09:00** and **Monday 09:00** (on a Monday, the next one);
- **When the checks finish**, while a check of the pull request runs (its latest status is pending). The item comes
  back as soon as no check of that head commit runs, when a new version is pushed, or after a day at the latest;
- **Pick a date**: a day from tomorrow to a year ahead, at 09:00.

The hours are those of the hub's time zone (`[time] DEFAULT_UI_LOCATION`). Snooze on a row of the list, the bulk bar
and a left swipe on a phone hide the item for four hours. Another snooze replaces the one before; Mark done and Wake
end it. A day in the past or more than a year ahead is refused.

## Asking the agent to change a pull request

When an agent opened the chosen pull request of the inbox, the reply box has **Ask agent to change**. The text of the
box becomes a comment of the pull request and the task of a new run of that agent, started as the person, on the
branch of the pull request; the agent makes the change and pushes the next version. The run is listed in Agents with
the title "Change #N as asked in review".

Only a person who may change the pull request is offered the button: its author, an owner or admin of the
repository, or a writer when the stack is open to writers. The pull request must be open, not archived, and its
branch must be in the same repository. asc checks the right to write the repository again when the run starts.

An empty text and a text over 32 KiB are refused. When the run cannot start, no comment is posted and the inbox shows
an error. When the run starts but the comment cannot be posted, the inbox warns; the run still has the instruction.
Each request is written to the log as an `asc audit: inbox ask` line.

## The health check of Get started

The health check card of Get started shows what the hub knows (the email, the SSH key, the first sign-in of the command
line) and what the machine of the person last reported. Run `asc doctor --report` on the machine, in a workspace whose
remote is this hub, or `asc doctor --report https://<hub>` anywhere. The command sends the name and level of each
check, the version of asc and of git, and the operating system and architecture to `POST /api/v1/user/asc/doctor`
with the token of `asc auth login`. It sends no path, no detail text and no secret.

The card then shows Git with its version, the shell shim, every check that warns or fails (failures first) and when
the machine was checked. Fix opens the docs. The hub keeps only the last report of each person, shows it only to them
and trusts it for nothing.

The route takes a token with the repository scope, the scope the command line signs in with. It answers 204 when the
report is kept, 422 when the report is not of the shape `asc doctor` sends (at most 64 checks; names of lower case
letters, digits, `.`, `_` and `-`; levels ok, info, warn and fail), and 404 when asc is off. A refused report keeps
the last one.

## New items, in the open inbox

While the inbox is open and in view, the page asks the hub every minute for the rows of the same tab, repository,
kind and sort (`GET /-/asc/inbox/rows`, signed in, never cached). Rows the list does not show yet come in where the
list puts them, with their day heading when that is new too, and keep a tint for a few seconds. The rows already shown
never move and never go away; a reload drops the ones that left the inbox. When the person has scrolled down, the
list scrolls by the height of what came in above, so the row they read stays where it is. A screen reader hears "2 new
items in the inbox". While a row is being swiped nothing changes; a hidden tab waits and asks again when it comes back.

An empty inbox shows **Show 2 new items** instead, which loads the page with them. Loading the rows marks nothing as
read and never opens one. Without scripts the page is the same and a reload shows the new items. The endpoint answers
404 when asc is off.

## Rows that act in place, on the dashboard

Each row of **Needs you** on the dashboard has one button that does the next step without opening the pull request:

- a review asked of the person: **Approve** sends an approving review at the head of the pull request, as Approve on
  the review page; a person cannot approve their own pull request. The button names the head commit the row showed
  (`head`): when the pull request was pushed to since, nothing is approved and the dashboard says it changed, so
  nobody approves commits they did not see. A row whose head the hub could not read opens the pull request instead;
- a change ready to land in a repository with the asc merge queue: **Add to merge queue** queues the stack up to it, as
  "Merge when ready" (the same permission checks). The dashboard then shows "Added to merge queue" with **Undo** for
  10 seconds, longer while the pointer or the focus is on it; Undo takes the stack out of the queue again;
- changes asked of the person: **Reply** opens the reply box of the pull request; a blocked change: **Resolve** opens
  it; a change ready in a repository without the queue: **Merge** opens it.

The buttons post to `POST /-/asc/dash/act` (`pull`, the id of the pull request, and `do`: approve, queue or unqueue).
A pull request the person may not read is a 404; a refusal (no permission, the queue paused or turned off, an archived
repository, a closed pull request) is a message on the dashboard. An agent run that waits for the person in **Agents
working now** has an answer box; Send posts the answer to the run (`.../asc/agents/runs/{id}/input` with
`back=dash`) and comes back to the dashboard.

While the dashboard is open, **Needs you**, the merge queue and **Agents working now** load again with the Today card,
once a minute (`GET /-/asc/dash/work`, `Cache-Control: private, no-store`), so a review asked, a change landed or a
run that finished shows without a reload. They do not change while the person uses them: the focus in them, or an
answer to an agent typed and not yet sent; the next minute tries again. A failed load keeps them as they are, and the
Today card shows the offline state.

## Chips, agent rows and who is active, in the people directory

The people directory (`/explore/users`) has the chips **All**, **People**, **Agents**, **Maintainers** and **Online
now** under its filter bar, the same choices as **Show**; each chip keeps the search, the order and the organization,
and they stay on a phone with 44 px targets. **Maintainers** keeps the people with that role and is for signed-in
viewers who are not restricted, like the roles; for anyone else `show=maintainers` shows everyone. **Online now** keeps
the people the viewer would see as online on their profile: a request in the last ten minutes, "Show when I am online"
on, and activity not kept private (a site admin and the person themself see past that).

For a viewer with agent features on, the first page of All and Agents starts with rows for the agents that ran in the
asc repositories the viewer can read (the agent directory, at most 50 repositories): a violet square avatar,
`@owner/name`, the owner, the repositories it ran in, the last run, the number of runs and **View runs**, which opens
its runs in its busiest repository. All shows five and **Show all agents (N)**; Agents shows all of them. The search
keeps the agents whose `owner/name` contains it; an organization filter keeps that organization's agents.

The side of the page has **Who is active now**: up to five people online in name order, with the repository of their
last public action when it was in the last hour, "and N more online" linking to Online now, and, for a viewer with
agent features on, **Agents at work** with the agents that have runs going. With no one it says "No one is active
right now."

## Agents a person runs, on the profile

The side of a profile has an **Agents <name> runs** card for a signed-in viewer with agent features on. It names, most
runs first, at most five agents with runs the person created in the asc repositories of the person and those the person
can reach, as far as the viewer can read their code: `@owner/name` (it opens the runs of that agent in its busiest
repository), the number of runs and that repository, and how many are going. Past five it says how many more. The agent
directory asks asc once per profile and keeps the answer for a minute. On one's own profile the card has **New agent**
(the agents page) and, with no runs yet, "No agent runs yet."; on someone else's profile with no runs the card is left
out, and it is left out when asc cannot be asked.

## Checks and Watch on organization rows

Each repository row of an organization page says how the checks of the head of its default branch went: **Checks
passing**, **N checks failing**, **Checks running** or **No checks**, linked to the commits of that branch. The latest
status of each check (commit status context) counts; a failure or an error fails, a pending check runs, a warning
passes. The text shows only to someone who may read the code of the repository, because checks name commits and tests;
an empty repository shows nothing. A failure to read the statuses is logged with the request id and the rows show no
check text.

A signed-in person also gets a **Watch** (or **Unwatch**) button with the watcher count on each row, and the same item
in the phone row menu. The button posts a plain form that comes back to the page (`redirect_to`, an address inside the
hub only); the Watch control in the repository header still swaps in place with htmx. The Star button of the rows also
comes back to the page it was on now, not to Explore.

The rows are `repoquick.LoadChecks` and `repoquick.LoadWatching` (`routers/web/shared/repoquick/rowstate.go`), called
from `routers/web/org/home.go`, and drawn in `templates/explore/repo_list.tmpl`. Tests: TestTheRowCheckSumsUpTheChecks,
TestTheOrgRowsShowChecksAndAWatchToggle.

## Request a review and Mention, on a profile

A signed-in person who looks at someone else's profile gets **Request a review** and **Mention** beside Follow (not on
an organization, not on one's own profile, not on a private profile the viewer may not see). Each opens a dialog that
loads, when it opens, a picker of the viewer's own open pull requests, the 20 most recently updated, keeping only those
the person may read, in a repository that is not archived and not locked for the viewer. Request a review also keeps
only those where the viewer may change the review requests and the person may review. A person who blocked the viewer
gets an empty picker.

**Request review** adds a review request, as the reviewer box of the pull request does: the person is told, it shows in
their inbox and the hub passes it to asc as `RequestReview`. The page comes back to the profile with a note naming the
pull request. **Post comment** posts the text as a comment on the pull request, with `@name` put first unless the text
already mentions the person, and opens the comment. A comment must have text and at most 64 KiB.

Every POST checks the pull request again: it must be the viewer's, open, and allowed as above; anything else is a 404
and an unknown `pull` a 400. The server logs each request and mention (`asc audit: profile …`). The routes are
`GET` and `POST /{username}/-/ask/{review|mention}` (`routers/web/user/asc_profile_ask.go`) and the picker is
`templates/user/asc_profile_ask.tmpl`. Tests: TestAscProfileAskPicker, TestAscProfileAskReview,
TestAscProfileAskMention, TestAscMentions, TestAscProfileAsk.

## Contributions on a profile

The Contributions card on a profile (the overview and the Activity tab) draws a square per day. Above the grid a year
switch offers **Last 12 months** and each calendar year from this year back to the year the person joined, at most ten;
the choice is `?year=` and stays on the tab. A year that is not offered shows the last twelve months. The card header
gives the total of the range.

Everything is counted from the hub's own data, for the viewer: the person's activity (as the dashboard grid), the pull
requests they opened, the pull requests the hub opened for agent runs the person started (`pull_request_asc_run`,
matched on `started_by`) and the reviews they gave (approve, comment, request changes). A pull request counts only
when the viewer may read the pull requests of its repository, and nothing is shown when the person keeps their
activity private from the viewer. A day where agent runs made most of the changes is violet, and its tooltip says how
many came from agents; a run's pull request that the person posted counts once.

Under the grid the summary gives changes (agent runs left out), changes by agents, reviews and the median review
turnaround: for each pull request of someone else the person reviewed, the time from its opening to their first
review. A day with activity opens the Activity tab for that day (`?tab=activity&date=YYYY-MM-DD`). **Show as a table**
lists the days with activity, newest first, with links: the way to reach the days with a keyboard or a screen reader,
since the grid itself is one image to them. Code: `routers/web/user/asc_profile_contrib.go`,
`models/activities/asc_user_heatmap.go`, `templates/user/asc_heatmap.tmpl`. Tests: TestAscHeatLayout,
TestAscContribYears, TestAscTurnaround, TestAscContribCounts, TestAscProfileContributions.

## Knows best on a profile

The overview of a profile has a **Knows best** card at the top of its side column: the three directories the person
changes and reviews most in the last six months, each with its share of all the files they changed or reviewed and
a bar. A path is cut to its first two directories (`src/payments/`); a file at the root counts as `/`. The tooltip of
a row gives the files changed and the files reviewed in it.

It is worked out by the hub alone. Changes are the pull requests the person opened or that the hub opened for agent
runs they started; reviews are the pull requests of others they approved, commented on or asked changes of. Each
side reads at most 30 pull requests, the most recently updated, and only those whose repository's pull requests the
viewer may read; nothing is shown when the person keeps their activity from the viewer or there is nothing to count.
The files of a pull request are those changed between its merge base and its head, read from the repository and kept
in `pull_request_asc_dirs` per head commit, so a page view reads the repository only for pull requests pushed since
(at most ten per view; the rest are counted on a later view). Code: `routers/web/user/asc_profile_knows.go`,
`models/issues/pull_asc_dirs.go`. Tests: TestAscKnowsDir, TestAscKnowsRank, TestPullRequestAscDirs,
Test_addPullRequestAscDirs, TestAscProfileKnows.

## Landing rate on the history

The history of a branch (`/{owner}/{repo}/commits/branch/{name}`) has a **Landing rate** card at the top of its side
column: the changes that landed on the viewed ref this week, how that compares with last week (`+4 on last week`,
`2 fewer than last week` or `Same as last week`) and a bar per week for the last twelve weeks, this week's bar in the
link colour. A week is seven days ending now, the same weeks as the Activity card of the repository home. The card is
left out when nothing landed in the twelve weeks or the repository cannot be read.

It is worked out by the hub alone, with one `git log --since` on the viewed ref, for git and asc repositories alike;
no asc call is made. Code: `routers/web/repo/commits_landing.go`, `templates/repo/commits.tmpl`. Tests:
TestHistoryLandingOf, TestHistoryLandingTimes, TestAscHistoryLandingRate.

## Removing an agent from a repository

On the access page of a repository (`Settings`, `Collaborators`), **Remove** on an agent's row opens its own confirm:
a run of the agent that is going now stops, and the pull requests it already opened stay open, owned by the person
who started each run. A person's row keeps the plain confirm.

When an agent account is removed from an asc repository, the hub asks asc for the agent's runs on the repository that
are queued, running or waiting for input (`Agents.ListRuns`) and stops each with `Agents.CancelRun`, as the person
who removed it, with the reason "The agent was removed from the repository." The removal itself never waits on asc:
when asc cannot be reached the agent is still removed and the page warns that its runs could not be stopped, so stop
them from the agent runs page. A git repository has no agent runs, so nothing else happens. The agent's name comes
from the account's login (`agent-<name>`); a shortened login is matched against the agents of the repository's runs.
Code: `services/ascagentui/removeagent.go`, `routers/web/repo/setting/collaboration.go`. Tests: TestAgentOfUser,
TestStopAgentRuns, TestStopRemovedAgentOutsideAsc, TestAccessAgentRows, TestAscAccessRemoveAgent.

## Replacing an AI key

**Replace** on a key's row (`Settings`, `AI`, for a person, an organization or a repository) opens a dialog for the
new key. asc tries the new key with the provider first (`AiProviders.ReplaceKey`) and swaps it in only when the
provider accepts it; until then the old key stays in use. When the provider refuses the key, the dialog says which
provider refused it: "Anthropic rejected this key (401 or 403). The old key is still in use." A key that cannot be
checked, because the provider cannot be reached or is limiting requests, is not swapped either, and the dialog says
so. Code:
`routers/web/shared/byoweb/aifeatures.go` (`AIReplace`, `replaceRefusal`), `templates/shared/asc/byo_ai_dialogs.tmpl`.
Tests: TestAIReplace.

## Who used an AI key

The **Activity** tab of a key's page (`Settings`, `AI`, a key's **Details**) starts with **Who used this key**, read
from asc's `AiProviders.ListActivity`. Each row sums the calls of one agent run, one person's review summaries of one
change, or one day's key tests, with the person, the repository, the model, requests, input and output tokens and the
cost; calls the price list does not cover are counted as unpriced. Agent runs link to the run when the viewer may read
the repository's code and its agents are on; review summaries link to the change's pull request or change page when
the viewer may read pull requests; repositories link only when the viewer may read their code. A repository the
viewer cannot read, or one that no longer exists, is shown by name only. asc keeps uses for 90 days; the table shows
50 rows a page, newest first, with **Older** and **Newest**. A key of an organization is shown only to its
administrators. When asc is older and has no `ListActivity`, the table says the uses could not be loaded and the
rest of the page works; the key's events below it are the hub's own audit log. Code: `services/ascbyo/uses.go`,
`routers/web/shared/byoweb/uses.go`, `templates/shared/asc/byo_ai_key.tmpl`. Tests: TestKeyUses, TestAIKeyUses.

## What uses AI

The AI page lists each feature that uses a key: agent runs and review summaries each have the key they use, a monthly
cap and a spend meter (amber at 80%, red at 100%), and a feature over its cap pauses until the next month or until
the cap is raised (`AiProviders.ListFeatures`, `SetFeature`). Meaning search is listed too, with "This server" and
"No spend": it runs on the asc server's own embedding model and never uses a key, so there is nothing to choose.
Code: `templates/shared/asc/byo_ai_features.tmpl`. Tests: TestUserAIPageShowsFeatures.

## Search indexes on the storage page

The storage page of an asc repository (`Settings`, `Storage`) has a **Search indexes** card, read from asc's
`CodeIntel.IndexStatus` for the repository:

- **Text and symbol search**: how much of the default branch is indexed, or "Not indexed yet".
- **Meaning search**: on, with how much is indexed and the embedding model; off for this repository, with a link to
  the repository settings where it is switched on again; or off on this server.
- How far the indexes are behind the default branch, in minutes.

Meaning search is off on the server until an instance administrator installs the embedding model on the asc server:
put the `asc-embed` helper, the `model/` directory and `calibration.bin` in one directory (the layout
`asc codeintel enable-embed` installs), set `[codeintel.embed] dir = "<that directory>"` in `asc-serve.toml` and
restart asc. The hub needs no change. When asc cannot be reached the card says the state of the indexes could not be
read; search and the rest of the page still work.

A meaning search on a repository without the meaning index says "Meaning search needs the meaning index, which is not
on for this repository yet. An admin can enable the index in storage settings.", with **Use text search** and, for
repository admins, **Open storage**. Code: `services/ascbyo/searchindex.go`, `routers/web/shared/byoweb/storage.go`,
`templates/shared/asc/byo_storage_repo.tmpl`, `templates/repo/asc_search.tmpl`. Tests: TestSearchIndexOf,
TestRepoStorageSearchIndexes, TestAscSearchMeaningIndexOff.

## Where repositories keep their objects

The Storage column of **Site administration > Repositories** names the storage target each repository on the page writes
its objects to, or "Server default" when none is assigned. "Moving to this storage" means a migration is copying the
objects there. A warning under the name means where the objects are and where they should be disagree:

- "This storage no longer exists." The assigned target was deleted. Assign another target, or the server default, on the
  repository's storage page.
- "This storage is not enabled." The target was disabled or failed its check. Enable it again on the owner's storage
  page, or move the repository.
- "Some objects are still in an earlier place." The repository was given another target without a migration, so new
  objects go to the new target and older ones are read from the earlier place. Start a migration on the repository's
  storage page to move them.

"Not known" means asc did not answer; the page itself still works. The hub asks asc once per hundred repositories on the
page (`Storage.ListRepoPlacements`, a site administrator's call).

Deleting a repository from this page asks for its full name (owner/name, case ignored); the delete button stays off
until it matches, and the server refuses a delete whose typed name differs.

## Your repositories in settings

**Settings > Repositories** lists the repositories you own. Each row shows whether it is public or private, its
description, how many open stacks it has and the storage target its objects go to ("Server default" when none is
assigned; the same warnings as the site administration list). The stack count is the number the repository's Stacks
page lists: every open pull request is in one stack, named by its top change. The hub counts it from its own records;
only the storage line asks asc (`Storage.ListRepoPlacements`, once per hundred repositories on the page).

**Transfer** asks for the new owner and the repository's full name, then starts the same transfer as the repository's
own settings. A transfer to another user waits for them to accept; until then the row says "Transfer pending" and
links to the repository settings, where it can be cancelled.

When you may adopt or delete repositories (site administrators, or when the server allows it), an **Unadopted** card
lists directories under your name in the repository root that have no repository record, up to 100 at a time; adopt or
delete them to see more.

## Signing changesets by default

**Settings > Signing keys** has a **Sign my changesets by default** switch. When it is on, every changeset you author
is signed with your key without a flag. The value is kept by asc with your account (`Signing.SetSigningDefaults`), so
the web page and every machine agree, and each change of it is in the audit log as `signing.defaults.set`. The card is
not shown when the asc server is older than the switch; the rest of the page works as before.

On a machine, `asc signing default` shows the value and `asc signing default on` or `off` sets it. Both keep a copy in
the global config key `asc.signByDefault`, which commands that write changes read, also offline. A machine follows a
change made on the web page the next time `asc signing default` runs there. A `commit.gpgsign` set in a repository or
globally wins over the account value; `asc signing default` says so when the two disagree.

## Following a storage migration

Under the progress bar the storage page shows the objects copied, the bytes moved once asc has counted them (for example
"1.2 GiB of 4 GiB"), and while the copy runs, roughly how long it has left. The hub estimates the time from the pace so far: the share
moved since the migration started, by bytes when they are counted and by objects before that. There is no estimate while the
migration waits, is paused, or checks the copies, and time spent paused makes the estimate cautious until the pace catches up. The
check compares every copy with its source and copies again any that is missing or different; the page then says how many it
copied again, and the migration history keeps that count after the migration is done. A few redone copies are normal when the
destination already held objects from an earlier attempt. The status endpoint the page polls
(`GET .../asc/storage/migrations/{id}/status`) also returns `bytes_copied`, `bytes_total`, `eta_seconds` and `mismatched` for scripts.

## Pausing a storage migration

While a repository's objects move to another place, the storage page shows the progress with **Pause** and **Cancel and keep
current**. Pause holds the migration before the switch: the asc worker stops after the chunk it is on (at most 64 objects), keeps
what it copied and where it was, and new writes still go to both places, so nothing is lost while it waits. The page then says
**Paused** and offers **Resume**, which carries on from where it stopped without copying anything again. A paused migration is still
the repository's migration: another one cannot start until it is resumed and finishes, or is cancelled. Cancel and keep current
works on a paused migration as well and leaves the objects where they were. Once the migration is switching over there is nothing
left to pause, and the buttons go away. Pausing and resuming are recorded in the audit log (`storage.migration.paused`,
`storage.migration.resumed`) and in the hub log.

## The plan on the storage page

When quotas are on (`[quota] ENABLED = true`) and an asc repository keeps its objects on the server's storage, the
**Usage** card of its storage page shows how much of the owner's plan is used, for example "38.2 GB of 100 GB on
your plan", with a meter (amber from 80%, red when full) and **Use my own bucket**, which goes to the target select,
or to Add a target when the repository has none it may use. The plan is the hub's quota: the limit on repository data
(`size:git:all`, or a rule on `size:all` that contains it) of the owner's quota groups, the group that leaves the
most room, or `[quota.default] TOTAL` when the owner is in no group. Without a limit, or with quotas off, there is no
plan line. The plan counts every repository of the owner, so only people who administer the owner (the user
themselves, an organisation owner or a site admin) see it; other repository admins see the usage without it.

asc says where the repository keeps its objects (the server's storage or a target, and an earlier place objects
are still in until a migration moves them) and how much it keeps. Each time the page is shown, the hub records asc's
last count as the repository's size, large files apart, so the quota counts an asc repository like a git one; a
repository whose objects are all on its own bucket counts as nothing, and while objects are moving the size is left
as it is. The target in use is selected in **Where new objects go**. Code: `models/quota/limit.go`,
`routers/web/shared/byoweb/plan.go`, `templates/shared/asc/byo_storage_repo.tmpl`. Tests: TestAllowanceFor,
TestRepoStoragePlan, TestRepoStorageAssignShowsTheCurrentTarget.
