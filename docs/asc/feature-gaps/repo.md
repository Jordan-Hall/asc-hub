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
| Repo | "Who writes the code" card: 46% people / 54% agents, "100% reviewed by a human", median review time | needs-both | asc: a landed-change statistics RPC (author kind, review latency); hub: card on `asc_about.tmpl` | L |
| Repo | Policy card: Valid / invalid chip, rule summary, link to `.asc/policy.toml` | done | `Policy.GetSource` / `Repo.GetPolicy` already called by `H:routers/web/repo/setting/asc_policy.go`; add the call to the home handler and a card in `asc_about.tmpl` | M |
| Repo | Policy card lines that need new policy keys (budget "20 min and 200k tokens", merge queue on) | needs-asc-backend | `A:crates/asc-core/src/policy.rs` (see Policy rows) | M |
| Repo | Activity card: landed per week over 12 weeks, Contributors list with agent marks | needs-both | same statistics RPC as Insights; hub `asc_about.tmpl` | L |
| Repo | Languages bar with legend and aria-label | done | `H:templates/repo/asc_about.tmpl` | S |
| Repo | Latest release card ("9 changes, 3 by agents. Published 6 days ago") | done (seen on a live hub (ui10): name, chip, change count, age) | `H:templates/repo/asc_about.tmpl` shows name and age only; counts need the release-changes data (see Releases) | S |
| Repo | Loading skeleton that keeps row geometry | ui-only-missing | `H:templates/repo/home_cards.tmpl` (skeleton `rp-sk` exists only for the last-commit column) | S |
| Repo | Error state: "Stacks did not load", request id, Try again | needs-hub-backend | `H:routers/web/repo/view_home_cards.go` (swallows errors), `H:templates/status`, request id from the access log context | S |
| Repo | Empty repository state with `asc init` and `asc push` commands | done | `H:templates/repo/empty.tmpl` | S |
| Repo | Clone popover: asc login and clone, Install asc, Open in IDE, Copy | done | `H:templates/repo/clone_dialog.tmpl` | S |
| Repo | Git compatibility note collapsed and expanded in the clone sheet | done | `H:templates/repo/clone_asc_tabs.tmpl` | S |
| Repo | "Switch version" popover that lists shared refs, stacks and tags with labels | needs-hub-backend | `H:web_src/js/components/RepoBranchTagSelector.vue` and `H:templates/repo/branch_dropdown.tmpl` list branches and tags only; add stacks (`Stacks`/open pulls) and a "Shared ref" label | M |
| Repo | Pull to refresh and sticky Clone / New stack buttons (Mobile-Repo) | ui-only-missing (the Watch, Star and Fork menu is done; pull to refresh remains) | sticky buttons exist (`rp-sticky-m` in `home_dir.tmpl`); pull to refresh is not implemented anywhere in `H:web_src/js` | S |
| Repo | Clone card shown before first clone ("Work on it locally", `asc stack new fix-refund-timeout`) | ui-only-missing | `H:templates/repo/home_dir.tmpl` / `clone_dialog.tmpl` | S |

## NewRepo

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| NewRepo | Page shell, owner select, name, description, auto-init options (Forgejo flow in design-system markup) | done | `H:templates/repo/create*.tmpl` | S |
| NewRepo | Name availability check as you type (300 ms), tick and inline "acme/ledger already exists" | needs-hub-backend | new JSON route `GET /repo/check_name` in `H:routers/web/repo/repo.go` plus script `H:web_src/js/features/repo-new.js` (not present); the existing form reports the error only after submit | M |
| NewRepo | "Start from" segment: Empty / Template / Import that keeps typed values | ui-only-missing | `H:templates/repo/create.tmpl` stacks the template section and the migrate page is separate (`H:templates/repo/migrate/*`); merge into one page | M |
| NewRepo | Visibility as three radio cards: Private, Internal, Public | needs-hub-backend | `H:models/repo/repo.go` has `IsPrivate` only; asc already returns `visibility` "private / internal / public" in `RepoConfig`; add an internal level (signed-in users read) in models, permission checks and the form | L |
| NewRepo | Working mode radio: Branchless (recommended) / With branches at creation | needs-hub-backend | `H:routers/web/repo/setting/asc_branchless.go` exists in settings only; add to `create.tmpl` and `forms.CreateRepoForm` | S |
| NewRepo | Add AGENTS.md switch | needs-hub-backend | `H:services/repository` init files list (add template file) | S |
| NewRepo | Add a policy file switch (`.asc/policy.toml`, 2 approvals, picked checks) | needs-hub-backend | `H:services/repository` init; uses `Policy.Propose`/first commit; depends on policy schema | M |
| NewRepo | License choice | done | `H:templates/repo/create_init.tmpl` | S |
| NewRepo | Preview card ("acme / payments-v2, Private, Policy on") and "Then, from your terminal" commands | ui-only-missing | `H:templates/repo/create.tmpl` plus script | S |
| NewRepo | Quota line "41 of 100 used on the Team plan" and quota-reached state | needs-hub-backend | `H:models/quota` is wired to size limits; repository count limit exists (`MaxCreationLimit` banner) but no plan name or usage line | S |
| NewRepo | Creating state ("Creating acme/payments-v2 and its first policy...") | ui-only-missing | button disabled and busy text in script | S |
| NewRepo | Import running: progress "3,412 of 9,880 history entries", Run in background, Cancel | needs-both | hub `H:web_src/js/features/repo-migration.js` polls Forgejo migration tasks; asc needs a history-to-changes import progress source (`Repo.Ingest`/`ImportGitPush` have no progress stream) | L |
| NewRepo | "Signed with your key mo_41c9" line | needs-hub-backend | `H:services/ascidentity` signing-key lookup, template line | S |
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
| Code | Shortcuts `b` toggle blame, `y` permalink, `l` go to line, `e` propose change, `t` find file, `[` collapse tree | ui-only-missing | none bound in `H:web_src/js`; add to a repo keymap module | M |
| Code | File tree with aria-current and "N files touched by open stacks" badges per file | needs-hub-backend | `H:routers/web/repo/view_asc_tree.go` builds the tree; add per-path badge from open pulls' changed files (`PathChange` via `Repo.Compare` or pull files table) | M |
| Code | Tree collapse with `[` and Files sheet on mobile | done | `rp-tree`, `rp-files-btn` in `code_view.tmpl` | S |
| Code | Outline panel (struct / impl / fn with line numbers) | done (seen on a live hub (ui10): needs [codeintel] enabled on asc serve) | `CodeIntel.Symbols` op=outline is served at `GET /{owner}/{repo}/asc/symbols?op=outline&path=` by `H:routers/web/repo/asc_symbols.go`, but `H:templates/repo/code_view.tmpl` right rail only has `file_info`; embed outline as htmx fragment | M |
| Code | "Used in / 4 references" code-intelligence panel on the file | done (seen on a live hub (ui10): needs [codeintel] enabled on asc serve) | same handler (`op=def`), or `CrossSearch.SymbolPanel` via `H:services/ascsearch/panel.go`; add fragment to right rail | M |
| Code | "Open changes touching this file" card (stack, change 3 of 4, lines changed) | done (seen on a live hub (ui10)) | open pulls by changed path, line ranges from diff; `H:services/pull` | M |
| Code | Shortcuts legend card | done | `code_view.tmpl` right rail | S |
| Code | Propose change bar ("Changes go into a new change on top of main"), Start a new change / Add to my stack | needs-hub-backend | bar exists (`rp-editnote`) and links to the Forgejo editor; the editor commits to a branch/PR rather than creating a change or adding to the user's stack | L |
| Code | Directory view columns: Last change, In open stack chip, Author, Size, Updated | done (seen on a live hub (ui16): author, size, last change and updated; the open-stack chip is not drawn) | `H:templates/repo/view_list.tmpl` has name, message, age only; author and size from `Repo.LastCommits`/`ReadTree`, open-stack chip as in tree badges | M |
| Code | Loading state (lines stream in) | ui-only-missing | skeleton in `view_file.tmpl` | S |
| Code | Not found at this version ("It was added in change mzu2d. You are looking at main@8d1c7e0.", Go to main, File history) | needs-hub-backend | `H:routers/web/repo/view.go` 404 path, `Repo.Log` for the introducing change | M |
| Code | Binary preview card | done | `view_file.tmpl` (image/binary branches) | S |
| Code | Long-press line number to select and Share (mobile), blame as avatar column with change sheet | ui-only-missing | `H:templates/repo/blame.tmpl` has the desktop columns only; add mobile column and sheet reusing the hover endpoint | M |

