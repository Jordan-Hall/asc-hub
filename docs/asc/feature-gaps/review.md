# Feature gaps: review, agent, workflow and trust boards

Audit of group `review` against the design boards in `project-v3` (Review, ReviewVersions, NewChange, Checks, ChangeChecks, Agents, AgentRun, AgentNew, AgentAccess, Workflows, WorkflowDef, WorkflowRun, Policy, Ops, Queue, Audit, Security and the Mobile twins). Hub: `D:/dev/asc-hub` branch `asc/v16`. asc server: `D:/dev/asc` (protos in `proto/asc/v1`). Status comes from reading templates, routers, services, `web_src/js` and the protos; the dev hub was not run.

Status words: **done** (present and wired), **ui-only-missing** (data and routes exist, markup or script missing), **needs-hub-backend** (hub handler or service work; the asc RPC exists), **needs-asc-backend** (asc proto or server work; the hub only displays), **needs-both**. Size: S under half a day, M one to three days, L more than three days. `-` means nothing to build. Template paths are under `templates/repo/` unless they start elsewhere.

## Counts

| Status | Rows |
|---|---|
| done | 72 |
| ui-only-missing | 69 |
| needs-hub-backend | 28 |
| needs-asc-backend | 4 |
| needs-both | 41 |
| total | 214 |

| Board | done | ui-only-missing | needs-hub-backend | needs-asc-backend | needs-both |
|---|---|---|---|---|---|
| Review | 7 | 8 | 7 | 0 | 8 |
| ReviewVersions | 5 | 1 | 4 | 0 | 3 |
| NewChange | 1 | 5 | 2 | 0 | 5 |
| Checks | 5 | 5 | 2 | 0 | 1 |
| ChangeChecks | 4 | 3 | 2 | 0 | 1 |
| Agents | 3 | 7 | 0 | 1 | 3 |
| AgentRun | 5 | 4 | 1 | 1 | 7 |
| AgentNew | 2 | 1 | 1 | 1 | 3 |
| AgentAccess | 4 | 1 | 0 | 0 | 3 |
| Workflows | 2 | 5 | 3 | 0 | 1 |
| WorkflowDef | 5 | 2 | 2 | 0 | 0 |
| WorkflowRun | 5 | 3 | 1 | 0 | 0 |
| Policy | 5 | 1 | 0 | 1 | 3 |
| Ops | 5 | 5 | 2 | 0 | 1 |
| Queue | 6 | 2 | 1 | 0 | 2 |
| Audit | 2 | 4 | 0 | 0 | 0 |
| Security | 5 | 4 | 0 | 0 | 0 |
| Mobile twins | 1 | 8 | 0 | 0 | 0 |

## Findings that change the plan

- The Review page is the Forgejo pull request view with an asc stack rail, versions panel, badges and a links-only side panel (`pulls/asc_panel.tmpl`). The board's intent block, risk, policy-gate card, review checklist, activity feed, suggestion flow and shortcuts do not exist.
- The hub stores code comments in its own database. It never calls the asc `Review` service (ListThreads, AddComment, ResolveThread), so threads are invisible to `asc` clients. The board's suggested change has no support in `review.proto`.
- The agent-run boards assume plan approval, permission requests (Allow once / Deny), pause, checkpoints with roll back, run-level limit edits, a task text and step counts. `agents.proto` has none of these: it has run states, `SendInput`, `CancelRun` and a token-level `SetBudget`. These are the largest cross-repo gaps.
- Spend views (Agents stat, Access spend, monthly token budgets) need aggregates that no RPC returns; budgets in `AgentBudgetLimits` are per run or lifetime, not monthly.
- Queue has no reorder control on the board and none in the proto; order is queue order. Live queue updates need the `MergeQueue.Watch` stream or hub polling.
- Several screens are separate pages where the board shows an in-place panel or dialog (undo preview, merge confirm, token revoke).

## Table

### Review

