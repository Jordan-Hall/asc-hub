# Feature gaps: the repository boards

Audit of the group "repo" against the v3 design boards (`project-v3/*.dc.html`, the annotation block of each board: states, interactions, accessibility, maps to, motion). Method: each board was read (markup and annotations) and every feature, state and behaviour was checked against the hub tree (`templates/repo/**`, `routers/web/**`, `services/**`, `web_src/js/**`) on branch `asc/v16` and against the asc server (`proto/asc/v1/*.proto`, `crates/asc-core`). The dev hub on port 3100 was used to confirm what the repository home, the stacks list and the history page really render. Nothing was changed except this file.

Paths: `H:` is `D:/dev/asc-hub`, `A:` is `D:/dev/asc`. "Twin" means the Mobile-* board of the same name; the twins reuse the desktop template (responsive), so a row is only repeated for the Mobile section when the twin adds behaviour.

## Status words

| Status | Meaning |
| --- | --- |
| done | the feature is in the product and matches the board (small copy differences are not listed) |
| ui-only-missing | the data or RPC already exists in the hub or in asc; only the template, script or handler wiring is missing |
| needs-hub-backend | the hub (models, services, handlers, notifiers) must produce the data; asc needs no change |
| needs-asc-backend | the asc server (proto, crates) must add the RPC, field or policy key |
| needs-both | both sides |

Size: S = under a day, M = one to three days, L = more than three days.

## Key findings (read before the table)

1. The asc policy schema does not contain the keys the Policy board edits. The board's file uses `[landing] approvals / checks / author_may_approve / keep_approvals_on_restack`, `[agents] write / deny / human_approval / max_minutes / max_tokens` and `[queue] enabled / batch / post_merge_checks`. `A:crates/asc-core/src/policy.rs` parses `[trunk] require_approvals / require_checks / allow_force`, `[agents] default_scope / denied_paths / max_runtime_seconds`, `[agents.budgets]`, `[merge] strategy / queue / revalidate` and rejects unknown keys. Either the schema or the board must move; the table assumes the schema grows.
2. There is no statistics RPC in asc and no insights code in the hub. Pulse, Contributors, "Who writes the code", Activity and the review-time numbers are all missing; they need aggregation over landed changes, reviews and agent runs.
3. Search is complete as a query tool but not as the board: no relevance bar or percentage, no "why it matched" text (the `Chunk` message has no reason field), no repository facet, and the code-intelligence panel (`CrossSearch.SymbolPanel`, REST `GET /api/v1/asc/...`) has no web UI. Nothing in `templates/**` links to `/asc/symbols`.
4. The Code board is the Forgejo file viewer in design-system markup. Blame rows show message, sha and age, but there is no hover card, no heat bar, no outline or "used in" panel, no open-stack badges in the tree and no keyboard shortcuts (`web_src/js` only handles `/` and Ctrl+K; `t`, `g c`, `b`, `y`, `l`, `e`, `[`, `j`, `k` are not bound).
5. Settings are still the Forgejo page set under a new shell. The board has one menu (Repository / Automation / Data / Safety), one Access page (people, teams, agents, deploy keys), one Rules page, one Secrets-and-variables page with scopes, and a Policy page with Form / TOML / Changes views. The hub has separate pages for collaborators, deploy keys, branches, tags, Actions secrets and variables.
6. The Stacks list (`/pulls`) is still the Forgejo pull list (filter menus, labels, milestones). The stack rail, status chips, "Your turn" card, Needs-me filter and sort by attention are not built, although `BuildAscStackPanel` already computes the chain for one pull request.