## Changeset

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Changeset | Page shell with breadcrumb History / id, title, Landed chip, Browse files, Copy id | done | `H:templates/repo/commit_page.tmpl`, `commit_header.tmpl` | S |
| Changeset | "Why this exists" (intent) block | needs-hub-backend | derive from the message body of the change (`Repo.ReadChangeset` header); `H:templates/repo/commit_header.tmpl` | S |
| Changeset | "Where to look" and "Risk" lines | needs-both | asc has no such field in `ChangeInfo`/`LogEntry`; produce them through the "Review summaries" AI feature (`A:proto` new RPC or trailer) and show in hub | L |
| Changeset | Facts strip: Size, Files, Checks 14 of 14, Risk | done (seen on a live hub (ui10): Size, Files, Checks; Risk comes with the brief) | stats from `Repo.Diff`, checks from commit statuses; `commit_header.tmpl` | M |
| Changeset | Stack position bar: previous / next change, "Change 2 in retry-with-backoff" | done (seen on a live hub (ui10): Prev, Next and "Change n in stack of m") | `repo.walk_commits` only walks parent commits; use `BuildAscStackPanel` for the pull of the commit, `commit_header.tmpl` | M |
| Changeset | Files grouped by meaning (Behaviour, Tests, Generated) with counts, collapsible groups, generated files folded | needs-hub-backend | `Repo.Classify` is wrapped in `H:modules/git/ascbackend/classify.go` and rendered as a pill (`diff/asc_class_badge.tmpl`) on pull files, not on the commit page; group the diff box by class in `H:templates/repo/diff/box.tmpl` | M |
| Changeset | Collapsed file buttons with aria-expanded, +/- signs | done | `diff/box.tmpl` | S |
| Changeset | Review block "2 of 2" with approvers and dates | done (seen on a live hub (ui10)) | reviews of the landed pull (`H:models/issues/review.go`), new card on commit page | M |
| Changeset | Checks list with durations | done (seen on a live hub (ui16): a Checks card lists each check with its state; durations are not recorded by the hub) | `commit_statuses.tmpl` shows a summary; expand to list with durations from action runs | S |
| Changeset | "Where it is now": On main, In release v2.18.0, Stack and position | done (seen on a live hub (ui10)) | tags containing the commit (`LoadBranchesAndTags` route exists), pull lookup | M |
| Changeset | Revert as a new change (review required) | done (seen on a live hub (ui16): the Undo card on the change page posts to pulls/N/asc/revert) | commit page uses the Forgejo cherry-pick revert modal; `POST /{owner}/{repo}/pulls/{index}/asc/revert` (`AscStackRevert`, `Stacks.RevertLanded`) exists only for pulls; wire the commit page to it and show "Revert opened as change hv3ps" | M |
| Changeset | Undo from the operation log while recent (replaces Revert) | ui-only-missing | `/asc/ops/undo` exists (`H:routers/web/repo/asc_ops.go`); link and the "too old" sentence on the commit page | S |
| Changeset | Large change state: "412 files changed, showing the 20 that matter most", Show 392 more | needs-hub-backend | `Repo.Classify` ranking, `diff/box.tmpl` | M |
| Changeset | Loading diff state | ui-only-missing | skeleton in `diff/box.tmpl` | S |

## History

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| History | List view with commit rows, copy hash, view at point, search by message and author, "Open operation log" card | done | `H:templates/repo/commits.tmpl`, `commits_list.tmpl` | S |
| History | Lane graph from `Repo.CommitGraph` (colour, lanes, refs, review chip) | done | `H:routers/web/repo/asc_graph.go`, `asc_graph.tmpl` (separate page, Forgejo-style form) | M |
| History | Graph and List as two views of one page with the same rows; graph beside the rows | ui-only-missing | merge `asc_graph.tmpl` rail into `commits.tmpl`; the segment links to two pages today | M |
| History | Node state shape plus chip (Needs review, Queued, Agent writing); agent vs person nodes; stack brackets; "A stack that landed together" | needs-hub-backend | map graph rows to pulls and queue state (`H:routers/web/repo/asc_queue.go`, `pull_asc_stack.go`), draw shapes in `asc_graph.tmpl` | L |
| History | "Open on top of main" section listing open changes with stack position, "Waiting on Priya Raman", agent run status | needs-hub-backend | open pulls + reviewers + agent runs; `commits.tmpl` | L |
| History | Comfy / Compact density switch | done | `commits.tmpl` seg + CSS + localStorage | S |
| History | Filter bar: Everyone / People / Agents, Path, Stack, date range, in the URL, sticky | needs-hub-backend | author kind needs agent identity; path and date filters exist in Forgejo `RefCommits` partly; `commits.tmpl`, `routers/web/repo/commit.go` | M |
| History | Export | done (seen on a live hub (ui18): Export CSV on the history page, newest first, at most 1000 rows, formula cells quoted) | new route (CSV/JSON of the filtered list) in `H:routers/web/repo/commit.go` | S |
| History | Keyboard: `j`/`k` move, Enter opens, `c` copies id | done | none bound; list-nav helper in `H:web_src/js` | S |
| History | Row hover actions (copy id, browse files) and hover lift | done | copy hash and view-at-point buttons in `commits_list.tmpl` | S |
| History | "Landing rate, last 12 weeks" card with this-week delta | needs-both | statistics RPC (see Insights) | M |
| History | Graph key legend | done | `asc_graph.tmpl` | S |
| History | "Show 25 older changes" on-demand load (44 px button on mobile) | done | pager/`AscGraphNextURL` | S |
| History | States: loading skeleton, no results with explanation and Clear filters, partial-data banner with Retry | ui-only-missing | `commits.tmpl`; partial banner needs index lag signal from `Repo.CommitGraph` (hub `AscGraphError` path) | M |
| History | Mobile rail-and-node list, chips open a bottom sheet for Path, Stack, date | ui-only-missing | `commits.tmpl`, sheet component | M |