| Board | Feature | Status | Where to implement | Size |
|---|---|---|---|---|
| Review | Stack rail: 4 changes, current marker, per-change state, trunk line, merge-order note, `asc stack sync` command | done | templates/repo/pulls/asc_stack.tmpl, routers/web/repo/pull_asc_stack.go | - |
| Review | Stack rail rows: author avatar with agent mark, +add/-del size, text like 'Approved, restacked' | needs-hub-backend | templates/repo/pulls/asc_stack.tmpl; pull_asc_stack.go (add author, diff stat, review state to entries) | S |
| Review | Stack rail click keeps scroll position per change | ui-only-missing | web_src/js/features (new asc-review.js) | S |
| Review | Rail action 'Merge 1 and 2 now' (merge bottom of stack, with dry run) | done | templates/repo/pulls/asc_stack.tmpl, asc_confirm.tmpl (op=merge) | - |
| Review | Change header: agent banner (waiting on you, 42 tests pass), change id + short hash, Signed badge, 'opened for X based on change 2' | needs-hub-backend | templates/repo/issue/view_title.tmpl; pulls/asc_signature_badge.tmpl (badge exists on versions only); agent run link needs reverse lookup run to change | M |
| Review | Tab strip: Overview, Files, Versions v1>v4, Checks, How it was made | ui-only-missing | templates/repo/pulls/tab_menu.tmpl (has Conversation/Commits/Files; Versions and Checks are a panel and a link) | M |
| Review | Intent, 'Where to look' pointers (file:line), Size, Files, Risk summary block | needs-both | hub: issue/view_content/pull.tmpl; asc: change metadata for pointers and a risk classifier (proto/asc/v1/changes.proto, repo.proto Classify) | L |
| Review | Files by meaning: per-file Behavior/Tests/Generated badge | done | templates/repo/diff/asc_class_badge.tmpl, diff/box.tmpl:142 | - |
| Review | Files by meaning: group counts and filter above the diff | ui-only-missing | templates/repo/diff/box.tmpl (data exists in AscFileClassBadges) | S |
| Review | 'Changed since you looked' count, 'Since my last review v3>v4' selector, 'Changed in v4' badge per file, hide unchanged hunks | ui-only-missing | templates/repo/pulls/asc_versions.tmpl (link asc-since-last-review exists; per-file badge, count, hunk hiding missing) | M |
| Review | Unified/Split layout, collapse file, Viewed checkbox with progress | done | templates/repo/diff/box.tmpl (Forgejo native) | - |
| Review | Hunk header shows enclosing function (impl PaymentsClient > async fn) | needs-hub-backend | services/gitdiff hunk header | S |
| Review | Inline comment threads: open/resolved, replies, Resolve, markdown composer | done | routers/web/repo/pull_review.go via /files/reviews/comments (Forgejo native, stored in the hub DB) | - |
| Review | Threads visible to asc clients: hub comments are not written to the asc Review service (ListThreads/AddComment/ResolveThread unused by the hub) | needs-both | proto/asc/v1/review.proto; services/ascsync (new review-thread sync); pull_review.go | L |
| Review | Agent reply in a thread labelled 'Agent' with avatar | ui-only-missing | templates/repo/issue/view_content/comments.tmpl show_role | S |
| Review | Suggested change block in a comment, 'Apply as v5' (creates a version, undoable op, resets approvals), 'Ask coder-2 to apply', '/' suggestions | needs-both | asc: no suggestion field in review.proto and no amend RPC; hub: composer + apply handler in pull_review.go | L |
| Review | Keyboard review shortcuts j/k file, a approve, r request changes, c comment, v viewed, n next change; shortcut list; can be turned off | ui-only-missing | web_src/js/features/asc-keys.js (list pages only today); new review key map + settings toggle | M |
| Review | 'Your review' checklist (read intent, opened 2 of 3 files, N open comments) with Approve / Request changes / comment in a sticky card | ui-only-missing | templates/repo/diff/new_review.tmpl, pulls/asc_panel.tmpl | M |
| Review | Activity timeline: version pushed, comment, approval carried, restacked (14 events, Show all) | needs-hub-backend | routers/web/repo/issue_view timeline + asc ListRefLog (proto/asc/v1/stacks.proto) merged in | M |
| Review | Checks on v4 summary card (unit, lint, integration running, security scan stale) with 'Open checks on v4' | needs-hub-backend | templates/repo/pulls/asc_panel.tmpl (links only); data from AscPullChecks in asc_checks.go | S |
| Review | Policy gate card: approvals 1 of 2, carried approval note, required checks, signed with run key, secret scan, stack order, rules link | needs-hub-backend | templates/repo/pulls/asc_panel.tmpl (links only); data: Policy.Evaluate LAND (proto/asc/v1/policy.proto) + pull reviews | M |
| Review | Approval carry-over: approval on an unchanged diff survives a new version and says so | needs-both | asc restack keeps approvals; hub must read and show it (services/ascsync/review_state.go, pulls/asc_versions.tmpl) | M |
| Review | Landing card: queue position, estimated start, 'Add changes 1 and 2 to queue', dry-run line | needs-both | templates/repo/pulls/asc_stack.tmpl (queue link only); ETA needs queue model (mergequeue.proto has position only) | M |
| Review | Restack banner + action when trunk moved | done | templates/repo/pulls/asc_stack.tmpl (asc-needs-restack), routers/web/repo/asc_restack.go | - |
| Review | 'Restacked onto main, automatic, 3 min ago' line with Undo | needs-hub-backend | routers/web/repo/asc_ops.go UndoRef link on the review page | S |
| Review | Reviewers list with status, request, suggested reviewers, agent reviewer (reviewer-1) | done | Forgejo sidebar reviewers (people); agent reviewer via workflow changed-src-review has no link | - |
| Review | 'How it was made': run id, task, tool calls, versions, files read, 'never touched infra/', link to run | needs-both | asc: change to run provenance (AgentRun.changes is run to change only) + run task text; hub: card in view_content/pull.tmpl | M |
| Review | Conflict with main: stays open, other changes unblocked, markers, 'Resolve in editor', 'Show both sides', 'Ask coder-2 to resolve' | needs-both | templates/repo/pulls/asc_badges.tmpl (conflict banner with paths exists); conflict hunk viewer + agent task (Agents.CreateRun) | L |
| Review | Loading skeleton keeping layout; load error with 'Try again' and 'Open previous version'; comments-saved note | ui-only-missing | templates/repo/pulls/files.tmpl, diff/box.tmpl | S |
| Review | Applied-as-v5 toast with Undo (8 s, role=status) | needs-both | depends on suggestion apply; toast infra web_src/js/features/ds-shell.js | S |

### ReviewVersions

