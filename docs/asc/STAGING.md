# ASC Hub staging

A staging instance of ASC Hub (the ASC fork of Forgejo, see [README.md](README.md)) at `https://asc-staging.example.org`.
It has its own SQLite database in its own volume. It shares nothing with production Forgejo or its Postgres.

## Deploy

1. Push `asc/v16`. The `asc-image` workflow publishes `hub.example.org/asc/asc-hub:<sha12>` and `:asc-v16`.
2. In Coolify, create a Docker Compose resource from `deploy/coolify/forgejo-staging.compose.yml` (source-control repository).
3. Set `ASC_HUB_IMAGE_TAG` to the `<sha12>` tag to run. The compose file refuses to start without it. Pin the sha, not `asc-v16`.
4. Deploy. Coolify routes the domain to port 3000 (`SERVICE_FQDN_FORGEJO_3000`).

Settings come from `FORGEJO__section__KEY` environment variables. The application name is `ASC Hub` (`FORGEJO__DEFAULT__APP_NAME`). The install is locked, registration is off, Actions are on, and the default theme is `asc-auto`.

## Create the admin user

Run once after the first healthy start, in the Coolify terminal for the `forgejo` service (user `git`):

```sh
forgejo admin user create --admin --username asc-admin --email admin@example.org   --random-password --must-change-password=true
```

The command prints a generated password once. Copy it from the terminal and set a new one at first sign-in.

The `forgejo admin user create` command accepts a password only as the `--password` argument. A password in argv is visible in `ps` and shell history. Do not pass `--password`. If you need a chosen password, create the user with `--random-password`, sign in, and change it in the web UI.

## Update

Build a new image, set `ASC_HUB_IMAGE_TAG` to the new `<sha12>`, redeploy. The volume keeps the data. To roll back, set the previous tag. The database schema may have migrated forward, so restore a volume backup if the older image refuses to start.

## Limits

Read-only root filesystem, all capabilities dropped, `no-new-privileges`, memory and pids limits via `deploy.resources.limits`. Only `/var/lib/gitea` (volume) and `/tmp` (tmpfs) are writable.