## Refs

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Refs | Branches page with filter, default branch card, ahead/behind, Protected chip, Compare | done | `H:templates/repo/branch/list.tmpl`, `row.tmpl` | S |
| Refs | One page with Shared refs, Stacks in progress and Tags sections | done (seen on a live hub (ui10)) | `list.tmpl` lists Forgejo branches and links to `/tags`; add stacks (open pulls with ahead count) and release-linked tags, `routers/web/repo/branch.go` | M |
| Refs | Filter pills All / Shared 3 / Stacks 3 / Tags 14 | ui-only-missing | `list.tmpl` pills show Branches and Tags only | S |
| Refs | Branchless wording ("only three shared refs"), `RepoConfig.shared_refs` | needs-hub-backend | read `Repo.RepoConfig` shared refs; branch list for asc repos comes from Forgejo's mirror | M |
| Refs | Stack cleanup note ("clean themselves up 7 days after the last change lands") | needs-asc-backend | the setting does not exist (see Rules) | S |
| Refs | Tags section with release title, change count, author, date | ui-only-missing | `H:templates/repo/tag/list.tmpl` is a separate page | M |
| Refs | Row More menu names its target; no delete on protected refs | done | `branch/row.tmpl` | S |
| Refs | State: nothing matches with "Search stacks" link | done | `list.tmpl` empty state has no stack search action | S |
| Refs | State: delete protected ref refused with "Open rule" | ui-only-missing | `H:routers/web/repo/branch.go` flash message and link to `settings/branches` | S |

## Compare

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Compare | From / To, swap, "only what the stack adds" checkbox, ahead / behind chips, common ancestor | done | `H:templates/repo/asc_compare.tmpl`, `H:routers/web/repo/asc_compare.go` | S |
| Compare | From / To as labelled selects listing shared refs, stacks and tags (text inputs today) | done | `asc_compare.tmpl`; options from refs/pulls | S |
| Compare | Per-change and per-file +/- counts and avatars | done (seen on a live hub (ui10)) | `AscCompareCommit`/`AscCompareFile` carry no stats; `Repo.Compare` returns `PathChange` and `LogEntry`; add numstat (`Repo.Diff`) | M |
| Compare | Same version state ("Both sides are main@a41f9c2") | done | `asc_compare.tmpl` shows "Nothing to compare" without the ref | S |
| Compare | Unknown ref with "Did you mean retry-with-backoff?" | needs-hub-backend | resolve error in `asc_compare.go`; suggest nearest ref name | S |
| Compare | Open any change or file from the lists, both sides kept in the address | done | links and query string | S |

## Search

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Search | Text, Symbol and Meaning modes as radio group (lexical / symbol / semantic) | done | `H:templates/repo/asc_search.tmpl`, `H:routers/web/repo/asc_search.go`, `H:services/ascsearch` | S |
| Search | Scope radios: this version, change or stack, all branches, all repositories | done | `H:routers/web/repo/asc_search_scope.go` | S |
| Search | Cross-repository results grouped by repository, URL `/-/asc/search` | needs-hub-backend | scope "repos" sends to the explore search page (`H:routers/web/explore/asc_search.go`, `templates/explore/asc_search.tmpl`); route in the board is `/-/asc/search`; REST `GET /api/v1/asc/search` exists | M |
| Search | Facets: Repository (21 / 9 / 5, "11 more"), Language, Path, Branch or commit, "Only files the stack changes" | ui-only-missing | Language and Path facets and the stack-diff checkbox exist; add Repository and Branch facets from `CrossSearch` result group keys in `ascsearch/types.go` | M |
| Search | Result card: path, language, branch list, snippet with highlights | done | `asc_search.tmpl` | S |
| Search | Relevance percentage with a bar (grows over 300 ms) | done | `Chunk` score is shown as a raw number pill (`$c.Score`); convert to percent bar in `asc_search.tmpl` | S |
| Search | "Why it matched" reason sentence per result | needs-asc-backend | `A:proto/asc/v1/codeintel.proto` `Chunk` has no reason; add a reason from the retrieval source or a short model-written line; show in `asc_search.tmpl` | L |
| Search | Symbol chips under a result ("RetryPolicy::delay", "14 references elsewhere", "changed 3 h ago") | ui-only-missing | symbol and reference counts from `CodeIntel.Symbols`; last change from `Repo.LastCommits` | M |
| Search | "Symbols matched: RetryPolicy, retry" line | ui-only-missing | `asc_search.tmpl` header line from `CodeIntel.Symbols` | S |
| Search | Code intelligence side panel (definition, signature, Go to definition, references, Called from, cross-repo callers) | ui-only-missing | `CrossSearch.SymbolPanel` (hub client `H:services/ascsearch/panel.go`, REST only) and `GET /asc/symbols`; no template renders it; add right-column fragment and update on symbol select | L |
| Search | Coverage line ("Indexed at e91b0d4, 99.2% of files; 4 files too large, listed in settings") | done | `asc_search.tmpl` coverage block (percent and lag); link to settings is missing | S |
| Search | Sort: best match, pagination "Showing 4 of 38", page links | ui-only-missing | `asc_search.tmpl` has a Truncated banner only | M |
| Search | "The other two kinds of search" teaser (Text and Symbol hits for the same query) | ui-only-missing | run two cheap queries in `asc_search.go`; fragment | M |
| Search | First-visit state with example queries | done (seen on a live hub (ui28): three example searches that open Meaning search) | `asc_search.tmpl` | S |
| Search | No-results state with "Search all branches / All repositories" actions | done | `asc_search.tmpl` shows a plain banner | S |
| Search | Not available (503) for meaning search with "Use Text search" and "Open storage settings" | done | `s_err_503` banner exists; add actions | S |
| Search | Rate limited (429) keeps the query | done | `s_err_429` | S |
| Search | Loading state | ui-only-missing | skeleton | S |
| Search | `/` focuses search in the repository | done | `ds-shell.js` | S |
| Search | Mobile: sticky search with mode and scope, Filters bottom sheet, results as cards | ui-only-missing | `asc_search.tmpl` + sheet component | M |