| Board | Feature | Status | Where to implement | Size |
|---|---|---|---|---|
| ReviewVersions | Version timeline with Base/Head radios per column, current badge, signature badge, actor and age | done | templates/repo/pulls/asc_versions.tmpl, routers/web/repo/pull_asc_versions.go | - |
| ReviewVersions | Version row detail: commit message, short hash, +add/-del, per-version check chips (unit pass, lint pass, integration running) | needs-hub-backend | templates/repo/pulls/asc_versions.tmpl; join AscChecks per version | M |
| ReviewVersions | 'Approved by Priya' marker on the version she approved | needs-both | asc review state per version + hub reviews; pulls/asc_versions.tmpl | S |
| ReviewVersions | Restack rows shown as 'ASC, automatic ... no diff change' | needs-hub-backend | pull_asc_versions.go (version kind from Changes.Versions) | S |
| ReviewVersions | Restore as new version (never rewrites history) | done | routers/web/repo/pull_asc_restore.go, route /pulls/{n}/asc/restore_version | - |
| ReviewVersions | Compare any two versions (interdiff), split/unified | done | routers/web/repo/pull_asc_versions.go (Changes.InterDiff) | - |
| ReviewVersions | Compare header: '+2 -1 in 1 of 3 files, Priya's approval still applies to the other 2' | needs-both | asc: per-file unchanged set; hub: pulls/asc_versions.tmpl | M |
| ReviewVersions | Restack-available banner 'Trunk moved 3 commits since v2' with Restack | done | templates/repo/pulls/asc_stack.tmpl | - |
| ReviewVersions | Land-the-stack panel: scope radios (Bottom 2 / All 4 / Just 1), dry run re-run on scope change | ui-only-missing | templates/repo/pulls/asc_stack.tmpl + asc_confirm.tmpl (scope exists only for revert); routers/web/repo/asc_queue.go | M |
| ReviewVersions | Land panel summary: merge order, signed-by list, queue position and ETA, undo window 30 min | needs-both | templates/repo/asc_confirm.tmpl; ETA as in Review | M |
| ReviewVersions | Confirm for Merge now names branch, current hash and result hash; default focus Cancel | needs-hub-backend | templates/repo/asc_confirm.tmpl, routers/web/repo/asc_confirm.go | M |
| ReviewVersions | Result toast 'Merged 2 changes... Undo is open for 30 minutes' with Undo; undone toast | needs-hub-backend | routers/web/repo/asc_ops.go (UndoRef exists); toast in ds-shell.js | M |
| ReviewVersions | Merge refused because trunk moved, 'Reload dry run' | done | templates/repo/asc_confirm.tmpl (asc-failure banner) | - |

### NewChange

| Board | Feature | Status | Where to implement | Size |
|---|---|---|---|---|
| NewChange | Open-change form: title, description, reviewers, draft | done | templates/repo/diff/compare.tmpl (Forgejo pull creation) | - |
| NewChange | Compare bar: base/head, +212 -48 in 9 files, no-conflict chip, ahead/behind, stack of N | ui-only-missing | templates/repo/diff/compare.tmpl; asc_compare.tmpl is a separate page | M |
| NewChange | Title counter '68 of 120', 'Start with a verb' hint, inline validation | ui-only-missing | templates/repo/diff/compare.tmpl | S |
| NewChange | Intent editor with What/Why/How to check/Risk template and Write/Preview | ui-only-missing | templates/repo/diff/compare.tmpl (description template) | S |
| NewChange | 'Draft from the diff' (AI writes What/Why/How/Risk from the diff) | needs-both | hub: new route using BYO AI key (services/ascbyo); asc: diff + Classify | L |
| NewChange | 'Where to look' pointers editor | needs-both | same as the Review intent block | M |
| NewChange | Reviewer suggestions from ownership and review history, out-of-office note, agent reviewers labelled | needs-both | asc: code owner / review history query; hub: reviewer panel in compare.tmpl | L |
| NewChange | Options: open as draft, keep as part of stack, land when approved and green, ask agent to babysit checks | needs-both | draft + auto-merge exist natively; babysit needs Agents.CreateRun trigger + hub option | L |
| NewChange | Local checks start on open; 'signed with your key' note | ui-only-missing | templates/repo/diff/compare.tmpl; asc signing (proto/asc/v1/signing.proto) | S |
| NewChange | Draft autosave on blur (1 s debounce), 'Save as draft', Ctrl+Enter opens | needs-hub-backend | routers/web/user (drafts page exists); form autosave handler; key handler | M |
| NewChange | Right rail: stack after this change, file list with Show all, 'What policy will ask for' preview | needs-hub-backend | routers/web/repo/compare.go + Changes.Stack and Policy.Evaluate LAND | M |
| NewChange | 'Review my diff first' self-review shortcut | ui-only-missing | templates/repo/diff/compare.tmpl | S |
| NewChange | Blocked-by-secret-scan banner before open; nothing-to-compare; opened/draft toasts | needs-both | nothing-to-compare exists (repo.pulls.nothing_to_compare); secret block needs secretscan.Scan on head (proto/asc/v1/secretscan.proto) + hub gate | M |

### Checks

