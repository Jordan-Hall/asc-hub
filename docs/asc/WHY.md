# Why ASC Hub

We wrote this page to say why ASC Hub exists, what we chose, what we gave up, and what is not built yet. Where a claim is about code, we checked it against the code and name the place. Where something is not built, we say "planned" and do not describe it as if it worked.

## The problem

Most teams work with branches and pull requests on top of git. After years of that we keep running into the same things.

- **A branch is a poor unit of review.** One long branch holds many ideas, so reviewers read a large diff, or the author splits the work into several branches that depend on each other and then fixes them by hand every time the first one changes.
- **History can be rewritten, and people are afraid of it.** To update a review you force-push. A mistake with a force-push, a bad rebase or a reset loses work, and the recovery tools are hard to find and harder to trust.
- **Reviews go stale.** A reviewer approves a branch, the author pushes again, and nobody can say exactly which version was approved or what changed since.
- **Agents make many parallel changes.** A coding agent can produce a dozen small changes in an hour. Branch names, force-pushes and one big pull request per task do not cope well with that, and the person who owns the repository has to be able to see, limit and reverse what an agent did.
- **Mistakes are hard to undo.** Reverting a merged pull request is a manual job, and reverting several that depended on each other is worse.

## What we chose instead

ASC (Agent Source Control) stores a repository as **changesets** and **stacks**: a stack is an ordered list of small changes, each with a stable id that survives rewriting. The tools (`asc`, the IDE plugins) make and rewrite changes, and the server (`asc serve`) keeps every version of each change. ASC Hub is the web side: where people review, discuss, approve, and land.

What that gives us, and what part of it is already in this repository:

| Idea | In ASC Hub today |
| --- | --- |
| Changesets and stacks | A stacked pull request is the review object of one change (`models/issues/pull_asc_stack.go`). The pull request page shows the stack panel and blocks merging a change in the middle of a stack before its parent has merged. |
| Review per version | The page lists every version of a change, shows the diff between any two, and shows "changes since your last review" (`services/pull/asc_versions.go`). |
| Undo | A version of a change can be restored from the web (`services/pull/asc_restore_version.go`). `asc undo` and `asc redo` undo local operations in the client. Reverting a whole landed stack is written (`services/pull/asc_revert_stack.go`) but has no button or route yet: planned. |
| Merge queue | Implemented in `services/automerge/asc_stack_queue.go`, entered from asc clients; no web button yet, and the content-merge revalidation fails closed until the asc backend carries the flag. |
| Policy in the repository | Which files count as docs or lockfiles is read from asc at the merge base of a change, so the change under review cannot alter the rules that classify it. Branch protection can then ask for fewer approvals for such changes (`services/pull/review_asc.go`). A fuller repository policy file is in progress in the ASC repository and is not part of the hub yet: planned. |
| Code search by meaning | Repository pages at `/<owner>/<repo>/asc/search` offer lexical, symbol and semantic search over asc's code index (`routers/web/repo/asc_search.go`). The page shows how much of the chosen scope the server has indexed. |

## Why a fork of Forgejo, not a new product

A source hosting site is mostly not about repositories. Accounts, organizations, teams, permissions, sign-in (including two-factor, OAuth and LDAP), email, issues, labels, webhooks, Actions, packages and an administration area are years of work, and Forgejo has them. We did not want to rebuild them. We replace only the repository layer: where repositories are stored, how they are written, and how a change is reviewed and landed.

The cost is real, and we say so in [FORK.md](FORK.md). We carry a patch stack on top of Forgejo, we merge its releases and security fixes, and we keep some of its internal names so that merges stay possible. The pages that are shared with git-stored repositories still say "pull request". ASC Hub is not Forgejo and not a git forge, and it credits Forgejo in the footer ("Based on Forgejo"), in the README and in the licence files.

## Why git compatibility and not a flag day

Teams do not move all their tools on one day. CI, deploy scripts, package builds and every developer's habits expect `git clone` to work. So an ASC repository can still be cloned and pushed to with git over HTTPS and SSH: the hub keeps a git directory per repository (the bridge) and imports a git push into ASC before accepting it (`services/ascbridge`, `services/ascsync`). The limits are stated: no LFS for ASC repositories, and a commit that must be signed cannot be created by the asc writer. Repositories that already exist stay git-stored until you choose otherwise (`[asc] NEW_REPOSITORIES` only affects new ones), and migrating an existing git repository into ASC storage is not done yet: planned.

## Who it is for

People first. The page layout, the review flow, the inbox (`/-/asc/inbox`) and the sign-in are made for a developer or reviewer, on a laptop or a phone. Agents are optional: an agent that uses `asc` works under the same tokens, permissions and review rules as a person.

## What it will not do

- It will not hide that it is built on Forgejo, and it will not pretend to be a drop-in Forgejo.
- It will not make agents mandatory, and it does not rank people by how much an agent produced.
- It will not undo a push, submit or land that has already reached the server by itself: the client-side `asc undo` says so, and server-side reversal is the restore and revert operations above.
- It does not claim to be faster than git. We have no published benchmark numbers for the hub, and the benchmark notes in the ASC repository say that no release results have been published yet, so this page makes no speed claim.
- It does not (yet) provide LFS for ASC repositories, signed ASC commits, migration of git repositories into ASC, or a dedicated audit page.

## Benefits