## Releases

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Releases | Timeline of release cards, tag, stable / pre-release / draft chips, Compare, edit, assets list with downloads | done | `H:templates/repo/release/list.tmpl`, `release_tag_header.tmpl` | S |
| Releases | Releases / Tags segment, search filter, feed | done | `release_tag_header.tmpl` | S |
| Releases | Filter pills All 14 / Stable 11 / Pre-release 2 / Draft 1 and Sort (Newest, Most downloaded) | done (seen on a live hub (ui10): pills clicked, sections hide and show) | `release_tag_header.tmpl`; `H:routers/web/repo/release.go` query params | M |
| Releases | Provenance strip ("Built by run #4218 from main@a41f9c2, 14 of 14 checks passed, signed with release key 7F3A...") | needs-hub-backend | `tag/verification_line.tmpl` gives the signature; add the building Actions run and check summary, link release to run (`H:models/actions`) | L |
| Releases | Notes grouped Added / Changed / Fixed, "written from the intent of each change" | needs-hub-backend | generator over landed changes since last tag (`Repo.Log`, `Compare`); category from change class or trailer | L |
| Releases | "N changes in this release" list with author, change id, category | needs-hub-backend | `Repo.Compare(prev tag, tag)` in `release.go`; template block | M |
| Releases | Asset table: size, SHA-256 checksum (copy on click), downloads | needs-hub-backend | `H:models/repo/attachment.go` has no checksum column; compute at upload; template shows name, count, age only | M |
| Releases | Next release summary ("6 changes since v2.18.0", Draft v2.19.0 View) | done (seen on a live hub (ui10)) | `Compare(latest tag, default branch)` | M |
| Releases | "Download the latest" with `asc release download v2.18.0` and per-platform line | ui-only-missing | right rail in `list.tmpl`; the `asc release` CLI command must exist (check `A:crates/asc/src/cli`) | S |
| Releases | Release health: cadence, rollbacks | needs-hub-backend | derive from release dates; rollbacks need a data source | M |
| Releases | Get notified: Watch releases, Atom feed | done | watch menu has releases, `.rss` link in header; Atom naming differs | S |
| Releases | New release dialog: proposes next tag, "Write from the N landed changes", assets drop zone, pre-release, Save draft, Publish | ui-only-missing | `H:templates/repo/release/new.tmpl` is a full page (no dialog, no generated notes, no tag proposal) | L |
| Releases | States: no releases (shows `asc release create v1.0.0`), loading, storage error with Retry | needs-asc-backend | the command does not exist in the CLI (see above); `list.tmpl` empty state has no command, loading skeleton or storage-error banner | S |
| Releases | Mobile: new release as bottom sheet, 44 px downloads | ui-only-missing | `new.tmpl` | M |

## Dependencies

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Dependencies | Views Findings / Updates / Configuration (`?view=`) | done | `H:templates/repo/asc_deps.tmpl`, `H:routers/web/repo/asc_deps.go` | S |
| Dependencies | Scan now, refresh advisory data, last-scan line, lookup-incomplete banner | done | `asc_deps.tmpl` | S |
| Dependencies | Summary tiles: findings by severity bar chart with text table, Outdated 11 (3 with security fix), Packages 214 by ecosystem, Last scan 2 h "Complete, 3 of 3 sources" | ui-only-missing | `DepsService.ListFindings/ListUpdates` give the counts; add tile row to `asc_deps.tmpl` | M |
| Dependencies | Severity chips All / Critical / High / Medium / Low with counts, Show dismissed | done | state tabs and min-severity select | S |
| Dependencies | Finding row expands (aria-expanded): "Why it matters", "How it gets in" text tree, Propose fix | ui-only-missing | rows are flat in `asc_deps.tmpl` (path is one line); render the path as a tree and add per-finding Propose fix posting to `/asc/deps/propose` | M |
| Dependencies | Dismiss with required reason and note, Reopen, "until Dec 1" expiry | done | `AscDepsDismiss`; reasons + note present, expiry date not verified | S |
| Dependencies | Updates table: package, current, latest, kind, security, proposal link (Change kq2nv) or Propose | done | `asc_deps.tmpl` updates view | S |
| Dependencies | Propose updates panel: Security fixes only, Largest jump, Most proposals, 5-stack limit, Preview, Propose | done | `asc_deps.tmpl`, `asc_deps_plan.tmpl` | S |
| Dependencies | Configuration view: ecosystems, schedule ("Weekly, Mondays 06:00 UTC, next run in 4 days"), threshold, grouping, ignored, "Propose a change" to `.asc/deps.toml` | done | `asc_deps.tmpl` (read-only list); "Propose a change" button not verified | S |
| Dependencies | Scan running state with real progress bar ("About 20 seconds left") | needs-asc-backend | `DepsService.Scan` is unary (`A:proto/asc/v1/deps.proto`); add a scan status/progress RPC or stream, hub polling | M |
| Dependencies | No findings state ("Checked 214 packages 2 hours ago against 3 sources") | ui-only-missing | `asc_deps.tmpl` prints a short empty line | S |
| Dependencies | Never scanned state with package count | done | `never_scanned` banner (count not shown) | S |
| Dependencies | Unsupported ecosystem ("2 files are not scanned. tools/build.zig.zon") | done | `unsupported` banner (file names not listed) | S |
| Dependencies | Advisory text plain, https-only links | done | cleaned in `H:services/asctrust` | S |