| Board | Feature | Status | Where to implement | Size |
|---|---|---|---|---|
| Checks | Window selector, state tabs, job/test/flaky filters persisted in URL | done | templates/repo/asc_checks.tmpl, routers/web/repo/asc_checks.go | - |
| Checks | Headline stats: pass rate vs previous period, failure groups open, flaky above threshold, median duration + slowest job | needs-both | asc: Ci.ListRuns aggregates (proto/asc/v1/ci.proto) or hub aggregation in asc_checks.go; stat cards | M |
| Checks | State tab counts (Open 3, Acknowledged 1, Quarantined 2, Resolved 14) | needs-hub-backend | asc_checks.go (counts from ListFailureGroups per state) | S |
| Checks | Failure groups list with cause, jobs, seen N times in M changes, new-in-version chip | done | templates/repo/asc_checks.tmpl (groups table), asc_check_group.tmpl | - |
| Checks | Inline expand of a group: log excerpt with line numbers, occurrences, note + Acknowledge / Quarantine / Open issue | ui-only-missing | templates/repo/asc_checks.tmpl (actions live on the group page asc_check_group.tmpl) | M |
| Checks | Row hover actions Acknowledge / Quarantine / More | ui-only-missing | templates/repo/asc_checks.tmpl | S |
| Checks | Quarantine hidden unless policy allows [ci] allow_quarantine | ui-only-missing | templates/repo/asc_check_group.tmpl:64 shows the button always; read the flag via services/asctrust/policy.go | S |
| Checks | Acknowledge / quarantine / resolve / reopen with optional note, logged | done | routers/web/repo/asc_checks.go AscCheckGroupState (CiInsights.SetFailureGroupState) | - |
| Checks | Repo-level 'Re-run failed' button | ui-only-missing | templates/repo/asc_checks.tmpl (rerun exists per occurrence and per pull) | S |
| Checks | Flaky tests view: flake rate, runs, flips, 14-run heat strip, last flip, state | done | templates/repo/asc_checks.tmpl (flaky table), strip markup | - |
| Checks | Trends view per job with pass rate | done | templates/repo/asc_checks.tmpl (asc-trend) | - |
| Checks | Side cards: 'Main is green', failing jobs bars, Quarantined list with Manage | needs-hub-backend | templates/repo/asc_checks.tmpl; main status service used by pull checks (asc_checks.go) | M |
| Checks | Empty 'Nothing needs attention', error with Try again, loading skeleton | ui-only-missing | templates/repo/asc_checks.tmpl (problem banner exists; empty wording and skeleton missing) | S |

### ChangeChecks

| Board | Feature | Status | Where to implement | Size |
|---|---|---|---|---|
| ChangeChecks | Headline, pill, version/commit/started/took, stale banner, Re-run failed only / Re-run all | done | templates/repo/pulls/asc_checks.tmpl, routers/web/repo/asc_checks.go AscPullChecks | - |
| ChangeChecks | Cause cards: title, test count, New in v4 / seen before, log excerpt, Show full log, group link | done | templates/repo/pulls/asc_checks.tmpl | - |
| ChangeChecks | 'Open the line' jumps to the diff at file:line | ui-only-missing | templates/repo/pulls/asc_checks.tmpl (parse path:line from excerpt) | S |
| ChangeChecks | 'Copy a repro command' (an asc command, not git) | ui-only-missing | templates/repo/pulls/asc_checks.tmpl + small copy script | S |
| ChangeChecks | 'Ask coder-2 to fix' (start an agent run on this failure) | needs-hub-backend | asc_checks.go calls Agents.CreateRun (proto/asc/v1/agents.proto) with the excerpt as task | M |
| ChangeChecks | Flaky card with 20-run strip, Quarantine this test, Open an issue | done | templates/repo/pulls/asc_checks.tmpl | - |
| ChangeChecks | All checks list with required flag, duration; 'Run security scan on v4' per row | ui-only-missing | templates/repo/pulls/asc_checks.tmpl (list exists; per-job run action missing) | S |
| ChangeChecks | Compared with main: main green, 'these 2 failures are new', slowest test 41 s (was 12 s) | needs-both | main status exists (pull.main_*); slowest-test delta needs per-test timing in CiInsights.ReportTestCases (proto/asc/v1/ciinsights.proto) | M |
| ChangeChecks | Versions history list (v4 2 fail, v3 pass, v2 pass) | needs-hub-backend | templates/repo/pulls/asc_checks.tmpl (pull.history key exists) | S |
| ChangeChecks | Trust text: results belong to the exact version | done | templates/repo/pulls/asc_checks.tmpl | - |

### Agents

| Board | Feature | Status | Where to implement | Size |
|---|---|---|---|---|
| Agents | Stats: Running now, Waiting on you | done | templates/repo/asc_agents.tmpl (counts are of the loaded page only) | - |
| Agents | Stats: Landed this week (31% of changes), Spend this month ($142 of $400 cap) | needs-both | asc: spend and landed aggregates (no RPC in proto/asc/v1/agents.proto); hub: asc_agents.go | M |
| Agents | State chips with counts, agent filter, text search over runs | ui-only-missing | templates/repo/asc_agents.tmpl (chips exist without counts; no search; ListRuns has no text filter) | M |
| Agents | Comfortable/Compact density, persisted per person | done | templates/repo/asc_agents.tmpl + user preference | S |
| Agents | Run row: title, 'for stack', run id, agent, tokens used/limit with bar, duration, starter avatar | ui-only-missing | templates/repo/asc_agents.tmpl (tokens and cost text only, no bar or duration) | S |
| Agents | Run row progress '3 of 4 steps' with bar | needs-asc-backend | asc: AgentRun has no plan or step counts (proto/asc/v1/agents.proto) | M |
| Agents | Row actions Open run and Stop (confirm + reason logged) on hover/focus | ui-only-missing | templates/repo/asc_agents.tmpl; POST runs/{id}/cancel exists | S |
| Agents | 'Needs you' panel: permission request (Allow once / Deny) and plan approval (Review plan), applied in place | needs-both | asc: permission-request and plan events + decision RPC missing (agents.proto has WAITING_INPUT only); hub: asc_agents.tmpl panel + routes (today a link list of waiting runs) | L |
| Agents | 'Agents on this repository' card (scope, status, changes landed in 30 days, Manage access) | needs-both | asc: per-agent landed count; hub: ListTokens + ListRuns aggregation in asc_agents.go | M |
| Agents | Numbered pagination and 'Showing 5 of 24' | ui-only-missing | templates/repo/asc_agents.tmpl (only an 'Older runs' cursor link) | S |
| Agents | Sub-navigation Runs / Workflows / Access / Spend | ui-only-missing | templates/repo/asc_agents_nav.tmpl (has Runs, Workflows, New task, Access; no Spend page) | S |
| Agents | Agent features off state with 'Turn on agent features' | done | templates/repo/asc_agents_notice.tmpl, POST /-/asc/agents/features | - |
| Agents | Empty state; loading skeleton | ui-only-missing | templates/repo/asc_agents.tmpl (empty done; skeleton missing) | S |
| Agents | Header pill 'N agents running' on every page | done | templates/repo/asc_agents_pill.tmpl | - |

