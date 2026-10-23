# Stage 2 hook glue and `refs/pull/*` writers (K09)

For a repository with `storage_backend=asc` (and `[asc] ENABLED`), asc is the source of truth and the bare git
directory is the git bridge. `services/ascsync` is the Forgejo side of that. Everything is behind the flag and
fails closed: a `asc` repository is never written through git because asc is off or unreachable.

## Pushes

- **pre-receive**, after every policy check, for a git-origin push (any `PushTrigger` but `asc`; wikis excluded):
  `ImportGitPush` with the repository directory, the quarantine (`GIT_OBJECT_DIRECTORY`, falling back to
  `GIT_QUARANTINE_PATH`), the alternates and the ref commands. asc imports the commits and moves its refs in
  fast-forward mode. A refusal (asc's own text, control characters removed, at most 512 bytes) rejects the push with
  403. Any other failure (asc down, a timeout, a bad internal token) rejects with 503 and a fixed text. Nothing from
  asc's error reaches the pusher except a refusal.
- `refs/for/*` commands are never sent: asc stores no such ref.
- A negative user id (the Actions user) is sent as `0`.
- **post-receive** for such a repository does nothing for a git-origin call and returns OK. asc's outbox calls
  post-receive with `PushTrigger=asc` for every ref change it holds, and that call is processed as always. The one
  exception is `pr-merge-to-base`: it still marks the pull request merged, because only that call carries the
  pull request id.

## `refs/pull/*`

`PushToBaseRepo` and `UpdateRef` (PR creation, PR head sync, AGit proc-receive) call `ascsync.SetPullHead`:

1. A commit asc has not seen is imported from the repository's `objects` directory (git has migrated an AGit push
   there before it runs proc-receive; `PushToBaseRepo` fetches into it) with `ImportGitPush`. If asc refuses that as
   a non-fast-forward (a rewritten head) the import is already done, and the ref is forced.
2. A known commit is set with `UpdateRefs` (`UPDATE_MODE_FORCE`, `HOOK_MODE_SKIP_PRE`, internal token).
3. The bridge is not written. asc's `bridge_promote` moves it, and `SetPullHead` waits up to 30 s for that (Forgejo still
   reads pull heads from the bridge), then fails with `ErrBridgeBehind`.

The clean-up of a pull request that failed to be created deletes the ref in asc the same way.

AGit proc-receive reads the pushed commit and checks for a force push in the bridge, because asc does not have the
commit until the pull request head is set.

## Limits (named in `services/ascsync/ascsync.go`)

`ImportTimeout` 55 s (under the 60 s the hook waits for pre-receive), `CallTimeout` 30 s, `BridgeWait` 30 s,
`MaxRefUpdates` 1024, `MaxAlternates` 16, `MaxDirPathBytes` 4096, `MaxRejectBytes` 512. The directories sent to asc
must resolve inside `<repo>/objects`.