## Insights and InsightsPeople

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Insights | Pulse and Contributors pages exist (Forgejo activity, git contribution graph) | done | `H:templates/repo/pulse.tmpl`, `contributors.tmpl` (git-level data, not the board) | S |
| Insights | Four tiles: Changes landed (vs previous 30 days), Stacks opened, Median review time, Written by agents | needs-both | asc: statistics over landed changes with author kind (new RPC, e.g. `Stats.Pulse`); hub: reviews and pulls from DB, new handler `H:routers/web/repo/insights.go` | L |
| Insights | Changes landed per day, people vs agents, chart with direct labels, Table toggle | needs-both | same data; chart component in `H:web_src/js/components`; `code-frequency.js` is git commit based | L |
| Insights | Seg controls Everyone / People / Agents and 7 days / 30 days / Year that update chart and tiles together | needs-hub-backend | query params + client update; depends on data above | M |
| Insights | Median review time by week ("Getting faster") and by size of change (Under 50 / 50 to 300 / Over 300) | needs-hub-backend | review events in hub DB (`models/issues/review.go`) | M |
| Insights | Recently landed stacks and "Waiting the longest" list | done (seen on a live hub (ui10)) | pulls + queue + last activity | M |
| Insights | States: new repository, loading, "Index behind" banner | ui-only-missing | `pulse.tmpl` | S |
| InsightsPeople | Contributors table: landed, reviews given, median review, 12-week sparkline, share, role, "Agent run by Priya Raman" | needs-both | agent ownership from `Agents`/`AgentTokens`; counts as above; replaces `contributors.tmpl` | L |
| InsightsPeople | Who writes the code (46/54) with "every agent change approved by a person" sentence | needs-both | same statistics | M |
| InsightsPeople | Review load list ("Agents never review") | needs-hub-backend | review counts per reviewer | S |
| InsightsPeople | Rows open the person or the agent run | ui-only-missing | links in new template | S |
| InsightsPeople | Empty, loading, partial-data states | ui-only-missing | new template | S |

## GitNote

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| GitNote | One-line collapsed disclosure after the asc command, aria-expanded, URL shown only after expand, git clone command with Copy | done | `H:templates/repo/clone_asc_tabs.tmpl`, `clone_dialog.tmpl` | S |
| GitNote | Body copy: "A push arrives as changes and goes through policy... Stacks appear as branches named asc/<stack>" | done | `options/locale/locale_en-US.ini` `repo.clone_with_git_help` differs from the board | S |
| GitNote | Bridge switched off by admin: one sentence "The Git remote is off on this hub. Everything works with asc." and no button | done | `clone_asc_tabs.tmpl` renders nothing when `AscGitAccess` is false | S |
| GitNote | Inline variant in the repository home hero card; also in settings Working mode when branches are on; also in the empty-repository state | ui-only-missing | `home_dir.tmpl`, `settings/asc_branchless.tmpl`, `empty.tmpl` | S |

## Stacks

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Stacks | Area sub-navigation Stacks / Merge queue / Policy gate / Operation log | done | `H:templates/repo/asc_stack_nav.tmpl` is used on queue, gate and ops pages; the `/pulls` list does not include it | S |
| Stacks | Merge queue page with pause and resume | done | `H:routers/web/repo/asc_queue.go`, `asc_queue.tmpl` | S |
| Stacks | Stack row: avatar, title, stack name, author, change count, +/- size, checks, age, status chip (Needs your review, Queued to merge, Checks failing, Agent writing, Waiting on author), reviewers | needs-hub-backend | `/pulls` renders the Forgejo issue list (`H:templates/repo/issue/list.tmpl`, `shared/issuelist.tmpl`); build a stack-grouped list from stacked pulls (`H:services/pull`, `BuildAscStackPanel`) | L |
| Stacks | Rail with segments per change and text alternative "Stack of 4 changes"; legend (Landed, Approved ready, Waiting, Agent writing, Draft) | needs-hub-backend | same data as the Repo progress rail; one shared template | M |
| Stacks | Filters Open / Mine / Agents / Needs me / Closed with counts; sticky filter bar | needs-hub-backend | Needs me = review requested or queue-ready for the viewer; Agents needs an agent flag; `H:routers/web/repo/issue.go` (`Pulls`) | M |
| Stacks | Sort Newest / Needs attention | needs-hub-backend | attention score from status | S |
| Stacks | "Your turn" card with jump to the exact change | needs-hub-backend | pulls awaiting the viewer's review or enqueue | M |
| Stacks | "From your terminal" card (`asc stack list`, `asc stack submit`) | done | template | S |
| Stacks | New stack button | needs-hub-backend | no web create-stack flow; `H:routers/web/repo/pull.go` creates pulls by compare; agents or terminal create stacks | M |
| Stacks | Keyboard `j`/`k` and Enter | done | list-nav helper | S |
| Stacks | States: empty (Nothing is in flight, New stack, Open history), skeleton, error with request id | ui-only-missing (empty state done; skeleton and error with request id remain) | list template | S |
| Stacks | Row hover slide-in, status chip pulses once on change | ui-only-missing (slide-in done; chip pulse on SSE change remains) | CSS + SSE event (`H:services/ascinbox` / `eventsource.sharedworker.js`) | S |
| Stacks | Mobile: 44 px rows, chips scroll | done | list template | S |

## RepoSettingsMenu (mobile) and the shared settings shell

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| RepoSettingsMenu | One left menu with groups Repository (General, Access N, Rules, Policy, Branches and stacks), Automation (Webhooks N, Secrets and variables), Data (Storage, AI keys), Safety (Secret scanning, Audit log) and count badges | ui-only-missing | `H:templates/repo/settings/navbar.tmpl` has General / Code / Integrations / BYO / Policy groups with Forgejo pages (Units, Collaboration, Git hooks, LFS, Actions runners) and no counts | M |
| RepoSettingsMenu | Mobile root list with status per row ("1 failing"), "Find a setting" search over names and descriptions, danger zone entry, back arrow keeping scroll | ui-only-missing | `snav_*` templates have no search or status; new `settings` index partial | M |

