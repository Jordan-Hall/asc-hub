# Merge queue, restack and revert of a stack

This page says what the web UI does for a stack of pull requests: merge when ready, merge now, restack and
revert a landed stack. It is for operators and reviewers. A stack is a chain of pull requests, one per `asc`
change, each on top of the one below; see [WHY.md](WHY.md).

All of it applies to repositories with `storage_backend = asc` and `[asc] ENABLED = true`. On any other
repository the buttons do not show and the routes answer that the repository is not stored in asc.

## Where it is

On a stacked pull request the stack panel lists the stack from the bottom. Under the list, a signed-in viewer
sees the actions they may start. Each action opens a confirmation page that runs a dry run first; nothing is
written by following a link.

| Action | Shown when | Confirmation page |
| --- | --- | --- |
| Merge when ready | the viewer may merge every pull request of the stack up to this one and this one is not queued | `/<owner>/<repo>/pulls/<n>/asc/confirm?op=queue` |
| Remove from the queue | this pull request is queued, and the viewer queued it or may merge it | `?op=unqueue` |
| Merge the stack up to here | the viewer may merge this pull request | `?op=merge` |
| Restack | the viewer may update this pull request (see below), or the stack needs a restack | `?op=restack` |
| Revert this pull request, Revert the whole stack | the pull request is merged and the viewer can write to the repository | `?op=revert&scope=this` or `scope=stack` |

The queue of a branch has its own page, `/<owner>/<repo>/asc/queue?branch=<name>` (the default branch when
`branch` is left out). It lists what waits, in order, with the state of each entry and why it waits, and the
last ten that finished. A repository administrator or site administrator can pause and resume the branch there.

Every write is a `POST` behind sign-in and the CSRF check of the router:

| Route | Does |
| --- | --- |
| `POST .../pulls/<n>/asc/enqueue` | queue the stack from the bottom up to `n` |
| `POST .../pulls/<n>/asc/dequeue` | take `n` and the queued pull requests above it out of the queue |
| `POST .../pulls/<n>/asc/merge` | land the stack from the bottom up to `n` now |
| `POST .../pulls/<n>/asc/restack` | restack the stack of `n` onto the target |
| `POST .../pulls/<n>/asc/revert` (`scope`, `squash`) | revert the landed stack and open the pull requests |
| `POST .../asc/queue/pause`, `.../resume` (`branch`) | pause or resume a branch (administrators) |

A visitor who is not signed in is sent to sign-in by the router (the handlers answer 403 if one is ever reached
without a user). A user who cannot read the pull requests gets 404. Whether the user may do the operation is decided again by the
service when the form is posted, and it fails closed: a lookup error refuses.

## Merge when ready

"Merge when ready" on pull request N queues pull requests 1..N of its stack. The queue has one worker per target
branch. The worker takes the oldest entry and lands the largest prefix of its stack in which every pull request is
ready, in one atomic land through asc.

- A pull request is ready when it is open, not a draft, on the same branch, its head has not moved since the
  check, and branch protection passes (approvals, required checks). A pull request that is not ready stays queued.
  It does not block a later one of another stack.
- Permission is checked at every land for the user who queued the top entry. A user who lost the right, or whose
  account was deactivated, has the entry cancelled.
- One land runs at a time on a branch, across processes; the worker holds a lease row. A crash between claiming and
  finishing is repaired by the next run.
- A paused branch lands nothing. "Merge now" is refused on a paused branch too.
- The poller (`InitAscStackQueue`, started by `automerge.Init`) re-runs every branch with waiting entries every
  20 seconds, so a check that turned green moves the queue.

### Revalidation after a content merge

`[asc_stack_queue] REVALIDATE` is `on_content_merge` by default (any value but `off` means this). With it on, every
land carries `UpdateRefsRequest.no_content_merge`. When the target moved so that landing would have to merge file
contents, asc answers `LAND_NEEDS_CONTENT_MERGE` and lands nothing, because what would land is not what was
reviewed. Then:

1. The queue worker restacks the stack through asc (`Stacks.Restack`). Every change gets a new version on top of the
   current target.
2. The entries go back to `queued`, with the reason "restacked: waiting for checks". The next try applies branch
   protection to the new heads, so required checks and approvals must hold for the exact versions that will land.
   An approval survives where the patch is unchanged.
3. If the restack conflicts, is refused, or finds nothing to rewrite, the entries become `blocked` on the heads they
   have. They wait for a new head: the author restacks and pushes.

With `REVALIDATE = off` the flag is left out and asc merges contents as it lands. The queue page says which applies.
"Merge now" never restacks by itself: it explains that a content merge is needed and offers the restack button.

## Merge now