### AgentRun

| Board | Feature | Status | Where to implement | Size |
|---|---|---|---|---|
| AgentRun | Runs side list / switcher (runs 214, 213, 212 ...) with New task | ui-only-missing | templates/repo/asc_agents_run.tmpl | S |
| AgentRun | Task quote ('Task by Maya Okafor: ...') on the run page | needs-asc-backend | asc: AgentRun lacks the task text, only the title (proto/asc/v1/agents.proto message AgentRun); hub shows the title only | S |
| AgentRun | Header: state chip, model, started-by, age, target, workflow link | done | templates/repo/asc_agents_run.tmpl | - |
| AgentRun | Pause (holds the run at the next tool call) | needs-both | asc: no PauseRun RPC or state; hub: route + button | M |
| AgentRun | Stop with confirmation and reason; keeps work as drafts | ui-only-missing | templates/repo/asc_agents_run.tmpl (Stop posts directly; CancelRun accepts a reason) | S |
| AgentRun | Plan block with step states and 'You approved at 10:02' | needs-both | asc: plan events/state missing; hub: plan card | L |
| AgentRun | Permission request banner: Allow once / Deny and continue / Answer in own words; records who and why | needs-both | asc: structured permission request + decision RPC; hub: asc_agents_run.tmpl (free-text SendInput answer exists) | L |
| AgentRun | Live activity log: poll, follow unless scrolled, 'Show everything' toggle, error handling | done | web_src/js/features/asc-agent-run.ts, asc_agents_events.tmpl, routers/web/repo/asc_agents_feed.go | - |
| AgentRun | 'Reconnecting: showing the log as of 10:14' banner | ui-only-missing | asc-agent-run.ts (maxFailures logic exists, no banner) | S |
| AgentRun | Steer this run / answer the agent | done | templates/repo/asc_agents_run.tmpl, POST runs/{id}/input | - |
| AgentRun | Result: stack of changes with state and Review links; 'agent cannot merge' note | done | templates/repo/asc_agents_run.tmpl, services/ascagentui/links.go | - |
| AgentRun | Budget meters: tokens, cost, time, changes with numbers | done | templates/repo/asc_agents_meters.tmpl, services/ascagentui/budget.go | - |
| AgentRun | Budget 'Edit limits' on a live run | needs-both | asc: only token-level SetBudget; need a run-level limit RPC; hub: dialog + route | M |
| AgentRun | Access for this run: can/cannot list, token id and expiry, signed by run key | needs-hub-backend | templates/repo/asc_agents_policy.tmpl (repo policy card only); use AgentRun.token_id + AgentTokens.GetToken | S |
| AgentRun | Checkpoints list with Roll back (logged) | needs-both | asc: checkpoint + rollback RPC missing; hub: card + route | L |
| AgentRun | Audit note and 'Download the audit log' (90 days, linked from each change) | needs-both | asc: per-run audit export (Audit.Export filtered by run); hub: link + route | M |
| AgentRun | Failed state 'hit its model token limit' with 'Raise the limit and continue' and Review the 3 changes | needs-both | failure text done (services/ascagentui/failure.go); continue needs an asc resume RPC | M |
| AgentRun | Done summary (38 tool calls, 11 min, $2.31) and Stopped notes | ui-only-missing | templates/repo/asc_agents_run.tmpl (data in AgentBudgetUsage) | S |

### AgentNew

| Board | Feature | Status | Where to implement | Size |
|---|---|---|---|---|
| AgentNew | Task, target branch, model/key, limits (changes, tokens, cost, minutes), narrow write scope, start, validation | done | templates/repo/asc_agents_new.tmpl, routers/web/repo/asc_agents.go AscAgentsCreate | - |
| AgentNew | Agent picker showing each agent's scope (coder-2 src/**, tests/**) | ui-only-missing | templates/repo/asc_agents_new.tmpl (free-text agent name) | S |
| AgentNew | 'Ask me to approve the plan first' switch | needs-both | asc: CreateAgentRunRequest has no plan_first; plan flow missing | L |
| AgentNew | Right rail 'What this run may do', computed from token and policy, updates as scope narrows | needs-hub-backend | Policy.Evaluate RUN/WRITE (proto/asc/v1/policy.proto); asc_agents_new.tmpl + JS fetch | M |
| AgentNew | Estimate 'About 10 min and $1.60' from similar runs | needs-asc-backend | asc: run statistics query (new RPC) + hub display | M |
| AgentNew | Recent tasks with Reuse | needs-both | asc: AgentRun lacks task text; hub: list on asc_agents_new.tmpl | S |
| AgentNew | Limits above the policy cap are lowered with a message, not rejected | needs-both | asc budget handling vs hub message in asc_agents.go (no 'lowered' text exists) | S |
| AgentNew | Started toast 'Run 216 started' with Open; can-start right required | done | redirect to run page + writer check (asc_agents_common.go) | - |

### AgentAccess

