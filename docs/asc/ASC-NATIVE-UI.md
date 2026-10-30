# asc-native repository lifecycle in the UI

ASC Hub stores repositories in asc (`[asc] WRITER = asc`, `NEW_REPOSITORIES = asc`; see `asc-stage4-writer.md`). The
pages that create, import, clone and start a repository say so. All changes are templates, English locale values and
one template flag. No route, API or i18n key was renamed or removed; keys whose meaning changed got a new key.

## How a page knows

- Create page: `ctx.Data["NewRepoIsAsc"]` (set by `Create` and `CreatePost` in `routers/web/repo/repo.go`). It is true
  when a repository created now is stored in asc, with the same condition as `newRepoStorageBackend` in
  `services/repository/create.go`: `[asc] ENABLED`, `WRITER = asc` and `NEW_REPOSITORIES = asc`.
- Repository pages: the template asks the repository, `eq .Repository.GetStorageBackend "asc"`. No Go change.
- The wiki clone panel stays git (a wiki is a git repository).

## Create page (`templates/repo/create*.tmpl`)

When `NewRepoIsAsc`:

| Item | Before | After |
|---|---|---|
| Initialize help (`repo.auto_init_description`, all instances) | Start the Git history with a README and optionally add License and .gitignore files. | Start the history with a README and optionally add a license and an ignore file. |
| `.gitignore` label, placeholder, help | `.gitignore`, `repo.repo_gitignore_helper`, `repo.repo_gitignore_helper_desc` | `repo.ignore_file` ("Ignore file"), `repo.repo_ignore_helper`, `repo.repo_ignore_helper_desc` (new keys) |
| Object format (SHA1/SHA256) choice | shown when more than one format is supported | not shown; a hidden `object_format_name` with the default (sha1) is sent, which is what asc storage requires |
| "Default branch" label and help | `repo.default_branch`, `repo.default_branch_helper` | `repo.trunk` ("Trunk"), `repo.trunk_helper` (new keys); the field name `default_branch` and its value are unchanged |
| Link to migration | `repo.new_repo_helper`: "Migrate repository" | `repo.new_repo_helper_asc`: "Import history from a Git repository", linking to `/repo/migrate?service_type=1` (the plain Git form) |

Git storage keeps every old string, the format choice and "Default branch".

## Import (`templates/repo/migrate/`)

- `git.tmpl` (service "Git"): heading `repo.migrate.git_title` "Import history from a Git repository", help
  `repo.migrate.git_help`, field `repo.migrate.git_clone_address` "Git repository URL" with
  `repo.migrate.git_clone_address_desc`. The other services keep `repo.migrate.clone_address` and the old heading.
- `migrate.tmpl` (service picker): one help line, `repo.migrate.select_help`.

## Empty repository (`templates/repo/empty.tmpl`), asc repositories only

- No link to the git-scm book (`repo.clone_helper` hidden); broken-repository text is `repo.broken_message_asc`.
- Clone panel: see below. The two quick-guide blocks use asc verbs only (checked against
  `crates/asc/src/cli`: `init -b`, `add`, `commit -m`, `remote add`, `push -u`):

```
touch README.md
asc init [-b <trunk> when it is not master]
asc add README.md
asc commit -m "first commit"
asc remote add origin <https url>
asc push -u origin <trunk>
```

and `asc remote add origin <url>` + `asc push -u origin <trunk>` for an existing repository. The `--object-format=sha256`
flag is dropped.

## Clone panel and home (`clone_buttons.tmpl`, `clone_script.tmpl`, `clone_asc_tabs.tmpl`, `home.tmpl`)

- `clone_buttons.tmpl`: for an asc repository the read-only box shows `asc clone <url>` (the HTTPS button, and the SSH button only when SSH is
  enabled, still pick the URL), and the copy button copies the whole command. `clone_script.tmpl` gained `data-clone-prefix`: an element
  with that attribute gets the prefix before the URL when the protocol changes. Git repositories and wikis are unchanged.
- `clone_asc_tabs.tmpl` (new): tab "Use with asc" (`asc clone <url>`) and tab "Use with git" (`git clone <url>` with the
  note `repo.clone_with_git_help`). Shown on the empty-repository page and, under a `<details>` "Clone with asc or git"
  (`repo.clone_other_ways`), on the repository home page.
- The URLs are the HTTPS clone links (they end in `.git`; asc accepts and strips that suffix); the SSH link exists only when `[asc] SSH_ENABLED` is true.
- Connecting needs no SSH: the asc tab and the empty-repository page show `asc auth login --host <this hub> --prompt` (the
  `AscHubHost` template function) before the clone or push, with a link to Settings, Applications for the token. The
  git tab says that git over HTTPS takes the user name and the token (`repo.clone_https_only_help`, shown while SSH is off).
