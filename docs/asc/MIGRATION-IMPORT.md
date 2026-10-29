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

`ImportGitPush` is a host call (internal token). asc applies its **import path rules** (SPEC 2.8, 25.4) to every added path and refuses the import with the reason when a path
breaks them; this is what a failed migration shows. asc moves the refs with `UpdateRefs` in fast-forward mode and
`HOOK_MODE_SKIP_PRE`: the pre-update hooks (gates, push-time secret scanning) are the caller's own pre-receive, which for a
migration is the migration itself. So imported history is **not scanned for secrets and not held to gates**: a source
that holds a key is imported (checked with a repository holding an AWS key, a private key and a token: no
`POLICY_DENIED`). The import is a system action attributed to the importing user. It changes nothing about later pushes:
the next push goes through pre-receive and asc's gates as for any repository (a push of the same files later is
judged by the policy of the repository at that time).

## What else is migrated

- Issues, pull requests, comments, labels, milestones and releases are written to the database as for a git
  repository. Pull request heads (`refs/pull/N/head`) are written to the clone and taken over by asc with
  `SetPullHead`; head branches of fork pull requests are imported with the history import.
- The wiki is a plain git repository next to the bridge (as for every asc repository) and is cloned as for git.
- LFS objects are fetched into the hub's LFS store after the import, reading the pointers from the clone, as for a
  git repository (the code path does not depend on the storage; not run against an LFS server in this check).

## Known limits

- asc needs about 5 times the pack size on disk and about 1.1 GB of memory to take a 160 MB history (5k commits)
  (measured with asc 0.1.0, debug build).
- The size of the repository (`RepoAdmin.size`) can time out right after a very large import; it is logged and filled in
  later.
