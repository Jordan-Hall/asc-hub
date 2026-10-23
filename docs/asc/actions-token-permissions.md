# Actions job token `permissions:` (K20)

Forgejo v16 ignores the `permissions:` of a workflow. ASC honours it for the automatic job token
(`secrets.GITHUB_TOKEN`), and it fails closed.

## Effective permissions

The permissions of a job token are the intersection of the `permissions:` of the job (or, if the job has none, of
the workflow) and a ceiling.

- The ceiling is what the token can do today in its own repository (write on every unit), and a fork pull request is
  read-only. The one addition is `packages: write`, which gives write access to the package and container registry of
  the owner of the repository (never another owner), and only when the repository's Actions setting allows it.
- `permissions:` can only narrow the ceiling. `write-all` is the ceiling. `read-all` is read. `{}` (or an empty value)
  is nothing. A scope the block does not name is none.
- Without a `permissions:` block nothing changes, and the token gets no `packages: write`.

| Scope                                  | What Forgejo enforces                                            |
| -------------------------------------- | ---------------------------------------------------------------- |
| `contents`                             | code (git over HTTP, LFS, API), releases and wiki                |
| `issues`                               | issues                                                           |
| `pull-requests`                        | pull requests                                                    |
| `actions`                              | the Actions unit                                                 |
| `repository-projects`                  | projects                                                         |
| `packages`                             | write access to the owner's packages (container, generic, ...)   |
| other GitHub scopes (`checks`, `id-token`, `statuses`, ...) | ignored: they never grant, and the run shows a warning |

A value other than `none`, `read` or `write`, a scope GitHub does not know, or a repeated scope fails the run with a
message (`permissions: unknown ...`) before any job starts.

## The setting

`AllowPackagesWrite` in the config of the repository's Actions unit (JSON, default `false`). It is read again on every
package request, so turning it off ends the grant of a job that is running. There is no web form for it yet.

## What a token with `contents: none` can still do

`actions/checkout` fetches the repository with the job token. Reading the code of the job's own repository over git or
LFS, and through `asc`, therefore always works, whatever the workflow says. The API (contents, branches, ...), writing,
and every other repository are refused.

## Where it is stored and enforced

- `action_task.permissions` holds the effective permissions as JSON when the task is created
  (`models/actions/token_permissions.go`, `CreateTaskForRunner`). Empty means no block.
- `access_model.GetActionRepoPermission` narrows the unit modes of the task: API (`RepoAccess`), git over HTTP, LFS
  and `asc`. The git push check passes the `contents` mode to the pre-receive hook.
- `packages_service.DeterminePackageAccessModeForTask` gives the package grant. A container registry token made from a
  job token carries the task (`TaskID` claim) and stops working when the task is no longer running.
