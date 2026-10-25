# Stage 4: the asc writer and the git bridge (K12, K13)

Settings (all under `[asc]`, all need `ENABLED = true`):

- `WRITER = git|asc` (default `git`). `asc` sends every write to a `storage_backend=asc` repository through
  `asc.v1` (`modules/repostore/asc`). With `git` the Stage 2/3 flow of K09 stays as it is.
- `BRIDGE = eager|lazy|off` (default `eager`). `lazy` builds the git directory on the first legacy git request with
  `asc export --git` and rebuilds it when the refs in asc moved (`services/ascbridge`). `off` refuses git access.
- `NEW_REPOSITORIES = git|asc`, `ASC_BINARY`, `BRIDGE_WORKSPACES`.

Writer rules: git ids are mapped with `GitMap` at the boundary; a change is built without a ref and published with one
`UpdateRefs` call (fast-forward, or force when asked); `refs/pull/*` use `HOOK_MODE_SKIP_PRE`; `BeforePublish` runs
with no git directory; a signing decision cannot be honoured; file content of one commit is limited to 15 MiB.

No LFS for asc repositories: the LFS routes answer 404, edits and uploads store files as they are.
Not done yet: wiki pull mirrors, migrations into asc, the web LFS settings pages, K15 and K16.
