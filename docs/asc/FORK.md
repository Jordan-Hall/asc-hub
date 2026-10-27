# The fork: how ASC Hub differs from Forgejo, and how upstream is merged

ASC Hub is a fork of [Forgejo](https://forgejo.org). It is not Forgejo, it is not an official Forgejo release, and it is not a git forge. This page names the base, says what the fork changes and what it leaves alone, and gives the procedure for taking in upstream releases. The licence and attribution rules are at the end.

## Base

| | |
| --- | --- |
| Upstream | Forgejo, <https://forgejo.org>; source at <https://codeberg.org/forgejo/forgejo> |
| Base release | v16.0.6, commit `1aa13944143b3bc4312607239a68da23512d5ce3`, "2026-10-12 security patches", branch `v16.0/forgejo` |
| Fork branch | `asc/v16` |
| Size of the difference | 168 commits, 668 files changed (about 469 added, 190 modified, 8 deleted, 1 renamed) on 2026-10-26. Run `git diff --stat 1aa13944143b..asc/v16` for the current figure. |

The full history of Forgejo, and before it of Gitea, is in this repository's git log. [BASELINE.md](BASELINE.md) records the test baseline of the base.

## What differs

The fork replaces the repository layer and the branding. It adds, in this order of importance:

1. **Storage and writes.** A per-repository `storage_backend` (`git`, `dual`, `asc`), a writer interface with a git and an asc implementation, and an asc client. Paths: `modules/repostore`, `modules/ascclient`, `modules/ascproto`, `modules/git/ascbackend`, `modules/gitrepo/backend_select.go`, `modules/setting/asc.go`.
2. **Compatibility layer.** The git bridge and the push import: `services/ascbridge`, `services/ascsync`, `services/lfs/asc.go`.
3. **Review model.** Stacked pull requests, versions and interdiff, restore, revert, the merge queue, change-class approvals: `models/issues/pull_asc_stack.go`, `services/pull/*asc*.go`, `services/automerge/asc_stack_queue.go`, `templates/repo/pulls/asc_*.tmpl`.
4. **Internal API for `asc serve`.** `routers/private/asc*.go`, `modules/private/asc.go`, `GET /.well-known/asc`.
5. **New pages and APIs.** The inbox, drafts, repository search, graph and compare, the read-only RPC proxy: `routers/web/user/asc_*.go`, `routers/web/repo/asc_*.go`, `routers/api/v1/user/asc_inbox.go`, `services/ascinbox`.
6. **Sign-in for IDEs.** The OAuth2 public client `asc-ide` (`routers/web/auth/oauth_asc_ide.go`).
7. **Actions.** `permissions:` for the job token with enforcement ([actions-token-permissions.md](actions-token-permissions.md)) and the asc context variables.
8. **Database tables.** Migrations `v17b`, `v17c`, `v17d`, `v17e`, `v17g`, `v17h` in `models/forgejo_migrations`.
9. **Branding and UI.** Name, logo, themes, landing page, footer, mobile-first CSS: [BRANDING.md](BRANDING.md), [THEME.md](THEME.md), [ASC-NATIVE-UI.md](ASC-NATIVE-UI.md).
10. **The all-in-one image.** `Dockerfile.allinone` (a copy of the build stage of `Dockerfile.rootless` plus a Debian runtime that carries the `asc` binary from the published `asc-serve` image), `docker/allinone` (the supervising entrypoint, the combined health check and the backup tool), `scripts/smoke.sh` (the post-deploy check) and `tests/allinone` (their tests). `Dockerfile.rootless` is left as upstream so that merges stay simple; when upstream changes its build stage, apply the same change to `Dockerfile.allinone`. [OPERATIONS.md](OPERATIONS.md) documents the image.
11. **CI.** `.github/workflows/asc-hub-test.yml` (including the `image-scripts` job), `.github/workflows/asc-hub-image.yml` (builds `Dockerfile.allinone`), `.forgejo/workflows/asc-fork.yml`, `.forgejo/workflows/asc-image.yml` (still builds the hub-only `Dockerfile.rootless`).

Existing upstream files that the fork edits (190 at the time of writing) are mostly in `services/repository`, `services/pull`, `modules/git`, `routers/web/repo`, `routers/private`, `routers/api/v1`, `models/repo` and `models/git`. These are where a merge from upstream can conflict.

## What is kept as upstream

Kept on purpose so that upstream changes still apply:

- The Go module path `forgejo.org`, the binary `gitea` and its `forgejo` link, the `forgejo` command line, `forgejo doctor`.
- The `FORGEJO__<section>__<KEY>` environment prefix (the code also accepts `GITEA__`), cookie names, the work directory layout (`/var/lib/gitea` in the image), and the `models/forgejo_migrations` package.
- The "Forgejo" webhook type, the Forgejo and Gitea themes (still selectable), the Forgejo and Gitea migration sources, and the Forgejo Runner for Actions.
- `CONTRIBUTING.md`, `RELEASE-NOTES.md`, `release-notes*`, and the upstream workflows under `.forgejo/workflows`.
- The `forgejo` command name and the `FORGEJO_` / `FORGEJO__` prefixes in command line help and messages, upstream test data, and the translations other than English.

Changed from upstream, so that nothing user-visible reads like Forgejo: the OCI labels in `Dockerfile` and `Dockerfile.rootless`, the first-start default of `APP_NAME` in the container templates (`docker/root/etc/s6/gitea/setup`, `docker/rootless/usr/local/bin/docker-setup.sh`), `custom/conf/app.example.ini`, the usage line and working-path help in `cmd/main.go`, `contrib/systemd/forgejo.service`, the `APP_NAME` and `APP_SLOGAN` of the test configurations in `tests/*.ini.tmpl`, and the title assertions in the integration and end-to-end tests.

## String and template overrides

User-visible text says ASC Hub. To keep upstream merges manageable we follow these rules.

- **English only, in the files the fork owns.** We change `options/locale/locale_en-US.ini` and `options/locale_next/locale_en-US.json`. Other languages stay as upstream ships them, so in other languages some text still says Forgejo until a translation catches up. Do not edit the other locale files; upstream's translation tooling rewrites them.
- **New keys, not changed meanings.** When a string's meaning changes (for example "Trunk" for the default branch on an ASC repository), add a new key and switch the template, and leave the old key as it was. The keys added by the fork: `startpage.*` (rewritten), `based_on`, `why_asc_hub`, `repo.trunk*`, `repo.ignore_file*`, `repo.new_repo_helper_asc`, `repo.migrate.git_*`, and the `repo.pulls.asc_*` family.
- **Replace the product name, keep the software names.** "Forgejo" in a sentence about the product became "ASC Hub". Names of other software or formats stay: Forgejo Runner, the Forgejo webhook type, the Forgejo and Gitea migration sources, the Forgejo and Gitea themes, `FORGEJO_` variable prefixes.
- **One attribution.** The footer shows "Powered by ASC Hub" and a "Based on Forgejo" link (`templates/base/footer_content.tmpl`, shown while `[other] SHOW_FOOTER_POWERED_BY` is true). Do not remove it, and keep the licenses link. A site that overrides the footer from `custom/` must keep both.
- **After a merge,** run `git grep -n "Forgejo" -- options/locale/locale_en-US.ini options/locale_next/locale_en-US.json templates` and check each new hit: change a product reference, keep a software name. Tests that assert English text (`tests/integration/footer_test.go`, `opengraph_test.go`) already use the ASC Hub text.
- **API documentation.** The Swagger title is "ASC Hub API": `SWAGGER_SPEC_BRANDING` in the `Makefile` and `templates/swagger/v1_json.tmpl` change together.

## Merging upstream

There is no `upstream` remote in a fresh clone. Add it once:

```sh
git remote add upstream https://codeberg.org/forgejo/forgejo.git
git fetch upstream --tags
```

Security fixes first, then patch releases of v16, then (later) a new major release. For a patch release, for example a tag `v16.0.7`:

```sh
git switch asc/v16 && git pull
git switch -c merge/v16.0.7
git merge v16.0.7            # resolve conflicts in the files listed above
```

Merge, do not rebase, when `asc/v16` is shared: images are tagged by commit id and others may have the branch checked out, and a rebase would rewrite every one of the fork's commits. If you prefer a patch stack on top of a new base (for example to prepare a clean major upgrade), `git rebase --onto <new-tag> <old-tag> asc/v16` does it, and everyone who has the branch must then reset to it; do this on a new branch name such as `asc/v17`.

When resolving conflicts:

1. Keep upstream's change and re-apply ours on top. Do not drop an upstream security fix to keep a fork change.
2. Where upstream changes a function that the fork routes through `modules/repostore` or `modules/gitrepo`, make the new code go through the same seam (see `services/repository/*.go` for examples) rather than calling git directly on an ASC repository.
3. Migrations: the fork's files are `v17b`, `v17c`, `v17d`, `v17e`, `v17g` and `v17h` (there is no `v17f`); upstream's last one at the base is `v17a`. Each registers itself in `init()`, so the file names decide the order. After a merge, list `models/forgejo_migrations` and check where upstream's new migrations sort. Never reorder or edit a migration that has already run on a database. If an upstream migration would sort in front of fork migrations that have shipped, stop and settle the order before releasing, because databases that ran the old order would disagree with it.
4. Locale files: take upstream's version of every file other than the two English files, and merge those two by key.

To see what a merge costs before you commit to it, start it without committing, read the conflicts, and abort:

```sh
git merge --no-commit --no-ff v16.0.7
git diff --name-only --diff-filter=U      # the conflicting files; none means a clean merge
git merge --abort
```

Check the two files that the fork copied from upstream and keeps in step by hand:

```sh
git diff 1aa13944143b..v16.0.7 -- Dockerfile.rootless docker/rootless    # nothing means nothing to copy
diff <(sed -n '/AS build-env/,/^# Copy local files/p' Dockerfile.rootless) \
     <(sed -n '/AS build-env/,/^# Copy local files/p' Dockerfile.allinone)
```

The second command shows only the differences that are on purpose: comments, and `environment-to-ini` built with `CGO_ENABLED=0` for the Debian base. Anything else is a build-stage change to copy into `Dockerfile.allinone`.

Then verify:

```sh
go build -tags 'sqlite sqlite_unlock_notify' ./...
make test-backend
make test-sqlite-migration test-sqlite
tests/allinone/run.sh
```

`tests/allinone/run.sh` runs the tests of the image scripts and needs only Linux, bash, curl, git, ssh and python3; with `ASC_ALLINONE_COMPOSE_FILES` set it also checks your deployment's Compose files.

`.forgejo/workflows/asc-fork.yml` records the passing test list of the base as an artifact; a test may leave that list only with a note ([BASELINE.md](BASELINE.md)). Check that the footer still has the attribution, that `/.well-known/asc` answers on a test instance, and that a git push to an ASC repository is imported (`services/ascsync`). Record the new base tag and commit in this file and in [BASELINE.md](BASELINE.md).

## Licence and attribution

- Forgejo is licensed GPL-3.0-or-later (from v9.0; earlier versions were MIT), and so is ASC Hub. The `LICENSE` file is Forgejo's and is unchanged.
- Every upstream copyright notice and SPDX header stays. Files that Forgejo took from Gitea keep their MIT headers. New files added by the fork carry their own header with an SPDX identifier.
- `assets/go-licenses.json` and `/assets/licenses.txt` list the licences of the dependencies.
- ASC Hub's README, the footer and this page credit Forgejo, and through it the Gitea contributors.
