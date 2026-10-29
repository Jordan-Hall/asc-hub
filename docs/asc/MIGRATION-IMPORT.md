# Migrating a repository into asc

A migration into a repository stored in asc (`storage_backend=asc`) clones the source into the repository's bridge
directory, hands the history to asc, and writes to the hub database only after the hub can read it back from asc.

## Steps (`services/repository/migrate.go`, `migrate_asc.go`)

1. The source is cloned (mirror) into the bridge directory, as for a git repository.
2. For an asc repository the clone is made fit for asc:
   - the `[http]` section that `git clone -c ...` leaves in the config is removed (asc refuses to promote refs into a
     directory whose config holds more than `git init` and `git clone --mirror` write);
   - refs that are not branches or tags (`refs/pull/*` of GitHub, `refs/merge-requests/*` of GitLab) are removed;
     pull request heads are made later, by the pull request migration.
3. The repository is made in asc again when it is not there (a retry deletes it first).
4. `ascsync.ImportHistory` sends the branches and tags to asc with `ImportGitPush`, in batches of at most 1024 refs,
   reading the objects from `<bridge>/objects`. Nothing is held in memory by the hub (a 160 MB repository: hub peak
   about 220 MB). The call is made with `WithBulkImport` (30 minutes instead of 55 seconds) and `WithImporter`, so asc
   and the fan-out attribute the import to the user who migrates.
5. The migration waits (up to 10 minutes) until the hub, reading through the asc backend, resolves the default branch
   to the cloned commit and loads it. Only then are branches, releases and the rest written to the database.
6. A failure at any step fails the task with its reason. The task row and the repository are kept, so the owner can use
   **Retry** (on the migrating page, or on the "Repository unavailable" page). A retry is idempotent: refs that asc
   holds are left alone and the ones it lacks are imported; nothing is forced and nothing is deleted.

The reason of a failed migration is shown redacted (`RedactMigrationMessage`: credentials in URLs, internal paths and
the asc address are removed) and only to repository administrators and the user who started the migration.

## Policy, gates and secret scanning on imported history

asc's gate (`policy::gate`, run by `UpdateRefs` before a ref moves; `SKIP_PRE` does **not** skip it, it only skips the
hub's own pre-receive hook) protects **trunk** (the default branch), `release/*` and the land branches: path rules,
secret scanning (on by default, blocking, no policy file needed), review and signing. A branch that is none of these
is not scanned, whoever pushes it. asc learns the trunk when the repository is made (`InitOp.default_branch`), and
from `RepoAdmin set_default_branch`.

What a migration does with that:

1. The repository is made in asc with a **placeholder** default branch (`asc-import-pending`), so the imported
   branches are ordinary branches and the import is not held to the gate: a history that once held a key is imported,
   not refused (a refusal would fail the migration of every old repository). Path import rules (SPEC 2.8, 25.4) still
   apply and a refusal is shown as the reason of the failed task.
2. After the import the tip of every branch (the default branch first, at most 20) is scanned with
   `SecretScan.Scan`. What it finds is recorded as open findings with the note **found in imported history** on the
   secret scanning page (Settings, ASC, Secrets). Nothing blocks. A scan that fails is logged.
3. Only then `set_default_branch` makes the real default branch the trunk. From that moment every push to it is
   judged by the policy of the repository (a secret in it is refused, with the path, line and rule in the message),
   exactly as in a repository that was never imported. The same call follows every change of the default branch in
   the hub (settings, branch rename), so the protected branch is always the default branch.

The placeholder never survives the migration (`settleAscTrunk`, `settleEmptyAscMigration`): an empty source gets
its real default branch (the source's, else the instance's) as trunk right after the clone; a failed, refused or
cancelled migration sets it before the task ends (the repair does not stop with the cancel); and when the server
starts, `RepairAscTrunks` tells asc the default branch of every ready repository whose trunk asc reports as
something else (an asc that does not report its trunk is repaired with `forgejo doctor check --run
asc-default-branch --fix`). A repository that is still being migrated cannot be pushed to. The first branch pushed
to an empty repository becomes its default branch, and pre-receive tells asc before asc judges that push, so the
first push is gated too.