## RepoSettingsGeneral

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| RepoSettingsGeneral | Name, description, one save bar with "2 unsaved changes", Discard, Save | done | `H:templates/repo/settings/options.tmpl`, `H:web_src/js/features/repo-settings.js` | S |
| RepoSettingsGeneral | Name rename note "redirects the old address for 90 days" and taken-name error | needs-hub-backend | Forgejo keeps redirects without the 90-day limit; `H:models/repo/redirect.go` expiry | M |
| RepoSettingsGeneral | Topics field in General (chips, up to 20, Add) | done (seen on a live hub (ui28): a comma list saved in place with a spoken answer, up to the hub limit of 20; no chips) | topics are edited in the About card only; add to `options.tmpl` | S |
| RepoSettingsGeneral | Visibility as radio cards (Private / Internal / Public) with confirmation when changing | needs-hub-backend | `options.tmpl` uses a Private checkbox; Internal does not exist in the model (see NewRepo) | L |
| RepoSettingsGeneral | Features switches: Stacks and change review (locked, Required), Agent runs, Dependencies, Releases, Meaning search, Wiki | needs-hub-backend | Forgejo units (`H:models/unit/unit.go`, `settings/units/*`) cover releases and wiki; add unit types or repo flags for agent runs, dependencies and meaning search, and the locked row | M |
| RepoSettingsGeneral | Working mode radio cards and shared refs (`main`, `release/*`), Cloud sync switch | done | `H:templates/repo/settings/asc_branchless.tmpl` (separate page, not inside General) | S |
| RepoSettingsGeneral | Danger zone: Transfer, Archive, Delete with typed full name `acme/payments` and counts ("Removes 1,204 changes, 14 releases and 23 issues") | done (seen on a live hub (ui28): the delete dialog names the changes on the default branch, stacks and pull requests, releases and issues; the 30-day undo is not possible, Forgejo deletes at once) | `options.tmpl` has transfer, archive, delete; typed string is the repo name and the counts are missing | M |
| RepoSettingsGeneral | "An owner can undo the delete from the operation log for 30 days" | done (seen on a live hub (ui30): Delete moves the repository to a 30 day trash (nobody can read it, it is in no list, its name stays taken); the owner, an organisation owner or a site administrator restores it from Recently deleted; a nightly cron task purges it) | soft delete in hub plus restore; asc keeps repo data for the window (`Repo.RepoAdmin`) | L |
| RepoSettingsGeneral | Saving state, saved toast (5 s), unsaved-bar motion | ui-only-missing | `repo-settings.js` | S |

## RepoSettingsAccess

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| RepoSettingsAccess | One list of people, teams and agents with role select, last active, Show All / People / Agents | needs-hub-backend | `H:templates/repo/settings/collaboration.tmpl` has separate collaborator and team sections with Forgejo roles read / write / admin; add a Maintain level and an agent flag and merge | L |
| RepoSettingsAccess | Combined search (people, teams, agents) with role chosen before adding | needs-hub-backend | `collaboration.tmpl` searches users and teams separately | M |
| RepoSettingsAccess | Role select changes access at once with an Undo toast | ui-only-missing | JS handler on `collaboration.go` POST | M |
| RepoSettingsAccess | Agent rows: Agent chip, violet mark, "Limited by policy to src/ and tests/" | needs-hub-backend | policy lookup per agent identity | M |
| RepoSettingsAccess | Remove agent confirm that says the running run stops and open changes stay with the owner | needs-both | `Agents.CancelRun` on removal in `collaboration.go`; confirm dialog text | M |
| RepoSettingsAccess | Invite that already has access ("Priya Raman already has Maintain access") | done (seen on a live hub (ui26): the invite answers "rev already has Write access to this repository.") | `collaboration.go` error text | S |
| RepoSettingsAccess | Deploy keys on the same page: title, fingerprint, read-only or read-write, added by, last used, Add dialog | ui-only-missing | `H:templates/repo/settings/deploy_keys.tmpl` is its own page at `/settings/keys`; the board says both routes render one layout | M |
| RepoSettingsAccess | No deploy keys empty state | done | `settings.no_deploy_keys` | S |

## RepoSettingsPolicy

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| RepoSettingsPolicy | File header: `.asc/policy.toml`, Valid chip, "Last changed by Priya Raman, 5 days ago, in change kz4np" | ui-only-missing | `H:templates/repo/settings/asc_policy.tmpl` has the chip; last-change info via `Repo.Log` path filter | S |
| RepoSettingsPolicy | TOML editor with Check (validate), diagnostics with line, Propose change | done | `H:routers/web/repo/setting/asc_policy.go` (`/check`, `/propose`) | S |
| RepoSettingsPolicy | Seg Form / TOML / Changes (three views of one file) | done (seen on a live hub (ui18): three tabs over one file, arrow keys move; Form fields are approvals, checks, signing, force, merge strategy and queue, agent scope, denied paths and budgets) | `asc_policy.tmpl` has a textarea and a read-only "what it means" table only | L |
| RepoSettingsPolicy | Form: approvals stepper, check chips with Add, authors-may-approve switch, keep-approvals switch | needs-both | form needs a structured parse (`ValidatePolicyResponse.parsed`); keys `author_may_approve`, `keep_approvals_on_restack` do not exist in `A:crates/asc-core/src/policy.rs` | L |
| RepoSettingsPolicy | Agents section: write paths, never-touch paths, budget minutes and tokens, locked "A person approves every agent change" | needs-both | schema has `default_scope`, `denied_paths`, `max_runtime_seconds`, `budgets.max_tokens`; no `human_approval`; UI mapping minutes to seconds | L |
| RepoSettingsPolicy | Merge queue section: use queue, batch size, checks after merge | done (seen on a live hub (ui20): use queue, batch size (1 to 20) and checks after merge, written as [merge] queue, batch and post_merge_checks) | `[merge] queue` exists; `batch` and `post_merge_checks` do not (`A:crates/asc-serve` merge queue, `proto/asc/v1/mergequeue.proto`) | L |
| RepoSettingsPolicy | Live file beside the form with changed lines flashing (+/-) | done (seen on a live hub (ui20): the file as it would be, changed lines marked + and -, beside the Form view) | client-side diff against `GetSource.text` | M |
| RepoSettingsPolicy | Changes tab: diff of the one pending edit; "Changing policy is a change" panel naming reviewers (Priya Raman and Diego Alvarez) | done (seen on a live hub (ui18): a line diff of the draft against the policy in force, and the sentence that changing policy is a change; the reviewers are not named) | `Policy.Propose` response and maintainers from `GetPolicy.roles` | M |
| RepoSettingsPolicy | TOML error state: "Line 11: max_minutes must be a whole number", form disabled while invalid | done | diagnostics list (`asc-policy-diagnostics`); form disable does not apply because there is no form | S |
| RepoSettingsPolicy | Read-only state for non-maintainers with names to ask | done (the page shows a banner naming the maintainers and disables the form; the page itself is for repository administrators, checked by the unit tests of the template data, not seen with a second account) | route needs repo admin (`registerAscTrustRepoSettingsRoutes`); readers cannot see the page; show read-only for readers with maintainers from `GetPolicy` | M |
| RepoSettingsPolicy | After-propose state: "Proposed as change kz9fw. Waiting for one approval" with Open change | done | `p_proposed` | S |
| RepoSettingsPolicy | Loading state | ui-only-missing | skeleton | S |