- Settings: the SSH keys and principals sections are shown only when the `SSHEnabled` template function is true; otherwise
  the page is "GPG keys" (signing keys stay).

## Left git-flavoured

- Pull request pages, `repo.pulls.*`, branch/tag/commit labels and the compare page: renaming would break
  features and wording shared with git-stored repositories.
- Commit table header (SHA1/SHA256, `templates/repo/commits_list.tmpl`), the SHA256 badge in the repository
  header and lists: asc ids are mapped git ids.
- "Use a template" (`Git Content`, `Git Hooks`): template generation is not an asc path yet.
- Repository settings (default branch, Git hooks, mirrors, branch protection), admin pages, API and Swagger text.
- The ignore file the initializer writes is still `.gitignore` (asc reads it when a directory has no `.ascignore`);
  only the label changed. LFS strings stay: asc repositories answer 404 for LFS.
- `options/locale_next/locale_en-US.json` (the migration picker title "Migrate repository", service descriptions,
  in-progress step names such as "Migrating Git data") is outside this change.
- Import is still stored as git: migrations into asc are not done (`asc-stage4-writer.md`), so a repository created by
  the Git import form, a mirror, or a SHA256 repository is a git-stored repository and shows the git clone panel.
- "Use with git" is shown for every asc repository. With `[asc] BRIDGE = off` the git routes refuse access, so the
  tab's command would fail; the page does not know the bridge setting.

## Suggested changes ("Apply as vN")

A reviewer's suggestion on lines of an asc thread (`Review.AddComment.suggestion`) is listed on the conversation page of a stacked change (`templates/repo/pulls/asc_suggestions.tmpl`). "Apply as vN" (the poster, an owner or administrator, or a writer when the pull request allows maintainer edits; the pull request open and at its newest version) does, in `services/pull/asc_suggestion.go`: `Review.PrepareSuggestion` as the person, `Changes.AmendChange` with the hub's credential (the next version of the change, the person as committer, `refs/pull/<n>/head` moved only while it still points at the newest version), the move of the pull head in the hub's repository if asc's own post-receive has not made it, the follow-up of a push (stack base, conflict check, stale reviews, push comment), and `Review.MarkSuggestionApplied` as the person. A suggestion that no longer fits, or a pull request that moved first, is refused with the reason and nothing is written.

## Releases made with `asc release create`

The Releases page lists the hub's release rows and, merged in, the releases that exist only in asc (`Releases.List`, read with the hub's credential). They are matched by tag and the hub row wins: its notes, assets and edit links are the ones shown, and the asc record only adds provenance and checksums (a release published on the hub is mirrored to asc, so it is on both sides). A bare tag of the hub is not a release, so an asc release of that tag is listed. An asc-only release is read-only: the notes and the assets as asc keeps them (an asset is a link to its https address; one without an address is left out of the list), no edit or delete link, a draft only for people who can write, and its title links to the tag in the code view. Channel pills and the keyword search count and match them. Pages are the hub's: an asc-only release rides on the page of the hub row just before it in time (a page can hold a few more than its size); when the hub has no row, the asc releases are paged among themselves. The Atom feed of the releases (`/releases.atom`) and the API list (`GET /repos/{owner}/{repo}/releases`) list them too, with the same rules: matched by tag, the hub row wins, no draft in the public feed, drafts for writers in the API, and the API's `draft`, `pre-release` and `q` filters applied to them. In the API an asc-only release has `id` 0, links to the tag in the code view, and lists only the assets that have an https address; `X-Total-Count` counts the hub's rows. The tag feed (`/tags.atom`) is the git tags and is unchanged. Code: `services/ascreleases` (the matching and the pages, shared), `routers/web/repo/asc_release_merge.go`, `routers/web/feed/asc_release.go`, `routers/api/v1/repo/asc_release.go`.

## Changes of the hub's own settings are audited

"Each change is audited" (Admin > Configuration) is true for the settings the hub keeps itself: a change of a runtime setting (who may register, email confirmation, avatars, the open-with apps) and of the maintenance notice is sent to `Audit.Append` of asc after it is made (`services/asctrust/audit_append.go`, `routers/web/admin/config.go`, `asc_maintenance.go`). asc records it in the instance log as `host.setting.updated` or `host.maintenance.updated`, with the administrator as actor, the hub as the relay, the old and new value and the address and browser of the request; the log is read on Admin > Audit like every other event. The hub uses its internal credential (only the host service may append) and asks `audit.append.v1` first; an asc without it, or an append that fails, is logged in the hub's log and never blocks or undoes the change. The asc settings on the same page (agents on or off, the default budget) are audited by asc itself.