A migration whose asc cannot set the default branch (an asc without `set_default_branch`) fails, with that reason:
leaving the real default branch unprotected is not an option.

Pushes through the hub (`git push` to the hub URL) go through the same gate: the pre-receive hook imports the push
with `ImportGitPush`, which runs the gate. A push of an AWS key to trunk is refused; the refusal text reaches the
pusher (asc sends it as `Internal` with the status in the text; the hub reads it out, see `unwrapStatusMismatch`).

## What else is migrated

- Issues, pull requests, comments, labels, milestones and releases are written to the database as for a git
  repository. Pull request heads (`refs/pull/N/head`) are written to the clone and taken over by asc with
  `SetPullHead`; head branches of fork pull requests are imported with the history import.
- The wiki is a plain git repository next to the bridge (as for every asc repository) and is cloned as for git.
- LFS objects are fetched into the hub's LFS store after the import, reading the pointers from the clone, as for a
  git repository (the code path does not depend on the storage; not run against an LFS server in this check).

## What was checked against real sources, and what was not

GitHub itself was used for the code, tags, issues, labels and pull requests of public repositories. Releases with
assets, milestones and LFS were checked against a mock of the GitHub API, the Git LFS batch API and release asset
downloads (`tests/asc-e2e/mock_github.py`, run by `import_mock.sh`): two milestones, two labels, two releases with an
asset each and an LFS object of 1850 bytes arrived in an asc repository. Not checked: GitHub's own redirects for
release assets (objects.githubusercontent.com), the rate limits of the real API with a token, Git LFS servers that
need authentication or the `lock` API, GitLab, Gitea and Gogs sources. LFS asked for on a server that does not run
LFS is refused with a message (API 422, web form error); an LFS object the source does not hold fails the migration
with that object named. Nothing is skipped silently.

## The trunk after a first push, and how it is compared

The first branch pushed to an empty repository becomes its default branch. Before asc judges that push, the hub asks asc
which trunk it holds (`ListRefs` reports it as `head`) and tells asc the pushed branch whenever the two differ, whatever
the hub's own value is: the hub's value and asc's drift (a placeholder a migration left, a change that never reached
asc), and a comparison against the hub's own value would fail open. When asc refuses the push, the trunk goes back (to
the hub's default branch when asc held the placeholder). The same comparison drives `RepairAscTrunks` at start and
`forgejo doctor check --run asc-default-branch`; both cover empty repositories. An asc that does not report `head`
(older than the fix that makes ListRefs report it for every repository) cannot be compared: `--fix` then tells asc the
default branch of every asc repository.

## Typed refusals

A refusal of asc's gate arrives as a gRPC status whose details carry an `asc.v1.ErrorDetail` (code and files) as an Any.
`ascclient.RefusalOf` is the one reader (older asc servers sent it in a form a Go client cannot read; the status and text
are then read out of the error text). Pushes, merges, imports, the review and queue pages show the code, asc's sentence
and the files. Not covered: `asc submit` from the browser goes through the RPC proxy, which passes the status on as it is.

## Design and accessibility of the import pages

The form, the progress page and the failure page were checked at 390 and 1440 px, light and dark, with axe (WCAG 2.2 AA):
no violations and no horizontal overflow in any of the 16 captures (`tests/asc-e2e/design_pass.mjs`). The NewRepo board
shows the progress as "N of M history entries" with a bar; git reports no such count, so the page shows an indeterminate
bar (it says the import runs, not how far), "Run in the background" and Cancel. The board has no failure state: the page
shows asc's or git's reason, Retry and Delete.

## Known limits

- asc needs about 5 times the pack size on disk and about 1.1 GB of memory to take a 160 MB history (5k commits)
  (measured with asc 0.1.0, debug build).
- The size of the repository (`RepoAdmin.size`) is asked for in the background when it takes more than five seconds
  (`modules/repository/size_asc.go`); the page shows the size stored before until the answer arrives.
- Only the tips of branches are scanned after an import, not every commit of the history, and only the first 20
  branches.