| Board | Feature | Status | Where to implement | Size |
|---|---|---|---|---|
| AgentAccess | Tokens: name, hint, scope paths, per-run limits, status, expiry, last used, meters; Make token; Budget; Revoke | done | templates/repo/asc_agents_tokens.tmpl, routers/web/repo/asc_agents_tokens.go | - |
| AgentAccess | Secret shown once with copy and ASC_TOKEN export line | done | templates/repo/asc_agents_tokens.tmpl (tokens_secret_*) | - |
| AgentAccess | Spend this month per token ($58 of $120) as a monthly budget | needs-both | asc: token budgets are per run or lifetime, no monthly window or month spend (AgentBudgetLimits/Usage); hub: column | M |
| AgentAccess | Approvals log (permission and plan requests with decision and decider) | needs-both | depends on the permission/plan feature (see AgentRun); asc audit events + hub list | L |
| AgentAccess | Policy defaults card: write scope, never trunk, changes per run, cost cap, token lifetime, who may decide, link to Policy | ui-only-missing | templates/repo/asc_agents_policy.tmpl (can/cannot and limits shown; cost cap, lifetime, deciders missing) | M |
| AgentAccess | Spend this month panel: cap, daily spend chart, per-agent totals, 'people using AI keys' | needs-both | asc: spend history aggregates; hub AI-key usage (services/ascbyo); new Spend page | L |
| AgentAccess | Revoke confirm dialog naming the agent; Budget dialog | done | templates/repo/asc_agents_tokens.tmpl (disclosure forms with reason, not modal dialogs) | - |
| AgentAccess | Empty state 'No tokens yet' | done | templates/repo/asc_agents_tokens.tmpl | - |

### Workflows

| Board | Feature | Status | Where to implement | Size |
|---|---|---|---|---|
| Workflows | Definitions list with triggers, description, path, valid/not valid reason | done | templates/repo/asc_workflows.tmpl, routers/web/repo/asc_workflows.go | - |
| Workflows | On/off switch (role=switch) and Run now on each list row | ui-only-missing | templates/repo/asc_workflows.tmpl (pill only; POST /{name}/enable and /trigger exist) | S |
| Workflows | Step chain 'scan > update > review' on the card | ui-only-missing | templates/repo/asc_workflows.tmpl (shows a step count) | S |
| Workflows | Last-12-runs history strip and 'last run' time per workflow | needs-hub-backend | asc_workflows.go (Workflows.ListRuns per workflow) + template strip | M |
| Workflows | State filter (All, On, Off, Not valid), search, counts | ui-only-missing | templates/repo/asc_workflows.tmpl | S |
| Workflows | 'New workflow' and 'Start from a template' (creates a reviewed change with the file) | needs-hub-backend | routers/web/repo/asc_workflows.go + web editor/propose-file flow; templates under options/ | M |
| Workflows | Latest runs list (running, passed, needs approval, failed) with link to all runs | done | templates/repo/asc_workflows_runlist.tmpl, asc_workflows_runs.tmpl | - |
| Workflows | Secrets and variables panel (names per workflow) with link to settings | needs-hub-backend | Workflows.ListSecrets (repo admin only) in asc_workflows.go; template card | S |
| Workflows | Capacity '3 running of 6 allowed' and shared agent budget line | needs-both | asc: repo-wide concurrency cap not exposed; spend aggregate as in Agents | M |
| Workflows | No-workflows state with template and guide buttons | ui-only-missing | templates/repo/asc_workflows.tmpl (text exists, buttons missing) | S |
| Workflows | Loading skeleton | ui-only-missing | templates/repo/asc_workflows.tmpl | S |

### WorkflowDef

| Board | Feature | Status | Where to implement | Size |
|---|---|---|---|---|
| WorkflowDef | Header: name, on/off switch, path, Run it now | done | templates/repo/asc_workflows_def.tmpl (definition commit hash not shown) | - |
| WorkflowDef | Steps table (kind, after, details, timeout) with state of latest run and connector fill | ui-only-missing | templates/repo/asc_workflows_def.tmpl (ordered list) | M |
| WorkflowDef | Starts-when: cron, by hand incl. CLI command | done | templates/repo/asc_workflows_def.tmpl | - |
| WorkflowDef | Run-it-now form with typed inputs and attach-to-change; inline validation | done | templates/repo/asc_workflows_def.tmpl, asc_workflows.go AscWorkflowTrigger | - |
| WorkflowDef | Latest runs of this workflow | done | templates/repo/asc_workflows_def.tmpl | - |
| WorkflowDef | Inputs and budget card (tokens, cost cap, write scope, signing key) | done | templates/repo/asc_workflows_def.tmpl (wf_inputs, run_budget) | - |
| WorkflowDef | Secrets used card (REGISTRY_TOKEN set) | needs-hub-backend | Workflows.ListSecrets in asc_workflows.go | S |
| WorkflowDef | File view with Copy button | ui-only-missing | templates/repo/asc_workflows_def.tmpl (details/pre without copy) | S |
| WorkflowDef | 'Edit as a change' opens the file in the editor as a reviewed change | needs-hub-backend | asc_workflows_def.tmpl + web editor route to a new branch | M |

### WorkflowRun