"Merge the stack up to here" lands pull requests 1..N at once, under the lease of the branch. The confirmation
page lists each pull request as ready or not, with the reason, and says whether the branch is paused and whether a
content merge would be needed. The button is disabled unless every pull request is ready and the branch is not
paused. The pull requests above N stay open; asc restacks them as part of the land.

## Restack

"Restack" asks asc to rebase the whole stack the pull request belongs to onto the current target
(`asc.v1.Stacks/Restack`), then moves `refs/pull/<n>/head` of each pull request whose change was rewritten. Each move
does what a push does: merge base, conflict check, stale reviews, push comment and notification.

- It first runs a dry run. Every change asc would rewrite must have an open pull request in this repository that the
  user may update: the poster, an owner or admin of the repository, a writer where the pull request allows
  maintainer edits, or anyone who may merge it. One that fails refuses the whole call before anything is written.
- A conflict, a change blocked by a conflict below it, or a frozen change (one in asc's merge queue) refuses the
  call. The confirmation page lists the change and the paths.
- If a head cannot be moved after asc has rewritten the stack, the flash says how many; restore their latest
  version from the Versions menu.

## Revert a landed stack

"Revert" asks asc to revert the landed changes (`asc.v1.Stacks/RevertLanded`). asc finds each landing from its ref
log, reverts from the top down and stores the reverts as new changes. **It never writes the target branch.** The hub
then opens one pull request per revert, stacked in order, and the page goes to the top one. They are reviewed and
merged like any other pull request, so branch protection and the queue apply.

- `scope = stack` (default) reverts from the chosen pull request down to the bottom of the landed stack; `this`
  reverts only that pull request. `squash` makes one revert for the selection.
- Every pull request from the chosen one down must be merged, in one repository and on one branch.
- The user needs write access to the repository; the repository must not be archived. One revert of a stack runs at
  a time.
- asc's answer is verified before any pull request is opened: one revert per landed pull request (or one for a
  squash), no repeats, ids of the right length, no conflicts.
- A conflict with later changes is shown on the confirmation page with its paths, and nothing is stored. Revert the
  later changes first, or revert less.
- If asc stored the reverts but a pull request could not be opened, the flash says how many were opened; the rest
  can be submitted from `asc` (`refs/users/...`).

## Timeline and notifications

What the pull request page and the notification list show of the queue:

- Queuing a pull request adds the "scheduled to auto merge" line to its timeline, written as the user who queued it. Taking it
  out of the queue adds the "canceled auto merging" line, written as the user who did it. When the queue itself cancels an
  entry (the user who queued it lost the right or was deactivated, the pull request left its stack), the line is written as
  the user who queued it.
- When the queue cancels an entry, or blocks it because the land needs a content merge and the restack did not help, the user
  who queued it and the author of the pull request get a notification. The user who removed an entry by hand is not told about
  their own action. The reason is on the queue page; no text from asc or from an error is put in a notification.
- A land raises the same merged event as a merge of one pull request: notifications, webhooks, the commit count, the issues the
  pull request closes ("fixes #n"), branch-chain children, and a new conflict check of the open pull requests on the target.
  A land by the queue raises the automatic merge event, "Merge the stack up to here" the ordinary one.
- A restack and a revert write the push comment and the notification of the pull requests they move or open (see above).
- Review summaries go back to asc after each of these events (`reviewed_version` is the newest version a review was made
  on). A check posted through the commit status API reaches asc the same way as one from Actions.

## Secrets and errors

The hub calls asc with the internal token after Forgejo has decided who may act (`ascclient.WithSystem`). No token
and no message from asc reaches a page, a flash or a log: a failed call is shown as one of the fixed texts, and
only the gRPC status code is logged. Conflict paths are made printable and bounded (200 paths of 1,024 bytes).
Pull request titles and paths are escaped by the template engine.

Every restack and revert writes an `asc audit:` line to the log with the repository, the user and the counts.

## Tests

- `services/ascbridge/stacks_test.go`: the Stacks call sites (restack states, heads, conflicts, frozen, revert,
  bad answers, error mapping that does not leak).
- `services/pull/asc_restack_test.go`, `asc_revert_stack_test.go`, `asc_stack_merge_test.go`: authorization, dry run,
  blockers, head moves, approvals kept for a patch-identical rewrite, answer verification, the
  `no_content_merge` flag and `LAND_NEEDS_CONTENT_MERGE` on the wire.
- `services/automerge/asc_stack_queue_test.go`, `asc_queue_view_test.go`: ordering, pause, lease, revalidation,
  the queue view and the dry run.
- `services/automerge/asc_queue_events_test.go`: timeline lines and notifications of the queue.
- `services/ascsync/review_version_test.go`: `reviewed_version` of the review summary.
- `routers/web/repo/asc_queue_test.go`: every handler, the error mapping and the action buttons.
- `tests/integration/asc_queue_test.go`: the routes through the full router.