## RepoSettingsRules

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| RepoSettingsRules | Protected refs table: pattern, kind (Trunk, Release lines, Tags), can land (Merge queue, Maintainers, Release key), force push Never, signed changes Required, edit and delete | done (seen on a live hub (ui16): the Rules page shows the asc policy of the default branch (approvals, checks, signing, force push, queue, maintainer-only paths); one rule only, no kind column; the branch protection API reads and proposes it, see docs/asc/BRANCH-PROTECTION.md) | `H:templates/repo/settings/branches.tmpl`, `protected_branch.tmpl`, `tags.tmpl` carry Forgejo rules (branch and tag rules on two pages); fold into one table and map to the columns | L |
| RepoSettingsRules | Add / Edit rule side panel with Who can land checkboxes (Merge queue, Maintainers, Release key (CI), Agents), Require signed changes, Allow force push, Allow deleting | needs-both | Forgejo rule has push whitelists; asc policy has `allow_force`/`require_signed` only, no per-ref kinds or agents (`A:policy.rs` Trunk) | L |
| RepoSettingsRules | Pattern overlap error ("Rules cannot overlap") and "2 open stacks target a ref this pattern matches" | needs-hub-backend | validate in `H:routers/web/repo/setting/protected_branch.go`; count pulls by base branch | M |
| RepoSettingsRules | Rule removed with Undo for 30 minutes from the operation log | needs-hub-backend | `asc_ops` entry for rule deletion; Forgejo rules are in the hub DB, not in asc ops | M |
| RepoSettingsRules | Stack behaviour switches: restack when main moves, keep approvals when only the base changed, let collaborators push onto a stack | needs-both | no repo-level config in asc (`Stacks.Restack` is invoked on demand); store in policy or repo config and enforce in `asc-serve` | L |
| RepoSettingsRules | Longest stack (8) and clean-up after landing (7 days) | needs-asc-backend | new policy/config keys and a janitor in `A:crates/asc-serve` | M |
| RepoSettingsRules | How stacks land: keep every change / squash into one change | needs-asc-backend | `[merge] strategy` values are rebase, rebase-disjoint, fast-forward; no squash (`A:policy.rs`) | M |
| RepoSettingsRules | Empty state "Nothing is protected" with Protect main | ui-only-missing | `branches.tmpl` | S |

## RepoSettingsWebhooks

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| RepoSettingsWebhooks | Hook list with URL, kind, events, last delivery, Active switch, Failing banner | ui-only-missing | `H:templates/repo/settings/webhook/list.tmpl`, `base_list.tmpl` (Forgejo list) | M |
| RepoSettingsWebhooks | One hook with tabs Settings / Recent deliveries (12), delivery detail with Request / Response, Redeliver | done | `webhook/history.tmpl`, Forgejo redeliver | S |
| RepoSettingsWebhooks | Hook kinds Generic, Chat, ASC agent (starts an agent run on an event) | needs-hub-backend | `H:modules/webhook/type.go` has Forgejo kinds; add `asc-agent` kind that calls `Agents.CreateRun`; map Chat to slack/discord/matrix | L |
| RepoSettingsWebhooks | ASC events: Changes (Landed, Opened, Review requested), Agents (Run started, Run finished), Releases (Published), Policy (Changed) | needs-hub-backend | event types and notifiers in `H:models/webhook`, `H:services/webhook`, fired from `ascsync`/agent callbacks | L |
| RepoSettingsWebhooks | Payload URL https-only with certificate check; loopback http only with local option | needs-hub-backend | `H:modules/setting` `ALLOWED_HOST_LIST`; add the explicit local option and the refusal text | S |
| RepoSettingsWebhooks | Secret shown masked with Replace, never shown again | ui-only-missing | `webhook/new.tmpl` shows the secret field | S |
| RepoSettingsWebhooks | Send test delivery | done | Forgejo test delivery | S |
| RepoSettingsWebhooks | Failing-hook alert banner role=alert and menu count "Webhooks 3" | ui-only-missing | list + navbar | S |
| RepoSettingsWebhooks | Add webhook dialog with http address refused | ui-only-missing | `webhook/new.tmpl` is a page | M |
| RepoSettingsWebhooks | Empty state | ui-only-missing | `base_list.tmpl` | S |

## RepoSettingsSecrets

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| RepoSettingsSecrets | Secrets and variables on one page | done (seen on a live hub (ui32): one menu entry with two tabs, Secrets and Variables, each its own list, not one scrolling page; axe clean) | `H:templates/shared/secrets/add_list.tmpl` and `shared/variables/variable_list.tmpl` live on `/settings/actions/secrets` and `/settings/actions/variables` | M |
| RepoSettingsSecrets | Scope chips per secret (Checks, Agent runs, Releases) and "Agents only receive it when Agent runs is ticked" | needs-both | `Workflows.SetSecret` has name and value only (`A:proto/asc/v1/workflows.proto`); add scopes to the asc secret and to the Forgejo secret model (`H:models/secret`), enforce in agent run env | L |
| RepoSettingsSecrets | "Updated by Maya Okafor, 12 d ago" | needs-hub-backend | `WorkflowSecret.updated_by` exists in asc; Forgejo secret has created time only; template | S |
| RepoSettingsSecrets | Update value dialog with name locked; value never shown | ui-only-missing | `H:routers/web/repo/setting/secrets.go`, `add_list.tmpl` | S |
| RepoSettingsSecrets | Delete confirmation that lists which scopes lose it | ui-only-missing | `add_list.tmpl` confirm | S |
| RepoSettingsSecrets | Name rule (capitals, digits, underscores) validation in dialog | done | Forgejo secret name validation | S |
| RepoSettingsSecrets | Variables table with "Available to" scope and updated-by | needs-both | same scope work as secrets | M |
| RepoSettingsSecrets | Secret-redaction in change logs ("the log redacts it") | needs-asc-backend | run log redaction in `A:crates/asc-ci` job log append | M |
| RepoSettingsSecrets | Saved toast and empty state | ui-only-missing | `add_list.tmpl` | S |

## RepoSettingsAI

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| RepoSettingsAI | Provider list (Anthropic, OpenAI, own endpoint) with key hint, added by, verified time, Replace, Remove; stored encrypted | done | `H:templates/shared/asc/byo_ai*.tmpl`, `H:services/ascbyo/aikeys.go`, `AiProviders` RPCs | S |
| RepoSettingsAI | Replace key dialog that verifies before swapping and shows "Anthropic rejected this key (401)" | needs-both | `UpdateKey` + `TestKey` exist; asc must keep the old key until the new one passes (atomic verify in `UpdateAiKey`), hub dialog | M |
| RepoSettingsAI | "What uses AI" table: Agent runs, Review summaries, Meaning search, each with its provider and monthly cap and spend meter (amber 80%, red 100%) | needs-both | asc caps are per key (`monthly_cap_micro_usd`), not per feature; add feature routing (`AiFeature` -> key + cap) in `A:proto/asc/v1/aiproviders.proto` and the usage grouping; hub table | L |
| RepoSettingsAI | Cap reached: feature pauses with "until 1 Nov or you raise it" | needs-asc-backend | key state `CAPPED` exists; per-feature pause message and reset date | M |
| RepoSettingsAI | No keys: "AI features are off" empty state | done | `ascbyo.ai.empty` | S |
| RepoSettingsAI | Repository-level page (keys are owner scope in asc, page lives under the repository menu) | ui-only-missing | board title "Your keys, your providers, your caps" under repo menu; hub embeds owner list | S |