| Board | Feature | Status | Where to implement | Size |
|---|---|---|---|---|
| WorkflowRun | Run header: state, started by, time, about change; Cancel run, Run again with same inputs | done | templates/repo/asc_workflows_run.tmpl, asc_workflows.go (cancel, rerun) | - |
| WorkflowRun | Cancel confirmation dialog | ui-only-missing | templates/repo/asc_workflows_run.tmpl | S |
| WorkflowRun | Approval banner: comment, Approve / Decline, signed log entry, 'an agent can never decide' | done | templates/repo/asc_workflows_run.tmpl, POST decide (Workflows.DecideApproval) | - |
| WorkflowRun | Flow diagram with connectors filling as steps finish | ui-only-missing | templates/repo/asc_workflows_run.tmpl | S |
| WorkflowRun | Steps list with output line, duration, state | done | templates/repo/asc_workflows_run.tmpl | - |
| WorkflowRun | Live polling of step state | done | web_src/js/features/asc-agent-run.ts (workflow poll), GET runs/{id}/state | - |
| WorkflowRun | 'Proposed by the agent': changes with size, state and Review links | needs-hub-backend | asc_workflows.go: resolve the step's agent run to changes via services/ascagentui/links.go | M |
| WorkflowRun | Step log (agent events) and 'Open the agent run' link | ui-only-missing | templates/repo/asc_workflows_run.tmpl (log details exist; link missing) | S |
| WorkflowRun | 'Used so far' meters (tokens, cost, time, agent steps) | done | templates/repo/asc_workflows_run.tmpl | - |

### Policy

| Board | Feature | Status | Where to implement | Size |
|---|---|---|---|---|
| Policy | Header: policy valid pill, 'Edit the rules' link to Settings, 7/30 day window | done | templates/repo/asc_gate.tmpl, routers/web/repo/asc_gate.go | - |
| Policy | Stat cards: Changes checked, Passed first time, Held by the gate (cleared/open), Median time to clear | needs-both | asc: gate decision history (no RPC); hub today shows held/rules/protections/quarantine counts (asc_gate.tmpl:50) | L |
| Policy | Held-by-the-gate list: change, reason, rule, who, how cleared, time later, Open change | done | templates/repo/asc_gate.tmpl (from audit refusals) | - |
| Policy | Policy file view, 'What this means', 'Who can write here' table | done | templates/repo/asc_gate.tmpl | - |
| Policy | 'Checked and held, per day' chart with table equivalent | needs-both | asc: per-day gate counts; hub chart in asc_gate.tmpl | M |
| Policy | Rules in force with 'N held of M' per rule | needs-both | asc: held counts per rule from audit/gate events; asc_gate.tmpl rule list (no counts) | M |
| Policy | Maintainer overrides (signed, logged exception, reason) | needs-asc-backend | asc: override mechanism and event (none in proto/asc/v1/policy.proto); hub list | L |
| Policy | Quarantined checks summary with link to the queue | done | templates/repo/asc_gate.tmpl | - |
| Policy | Invalid-file fallback, nothing-held state | done | templates/repo/asc_gate.tmpl | - |
| Policy | Loading skeleton | ui-only-missing | templates/repo/asc_gate.tmpl | S |

### Ops

| Board | Feature | Status | Where to implement | Size |
|---|---|---|---|---|
| Ops | Operation list: kind pill, ref, actor, age, hash pair, Undo | done | templates/repo/asc_ops.tmpl, routers/web/repo/asc_ops.go (Stacks.ListRefLog) | - |
| Ops | Blocked reasons beside a lock icon (not yours, branch changed, too old, protected needs maintainer) | done | templates/repo/asc_ops.tmpl (asc_trust.ops.blocked.*) | - |
| Ops | Branch filter, Only my operations; filters in URL; older operations pager | done | templates/repo/asc_ops.tmpl | - |
| Ops | Kind filter chips (Push, Landed, Restack, Undo, Agent) and '6 of 214' count | ui-only-missing | templates/repo/asc_ops.tmpl (ViaKey exists per row) | S |
| Ops | Restack banner with 'See what changed' and 'Undo restack' | ui-only-missing | templates/repo/asc_ops.tmpl | M |
| Ops | Export of operations | needs-hub-backend | routers/web/repo/asc_ops.go (stream ListRefLog as JSONL/CSV) | M |
| Ops | 'Who changed branches this week' actor bars with 7/30 window | needs-hub-backend | asc_ops.go aggregation of ListRefLog | M |
| Ops | Selecting a row loads the undo preview in a right panel without navigating | ui-only-missing | templates/repo/asc_ops.tmpl, asc_ops_undo.tmpl (separate page today); web_src/js | M |
| Ops | Undo preview (now at / goes back to / who / reason) and undo is itself logged | done | templates/repo/asc_ops_undo.tmpl, AscOpsUndo (Stacks.UndoRef) | - |
| Ops | Preview lines '4 changes go back', 'approvals on 2 unchanged diffs are kept' | needs-both | UndoRef/preview must report affected changes and kept approvals; asc_ops_undo.tmpl | S |
| Ops | Undone toast with Redo | ui-only-missing | templates/repo/asc_ops.tmpl (Redo = undo of the undo row) | S |
| Ops | No-match state with Clear filters; loading skeleton | ui-only-missing | templates/repo/asc_ops.tmpl | S |
| Ops | Refused state with Reload | done | templates/repo/asc_ops.tmpl (problem banner with reload) | - |

### Queue

