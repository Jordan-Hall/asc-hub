# ASC Hub

ASC Hub is the server for ASC (Agent Source Control). It hosts `asc` changesets and stacks, review, the inbox, and CI (Actions).
It is a fork of [Forgejo](https://forgejo.org) v16 and is not Forgejo or a git forge; [FORK.md](FORK.md) says how it differs and how upstream is merged. The repository is `ASC/asc-hub`.
The image is `<REGISTRY>/asc/asc-hub`, with the registry host taken from the `REGISTRY` repository variable.

## How it relates to asc

- `asc` is the client: the CLI and the IDE tooling. It makes changesets and stacks and submits them.
- `asc serve` is the storage and RPC server. It owns the repository data.
- ASC Hub is the web side: the web UI, sign-in and OAuth for IDEs, review, Actions and the package registry.
  It asks `asc serve` over RPC for repository data and for authorization.

## What the fork adds

Only changes that are in the `asc/v16` log (unit ids in brackets):

- Storage seams: a `Backend` and a `Writer` interface, with the git CLI implementation and an asc backend (K03 to K05, K08, K10).
  `storage_backend` is chosen per repository; `dual` runs asc with a git shadow and samples differences (K11).
- Authorization: an internal authz endpoint, SSH key lookup (used only when SSH is enabled), and path hiding (K02, K07, K19).
- Git pushes are imported into asc, and `refs/pull/*` moves through asc first (K09).
- Relaxed approvals by change class on branch protection, and a per-file class badge (K17).
- Actions: a `permissions:` block with enforcement for the API, git, LFS and asc (K20), and `packages: write` for the job token (K20, K20b).
  See [actions-token-permissions.md](actions-token-permissions.md). The Actions context has `asc_server_url`, `asc_changeset` and `asc_base_merge_base` (K14).
- OAuth2 public client `asc-ide` with PKCE and IDE redirects (P06).
- A read-only asc RPC proxy at `/-/asc/rpc` (P04).
- A commit graph page at `/asc/graph` (C42).
- Agents and workflows pages behind a per-person switch that is off by default (H5). See [AGENTS-UI.md](AGENTS-UI.md).
- The ASC themes `asc-auto`, `asc-light`, `asc-dark` (see [THEME.md](THEME.md)).
- A CI baseline and the `asc-image` workflow (K01, see [BASELINE.md](BASELINE.md)).

## What it keeps from Forgejo

Git hosting, issues, pull requests, wikis, releases, the package registry and Actions all remain.
Git-compatible clients (`git` over HTTPS) still work. Git over SSH works only when the operator turns it on (off by default; HTTPS is the only inbound port a hub needs).

## Licence and credit

Forgejo is GPL-3.0-or-later, and so is ASC Hub. The `LICENSE` file stays, and all upstream copyright notices stay.
ASC Hub is built on the work of the Forgejo and Gitea contributors. The top-level `README.md` is ASC Hub's own; the Forgejo attribution is in its "Fork and attribution" section and in [FORK.md](FORK.md).

## Read first

- [WHY.md](WHY.md): why ASC Hub exists, and the benefits with their status.
- [ARCHITECTURE.md](ARCHITECTURE.md): components, data ownership, request flows.
- [OPERATIONS.md](OPERATIONS.md): deploy, volumes, ports, backup, upgrade.
- [STACK-OPERATIONS.md](STACK-OPERATIONS.md): merge queue, merge now, restack and revert of a stack on the web.
- [STORAGE-AND-KEYS.md](STORAGE-AND-KEYS.md): bring your own storage and AI keys: pages, protections, tests.
- [FORK.md](FORK.md): the difference from Forgejo and the merge procedure.

## Run it

- Staging: [STAGING.md](STAGING.md). The default `APP_NAME` is `ASC Hub` (`FORGEJO__DEFAULT__APP_NAME`).
- Theme: [THEME.md](THEME.md).

## Upstream policy and names

- `asc/v16` takes in upstream Forgejo v16 releases; the procedure is in [FORK.md](FORK.md). Security fixes from upstream come first.
- User-facing name: ASC Hub. This covers the UI, docs, the image and the repository.
- Internal names stay as upstream: Go packages, the module path, the `forgejo` binary, API paths and `FORGEJO__` environment variables.
  Renaming them would break merges of upstream fixes.