## RepoSettingsStorage

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| RepoSettingsStorage | Choose ASC Hub storage or own S3-compatible bucket (endpoint https only, region, bucket, prefix, access key, secret with Replace, path-style) | done | `H:templates/shared/asc/byo_storage.tmpl`, `H:services/ascbyo/storage.go`, `Storage` RPCs | S |
| RepoSettingsStorage | Test connection with four steps (reachable, can write, can read, versioning on) and AccessDenied explanation naming the needed permissions | needs-asc-backend | `VerifyStorageTarget` returns `checks`; confirm it covers TLS time, write, read and versioning, and the permission wording in `A:crates/asc-serve` storage verify | M |
| RepoSettingsStorage | Usage: 38.2 GB split into changes, large files, search indexes plus objects and "read this month" | needs-asc-backend | no usage RPC in `A:proto/asc/v1/storage.proto`; add per-repo usage by kind and monthly reads | M |
| RepoSettingsStorage | Moving data: 64% progress, Pause, Cancel and keep current | needs-both | `StartMigration/GetMigration/CancelMigration` exist; no Pause RPC; hub progress UI | M |
| RepoSettingsStorage | Managed storage selected: "38.2 GB of 100 GB on your plan", Use my own bucket | needs-both | plan quota from `H:models/quota`; usage as above | M |
| RepoSettingsStorage | Local-option and http refused message | done | `H:services/ascbyo/validate.go` | S |
| RepoSettingsStorage | Bucket settings changed bar with Discard / Save and test | ui-only-missing | `byo_storage.tmpl` uses plain forms; reuse `data-savebar` | S |
| RepoSettingsStorage | Meaning-search index enabling link ("An admin can enable the index in storage settings") | needs-both | no index toggle in the storage page or `CodeIntel.IndexStatus` surface | M |

## Mobile twins: behaviour that differs from the desktop board

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Mobile-Repo | Stack cards replacing the table; filter chips that snap; pull to refresh; sticky Clone and New stack | needs-hub-backend | as the desktop rows; chips and pull to refresh in `home_cards.tmpl` and a script | M |
| Mobile-Code | Avatar column for blame, tap opens the change sheet (swipe down closes), Files sheet, long-press range select with Share | ui-only-missing | see Code rows | M |
| Mobile-History | Rail-and-node list, chips open a bottom sheet, 44 px older-changes button | ui-only-missing | see History rows | M |
| Mobile-Search | Sticky search, Filters bottom sheet, cards with reason first | ui-only-missing | see Search rows | M |
| Mobile-Releases | New release as a bottom sheet; channel chips | ui-only-missing | see Releases rows | M |
| Mobile-NewRepo | Name check after 300 ms, Start from keeps typed values, sticky primary | needs-hub-backend | see NewRepo rows | M |
| Mobile-RepoSettings* | Detail pages with back arrow and same title, one save bar | done | `ds-settings-nav` shell with `snav_bar` | S |
| Mobile-Changeset, Compare, Refs, Dependencies, Insights, GitNote, Stacks | Same behaviour as the desktop boards | done | covered by the rows of those boards; the twin adds no behaviour beyond 44 px targets | S |

## Counts

Total rows: 263.

| Status | Rows |
| --- | --- |
| done | 64 |
| ui-only-missing | 100 |
| needs-hub-backend | 65 |
| needs-asc-backend | 12 |
| needs-both | 22 |

| Board | done | ui-only-missing | needs-hub-backend | needs-asc-backend | needs-both |
| --- | --- | --- | --- | --- | --- |
| Repo | 8 | 11 | 6 | 1 | 2 |
| NewRepo | 3 | 3 | 7 | 0 | 1 |
| Code | 6 | 8 | 6 | 0 | 0 |
| Changeset | 2 | 4 | 7 | 0 | 1 |
| History | 4 | 6 | 4 | 0 | 1 |
| Refs | 2 | 4 | 2 | 1 | 0 |
| Compare | 2 | 2 | 2 | 0 | 0 |
| Search | 6 | 12 | 1 | 1 | 0 |
| Releases | 3 | 4 | 6 | 1 | 0 |
| Dependencies | 10 | 3 | 0 | 1 | 0 |
| Insights | 1 | 1 | 3 | 0 | 2 |
| InsightsPeople | 0 | 2 | 1 | 0 | 2 |
| GitNote | 1 | 3 | 0 | 0 | 0 |
| Stacks | 1 | 6 | 6 | 0 | 0 |
| RepoSettingsMenu | 0 | 2 | 0 | 0 | 0 |
| RepoSettingsGeneral | 2 | 3 | 3 | 0 | 1 |
| RepoSettingsAccess | 1 | 3 | 3 | 0 | 1 |
| RepoSettingsPolicy | 3 | 6 | 0 | 1 | 2 |
| RepoSettingsRules | 0 | 2 | 2 | 2 | 2 |
| RepoSettingsWebhooks | 2 | 5 | 3 | 0 | 0 |
| RepoSettingsSecrets | 1 | 4 | 1 | 1 | 2 |
| RepoSettingsAI | 2 | 1 | 0 | 1 | 2 |
| RepoSettingsStorage | 2 | 1 | 0 | 2 | 3 |
| Mobile-Repo | 0 | 0 | 1 | 0 | 0 |
| Mobile-Code | 0 | 1 | 0 | 0 | 0 |
| Mobile-History | 0 | 1 | 0 | 0 | 0 |
| Mobile-Search | 0 | 1 | 0 | 0 | 0 |
| Mobile-Releases | 0 | 1 | 0 | 0 | 0 |
| Mobile-NewRepo | 0 | 0 | 1 | 0 | 0 |
| Mobile-RepoSettings* | 1 | 0 | 0 | 0 | 0 |
| Mobile-Changeset, Compare, Refs, Dependencies, Insights, GitNote, Stacks | 1 | 0 | 0 | 0 | 0 |

Open-size totals of the rows that are not done: S 74, M 92, L 33.