Grouped by who gains. For each one we name the mechanism and whether it is available, partial, or planned. "asc" means the client and server in the ASC repository; the rest is this repository.

### Individual developers

| Benefit | Mechanism | Status |
| --- | --- | --- |
| Undo most mistakes | The client keeps an operation log; `asc undo` and `asc redo` apply the inverse of the logged ref changes and save the work tree first, so an undo can itself be redone. Push, submit and land are not undone by it. On the hub, "restore version" moves a stacked pull request back to an earlier version. | available (asc, and the hub restore); revert of a landed stack planned in the UI |
| Fewer lost changes | Every version of a change is kept by asc, so a rewrite does not destroy the earlier version (`services/pull/asc_versions.go`). Unpushed work can be kept on the server as drafts in a repository with the branchless setting, and `/-/asc/drafts` lists them (`routers/web/user/asc_drafts.go`). | available |
| Small, reviewable changes | A stack of changes, one review per change, with a stack panel and overlap and mergeability badges on each (`templates/repo/pulls/asc_stack.tmpl`, `asc_badges.tmpl`). | available |
| Stacks restack when the base moves | `asc sync` fetches, fast-forwards trunk, drops landed changes and restacks. On the hub, merging a stack restacks open children and keeps an approval where the patch is unchanged (`services/pull/asc_stack_merge.go`). | available in asc; the hub restack is partial (marked `TODO(C22)` in the file) |
| Works in your editor | Plugin sources for VS Code (and Cursor, VSCodium) and JetBrains are in the ASC repository, and the hub provides the sign-in client `asc-ide` with PKCE (`routers/web/auth/oauth_asc_ide.go`). | sign-in available; plugin maturity is the ASC repository's to state |
| Review from a phone | The ASC themes and the landing, header, repository, clone and sign-in pages are written mobile first from 320 px, with 44 px touch targets ([THEME.md](THEME.md)). | available |
| Speed | No measured numbers exist for the hub. | no claim |

### Teams

| Benefit | Mechanism | Status |
| --- | --- | --- |
| Approvals that do not go stale | A review is tied to the exact version it was made on; the page shows what changed since your review, and the diff between any two versions. | available |
| One ordered way to land | The merge queue lands the largest ready prefix of a stack, can be paused per branch and re-checks the enqueuing user's permission at every land. | partial (no web button; revalidation fails closed) |
| Rules the change cannot edit | Change classes come from asc's rules at the merge base; branch protection can require fewer approvals for docs-only style changes (`asc_relaxed_classes`, `asc_relaxed_approvals`, `services/pull/review_asc.go`). | available |
| Policy as code in the repository | A full repository policy file. | planned (in progress in the ASC repository) |
| An audit trail | Restoring a version writes an `asc audit:` log line; path policy refusals are reported to the pusher. | partial: no audit page |
| Secrets stay out of drafts | Before a draft is uploaded, the asc client holds a change that adds denied paths (such as `.env*` or `*.pem`) or a known token pattern, until it is released with `asc cloud release`. This guards drafts and does not replace a scanner on your server. | available in asc |
| Signed changes | The asc writer cannot honour a signing request (`modules/repostore/asc/commit.go`). | planned |
| Failure and flaky-test grouping in checks | Not in the hub. | planned |

### People who use AI agents

| Benefit | Mechanism | Status |
| --- | --- | --- |
| Agents get limited credentials | A hub access token carries scopes, and the hub's authz endpoint accepts bearer access tokens, OAuth2 tokens and Actions task tokens (`routers/private/asc.go`). `asc mcp` write tools can be enabled only by the user's or the system's configuration, never by the repository's, and `--read-only` overrides everything. | available |
| Agent budgets (spend or change count) | Not built. | planned |
| An agent's work stays off trunk | In a repository with the branchless setting, a pushed branch that is not one of the repository's shared refs is stored as the pusher's own ref and counted against a per-user limit, and only shared refs advance trunk. Branch protection still applies to shared refs. | available in asc; the setting is asc's |
| Everything is reversible | Every version of a change is kept, `asc undo` covers local operations, and the hub restores versions. | available / partial as above |
| MCP tools | `asc mcp` (stdio) and the `/mcp` endpoint of `asc serve` expose read tools (status, diff, log, stack, versions, op log) and, only when enabled, write tools, with an audit of calls on the server. | available in asc |
| Search by meaning | `asc/search` in semantic mode returns ranked code chunks from asc's index. | available (coverage depends on asc's index) |

### Operators

| Benefit | Mechanism | Status |
| --- | --- | --- |
| One deployment | The hub and `asc serve` run as two services of one compose project and meet on loopback in one network namespace; the hub image itself does not contain `asc serve` ([OPERATIONS.md](OPERATIONS.md)). | available |
| SQLite or Postgres | The hub supports both (`DB_TYPE`); `asc serve` takes its own database setting. SQLite for staging, Postgres for production. | available |
| Coolify-ready | The staging notes describe a Coolify Docker Compose deployment ([STAGING.md](STAGING.md), [OPERATIONS.md](OPERATIONS.md)). | available |
| Git clients keep working | The bridge and the pre-receive import (see above). | available, with the stated limits |
| Bring your own S3 storage | The object store of `asc serve` can be a local path or an S3-compatible bucket, chosen by the operator in the asc configuration. A store chosen per user or per organization is not built. | operator-level available; per user planned |
| Bring your own AI keys | Not built in the hub. | planned |