## Repo (home) , board Repo

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Repo | Title row (owner / name, Private chip, Watch, Star, Fork) and tab bar with Stacks and Releases and Issues counts | done | `H:templates/repo/header*.tmpl` | S |
| Repo | Tab counts on Agent runs and Dependencies (3, 2) | done (the counts come from asc (open runs, open findings), cached 30 s, drawn when above 0; the empty hub showed none, so the number itself was not seen) | `H:templates/repo/header_tabs.tmpl` plus a cached count from `Agents.ListRuns` and `DepsService.ListFindings` in the repo header context | S |
| Repo | Ref meta line ("4 open stacks, 12 branches, a41f9c2 landed 3 h ago"), Go to file button, Clone button | done | `H:templates/repo/home_dir.tmpl` | S |
| Repo | Shortcuts: `t` go to file, `/` focus search, `g` then `c` jump to Code | done (t, g c, and the key attributes; / was there) | `H:web_src/js/features/ds-shell.js` (`onGlobalKey` handles only Ctrl+K and `/`); add a key-sequence helper | S |
| Repo | Last-change row: person, intent, change id, age, "1,204 changes" total | done | `H:templates/repo/latest_commit.tmpl`, `view_list.tmpl` (shows commit summary and sha, no change-id or count label) | S |
| Repo | Directory table (name, last change message, age) with htmx last-commit load | done | `H:templates/repo/view_list.tmpl` | S |
| Repo | Stacks in flight: one row per stack with status chip (Needs your review, Queued to merge, Agent writing, Checks failing), people and agents, change count, +/- size, age | done (seen on a live hub (ui10): chip, people, change count, +/- size, age; the stack is grouped by the git parent when asc names none at push time) | `H:routers/web/repo/view_home_cards.go` (`HomePulls` is a flat PR list with an Open/Draft chip), `H:services/pull` (`BuildAscStackPanel` per PR, needs a batched list form), `H:templates/repo/home_cards.tmpl` | L |
| Repo | Progress rail: segments per change ("2 of 4 landed"), segments link to each change, text label and aria-label, "12 of 12 checks" / "1 of 31 failing" | done (seen on a live hub (ui10)) | same as above plus commit-status counts per head from `H:models/git` | M |
| Repo | Segment filter Open / Mine / Agents / All N stacks without page load | done (seen on a live hub (ui10): clicked, counts and the empty note checked) | `H:templates/repo/home_cards.tmpl`, small script in `H:web_src/js/features/repo-home.js`; needs an "agent-written" flag per pull (agent identity of the poster) | M |
| Repo | Row hover and focus reveals "Open"; row click opens the stack review | done | `H:templates/repo/home_cards.tmpl`, `H:web_src/css` (rp-flight) | S |
| Repo | Recently landed on main: title, author, change id, stack name, +/- size, checks chip | done | `H:templates/repo/home_cards.tmpl` (has title, author, sha, time only); sizes from `Repo.Log` stats, checks from `commit_statuses` | M |
| Repo | README card with tabs README / AGENTS.md / License | done (seen on a live hub (ui10): tabs switch the panel, arrow keys move; AGENTS.md as markdown, License as text) | `H:templates/repo/home_dir.tmpl`, `view_file.tmpl` (only one README is shown) | M |
| Repo | About card: description, topics, website, numbers | done | `H:templates/repo/asc_about.tmpl` | S |
| Repo | About card: license line and "1,204 changes on main" wording | done | `H:templates/repo/asc_about.tmpl` (`rp-numbers`) | S |
| Repo | "Who writes the code" card: 46% people / 54% agents, "100% reviewed by a human", median review time | done | Re-judged 2026-11-09: built in commit 4960bebe16 from the existing `Insights.GetStats` (author kind, review latency) and the hub's pull requests; no new RPC. `H:routers/web/repo/asc_insights_home.go`, `asc_about.tmpl` (TestHomeAuthorsOf) | L |
| Repo | Policy card: Valid / invalid chip, rule summary, link to `.asc/policy.toml` | done | `Policy.GetSource` / `Repo.GetPolicy` already called by `H:routers/web/repo/setting/asc_policy.go`; add the call to the home handler and a card in `asc_about.tmpl` | M |
| Repo | Policy card lines that need new policy keys (budget "20 min and 200k tokens", merge queue on) | done (the card adds "Agent runs stop at 20 min and 200k tokens." from `[agents] max_runtime_seconds` and `[agents.budgets] max_tokens, max_cost_micro_usd`, and "Changes land on trunk through the merge queue." when `[merge] queue = true`; each line is left out when the key is not set) | `H:services/asctrust/ops.go` (`LoadPolicySummary`, `RunBudgetWords`), `H:templates/repo/asc_about.tmpl`; `asc_home_policy_test.go`, `ops_test.go` | M |
| Repo | Activity card: landed per week over 12 weeks, Contributors list with agent marks | done | Re-judged 2026-11-09: the weeks come from one git log on the viewed ref (`prepareHomeActivity`, hub only) and the agent marks from the existing `Insights.ListContributors`; `asc_about.tmpl` (TestHomeContributorsOf) | L |
| Repo | Languages bar with legend and aria-label | done | `H:templates/repo/asc_about.tmpl` | S |
| Repo | Latest release card ("9 changes, 3 by agents. Published 6 days ago") | done (seen on a live hub (ui10): name, chip, change count, age) | `H:templates/repo/asc_about.tmpl` shows name and age only; counts need the release-changes data (see Releases) | S |
| Repo | Loading skeleton that keeps row geometry | done | `H:templates/repo/view_list.tmpl`: while the last commit loads, the author and age cells hold `rp-sk-word` blocks of their usual width, so the columns keep their size; `routers/web/repo/asc_files_skeleton_test.go` | S |
| Repo | Error state: "Stacks did not load", request id, Try again | done | `H:routers/web/repo/view_home_cards.go` (swallows errors), `H:templates/status`, request id from the access log context;; Audit 2026-10-30 (built): Gap file says open; the template has the Stacks did not load error with request id and Try again. | S |
| Repo | Empty repository state with `asc init` and `asc push` commands | done | `H:templates/repo/empty.tmpl` | S |
| Repo | Clone popover: asc login and clone, Install asc, Open in IDE, Copy | done | `H:templates/repo/clone_dialog.tmpl` | S |
| Repo | Git compatibility note collapsed and expanded in the clone sheet | done | `H:templates/repo/clone_asc_tabs.tmpl` | S |
| Repo | "Switch version" popover that lists shared refs, stacks and tags with labels | done | `H:routers/web/repo/asc_refs_menu.go` (`GET /{owner}/{repo}/asc/refs`: shared refs, open stacks and tags with their kind; in a branchless repository the shared refs are asc's `RepoConfig.shared_refs`), the asc mode of `H:web_src/js/components/RepoBranchTagSelector.vue` (one searched list, ✓ on the current ref, kind label, the branchless note; falls back to branches and tags when asc does not answer) | M |
| Repo | Pull to refresh and sticky Clone / New stack buttons (Mobile-Repo) | done | sticky buttons `rp-sticky-m` in `home_dir.tmpl`; pull to refresh by `data-asc-pull-refresh` on `.rp-body` and the `ds-ptr` arrow, run by `web_src/js/features/asc-pull-refresh.js` in the installed app | - |
| Repo | Clone card shown before first clone ("Work on it locally", `asc stack new fix-refund-timeout`) | done | `H:templates/repo/home_cards.tmpl` "Work on it locally" with the clone, create and submit commands to copy, shown to a person who may write code here and has sent no change yet; Hide posts `/asc/clone-card/hide`, which hides it in that browser (`asc_clone_card.go`); `asc_clone_card_test.go` | S |

## NewRepo

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| NewRepo | Page shell, owner select, name, description, auto-init options (Forgejo flow in design-system markup) | done | `H:templates/repo/create*.tmpl` | S |
| NewRepo | Name availability check as you type (300 ms), tick and inline "acme/ledger already exists" | done | `H:routers/web/repo/asc_newrepo.go` CheckName (`GET /repo/check_name?uid=&name=`: free, taken as "owner/name already exists", reserved or bad characters), `H:web_src/js/features/repo-new.js` (300 ms, text only, aria-invalid, status line `repo_name_status` in `create_basic.tmpl`); the form still decides on submit; tests: TestCheckNameAnswersFreeTakenAndReserved, vitest applyNameCheck; checked on a live hub | - |
| NewRepo | "Start from" segment: Empty / Template / Import that keeps typed values | done | `H:templates/repo/create_start.tmpl` (Empty / Template / Import, `aria-pressed`) and `create_import.tmpl` (one link per importer, hidden when migrations are off); `repo-new.js` keeps the owner, name, description and visibility in the import links and `migrate.go` fills them in; `asc_newrepo_start_test.go`, `repo-new.test.js` | M |
| NewRepo | Visibility as three radio cards: Private, Internal, Public | done (seen on a live hub (ui36): the create form has the three cards; Internal needs the migration v17z) | `H:models/repo/repo.go` has `IsPrivate` only; asc already returns `visibility` "private / internal / public" in `RepoConfig`; add an internal level (signed-in users read) in models, permission checks and the form | L |
| NewRepo | Working mode radio: Branchless (recommended) / With branches at creation | done | asc: `RepoAdmin.set_branchless` (new op 9 of RepoAdminRequest in `proto/asc/v1/repo.proto`; repo admin, x-asc-op-id, audited as `repo.branchless`; asc refuses to strand a ref: branchless on only when every branch is shared, off only when no user ref exists; branch `feat/repo-set-branchless` in the asc repository). Hub: `services/ascsync/branchless.go` SetBranchless, `services/repository/create.go` (set while the repository is still empty, before the first commit), the radio in `templates/repo/create_asc.tmpl` (default Branchless, as the board recommends). Checked on a live asc: branchless repository has the flag and one audit event, "With branches" is the asc default and records nothing. The settings page still has no form to change the mode of an existing repository. | - |
| NewRepo | Add AGENTS.md switch | done | `H:templates/repo/create_asc.tmpl`, `H:services/repository/asc_init_files.go` (AgentsFileText; a first commit even without the README switch), `forms.CreateRepoForm.AgentsFile`; checked on a live asc: the file is in the first commit | - |
| NewRepo | Add a policy file switch (`.asc/policy.toml`, 2 approvals, picked checks) | done | `H:templates/repo/create_asc.tmpl` (approvals 0 to 5, default 2, required checks by name), `H:services/repository/asc_init_files.go` (PolicyFileText: `version = 1`, you as maintainer, `[trunk]` approvals and checks; names are checked, nothing typed reaches the file unquoted; only where asc keeps the repository); asc accepted the file on a live asc; tests: TestParsePolicyChecks, TestPolicyFileText, TestAscExtraFiles | - |
| NewRepo | License choice | done | `H:templates/repo/create_init.tmpl` | S |
| NewRepo | Preview card ("acme / payments-v2, Private, Policy on") and "Then, from your terminal" commands | done | `H:templates/repo/create_preview.tmpl`: owner / name, visibility and policy follow the form (`repo-new.js`), then the terminal commands; `repo-new.test.js` | S |
| NewRepo | Quota line "41 of 100 used on the Team plan" and quota-reached state | done | `H:routers/web/repo/asc_newrepo.go` prepareAscNewRepo: "N of M repositories used." and "The limit is reached." from the owner's repository count and limit (shown only when there is a limit). The hub has no plans, so there is no plan name; tests: TestPrepareAscNewRepoQuotaLine | - |
| NewRepo | Creating state ("Creating acme/payments-v2 and its first policy...") | done | `repo-new.js`: on submit the button is disabled and says "Creating owner/name…" (or "…and its first policy"); `repo-new.test.js` | S |
| NewRepo | Import running: progress "3,412 of 9,880 history entries", Run in background, Cancel | needs-both | hub `H:web_src/js/features/repo-migration.js` polls Forgejo migration tasks; asc needs a history-to-changes import progress source (`Repo.Ingest`/`ImportGitPush` have no progress stream) | L |
| NewRepo | "Signed with your key mo_41c9" line | done | `H:routers/web/repo/asc_newrepo.go` prepareAscNewRepo reads Signing.ListKeys as the person (asctrust.LoadKeys): "Signed with your key <id>", or "You have no signing key yet" with a link to add one; left out when asc does not answer; checked on a live asc (no key case) | - |
| NewRepo | Sticky primary action on mobile | done | `.ds-sticky-actions` in `create.tmpl` | S |

## Code

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Code | File viewer, line numbers, breadcrumb, raw, copy permalink, large-file card | done | `H:templates/repo/code_view.tmpl`, `view_file.tmpl` | S |
| Code | Segment Code / Blame / History on the file | done | `H:templates/repo/code_view.tmpl` | S |
| Code | Blame cell shows author avatar, intent, change id, age | done | `H:templates/repo/blame.tmpl` | S |
| Code | Blame hover and focus card (intent as "Why", stack, approver, size, Open change, Blame before this); 150 ms delay, 120 ms fade | done (seen on a live hub (ui16): the card shows the change, author, id and links to open it and blame before it; "Why", stack, approver and size are not in it) | `H:templates/repo/blame.tmpl` rows carry no stack or approver data; new endpoint returning change summary (pull lookup by commit, `Changes.ResolveChanges`) and popover script (`H:web_src/js/features/contextpopup.js` is for issue refs only) | L |
| Code | Heat bar paired with age text (recency scale) | done | `H:routers/web/repo/blame.go` (compute age rank per row), `blame.tmpl`, CSS | S |
| Code | Agent author carries the violet agent mark in blame | done (seen on a live hub (ui16): a bot account author gets the agent chip) | author kind from agent identity (`H:services/ascidentity`) in blame row struct | S |
| Code | Click a line number selects and shares a range; selected lines flash once | done | Forgejo `repo-code.ts` range select; flash not verified | S |
| Code | Shortcuts `b` toggle blame, `y` permalink, `l` go to line, `e` propose change, `t` find file, `[` collapse tree | done | `H:templates/repo/code_keys.tmpl` lists the keys of the view; `asc-goto-line.js` adds `l` (a dialog that goes to and marks the line) next to the `t`, `b`, `y`, `e` and `[` bindings; `asc_code_keys_test.go`, `asc-goto-line.test.js` | M |
| Code | File tree with aria-current and "N files touched by open stacks" badges per file | done | `H:routers/web/repo/view_asc_tree_stacks.go`: open pull requests onto the shown branch (at most 50, newest first), their changed paths cached per head, a count per file titled "Touched by open stacks" and the line under the rail; shown only to people who may read pull requests | M |
| Code | Tree collapse with `[` and Files sheet on mobile | done | `rp-tree`, `rp-files-btn` in `code_view.tmpl` | S |
| Code | Outline panel (struct / impl / fn with line numbers) | done (seen on a live hub (ui10): needs [codeintel] enabled on asc serve) | `CodeIntel.Symbols` op=outline is served at `GET /{owner}/{repo}/asc/symbols?op=outline&path=` by `H:routers/web/repo/asc_symbols.go`, but `H:templates/repo/code_view.tmpl` right rail only has `file_info`; embed outline as htmx fragment | M |
| Code | "Used in / 4 references" code-intelligence panel on the file | done (seen on a live hub (ui10): needs [codeintel] enabled on asc serve) | same handler (`op=def`), or `CrossSearch.SymbolPanel` via `H:services/ascsearch/panel.go`; add fragment to right rail | M |
| Code | "Open changes touching this file" card (stack, change 3 of 4, lines changed) | done (seen on a live hub (ui10)) | open pulls by changed path, line ranges from diff; `H:services/pull` | M |
| Code | Shortcuts legend card | done | `code_view.tmpl` right rail | S |
| Code | Propose change bar ("Changes go into a new change on top of main"), Start a new change / Add to my stack | done | `H:routers/web/repo/view_asc_my_stacks.go`; Start a new change opens the editor on a new branch (a new stack); Add to my stack opens it on the head of the person's own open stack onto the shown branch (a menu when there are several, up to 10), so the commit becomes the stack's next change; shown only to people who can write the code | L |
| Code | Directory view columns: Last change, In open stack chip, Author, Size, Updated | done (seen on a live hub (ui16): author, size, last change and updated; the open-stack chip is not drawn) | `H:templates/repo/view_list.tmpl` has name, message, age only; author and size from `Repo.LastCommits`/`ReadTree`, open-stack chip as in tree badges | M |
| Code | Loading state (lines stream in) | done | `H:templates/repo/code_loading.tmpl`: while the next file, view or tree link loads, lines of code of different lengths take the place of the file (`asc-page-loading.js`, reset on back); `asc_code_keys_test.go`, `asc-page-loading.test.js` | S |
| Code | Not found at this version ("It was added in change mzu2d. You are looking at main@8d1c7e0.", Go to main, File history) | done | `H:routers/web/repo/view_asc_not_found.go` on the 404 of `HandleGitError`: names the change of the default branch that added the path (or removed it, or says main never had it), the version being looked at, Go to main (the path there, else its root) and File history; read once per head of the default branch | M |
| Code | Binary preview card | done | `view_file.tmpl` (image/binary branches) | S |
| Code | Long-press line number to select and Share (mobile), blame as avatar column with change sheet | done | `repo/blame.tmpl`: under 1024px the avatar is a button that opens the change in a bottom sheet, and a long press on a line number selects it (or extends the selection) and opens Share, Copy permalink and Reference in a new issue (`asc-blame-sheet.js`); tests `TestBlamePhoneSheet`, `asc-blame-sheet.test.js` | M |

## Changeset

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Changeset | Page shell with breadcrumb History / id, title, Landed chip, Browse files, Copy id | done | `H:templates/repo/commit_page.tmpl`, `commit_header.tmpl` | S |
| Changeset | "Why this exists" (intent) block | done | `H:templates/repo/change_facts.tmpl` (ChangeIntent from the message body of the change, `routers/web/repo/asc_change_facts.go`); tests: asc_change_facts_test.go | - |
| Changeset | "Where to look" and "Risk" lines | needs-both | asc has no such field in `ChangeInfo`/`LogEntry`; produce them through the "Review summaries" AI feature (`A:proto` new RPC or trailer) and show in hub | L |
| Changeset | Facts strip: Size, Files, Checks 14 of 14, Risk | done (seen on a live hub (ui10): Size, Files, Checks; Risk comes with the brief) | stats from `Repo.Diff`, checks from commit statuses; `commit_header.tmpl` | M |
| Changeset | Stack position bar: previous / next change, "Change 2 in retry-with-backoff" | done (seen on a live hub (ui10): Prev, Next and "Change n in stack of m") | `repo.walk_commits` only walks parent commits; use `BuildAscStackPanel` for the pull of the commit, `commit_header.tmpl` | M |
| Changeset | Files grouped by meaning (Behaviour, Tests, Generated) with counts, collapsible groups, generated files folded | done | `H:routers/web/repo/asc_change_files.go` prepareAscChangeFiles (Repo.Classify of the change against its parent): groups Behaviour, Tests, Docs and comments, Renames and formatting, Generated with counts and +/-, collapsible, each file linking to its diff; generated and lockfile files are folded in the diff (IsGenerated); panel in `change_facts.tmpl`; a file asc does not name counts as Behaviour; tests: TestAscChangeGroupOf, TestAscChangeFilesGroupsAndFoldsGenerated, TestChangeFactsRender | - |
| Changeset | Collapsed file buttons with aria-expanded, +/- signs | done | `diff/box.tmpl` | S |
| Changeset | Review block "2 of 2" with approvers and dates | done (seen on a live hub (ui10)) | reviews of the landed pull (`H:models/issues/review.go`), new card on commit page | M |
| Changeset | Checks list with durations | done (seen on a live hub (ui16): a Checks card lists each check with its state; durations are not recorded by the hub) | `commit_statuses.tmpl` shows a summary; expand to list with durations from action runs | S |
| Changeset | "Where it is now": On main, In release v2.18.0, Stack and position | done (seen on a live hub (ui10)) | tags containing the commit (`LoadBranchesAndTags` route exists), pull lookup | M |
| Changeset | Revert as a new change (review required) | done (seen on a live hub (ui16): the Undo card on the change page posts to pulls/N/asc/revert) | commit page uses the Forgejo cherry-pick revert modal; `POST /{owner}/{repo}/pulls/{index}/asc/revert` (`AscStackRevert`, `Stacks.RevertLanded`) exists only for pulls; wire the commit page to it and show "Revert opened as change hv3ps" | M |
| Changeset | Undo from the operation log while recent (replaces Revert) | done | `H:routers/web/repo/asc_change_undo.go`: on a landed change that is still the newest on the default branch, the rail offers "Undo from the operation log" (`/asc/ops/undo` with its seq) in place of the revert; once newer changes land or the log refuses, it says why and keeps the revert (`change_rail.tmpl`); `asc_change_undo_test.go` | S |
| Changeset | Large change state: "412 files changed, showing the 20 that matter most", Show 392 more | done | same file: over 40 files the diff shows the 20 that matter most (Behaviour first, then Tests, Docs, Renames, Generated; larger first), "N files changed, showing the 20 that matter most" and "Show N more" (`?all=1`); the group lists link hidden files to `?all=1#diff-...`; the hub still caps a diff at its own MaxGitDiffFiles; tests: TestAscChangeFilesLargeChangeShowsWhatMattersMost | - |
| Changeset | Loading diff state | done | `H:templates/repo/diff/box.tmpl`: the per-file skeleton and Retry now show on the change (commit) page too, not only on a pull request; a failed load offers Retry (`repo-diff.js`); `asc_review_files_test.go` | S |

## History

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| History | List view with commit rows, copy hash, view at point, search by message and author, "Open operation log" card | done | `H:templates/repo/commits.tmpl`, `commits_list.tmpl` | S |
| History | Lane graph from `Repo.CommitGraph` (colour, lanes, refs, review chip) | done | `H:routers/web/repo/asc_graph.go`, `asc_graph.tmpl` (separate page, Forgejo-style form) | M |
| History | Graph and List as two views of one page with the same rows; graph beside the rows | done | `commits.tmpl`: the List / Graph segment switches `?view=graph` on the same page; the graph draws the lines between the page's rows beside them (`asc_history_graph.go`, any backend), and "All branches" opens the full graph; tests `TestHistoryLanes*`, `TestHistoryGraphView` | M |
| History | Node state shape plus chip (Needs review, Queued, Agent writing); agent vs person nodes; stack brackets; "A stack that landed together" | done | open rows: a shape per state (dashed agent circle writing, hollow warn needs review, ok circle with a check queued, filled ok approved, err bar changes asked, dashed grey draft) plus a chip; landed agent changes get an agent node with a hole; merged pulls of one stack on adjacent rows get a bracket and a screen-reader "Landed together as one stack" (`H:routers/web/repo/commits_asc_open.go`, `commits_list.tmpl`) | L |
| History | "Open on top of main" section listing open changes with stack position, "Waiting on Priya Raman", agent run status | done | `prepareAscOpenChanges`: up to 10 open pulls onto the branch on page 1 with no filter, size (+/-), author, change id, "stack k of n", and a status line (agent run and minutes for people who may read runs, merge queue place, waiting on / approved by / changes asked by); `H:templates/repo/commits_asc_open.tmpl` | L |
| History | Comfy / Compact density switch | done | `commits.tmpl` seg + CSS + localStorage | S |
| History | Filter bar: Everyone / People / Agents, Path, Stack, date range, in the URL, sticky | done (seen on a live hub (ui38): Everyone / People / Agents in the URL (?who=); it reads the newest 500 commits and pages through them; Path, Stack and date range are the existing search; Agents was empty here because no agent account has authored a commit) | author kind needs agent identity; path and date filters exist in Forgejo `RefCommits` partly; `commits.tmpl`, `routers/web/repo/commit.go` | M |
| History | Export | done (seen on a live hub (ui18): Export CSV on the history page, newest first, at most 1000 rows, formula cells quoted) | new route (CSV/JSON of the filtered list) in `H:routers/web/repo/commit.go` | S |
| History | Keyboard: `j`/`k` move, Enter opens, `c` copies id | done | none bound; list-nav helper in `H:web_src/js` | S |
| History | Row hover actions (copy id, browse files) and hover lift | done | copy hash and view-at-point buttons in `commits_list.tmpl` | S |
| History | "Landing rate, last 12 weeks" card with this-week delta | done | Re-judged 2026-11-09 as hub only: one git log on the viewed ref, counted per week like the home Activity card; this week, the change on last week and a bar per week. `H:routers/web/repo/commits_landing.go`, `commits.tmpl` (TestHistoryLandingOf, TestAscHistoryLandingRate) | M |
| History | Graph key legend | done | `asc_graph.tmpl` | S |
| History | "Show 25 older changes" on-demand load (44 px button on mobile) | done | pager/`AscGraphNextURL` | S |
| History | States: loading skeleton, no results with explanation and Clear filters, partial-data banner with Retry | done | `H:templates/repo/commits_table.tmpl`: a skeleton while the next page, filter or search loads (`commits_loading.tmpl`); no results under the people / agents filter says so and offers Clear filters; the partial banner is for that filter, which reads the newest 500 changes (the hub has no index lag), and offers "Show all changes" in place of Retry; `asc_history_states_test.go` | M |
| History | Mobile rail-and-node list, chips open a bottom sheet for Path, Stack, date | done | `commits.tmpl`: under 640px a chip row (who, Path, Stack, Date) replaces the bar; each chip opens its part of one bottom sheet (`asc-history-sheet.js`); Date searches with `after:` / `before:` and keeps the other words; sheets close on a drag down or a backdrop tap (`asc-sheet.js`); tests `TestHistoryPhoneChips`, `asc-history-sheet.test.js`, `asc-sheet.test.js` | M |

## Refs

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Refs | Branches page with filter, default branch card, ahead/behind, Protected chip, Compare | done | `H:templates/repo/branch/list.tmpl`, `row.tmpl` | S |
| Refs | One page with Shared refs, Stacks in progress and Tags sections | done (seen on a live hub (ui10)) | `list.tmpl` lists Forgejo branches and links to `/tags`; add stacks (open pulls with ahead count) and release-linked tags, `routers/web/repo/branch.go` | M |
| Refs | Filter pills All / Shared 3 / Stacks 3 / Tags 14 | done | `H:templates/repo/branch/list.tmpl` pills All / Shared / Stacks / Tags with counts show one kind at a time (`asc-ui.js` `data-ref-filter`, `asc-ui.test.js`); found done in the tree | S |
| Refs | Branchless wording ("only three shared refs"), `RepoConfig.shared_refs` | done | a branchless asc repository (asc `RepoConfig`) titles the page "Shared refs and tags", says how many shared refs there are, and lists only the shared refs in one "Shared refs" section (`H:services/repository/branch.go` `LoadSharedBranches`, `routers/web/repo/branch.go`, `templates/repo/branch/list.tmpl`); when asc cannot answer the page lists every branch | M |
| Refs | Stack cleanup note ("clean themselves up 7 days after the last change lands") | done (the Stacks section says "Stacks clean themselves up N days after the last change lands." from the `[stacks] cleanup_days` asc parsed; the line is left out when the policy leaves the days to the server, whose `landed_ref_days` the hub does not know) | `H:routers/web/repo/asc_ref_sections.go`, `H:templates/repo/branch/list.tmpl`, `H:services/asctrust/ops.go` (`PolicySummary.CleanupDays`); `A:crates/asc-serve/src/reftl.rs` (`LandedReaper`); `asc_ref_cleanup_test.go`, `ops_test.go` | S |
| Refs | Tags section with release title, change count, author, date | done | `H:templates/repo/branch/ref_tags.tmpl`: the newest tags with release title, how many changes each adds to the tag before it (from the stored commit counts, `refTagsOf`), author and age; `asc_ref_tags_test.go` | M |
| Refs | Row More menu names its target; no delete on protected refs | done | `branch/row.tmpl` | S |
| Refs | State: nothing matches with "Search stacks" link | done | `list.tmpl` empty state has no stack search action | S |
| Refs | State: delete protected ref refused with "Open rule" | done | `H:routers/web/repo/asc_rule_link.go`: the refusal to delete a protected branch (`branch.go`) or tag (`release.go`) ends with "Open rule", a link to the matching rule, for a repository admin; `asc_rule_link_test.go` | S |

## Compare

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Compare | From / To, swap, "only what the stack adds" checkbox, ahead / behind chips, common ancestor | done | `H:templates/repo/asc_compare.tmpl`: From / To selects, swap, ahead / behind chips and the common ancestor; "Only what the stack adds" is the three-dot compare (from the common ancestor) with a hint; `asc_compare_test.go` | S |
| Compare | From / To as labelled selects listing shared refs, stacks and tags (text inputs today) | done | `asc_compare.tmpl`; options from refs/pulls | S |
| Compare | Per-change and per-file +/- counts and avatars | done (seen on a live hub (ui10)) | `AscCompareCommit`/`AscCompareFile` carry no stats; `Repo.Compare` returns `PathChange` and `LogEntry`; add numstat (`Repo.Diff`) | M |
| Compare | Same version state ("Both sides are main@a41f9c2") | done | `asc_compare.tmpl` shows "Nothing to compare" without the ref | S |
| Compare | Unknown ref with "Did you mean retry-with-backoff?" | done | `H:routers/web/repo/asc_compare_hints.go` suggests the nearest ref name for an unknown one | S |
| Compare | Open any change or file from the lists, both sides kept in the address | done | links and query string | S |

## Search

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Search | Text, Symbol and Meaning modes as radio group (lexical / symbol / semantic) | done | `H:templates/repo/asc_search.tmpl`, `H:routers/web/repo/asc_search.go`, `H:services/ascsearch` | S |
| Search | Scope radios: this version, change or stack, all branches, all repositories | done | `H:routers/web/repo/asc_search_scope.go` | S |
| Search | Cross-repository results grouped by repository, URL `/-/asc/search` | done | `GET /-/asc/search` serves the search of every repository, grouped by repository, while asc is on and code is not turned off site-wide (`H:routers/web/explore/asc_search.go` `AscGlobalSearch`, `routers/web/web.go`); the header search, the page form, scope "repos" and the Meaning tip link there; `/explore/code` still serves the page; REST `GET /api/v1/asc/search` | M |
| Search | Facets: Repository (21 / 9 / 5, "11 more"), Language, Path, Branch or commit, "Only files the stack changes" | done | Repository facet on the all-repositories search (`H:routers/web/explore/asc_search_repos.go`, three shown and "N more"), Branch facet on an all-branches search (`ascSearchBranchFacets`); tests `TestAscExploreRepoFacets`, `TestAscSearchAllBranches` | M |
| Search | Result card: path, language, branch list, snippet with highlights | done | `asc_search.tmpl` | S |
| Search | Relevance percentage with a bar (grows over 300 ms) | done | `Chunk` score is shown as a raw number pill (`$c.Score`); convert to percent bar in `asc_search.tmpl` | S |
| Search | "Why it matched" reason sentence per result | done (each file hit and chunk has a line under its path: "Matched the text `retry`, `backoff` on 3 lines", "Matched the symbol name `RetryPolicy`" or "Close in meaning to your question and mentions `refund`", from the `MatchReason` asc sends on `FileHit` and `Chunk`; a hidden file keeps its terms and loses its spans; a result without a reason draws no line. The repository search, the all-repositories search and the REST answers (`reason` on `AscFileHit` and `AscChunk`) carry it) | `H:services/ascsearch/reason.go`, `H:templates/repo/asc_search_why.tmpl`, `H:routers/web/repo/asc_search.go`, `H:routers/web/explore/asc_search.go`, `H:modules/structs/asc_search.go`; `A:crates/asc-serve/src/codeintel/reason.rs`; `reason_test.go`, `asc_search_why_test.go`, `asc_search_test.go` (API and explore) | L |
| Search | Symbol chips under a result ("RetryPolicy::delay", "14 references elsewhere", "changed 3 h ago") | done | `H:routers/web/repo/asc_search_defs.go`: DEF and REFS per word of a text search of one version, last change from `Repo.LastCommits`; test `TestAscSearchSymbolsMatched` | M |
| Search | "Symbols matched: RetryPolicy, retry" line | done | repository search (`ascSearchDefinitions`) and the all-repositories search (`definitions`) | S |
| Search | Code intelligence side panel (definition, signature, Go to definition, references, Called from, cross-repo callers) | done | `H:routers/web/repo/asc_search_panel.go`: `GET /{repo}/asc/search/panel` fragment from `ascsearch.Panel` (CodeIntel fallback), cross-repo rows only for readable repositories; `asc-search-panel.js` loads it into the right column at 1200 px and wider | L |
| Search | Coverage line ("Indexed at e91b0d4, 99.2% of files; 4 files too large, listed in settings") | done | `asc_search.tmpl` coverage block (percent and lag); link to settings is missing | S |
| Search | Sort: best match, pagination "Showing 4 of 38", page links | done | `H:routers/web/repo/asc_search_page.go`: Best match or Path sort, pages of 10 over the bounded answer with "Showing 11–20 of 38" (of more than N when asc cut it); facets count the whole answer | M |
| Search | "The other two kinds of search" teaser (Text and Symbol hits for the same query) | done | `H:routers/web/repo/asc_search_teaser.go`: Text and Symbol lookups counted with their first paths; Meaning offered as a link without a model call; a failed or empty lookup hides its line | M |
| Search | First-visit state with example queries | done (seen on a live hub (ui28): three example searches that open Meaning search) | `asc_search.tmpl` | S |
| Search | No-results state with "Search all branches / All repositories" actions | done | `asc_search.tmpl` shows a plain banner | S |
| Search | Not available (503) for meaning search with "Use Text search" and "Open storage settings" | done | `s_err_503` banner exists; add actions | S |
| Search | Rate limited (429) keeps the query | done | `s_err_429` | S |
| Search | Loading state | done | `repo/asc_search_loading.tmpl`: a new search, a facet or an example query shows three result cards while the page loads (`asc-page-loading.js`); test `TestSearchLoadingState` | S |
| Search | `/` focuses search in the repository | done | `ds-shell.js` | S |
| Search | Mobile: sticky search with mode and scope, Filters bottom sheet, results as cards | done | below 900 px the form (query, kind, scope) is sticky and the narrowing fields sit under it (`form=` attribute); `asc-search-filters.js` moves the facets into a `dialog.rp-sheet` opened by Filters (count of filters set); results are `.asc-card` | M |

## Releases

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Releases | Timeline of release cards, tag, stable / pre-release / draft chips, Compare, edit, assets list with downloads | done | `H:templates/repo/release/list.tmpl`, `release_tag_header.tmpl` | S |
| Releases | Releases / Tags segment, search filter, feed | done | `release_tag_header.tmpl` | S |
| Releases | Filter pills All 14 / Stable 11 / Pre-release 2 / Draft 1 and Sort (Newest, Most downloaded) | done (seen on a live hub (ui10): pills clicked, sections hide and show) | `release_tag_header.tmpl`; `H:routers/web/repo/release.go` query params | M |
| Releases | Provenance strip ("Built by run #4218 from main@a41f9c2, 14 of 14 checks passed, signed with release key 7F3A...") | done | `tag/verification_line.tmpl` gives the signature; add the building Actions run and check summary, link release to run (`H:models/actions`);; Audit 2026-10-30 (part built): Provenance line exists from Forgejo data; the hub has no client for asc Releases, so no run id or release key.; Audit 2026-10-30 (built): the release list shows where the release was built, its checks and who signed it from the asc record (Releases.List, services/asctrust/releases.go ListReleaseRecords, release/list.tmpl); a release published on the hub is made in asc as the publisher (MirrorRelease). The build run is empty unless the release was made with asc release or a CI run was named | L |
| Releases | Notes grouped Added / Changed / Fixed, "written from the intent of each change" | done | the New release dialog writes the notes from asc Releases.DraftNotes (`H:routers/web/repo/asc_release_draft.go`, `web_src/js/features/asc-release-new.js`); a release without notes shows notes grouped Added, Changed, Fixed from the intent of each change since the release before, with "Edit before publishing." for writers (`routers/web/repo/asc_release_facts.go`, `templates/repo/release/list.tmpl`) | L |
| Releases | "N changes in this release" list with author, change id, category | done | each card counts its changes in the meta line and lists them newest first with the avatar, author, a category chip and the asc change id the commit landed as (the short commit id otherwise); chips All, Added, Changed, Fixed filter the list (`H:routers/web/repo/asc_release_facts.go` `relChangeIDs`, `models/issues/pull_asc_stack.go`, `web_src/js/features/asc-release-changes.js`) | M |
| Releases | Asset table: size, SHA-256 checksum (copy on click), downloads | done | `H:models/repo/attachment.go` has no checksum column; compute at upload; template shows name, count, age only; Audit 2026-10-30 (built): the SHA-256 of each asset is computed when the release is published and recorded in asc (Releases.Create); the asset table shows it under the name. Downloads count and size were already there | M |
| Releases | Next release summary ("6 changes since v2.18.0", Draft v2.19.0 View) | done (seen on a live hub (ui10)) | `Compare(latest tag, default branch)` | M |
| Releases | "Download the latest" with `asc release download v2.18.0` and per-platform line | done (2026-11-05: rail lists Linux/macOS/Windows files with arch, size and a copyable `asc release download TAG --asset NAME`; other files below) | right rail in `list.tmpl`; the `asc release` CLI command must exist (check `A:crates/asc/src/cli`) | S |
| Releases | Release health: cadence, rollbacks | done | the rail card shows the median gap between published releases, the rollbacks among the newest six (a release cut from a commit older than the commit of the release before it) and releases per week over 12 weeks (`H:routers/web/repo/asc_release_health.go`, `templates/repo/release/list.tmpl`) | M |
| Releases | Get notified: Watch releases, Atom feed | done | watch menu has releases, `.rss` link in header; Atom naming differs | S |
| Releases | New release dialog: proposes next tag, "Write from the N landed changes", assets drop zone, pre-release, Save draft, Publish | done (2026-11-05: `release/new_dialog.tmpl` + `asc-release-new.js`; fills the proposed tag from `releases/asc_draft`, appends the drafted notes, dropzone, pre-release, Save draft, Publish; posts to `/releases/new`; no script opens the full page) | `H:templates/repo/release/new.tmpl` is a full page (no dialog, no generated notes, no tag proposal); Audit 2026-10-30: the data for it exists (GET /releases/asc_draft: proposed tag, notes written from the landed changes); the dialog itself is not built | L |
| Releases | States: no releases (shows `asc release create v1.0.0`), loading, storage error with Retry | done (2026-11-05: the empty state shows the copyable command to writers; the New release dialog says while asc drafts; an asc failure to read its release records shows a banner with Retry, `asctrust.ReadReleaseRecords`) | the command does not exist in the CLI (see above); `list.tmpl` empty state has no command, loading skeleton or storage-error banner | S |
| Releases | Mobile: new release as bottom sheet, 44 px downloads | done (2026-11-05: the dialog is an `rp-sheet` below 1024 px with 44 px actions; rail downloads and copy buttons are 44 px targets) | `new.tmpl` | M |

## Dependencies

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Dependencies | Views Findings / Updates / Configuration (`?view=`) | done | `H:templates/repo/asc_deps.tmpl`, `H:routers/web/repo/asc_deps.go` | S |
| Dependencies | Scan now, refresh advisory data, last-scan line, lookup-incomplete banner | done | `asc_deps.tmpl` | S |
| Dependencies | Summary tiles: findings by severity bar chart with text table, Outdated 11 (3 with security fix), Packages 214 by ecosystem, Last scan 2 h "Complete, 3 of 3 sources" | done | `DepsService.ListFindings/ListUpdates` give the counts; add tile row to `asc_deps.tmpl` | M |
| Dependencies | Severity chips All / Critical / High / Medium / Low with counts, Show dismissed | done | state tabs and min-severity select | S |
| Dependencies | Finding row expands (aria-expanded): "Why it matters", "How it gets in" text tree, Propose fix | done | rows are flat in `asc_deps.tmpl` (path is one line); render the path as a tree and add per-finding Propose fix posting to `/asc/deps/propose` | M |
| Dependencies | Dismiss with required reason and note, Reopen, "until Dec 1" expiry | done | `AscDepsDismiss`; reasons + note present, expiry date not verified | S |
| Dependencies | Updates table: package, current, latest, kind, security, proposal link (Change kq2nv) or Propose | done | `asc_deps.tmpl` updates view | S |
| Dependencies | Propose updates panel: Security fixes only, Largest jump, Most proposals, 5-stack limit, Preview, Propose | done | `asc_deps.tmpl`, `asc_deps_plan.tmpl` | S |
| Dependencies | Configuration view: ecosystems, schedule ("Weekly, Mondays 06:00 UTC, next run in 4 days"), threshold, grouping, ignored, "Propose a change" to `.asc/deps.toml` | done | `asc_deps.tmpl` (read-only list); "Propose a change" button not verified | S |
| Dependencies | Scan running state with real progress bar ("About 20 seconds left") | done | `DepsService.ScanStatus` (`A:proto/asc/v1/deps.proto`, `A:crates/asc-serve/src/deps/progress.rs`) says the phase, the advisory steps done of total, a bar in thousandths and an estimate; the hub polls `asc/deps/scan/status` once a second while the scan form is submitted (`routers/web/repo/asc_deps.go` `AscDepsScanStatus`, `services/asctrust/deps.go` `LoadDepScanStatus` clamps every number, `web_src/js/features/asc-deps.js` draws the bar, the phase and "About N seconds left") | M |
| Dependencies | No findings state ("Checked 214 packages 2 hours ago against 3 sources") | done | `asc_deps.tmpl` prints a short empty line | S |
| Dependencies | Never scanned state with package count | done | `never_scanned` banner (count not shown) | S |
| Dependencies | Unsupported ecosystem ("2 files are not scanned. tools/build.zig.zon") | done | `unsupported` banner (file names not listed) | S |
| Dependencies | Advisory text plain, https-only links | done | cleaned in `H:services/asctrust` | S |

## Insights and InsightsPeople

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Insights | Pulse and Contributors pages exist (Forgejo activity, git contribution graph) | done | `H:templates/repo/pulse.tmpl`, `contributors.tmpl` (git-level data, not the board) | S |
| Insights | Four tiles: Changes landed (vs previous 30 days), Stacks opened, Median review time, Written by agents | done | asc: statistics over landed changes with author kind (new RPC, e.g. `Stats.Pulse`); hub: reviews and pulls from DB, new handler `H:routers/web/repo/insights.go`;; Audit 2026-10-30 (built): Insights service exists in asc master and the hub. | L |
| Insights | Changes landed per day, people vs agents, chart with direct labels, Table toggle | done | same data; chart component in `H:web_src/js/components`; `code-frequency.js` is git commit based;; Audit 2026-10-30 (built): templates/repo/insights_asc.tmpl:51 svg chart, table toggle | L |
| Insights | Seg controls Everyone / People / Agents and 7 days / 30 days / Year that update chart and tiles together | done | query params + client update; depends on data above;; Audit 2026-10-30 (built): templates/repo/insights_asc.tmpl:10-15 ins_who, ins_range segmented controls | M |
| Insights | Median review time by week ("Getting faster") and by size of change (Under 50 / 50 to 300 / Over 300) | done | review events in hub DB (`models/issues/review.go`);; Audit 2026-10-30 (built): templates/repo/insights_asc.tmpl:76-86 by week, by size of change | M |
| Insights | Recently landed stacks and "Waiting the longest" list | done (seen on a live hub (ui10)) | pulls + queue + last activity | M |
| Insights | States: new repository, loading, "Index behind" banner | done | `pulse.tmpl` | S |
| InsightsPeople | Contributors table: landed, reviews given, median review, 12-week sparkline, share, role, "Agent run by Priya Raman" | done | agent ownership from `Agents`/`AgentTokens`; counts as above; replaces `contributors.tmpl`;; Audit 2026-10-30 (built): templates/repo/insights_people.tmpl (people_weeks, run_by, people_share) | L |
| InsightsPeople | Who writes the code (46/54) with "every agent change approved by a person" sentence | done | same statistics;; Audit 2026-10-30 (built): templates/repo/insights_people.tmpl (who_writes, who_split, ins_all_approved) | M |
| InsightsPeople | Review load list ("Agents never review") | done | review counts per reviewer;; Audit 2026-10-30 (built): templates/repo/insights_people.tmpl:58 review_load, agents_never_review | S |
| InsightsPeople | Rows open the person or the agent run | done | links in new template | S |
| InsightsPeople | Empty, loading, partial-data states | done | new template;; Audit 2026-10-30 (part built): Empty state is there; loading and partial-data states were not found. | S |

## GitNote

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| GitNote | One-line collapsed disclosure after the asc command, aria-expanded, URL shown only after expand, git clone command with Copy | done | `H:templates/repo/clone_asc_tabs.tmpl`, `clone_dialog.tmpl` | S |
| GitNote | Body copy: "A push arrives as changes and goes through policy... Stacks appear as branches named asc/<stack>" | done | `options/locale/locale_en-US.ini` `repo.clone_with_git_help` differs from the board | S |
| GitNote | Bridge switched off by admin: one sentence "The Git remote is off on this hub. Everything works with asc." and no button | done | `clone_asc_tabs.tmpl` renders nothing when `AscGitAccess` is false | S |
| GitNote | Inline variant in the repository home hero card; also in settings Working mode when branches are on; also in the empty-repository state | done | `home_dir.tmpl`, `settings/asc_branchless.tmpl`, `empty.tmpl` | S |

## Stacks

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Stacks | Area sub-navigation Stacks / Merge queue / Policy gate / Operation log | done | `H:templates/repo/asc_stack_nav.tmpl` is used on queue, gate and ops pages; the `/pulls` list does not include it | S |
| Stacks | Merge queue page with pause and resume | done | `H:routers/web/repo/asc_queue.go`, `asc_queue.tmpl` | S |
| Stacks | Stack row: avatar, title, stack name, author, change count, +/- size, checks, age, status chip (Needs your review, Queued to merge, Checks failing, Agent writing, Waiting on author), reviewers | done | `/pulls` renders the Forgejo issue list (`H:templates/repo/issue/list.tmpl`, `shared/issuelist.tmpl`); build a stack-grouped list from stacked pulls (`H:services/pull`, `BuildAscStackPanel`); Audit 2026-10-30 (read): built by the repo pages work, templates/repo/stack_row.tmpl and stack_board.tmpl, wired in repo/issue/list.tmpl | L |
| Stacks | Rail with segments per change and text alternative "Stack of 4 changes"; legend (Landed, Approved ready, Waiting, Agent writing, Draft) | done | same data as the Repo progress rail; one shared template; Audit 2026-10-30 (read): templates/repo/stack_board.tmpl legend and the row rail | M |
| Stacks | Filters Open / Mine / Agents / Needs me / Closed with counts; sticky filter bar | done | Needs me = review requested or queue-ready for the viewer; Agents needs an agent flag; `H:routers/web/repo/issue.go` (`Pulls`); Audit 2026-10-30 (read): stack_board.tmpl filter group with counts (f_open, f_mine, f_agents, f_needs_me, f_closed) | M |
| Stacks | Sort Newest / Needs attention | done | attention score from status; Audit 2026-10-30 (read): stack_board.tmpl sort links | S |
| Stacks | "Your turn" card with jump to the exact change | done | pulls awaiting the viewer's review or enqueue; Audit 2026-10-30 (read): stack_board.tmpl your_turn card with open_change | M |
| Stacks | "From your terminal" card (`asc stack list`, `asc stack submit`) | done | template | S |
| Stacks | New stack button | done | no web create-stack flow; `H:routers/web/repo/pull.go` creates pulls by compare; agents or terminal create stacks; Audit 2026-10-30 (read): stacks_new links to the compare page in the empty state and the header | M |
| Stacks | Keyboard `j`/`k` and Enter | done | list-nav helper | S |
| Stacks | States: empty (Nothing is in flight, New stack, Open history), skeleton, error with request id | done | list template; Audit 2026-10-30 (read): stacks_empty_title, stacks_failed with request id and Try again in stack_board.tmpl | S |
| Stacks | Row hover slide-in, status chip pulses once on change | done | CSS + SSE event (`H:services/ascinbox` / `eventsource.sharedworker.js`) | S |
| Stacks | Mobile: 44 px rows, chips scroll | done | list template | S |

## RepoSettingsMenu (mobile) and the shared settings shell

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| RepoSettingsMenu | One left menu with groups Repository (General, Access N, Rules, Policy, Branches and stacks), Automation (Webhooks N, Secrets and variables), Data (Storage, AI keys), Safety (Secret scanning, Audit log) and count badges | done | `H:templates/repo/settings/navbar.tmpl` has General / Code / Integrations / BYO / Policy groups with Forgejo pages (Units, Collaboration, Git hooks, LFS, Actions runners) and no counts | M |
| RepoSettingsMenu | Mobile root list with status per row ("1 failing"), "Find a setting" search over names and descriptions, danger zone entry, back arrow keeping scroll | done | `snav_*` templates have no search or status; new `settings` index partial | M |

## RepoSettingsGeneral

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| RepoSettingsGeneral | Name, description, one save bar with "2 unsaved changes", Discard, Save | done | `H:templates/repo/settings/options.tmpl`, `H:web_src/js/features/repo-settings.js` | S |
| RepoSettingsGeneral | Name rename note "redirects the old address for 90 days" and taken-name error | done | `H:models/repo/redirect.go` (`ExpiresUnix`, `RedirectKeepDays`, migration v18p), `H:templates/repo/settings/options.tmpl` hint and inline taken-name error, saved flash | M |
| RepoSettingsGeneral | Topics field in General (chips, up to 20, Add) | done (seen on a live hub (ui38): chips, up to 20, added with comma or Enter, removed with their button, saved in place) | topics are edited in the About card only; add to `options.tmpl` | S |
| RepoSettingsGeneral | Visibility as radio cards (Private / Internal / Public) with confirmation when changing | done (seen on a live hub (ui38): three cards; changing the level opens a dialog that names the new level and what it means, Cancel sends nothing; Internal is open to every signed-in, non-restricted person) | `options.tmpl` uses a Private checkbox; Internal does not exist in the model (see NewRepo) | L |
| RepoSettingsGeneral | Features switches: Stacks and change review (locked, Required), Agent runs, Dependencies, Releases, Meaning search, Wiki | done (a Features card on General: Stacks locked on as Required, the other switches saved in one form; agent runs, dependencies and meaning search are per-repository switches (`asc_repo_feature_off`, migration v18r) that hide their tab and answer 404, meaning search falls back to text search; releases and wiki add or remove the units) | `H:models/repo/asc_feature.go`, `H:services/context/asc_features.go`, `H:routers/web/repo/setting/asc_features.go`, `H:templates/repo/settings/options.tmpl` | M |
| RepoSettingsGeneral | Working mode radio cards and shared refs (`main`, `release/*`), Cloud sync switch | done | `H:templates/repo/settings/asc_branchless.tmpl` (separate page, not inside General) | S |
| RepoSettingsGeneral | Danger zone: Transfer, Archive, Delete with typed full name `acme/payments` and counts ("Removes 1,204 changes, 14 releases and 23 issues") | done (seen on a live hub (ui28): the delete dialog names the changes on the default branch, stacks and pull requests, releases and issues; the 30-day undo is not possible, Forgejo deletes at once) | `options.tmpl` has transfer, archive, delete; typed string is the repo name and the counts are missing | M |
| RepoSettingsGeneral | "An owner can undo the delete from the operation log for 30 days" | done (seen on a live hub (ui30): Delete moves the repository to a 30 day trash (nobody can read it, it is in no list, its name stays taken); the owner, an organisation owner or a site administrator restores it from Recently deleted; a nightly cron task purges it) | soft delete in hub plus restore; asc keeps repo data for the window (`Repo.RepoAdmin`) | L |
| RepoSettingsGeneral | Saving state, saved toast (5 s), unsaved-bar motion | done | `repo-settings.js` | S |

## RepoSettingsAccess

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| RepoSettingsAccess | One list of people, teams and agents with role select, last active, Show All / People / Agents | done | `H:templates/repo/settings/collaboration.tmpl` renders one list (`asc_access_list.go`: rows, last active from sign-in and token use, Show filter with counts); Maintain role is `collaboration.asc_maintain` (migration v18t) with write mode, which opens General, Rules and Tags but not rename, visibility, the default branch or other settings; the search lists teams only to who may add them (`services/context/permission.go` `RequireRepoSettings`); tests `TestAscAccessMaintain`, `TestAscAccessList`, `TestAscMaintain` | L |
| RepoSettingsAccess | Combined search (people, teams, agents) with role chosen before adding | done | `/settings/collaboration/search` answers people, agents and teams with their kind; one add form takes a name and a role (read, write, maintain, admin) and adds a team when the name is a team; test `TestAscAccessList` | M |
| RepoSettingsAccess | Role select changes access at once with an Undo toast | done (seen on a live hub (ui34): the role applies when picked, a toast reports it and Undo puts the old role back (checked: it persisted)) | JS handler on `collaboration.go` POST | M |
| RepoSettingsAccess | Agent rows: Agent chip, violet mark, "Limited by policy to src/ and tests/" | done | policy lookup per agent identity;; Audit 2026-10-30 (was marked done, part missing): Marked done; the text Limited by policy to ... was not found. | M |
| RepoSettingsAccess | Remove agent confirm that says the running run stops and open changes stay with the owner | needs-both | `Agents.CancelRun` on removal in `collaboration.go`; confirm dialog text | M |
| RepoSettingsAccess | Invite that already has access ("Priya Raman already has Maintain access") | done (seen on a live hub (ui26): the invite answers "rev already has Write access to this repository."; a maintainer is named with Maintain, test `TestAscAccessMaintain`) | `collaboration.go` error text | S |
| RepoSettingsAccess | Deploy keys on the same page: title, fingerprint, read-only or read-write, added by, last used, Add dialog | done | `/settings/collaboration` and `/settings/keys` both render the access page; `H:templates/repo/settings/deploy_keys_section.tmpl` with the Add dialog (`asc-deploy-key-add.js`); `deploy_key.added_by_id` (migration v18k) set on web and API add | M |
| RepoSettingsAccess | No deploy keys empty state | done | `settings.no_deploy_keys` | S |

## RepoSettingsPolicy

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| RepoSettingsPolicy | File header: `.asc/policy.toml`, Valid chip, "Last changed by Priya Raman, 5 days ago, in change kz4np" | done | `H:templates/repo/settings/asc_policy.tmpl` header: the Valid chip, the changeset it was read from and "Last changed by …, … ago, in change …" from the newest default-branch change touching `.asc/policy.toml` (`ascPolicyLastChange`, through the git backend); `asc_policy_lastchange_test.go` | S |
| RepoSettingsPolicy | TOML editor with Check (validate), diagnostics with line, Propose change | done | `H:routers/web/repo/setting/asc_policy.go` (`/check`, `/propose`) | S |
| RepoSettingsPolicy | Seg Form / TOML / Changes (three views of one file) | done (seen on a live hub (ui18): three tabs over one file, arrow keys move; Form fields are approvals, checks, signing, force, merge strategy and queue, agent scope, denied paths and budgets) | `asc_policy.tmpl` has a textarea and a read-only "what it means" table only | L |
| RepoSettingsPolicy | Form: approvals stepper, check chips with Add, authors-may-approve switch, keep-approvals switch | needs-both | form needs a structured parse (`ValidatePolicyResponse.parsed`); keys `author_may_approve`, `keep_approvals_on_restack` do not exist in `A:crates/asc-core/src/policy.rs` | L |
| RepoSettingsPolicy | Agents section: write paths, never-touch paths, budget minutes and tokens, locked "A person approves every agent change" | needs-both | schema has `default_scope`, `denied_paths`, `max_runtime_seconds`, `budgets.max_tokens`; no `human_approval`; UI mapping minutes to seconds;; Audit 2026-10-30 (part built): Agents section exists as text areas and numbers; the approvals stepper, check chips and switches of the board were not found. | L |
| RepoSettingsPolicy | Merge queue section: use queue, batch size, checks after merge | done (seen on a live hub (ui20): use queue, batch size (1 to 20) and checks after merge, written as [merge] queue, batch and post_merge_checks) | `[merge] queue` exists; `batch` and `post_merge_checks` do not (`A:crates/asc-serve` merge queue, `proto/asc/v1/mergequeue.proto`) | L |
| RepoSettingsPolicy | Live file beside the form with changed lines flashing (+/-) | done (seen on a live hub (ui20): the file as it would be, changed lines marked + and -, beside the Form view) | client-side diff against `GetSource.text` | M |
| RepoSettingsPolicy | Changes tab: diff of the one pending edit; "Changing policy is a change" panel naming reviewers (Priya Raman and Diego Alvarez) | done | `H:templates/repo/settings/asc_policy.tmpl` Changes view: the line diff of the pending edit and a "Changing policy is a change" panel listing the maintainers named in the policy as reviewers (or saying none is named); `TestAscPolicyChangesReviewers` | M |
| RepoSettingsPolicy | TOML error state: "Line 11: max_minutes must be a whole number", form disabled while invalid | done | diagnostics list (`asc-policy-diagnostics`); form disable does not apply because there is no form | S |
| RepoSettingsPolicy | Read-only state for non-maintainers with names to ask | done (the page shows a banner naming the maintainers and disables the form; the page itself is for repository administrators, checked by the unit tests of the template data, not seen with a second account) | route needs repo admin (`registerAscTrustRepoSettingsRoutes`); readers cannot see the page; show read-only for readers with maintainers from `GetPolicy` | M |
| RepoSettingsPolicy | After-propose state: "Proposed as change kz9fw. Waiting for one approval" with Open change | done | `p_proposed` | S |
| RepoSettingsPolicy | Loading state | done | `H:templates/repo/settings/asc_policy_loading.tmpl`: while asc reviews, checks, proposes or toggles, the file header, the three tabs and the fields show as skeleton (`asc-page-loading.js`, reset on back); `asc_policy_loading_test.go` | S |

## RepoSettingsRules

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| RepoSettingsRules | Protected refs table: pattern, kind (Trunk, Release lines, Tags), can land (Merge queue, Maintainers, Release key), force push Never, signed changes Required, edit and delete | done (seen on a live hub (ui16): the Rules page shows the asc policy of the default branch (approvals, checks, signing, force push, queue, maintainer-only paths); one rule only, no kind column; the branch protection API reads and proposes it, see docs/asc/BRANCH-PROTECTION.md) | `H:templates/repo/settings/branches.tmpl`, `protected_branch.tmpl`, `tags.tmpl` carry Forgejo rules (branch and tag rules on two pages); fold into one table and map to the columns | L |
| RepoSettingsRules | Add / Edit rule side panel with Who can land checkboxes (Merge queue, Maintainers, Release key (CI), Agents), Require signed changes, Allow force push, Allow deleting | needs-both | Forgejo rule has push whitelists; asc policy has `allow_force`/`require_signed` only, no per-ref kinds or agents (`A:policy.rs` Trunk) | L |
| RepoSettingsRules | Pattern overlap error ("Rules cannot overlap") and "2 open stacks target a ref this pattern matches" | done (a git repository: saving a new or renamed pattern that protects a branch another rule protects is refused, on the web form and the API (422); two patterns overlap when either matches the other or a sample name of it, or a branch matches both; under the pattern the form shows the rule it overlaps and the open pull requests that target a branch it matches, checked as you type; an asc repository has one rule, its policy) | `H:models/git/asc_rule_overlap.go`, `H:models/issues/asc_open_pulls_base.go`, `H:routers/web/repo/setting/asc_rule_check.go` (`GET settings/branches/check`), `H:web_src/js/features/asc-rule-check.js`, `protected_branch.tmpl` | M |
| RepoSettingsRules | Rule removed with Undo for 30 minutes from the operation log | done | `H:models/git/asc_removed_rule.go`: removing a rule (page or `DELETE /branch_protections/{name}`) keeps a copy in `asc_removed_rule` (migration v18s) for 30 minutes; the rules page lists each removal with Undo until its time, `POST settings/branches/removed/{id}/restore` (`asc_rule_undo.go`) puts it back with every setting and refuses a late undo, a name in use again or an overlap with a rule added since; Forgejo rules live in the hub DB, so the copy is kept there, not in asc ops; `asc_removed_rule_test.go`, `asc_rule_undo_test.go` | M |
| RepoSettingsRules | Stack behaviour switches: restack when main moves, keep approvals when only the base changed, let collaborators push onto a stack | done (the Rules card shows the three switches On or Off, and the policy Form view has "Restack open stacks when the default branch moves" and "People with write access may update any stack" next to "Keep approvals when a change is restacked"; they write `[stacks] restack_on_main_move`, `[stacks] collaborators_may_push` and `[review] keep_approvals_on_restack`. asc parses and returns the stack switches and the hub acts on them: a push that moves the default branch queues the repository once, and the job restacks each open stack on that branch as the person who opened it, leaving a stack that conflicts or that they may not rewrite for a person; with `collaborators_may_push` a writer may restack, restore a version of or resolve a conflict on a stack they did not start. A policy that cannot be read opens nothing) | `H:services/pull/asc_auto_restack.go`, `H:services/pull/asc_restore_version.go` (`ascCollaboratorsMayPush`), `H:services/ascbridge/stackrules.go`, `H:services/asctrust/policyform.go`, `H:routers/web/repo/setting/asc_rules.go`, `H:templates/repo/settings/branches.tmpl`, `H:templates/repo/settings/asc_policy.tmpl`; `A:crates/asc-core/src/policy.rs`; `asc_auto_restack_test.go`, `asc_collaborators_push_test.go`, `stackrules_test.go`, `policyform_test.go`, `asc_policy_strategy_test.go` | L |
| RepoSettingsRules | Longest stack (8) and clean-up after landing (7 days) | done (the Rules card shows "Longest stack" and "Clean-up after landing", or "No limit" and "The server's setting"; the policy Form view has a Stacks fieldset that writes `[stacks] max_depth` (1 to 100) and `cleanup_days` (1 to 3650) and takes a key out when its field is emptied; asc refuses a longer stack at the merge queue and deletes landed refs after the days) | `H:services/asctrust/policyform.go`, `H:services/asctrust/toml.go` (`removeKey`), `H:routers/web/repo/setting/asc_rules.go`, `H:templates/repo/settings/branches.tmpl`, `H:templates/repo/settings/asc_policy.tmpl`; `A:crates/asc-core/src/policy.rs`, `A:crates/asc-serve/src/queue/mod.rs`; `policyform_test.go`, `asc_policy_strategy_test.go` | M |
| RepoSettingsRules | How stacks land: keep every change / squash into one change | done (the Rules card shows "How stacks land": Keep every change, or Squash into one change when the strategy in force is `squash`; the policy form and the organisation merge defaults offer "Squash into one change" next to the three strategies that keep every change; asc lands a squash of trunk as one changeset with the tree of the last change, the author of the first and every message, and needs every pushed change approved) | `H:routers/web/repo/setting/asc_rules.go`, `H:templates/repo/settings/branches.tmpl`, `H:services/asctrust/policyform.go`, `H:services/asctrust/orgsettings.go`, `A:crates/asc-serve/src/land.rs`; `asc_policy_strategy_test.go` | M |
| RepoSettingsRules | Empty state "Nothing is protected" with Protect main | done | `H:templates/repo/settings/rules_empty.tmpl` on `branches.tmpl` when there is no rule: the sentence and "Protect <default branch>", which opens a new rule for it (not on an empty repository); `asc_rule_link_test.go` | S |

## RepoSettingsWebhooks

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| RepoSettingsWebhooks | Hook list with URL, kind, events, last delivery, Active switch, Failing banner | done | `H:templates/shared/webhook/list.tmpl` (address title with the scheme dimmed, kind chip Generic/Chat/Service, event chips, last delivery "200 OK, 2 minutes ago" or "Failing: the last 3 deliveries returned 502", Active switch that pauses at once via `WebhookSetActive`, Edit; Add webhook in the card head, full width on a phone), `H:services/webhook/rows.go` (`Kind`, `FailStreak`, `StatusLine`), `H:models/webhook/hooktask.go` `FailingStreaks`, `webhook.go` `SetWebhookActive`. Tests: `asc_webhook_list_test.go`, `services/webhook/rows_test.go`, `models/webhook/asc_streak_test.go` | - |
| RepoSettingsWebhooks | One hook with tabs Settings / Recent deliveries (12), delivery detail with Request / Response, Redeliver | done | `H:templates/webhook/hook.tmpl` (address, Failing chip, Delete, Settings / Recent deliveries tabs with the count), `H:templates/webhook/deliveries.tmpl` (paged list of links with status chip, event, id and time taken; the selected delivery's Request or Response numbered line by line; Redeliver), `H:services/webhook/deliveries.go`, `loadDeliveries` in `routers/web/repo/setting/webhook.go` (tab, page, delivery and part in the query, so it works without script), `hook_task.duration_ms` (migration v18i, timed in `deliver.go`). Tests: `asc_webhook_list_test.go` `TestWebhookEditShowsHook`, `TestWebhookDeliveries`; `services/webhook/deliveries_test.go`; `tests/integration/repo_settings_hook_test.go` | - |
| RepoSettingsWebhooks | Hook kinds Generic, Chat, ASC agent (starts an agent run on an event) | done | `H:modules/webhook/type.go` `ASCAGENT`; `H:services/webhook/asc_agent.go` (an agent hook sends no request: each event starts `Agents.CreateRun` as the person who saved it, for its branch, with its task and the event as data; agent run events and bot senders start no run; the delivery records the run or the reason); Settings tab Agent / Branch / Task (`H:templates/webhook/settings.tmpl`); Chat is Slack format, Discord for its hosts; a template copy leaves agent hooks out (`H:services/repository/hooks.go`). Tests: `services/webhook/asc_agent_test.go`, `asc_webhook_settings_test.go`, `services/repository/hooks_test.go` | - |
| RepoSettingsWebhooks | ASC events: Changes (Landed, Opened, Review requested), Agents (Run started, Run finished), Releases (Published), Policy (Changed) | done | `H:modules/webhook/type.go` `AscHookEvents`; `H:services/webhook/asc_events.go` (changes from the PR notifier, releases, `AscRun` and `AscPolicy` from `H:routers/private/asc_hook_event.go`); Send me in four groups (`H:services/webhook/send_me.go`) | - |
| RepoSettingsWebhooks | Payload URL https-only with certificate check; loopback http only with local option | done | `H:models/webhook/payload_url.go` `CheckPayloadURL` (https always; http only for localhost, 127.0.0.0/8 or ::1 when `[webhook] ALLOW_LOCAL_HTTP` is on and `ALLOWED_HOST_LIST` allows loopback; private addresses need https), enforced on create and update in the model, the API and the web form; `H:services/webhook/deliver.go` `skipTLSVerify` (an ASC hub always checks the certificate); `H:templates/webhook/payload_url_help.tmpl` (hint under the field, refusal as a role=alert error under it with the typed address kept). Tests: `models/webhook/payload_url_test.go`, `services/webhook/payload_url_test.go`, `services/webhook/asc_tls_test.go`, `asc_webhook_list_test.go` `TestWebhookCreateRefusesHTTP`, `TestWebhookFormHintsHTTPS` | - |
| RepoSettingsWebhooks | Secret shown masked with Replace, never shown again | done | `H:templates/webhook/secret.tmpl`: the stored secret is never rendered, an empty field keeps it, Regenerate secret shows a new one once (`WebhookRegenerateSecret`). Tests: `routers/web/repo/setting/asc_webhook_list_test.go` | - |
| RepoSettingsWebhooks | Send test delivery | done | `H:templates/webhook/test_delivery.tmpl` at the foot of the Settings tab (and the empty Recent deliveries), reloads on Recent deliveries; off while the hook is paused | - |
| RepoSettingsWebhooks | Failing-hook alert banner role=alert and menu count "Webhooks 3" | done | menu count (`asc_menu_status.go`); a failed delivery's detail ends in a role=alert banner, "The receiver answered 502. Nothing was retried automatically; use Redeliver once the receiver is back." (`H:templates/webhook/deliveries.tmpl`). Test: `TestWebhookDeliveries` | - |
| RepoSettingsWebhooks | Add webhook dialog with http address refused | done | `H:templates/repo/settings/webhook/add_dialog.tmpl` (Kind cards Generic / Chat / ASC agent, the last only where asc serves the repository; Payload URL, or Agent and Task; Cancel, Add webhook off while a refused http address shows its message), `H:web_src/js/features/asc-webhook-add.js`, `H:routers/web/repo/setting/asc_webhook_add.go` `WebhookAdd` (`POST {hooks}/add` for repository, organization and user; a generic hook gets a secret shown once). Tests: `asc-webhook-add.test.js`, `asc_webhook_add_test.go`, integration `TestWebhookAddDialog` | - |
| RepoSettingsWebhooks | Empty state | done | `H:templates/repo/settings/webhook/base_list.tmpl` (shared/ds/empty) | - |

## RepoSettingsSecrets

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| RepoSettingsSecrets | Secrets and variables on one page | done (seen on a live hub (ui32): one menu entry with two tabs, Secrets and Variables, each its own list, not one scrolling page; axe clean) | `H:templates/shared/secrets/add_list.tmpl` and `shared/variables/variable_list.tmpl` live on `/settings/actions/secrets` and `/settings/actions/variables` | M |
| RepoSettingsSecrets | Scope chips per secret (Checks, Agent runs, Releases) and "Agents only receive it when Agent runs is ticked" | needs-both | `Workflows.SetSecret` has name and value only (`A:proto/asc/v1/workflows.proto`); add scopes to the asc secret and to the Forgejo secret model (`H:models/secret`), enforce in agent run env | L |
| RepoSettingsSecrets | "Updated by Maya Okafor, 12 d ago" | done (the secret records who last set its value and when, from the web page and the API; each row shows "Updated by X, <time>", or "Updated <time>" for a secret set before this was recorded; migration v18q backfills the time; TestActionsSecretsPageShowsWhoUpdated, TestActionsSecretsAPIRecordsWhoUpdated, TestMarkUpdatedAndLastSet) | `H:models/secret/secret.go`, `services/secrets/secrets.go`, `routers/web/shared/secrets/secrets.go`, `templates/shared/secrets/add_list.tmpl` | S |
| RepoSettingsSecrets | Update value dialog with name locked; value never shown | done (the dialog's name is read-only and the server ignores a sent name; an edit needs a new value; TestActionsSecretsEditSecretKeepsItsName, TestActionsSecretsPage) | `H:routers/web/repo/setting/secrets.go`, `add_list.tmpl` | S |
| RepoSettingsSecrets | Delete confirmation that lists which scopes lose it | done (the confirmation names the secret and says the runs that read it lose it; each scope is named once secrets have scopes, row "Scope chips") | `add_list.tmpl` confirm | S |
| RepoSettingsSecrets | Name rule (capitals, digits, underscores) validation in dialog | done | Forgejo secret name validation | S |
| RepoSettingsSecrets | Variables table with "Available to" scope and updated-by | needs-both | same scope work as secrets | M |
| RepoSettingsSecrets | Secret-redaction in change logs ("the log redacts it") | done | `A:crates/asc-serve/src/ci/rpc.rs` masks every value of the repository's secrets and recognised credentials with `***` on `AppendJobLog` and `JobLog`, with stored context around each window, and refuses both while the vault is locked; the secrets page says so (`templates/shared/secrets/add_list.tmpl`, `secrets.log_redacts`) | M |
| RepoSettingsSecrets | Saved toast and empty state | done (the saved flash is the settings toast (layout_head data-ds-flash-toast); empty state with key icon, text and Add secret) | `add_list.tmpl` | S |

## RepoSettingsAI

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| RepoSettingsAI | Provider list (Anthropic, OpenAI, own endpoint) with key hint, added by, verified time, Replace, Remove; stored encrypted | done | `H:templates/shared/asc/byo_ai*.tmpl`, `H:services/ascbyo/aikeys.go`, `AiProviders` RPCs | S |
| RepoSettingsAI | Replace key dialog that verifies before swapping and shows "Anthropic rejected this key (401)" | needs-both | `UpdateKey` + `TestKey` exist; asc must keep the old key until the new one passes (atomic verify in `UpdateAiKey`), hub dialog | M |
| RepoSettingsAI | "What uses AI" table: Agent runs, Review summaries, Meaning search, each with its provider and monthly cap and spend meter (amber 80%, red 100%) | needs-both | asc caps are per key (`monthly_cap_micro_usd`), not per feature; add feature routing (`AiFeature` -> key + cap) in `A:proto/asc/v1/aiproviders.proto` and the usage grouping; hub table | L |
| RepoSettingsAI | Cap reached: feature pauses with "until 1 Nov or you raise it" | done | `ListAiFeatures` returns `paused`, `resumes_at` and `paused_by`; `services/ascbyo/view.go` builds the pause line, `templates/shared/asc/byo_ai_features.tmpl` shows it with `ai.feature.*.paused` ("Cap reached. Agent runs are paused until %s or you raise it.") and `.paused_by`; tests in `routers/web/shared/byoweb/aifeatures_test.go` | M |
| RepoSettingsAI | No keys: "AI features are off" empty state | done | `ascbyo.ai.empty` | S |
| RepoSettingsAI | Repository-level page (keys are owner scope in asc, page lives under the repository menu) | done | `H:templates/shared/asc/byo_ai_repo.tmpl` under the repository menu (`/settings/asc/ai`) with the lead "Your keys, your providers, your caps."; keys of the repository are made there, and the owner's keys are linked, not listed, since agents use a repository key first and then the owner's; `TestRepoAIKeysPage` | S |

## RepoSettingsStorage

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| RepoSettingsStorage | Choose ASC Hub storage or own S3-compatible bucket (endpoint https only, region, bucket, prefix, access key, secret with Replace, path-style) | done | `H:templates/shared/asc/byo_storage.tmpl`, `H:services/ascbyo/storage.go`, `Storage` RPCs | S |
| RepoSettingsStorage | Test connection with four steps (reachable, can write, can read, versioning on) and AccessDenied explanation naming the needed permissions | done | `VerifyStorageTarget` reports resolve, connect (TLS, with its time), put, get, delete and an advisory `versioning` step (a signed `GET ?versioning`; off or suspended is a warning, never a failure), and a refused step names the permission in `needs` (`s3:PutObject`, `s3:GetObject`, `s3:DeleteObject`, `s3:GetBucketVersioning`) in `A:crates/asc-serve/src/storage/live.rs`; the hub shows the warning and `storage.detail.needs` in `templates/shared/asc/byo_storage_target.tmpl`, keeping only the permissions it knows (`services/ascbyo/storage.go`); tests in `services/ascbyo/service_test.go` and `routers/web/shared/byoweb/byoweb_test.go` | M |
| RepoSettingsStorage | Usage: 38.2 GB split into changes, large files, search indexes plus objects and "read this month" | done | `A:proto/asc/v1/storage.proto` `Storage.GetRepoUsage` (last scan by kind, bytes read this month; scanned in the background, `A:crates/asc-serve/src/storage/usage.rs`); `H:services/ascbyo/storage_usage.go`, the Usage card of `H:templates/shared/asc/byo_storage_repo.tmpl` with Count again (`POST .../asc/storage/usage/refresh`); tests in `services/ascbyo/service_test.go` and `routers/web/shared/byoweb/byoweb_test.go` | M |
| RepoSettingsStorage | Moving data: 64% progress, Pause, Cancel and keep current | needs-both | `StartMigration/GetMigration/CancelMigration` exist; no Pause RPC; hub progress UI | M |
| RepoSettingsStorage | Managed storage selected: "38.2 GB of 100 GB on your plan", Use my own bucket | needs-both | plan quota from `H:models/quota`; usage as above | M |
| RepoSettingsStorage | Local-option and http refused message | done | `H:services/ascbyo/validate.go` | S |
| RepoSettingsStorage | Bucket settings changed bar with Discard / Save and test | done | `H:templates/shared/asc/byo_storage_target.tmpl` savebar; `StorageUpdate` saves a changed target, tests it and shows both results | S |
| RepoSettingsStorage | Meaning-search index enabling link ("An admin can enable the index in storage settings") | needs-both | no index toggle in the storage page or `CodeIntel.IndexStatus` surface | M |

## Mobile twins: behaviour that differs from the desktop board

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Mobile-Repo | Stack cards replacing the table; filter chips that snap; pull to refresh; sticky Clone and New stack | done | below 640 px `stack_row.tmpl` rows wrap into cards with the rail under the title; the `rp-stack-filter` chips scroll and snap (`repo-home.css`); pull to refresh by `data-asc-pull-refresh` and `asc-pull-refresh.js`; the `rp-sticky-m` bar holds Clone and New stack (New pull request in a git repository), which opens the compare page; tested by `TestAscRepoStickyNewStack` | - |
| Mobile-Code | Avatar column for blame, tap opens the change sheet (swipe down closes), Files sheet, long-press range select with Share | done | `blame.tmpl` + `asc-blame-sheet.js` (row 103); drag-down close in `asc-sheet.js`; the Files sheet is the `rp-tree` panel | M |
| Mobile-History | Rail-and-node list, chips open a bottom sheet, 44 px older-changes button | done | rows 130, 140 and 142 | M |
| Mobile-Search | Sticky search, Filters bottom sheet, cards with reason first | done | as the Search mobile row; the reason line on each card comes with the Search "Why it matched" row (needs-asc-backend) | M |
| Mobile-Releases | New release as a bottom sheet; channel chips | done (2026-11-05: the New release dialog is a bottom sheet below 1024 px; the channel chips scroll in one row, `rp-pills`) | see Releases rows | M |
| Mobile-NewRepo | Name check after 300 ms, Start from keeps typed values, sticky primary | done (the phone form is the desktop form: `repo-new.js` checks the name 300 ms after the last key and keeps the typed values in the Start from links at every width; below 1024 px `.ds-sticky-actions` keeps Create above the bottom navigation and the primary button fills the row below 768 px; tests as the NewRepo rows) | `H:templates/repo/create.tmpl`, `web_src/js/features/repo-new.js`, `web_src/css/asc-ds/layout.css`, `asc-pages/repo-new.css` | M |
| Mobile-RepoSettings* | Detail pages with back arrow and same title, one save bar | done | `ds-settings-nav` shell with `snav_bar` | S |
| Mobile-Changeset, Compare, Refs, Dependencies, Insights, GitNote, Stacks | Same behaviour as the desktop boards | done | covered by the rows of those boards; the twin adds no behaviour beyond 44 px targets | S |

## Counts

Total rows: 263.

| Status | Rows |
| --- | --- |
| done | 250 |
| partial | 0 |
| ui-only-missing | 0 |
| needs-hub-backend | 0 |
| needs-asc-backend | 0 |
| needs-both | 13 |
| total | 263 |

Audit of 2026-10-30: the counts above were recomputed from the table rows (the earlier header counts were not updated as rows were marked). Rows that were read in the tree and corrected carry an Audit note in their Where cell. Per-board counts and the verified status of every board are in `docs/asc/STATUS.md`; the per-board count table below is not maintained.

| Board | done | ui-only-missing | needs-hub-backend | needs-asc-backend | needs-both |
| --- | --- | --- | --- | --- | --- |
| Repo | 28 | 0 | 0 | 0 | 0 |
| NewRepo | 13 | 0 | 0 | 0 | 1 |
| Code | 20 | 0 | 0 | 0 | 0 |
| Changeset | 13 | 0 | 0 | 0 | 1 |
| History | 15 | 0 | 0 | 0 | 0 |
| Refs | 9 | 0 | 0 | 0 | 0 |
| Compare | 6 | 0 | 0 | 0 | 0 |
| Search | 20 | 0 | 0 | 0 | 0 |
| Releases | 14 | 0 | 0 | 0 | 0 |
| Dependencies | 13 | 0 | 0 | 1 | 0 |
| Insights | 7 | 0 | 0 | 0 | 0 |
| InsightsPeople | 5 | 0 | 0 | 0 | 0 |
| GitNote | 4 | 0 | 0 | 0 | 0 |
| Stacks | 13 | 0 | 0 | 0 | 0 |
| RepoSettingsMenu | 2 | 0 | 0 | 0 | 0 |
| RepoSettingsGeneral | 9 | 0 | 0 | 0 | 0 |
| RepoSettingsAccess | 7 | 0 | 0 | 0 | 1 |
| RepoSettingsPolicy | 10 | 0 | 0 | 0 | 2 |
| RepoSettingsRules | 7 | 0 | 0 | 0 | 1 |
| RepoSettingsWebhooks | 10 | 0 | 0 | 0 | 0 |
| RepoSettingsSecrets | 6 | 0 | 0 | 1 | 2 |
| RepoSettingsAI | 4 | 0 | 0 | 0 | 2 |
| RepoSettingsStorage | 4 | 0 | 0 | 1 | 3 |
| Mobile-Repo | 1 | 0 | 0 | 0 | 0 |
| Mobile-Code | 1 | 0 | 0 | 0 | 0 |
| Mobile-History | 1 | 0 | 0 | 0 | 0 |
| Mobile-Search | 1 | 0 | 0 | 0 | 0 |
| Mobile-Releases | 1 | 0 | 0 | 0 | 0 |
| Mobile-NewRepo | 1 | 0 | 0 | 0 | 0 |
| Mobile-RepoSettings* | 1 | 0 | 0 | 0 | 0 |
| Mobile-Changeset, Compare, Refs, Dependencies, Insights, GitNote, Stacks | 1 | 0 | 0 | 0 | 0 |

Open-size totals of the rows that are not done: S 73, M 83, L 30.
