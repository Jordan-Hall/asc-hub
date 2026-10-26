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

- `clone_buttons.tmpl`: for an asc repository the read-only box shows `asc clone <url>` (the HTTPS/SSH buttons still pick
  the URL), and the copy button copies the whole command. `clone_script.tmpl` gained `data-clone-prefix`: an element
  with that attribute gets the prefix before the URL when the protocol changes. Git repositories and wikis are unchanged.
- `clone_asc_tabs.tmpl` (new): tab "Use with asc" (`asc clone <url>`) and tab "Use with git" (`git clone <url>` with the
  note `repo.clone_with_git_help`). Shown on the empty-repository page and, under a `<details>` "Clone with asc or git"
  (`repo.clone_other_ways`), on the repository home page.
- The URLs are the existing HTTPS and SSH clone links (they end in `.git`; asc accepts and strips that suffix).

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