| Board | Feature | Status | Where to implement | Size |
|---|---|---|---|---|
| Queue | Branch, running/paused chip, revalidate on/off, Pause/Resume confirm dialogs | done | templates/repo/asc_queue.tmpl, routers/web/repo/asc_queue.go | - |
| Queue | Stats: Waiting, Landed today, Median wait (was 9 min last week), Refused after dry run | needs-hub-backend | queue DB aggregates in asc_queue.go; stat cards (today: waiting/recent/quarantine counts) | M |
| Queue | Waiting list: position, title, state chip (Checking/Queued), queued-by, Remove | done | templates/repo/asc_queue.tmpl | - |
| Queue | Row detail: 'unit running on merged trunk, about 3 min' and 'starts about 6 min' ETAs | needs-both | asc: ETA and check progress per entry (mergequeue.proto QueueEntry has state/detail/position only); hub display | M |
| Queue | Merge now per row (hover/focus) with confirm | ui-only-missing | templates/repo/asc_queue.tmpl (POST /merge exists) | S |
| Queue | Queue reordering (not on the board: order is queue order, no reorder RPC in mergequeue.proto) | done | nothing to build | - |
| Queue | Quarantined checks with reason, owner, review date | done | templates/repo/asc_queue.tmpl, services/asctrust queue quarantine | - |
| Queue | Recently finished (merged / removed with who and when) | done | templates/repo/asc_queue.tmpl | - |
| Queue | 'Merge a stack' panel: scope radios, dry run, Add to queue / Merge now | ui-only-missing | templates/repo/asc_queue.tmpl; reuse asc_confirm.tmpl | M |
| Queue | 'How landing stays safe' card; paused and empty states | done | templates/repo/asc_queue.tmpl | - |
| Queue | Live update: list slides when an item lands, Checking pulses | needs-both | asc: MergeQueue.Watch stream exists; hub poll/SSE endpoint + script | M |

### Audit

| Board | Feature | Status | Where to implement | Size |
|---|---|---|---|---|
| Audit | Chain status, Verify, filters (who, what, outcome, target, from/to, order, rows), table, older events, JSONL/CSV export | done | templates/repo/asc_audit_table.tmpl, routers/web/repo/asc_audit.go (Audit.List/Export/VerifyChain) | - |
| Audit | Repository, organization and site scope from one page | done | routes settings/asc/audit, org/{org}/settings/asc/audit, admin/asc/audit | - |
| Audit | Verify shows a determinate progress bar | ui-only-missing | templates/repo/asc_audit_table.tmpl (plain POST; VerifyChain is unary, so progress needs polling or an asc stream) | M |
| Audit | Broken chain: 'Show event 9,204' and 'Export evidence' | ui-only-missing | templates/repo/asc_audit_table.tmpl (text exists) | S |
| Audit | Export-ready card (file name, size, event count, Download) | ui-only-missing | templates/repo/asc_audit_table.tmpl (direct download link only) | S |
| Audit | Retention label 'Kept 365 days'; no-match state with Clear filters | ui-only-missing | templates/repo/asc_audit_table.tmpl | S |

### Security

| Board | Feature | Status | Where to implement | Size |
|---|---|---|---|---|
| Security | Summary cards: Open findings, Pushes refused 30 days, Rules count, Allowlist count | ui-only-missing | templates/repo/settings/asc_security.tmpl | S |
| Security | Scan main now with inline progress | ui-only-missing | templates/repo/settings/asc_security.tmpl (POST then reload; Scan RPC is unary) | M |
| Security | Findings list: file:line, rule, who, age, severity, state tabs | done | templates/repo/settings/asc_security.tmpl, routers/web/repo/setting/asc_security.go (SecretScan.ListFindings) | - |
| Security | Set state Revoked / False positive / Accept risk with Undo toast | ui-only-missing | templates/repo/settings/asc_security.tmpl (state buttons with note exist; no toast) | S |
| Security | 'File starts with...' filter | ui-only-missing | templates/repo/settings/asc_security.tmpl | S |
| Security | Pushes refused list from the audit log | done | templates/repo/settings/asc_security.tmpl | - |
| Security | Allowlist saved as a proposed change | done | templates/repo/settings/asc_security.tmpl, AscSecurityAllowlist (Policy.Propose) | - |
| Security | Rules table with source and 'Show all 142' | done | templates/repo/settings/asc_security.tmpl | - |
| Security | Blocks-pushes mode from policy secret_scan.mode; unavailable and empty states | done | templates/repo/settings/asc_security.tmpl | - |

### Mobile twins

| Board | Feature | Status | Where to implement | Size |
|---|---|---|---|---|
| Mobile twins | Mobile-Review: actions sheet (Approve, Request changes, queue, Undo restack, Copy asc command) | ui-only-missing | new mobile sheet + web_src/js; templates/repo/diff/box.tmpl | M |
| Mobile twins | Mobile-Review: Approve / Request changes pinned above the tab bar | ui-only-missing | templates/repo/pulls/files.tmpl (rv-sticky exists on Agents only) | S |
| Mobile twins | Mobile-Review: long-press a line to comment | ui-only-missing | web_src/js (touch handler on diff lines) | S |
| Mobile twins | Mobile-ReviewVersions: confirm as a sheet with default focus Cancel; toast with Undo | ui-only-missing | templates/repo/asc_confirm.tmpl (page, not sheet) | M |
| Mobile twins | Mobile-Checks: failures as cards, chips scroll sideways, Re-run failed pinned | ui-only-missing | templates/repo/asc_checks.tmpl responsive cards | S |
| Mobile twins | Mobile-Checks: swipe a card left for Acknowledge | ui-only-missing | web_src/js (new gesture) | S |
| Mobile twins | Mobile-AgentRun: Allow once / Deny pinned; pull to refresh | ui-only-missing | templates/repo/asc_agents_run.tmpl + asc-agent-run.ts (follow logic exists) | S |
| Mobile twins | Mobile-Ops: undo preview as a bottom sheet | ui-only-missing | templates/repo/asc_ops_undo.tmpl | M |
| Mobile twins | Other mobile twins (Agents, Workflows, Queue, Policy, Audit, Security, AgentNew, AgentAccess, WorkflowDef, WorkflowRun, NewChange): same data as desktop, stacked cards | done | templates share desktop markup with rv-desk/rv-mob classes; gaps equal the desktop rows | - |
