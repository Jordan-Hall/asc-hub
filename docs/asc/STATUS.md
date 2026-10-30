# ASC Hub completeness status

Audit of 2026-10-30 of the hub (`D:/dev/asc-hub`, base `origin/asc/v16` at fdcf7e595f) against the 191 design boards of `design/project-v3` (95 desktop boards, 96 Mobile-* twins) and against asc `origin/master` (0817c5a9). The requirement: every screen matches its board exactly (layout, copy, every feature), desktop and mobile, light and dark, with frontend and backend working, with no TODO or stub. This page says how far that is true. It replaces the reading of `UI-COVERAGE.md` (which counts markup classes only) and corrects `feature-gaps/*.md`.

## How to read this page

- **Evidence levels.** *Read* means I opened the file and the cited line shows the behaviour. *Static* means the files and symbols named by the gap row exist in the tree; the behaviour was not run. *Logged* means only the gap file says it. Nothing here was run in a browser: there is no pixel report (`scratchpad/pixel-report` does not exist; the harness is `tests/pixel` on branch `pixel/harness`, unmerged, with no result). The live run of `QA-REPORT.md` (tree 98d46e82f2) is older than 571 commits.
- **Status words.** done, partial, missing. Beside missing: the kind of work (UI = template or script only, hub, asc, hub+asc), taken from the gap row.
- **Rows** are the 845 rows of `feature-gaps/*.md` (the roadmap says 846). Per board, a copy check compares every text node of the board with the hub's templates, English locale, scripts and Go handlers. Sample data (names, repository names, numbers) is ignored, so the percentage is indicative: a low value means copy differs or the element is missing, a high value does not prove behaviour.
- **Evidence column.** path:N is the line where the behaviour shows (named in the gap row, or the best word match inside the board's own templates); path (file) names the file only; see where-cell means the gap row names no file; absent: lines record a search that found nothing; logged as not built means the gap file says so and I did not re-probe it. The Gap column for a missing row is the gap file's plan.
- Rows I changed after reading the code are listed in section 5 and applied in `feature-gaps/*.md`.

## 1. Counts

Gap files as logged: done 443, needs-asc-backend 15, needs-both 95, needs-hub-backend 139, ui-only-missing 153 (total 845). The status counts in the headers of `repo.md` and `review.md` (64 and 72 done) were behind the table rows; this audit recomputed all four headers (130 and 131 done after corrections). The roadmap table (`FEATURE-ROADMAP.md`) is all zeros.

After this audit:

| Area | Boards | Rows | Done | Partial | Missing | Done % |
|---|---|---|---|---|---|---|
| shell/settings | 47 | 317 | 155 | 33 | 129 | 49% |
| repo | 24 | 274 | 133 | 26 | 115 | 49% |
| review | 5 | 88 | 35 | 15 | 38 | 40% |
| agents-ops | 12 | 126 | 96 | 7 | 23 | 76% |
| auth | 6 | 40 | 24 | 5 | 11 | 60% |
| **all** | 94 | 845 | 443 | 86 | 316 | 52% |

Missing rows by kind of work: UI 153, hub 135, asc 15, hub+asc 87. Rows that need asc work are fewer than the logs say: asc `origin/master` has 272 RPCs and the hub's generated stubs have the same 272; 52 of them are newer than the local asc checkout of 2026-10-28 (Insights, Releases, Backups, Sessions, Usage, OrgSettings, DataExport, AiKeyShares, SecretScan, Review brief and suggestions, Agents pause and checkpoints) exist on both sides.

Copy match across all 95 desktop boards (copy-like strings found in the hub): NewChange 35%, ReviewVersions 49%, Download 50%, Dependencies 51%, Drafts 51%, Inbox 53%, RepoSettingsStorage 53%, GetStarted 54%, Projects 54%, Checks 55%, ChangeChecks 56%, Refs 56% are the lowest; no board is at 100%, the highest is About at 90%.

## 2. Requirements you listed, checked one by one

| Check | Verdict | Evidence | Gap |
|---|---|---|---|
| Tokens are the platform's personal access tokens | done (2026-10-30, branch feat/wu03-tokens) | One token system: `models/auth/access_token.go`. New `ExpiresUnix` (0 = never); `GetAccessTokenBySHA` refuses an expired token for API, git, packages and asc alike; the form offers 7/30/90/365 days or never (default 30), the list shows Expires and an Expired chip; regenerating gives the same lifetime again. New scopes `read/write:changes` and `read/write:runs` (`access_token_scope.go`, migration `v17z_asc_token_expiry.go` gives existing repository tokens the matching changes and runs scopes). The asc forwarder holds a personal token to the scope of the part of asc it calls (`services/ascforward/scope.go`: Changes, Stacks, Review, MergeQueue, Inbox, Projects need changes; Agents, AgentTokens need runs; read-only method names need read, any other name needs write). `routers/private/asc.go` accepts an `area` for asc's own authz calls. Device login (`asc_device.go`) now takes the scope the terminal asks for (`scope` parameter, only repository, changes, runs, read:organization, read:user; default write repository/changes/runs + read org/user), shows it on the approval page and mints a token that expires after 90 days; it never mints `all`. | Open: asc does not send `area` yet (`crates/asc-serve/src/forgejo.rs`), so only calls through the hub's forwarder are held to changes/runs (that is every public client). Tokens made through the REST API (`POST /users/{u}/tokens`) take no expiry option yet. |
| Signed forwarded claim to asc | done | services/ascidentity/ascidentity.go signs asc-forwarded-v1 with HMAC-SHA256 (user, role, subject, ts, 60 s skew); services/asctrust/usercall.go:64,129 and services/ascagentui decide with the hub's permission model first and attach the claim; services/ascforward/forward.go strips client-supplied x-asc-forwarded-* and x-asc-hub-*. asc verifies in crates/asc-rpc/src/forwarded.rs and uses it in changes/rpc.rs:812, cloud/rpc.rs:824, review/mod.rs:259, review/acting.rs:64. | ARCHITECTURE.md:157 still says asc honours only the admin role; asc master has read and write roles and the org/user/instance subjects. The statement is stale; add a per-service test list of which RPCs accept which claim. |
| Row-level visibility equals the platform | partial | routers/private/asc_parity_test.go compares asc's authz answer, the hub's page decisions and git storage for the same grants; the authz endpoint answers a denied repository like a missing one (README.md:195). | The matrix covers repository read/write/admin. Cross-repository lists (dashboard, inbox, palette, explore, profile heatmap, admin) have per-handler tests but no one matrix; org-level and private-profile rows are not in it. |
| Landing download section wired to real GitHub releases of Jordan-Hall/asc | partial | The section is real: templates/home.tmpl:292-320 renders AscDownload from the signed manifest (routers/web/asc_download.go:67, modules/ascdownload); services/ascdownloadsync fetches and verifies a GitHub release API address (ASC_HUB_DOWNLOAD_SOURCE_URL, docker/allinone/usr/local/bin/asc-hub-entrypoint:217-236). | Nothing points it at Jordan-Hall/asc: [asc] DOWNLOAD_SOURCE_URL defaults to none (modules/setting/asc.go:56) and needs DOWNLOAD_PUBLIC_KEY; gh api repos/Jordan-Hall/asc/releases/latest answers 404 (no release is published, signing key not set). So the landing download section and the Download board are hidden on a fresh hub (home.tmpl:14 and 292 test .AscDownload). |
| Landing content word for word against the Main board | partial (requirement not met) | Main copy match is 83% (182 strings found, 38 not). Hero headline, lead, install pills with aria-pressed, product window, stack, review, log and queue, agents, workflows and BYO sections are in (home.tmpl, locale asc_land). | Differs from the board: announcement pill (startpage.pill Built for teams where agents write a lot of the code, board ASC 1.4: agent runs and the merge queue are here); strip heading (board Works with the editors and agents your team already uses); the board's Why ASC block (Agents write faster than people can read, three problem cards: Review 80 lines at a time, Every change says who and why, Undo any operation) is replaced by startpage.why_* and three benefits; extra sections not on the board (How it works, Compatibility, Public repositories); footer groups of the board (Product, Developers, Company, Legal with Stacks and changes, Agent runs, Changelog, CLI reference, REST API, MCP server, Status, Security, Contact, Privacy, Terms, Acceptable use, Subprocessors, Cookies) are not in footer_content.tmpl; no All systems operational line. |
| Sign-in and sign-up per the Onboarding board | partial | Done: split layout (user/auth/shell_start.tmpl), password Show toggle, remember me, forgot and reset pages, device approval (user/auth/asc_device.tmpl, routers/web/auth/asc_device.go, RFC 8628 routes at routers/web/web.go:744), live Your handle hint with availability check (web_src/js/features/asc-signup.js, routers/web/auth/signup_name.go, rate limited 20 per minute), optional full name, six-box code input (asc-codeboxes.js), invite-only banner (signup_inner.tmpl:5). | Missing: Sign in with a passkey (no discoverable-credential route; only 2FA keys); the 5-attempts-5-minutes lockout (no login throttle in routers/web/auth/auth.go); sign-up strength meter and the three live rules; the Terms and Privacy checkbox with stored acceptance; Use a security key instead on the 2FA page; Try another way on the key page; SSO button label Single sign-on (Acme SSO) comes from oauth_container.tmpl not the board; copy of the states frame (Sign in to ASC Hub, Pick up your stacks...) differs: Onboarding copy match 57%. |
| CLI hint reflects on-demand per-repo auth | missing | The hub hint is asc_auth.cli_hint (options/locale/locale_en-US.ini:4602, templates/user/auth/signin_inner.tmpl:36): Using the asc command line? Run asc auth login --host X --prompt and paste an access token. The same up-front command is on home.tmpl:36,42, applications.tmpl:15, post-install.tmpl. The board says Run asc auth login --host hub.acme.dev. This page will ask you to approve the device. | Both are the up-front model. In asc, device sign-in exists only under asc auth login (crates/asc/src/cli/auth.rs:211, auth_device.rs); asc clone does not ask for credentials when the server answers that the repository needs them (no call to auth_device outside auth.rs). Needs the asc change first (clone and fetch ask per host and path, reuse the credential helper, start the device flow inline, prompt for a token without a browser), then the hint: asc clone <url> signs you in when the repository needs it; public repositories need nothing. |
| TSRX first-class | partial | Hub: .tsrx detected as TSRX (modules/analyze/code_language.go:13-39), Chroma lexer registered (modules/highlight/highlight.go:20, modules/highlight/tsrx/tsrx.go, golden tests), CodeMirror language (web_src/js/features/codemirror-lang.ts:153, codemirror-tsrx.ts). asc: crates/asc-codeintel/src/tsrx.rs, tags/tsrx.rs, chunk.rs:471 (template regions blanked), asc-serve/src/deps/scan.rs:229 (imports). | Not found: linguist-language entry in .gitattributes; syntax-aware hunk header with the enclosing component for .tsrx in diffs and the review where-to-look block (row Review hunk header is open); a hub test that a markdown fence and a diff of .tsrx use the lexer end to end; docs of language server use. |

Other findings that cross boards:

- Review threads never reach asc: the hub does not call `Review.ListThreads`, `AddComment`, `ResolveThread`, `RequestReview`, `SetBrief`, `CompareVersions`, `PrepareSuggestion`, `MarkSuggestionApplied`, `SuggestReviewers`, `DraftContext`, `HunkContext` (11 RPCs that exist in asc), so an `asc` client does not see hub comments and suggestions cannot be applied as a new version.
- The hub has no client for the asc `Releases` service (`ascv1.NewReleasesClient` is never built); release notes, provenance and checksums on the Releases board are Forgejo data. Also never called: `Ci.JobLog`, `Ci.CompareWithMain`, `DepsService.SetConfig`, `DataExport.GetExport`, `Agents.GetPerson`, `MergeQueue.SetPaused` (the hub pauses through `services/automerge`; check it reaches asc).
- `UI-COVERAGE.md` claims 95 of 95 boards are design system and 0 partial. That measures class names. By behaviour and copy no board is complete (section 3).
- `FEATURE-ROADMAP.md` counts table shows zeros and must be replaced by the table in section 1 (it is outside the files I may edit here).
- `QA-REPORT.md` lists open defects O1 to O15 from an older tree; re-run the journeys (work unit WU-30).

## 3. Per-area counts and boards

- **shell/settings**: 47 boards, 317 rows, done 155, partial 33, missing 129. Boards: About, AdminAudit, AdminBackups, AdminConfig, AdminDashboard, AdminHealth, AdminHooks, AdminModeration, AdminMonitor, AdminNotices, AdminOrgs, AdminPackages, AdminRepos, AdminRunners, AdminUser, AdminUsers, Dashboard, Docs, Download, Drafts, Errors, Explore, ExploreOrgs, ExploreUsers, GetStarted, Inbox, Install, Main, NewOrg, OrgPeople, OrgProfile, OrgSettings, OrgTeams, Packages, Profile, Projects, Shell, UserSettingsAI, UserSettingsAccount, UserSettingsAppearance, UserSettingsNotifications, UserSettingsOrgs, UserSettingsPrivacy, UserSettingsProfile, UserSettingsRepos, UserSettingsStorage, UserSettingsUsage.
- **repo**: 24 boards, 274 rows, done 133, partial 26, missing 115. Boards: Changeset, Code, Compare, Dependencies, GitNote, History, Insights, InsightsPeople, IssueView, Issues, NewRepo, Refs, Releases, Repo, RepoSettingsAI, RepoSettingsAccess, RepoSettingsGeneral, RepoSettingsPolicy, RepoSettingsRules, RepoSettingsSecrets, RepoSettingsStorage, RepoSettingsWebhooks, Search, Stacks.
- **review**: 5 boards, 88 rows, done 35, partial 15, missing 38. Boards: ChangeChecks, Checks, NewChange, Review, ReviewVersions.
- **agents-ops**: 12 boards, 126 rows, done 96, partial 7, missing 23. Boards: AgentAccess, AgentNew, AgentRun, Agents, Audit, Ops, Policy, Queue, Security, WorkflowDef, WorkflowRun, Workflows.
- **auth**: 6 boards, 40 rows, done 24, partial 5, missing 11. Boards: AdminAuth, Onboarding, UserSettingsApps, UserSettingsKeys, UserSettingsSecurity, UserSettingsTokens.

Cross-cutting mobile rows (not tied to one board) are counted in the area they belong to and listed in section 6.

## 4. Per-board tables

Each Mobile-* twin is the same page at 390 px; its rows are prefixed Mobile. Boards `Mobile-Menu`, `Mobile-RepoSettingsMenu` and `Mobile-UserSettingsMenu` are mobile-only index pages. Board `components` (A1, design system) has no page; its tokens live in `web_src/css/asc-ds/tokens.css`.

### Area: shell/settings

#### About (ASC Hub about)

Area shell/settings. Rows 3: done 2, partial 0, missing 1. Copy found in the hub: 90% of 88 copy strings (mobile twin 79%). Template(s): -. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Contact us; Stacks and changes; CLI reference; MCP server; Acceptable use; Subprocessors.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Hero, story with before and after table, principles, open source chips, data ownership, contact | done | templates/asc/about.tmpl:1 | - |
| Header About and footer link go to /-/about | done | services/ascdocs/content/WHY.md (file) | - |
| Source code, Security policy and Contact targets | missing (UI) | logged as not built (not re-probed) | Targets come from [asc] settings; confirm they are set |
| Board note: Calls to action: Download ASC (/download), Read the docs | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Principle cards are not links; their content is the content | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Hero, story with before and after table, four principles, open source and data ownership,... | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Light and dark; hero stays navy | unverified | not tracked in the gap files | needs a row and a test |

#### AdminAudit (ASC Hub admin: audit log)

Area shell/settings. Rows 2: done 1, partial 0, missing 1. Copy found in the hub: 72% of 69 copy strings (mobile twin 65%). Template(s): asc_audit.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Search actor, action or target; All actions; Today 09:41; Runner registered; Runner registered.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Hash-chain banner, filters, details, export, verify | done | routers/web/admin/asc_audit.go (file) | - |
| Signed export file for the range | missing (asc) | logged as not built (not re-probed) | Audit.Export streams a BLAKE3 digest but no signature (proto/asc/v1/audit.proto) |
| Board note: Rows expand in place to show the full record, request id and the diff | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Export downloads a signed file for the range | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, filtered, no results, chain verification failed (error banner), export in progre... | unverified | not tracked in the gap files | needs a row and a test |

#### AdminBackups (ASC Hub admin: backups)

Area shell/settings. Rows 2: done 2, partial 0, missing 0. Copy found in the hub: 64% of 75 copy strings (mobile twin 54%). Template(s): -. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Schedule, check and restore copies of this server.; Backups need attention.; The last backup failed because the target timed out. Your newest good copy is 2...; Every day at 02:00; UTC. Kept 30 days, then weekly for a year..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Page with how to run backups (read-only text) | done | templates/admin/backups.tmpl (file) | - |
| Schedule, target, next backup, Back up now, history with Retry/Restore/Verify, in-progress met... | done | routers/web/admin/asc_backups.go:24 | - |
| Board note: Restore needs RESTORE typed and puts the server read only | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Verify restores into a scratch area and checks the result | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Retention is shown in plain words | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Healthy, failed (banner + Retry), running (progress), restore confirmation, never run (se... | unverified | not tracked in the gap files | needs a row and a test |

#### AdminConfig (ASC Hub admin: configuration)

Area shell/settings. Rows 4: done 1, partial 0, missing 3. Copy found in the hub: 58% of 69 copy strings (mobile twin 40%). Template(s): config.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Runtime settings you can change, and server settings you can read.; Change without a restart. Each change is audited.; People can create accounts themselves.; Accounts stay inactive until the address is confirmed.; Allow agents on this server.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Server settings read only (domain, protocol, SSH, storage, mail) | done | templates/admin/config_server.tmpl (file) | - |
| Runtime switches: Allow registration, Require email confirmation (runtime set has only avatar... | missing (hub) | logged as not built (not re-probed) | modules/setting/config + admin/config_runtime.tmpl + web_src/js/features/admin/config.js |
| Allow agents on this server; Default agent budget | missing (hub+asc) | logged as not built (not re-probed) | asc: instance flag and default budget setter (agents.proto has AgentBudgetLimits only); hub: switch + field |
| Restart-required banner, invalid value error, file and line shown, Copy key/value | missing (UI) | logged as not built (not re-probed) | config_server.tmpl + config.js |
| Board note: Runtime settings save on change; server settings are read only and show the file and line | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Copy copies the key and value | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, unsaved change, restart required banner, invalid value error | unverified | not tracked in the gap files | needs a row and a test |

#### AdminDashboard (ASC Hub admin: dashboard)

Area shell/settings. Rows 7: done 3, partial 1, missing 3. Copy found in the hub: 70% of 79 copy strings (mobile twin 63%). Template(s): dashboard.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; +38 this week; Changesets today; Agent runs today; Last backup is 26 hours old; Nightly backup missed at 02:00. The target was unreachable..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Stat tiles linking to pages (people, repos, orgs, issues) | done | templates/admin/dashboard.tmpl (file) | - |
| Tiles "Changesets today (31% by agents)", "Agent runs today (96.4% finished)", "+38 this week" | missing (hub+asc) | logged as not built (not re-probed) | asc: new admin stats RPC; hub: overview.go dashboardFacts |
| Needs attention: runners offline, reports, notices | done | see where-cell | - |
| Needs attention: backup overdue with "Back up now" | partial (hub+asc) | logged as not built (not re-probed) | asc: backup status RPC (only CLI asc serve backup exists, crates/asc-serve/src/backup.rs); hub: attention item |
| System card rows: object storage usage and mail queue age (database, queues, runners exist) | missing (hub+asc) | logged as not built (not re-probed) | asc: storage usage; hub: overview.go system rows |
| Recent audit entries card | missing (hub) | logged as not built (not re-probed) | call Audit.List via asctrust.CallInstance (as admin/asc_audit.go) from dashboardFacts + template |
| Numbers refresh every 30 s without layout shift | done | see where-cell | - |

#### AdminHealth (ASC Hub admin: health)

Area shell/settings. Rows 4: done 1, partial 0, missing 3. Copy found in the hub: 75% of 64 copy strings (mobile twin 69%). Template(s): self_check.tmpl, system_status.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Primary reachable, 4 ms. Migrations are up to date.; Disk usage; Data volume is 78% full. At the current rate it fills in about 41 days.; Outgoing mail; Runner capacity.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Self check list with passed/warn/fail chips, Copy report, Run again | done | templates/admin/self_check.tmpl (file) | - |
| Checks for disk fill rate, object storage probe, signing key validity, outgoing mail queue, ti... | missing (hub+asc) | logged as not built (not re-probed) | hub: overview.go healthFacts; asc: health RPC (object store probe, signing key expiry) |
| Gauges CPU, Memory, Disk, Object storage with percent text (hub shows uptime, memory, goroutin... | missing (hub) | logged as not built (not re-probed) | overview.go healthFacts (add disk/CPU), self_check.tmpl |
| Running state (steps appear one by one), could-not-run state | missing (UI) | logged as not built (not re-probed) | self_check.tmpl + htmx streaming |
| Board note: Run again re-runs every check and keeps the old result until it finishes | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Fix links to the page that solves it: Backups, Runners, Configuration | unverified | not tracked in the gap files | needs a row and a test |
| Board note: All passed, warnings, failed, running (steps appear one by one), check could not run | unverified | not tracked in the gap files | needs a row and a test |

#### AdminHooks (ASC Hub admin: hooks and applications)

Area shell/settings. Rows 2: done 1, partial 0, missing 1. Copy found in the hub: 80% of 59 copy strings (mobile twin 78%). Template(s): hook_new.tmpl, hooks.tmpl, list.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Webhooks and OAuth applications across the site.; System hooks fire for every repository. Default hooks seed new repositories.; Audit sink; Default for new repos; Apps registered by people and by the site..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| System and default webhooks, OAuth applications list | done | templates/admin/hooks.tmpl:5 | - |
| Columns scope, events, last delivery with status text; Replay from the list; secret regenerate... | missing (UI) | logged as not built (not re-probed) | hooks_list.tmpl uses the old base_list markup |
| Board note: Replay re-sends the last failed delivery | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Secrets are shown once | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, failing delivery, empty (no hooks), secret regenerated toast | unverified | not tracked in the gap files | needs a row and a test |

#### AdminModeration (ASC Hub admin: moderation)

Area shell/settings. Rows 2: done 1, partial 0, missing 1. Copy found in the hub: 75% of 52 copy strings (mobile twin 65%). Template(s): -. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; README of casey/promo-kit; Reported by 3 people. The README contains a phishing link.; Comment on change 88; Reported by 1 person and 1 agent. The comment is hostile toward a reviewer.; Profile of @sam.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Reports queue with Dismiss, mark handled, content actions | done | templates/admin/moderation/reports.tmpl:28 | - |
| Reason per action written to the audit log; reports grouped by target; appeal state; agent rep... | missing (hub) | logged as not built (not re-probed) | reports.go PerformAction, moderation service, reports.tmpl |
| Board note: Dismiss, Hide and Suspend are separate, each with a reason that goes to the audit log | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Reports group by target | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Queue with items, empty (all caught up), item hidden toast, appeal received | unverified | not tracked in the gap files | needs a row and a test |

#### AdminMonitor (ASC Hub admin: monitor)

Area shell/settings. Rows 2: done 1, partial 1, missing 0. Copy found in the hub: 80% of 64 copy strings (mobile twin 84%). Template(s): queue.tmpl, stacktrace.tmpl, stats.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Queues, scheduled tasks and stack traces.; Work waiting to be done. Flushing deletes waiting items.; Cron-style jobs.; Clean old sessions; Verify audit chain.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Queues table with Flush, cron table with Run now, stack traces | done | templates/admin/queue.tmpl (file) | - |
| Flush confirmation with count; Run now streams output; "Verify audit chain" cron task | partial (hub+asc) | logged as not built (not re-probed) | hub: queue.tmpl modal and streaming; asc: scheduled audit verify (Audit.VerifyChain exists, no schedule) |
| Board note: Flush asks for confirmation and counts what will be removed | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Run now runs a task immediately and streams its output | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, backed-up queue, failed cron task, empty queue, flush confirmation | unverified | not tracked in the gap files | needs a row and a test |

#### AdminNotices (ASC Hub admin: notices)

Area shell/settings. Rows 1: done 1, partial 0, missing 0. Copy found in the hub: 77% of 47 copy strings (mobile twin 67%). Template(s): notice.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Disk usage crossed 75%; Data volume is 78% full. See Health.; Cron: verify audit chain finished; Mail delivery failed 3 times; Notices older than 30 days are removed automatically..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Notices list, dismiss, dismiss all with confirm | done | templates/admin/notice.tmpl:31 | - |
| Board note: Dismiss removes one; Dismiss all asks to confirm | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, empty (nothing to report) | unverified | not tracked in the gap files | needs a row and a test |

#### AdminOrgs (ASC Hub admin: organizations)

Area shell/settings. Rows 2: done 1, partial 0, missing 1. Copy found in the hub: 80% of 49 copy strings (mobile twin 82%). Template(s): list.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; garage-eu; Rail Guild; rail-guild; r2-backup; ASC Hub 1.4.2.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Organizations table | done | templates/admin/org/list.tmpl (file) | - |
| Storage column (garage-eu / Server default) and Open button | missing (hub+asc) | logged as not built (not re-probed) | asc Storage.ListTargets/AssignRepo for the owner; hub list.tmpl (shows a Teams column instead) |
| Board note: Open goes to the organization page; admin actions are inside organization settings | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, empty fresh install, search with no match | unverified | not tracked in the gap files | needs a row and a test |

#### AdminPackages (ASC Hub admin: packages)

Area shell/settings. Rows 2: done 1, partial 1, missing 0. Copy found in the hub: 85% of 48 copy strings (mobile twin 91%). Template(s): list.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; asc-ui-tokens; ASC Hub 1.4.2; Hub and web UI based on Forgejo; All systems operational; Rendered in 18 ms.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Packages table and delete | done | templates/admin/packages/list.tmpl:54 | - |
| Run cleanup rules with a preview of counts (hub runs CleanupExpiredData directly) | partial (hub) | logged as not built (not re-probed) | routers/web/admin/packages.go + preview partial from org/settings/packages_cleanup_rules_preview.tmpl |
| Board note: Cleanup shows exactly what it will delete before it runs | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, empty, cleanup preview with counts | unverified | not tracked in the gap files | needs a row and a test |

#### AdminRepos (ASC Hub admin: repositories)

Area shell/settings. Rows 2: done 1, partial 0, missing 1. Copy found in the hub: 84% of 61 copy strings (mobile twin 88%). Template(s): list.tmpl, unadopted.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Every repository, its size and where its objects live.; garage-eu; garage-eu; Repositories found on disk without an owner appear here.; ASC Hub 1.4.2.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Repository table, unadopted list, delete | done | templates/admin/repo/list.tmpl:2 | - |
| Storage column and "storage mismatch" warning; delete needs full name typed | missing (hub+asc) | logged as not built (not re-probed) | asc storage assignment per repo; list.tmpl; delete modal in admin/repo |
| Board note: Manage opens the repository settings with admin rights | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Delete needs the full name typed | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, unadopted empty, delete confirmation, storage mismatch warning | unverified | not tracked in the gap files | needs a row and a test |

#### AdminRunners (ASC Hub admin: runners)

Area shell/settings. Rows 4: done 1, partial 0, missing 3. Copy found in the hub: 73% of 60 copy strings (mobile twin 65%). Template(s): actions.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; runner-eu-1; linux/amd64, docker; Running changeset checks, 4 min; runner-eu-2; linux/amd64, docker.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Runner list, status, labels, version, register, details | done | templates/admin/actions.tmpl:5 | - |
| Drain action and draining state | missing (hub) | logged as not built (not re-probed) | models/actions runner (no drain flag), task pick in services/actions, admin/runners/details.tmpl |
| Agent runners (agent-box-1, "Agent run #214", vCPU) | missing (hub+asc) | logged as not built (not re-probed) | asc agent runs on runners; hub shows only Forgejo actions runners |
| Registration token that expires in one hour, shown once, in a copy block | missing (UI) | logged as not built (not re-probed) | runners/create.tmpl, setup.tmpl (token is long lived in Forgejo) |
| Board note: Details shows labels, jobs and logs; Drain stops new jobs | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Registration token expires and is shown once | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Online, idle, offline (with since), draining, no runners (illustration + register command) | unverified | not tracked in the gap files | needs a row and a test |

#### AdminUser (ASC Hub admin: dana lee)

Area shell/settings. Rows 5: done 1, partial 0, missing 4. Copy found in the hub: 79% of 62 copy strings (mobile twin 73%). Template(s): view.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Dana Lee; Account facts, usage and administrative actions for one person.; Dana Lee; Password and one passkey; None owned.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Single page with account facts, emails, repos, orgs, delete | done | templates/admin/user/view.tmpl:87 | - |
| Tabs Overview/Emails/Keys/Sessions/Activity | missing (UI) | logged as not built (not re-probed) | view.tmpl is one scroll; keys, sessions and activity panels are missing |
| Reset 2FA and Suspend as separate buttons that ask for a reason written to the audit log (toda... | missing (hub) | logged as not built (not re-probed) | users.go EditUserPost (Reset2FA, ProhibitLogin), audit write via asctrust |
| Usage block (storage, agent minutes) and agents owned | missing (hub+asc) | logged as not built (not re-probed) | asc usage RPC (see UserSettingsUsage); view.tmpl |
| Sessions tab (devices, revoke) | missing (hub+asc) | logged as not built (not re-probed) | same session registry as UserSettingsSecurity |
| Board note: Tabs are views of one person: Overview, Emails, Keys, Sessions, Activity | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Reset 2FA and Suspend ask for a reason, which goes to the audit log | unverified | not tracked in the gap files | needs a row and a test |
| Board note: No impersonation: support acts through documented actions | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, suspended (banner), pending verification, deletion blocked by owned repositories | unverified | not tracked in the gap files | needs a row and a test |

#### AdminUsers (ASC Hub admin: people)

Area shell/settings. Rows 4: done 2, partial 0, missing 2. Copy found in the hub: 77% of 62 copy strings (mobile twin 79%). Template(s): edit.tmpl, list.tmpl, new.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Admins 6; Suspended 3; Require 2FA; Dana Lee; Casey Brandt.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Search, filter chips (Admins, 2FA off, Inactive, Suspended), table with role/2FA/repos/last ac... | done | templates/admin/user/list.tmpl:30 | - |
| Counts on the filter chips (Admins 6, 2FA off 212 ...) | missing (hub) | logged as not built (not re-probed) | list.tmpl + users.go count queries |
| Row checkboxes, "2 selected" bar, Require 2FA and Suspend bulk actions with affected-people di... | missing (hub) | logged as not built (not re-probed) | list.tmpl, new POST /admin/users/bulk in web.go + users.go, audit entries |
| New person, Edit | done | templates/admin/user/new.tmpl (file) | - |
| Board note: Filter chips narrow in place and sync to the address | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Row click opens the person; Edit opens the edit form | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Bulk actions confirm with a dialog that lists who is affected | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, bulk selection bar, suspended user, no matches, loading skeleton, empty (fresh i... | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Suspended people stay visible with a red chip | unverified | not tracked in the gap files | needs a row and a test |

#### Dashboard (ASC Hub dashboard)

Area shell/settings. Rows 18: done 9, partial 2, missing 7. Copy found in the hub: 61% of 102 copy strings (mobile twin 60%). Template(s): asc_home.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; j/k to move - Enter to open - e to done; "Can we keep the old cursor format for one release? Mobile clients pin to it."; Agent asked a question: which currency table wins?; Blocking 2 changes; Ready to merge: Rate limit headers.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Greeting by time of day and summary line | done | routers/web/user/asc_dashboard.go (file) | - |
| Work list with counts (Inbox, My stacks, Review requests, Agent runs), repositories, teams | done | templates/user/dashboard/asc_home.tmpl:17 | - |
| Pinned repositories per person; counts update live | missing (hub) | logged as not built (not re-probed) | ascDashRepos takes the five latest. Pin table (models/user) and a star-like action; counts need the live update below |
| Scope chips Everything / org / Personal filter the whole page, remembered | done | templates/user/dashboard/asc_home.tmpl:48 (best match) | - |
| Needs-you rows with chips (Agent, checks, Touches billing), diff size, age | done | templates/user/dashboard/asc_home.tmpl (file) | - |
| Rows act in place: Approve, Reply, Answer (agent question), Add to merge queue, Resolve | missing (hub+asc) | logged as not built (not re-probed) | Buttons are links (aria-hidden, tabindex -1) to the review. Hub: POST handlers for approve (review in routers/web/repo/pull.go),... |
| Row kinds: reply from a person, agent question, ready to merge with queue position and estimat... | missing (hub+asc) | logged as not built (not re-probed) | Kinds come from inbox sections; reply quote, "about 4 min" and "ASC restacked 2 of 3" need asc data (mergequeue.proto List, stack... |
| Keys j k, Enter, e (done), g then i, / | missing (hub) | logged as not built (not re-probed) | j, k, g i, / done in asc-keys.js; e needs the done state (Inbox) |
| Agents working now with budget bar, paused state, All runs | done | templates/user/dashboard/asc_home.tmpl (file) | - |
| Stack bars: click a segment opens the review at that segment | done | see where-cell | - |
| Merge queue card (Running, median wait, positions, "2 of 3 checks", Open) | missing (hub+asc) | logged as not built (not re-probed) | No such card. Hub aggregates the person's queue entries (routers/web/repo/asc_queue.go; asc mergequeue.proto List, Get, Watch); m... |
| Today feed with Filter | done | see where-cell | - |
| From your terminal card with Copy; g i tip | done | templates/user/dashboard/asc_home.tmpl (file) | - |
| Live updates | missing (hub+asc) | logged as not built (not re-probed) | Counts load once. Hub: poll /-/asc/inbox/count or reuse eventsource.sharedworker.js; asc WatchRefs / queue Watch for pushes |
| States: loading skeleton, new account, offline error with retry countdown | partial (UI) | logged as not built (not re-probed) | Inbox zero exists; the others missing |
| Undo toast ("Added to merge queue. Undo for 10 s") | partial (hub+asc) | logged as not built (not re-probed) | toast.js has no action button; asc Dequeue exists for undo; goes with row actions |
| Appearance setting hides agent surfaces | done | see where-cell | - |
| Mobile: Pull to refresh; swipe a row left to snooze, right to mark done | missing (hub) | logged as not built (not re-probed) | New touch module; done state (Inbox) |

#### Docs (ASC Hub documentation)

Area shell/settings. Rows 7: done 2, partial 1, missing 4. Copy found in the hub: 57% of 159 copy strings (mobile twin 62%). Template(s): -. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: A short path to the right page: search first, six entry points, and the latest...; Install ASC, connect to a hub and open your first stack in five minutes.; Stacks and restack; How a feature becomes four reviewable changes, and how they stay in order.; Undo and the operation log; Recover from any merge, restack or agent apply..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Docs home: search, Popular chips, six entry cards, "New in ASC 1.4" list, Need a person card | done | templates/asc/docs.tmpl (file) | - |
| Docs search with grouped results (Commands, Guides), did-you-mean, no results, loading; Ctrl K... | missing (hub) | logged as not built (not re-probed) | Index over docs/asc/*.md and the CLI reference built at start (new modules/ascdocs) plus GET /-/docs/search?q=; JS list |
| Article layout: sidebar tree, 720 px column, breadcrumb, steps, callouts, Previous and Next | missing (hub) | logged as not built (not re-probed) | Docs live on GitHub ($docsURL). Render docs/asc/** with the hub markdown renderer: AscDocs in routers/web/asc_public.go, new temp... |
| OS code tabs remembered; Copy without the prompt | missing (UI) | logged as not built (not re-probed) | Reuse the initDownload tab code; localStorage key |
| On-this-page TOC highlighting the section in view; Edit this page; Open in the hub | done | docs.js (not found in tree) | the where-cell cites docs.js, not in the tree (renamed or stale text); behaviour taken from the gap file |
| "Was this page helpful?" Yes and No | missing (hub) | logged as not built (not re-probed) | POST /-/docs/feedback, stored (new table or issue on the docs repo), rate limited |
| Mobile: Docs menu drawer, sticky "On this page" row, sticky next-page bar | partial (UI) | logged as not built (not re-probed) | "On this page" row and a next-guide link exist on a phone; the docs menu drawer is missing (the guide list sits under the article) |

#### Download (ASC Hub download)

Area shell/settings. Rows 12: done 8, partial 1, missing 3. Copy found in the hub: 50% of 167 copy strings (mobile twin 42%). Template(s): download.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: The command line for changesets, stacks and agent runs. One static binary, sign...; Version 1.4.2; We detected macOS on Apple silicon; ASC 1.4.2 for macOS; Download for macOS; brew install asc.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Detected system card, recommended file, size, truncated sha256, "Not your system?" and OS sect... | done | templates/download.tmpl:59 | - |
| Package manager cards per OS with Copy | done | modules/ascdownload/offer.go:75 | - |
| Files table with size, sha256 (copy whole, full value in title), .minisig link | done | templates/download.tmpl (file) | - |
| Copy confirms with an inverse-surface toast ("SHA-256 copied") | done | web_src/js/features/clipboard.js (file) | - |
| Connect card, access-token alternative, Verify your download, release key | done | templates/download.tmpl:12 | - |
| Editor extensions | done | templates/download.tmpl:177 | - |
| System requirements card | done | see where-cell | - |
| Release notes link, Older releases | partial (UI) | logged as not built (not re-probed) | Release notes and build-from-source exist. "Older releases" missing: modules/ascdownload/manifest.go holds one manifest, add a re... |
| States: No release yet (build from source, admin note), system not detected | done | templates/download.tmpl (file) | - |
| Loading releases skeleton | missing (UI) | logged as not built (not re-probed) | Page is server rendered; only needed if offers load client side |
| "Signature check failed" and "verified" banners | missing (UI) | logged as not built (not re-probed) | Board shows guidance copy, not a live check (a browser cannot verify minisign without a library). Decide: explainer card, or a WA... |
| Mobile: Sticky bottom bar "Download for your system"; files as cards | missing (UI) | logged as not built (not re-probed) | download.tmpl, asc-ds/touch.css |

#### Drafts (ASC Hub drafts)

Area shell/settings. Rows 3: done 1, partial 0, missing 2. Copy found in the hub: 51% of 39 copy strings (mobile twin 69%). Template(s): asc_drafts.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Add rate limit headers; - 3 changes - On this laptop and on the hub; Spike: streaming exports; - 1 change - Only on this laptop; Saved yesterday.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| List of drafts from the hub (name, repo, change count, saved time), terminal card, empty and e... | done | routers/web/user/asc_drafts.go:32 | - |
| Location line "On this laptop and on the hub / only on this laptop" | missing (asc) | logged as not built (not re-probed) | Cloud.GetWorkspace returns hub heads only; local-only drafts need a client report RPC |
| Continue (open in editor or show asc stack resume) and Discard buttons, 30-day undo, discarded... | missing (hub+asc) | logged as not built (not re-probed) | asc: Cloud.Purge deletes for good, add soft discard + undo; hub: POST /-/asc/drafts/discard and undo, buttons in asc_drafts.tmpl |
| Board note: Continue opens the draft in the editor or shows the asc command to resume | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Discard asks nothing: it is in the operation log and can be undone for 30 days | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Drafts sync when you opt in; local-only drafts say so | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Drafts from the terminal and the hub, where each lives, empty, discarded with undo, loadi... | unverified | not tracked in the gap files | needs a row and a test |

#### Errors (ASC Hub error and status pages)

Area shell/settings. Rows 8: done 5, partial 1, missing 2. Copy found in the hub: 61% of 38 copy strings (mobile twin 82%). Template(s): 404.tmpl, 413.tmpl, 500.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Error and system pages; When something goes wrong, say what and what next.; Each page keeps the header and footer, uses the same illustration language, off...; Switch account; Learn about artifact storage; Nothing you did caused this, and nothing was changed. Our team has been told..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| 404, 403, 413, 500, 503, offline cards with header and footer | done | templates/status/*.tmpl (not found in tree) | the where-cell cites templates/status/*.tmpl, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Request id ("Request id 7f3a91c2 - date UTC", selectable) | done | templates/status/500.tmpl (file) | - |
| 500 actions: Try again (same request id) and Status page | done | templates/status/500.tmpl (file) | - |
| 403 actions: Request access and Switch account | missing (hub) | logged as not built (not re-probed) | 403.tmpl has Sign in, Dashboard, Search. Request access needs an access-request flow to the owners of the org |
| 404 actions: dashboard, search the hub | done | templates/status/404.tmpl (file) | - |
| 413 card with limit, artifact storage link and Back | done | templates/status/413.tmpl (file) | - |
| 503 maintenance: version being upgraded, reads continue through the CLI, window, expected back... | missing (asc) | logged as not built (not re-probed) | 503.tmpl is a generic card and nothing serves it for maintenance. Hub: maintenance setting with window text and a middleware retu... |
| Offline page: "your edit is saved on this device", Retry now, served by a service worker | partial | routers/web/web.go:657 /-/asc-sw.js; web_src/js/features/asc-offline.js | Service worker and offline page exist; draft saving in editors and the copy your edit is saved on this device were not found. |

#### Explore (ASC Hub explore repositories)

Area shell/settings. Rows 15: done 8, partial 2, missing 5. Copy found in the hub: 68% of 144 copy strings (mobile twin 77%). Template(s): navbar.tmpl, repo_list.tmpl, repos.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Filter repositories; Has open stacks; repositories, sorted by recent activity; The hosted checkout: cart, address, payment and confirmation, built for sub-sec...; Updated Yesterday; design-tokens.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Tabs Repositories, Users, Organizations with counts | done | templates/explore/navbar.tmpl (file) | - |
| Filter bar: search, Sort, Visibility, kind pills; / focuses search | done | templates/shared/repo_search.tmpl:2 | - |
| Language filter menu | done | models/repo/language_stats.go:37 | - |
| Quick chips All, Has open stacks, Agent activity, Starred, Templates, Archived | missing (hub) | logged as not built (not re-probed) | repo_search has fork, mirror, template, archived pills only. "Has open stacks" needs an option on repo_model.SearchRepoOptions (r... |
| List and Grid layout toggle; layout persists per person | done | templates/shared/repo_search.tmpl (file) | - |
| Row: owner avatar, name, visibility chip, description, topics, language, stars, updated, open-... | done | templates/explore/repo_list.tmpl (file) | - |
| Agent activity chip on a row | missing (hub+asc) | logged as not built (not re-probed) | Not in repo_list.tmpl. Needs agent runs per repo over a recent window: asc ListRuns batched for a page, or a hub cache table fed... |
| Changeset count on a row ("3,812 changesets") | missing (hub+asc) | logged as not built (not re-probed) | Row shows stars and forks only. asc has per-repo change data but no batch stats RPC; add RepoStats or cache in services/ascsync |
| Contributor avatar stack on a row (people and agents) | partial (hub) | logged as not built (not re-probed) | Not present. Top contributors from models/activities or repo_model.GetContributors, cached per repo; agents depend on the agent d... |
| Row actions on hover or focus-within: Copy clone command, Star | done | see where-cell | the where-cell cites asc clone host/owner/name, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Right rail: Trending this week (changesets this week), Popular topics, Clone anything you can... | missing (hub) | logged as not built (not re-probed) | explore/rail.tmpl has only the clone card. Trending: weekly merged pull count per repo (models/issues/pull.go query, cached); top... |
| States: loading skeleton, empty hub (New repository, Import), no results with removable filter... | partial (UI) | logged as not built (not re-probed) | Empty and no-results text exist (shared/ds/empty); removable chips, skeleton and error card missing (request id: see Errors) |
| Pagination "Showing 1 to 8 of 128" | done | templates/base/paginate.tmpl (file) | - |
| Mobile: Sort and filter bottom sheet with result count on the primary button | done | templates/shared/repo_search.tmpl:138 | - |
| Mobile: Pull to refresh; infinite scroll after 30 rows with Load more fallback | missing (UI) | logged as not built (not re-probed) | Classic pagination only. New explore-more.js plus a list-fragment route |

#### ExploreOrgs (ASC Hub explore organizations)

Area shell/settings. Rows 6: done 2, partial 1, missing 3. Copy found in the hub: 67% of 109 copy strings (mobile twin 47%). Template(s): user_list.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Teams that own repositories, with who is in them.; Has agents; I am a member; Checkout, design system and storefront tooling.; Active Yesterday; Agent workflows, evaluations and the playbook library..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Org row: avatar, name, description, repositories and people counts | done | templates/explore/user_list.tmpl:6 | - |
| Verified badge (domain proof) and explainer card | done | templates/admin/federation/user_list.tmpl (file) | - |
| Agents count; member stack with up to four avatars and +N | missing (hub+asc) | logged as not built (not re-probed) | Members: organization.GetOrgUsersByOrgID (hub). Agents count: asc, as for Explore users |
| 12-week sparkline and "Active 12 min ago" | partial (hub) | logged as not built (not re-probed) | Sparkline partial; org activity from the action table |
| Chips All, Verified, Has agents, I am a member; Size filter; Sort most active | missing (hub) | logged as not built (not re-probed) | routers/web/explore/org.go; membership filter is simple, the others depend on rows above |
| View hover action; Start an organization card; grid layout; states | missing (UI) | logged as not built (not re-probed) | user_list.tmpl, explore/rail.tmpl, layout toggle |

#### ExploreUsers (ASC Hub explore people and agents)

Area shell/settings. Rows 13: done 2, partial 5, missing 6. Copy found in the hub: 67% of 145 copy strings (mobile twin 80%). Template(s): user_list.tmpl, users.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: The people and agents who write and review here, with how much they do.; Search by name, handle or role; Online now; accounts: 34 people and 8 agents; Staff engineer. Design reviews, flaky tests and anything that touches checkout.; Bengaluru.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| "People and agents" header and Invite people button | partial (UI) | logged as not built (not re-probed) | explore/navbar.tmpl offers New repository / New organization only; invite needs a hub-level invite flow (org invite exists) |
| Search by name, handle or role | done | templates/explore/users.tmpl (file) | - |
| Show filter (People and agents / People / Agents), Organization filter, Sort "Most active" | missing (hub) | logged as not built (not re-probed) | Sort has Newest, Oldest, A to Z only. Add activity sort and organization filter in routers/web/explore/user.go / user_model.Searc... |
| Chips All, People, Agents, Maintainers, Online now | partial (hub+asc) | logged as not built (not re-probed) | None present. Agents are not hub users (they are tokens and runs in proto/asc/v1/agents.proto); Maintainers needs a role; Online... |
| Role chip (Owner, Maintainer, Member, Agent) and "Roles on this hub" card | missing (hub) | logged as not built (not re-probed) | No hub-level role beyond site admin. Define Owner = site admin, Maintainer = write on any repo, Member; compute in the search; us... |
| Agent rows: violet square avatar, @org/name, owner, scope, View runs | missing (hub+asc) | logged as not built (not re-probed) | asc has repo-scoped ListTokens and ListRuns; no hub-wide agent directory. asc: ListAgents(owner) RPC; hub: row type in user_list.... |
| Meta row: location, joined, organizations, public email as own labelled item | missing (UI) | logged as not built (not re-probed) | user_list.tmpl shows location, email, joined; organizations missing |
| Activity sparkline (12 weeks, text equivalent), counts "changesets, reviews" | partial (hub) | logged as not built (not re-probed) | No sparkline component anywhere. Weekly series from models/activities/user_heatmap.go or pull and review tables, cached; partial... |
| Hover action Follow; View runs for agents | partial (UI) | logged as not built (not re-probed) | Follow endpoint exists (profile htmx); row button in user_list.tmpl |
| Rail "Who is active now" with presence and current activity | missing (hub+asc) | logged as not built (not re-probed) | No presence model. Hub: last-seen heartbeat (models/user); asc: run state for agents (GetRun) |
| Grid layout | done | see where-cell | - |
| States: loading, no people match, filter chips, error | partial (UI) | logged as not built (not re-probed) | Empty text exists; rest as in Explore |
| Mobile: Chips filter People, Agents, Maintainers, Online now; Follow inside the row at 44 px | missing (hub+asc) | logged as not built (not re-probed) | As above |

#### GetStarted (ASC Hub get started)

Area shell/settings. Rows 10: done 4, partial 1, missing 5. Copy found in the hub: 54% of 85 copy strings (mobile twin 57%). Template(s): home.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Your hub is empty. Let's fix that.; Four steps from nothing to your first reviewed stack. Skip any step; this page...; Sign the command line in and trust this device. The command line is how you wor...; Start a new repository; Import an existing project; Invite teammates.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Four-step checklist with aria-current=step, done as a word | done | templates/asc/get_started.tmpl:1 | the where-cell cites aria-current=step, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Shown on GET / for a signed-in person with no repositories; dismissible; returns while none ex... | missing (hub) | logged as not built (not re-probed) | Nothing links to /-/get-started or /user/welcome; routers/web/home.go and asc_dashboard.go do not branch on NumRepos == 0. Branch... |
| Two parallel first-run pages (asc/get_started.tmpl, user/asc_welcome.tmpl with old hub- classe... | done | user/asc_welcome.tmpl (not found in tree) | the where-cell cites user/asc_welcome.tmpl, routers/web/asc_welcome.go, not in the tree (renamed or stale text); behaviour taken... |
| Step 2 ticks by itself when the first CLI sign-in arrives, no reload | missing (hub) | logged as not built (not re-probed) | GSConnected is "has an access token" at render time. Add a status route (token last used by the asc CLI) and a poll in a new asc-... |
| Import step with source tabs and progress "Reading history 12,408 commits" | missing (hub) | logged as not built (not re-probed) | Links to /repo/migrate today. Inline form plus migration-task polling (routers/web/repo/migrate.go, services/migrations) |
| Health check (Git found, signed in, SSH key, shell shim; asc doctor on machine and server) | partial (hub+asc) | logged as not built (not re-probed) | Hub card lists email, SSH key and token only. asc doctor exists (crates/asc/src/cli/doctor.rs) but never reports to the hub: add... |
| "This hub" card: Storage, AI providers, Email with Change and Add | done | see where-cell | - |
| Shortcuts card | done | see where-cell | - |
| "Open the sample project" read-only | missing (hub) | logged as not built (not re-probed) | No sample repository; seed one at first start (routers/init.go, services/repository) |
| Empty-area cards: inbox zero on day one, no repositories, "just you" with Invite | missing (UI) | logged as not built (not re-probed) | asc_home.tmpl has an inbox-zero block; add the other two |

#### Inbox (ASC Hub inbox)

Area shell/settings. Rows 15: done 6, partial 2, missing 7. Copy found in the hub: 53% of 79 copy strings (mobile twin 45%). Template(s): asc_inbox.tmpl, notification.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; for shortcuts; Sorted by: oldest waiting; Mark done; "Can we keep the old cursor format for one release? Mobile clients pin to it."; Mark done.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Tabs Needs you, Waiting on others, Agents, Done with counts (hub adds Drafts) | done | routers/web/user/asc_inbox_page.go:26 | - |
| Filters: All repos, Humans and agents, Sorted by | done | see where-cell | - |
| Unread dot plus bold title | missing (hub) | logged as not built (not re-probed) | No unread state (no unread in the template). last_seen per pull and user in models/issues/asc_inbox_state.go (migration) |
| Row hover actions (Review, Answer, Mark done, Snooze) | missing (UI) | logged as not built (not re-probed) | Rows are a link only; actions posting to /-/asc/inbox/state |
| Select rows (x, checkbox); bulk bar "2 selected: Mark done, Snooze, Unwatch" in a live region | missing (hub) | logged as not built (not re-probed) | None. AscInboxStatePost takes one pull; accept many, add unwatch, selection JS in asc-keys.js |
| Mark done (e) with fade and undo toast; Done keeps 30 days and can be restored | partial (hub) | logged as not built (not re-probed) | Only pin, unpin, snooze, wake exist; Done tab lists merged items. Add done and restore with expiry in asc_inbox_state.go; asc inb... |
| Snooze menu: Later today 17:00, Tomorrow 09:00, Monday 09:00, When the checks finish, Pick a d... | missing (hub+asc) | logged as not built (not re-probed) | Menu has 1 h, 4 h, tomorrow, week. "When the checks finish" needs a check-completion wake (asc ci.proto events); date picker is UI |
| Detail pane: why waiting, changed since last look, policy line (approvals 1 of 2), thread, rep... | done | templates/user/asc_inbox.tmpl:167 | - |
| "Ask agent to change" (comment becomes an instruction for the author agent) | missing (hub+asc) | logged as not built (not re-probed) | Reply form has Approve, Request changes, Reply only. Hub: POST to CreateRun or SendInput; asc: instruction flag on review comment... |
| Keys j k, x select, e done, s snooze, a approve, r request changes, p pin, Enter | partial (hub) | logged as not built (not re-probed) | j k s p g i present; x, e need the new states; a and r submit the form with confirm |
| Pagination "Showing 8 of 23" | done | templates/user/asc_inbox.tmpl (file) | - |
| New items slide in from the top without jumping the list | missing (hub+asc) | logged as not built (not re-probed) | No live update. Hub poll or SSE; asc inbox.proto Count is poll friendly |
| States: bulk bar, inbox zero with Watch more repositories, loading skeleton | done | templates/user/asc_inbox.tmpl:30 (best match) | - |
| Subscriptions link to Settings, Notifications | done | templates/user/asc_inbox.tmpl:110 (best match) | - |
| Mobile: Swipe right to mark done, left to snooze; sticky action bar with Open next | missing (hub) | logged as not built (not re-probed) | New touch JS; done state |

#### Install (ASC Hub first-run setup)

Area shell/settings. Rows 8: done 1, partial 1, missing 6. Copy found in the hub: 57% of 84 copy strings (mobile twin 47%). Template(s): install.tmpl, post-install.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Installation guide; SQLite is fine for a team of ten. Choose PostgreSQL for more.; One file. No server to run.; For teams and production.; Test connection; Connected to PostgreSQL 16.4.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Setup form: database cards, general settings, administrator, email and service options | done | templates/install.tmpl:54 | - |
| Test connection with result ("Connected to PostgreSQL 16.4" or the cause); Install disabled un... | missing (hub) | logged as not built (not re-probed) | No test endpoint; routers/install/install.go checks on submit only. Add POST /install/test-db reusing checkDatabase, JS in instal... |
| Database error card with "Test again" and "Use SQLite instead" | missing (UI) | logged as not built (not re-probed) | Error shows as a form flash today; card and actions in install.tmpl |
| Sticky summary rail (Database, URL, Registration) and live step progress | missing (UI) | logged as not built (not re-probed) | install.tmpl has the step list (ins-steps), static; live summary in install.js |
| "Setting up your hub" progress page (Step n of 5) that polls, then redirects | missing (hub) | logged as not built (not re-probed) | SubmitInstall runs synchronously. Run steps as a job with a status route, poll with aria-live=polite |
| Post-install page: signed in as, asc auth login --host ... with Copy, Open the hub, Download A... | partial (UI) | logged as not built (not re-probed) | templates/post-install.tmpl exists; compare and add the command card |
| Skip administrator: first registered user becomes owner | missing (hub) | logged as not built (not re-probed) | Installer needs an admin today; add the skip path and "first user is admin" in routers/web/auth/auth.go |
| Mobile: One section per screen with progress bar, Back and Next | missing (UI) | logged as not built (not re-probed) | install.tmpl, install.js stepper under 768 px |

#### Main (ASC Hub landing)

Area shell/settings. Rows 12: done 8, partial 4, missing 0. Copy found in the hub: 83% of 220 copy strings (mobile twin 74%). Template(s): home.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: ASC 1.4: agent runs and the merge queue are here; .deb .rpm; signature verified by the installer; brew install asc; asc 1.4.2 (minisign verified); macOS, Linux and Windows. This hub also serves a git-compatible remote over HTT....

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Hero: announcement pill, headline, lead, Download ASC / Create account / Sign in | partial | templates/home.tmpl:9 startpage.pill | Pill copy differs: hub Built for teams where agents write a lot of the code, board ASC 1.4: agent runs and the merge queue are he... |
| Download ASC goes to /download and preselects the detected OS | done | templates/home.tmpl:14,305 (href /download, detected OS in routers/web/asc_download.go) | - |
| Install method pills (Homebrew, Scoop, winget, curl, PowerShell, .deb .rpm) as a labelled grou... | done | modules/ascdownload/offer.go:60 | - |
| Product window in the hero (stack rail, change with intent, comment, whose turn) | done | templates/home.tmpl:58 | - |
| Works-with strip | partial | templates/home.tmpl:72 | Heading differs: hub Works with the tools your team already uses, board Works with the editors and agents your team already uses. |
| Why ASC, stacks, review, code intelligence, policy cards | partial | templates/home.tmpl:85-104 startpage.why_* | The board's Why ASC block (Agents write faster than people can read, three problem cards) is not there; the hub has an older Why... |
| Operation log and merge queue demo | done | templates/home.tmpl:160 | - |
| Agents band and run preview | done | templates/home.tmpl:210 | - |
| Workflow cards (dependency upgrade, flaky triage, large migration) | done | templates/home.tmpl:226 | - |
| Bring your own keys and storage (provider rows, S3 endpoint, bucket, region) | done | templates/home.tmpl:264 | - |
| Final call to action and footer | partial | templates/home.tmpl:338; templates/base/footer_content.tmpl | CTA copy differs (Import a repository in a minute... vs Install ASC in a minute, import a repository with its history...); footer... |
| Mobile: Sticky bottom bar with Download ASC and Sign in; install pills scroll sideways; workflow cards... | done | templates/home.tmpl:31 | - |

#### NewOrg (ASC Hub new organization)

Area shell/settings. Rows 3: done 1, partial 0, missing 2. Copy found in the hub: 62% of 29 copy strings (mobile twin 70%). Template(s): create.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Who owns it; Create a first team; A team called Everyone, with you as lead.; You can change this in organization settings later.; ASC Hub 1.4.2.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Handle field, visibility, create | done | templates/org/create.tmpl (file) | - |
| Display name field, "Who owns it", "Create a first team Everyone", "Allow agents" switch | missing (hub+asc) | logged as not built (not re-probed) | hub: forms.CreateOrgForm + create.tmpl; asc: agents flag per org (see OrgSettings) |
| Handle availability check as you type, reserved-name error, live preview card, loading button | missing (UI) | logged as not built (not re-probed) | create.tmpl + JS (availability endpoint is hub S) |
| Board note: Handle availability checks as you type | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Preview updates live | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, handle taken (error), handle reserved, creating (button loading), created toast | unverified | not tracked in the gap files | needs a row and a test |

#### OrgPeople (ASC Hub organization people)

Area shell/settings. Rows 7: done 1, partial 2, missing 4. Copy found in the hub: 62% of 80 copy strings (mobile twin 61%). Template(s): add_org_member.tmpl, members.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; London and remote; Everyone 38; Owners 3; Pending 2; Change role.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Member rows with avatar, owner/member chips, 2FA chip, remove/leave, public/private toggle | done | templates/org/member/members.tmpl:47 | - |
| Search box, filter chips with counts (Everyone, Owners, 2FA off done; Pending needs the invita... | missing (UI) | logged as not built (not re-probed) | members.tmpl + routers/web/org/members.go query params |
| Change role menu with Maintainer role and undo toast; last owner cannot be demoted | missing (hub) | logged as not built (not re-probed) | org members are owner/member only (teams carry access); new role model or Maintainer team mapping in routers/web/org/members.go |
| Row detail: teams list, last active, changesets count | missing (hub+asc) | logged as not built (not re-probed) | hub: teams and last login; asc: authored-changeset count per person (no stats RPC) |
| Agents section (agent identity, run by, scoped to paths, Manage) | partial (hub+asc) | logged as not built (not re-probed) | asc: org-owned agent identities (only per-repo AgentTokens exist); hub: members.tmpl section |
| Pending invitations (email, role, invited by, Resend, Revoke) | partial (hub) | logged as not built (not re-probed) | team invite table exists; list + resend + revoke in members.tmpl |
| Invite dialog accepting several emails and a role | missing (hub) | logged as not built (not re-probed) | templates/org/member/add_org_member.tmpl takes one user; extend routers/web/org/members.go |

#### OrgProfile (ASC Hub organization profile)

Area shell/settings. Rows 8: done 4, partial 1, missing 3. Copy found in the hub: 64% of 58 copy strings (mobile twin 62%). Template(s): header.tmpl, home.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; London and remote; Find a repository; Sort: Recently updated; Customer dashboard and checkout.; Terraform, queues and rate limits..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Head: avatar, name, description, location, website, Follow, Org settings | done | templates/org/header.tmpl:1 | - |
| Verified badge; "38 people, 4 agents" counts | missing (hub+asc) | logged as not built (not re-probed) | See ExploreOrgs |
| Tabs Repositories, People, Teams, Packages, Projects with counts | done | templates/org/menu.tmpl:1 | - |
| Repo rows with check status text (Passing, 1 failing, No checks) and a Watch toggle with count | missing (hub) | logged as not built (not re-probed) | Rows are the shared explore/repo_list.tmpl. Default-branch check status (commit status or asc ci.proto) and a row watch toggle mi... |
| Type and Sort menus land in the URL | done | templates/shared/repo_search.tmpl (file) | - |
| Side cards: People, Teams, Agents, Merge policy with "Read the policy" | partial (hub+asc) | logged as not built (not re-probed) | People and Teams exist (org/home.tmpl); Agents and Merge policy missing. Policy from asc GetPolicy (repo level) or an org policy;... |
| Member view shows Org settings; lock chip for private repositories | done | templates/org/header.tmpl:11 | - |
| Loading skeleton; mobile row menu | missing (UI) | logged as not built (not re-probed) | With the row actions |

#### OrgSettings (ASC Hub organization settings)

Area shell/settings. Rows 8: done 5, partial 0, missing 3. Copy found in the hub: 63% of 70 copy strings (mobile twin 57%). Template(s): layout_head.tmpl, navbar.tmpl, options.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Runners and secrets; Upload logo; Lisbon and remote; Default repository visibility; Who can create repositories.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Profile fields, logo upload/remove, visibility, repo-admin team access, repository creation li... | done | templates/org/settings/options.tmpl:52 | - |
| Who can create repositories (owners / maintainers), base permission for members, default repos... | missing (hub) | logged as not built (not re-probed) | models/organization columns, options.tmpl, repo create permission checks |
| Agents card: allow agents, agent changesets need a person, monthly agent spend ceiling | done | routers/web/org/setting/asc_agents.go:23 | - |
| Default merge policy shown + Edit policy (repositories inherit and can only tighten) | done | routers/web/org/setting/asc_agents.go:28 | - |
| Transfer ownership | missing (hub) | logged as not built (not re-probed) | org settings route + services/org owner change, options.tmpl danger zone |
| Delete organization with typed handle | done | templates/org/settings/delete.tmpl:1 | - |
| Owner-only banner and disabled fields for non-owners (settings routes require owner today) | missing (UI) | logged as not built (not re-probed) | routers/web/web.go org settings group, options.tmpl |
| Left menu: AI keys, Storage, Audit, Packages, Webhooks, Applications, Runners, Blocked users,... | done | templates/org/settings/navbar.tmpl (file) | - |

#### OrgTeams (ASC Hub organization teams)

Area shell/settings. Rows 4: done 1, partial 0, missing 3. Copy found in the hub: 68% of 53 copy strings (mobile twin 66%). Template(s): members.tmpl, new.tmpl, repositories.tmpl, teams.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; London and remote; Infra, queues, rate limits and runners.; Cross-team reviewers for money-touching paths.; Tokens, components and docs.; Edit team.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Team cards (name, access, description, avatars, member and repo counts), create/edit/delete te... | done | templates/org/team/teams.tmpl (file) | - |
| Selecting a card loads detail below without leaving (members + repositories with access) | missing (UI) | logged as not built (not re-probed) | teams.tmpl links to separate pages; add htmx panel |
| Policy owner note: team linked to a path pattern in merge policy | missing (hub+asc) | logged as not built (not re-probed) | asc: policy path-owner rule naming a hub team (crates/asc-core/src/policy.rs); hub: team page card |
| Agents as team members with a path scope | missing (hub+asc) | logged as not built (not re-probed) | asc: agent identity + path scope; hub: team members list |
| Board note: Teams is a view of the organization; selecting a card loads detail below without leaving | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Policy owner links a team to a path pattern in merge policy | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Agents can be team members with a path scope | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default with selected team, empty team (add member prompt), team without repositories, de... | unverified | not tracked in the gap files | needs a row and a test |

#### Packages (ASC Hub packages)

Area shell/settings. Rows 7: done 2, partial 0, missing 5. Copy found in the hub: 62% of 61 copy strings (mobile twin 64%). Template(s): list.tmpl, packages_cleanup_rules_edit.tmpl, view.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; London and remote; Filter packages; - published Today; - published Today; design-tokens.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Package list with type, version, downloads, filter by type | done | templates/package/shared/list.tmpl:1 | - |
| Type chips with counts (a select today) | missing (UI) | logged as not built (not re-probed) | shared/list.tmpl |
| Visibility chip per package (Public/Private/Internal) | missing (hub) | logged as not built (not re-probed) | packages inherit owner visibility; per-package flag in models/packages |
| Package page views Install/Versions/Dependencies/Settings as tabs | missing (UI) | logged as not built (not re-probed) | templates/package/view.tmpl (single page + /versions) |
| "Published from acme/payments by a landed stack", "Signed with release key" | missing (hub+asc) | logged as not built (not re-probed) | asc: publish provenance and release signature record; hub: package metadata + view.tmpl |
| Cleanup rule preview ("removes 14 versions of 6 packages, frees 1.3 GB") | done | templates/org/settings/packages_cleanup_rules_preview.tmpl:3 | - |
| Error state with request id and Status page link | missing (UI) | logged as not built (not re-probed) | package list handler error render |

#### Profile (ASC Hub profile)

Area shell/settings. Rows 13: done 4, partial 2, missing 7. Copy found in the hub: 58% of 73 copy strings (mobile twin 56%). Template(s): asc_agents.tmpl, asc_heatmap.tmpl, header.tmpl, profile.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Online, local time 14:20; Request a review; Draft: provider failover table; Fix clock skew in token expiry; Customer dashboard and checkout. Cursor pagination for every list..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Header: avatar, name, handle, location, bio, followers, following, joined, website, Follow | done | templates/user/overview/header.tmpl (file) | - |
| Presence pill ("Online, local time 14:20") | missing (hub) | logged as not built (not re-probed) | None. Last-seen model plus the person's time zone; pill in header.tmpl with text |
| Role line ("Staff engineer - Payments team - London") | missing (UI) | logged as not built (not re-probed) | Combine existing description, location and team names |
| Request a review (picker with repository and change) and Mention | partial (hub+asc) | logged as not built (not re-probed) | Absent. Hub: picker modal of the viewer's open pull requests (routers/web/user/profile.go) and POST to asc RequestReview (exists... |
| Tabs Overview, Work (11), Reviews (34), Repositories (9), Activity | done | templates/user/overview/header.tmpl (file) | - |
| Working-now list (stack, draft, ready) | done | templates/user/profile.tmpl (file) | - |
| Pinned repositories (up to six) with Customize dialog and drag reorder | missing (hub) | logged as not built (not re-probed) | None. Pin table, POST /user/settings/pins, keyboard dialog, htmx |
| Contributions: year switch, summary (changes, reviews, median turnaround), violet cells for ag... | partial (hub+asc) | logged as not built (not re-probed) | One-colour grid exists (asc_heatmap.tmpl) with a text total. Agent attribution needs asc (changeset author kind via changes.proto... |
| "Agents Priya runs" with New agent | missing (hub+asc) | logged as not built (not re-probed) | Not on the profile. Hub must list the person's agents (see Explore users) with scope and counts |
| Recent activity | done | templates/user/profile.tmpl:76 | - |
| "Knows best" (path share of changes and reviews, six months) | missing (hub+asc) | logged as not built (not re-probed) | No code. Aggregate asc LastCommits / blame data per user: new RPC or hub job |
| Teams and Focus chips beside Repositories and Organizations | missing (UI) | logged as not built (not re-probed) | Teams card is on the profile (templates/user/profile.tmpl wk-pteams, members only). Open: Focus chips from repository topics |
| States: owner view (Edit profile, View as visitor), private-profile banner, empty pins and con... | missing (UI) | logged as not built (not re-probed) | "View as visitor", the private banner (name and shared repositories only) and the empty rows missing |

#### Projects (ASC Hub projects)

Area shell/settings. Rows 5: done 1, partial 0, missing 4. Copy found in the hub: 54% of 48 copy strings (mobile twin 56%). Template(s): list.tmpl, new.tmpl, view.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; London and remote; Release 2.19; Everything planned for the next release, with the real state of each stack.; Add card; Webhook receivers back off per host.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Kanban board with columns, drag, edit/close/delete | done | templates/projects/view.tmpl:82 | - |
| Cards that are stacks following real state; Landed column by stack state | missing (hub) | logged as not built (not re-probed) | project_issue model holds issues/PRs; map PullRequestAscStack state to a column, issue/card.tmpl |
| Move menu (keyboard alternative) announcing the new column | missing (UI) | logged as not built (not re-probed) | view.tmpl + web_src/js/features/repo-projects.js |
| Add card, Switch project select, rename and hide columns | missing (UI) | logged as not built (not re-probed) | view.tmpl, column hide flag (small migration) |
| Empty, loading and error ("request id") states | missing (UI) | logged as not built (not re-probed) | projects/list.tmpl |
| Board note: Cards move between columns by drag or by the Move menu; a stack card follows its real sta... | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Cards link to the stack or the issue | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Columns can be renamed and hidden | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Project board with four columns of stacks and issues; empty; loading; error | unverified | not tracked in the gap files | needs a row and a test |

#### Shell (ASC Hub shell: header, menus, palette, footer)

Area shell/settings. Rows 23: done 15, partial 1, missing 7. Copy found in the hub: 64% of 171 copy strings. Template(s): footer_content.tmpl, head_navbar.tmpl. Mobile twin: no.

Board copy not found word for word in locale, templates or scripts: Four links, one clear way in; Search always one key away, Inbox always one click away; Account, create and notifications; New changeset; Mark all read; approved Backoff with jitter.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Signed-out header: Explore, Download, Docs, About, quiet Sign in, cyan Create account | done | templates/base/head_navbar.tmpl:5 | - |
| Docs and About links (header and footer) open the hub's own pages (/-/docs, /-/about, both rou... | done | templates/base/head_navbar.tmpl:14 | - |
| Signed-in header: Changes, Agents, Explore, Download, search field with / and Ctrl K hints, In... | done | templates/base/head_navbar.tmpl:3 | - |
| Active link: white label, 2px cyan marker, aria-current=page | done | templates/base/head_navbar.tmpl:85 | the where-cell cites aria-current=page, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Inbox amber count from /-/asc/inbox/count, cached 60 s | done | web_src/js/features/asc-keys.js:196 | - |
| "N agents running" pill in the header (cyan dot, links to Agents) and in the mobile top bar | missing (hub+asc) | absent: no agents-running text in templates/base, locale, asc-keys.js, routers/web/web.go | Not in any template or locale. Hub: GET /-/asc/agents/count beside AscInboxCount, pill in head_navbar.tmpl (desktop and .ds-topba... |
| Notifications bell opens a popover (unread count, Mark all read, items with avatar, text, repo... | missing (hub) | absent: no popover markup or fragment route for notifications in templates/base, ds-shell.js, route... | Bell is a plain link to /notifications (head_navbar.tmpl). Add a fragment route in routers/web/user/notification.go (reuse Notifi... |
| Account menu: identity, profile, inbox with count, agents, settings, Appearance (Light, Dark,... | done | templates/base/head_navbar.tmpl:97 | the where-cell cites #ds-account, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Theme and density persist per person: localStorage, then the account setting | missing (hub) | absent: no density or theme column in models/user or routers/web/user/setting | Only localStorage (asc-theme-mode, asc-density) in head_style.tmpl. Add a user setting (models/user/setting.go), save on change (... |
| Create menu: New repository, Import a project, New organization, New changeset, Start an agent... | missing (UI) | logged as not built (not re-probed) | New repository, Import, New organization and "Start an agent run" are in #ds-create (head_navbar.tmpl). Open: "New changeset" (ne... |
| Menus close on Esc or outside click, return focus to trigger, fade and rise, reduced motion | done | web_src/js/features/ds-shell.js (file) | - |
| Command palette opens from /, Ctrl K, header search, mobile search; Esc, arrows, Enter | done | web_src/js/features/ds-shell.js:611 | - |
| Palette is a combobox: role=combobox input, listbox results, aria-activedescendant | done | templates/base/head_navbar.tmpl:275 | the where-cell cites role=dialog, role=search, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Palette scope chips All, Changes, Code, People, Commands; Tab cycles the scope | done | templates/base/head_navbar.tmpl:271 | - |
| Palette results grouped Changes (title, repo and number), Code (path, symbol), People, fetched... | partial (asc) | routers/web/asc_palette.go:34-60 GET /-/asc/palette (groups changes, people, repos); web_src/js/fea... | Server results exist for Changes, People and repositories; the Code group (path, symbol from asc CrossSearch) was not found. |
| Command verbs (undo, restack, merge) switch the list to actions; Commands group with Undo last... | missing (hub) | absent: no undo or restack command text in locale, templates/base or ds-shell.js | Static Commands group has New repository and New organization only. asc RPCs exist: UndoRef, Restack, RequestMerge, CreateRun. Hu... |
| Empty query shows Recent items and Shortcuts (Go to inbox: g i) | done | web_src/js/features/ds-shell.js:186 | - |
| No-results text names the query and suggests a symbol, handle or title | done | see where-cell | the where-cell cites asc_shell.palette_empty, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Keyboard shortcuts overlay (?, "All shortcuts") | done | web_src/js/features/asc-keys.js (file) | - |
| Footer: full footer on public pages (four groups, version, Based on Forgejo, language, theme s... | done | templates/base/footer_content.tmpl:1 | - |
| Footer links: Stacks and changes, Agent runs, Changelog, CLI reference, REST API, MCP server,... | missing (UI) | absent: Acceptable use, Subprocessors, MCP server not in locale or templates/base | Today: Download, Explore, Users, Organizations, Docs, Operations, API, Why, Licenses. Add links (docs pages or [asc] settings URL... |
| "All systems operational" status in the footer | missing (hub) | absent: no systems-operational status in locale, templates/base or routers/web | Not present. Cached value from /healthcheck plus asc serve health, or a configured status URL; footer_content.tmpl |
| "Rendered in 18 ms" in the footer | done | see where-cell | - |

#### UserSettingsAI (ASC Hub settings: ai keys)

Area shell/settings. Rows 8: done 6, partial 0, missing 2. Copy found in the hub: 64% of 114 copy strings (mobile twin 61%). Template(s): asc_ai.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Bring your own API keys. Agents use the keys of their own scope, and the cost g...; Personal Anthropic; Embeddings key; - text-embedding-3-large - tested yesterday.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Key list with provider, hint, model, tested time, spend meter, Test/Details; add form; state c... | done | routers/web/shared/byoweb/aikeys.go (file) | - |
| Test connection result with latency (ms), polite live region | done | see where-cell | - |
| Test result lists model count ("9 models available") | done | routers/web/shared/byoweb/aikeys.go (file) | - |
| Key detail tabs (Usage, Limits, Models, Activity) and spend-by-day chart | missing (UI) | logged as not built (not re-probed) | byo_ai_key.tmpl is one long page with a by-day table; split into tabs, add a small bar chart |
| Warn me at 80% (inbox + email), Stop agents at the cap (pause with a question) | done | routers/web/shared/byoweb/aishares.go (file) | - |
| Who can use it: allow my agents, share with organization (org owners attach it, see spend not... | done | routers/web/shared/byoweb/aishares.go (file) | - |
| Activity tab (who used the key, runs, tokens) | missing (hub+asc) | logged as not built (not re-probed) | asc: AiProviders.Usage groups by day/model/repo/agent only, no event list; hub: new tab |
| Masked key field with Show button; remove needs a ticked box | done | see where-cell | - |

#### UserSettingsAccount (ASC Hub settings: account)

Area shell/settings. Rows 5: done 2, partial 1, missing 2. Copy found in the hub: 69% of 68 copy strings (mobile twin 61%). Template(s): account.tmpl, repos.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Email addresses, sign-in methods and what happens to your data.; Notifications and sign-in alerts go to your primary address.; Receives notifications and sign-in alerts; Waiting for you to click the link we sent.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Email list, primary, verify/resend, add, delete | done | templates/user/settings/account.tmpl (file) | - |
| Resend rate limit that says when to retry | missing (hub) | logged as not built (not re-probed) | routers/web/user/setting/account.go (EmailPost SENDACTIVATION) |
| Sign-in methods card (SSO connected, link to Security for passkeys/password) | done | templates/user/settings/security/accountlinks.tmpl (file) | - |
| Export your data (changesets, reviews, comments, settings; emailed link; requested/ready state... | missing (hub) | logged as not built (not re-probed) | done: Sessions-style page /user/settings/asc/export (request, states, download, delete) over DataExport (routers/web/user/setting... |
| Delete account: typed username, 2FA code, blocked state when repos are owned, dialog focus ret... | partial (UI) | logged as not built (not re-probed) | Typed username and the blocked-by-repositories state are done (account.go DeleteAccount, account.tmpl). Open: 2FA code step and d... |
| Board note: Primary address is changed from a menu on the verified row | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Delete requires typing the username and, with 2FA on, a code | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Resend is rate limited and says when you can try again | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Left menu is the only navigation for settings: no tabs repeat these pages | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, unverified address, address added toast, delete confirmation dialog (typed usern... | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Export requested and ready states | unverified | not tracked in the gap files | needs a row and a test |

#### UserSettingsAppearance (ASC Hub settings: appearance)

Area shell/settings. Rows 8: done 6, partial 0, missing 2. Copy found in the hub: 68% of 63 copy strings (mobile twin 58%). Template(s): appearance.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Make ASC Hub comfortable. Changes apply immediately on every device you are sig...; Choose how ASC Hub looks. System follows your device and switches at sunset.; Small switches that change how the interface feels.; Agent runs, the running-agents chip and Ask an agent. Off hides them everywhere....

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Theme cards (light, dark, system) with preview | done | templates/user/settings/appearance.tmpl (file) | - |
| Density (comfortable, compact) | done | templates/base/head_style.tmpl:4 | - |
| Language select; hints switch; hidden comment types | done | templates/user/settings/appearance.tmpl:112 (best match) | - |
| Show agent features switch on this page (exists on /-/asc/agents only) | done | templates/user/settings/appearance.tmpl:59 (best match) | - |
| Reduce motion and Keyboard shortcuts switches | done | web_src/css/asc-ds/base.css:249 | - |
| Hide resolved comments switch | missing (hub) | logged as not built (not re-probed) | user setting + templates/repo/issue/view_content/conversation.tmpl |
| Date and time format (12/24 h, order) | missing (hub) | logged as not built (not re-probed) | user column, modules/timeutil, DateUtils in modules/templates |
| Apply instantly with an undo toast (no Save step); an app-wide toast/undo component does not e... | done | web_src/js/features/ds-toast.js (file) | - |

#### UserSettingsNotifications (ASC Hub settings: notifications)

Area shell/settings. Rows 6: done 4, partial 0, missing 2. Copy found in the hub: 78% of 74 copy strings (mobile twin 78%). Template(s): notification_subscriptions.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Choose a channel for each kind of event. Needs-you items always land in the inb...; Rules for repos you can write to; Digest and rate limits.; Right away, at most one per 5 minutes per thread.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Page itself (not in the settings menu; only the Forgejo email preference on Account and /notif... | missing (hub) | logged as not built (not re-probed) | templates/user/settings/navbar.tmpl, new templates/user/settings/notifications.tmpl, route in routers/web/web.go |
| Event x channel matrix (Inbox/Email/Push for review requested, mention, agent question, run fi... | missing (hub+asc) | logged as not built (not re-probed) | hub: new model asc_notify_pref + migration + dispatch in services/notify and routers/web/user/asc_inbox.go; asc: proto/asc/v1/inb... |
| Email frequency (right away / per-thread limit) and daily summary at a time | done | see where-cell | - |
| Quiet hours (from/until, time zone aware) | done | see where-cell | - |
| Push channel, permission prompt and denied state | done | see where-cell | - |
| Watching table with level and last notified, Unwatch with undo | done | templates/user/notification/notification_subscriptions.tmpl:13 (best match) | - |

#### UserSettingsOrgs (ASC Hub settings: organizations)

Area shell/settings. Rows 3: done 2, partial 0, missing 1. Copy found in the hub: 75% of 52 copy strings (mobile twin 68%). Template(s): organization.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Teams you belong to and invitations waiting for you.; Where you are a member or owner.; Rail Guild; Join with one click..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Organization rows with role chip, "38 people - 14 repositories", Settings for owners | done | templates/user/settings/organization.tmpl:1 | - |
| Pending invitations with Accept and Decline | missing (hub) | logged as not built (not re-probed) | team invites are token mails (routers/web/org TeamInvite); add listing by user email + accept/decline |
| Last owner cannot leave (explains transfer first) | done | templates/user/settings/organization.tmpl:32 (best match) | - |
| Board note: Leave asks to confirm; the last owner must transfer first | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Left menu is the only navigation for settings: no tabs repeat these pages | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, pending invitation, empty (illustration + New organization), last owner cannot l... | unverified | not tracked in the gap files | needs a row and a test |

#### UserSettingsPrivacy (ASC Hub settings: privacy)

Area shell/settings. Rows 5: done 3, partial 1, missing 1. Copy found in the hub: 75% of 57 copy strings (mobile twin 70%). Template(s): blocked_users.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Agents in your organizations can see your name, role and focus.; Casey Brandt; promo-bot-77; @promo77.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Page itself; Show activity and Hide email switches live on Profile instead | done | see where-cell | - |
| Show when I am online (green presence dot) | done | see where-cell | - |
| Allow agents to read my profile | partial (hub+asc) | templates/user/settings/privacy.tmpl (sp_agents_profile) | Switch exists; asc honouring it when an agent reads the profile was not found. |
| Blocked people list, block by username, unblock | done | templates/user/settings/blocked_users.tmpl (file) | - |
| Block confirm dialog, 10 s undo on unblock; blocking also drops pending review requests (only... | missing (hub) | logged as not built (not re-probed) | services/user/block.go, blocked_users.tmpl, toast component |
| Board note: Unblock asks no questions and is undoable for 10 s | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Blocking someone removes their follows and pending review requests | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Left menu is the only navigation for settings: no tabs repeat these pages | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, empty blocked list, block confirmation dialog, username not found | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Switches save immediately with an undo toast | unverified | not tracked in the gap files | needs a row and a test |

#### UserSettingsProfile (ASC Hub settings: profile)

Area shell/settings. Rows 8: done 5, partial 0, missing 3. Copy found in the hub: 79% of 75 copy strings (mobile twin 78%). Template(s): profile.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; PNG, JPG or SVG, up to 1 MB. Shown at 24 to 96 px.; This is what people find when they look you up.; Changing it redirects the old profile address for 90 days.; (UTC+00:00) Lisbon.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Photo upload and remove (initials placeholder) | done | templates/user/settings/profile.tmpl (file) | - |
| Photo preview before saving; upload-failed error text | done | web_src/js/features/asc-ui.js (file) | - |
| Display name, username (redirect hint), bio, pronouns, location, website, visibility radio | done | templates/user/settings/profile.tmpl:53 (best match) | - |
| Username availability check after 400 ms of no typing | missing (hub) | logged as not built (not re-probed) | new GET route in routers/web/web.go under /user/settings + handler in routers/web/user/setting/profile.go; JS in asc-ui.js |
| "How others see you" live preview card | done | see where-cell | - |
| Public email select (verified addresses), time zone select | missing (hub) | logged as not built (not re-probed) | models/user (new columns + models/forgejo_migrations), forms.UpdateProfileForm, profile.tmpl; time zone must feed DateUtils and q... |
| Save bar that turns active on edit, names the change count, Discard restores, "Saved" confirma... | done | web_src/js/features/repo-settings.js:10 | - |
| Loading skeleton inputs | missing (UI) | logged as not built (not re-probed) | profile.tmpl + CSS (page is server rendered, low value) |

#### UserSettingsRepos (ASC Hub settings: repositories)

Area shell/settings. Rows 2: done 1, partial 1, missing 0. Copy found in the hub: 74% of 53 copy strings (mobile twin 65%). Template(s): repos.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Repositories you own and ones that need adopting.; Move, rename or delete from each repository's own settings.; Shell and editor setup.; garage-eu.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Owned repositories list, unadopted adopt/delete | done | templates/user/settings/repos.tmpl:47 | - |
| Stack count and storage target per row; Transfer dialog (new owner + repository name) | partial (hub+asc) | logged as not built (not re-probed) | hub: repos.tmpl (repo transfer exists in repo settings); asc: stack counts (Stacks service) and Storage.AssignRepo target |
| Board note: Transfer asks for the new owner and the repository name | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Left menu is the only navigation for settings: no tabs repeat these pages | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, empty unadopted, transfer dialog | unverified | not tracked in the gap files | needs a row and a test |

#### UserSettingsStorage (ASC Hub settings: storage)

Area shell/settings. Rows 6: done 4, partial 0, missing 2. Copy found in the hub: 71% of 108 copy strings (mobile twin 68%). Template(s): asc_storage.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Keep your repository objects in an S3-compatible bucket that you own.; Keep your repository objects in a bucket you own. ASC writes a probe object, re...; S3-compatible buckets you own: Garage, MinIO, Backblaze B2, Cloudflare R2 or Am...; garage-eu.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Targets list and state chips, add form (name, region, endpoint, bucket, prefix, addressing, ke... | done | routers/web/shared/byoweb/storage.go:101 | - |
| Verify handshake: five steps with latency, failure message in words | done | see where-cell | - |
| Per repository assignment ("where new objects go") | done | see where-cell | - |
| Migration progress (copied/total) and cancel | done | web_src/js/features/asc-byo.js:5 | - |
| Migration Pause/resume, bytes and ETA, mismatch count, paused state | missing (hub+asc) | logged as not built (not re-probed) | asc: proto/asc/v1/storage.proto has no pause RPC, no bytes_total, no mismatch counter; hub: byo_storage_repo.tmpl + asc-byo.js |
| Card text "Used by N repositories - verified 2 days ago" | missing (UI) | logged as not built (not re-probed) | byo_storage_cards.tmpl (usage count from the repo list) |

#### UserSettingsUsage (ASC Hub settings: usage and quota)

Area shell/settings. Rows 4: done 4, partial 0, missing 0. Copy found in the hub: 76% of 51 copy strings (mobile twin 77%). Template(s): storage_overview.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; What you used this month across storage, agents and AI.; Resets on 1 November, UTC.; of 5 GB on the server default; of $35 across 2 keys.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Four tiles (object storage, agent minutes, AI spend, bandwidth) with meters and "of N" text | done | templates/shared/asc_usage.tmpl (file) | - |
| By repository table (storage, agent minutes, AI spend) | done | see where-cell | - |
| Over-quota banner, empty month, "BYO does not count" note | done | see where-cell | - |
| Menu entry always shown (hub hides it unless quota is enabled) | done | see where-cell | - |
| Board note: Tiles open the page that explains each limit | unverified | not tracked in the gap files | needs a row and a test |
| Board note: BYO storage and keys do not count against the server quota | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Left menu is the only navigation for settings: no tabs repeat these pages | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, over-quota (banner + upgrade or free space), empty month | unverified | not tracked in the gap files | needs a row and a test |

### Area: repo

#### Changeset (ASC Hub change)

Area repo. Rows 14: done 8, partial 1, missing 5. Copy found in the hub: 67% of 81 copy strings (mobile twin 66%). Template(s): commit_page.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: main@c8a02f1; Browse files; Spread retries so a provider outage does not turn into a synchronized stampede...; The cap on line 23 and the new bounds test. Everything else is mechanical.; , attempt: u32) ->; .base * 2u32..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Page shell with breadcrumb History / id, title, Landed chip, Browse files, Copy id | done | templates/repo/commit_page.tmpl (file) | - |
| "Why this exists" (intent) block | missing (hub) | logged as not built (not re-probed) | derive from the message body of the change (Repo.ReadChangeset header); H:templates/repo/commit_header.tmpl |
| "Where to look" and "Risk" lines | missing (hub+asc) | logged as not built (not re-probed) | asc has no such field in ChangeInfo/LogEntry; produce them through the "Review summaries" AI feature (A:proto new RPC or trailer)... |
| Facts strip: Size, Files, Checks 14 of 14, Risk | done | templates/repo/commit_header.tmpl (file) | - |
| Stack position bar: previous / next change, "Change 2 in retry-with-backoff" | done | templates/repo/commit_header.tmpl:23 | the where-cell cites repo.walk_commits, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Files grouped by meaning (Behaviour, Tests, Generated) with counts, collapsible groups, genera... | missing (hub) | logged as not built (not re-probed) | Repo.Classify is wrapped in H:modules/git/ascbackend/classify.go and rendered as a pill (diff/asc_class_badge.tmpl) on pull files... |
| Collapsed file buttons with aria-expanded, +/- signs | done | templates/repo/diff/box.tmpl:15 | - |
| Review block "2 of 2" with approvers and dates | done | models/issues/review.go:96 | - |
| Checks list with durations | done | templates/repo/commit_statuses.tmpl (file) | - |
| "Where it is now": On main, In release v2.18.0, Stack and position | done | see where-cell | - |
| Revert as a new change (review required) | done | see where-cell | - |
| Undo from the operation log while recent (replaces Revert) | partial (UI) | logged as not built (not re-probed) | /asc/ops/undo exists (H:routers/web/repo/asc_ops.go); link and the "too old" sentence on the commit page |
| Large change state: "412 files changed, showing the 20 that matter most", Show 392 more | missing (hub) | logged as not built (not re-probed) | Repo.Classify ranking, diff/box.tmpl |
| Loading diff state | missing (UI) | logged as not built (not re-probed) | skeleton in diff/box.tmpl |

#### Code (ASC Hub code)

Area repo. Rows 21: done 14, partial 1, missing 6. Copy found in the hub: 61% of 112 copy strings (mobile twin 62%). Template(s): asc_symbols.tmpl, blame.tmpl, view_file.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Go to file or symbol; files touched by open stacks; Diego Alvarez; std::time::; /// How a failed provider call is retried.; attempts: u32,.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| File viewer, line numbers, breadcrumb, raw, copy permalink, large-file card | done | templates/repo/code_view.tmpl (file) | - |
| Segment Code / Blame / History on the file | done | templates/repo/code_view.tmpl (file) | - |
| Blame cell shows author avatar, intent, change id, age | done | templates/repo/blame.tmpl (file) | - |
| Blame hover and focus card (intent as "Why", stack, approver, size, Open change, Blame before... | done | templates/repo/blame.tmpl (file) | - |
| Heat bar paired with age text (recency scale) | done | routers/web/repo/blame.go (file) | - |
| Agent author carries the violet agent mark in blame | done | templates/repo/blame.tmpl:35 (best match) | the where-cell cites H:services/ascidentity, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Click a line number selects and shares a range; selected lines flash once | done | web_src/js/features/repo-code.ts:19 | - |
| Shortcuts b toggle blame, y permalink, l go to line, e propose change, t find file, [ collapse... | missing (UI) | logged as not built (not re-probed) | none bound in H:web_src/js; add to a repo keymap module |
| File tree with aria-current and "N files touched by open stacks" badges per file | missing (hub) | logged as not built (not re-probed) | H:routers/web/repo/view_asc_tree.go builds the tree; add per-path badge from open pulls' changed files (PathChange via Repo.Compa... |
| Tree collapse with [ and Files sheet on mobile | done | templates/repo/code_view.tmpl:10 | - |
| Outline panel (struct / impl / fn with line numbers) | done | routers/web/repo/asc_symbols.go:35 | the where-cell cites GET /{owner}/{repo}/asc/symbols?op=outline&path=, not in the tree (renamed or stale text); behaviour taken f... |
| "Used in / 4 references" code-intelligence panel on the file | done | services/ascsearch/panel.go (file) | - |
| "Open changes touching this file" card (stack, change 3 of 4, lines changed) | done | templates/repo/blame.tmpl:1 (best match) | the where-cell cites H:services/pull, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Shortcuts legend card | done | templates/repo/code_view.tmpl (file) | - |
| Propose change bar ("Changes go into a new change on top of main"), Start a new change / Add t... | partial (hub) | logged as not built (not re-probed) | bar exists (rp-editnote) and links to the Forgejo editor; the editor commits to a branch/PR rather than creating a change or addi... |
| Directory view columns: Last change, In open stack chip, Author, Size, Updated | done | templates/repo/view_list.tmpl:1 | - |
| Loading state (lines stream in) | missing (UI) | logged as not built (not re-probed) | skeleton in view_file.tmpl |
| Not found at this version ("It was added in change mzu2d. You are looking at main@8d1c7e0.", G... | missing (hub) | logged as not built (not re-probed) | H:routers/web/repo/view.go 404 path, Repo.Log for the introducing change |
| Binary preview card | done | templates/repo/view_file.tmpl (file) | - |
| Long-press line number to select and Share (mobile), blame as avatar column with change sheet | missing (UI) | logged as not built (not re-probed) | H:templates/repo/blame.tmpl has the desktop columns only; add mobile column and sheet reusing the hover endpoint |
| Mobile: Avatar column for blame, tap opens the change sheet (swipe down closes), Files sheet, long-pre... | missing (UI) | logged as not built (not re-probed) | see Code rows |

#### Compare (ASC Hub compare)

Area repo. Rows 6: done 4, partial 1, missing 1. Copy found in the hub: 72% of 50 copy strings (mobile twin 71%). Template(s): asc_compare.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Only what the stack adds; . The stack restacks onto main when you land it.; Ahead of main; Only on main; - Diego Alvarez; - Diego Alvarez.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| From / To, swap, "only what the stack adds" checkbox, ahead / behind chips, common ancestor | partial | templates/repo/asc_compare.tmpl | Marked done; the checkbox only what the stack adds was not found. |
| From / To as labelled selects listing shared refs, stacks and tags (text inputs today) | done | templates/repo/asc_compare.tmpl (file) | - |
| Per-change and per-file +/- counts and avatars | done | see where-cell | - |
| Same version state ("Both sides are main@a41f9c2") | done | templates/repo/asc_compare.tmpl (file) | - |
| Unknown ref with "Did you mean retry-with-backoff?" | missing (hub) | logged as not built (not re-probed) | resolve error in asc_compare.go; suggest nearest ref name |
| Open any change or file from the lists, both sides kept in the address | done | see where-cell | - |

#### Dependencies (ASC Hub dependencies)

Area repo. Rows 14: done 10, partial 0, missing 4. Copy found in the hub: 51% of 132 copy strings (mobile twin 46%). Template(s): asc_deps.tmpl, asc_deps_plan.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: What this repository stands on; Refresh advisory data; Findings by severity; Complete, 3 of 3 sources; Critical 0; Medium 1.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Views Findings / Updates / Configuration (?view=) | done | templates/repo/asc_deps.tmpl (file) | - |
| Scan now, refresh advisory data, last-scan line, lookup-incomplete banner | done | templates/repo/asc_deps.tmpl:38 | - |
| Summary tiles: findings by severity bar chart with text table, Outdated 11 (3 with security fi... | missing (UI) | logged as not built (not re-probed) | DepsService.ListFindings/ListUpdates give the counts; add tile row to asc_deps.tmpl |
| Severity chips All / Critical / High / Medium / Low with counts, Show dismissed | done | see where-cell | - |
| Finding row expands (aria-expanded): "Why it matters", "How it gets in" text tree, Propose fix | missing (UI) | logged as not built (not re-probed) | rows are flat in asc_deps.tmpl (path is one line); render the path as a tree and add per-finding Propose fix posting to /asc/deps... |
| Dismiss with required reason and note, Reopen, "until Dec 1" expiry | done | templates/repo/asc_deps.tmpl:93 (best match) | - |
| Updates table: package, current, latest, kind, security, proposal link (Change kq2nv) or Propo... | done | templates/repo/asc_deps.tmpl:147 | - |
| Propose updates panel: Security fixes only, Largest jump, Most proposals, 5-stack limit, Previ... | done | templates/repo/asc_deps.tmpl:170 | - |
| Configuration view: ecosystems, schedule ("Weekly, Mondays 06:00 UTC, next run in 4 days"), th... | done | templates/repo/asc_deps.tmpl:219 | the where-cell cites .asc/deps.toml, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Scan running state with real progress bar ("About 20 seconds left") | missing (asc) | logged as not built (not re-probed) | DepsService.Scan is unary (A:proto/asc/v1/deps.proto); add a scan status/progress RPC or stream, hub polling |
| No findings state ("Checked 214 packages 2 hours ago against 3 sources") | missing (UI) | logged as not built (not re-probed) | asc_deps.tmpl prints a short empty line |
| Never scanned state with package count | done | templates/repo/asc_deps.tmpl:33 (best match) | - |
| Unsupported ecosystem ("2 files are not scanned. tools/build.zig.zon") | done | see where-cell | - |
| Advisory text plain, https-only links | done | templates/repo/asc_deps.tmpl:5 (best match) | the where-cell cites H:services/asctrust, not in the tree (renamed or stale text); behaviour taken from the gap file |

#### GitNote (ASC Hub Git compatibility note)

Area repo. Rows 4: done 3, partial 0, missing 1. Copy found in the hub: 61% of 56 copy strings (mobile twin 79%). Template(s): asc_branchless.tmpl, clone_asc_tabs.tmpl, clone_buttons.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Collapsed (default); Bridge switched off by an admin; Inline, repository home (hero card); Put the asc command first, in its own block; Keep the note to one line when collapsed; Say what Git sees: a read-write view of the same changes.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| One-line collapsed disclosure after the asc command, aria-expanded, URL shown only after expan... | done | templates/repo/clone_asc_tabs.tmpl:1 | - |
| Body copy: "A push arrives as changes and goes through policy... Stacks appear as branches nam... | done | see where-cell | - |
| Bridge switched off by admin: one sentence "The Git remote is off on this hub. Everything work... | done | templates/repo/clone_asc_tabs.tmpl:1 | - |
| Inline variant in the repository home hero card; also in settings Working mode when branches a... | missing (UI) | logged as not built (not re-probed) | home_dir.tmpl, settings/asc_branchless.tmpl, empty.tmpl |
| Board note: The whole row is one disclosure button; Enter and Space open it | unverified | not tracked in the gap files | needs a row and a test |
| Board note: The URL is copyable only after expanding | unverified | not tracked in the gap files | needs a row and a test |
| Board note: When the admin switches the Git remote off the row says so in a single sentence and has n... | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Collapsed, expanded, switched off and inline | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Dark and light through the Theme tweak | unverified | not tracked in the gap files | needs a row and a test |

#### History (ASC Hub history and graph)

Area repo. Rows 16: done 8, partial 2, missing 6. Copy found in the hub: 57% of 88 copy strings (mobile twin 70%). Template(s): asc_graph.tmpl, asc_ops.tmpl, commits.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Filter by message, id or author; Open on top of main; writing now; Needs review; Diego Alvarez; In the merge queue, lands next.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| List view with commit rows, copy hash, view at point, search by message and author, "Open oper... | done | templates/repo/commits.tmpl (file) | - |
| Lane graph from Repo.CommitGraph (colour, lanes, refs, review chip) | done | routers/web/repo/asc_graph.go (file) | - |
| Graph and List as two views of one page with the same rows; graph beside the rows | missing (UI) | logged as not built (not re-probed) | merge asc_graph.tmpl rail into commits.tmpl; the segment links to two pages today |
| Node state shape plus chip (Needs review, Queued, Agent writing); agent vs person nodes; stack... | missing (hub) | logged as not built (not re-probed) | map graph rows to pulls and queue state (H:routers/web/repo/asc_queue.go, pull_asc_stack.go), draw shapes in asc_graph.tmpl |
| "Open on top of main" section listing open changes with stack position, "Waiting on Priya Rama... | missing (hub) | logged as not built (not re-probed) | open pulls + reviewers + agent runs; commits.tmpl |
| Comfy / Compact density switch | done | templates/repo/commits.tmpl:16 | - |
| Filter bar: Everyone / People / Agents, Path, Stack, date range, in the URL, sticky | partial (hub) | logged as not built (not re-probed) | author kind needs agent identity; path and date filters exist in Forgejo RefCommits partly; commits.tmpl, routers/web/repo/commit... |
| Export | done | routers/web/repo/commit.go (file) | - |
| Keyboard: j/k move, Enter opens, c copies id | done | see where-cell | the where-cell cites H:web_src/js, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Row hover actions (copy id, browse files) and hover lift | done | templates/repo/commits_list.tmpl (file) | - |
| "Landing rate, last 12 weeks" card with this-week delta | missing (hub+asc) | logged as not built (not re-probed) | statistics RPC (see Insights) |
| Graph key legend | done | templates/repo/asc_graph.tmpl (file) | - |
| "Show 25 older changes" on-demand load (44 px button on mobile) | done | see where-cell | - |
| States: loading skeleton, no results with explanation and Clear filters, partial-data banner w... | partial (UI) | logged as not built (not re-probed) | commits.tmpl; partial banner needs index lag signal from Repo.CommitGraph (hub AscGraphError path) |
| Mobile rail-and-node list, chips open a bottom sheet for Path, Stack, date | missing (UI) | logged as not built (not re-probed) | commits.tmpl, sheet component |
| Mobile: Rail-and-node list, chips open a bottom sheet, 44 px older-changes button | missing (UI) | logged as not built (not re-probed) | see History rows |

#### Insights (ASC Hub insights: Pulse)

Area repo. Rows 7: done 6, partial 0, missing 1. Copy found in the hub: 76% of 71 copy strings (mobile twin 75%). Template(s): contributors.tmpl, pulse.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: +12% on the previous 30 days; Oct 1 to Oct 30, main; People 68; Agents 80; Median time to the first review, by size of change.; provider-timeouts.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Pulse and Contributors pages exist (Forgejo activity, git contribution graph) | done | templates/repo/pulse.tmpl (file) | - |
| Four tiles: Changes landed (vs previous 30 days), Stacks opened, Median review time, Written b... | done | templates/repo/insights_asc.tmpl (ins_landed, ins_opened, ins_median, ins_agents); routers/web/repo... | Insights service exists in asc master and the hub. |
| Changes landed per day, people vs agents, chart with direct labels, Table toggle | done | templates/repo/insights_asc.tmpl:51 svg chart, table toggle | - |
| Seg controls Everyone / People / Agents and 7 days / 30 days / Year that update chart and tile... | done | templates/repo/insights_asc.tmpl:10-15 ins_who, ins_range segmented controls | - |
| Median review time by week ("Getting faster") and by size of change (Under 50 / 50 to 300 / Ov... | done | templates/repo/insights_asc.tmpl:76-86 by week, by size of change | - |
| Recently landed stacks and "Waiting the longest" list | done | see where-cell | - |
| States: new repository, loading, "Index behind" banner | missing (UI) | logged as not built (not re-probed) | pulse.tmpl |

#### InsightsPeople (ASC Hub insights: Contributors)

Area repo. Rows 5: done 3, partial 1, missing 1. Copy found in the hub: 69% of 68 copy strings (mobile twin 75%). Template(s): contributors.tmpl, pulse.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: People and agents who changed this repository, side by side and labelled.; Changes on main, last 30 days; Diego Alvarez; Hannah Weiss; Every agent change was approved by a person before landing. Median wait for tha...; People who reviewed the most. Agents never review..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Contributors table: landed, reviews given, median review, 12-week sparkline, share, role, "Age... | done | templates/repo/insights_people.tmpl (people_weeks, run_by, people_share) | - |
| Who writes the code (46/54) with "every agent change approved by a person" sentence | done | templates/repo/insights_people.tmpl (who_writes, who_split, ins_all_approved) | - |
| Review load list ("Agents never review") | done | templates/repo/insights_people.tmpl:58 review_load, agents_never_review | - |
| Rows open the person or the agent run | missing (UI) | logged as not built (not re-probed) | links in new template |
| Empty, loading, partial-data states | partial (UI) | templates/repo/insights_people.tmpl (ins_empty_title) | Empty state is there; loading and partial-data states were not found. |
| Board note: Seg controls change who and period; the chart and tiles update together | unverified | not tracked in the gap files | needs a row and a test |
| Board note: The Table button shows the same numbers as a table | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Rows open the person or the agent run | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Pulse: four tiles, daily landings by people and agents, review time trend, recent and wai... | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Contributors: table with share and 12-week activity, review load, who writes the code; em... | unverified | not tracked in the gap files | needs a row and a test |

#### IssueView (ASC Hub issue)

Area repo. Rows 5: done 2, partial 0, missing 3. Copy found in the hub: 69% of 54 copy strings (mobile twin 69%). Template(s): view.tmpl, view_content.tmpl, view_title.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Retries stampede the provider after an outage; Agreed. Keep the ceiling in config and add a metric for exhausted retries so we...; Leave a comment. Markdown works; type @ to mention a person or an agent.; Due Nov 14; Linked stacks.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Timeline, comment box, close/reopen, assignees, labels, milestone, participants, lock | done | templates/repo/issue/view_content.tmpl:10 (best match) | - |
| Agent chip on comments and assignees; agents cannot approve or merge | missing (hub+asc) | logged as not built (not re-probed) | asc agent identity surfaced to hub users (no agent principal in hub models/user); view_content/show_role.tmpl |
| Linked stacks rail ("1 of 4 landed") and timeline event "coder-2 opened stack" | missing (hub) | logged as not built (not re-probed) | same link table; templates/repo/issue/view_content/sidebar, comments.tmpl |
| Closed by a landed stack banner with Reopen | missing (hub) | logged as not built (not re-probed) | view_content.tmpl + close reason on the issue |
| Comment draft kept; Cmd+Enter posts | done | templates/repo/issue/view_content.tmpl:23 (best match) | - |
| Board note: Comment box keeps a draft; Cmd+Enter posts | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Agent comments carry an Agent chip and cannot approve or merge | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Linked stack shows the rail; landing closes the issue with an undo | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Open issue with discussion, agent comment, linked stack event; closed by stack; locked; l... | unverified | not tracked in the gap files | needs a row and a test |

#### Issues (ASC Hub issues)

Area repo. Rows 6: done 1, partial 0, missing 5. Copy found in the hub: 59% of 71 copy strings (mobile twin 57%). Template(s): filters.tmpl, labels.tmpl, list.tmpl, milestones.tmpl, new.tmpl, search.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Retries stampede the provider after an outage; Document the failover order for card providers; - opened Yesterday by Hannah Weiss; Webhook retries should back off per receiver; Timeout in sandbox gateway test is flaky.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| List with filters, sort, open/closed counts, label rail, milestone rail | done | templates/repo/issue/list.tmpl:110 | - |
| Search accepts label: and author: filters and chips apply the same | missing (hub) | logged as not built (not re-probed) | modules/indexer/issues + routers/web/repo/issue_list.go keyword parser |
| Row chips "Fixed by stack X", linked stack with state, "Quarantined by" flaky chip | missing (hub) | logged as not built (not re-probed) | link table issue to PullRequestAscStack (models/issues/pull_asc_stack.go), list.tmpl, queue quarantine data in routers/web/repo/a... |
| Linking a stack closes the issue when it lands, with undo | missing (hub) | logged as not built (not re-probed) | landing path in routers/private/asc_request_merge.go + services/issue close; reopen action |
| New issue as a dialog that keeps a draft ("Save as draft") | missing (UI) | logged as not built (not re-probed) | templates/repo/issue/new.tmpl is a page; localStorage draft in repo-issue.js |
| No results with "Clear filters", loading skeleton | missing (UI) | logged as not built (not re-probed) | list.tmpl |

#### NewRepo (ASC Hub new repository)

Area repo. Rows 15: done 3, partial 4, missing 8. Copy found in the hub: 65% of 91 copy strings (mobile twin 61%). Template(s): create.tmpl, create_advanced.tmpl, create_basic.tmpl, create_from_template.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Second-generation payment service; Import brings history, refs and tags from another server. It never changes the...; Only people and agents you add can see it.; Everyone signed in to this hub can read it.; Anyone can read it. Only collaborators can change it.; Working mode.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Page shell, owner select, name, description, auto-init options (Forgejo flow in design-system... | done | templates/repo/create*.tmpl (not found in tree) | the where-cell cites H:templates/repo/create*.tmpl, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Name availability check as you type (300 ms), tick and inline "acme/ledger already exists" | partial (hub) | logged as not built (not re-probed) | new JSON route GET /repo/check_name in H:routers/web/repo/repo.go plus script H:web_src/js/features/repo-new.js (not present); th... |
| "Start from" segment: Empty / Template / Import that keeps typed values | missing (UI) | logged as not built (not re-probed) | H:templates/repo/create.tmpl stacks the template section and the migrate page is separate (H:templates/repo/migrate/*); merge int... |
| Visibility as three radio cards: Private, Internal, Public | partial (hub) | logged as not built (not re-probed) | H:models/repo/repo.go has IsPrivate only; asc already returns visibility "private / internal / public" in RepoConfig; add an inte... |
| Working mode radio: Branchless (recommended) / With branches at creation | partial (hub) | logged as not built (not re-probed) | H:routers/web/repo/setting/asc_branchless.go exists in settings only; add to create.tmpl and forms.CreateRepoForm |
| Add AGENTS.md switch | missing (hub) | logged as not built (not re-probed) | H:services/repository init files list (add template file) |
| Add a policy file switch (.asc/policy.toml, 2 approvals, picked checks) | missing (hub) | logged as not built (not re-probed) | H:services/repository init; uses Policy.Propose/first commit; depends on policy schema |
| License choice | done | templates/repo/create_init.tmpl (file) | - |
| Preview card ("acme / payments-v2, Private, Policy on") and "Then, from your terminal" commands | missing (UI) | logged as not built (not re-probed) | H:templates/repo/create.tmpl plus script |
| Quota line "41 of 100 used on the Team plan" and quota-reached state | partial (hub) | logged as not built (not re-probed) | H:models/quota is wired to size limits; repository count limit exists (MaxCreationLimit banner) but no plan name or usage line |
| Creating state ("Creating acme/payments-v2 and its first policy...") | missing (UI) | logged as not built (not re-probed) | button disabled and busy text in script |
| Import running: progress "3,412 of 9,880 history entries", Run in background, Cancel | missing (hub+asc) | logged as not built (not re-probed) | hub H:web_src/js/features/repo-migration.js polls Forgejo migration tasks; asc needs a history-to-changes import progress source... |
| "Signed with your key mo_41c9" line | missing (hub) | logged as not built (not re-probed) | H:services/ascidentity signing-key lookup, template line |
| Sticky primary action on mobile | done | templates/admin/runners/create.tmpl:18 | - |
| Mobile: Name check after 300 ms, Start from keeps typed values, sticky primary | missing (hub) | logged as not built (not re-probed) | see NewRepo rows |

#### Refs (ASC Hub refs and tags)

Area repo. Rows 9: done 4, partial 0, missing 5. Copy found in the hub: 56% of 73 copy strings (mobile twin 58%). Template(s): list.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Shared refs and tags; Branchless repository: stacks are the unit of work, so there are only three sha...; Filter refs and tags; Shared 3; Stacks 3; These are the refs other people and tools can rely on. Nobody can rewrite them..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Branches page with filter, default branch card, ahead/behind, Protected chip, Compare | done | templates/repo/branch/list.tmpl:13 | - |
| One page with Shared refs, Stacks in progress and Tags sections | done | templates/admin/applications/list.tmpl (file) | - |
| Filter pills All / Shared 3 / Stacks 3 / Tags 14 | missing (UI) | logged as not built (not re-probed) | list.tmpl pills show Branches and Tags only |
| Branchless wording ("only three shared refs"), RepoConfig.shared_refs | missing (hub) | logged as not built (not re-probed) | read Repo.RepoConfig shared refs; branch list for asc repos comes from Forgejo's mirror |
| Stack cleanup note ("clean themselves up 7 days after the last change lands") | missing (asc) | logged as not built (not re-probed) | the setting does not exist (see Rules) |
| Tags section with release title, change count, author, date | missing (UI) | logged as not built (not re-probed) | H:templates/repo/tag/list.tmpl is a separate page |
| Row More menu names its target; no delete on protected refs | done | templates/repo/branch/row.tmpl:53 | - |
| State: nothing matches with "Search stacks" link | done | templates/admin/applications/list.tmpl (file) | - |
| State: delete protected ref refused with "Open rule" | missing (UI) | logged as not built (not re-probed) | H:routers/web/repo/branch.go flash message and link to settings/branches |

#### Releases (ASC Hub releases)

Area repo. Rows 15: done 5, partial 3, missing 7. Copy found in the hub: 57% of 115 copy strings (mobile twin 61%). Template(s): list.tmpl, new.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Stable 11; Pre-release 2; Most downloaded; main@a41f9c2; Signed with release key 7F3A...C21B; Written from the intent of each change; edit before publishing..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Timeline of release cards, tag, stable / pre-release / draft chips, Compare, edit, assets list... | done | templates/repo/release/list.tmpl:96 | - |
| Releases / Tags segment, search filter, feed | done | templates/repo/release_tag_header.tmpl:28 | - |
| Filter pills All 14 / Stable 11 / Pre-release 2 / Draft 1 and Sort (Newest, Most downloaded) | done | templates/repo/release_tag_header.tmpl:27 | - |
| Provenance strip ("Built by run #4218 from main@a41f9c2, 14 of 14 checks passed, signed with r... | partial (hub) | templates/repo/release/list.tmpl (rel_prov) | Provenance line exists from Forgejo data; the hub has no client for asc Releases, so no run id or release key. |
| Notes grouped Added / Changed / Fixed, "written from the intent of each change" | partial (hub) | templates/repo/release/list.tmpl (rel_written_from) | Text exists; notes are not generated through asc Releases.DraftNotes (RPC never called by the hub). |
| "N changes in this release" list with author, change id, category | missing (hub) | logged as not built (not re-probed) | Repo.Compare(prev tag, tag) in release.go; template block |
| Asset table: size, SHA-256 checksum (copy on click), downloads | missing (hub) | logged as not built (not re-probed) | H:models/repo/attachment.go has no checksum column; compute at upload; template shows name, count, age only |
| Next release summary ("6 changes since v2.18.0", Draft v2.19.0 View) | done | templates/repo/branch/list.tmpl:105 (best match) | the where-cell cites Compare(latest tag, default branch), not in the tree (renamed or stale text); behaviour taken from the gap f... |
| "Download the latest" with asc release download v2.18.0 and per-platform line | partial (UI) | logged as not built (not re-probed) | right rail in list.tmpl; the asc release CLI command must exist (check A:crates/asc/src/cli) |
| Release health: cadence, rollbacks | missing (hub) | logged as not built (not re-probed) | derive from release dates; rollbacks need a data source |
| Get notified: Watch releases, Atom feed | done | see where-cell | - |
| New release dialog: proposes next tag, "Write from the N landed changes", assets drop zone, pr... | missing (UI) | logged as not built (not re-probed) | H:templates/repo/release/new.tmpl is a full page (no dialog, no generated notes, no tag proposal) |
| States: no releases (shows asc release create v1.0.0), loading, storage error with Retry | missing (asc) | logged as not built (not re-probed) | the command does not exist in the CLI (see above); list.tmpl empty state has no command, loading skeleton or storage-error banner |
| Mobile: new release as bottom sheet, 44 px downloads | missing (UI) | logged as not built (not re-probed) | new.tmpl |
| Mobile: New release as a bottom sheet; channel chips | missing (UI) | logged as not built (not re-probed) | see Releases rows |

#### Repo (ASC Hub repository home)

Area repo. Rows 29: done 21, partial 2, missing 6. Copy found in the hub: 57% of 158 copy strings (mobile twin 60%). Template(s): asc_about.tmpl, clone_asc_tabs.tmpl, header.tmpl, home.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Diego Alvarez; Add repository policy file; Document the failover order; Describe how agents work in this repo; Explain local setup; Ordered by what needs a person next.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Title row (owner / name, Private chip, Watch, Star, Fork) and tab bar with Stacks and Releases... | done | templates/repo/header*.tmpl (not found in tree) | the where-cell cites H:templates/repo/header*.tmpl, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Tab counts on Agent runs and Dependencies (3, 2) | done | templates/repo/header_tabs.tmpl (file) | - |
| Ref meta line ("4 open stacks, 12 branches, a41f9c2 landed 3 h ago"), Go to file button, Clone... | done | templates/repo/home_dir.tmpl:40 | - |
| Shortcuts: t go to file, / focus search, g then c jump to Code | done | web_src/js/features/ds-shell.js:406 | - |
| Last-change row: person, intent, change id, age, "1,204 changes" total | done | templates/repo/latest_commit.tmpl (file) | - |
| Directory table (name, last change message, age) with htmx last-commit load | done | templates/repo/view_list.tmpl:1 | - |
| Stacks in flight: one row per stack with status chip (Needs your review, Queued to merge, Agen... | done | routers/web/repo/view_home_cards.go:29 | the where-cell cites HomePulls, H:services/pull, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Progress rail: segments per change ("2 of 4 landed"), segments link to each change, text label... | done | templates/repo/asc_about.tmpl:85 (best match) | the where-cell cites H:models/git, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Segment filter Open / Mine / Agents / All N stacks without page load | done | templates/repo/home_cards.tmpl:20 | - |
| Row hover and focus reveals "Open"; row click opens the stack review | done | templates/repo/home_cards.tmpl (file) | the where-cell cites H:web_src/css, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Recently landed on main: title, author, change id, stack name, +/- size, checks chip | done | templates/repo/home_cards.tmpl:53 | - |
| README card with tabs README / AGENTS.md / License | done | templates/repo/home_dir.tmpl (file) | - |
| About card: description, topics, website, numbers | done | templates/repo/asc_about.tmpl (file) | - |
| About card: license line and "1,204 changes on main" wording | done | templates/repo/asc_about.tmpl:35 | - |
| "Who writes the code" card: 46% people / 54% agents, "100% reviewed by a human", median review... | missing (hub+asc) | logged as not built (not re-probed) | asc: a landed-change statistics RPC (author kind, review latency); hub: card on asc_about.tmpl |
| Policy card: Valid / invalid chip, rule summary, link to .asc/policy.toml | done | routers/web/repo/setting/asc_policy.go (file) | the where-cell cites .asc/policy.toml, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Policy card lines that need new policy keys (budget "20 min and 200k tokens", merge queue on) | missing (asc) | logged as not built (not re-probed) | A:crates/asc-core/src/policy.rs (see Policy rows) |
| Activity card: landed per week over 12 weeks, Contributors list with agent marks | missing (hub+asc) | logged as not built (not re-probed) | same statistics RPC as Insights; hub asc_about.tmpl |
| Languages bar with legend and aria-label | done | templates/repo/asc_about.tmpl (file) | - |
| Latest release card ("9 changes, 3 by agents. Published 6 days ago") | done | templates/repo/asc_about.tmpl:106 | - |
| Loading skeleton that keeps row geometry | partial (UI) | logged as not built (not re-probed) | H:templates/repo/home_cards.tmpl (skeleton rp-sk exists only for the last-commit column) |
| Error state: "Stacks did not load", request id, Try again | done | templates/repo/home_cards.tmpl:12 | Gap file says open; the template has the Stacks did not load error with request id and Try again. |
| Empty repository state with asc init and asc push commands | done | templates/repo/empty.tmpl (file) | - |
| Clone popover: asc login and clone, Install asc, Open in IDE, Copy | done | templates/repo/clone_dialog.tmpl (file) | - |
| Git compatibility note collapsed and expanded in the clone sheet | done | templates/repo/clone_asc_tabs.tmpl:1 | - |
| "Switch version" popover that lists shared refs, stacks and tags with labels | missing (hub) | logged as not built (not re-probed) | H:web_src/js/components/RepoBranchTagSelector.vue and H:templates/repo/branch_dropdown.tmpl list branches and tags only; add stac... |
| Pull to refresh and sticky Clone / New stack buttons (Mobile-Repo) | partial (UI) | logged as not built (not re-probed) | sticky buttons exist (rp-sticky-m in home_dir.tmpl); pull to refresh is not implemented anywhere in H:web_src/js |
| Clone card shown before first clone ("Work on it locally", asc stack new fix-refund-timeout) | missing (UI) | logged as not built (not re-probed) | H:templates/repo/home_dir.tmpl / clone_dialog.tmpl |
| Mobile: Stack cards replacing the table; filter chips that snap; pull to refresh; sticky Clone and New... | missing (hub) | logged as not built (not re-probed) | as the desktop rows; chips and pull to refresh in home_cards.tmpl and a script |

#### RepoSettingsAI (ASC Hub repository settings: AI keys)

Area repo. Rows 6: done 2, partial 1, missing 3. Copy found in the hub: 59% of 106 copy strings (mobile twin 55%). Template(s): asc_ai.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Your keys, your providers, your caps.; Add a key once and choose where each feature uses it.; sk-ant-...c4fA; Agent runs, review summaries; Replace key; sk-proj-...9tLq.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Provider list (Anthropic, OpenAI, own endpoint) with key hint, added by, verified time, Replac... | done | services/ascbyo/aikeys.go:96 | the where-cell cites H:templates/shared/asc/byo_ai*.tmpl, not in the tree (renamed or stale text); behaviour taken from the gap f... |
| Replace key dialog that verifies before swapping and shows "Anthropic rejected this key (401)" | partial (hub+asc) | logged as not built (not re-probed) | UpdateKey + TestKey exist; asc must keep the old key until the new one passes (atomic verify in UpdateAiKey), hub dialog |
| "What uses AI" table: Agent runs, Review summaries, Meaning search, each with its provider and... | missing (hub+asc) | logged as not built (not re-probed) | asc caps are per key (monthly_cap_micro_usd), not per feature; add feature routing (AiFeature -> key + cap) in A:proto/asc/v1/aip... |
| Cap reached: feature pauses with "until 1 Nov or you raise it" | missing (asc) | logged as not built (not re-probed) | key state CAPPED exists; per-feature pause message and reset date |
| No keys: "AI features are off" empty state | done | see where-cell | - |
| Repository-level page (keys are owner scope in asc, page lives under the repository menu) | missing (UI) | logged as not built (not re-probed) | board title "Your keys, your providers, your caps" under repo menu; hub embeds owner list |

#### RepoSettingsAccess (ASC Hub repository settings: Access)

Area repo. Rows 8: done 3, partial 1, missing 4. Copy found in the hub: 65% of 110 copy strings (mobile twin 54%). Template(s): collaboration.tmpl, deploy_keys.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Who and what can work in this repository: people, teams, agents and deploy keys.; People, teams and agents; Who can open stacks here. What an agent may change is limited again by the repo...; Search people, teams or agents; Add to repository; All repository settings.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| One list of people, teams and agents with role select, last active, Show All / People / Agents | missing (hub) | logged as not built (not re-probed) | H:templates/repo/settings/collaboration.tmpl has separate collaborator and team sections with Forgejo roles read / write / admin;... |
| Combined search (people, teams, agents) with role chosen before adding | missing (hub) | logged as not built (not re-probed) | collaboration.tmpl searches users and teams separately |
| Role select changes access at once with an Undo toast | done | routers/web/repo/setting/collaboration.go:134 | - |
| Agent rows: Agent chip, violet mark, "Limited by policy to src/ and tests/" | partial | templates/repo/settings/collaboration.tmpl | Marked done; the text Limited by policy to ... was not found. |
| Remove agent confirm that says the running run stops and open changes stay with the owner | missing (hub+asc) | logged as not built (not re-probed) | Agents.CancelRun on removal in collaboration.go; confirm dialog text |
| Invite that already has access ("Priya Raman already has Maintain access") | done | routers/web/repo/setting/collaboration.go:86 | - |
| Deploy keys on the same page: title, fingerprint, read-only or read-write, added by, last used... | missing (UI) | logged as not built (not re-probed) | H:templates/repo/settings/deploy_keys.tmpl is its own page at /settings/keys; the board says both routes render one layout |
| No deploy keys empty state | done | see where-cell | - |

#### RepoSettingsGeneral (ASC Hub repository settings: General)

Area repo. Rows 9: done 5, partial 1, missing 3. Copy found in the hub: 65% of 115 copy strings (mobile twin 58%). Template(s): asc_branchless.tmpl, options.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Name, visibility, features and how this repository works.; How people find this repository.; Changing the name redirects the old address for 90 days.; One sentence, shown in search and on the home page.; Up to 20. Used by Explore.; failover.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Name, description, one save bar with "2 unsaved changes", Discard, Save | done | templates/repo/settings/options.tmpl (file) | - |
| Name rename note "redirects the old address for 90 days" and taken-name error | missing (hub) | logged as not built (not re-probed) | Forgejo keeps redirects without the 90-day limit; H:models/repo/redirect.go expiry |
| Topics field in General (chips, up to 20, Add) | done | templates/org/settings/options.tmpl (file) | - |
| Visibility as radio cards (Private / Internal / Public) with confirmation when changing | partial (hub) | logged as not built (not re-probed) | options.tmpl uses a Private checkbox; Internal does not exist in the model (see NewRepo) |
| Features switches: Stacks and change review (locked, Required), Agent runs, Dependencies, Rele... | missing (hub) | logged as not built (not re-probed) | Forgejo units (H:models/unit/unit.go, settings/units/*) cover releases and wiki; add unit types or repo flags for agent runs, dep... |
| Working mode radio cards and shared refs (main, release/*), Cloud sync switch | done | templates/repo/settings/asc_branchless.tmpl (file) | - |
| Danger zone: Transfer, Archive, Delete with typed full name acme/payments and counts ("Removes... | done | templates/org/settings/options.tmpl:99 | - |
| "An owner can undo the delete from the operation log for 30 days" | done | see where-cell | - |
| Saving state, saved toast (5 s), unsaved-bar motion | missing (UI) | logged as not built (not re-probed) | repo-settings.js |

#### RepoSettingsPolicy (ASC Hub repository settings: Policy)

Area repo. Rows 12: done 7, partial 3, missing 2. Copy found in the hub: 75% of 142 copy strings (mobile twin 73%). Template(s): asc_policy.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: The rules every change and every agent run follows in this repository.; , so they are reviewed like code and travel with a fork. Approvals and checks a...; What a change needs before it can land on main.; People who must approve each change. Agents never count.; A change cannot land while any of these is failing or missing.; Authors may approve their own change.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| File header: .asc/policy.toml, Valid chip, "Last changed by Priya Raman, 5 days ago, in change... | missing (UI) | logged as not built (not re-probed) | H:templates/repo/settings/asc_policy.tmpl has the chip; last-change info via Repo.Log path filter |
| TOML editor with Check (validate), diagnostics with line, Propose change | done | routers/web/repo/setting/asc_policy.go:77 | - |
| Seg Form / TOML / Changes (three views of one file) | done | templates/repo/settings/asc_policy.tmpl (file) | - |
| Form: approvals stepper, check chips with Add, authors-may-approve switch, keep-approvals swit... | partial (hub+asc) | logged as not built (not re-probed) | form needs a structured parse (ValidatePolicyResponse.parsed); keys author_may_approve, keep_approvals_on_restack do not exist in... |
| Agents section: write paths, never-touch paths, budget minutes and tokens, locked "A person ap... | partial (hub+asc) | templates/repo/settings/asc_policy.tmpl:79-88 agents fieldset (scope, denied paths, runtime, change... | Agents section exists as text areas and numbers; the approvals stepper, check chips and switches of the board were not found. |
| Merge queue section: use queue, batch size, checks after merge | done | proto/asc/v1/mergequeue.proto (not found in tree) | the where-cell cites A:crates/asc-serve, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Live file beside the form with changed lines flashing (+/-) | done | see where-cell | the where-cell cites GetSource.text, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Changes tab: diff of the one pending edit; "Changing policy is a change" panel naming reviewer... | partial | templates/repo/settings/asc_policy.tmpl:93 live file | Marked done; the Changing policy is a change panel naming reviewers was not found. |
| TOML error state: "Line 11: max_minutes must be a whole number", form disabled while invalid | done | see where-cell | - |
| Read-only state for non-maintainers with names to ask | done | see where-cell | - |
| After-propose state: "Proposed as change kz9fw. Waiting for one approval" with Open change | done | templates/repo/settings/asc_policy.tmpl:171 (best match) | - |
| Loading state | missing (UI) | logged as not built (not re-probed) | skeleton |

#### RepoSettingsRules (ASC Hub repository settings: Branches and stacks)

Area repo. Rows 8: done 1, partial 0, missing 7. Copy found in the hub: 56% of 108 copy strings (mobile twin 44%). Template(s): branches.tmpl, protected_branch.tmpl, tags.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Which refs are protected, how stacks move and how they land.; Approvals and required checks are set in; . This page covers which refs are protected and how stacks behave.; Nobody can rewrite or delete these, whatever their role.; Everything lands here through the merge queue.; Cherry-picked fixes only..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Protected refs table: pattern, kind (Trunk, Release lines, Tags), can land (Merge queue, Maint... | done | templates/repo/settings/branches.tmpl:43 | - |
| Add / Edit rule side panel with Who can land checkboxes (Merge queue, Maintainers, Release key... | missing (hub+asc) | logged as not built (not re-probed) | Forgejo rule has push whitelists; asc policy has allow_force/require_signed only, no per-ref kinds or agents (A:policy.rs Trunk) |
| Pattern overlap error ("Rules cannot overlap") and "2 open stacks target a ref this pattern ma... | missing (hub) | logged as not built (not re-probed) | validate in H:routers/web/repo/setting/protected_branch.go; count pulls by base branch |
| Rule removed with Undo for 30 minutes from the operation log | missing (hub) | logged as not built (not re-probed) | asc_ops entry for rule deletion; Forgejo rules are in the hub DB, not in asc ops |
| Stack behaviour switches: restack when main moves, keep approvals when only the base changed,... | missing (hub+asc) | logged as not built (not re-probed) | no repo-level config in asc (Stacks.Restack is invoked on demand); store in policy or repo config and enforce in asc-serve |
| Longest stack (8) and clean-up after landing (7 days) | missing (asc) | logged as not built (not re-probed) | new policy/config keys and a janitor in A:crates/asc-serve |
| How stacks land: keep every change / squash into one change | missing (asc) | logged as not built (not re-probed) | [merge] strategy values are rebase, rebase-disjoint, fast-forward; no squash (A:policy.rs) |
| Empty state "Nothing is protected" with Protect main | missing (UI) | logged as not built (not re-probed) | branches.tmpl |

#### RepoSettingsSecrets (ASC Hub repository settings: Secrets and variables)

Area repo. Rows 9: done 2, partial 1, missing 6. Copy found in the hub: 68% of 95 copy strings (mobile twin 58%). Template(s): -. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Values your checks, agent runs and releases need.; Secret values are encrypted and shown only once. Agents get a secret only when...; for it, and a change cannot print one: the log redacts it.; Values for checks, agent runs and releases. Names are fixed once created.; CARD_PROVIDER_API_KEY; Update value.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Secrets and variables on one page | done | templates/shared/secrets/add_list.tmpl (file) | - |
| Scope chips per secret (Checks, Agent runs, Releases) and "Agents only receive it when Agent r... | missing (hub+asc) | logged as not built (not re-probed) | Workflows.SetSecret has name and value only (A:proto/asc/v1/workflows.proto); add scopes to the asc secret and to the Forgejo sec... |
| "Updated by Maya Okafor, 12 d ago" | partial (hub) | logged as not built (not re-probed) | WorkflowSecret.updated_by exists in asc; Forgejo secret has created time only; template |
| Update value dialog with name locked; value never shown | missing (UI) | logged as not built (not re-probed) | H:routers/web/repo/setting/secrets.go, add_list.tmpl |
| Delete confirmation that lists which scopes lose it | missing (UI) | logged as not built (not re-probed) | add_list.tmpl confirm |
| Name rule (capitals, digits, underscores) validation in dialog | done | see where-cell | - |
| Variables table with "Available to" scope and updated-by | missing (hub+asc) | logged as not built (not re-probed) | same scope work as secrets |
| Secret-redaction in change logs ("the log redacts it") | missing (asc) | logged as not built (not re-probed) | run log redaction in A:crates/asc-ci job log append |
| Saved toast and empty state | missing (UI) | logged as not built (not re-probed) | add_list.tmpl |

#### RepoSettingsStorage (ASC Hub repository settings: Storage)

Area repo. Rows 8: done 2, partial 1, missing 5. Copy found in the hub: 53% of 96 copy strings (mobile twin 42%). Template(s): asc_storage.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Where changes, large files and indexes are kept.; Storage holds changes, large files and search indexes. Changing it never touche...; Where this repository's data lives; Pick one. Moving data is a copy first and a switch after.; ASC Hub storage; Managed by us. Nothing to set up; counted against your plan..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Choose ASC Hub storage or own S3-compatible bucket (endpoint https only, region, bucket, prefi... | done | templates/shared/asc/byo_storage.tmpl:45 | - |
| Test connection with four steps (reachable, can write, can read, versioning on) and AccessDeni... | missing (asc) | logged as not built (not re-probed) | VerifyStorageTarget returns checks; confirm it covers TLS time, write, read and versioning, and the permission wording in A:crate... |
| Usage: 38.2 GB split into changes, large files, search indexes plus objects and "read this mon... | missing (asc) | logged as not built (not re-probed) | no usage RPC in A:proto/asc/v1/storage.proto; add per-repo usage by kind and monthly reads |
| Moving data: 64% progress, Pause, Cancel and keep current | partial (hub+asc) | logged as not built (not re-probed) | StartMigration/GetMigration/CancelMigration exist; no Pause RPC; hub progress UI |
| Managed storage selected: "38.2 GB of 100 GB on your plan", Use my own bucket | missing (hub+asc) | logged as not built (not re-probed) | plan quota from H:models/quota; usage as above |
| Local-option and http refused message | done | services/ascbyo/validate.go (file) | - |
| Bucket settings changed bar with Discard / Save and test | missing (UI) | logged as not built (not re-probed) | byo_storage.tmpl uses plain forms; reuse data-savebar |
| Meaning-search index enabling link ("An admin can enable the index in storage settings") | missing (hub+asc) | logged as not built (not re-probed) | no index toggle in the storage page or CodeIntel.IndexStatus surface |

#### RepoSettingsWebhooks (ASC Hub repository settings: Webhooks)

Area repo. Rows 10: done 2, partial 0, missing 8. Copy found in the hub: 75% of 116 copy strings (mobile twin 72%). Template(s): -. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Send events to other systems.; Tell another system when something happens here. Each request is signed with th...; stack opened; One webhook, two views; Settings and recent deliveries belong to this hook, so they are tabs here.; Delivery d_7q2m1.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Hook list with URL, kind, events, last delivery, Active switch, Failing banner | missing (UI) | logged as not built (not re-probed) | H:templates/repo/settings/webhook/list.tmpl, base_list.tmpl (Forgejo list) |
| One hook with tabs Settings / Recent deliveries (12), delivery detail with Request / Response,... | done | templates/repo/settings/webhook/history.tmpl:4 | - |
| Hook kinds Generic, Chat, ASC agent (starts an agent run on an event) | missing (hub) | logged as not built (not re-probed) | H:modules/webhook/type.go has Forgejo kinds; add asc-agent kind that calls Agents.CreateRun; map Chat to slack/discord/matrix |
| ASC events: Changes (Landed, Opened, Review requested), Agents (Run started, Run finished), Re... | missing (hub) | logged as not built (not re-probed) | event types and notifiers in H:models/webhook, H:services/webhook, fired from ascsync/agent callbacks |
| Payload URL https-only with certificate check; loopback http only with local option | missing (hub) | logged as not built (not re-probed) | H:modules/setting ALLOWED_HOST_LIST; add the explicit local option and the refusal text |
| Secret shown masked with Replace, never shown again | missing (UI) | logged as not built (not re-probed) | webhook/new.tmpl shows the secret field |
| Send test delivery | done | see where-cell | - |
| Failing-hook alert banner role=alert and menu count "Webhooks 3" | missing (UI) | logged as not built (not re-probed) | list + navbar |
| Add webhook dialog with http address refused | missing (UI) | logged as not built (not re-probed) | webhook/new.tmpl is a page |
| Empty state | missing (UI) | logged as not built (not re-probed) | base_list.tmpl |

#### Search (ASC Hub search)

Area repo. Rows 21: done 10, partial 2, missing 9. Copy found in the hub: 59% of 124 copy strings (mobile twin 65%). Template(s): asc_search.tmpl, asc_symbols.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: In: all repositories; This version (main); A change or stack; Only files the stack changes; Indexed at e91b0d4, 99.2% of files. 4 files are too large for search and are li...; Sort: best match.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Text, Symbol and Meaning modes as radio group (lexical / symbol / semantic) | done | templates/repo/asc_search.tmpl:15 | the where-cell cites H:services/ascsearch, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Scope radios: this version, change or stack, all branches, all repositories | done | routers/web/repo/asc_search_scope.go:42 | - |
| Cross-repository results grouped by repository, URL /-/asc/search | partial (hub) | logged as not built (not re-probed) | scope "repos" sends to the explore search page (H:routers/web/explore/asc_search.go, templates/explore/asc_search.tmpl); route in... |
| Facets: Repository (21 / 9 / 5, "11 more"), Language, Path, Branch or commit, "Only files the... | partial (UI) | logged as not built (not re-probed) | Language and Path facets and the stack-diff checkbox exist; add Repository and Branch facets from CrossSearch result group keys i... |
| Result card: path, language, branch list, snippet with highlights | done | templates/explore/asc_search.tmpl:189 | - |
| Relevance percentage with a bar (grows over 300 ms) | done | templates/explore/asc_search.tmpl:168 | - |
| "Why it matched" reason sentence per result | missing (asc) | logged as not built (not re-probed) | A:proto/asc/v1/codeintel.proto Chunk has no reason; add a reason from the retrieval source or a short model-written line; show in... |
| Symbol chips under a result ("RetryPolicy::delay", "14 references elsewhere", "changed 3 h ago... | missing (UI) | logged as not built (not re-probed) | symbol and reference counts from CodeIntel.Symbols; last change from Repo.LastCommits |
| "Symbols matched: RetryPolicy, retry" line | missing (UI) | logged as not built (not re-probed) | asc_search.tmpl header line from CodeIntel.Symbols |
| Code intelligence side panel (definition, signature, Go to definition, references, Called from... | missing (UI) | logged as not built (not re-probed) | CrossSearch.SymbolPanel (hub client H:services/ascsearch/panel.go, REST only) and GET /asc/symbols; no template renders it; add r... |
| Coverage line ("Indexed at e91b0d4, 99.2% of files; 4 files too large, listed in settings") | done | templates/explore/asc_search.tmpl (file) | - |
| Sort: best match, pagination "Showing 4 of 38", page links | missing (UI) | logged as not built (not re-probed) | asc_search.tmpl has a Truncated banner only |
| "The other two kinds of search" teaser (Text and Symbol hits for the same query) | missing (UI) | logged as not built (not re-probed) | run two cheap queries in asc_search.go; fragment |
| First-visit state with example queries | done | templates/explore/asc_search.tmpl (file) | - |
| No-results state with "Search all branches / All repositories" actions | done | templates/explore/asc_search.tmpl:49 | - |
| Not available (503) for meaning search with "Use Text search" and "Open storage settings" | done | templates/repo/asc_search.tmpl:85 (best match) | - |
| Rate limited (429) keeps the query | done | see where-cell | - |
| Loading state | missing (UI) | logged as not built (not re-probed) | skeleton |
| / focuses search in the repository | done | web_src/js/features/ds-shell.js (file) | - |
| Mobile: sticky search with mode and scope, Filters bottom sheet, results as cards | missing (UI) | logged as not built (not re-probed) | asc_search.tmpl + sheet component |
| Mobile: Sticky search, Filters bottom sheet, cards with reason first | missing (UI) | logged as not built (not re-probed) | see Search rows |

#### Stacks (ASC Hub stacks)

Area repo. Rows 13: done 5, partial 0, missing 8. Copy found in the hub: 66% of 107 copy strings (mobile twin 61%). Template(s): asc_badges.tmpl, asc_stack.tmpl, list.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Agents 2; Needs me 2; - Diego Alvarez - 3 changes -; - Hannah Weiss - 5 changes -; No reviewers yet; Raise tokio to 1.41 and drop the unused rt-multi-thread feature.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Area sub-navigation Stacks / Merge queue / Policy gate / Operation log | done | templates/repo/asc_stack_nav.tmpl:6 | - |
| Merge queue page with pause and resume | done | routers/web/repo/asc_queue.go (file) | - |
| Stack row: avatar, title, stack name, author, change count, +/- size, checks, age, status chip... | missing (hub) | logged as not built (not re-probed) | /pulls renders the Forgejo issue list (H:templates/repo/issue/list.tmpl, shared/issuelist.tmpl); build a stack-grouped list from... |
| Rail with segments per change and text alternative "Stack of 4 changes"; legend (Landed, Appro... | missing (hub) | logged as not built (not re-probed) | same data as the Repo progress rail; one shared template |
| Filters Open / Mine / Agents / Needs me / Closed with counts; sticky filter bar | missing (hub) | logged as not built (not re-probed) | Needs me = review requested or queue-ready for the viewer; Agents needs an agent flag; H:routers/web/repo/issue.go (Pulls) |
| Sort Newest / Needs attention | missing (hub) | logged as not built (not re-probed) | attention score from status |
| "Your turn" card with jump to the exact change | missing (hub) | logged as not built (not re-probed) | pulls awaiting the viewer's review or enqueue |
| "From your terminal" card (asc stack list, asc stack submit) | done | templates/repo/issue/list.tmpl:84 (best match) | - |
| New stack button | missing (hub) | logged as not built (not re-probed) | no web create-stack flow; H:routers/web/repo/pull.go creates pulls by compare; agents or terminal create stacks |
| Keyboard j/k and Enter | done | see where-cell | - |
| States: empty (Nothing is in flight, New stack, Open history), skeleton, error with request id | missing (UI) | logged as not built (not re-probed) | list template |
| Row hover slide-in, status chip pulses once on change | missing (UI) | logged as not built (not re-probed) | CSS + SSE event (H:services/ascinbox / eventsource.sharedworker.js) |
| Mobile: 44 px rows, chips scroll | done | see where-cell | - |

### Area: review

#### ChangeChecks (ASC Hub checks on a change)

Area review. Rows 10: done 5, partial 3, missing 2. Copy found in the hub: 56% of 52 copy strings (mobile twin 62%). Template(s): asc_check_group.tmpl, asc_checks.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Version 4 - commit; Security scan is stale.; Not seen on main; Both failures hit the same line.; divides by; , which is 0 in these tests. Your change touched this function in.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Headline, pill, version/commit/started/took, stale banner, Re-run failed only / Re-run all | done | templates/repo/pulls/asc_checks.tmpl:18 | - |
| Cause cards: title, test count, New in v4 / seen before, log excerpt, Show full log, group link | done | templates/repo/pulls/asc_checks.tmpl (file) | - |
| 'Open the line' jumps to the diff at file:line | done | services/asctrust/pullchecks.go (file) | - |
| 'Copy a repro command' (an asc command, not git) | missing (UI) | logged as not built (not re-probed) | templates/repo/pulls/asc_checks.tmpl + small copy script |
| 'Ask coder-2 to fix' (start an agent run on this failure) | missing (hub) | logged as not built (not re-probed) | asc_checks.go calls Agents.CreateRun (proto/asc/v1/agents.proto) with the excerpt as task |
| Flaky card with 20-run strip, Quarantine this test, Open an issue | done | templates/repo/pulls/asc_checks.tmpl (file) | - |
| All checks list with required flag, duration; 'Run security scan on v4' per row | partial (UI) | logged as not built (not re-probed) | templates/repo/pulls/asc_checks.tmpl (list exists; per-job run action missing) |
| Compared with main: main green, 'these 2 failures are new', slowest test 41 s (was 12 s) | partial (hub+asc) | logged as not built (not re-probed) | main status exists (pull.main_*); slowest-test delta needs per-test timing in CiInsights.ReportTestCases (proto/asc/v1/ciinsights... |
| Versions history list (v4 2 fail, v3 pass, v2 pass) | partial (hub) | logged as not built (not re-probed) | templates/repo/pulls/asc_checks.tmpl (pull.history key exists) |
| Trust text: results belong to the exact version | done | templates/repo/pulls/asc_checks.tmpl (file) | - |

#### Checks (ASC Hub checks)

Area review. Rows 13: done 6, partial 1, missing 6. Copy found in the hub: 55% of 120 copy strings (mobile twin 61%). Template(s): asc_check_group.tmpl, asc_checks.tmpl, asc_queue.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Failures grouped by cause, the jobs they happen in, and the tests that flip bet...; Pass rate; up 1.1 pts vs previous 30 days; Failure groups open; Median duration; slowest job: integration, 11m.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Window selector, state tabs, job/test/flaky filters persisted in URL | done | templates/repo/asc_checks.tmpl:110 | - |
| Headline stats: pass rate vs previous period, failure groups open, flaky above threshold, medi... | missing (hub+asc) | logged as not built (not re-probed) | asc: Ci.ListRuns aggregates (proto/asc/v1/ci.proto) or hub aggregation in asc_checks.go; stat cards |
| State tab counts (Open 3, Acknowledged 1, Quarantined 2, Resolved 14) | missing (hub) | logged as not built (not re-probed) | asc_checks.go (counts from ListFailureGroups per state) |
| Failure groups list with cause, jobs, seen N times in M changes, new-in-version chip | done | templates/repo/asc_checks.tmpl:3 | - |
| Inline expand of a group: log excerpt with line numbers, occurrences, note + Acknowledge / Qua... | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_checks.tmpl (actions live on the group page asc_check_group.tmpl) |
| Row hover actions Acknowledge / Quarantine / More | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_checks.tmpl |
| Quarantine hidden unless policy allows [ci] allow_quarantine | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_check_group.tmpl:64 shows the button always; read the flag via services/asctrust/policy.go |
| Acknowledge / quarantine / resolve / reopen with optional note, logged | done | routers/web/repo/asc_checks.go:34 | - |
| Repo-level 'Re-run failed' button | done | routers/web/repo/asc_checks.go:496 | - |
| Flaky tests view: flake rate, runs, flips, 14-run heat strip, last flip, state | done | templates/repo/asc_checks.tmpl (file) | - |
| Trends view per job with pass rate | done | templates/repo/asc_checks.tmpl (file) | - |
| Side cards: 'Main is green', failing jobs bars, Quarantined list with Manage | missing (hub) | logged as not built (not re-probed) | templates/repo/asc_checks.tmpl; main status service used by pull checks (asc_checks.go) |
| Empty 'Nothing needs attention', error with Try again, loading skeleton | partial (UI) | templates/repo/asc_checks.tmpl (checks.nothing_title) | Empty state exists; error with Try again and loading skeleton not found. |

#### NewChange (ASC Hub open a change)

Area review. Rows 13: done 4, partial 3, missing 6. Copy found in the hub: 35% of 75 copy strings (mobile twin 41%). Template(s): asc_compare.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: You are on a stack of 3 changes. This one goes on top. Title it for the person...; Stack: idempotency-keys; idempotency-keys; in 9 files; Ahead 3, behind 0; Intent and description.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Open-change form: title, description, reviewers, draft | done | templates/repo/diff/compare.tmpl (file) | - |
| Compare bar: base/head, +212 -48 in 9 files, no-conflict chip, ahead/behind, stack of N | missing (UI) | logged as not built (not re-probed) | templates/repo/diff/compare.tmpl; asc_compare.tmpl is a separate page |
| Title counter '68 of 120', 'Start with a verb' hint, inline validation | done | templates/repo/diff/compare.tmpl (file) | - |
| Intent editor with What/Why/How to check/Risk template and Write/Preview | done | templates/repo/diff/compare.tmpl (file) | - |
| 'Draft from the diff' (AI writes What/Why/How/Risk from the diff) | missing (hub+asc) | logged as not built (not re-probed) | hub: new route using BYO AI key (services/ascbyo); asc: diff + Classify |
| 'Where to look' pointers editor | missing (hub+asc) | logged as not built (not re-probed) | same as the Review intent block |
| Reviewer suggestions from ownership and review history, out-of-office note, agent reviewers la... | missing (hub+asc) | logged as not built (not re-probed) | asc: code owner / review history query; hub: reviewer panel in compare.tmpl |
| Options: open as draft, keep as part of stack, land when approved and green, ask agent to baby... | partial (hub+asc) | logged as not built (not re-probed) | draft + auto-merge exist natively; babysit needs Agents.CreateRun trigger + hub option |
| Local checks start on open; 'signed with your key' note | missing (UI) | logged as not built (not re-probed) | templates/repo/diff/compare.tmpl; asc signing (proto/asc/v1/signing.proto) |
| Draft autosave on blur (1 s debounce), 'Save as draft', Ctrl+Enter opens | partial (hub) | logged as not built (not re-probed) | routers/web/user (drafts page exists); form autosave handler; key handler |
| Right rail: stack after this change, file list with Show all, 'What policy will ask for' previ... | missing (hub) | logged as not built (not re-probed) | routers/web/repo/compare.go + Changes.Stack and Policy.Evaluate LAND |
| 'Review my diff first' self-review shortcut | done | templates/repo/diff/compare.tmpl (file) | - |
| Blocked-by-secret-scan banner before open; nothing-to-compare; opened/draft toasts | partial (hub+asc) | logged as not built (not re-probed) | nothing-to-compare exists (repo.pulls.nothing_to_compare); secret block needs secretscan.Scan on head (proto/asc/v1/secretscan.pr... |

#### Review (ASC Hub review)

Area review. Rows 30: done 10, partial 5, missing 15. Copy found in the hub: 59% of 150 copy strings (mobile twin 49%). Template(s): asc_confirm.tmpl, asc_graph.tmpl, asc_queue.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Stack - 4 changes; main @ 4be19a2; Merge order is enforced:; Merge 1 and 2 now; based on change 2; . Everything else is mechanical..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Stack rail: 4 changes, current marker, per-change state, trunk line, merge-order note, asc sta... | done | templates/repo/pulls/asc_stack.tmpl:12 | - |
| Stack rail rows: author avatar with agent mark, +add/-del size, text like 'Approved, restacked' | missing (hub) | logged as not built (not re-probed) | templates/repo/pulls/asc_stack.tmpl; pull_asc_stack.go (add author, diff stat, review state to entries) |
| Stack rail click keeps scroll position per change | missing (UI) | logged as not built (not re-probed) | web_src/js/features (new asc-review.js) |
| Rail action 'Merge 1 and 2 now' (merge bottom of stack, with dry run) | done | templates/repo/pulls/asc_stack.tmpl (file) | - |
| Change header: agent banner (waiting on you, 42 tests pass), change id + short hash, Signed ba... | partial (hub) | logged as not built (not re-probed) | templates/repo/issue/view_title.tmpl; pulls/asc_signature_badge.tmpl (badge exists on versions only); agent run link needs revers... |
| Tab strip: Overview, Files, Versions v1>v4, Checks, How it was made | missing (UI) | logged as not built (not re-probed) | templates/repo/pulls/tab_menu.tmpl (has Conversation/Commits/Files; Versions and Checks are a panel and a link) |
| Intent, 'Where to look' pointers (file:line), Size, Files, Risk summary block | missing (hub+asc) | logged as not built (not re-probed) | hub: issue/view_content/pull.tmpl; asc: change metadata for pointers and a risk classifier (proto/asc/v1/changes.proto, repo.prot... |
| Files by meaning: per-file Behavior/Tests/Generated badge | done | templates/repo/diff/asc_class_badge.tmpl (file) | - |
| Files by meaning: group counts and filter above the diff | partial (UI) | logged as not built (not re-probed) | templates/repo/diff/box.tmpl (data exists in AscFileClassBadges) |
| 'Changed since you looked' count, 'Since my last review v3>v4' selector, 'Changed in v4' badge... | partial (UI) | logged as not built (not re-probed) | templates/repo/pulls/asc_versions.tmpl (link asc-since-last-review exists; per-file badge, count, hunk hiding missing) |
| Unified/Split layout, collapse file, Viewed checkbox with progress | done | templates/repo/diff/box.tmpl:32 | - |
| Hunk header shows enclosing function (impl PaymentsClient > async fn) | missing (hub) | logged as not built (not re-probed) | services/gitdiff hunk header |
| Inline comment threads: open/resolved, replies, Resolve, markdown composer | done | routers/web/repo/pull_review.go:188 | - |
| Threads visible to asc clients: hub comments are not written to the asc Review service (ListTh... | missing (hub+asc) | logged as not built (not re-probed) | proto/asc/v1/review.proto; services/ascsync (new review-thread sync); pull_review.go |
| Agent reply in a thread labelled 'Agent' with avatar | missing (UI) | logged as not built (not re-probed) | templates/repo/issue/view_content/comments.tmpl show_role |
| Suggested change block in a comment, 'Apply as v5' (creates a version, undoable op, resets app... | missing (hub+asc) | logged as not built (not re-probed) | asc: no suggestion field in review.proto and no amend RPC; hub: composer + apply handler in pull_review.go |
| Keyboard review shortcuts j/k file, a approve, r request changes, c comment, v viewed, n next... | done | web_src/js/features/asc-keys.js:3 | - |
| 'Your review' checklist (read intent, opened 2 of 3 files, N open comments) with Approve / Req... | missing (UI) | logged as not built (not re-probed) | templates/repo/diff/new_review.tmpl, pulls/asc_panel.tmpl |
| Activity timeline: version pushed, comment, approval carried, restacked (14 events, Show all) | missing (hub) | logged as not built (not re-probed) | routers/web/repo/issue_view timeline + asc ListRefLog (proto/asc/v1/stacks.proto) merged in |
| Checks on v4 summary card (unit, lint, integration running, security scan stale) with 'Open ch... | missing (hub) | logged as not built (not re-probed) | templates/repo/pulls/asc_panel.tmpl (links only); data from AscPullChecks in asc_checks.go |
| Policy gate card: approvals 1 of 2, carried approval note, required checks, signed with run ke... | done | templates/repo/asc_gate.tmpl (file) | - |
| Approval carry-over: approval on an unchanged diff survives a new version and says so | done | templates/repo/pulls/asc_brief.tmpl:50 | - |
| Landing card: queue position, estimated start, 'Add changes 1 and 2 to queue', dry-run line | missing (hub+asc) | logged as not built (not re-probed) | templates/repo/pulls/asc_stack.tmpl (queue link only); ETA needs queue model (mergequeue.proto has position only) |
| Restack banner + action when trunk moved | done | templates/repo/pulls/asc_stack.tmpl:2 | - |
| 'Restacked onto main, automatic, 3 min ago' line with Undo | missing (hub) | logged as not built (not re-probed) | routers/web/repo/asc_ops.go UndoRef link on the review page |
| Reviewers list with status, request, suggested reviewers, agent reviewer (reviewer-1) | done | templates/repo/asc_confirm.tmpl:70 (best match) | - |
| 'How it was made': run id, task, tool calls, versions, files read, 'never touched infra/', lin... | partial (hub+asc) | templates/repo/pulls/asc_brief.tmpl (how_made_counts) | Counts line exists; run id, task and never-touched paths were not all found. |
| Conflict with main: stays open, other changes unblocked, markers, 'Resolve in editor', 'Show b... | partial (hub+asc) | logged as not built (not re-probed) | templates/repo/pulls/asc_badges.tmpl (conflict banner with paths exists); conflict hunk viewer + agent task (Agents.CreateRun) |
| Loading skeleton keeping layout; load error with 'Try again' and 'Open previous version'; comm... | missing (UI) | logged as not built (not re-probed) | templates/repo/pulls/files.tmpl, diff/box.tmpl |
| Applied-as-v5 toast with Undo (8 s, role=status) | missing (hub+asc) | logged as not built (not re-probed) | depends on suggestion apply; toast infra web_src/js/features/ds-shell.js |

#### ReviewVersions (ASC Hub versions and landing)

Area review. Rows 13: done 9, partial 1, missing 3. Copy found in the hub: 49% of 78 copy strings (mobile twin 64%). Template(s): asc_compare.tmpl, asc_confirm.tmpl, asc_graph.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Stack - 4 changes; main @ 4be19a2; Merge order is enforced:; Merge 1 and 2 now; Change 3 of 4; Every push is kept as a version. Compare any two, restore one as a new version,....

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Version timeline with Base/Head radios per column, current badge, signature badge, actor and a... | done | templates/repo/pulls/asc_versions.tmpl:9 | - |
| Version row detail: commit message, short hash, +add/-del, per-version check chips (unit pass,... | done | templates/repo/pulls/asc_versions.tmpl:10 | - |
| 'Approved by Priya' marker on the version she approved | done | templates/repo/pulls/asc_versions.tmpl:12 | - |
| Restack rows shown as 'ASC, automatic ... no diff change' | done | services/pull/asc_version_details.go (file) | - |
| Restore as new version (never rewrites history) | done | routers/web/repo/pull_asc_restore.go:16 | - |
| Compare any two versions (interdiff), split/unified | done | routers/web/repo/pull_asc_versions.go:20 | - |
| Compare header: '+2 -1 in 1 of 3 files, Priya's approval still applies to the other 2' | missing (hub+asc) | logged as not built (not re-probed) | asc: per-file unchanged set; hub: pulls/asc_versions.tmpl |
| Restack-available banner 'Trunk moved 3 commits since v2' with Restack | done | templates/repo/pulls/asc_stack.tmpl:2 | - |
| Land-the-stack panel: scope radios (Bottom 2 / All 4 / Just 1), dry run re-run on scope change | done | templates/repo/asc_confirm.tmpl (file) | - |
| Land panel summary: merge order, signed-by list, queue position and ETA, undo window 30 min | missing (hub+asc) | logged as not built (not re-probed) | templates/repo/asc_confirm.tmpl; ETA as in Review |
| Confirm for Merge now names branch, current hash and result hash; default focus Cancel | missing (hub) | logged as not built (not re-probed) | templates/repo/asc_confirm.tmpl, routers/web/repo/asc_confirm.go |
| Result toast 'Merged 2 changes... Undo is open for 30 minutes' with Undo; undone toast | partial (hub) | logged as not built (not re-probed) | routers/web/repo/asc_ops.go (UndoRef exists); toast in ds-shell.js |
| Merge refused because trunk moved, 'Reload dry run' | done | templates/repo/asc_confirm.tmpl (file) | - |

### Area: agents-ops

#### AgentAccess (ASC Hub agent access)

Area agents-ops. Rows 8: done 5, partial 0, missing 3. Copy found in the hub: 77% of 111 copy strings (mobile twin 81%). Template(s): asc_agents_policy.tmpl, asc_agents_tokens.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Each agent works with its own token. A token says which paths it may write, how...; Limits per run; reviewer-1; Expires in 2 days; Permission and plan requests from agents; Plan: rewrite 12 docs pages.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Tokens: name, hint, scope paths, per-run limits, status, expiry, last used, meters; Make token... | done | templates/repo/asc_agents_tokens.tmpl:60 | - |
| Secret shown once with copy and ASC_TOKEN export line | done | templates/repo/asc_agents_tokens.tmpl (file) | - |
| Spend this month per token ($58 of $120) as a monthly budget | missing (hub+asc) | logged as not built (not re-probed) | asc: token budgets are per run or lifetime, no monthly window or month spend (AgentBudgetLimits/Usage); hub: column |
| Approvals log (permission and plan requests with decision and decider) | missing (hub+asc) | logged as not built (not re-probed) | depends on the permission/plan feature (see AgentRun); asc audit events + hub list |
| Policy defaults card: write scope, never trunk, changes per run, cost cap, token lifetime, who... | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_agents_policy.tmpl (can/cannot and limits shown; cost cap, lifetime, deciders missing) |
| Spend this month panel: cap, daily spend chart, per-agent totals, 'people using AI keys' | done | see where-cell | - |
| Revoke confirm dialog naming the agent; Budget dialog | done | templates/repo/asc_agents_tokens.tmpl (file) | - |
| Empty state 'No tokens yet' | done | templates/repo/asc_agents_tokens.tmpl (file) | - |

#### AgentNew (ASC Hub give an agent a task)

Area agents-ops. Rows 8: done 6, partial 1, missing 1. Copy found in the hub: 61% of 46 copy strings (mobile twin 67%). Template(s): asc_agents_new.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: What to do, and what to leave alone. Keep changes small and stacked. The agent...; Plain language. Mention files or tests it should use, and anything it must not...; Limits for this run; Add a path, one per line; Leave empty for the token scope. A run can only narrow what its token allows.; Starting is logged. You can stop the run at any time and keep the work..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Task, target branch, model/key, limits (changes, tokens, cost, minutes), narrow write scope, s... | done | templates/repo/asc_agents_new.tmpl (file) | - |
| Agent picker showing each agent's scope (coder-2 src/**, tests/**) | done | templates/repo/asc_agents_new.tmpl (file) | - |
| 'Ask me to approve the plan first' switch | done | templates/repo/asc_agents_new.tmpl:82 (best match) | - |
| Right rail 'What this run may do', computed from token and policy, updates as scope narrows | missing (hub) | logged as not built (not re-probed) | Policy.Evaluate RUN/WRITE (proto/asc/v1/policy.proto); asc_agents_new.tmpl + JS fetch |
| Estimate 'About 10 min and $1.60' from similar runs | done | templates/repo/asc_agents_new.tmpl:134 (best match) | - |
| Recent tasks with Reuse | done | templates/repo/asc_agents_new.tmpl:143 (best match) | - |
| Limits above the policy cap are lowered with a message, not rejected | partial (hub+asc) | templates/repo/asc_agents_run.tmpl (run_edit_limits_help) | Help text says a value above the cap is lowered; the New agent form message was not found. |
| Started toast 'Run 216 started' with Open; can-start right required | done | templates/repo/asc_agents_new.tmpl:124 (best match) | - |

#### AgentRun (ASC Hub agent run)

Area agents-ops. Rows 18: done 13, partial 2, missing 3. Copy found in the hub: 58% of 101 copy strings (mobile twin 57%). Template(s): asc_agents_run.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: reviewer-1 -; done, 2 changes; sonnet-5; It wants to write; to raise the gateway timeout. This is outside its write scope (; You approved at 10:02.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Runs side list / switcher (runs 214, 213, 212 ...) with New task | done | templates/repo/asc_agents_run.tmpl (file) | - |
| Task quote ('Task by Maya Okafor: ...') on the run page | done | see where-cell | - |
| Header: state chip, model, started-by, age, target, workflow link | done | templates/repo/asc_agents_run.tmpl:4 | - |
| Pause (holds the run at the next tool call) | done | templates/repo/asc_agents_run.tmpl (run_pause_help); routers/web/repo/asc_agents_ctl.go (PauseRun) | Gap file says asc has no PauseRun; asc master has Agents.PauseRun and the hub calls it. |
| Stop with confirmation and reason; keeps work as drafts | partial (UI) | templates/repo/asc_agents_run.tmpl (agents_stop_help) | Stop help text is there; confirmation with a reason was not found. |
| Plan block with step states and 'You approved at 10:02' | missing (hub+asc) | logged as not built (not re-probed) | asc: plan events/state missing; hub: plan card |
| Permission request banner: Allow once / Deny and continue / Answer in own words; records who a... | done | templates/repo/asc_agents_run.tmpl:91 (best match) | - |
| Live activity log: poll, follow unless scrolled, 'Show everything' toggle, error handling | done | web_src/js/features/asc-agent-run.ts:334 | - |
| 'Reconnecting: showing the log as of 10:14' banner | partial (UI) | logged as not built (not re-probed) | asc-agent-run.ts (maxFailures logic exists, no banner) |
| Steer this run / answer the agent | done | templates/repo/asc_agents_run.tmpl (file) | - |
| Result: stack of changes with state and Review links; 'agent cannot merge' note | done | templates/repo/asc_agents_run.tmpl:182 | - |
| Budget meters: tokens, cost, time, changes with numbers | done | templates/repo/asc_agents_meters.tmpl:2 | - |
| Budget 'Edit limits' on a live run | done | see where-cell | - |
| Access for this run: can/cannot list, token id and expiry, signed by run key | missing (hub) | logged as not built (not re-probed) | templates/repo/asc_agents_policy.tmpl (repo policy card only); use AgentRun.token_id + AgentTokens.GetToken |
| Checkpoints list with Roll back (logged) | done | templates/repo/asc_agents_run.tmpl (run_cp_help); asc_agents_ctl.go (ListCheckpoints, RollbackRun) | Gap file says asc has no checkpoint RPC; it has, and the hub calls it. |
| Audit note and 'Download the audit log' (90 days, linked from each change) | done | templates/repo/asc_agents_run.tmpl:225 (best match) | - |
| Failed state 'hit its model token limit' with 'Raise the limit and continue' and Review the 3... | done | templates/repo/asc_agents_run.tmpl (run_continue) | Raise the limit and continue is in the template. |
| Done summary (38 tool calls, 11 min, $2.31) and Stopped notes | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_agents_run.tmpl (data in AgentBudgetUsage) |

#### Agents (ASC Hub agent runs)

Area agents-ops. Rows 14: done 12, partial 0, missing 2. Copy found in the hub: 71% of 93 copy strings (mobile twin 62%). Template(s): asc_agents.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: asks to write infra/limits.tf; Review web-app #88 and leave notes; read-only run; reviewer-1; Add rate limit headers; Fix flaky test cursor::page_boundary.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Stats: Running now, Waiting on you | done | templates/repo/asc_agents.tmpl:68 | - |
| Stats: Landed this week (31% of changes), Spend this month ($142 of $400 cap) | done | see where-cell | - |
| State chips with counts, agent filter, text search over runs | done | templates/repo/asc_agents.tmpl:70 | - |
| Comfortable/Compact density, persisted per person | done | templates/repo/asc_agents.tmpl:85 | - |
| Run row: title, 'for stack', run id, agent, tokens used/limit with bar, duration, starter avat... | done | templates/repo/asc_agents.tmpl:101 | - |
| Run row progress '3 of 4 steps' with bar | done | templates/repo/asc_agents.tmpl:105 (best match) | - |
| Row actions Open run and Stop (confirm + reason logged) on hover/focus | done | templates/repo/asc_agents.tmpl (file) | - |
| 'Needs you' panel: permission request (Allow once / Deny) and plan approval (Review plan), app... | done | templates/repo/asc_agents.tmpl:5 (best match) | - |
| 'Agents on this repository' card (scope, status, changes landed in 30 days, Manage access) | done | templates/repo/asc_agents.tmpl:164 (best match) | - |
| Numbered pagination and 'Showing 5 of 24' | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_agents.tmpl (only an 'Older runs' cursor link) |
| Sub-navigation Runs / Workflows / Access / Spend | done | templates/repo/asc_agents_nav.tmpl:2 | - |
| Agent features off state with 'Turn on agent features' | done | templates/repo/asc_agents_notice.tmpl (file) | - |
| Empty state; loading skeleton | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_agents.tmpl (empty done; skeleton missing) |
| Header pill 'N agents running' on every page | done | templates/repo/asc_agents_pill.tmpl (file) | - |

#### Audit (ASC Hub audit log)

Area agents-ops. Rows 6: done 2, partial 1, missing 3. Copy found in the hub: 85% of 96 copy strings (mobile twin 81%). Template(s): asc_audit.tmpl, asc_audit_table.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: main, ag_, .asc/; change.approve; agent.token.use; Hannah Weiss; secret scan: cloud access key; reason: release freeze.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Chain status, Verify, filters (who, what, outcome, target, from/to, order, rows), table, older... | done | templates/repo/asc_audit_table.tmpl:112 | - |
| Repository, organization and site scope from one page | done | templates/repo/asc_audit_table.tmpl:2 (best match) | - |
| Verify shows a determinate progress bar | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_audit_table.tmpl (plain POST; VerifyChain is unary, so progress needs polling or an asc stream) |
| Broken chain: 'Show event 9,204' and 'Export evidence' | partial (UI) | templates/repo/asc_audit_table.tmpl (chain_broken) | Broken-chain text exists; Show event and Export evidence actions not confirmed. |
| Export-ready card (file name, size, event count, Download) | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_audit_table.tmpl (direct download link only) |
| Retention label 'Kept 365 days'; no-match state with Clear filters | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_audit_table.tmpl |

#### Ops (ASC Hub operation log)

Area agents-ops. Rows 13: done 8, partial 0, missing 5. Copy found in the hub: 74% of 96 copy strings (mobile twin 87%). Template(s): asc_ops.tmpl, asc_ops_undo.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Every change to a branch of this repository, newest first. You can undo your ow...; Branch or ref: any; Trunk moved 3 commits. Your stack restacked cleanly.; See what changed; ASC, automatic; undo is open for 30 min.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Operation list: kind pill, ref, actor, age, hash pair, Undo | done | templates/repo/asc_ops.tmpl (file) | - |
| Blocked reasons beside a lock icon (not yours, branch changed, too old, protected needs mainta... | done | templates/repo/asc_ops.tmpl:108 | - |
| Branch filter, Only my operations; filters in URL; older operations pager | done | templates/repo/asc_ops.tmpl:29 | - |
| Kind filter chips (Push, Landed, Restack, Undo, Agent) and '6 of 214' count | done | templates/repo/asc_ops.tmpl (file) | - |
| Restack banner with 'See what changed' and 'Undo restack' | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_ops.tmpl |
| Export of operations | done | routers/web/repo/asc_ops.go (file) | - |
| 'Who changed branches this week' actor bars with 7/30 window | done | see where-cell | - |
| Selecting a row loads the undo preview in a right panel without navigating | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_ops.tmpl, asc_ops_undo.tmpl (separate page today); web_src/js |
| Undo preview (now at / goes back to / who / reason) and undo is itself logged | done | templates/repo/asc_ops_undo.tmpl (file) | - |
| Preview lines '4 changes go back', 'approvals on 2 unchanged diffs are kept' | missing (hub+asc) | logged as not built (not re-probed) | UndoRef/preview must report affected changes and kept approvals; asc_ops_undo.tmpl |
| Undone toast with Redo | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_ops.tmpl (Redo = undo of the undo row) |
| No-match state with Clear filters; loading skeleton | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_ops.tmpl |
| Refused state with Reload | done | templates/repo/asc_ops.tmpl (file) | - |

#### Policy (ASC Hub policy gate)

Area agents-ops. Rows 10: done 9, partial 0, missing 1. Copy found in the hub: 65% of 121 copy strings (mobile twin 57%). Template(s): asc_confirm.tmpl, asc_policy.tmpl, asc_queue.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: What the rules of this repository held back, why, and how it was cleared. Rules...; Policy: 2 approvals, checks required; why and how it was cleared; Waiting for Diego Alvarez; Unsigned commit; Re-signed with key tr_09ab.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Header: policy valid pill, 'Edit the rules' link to Settings, 7/30 day window | done | templates/repo/asc_gate.tmpl:137 | - |
| Stat cards: Changes checked, Passed first time, Held by the gate (cleared/open), Median time t... | done | see where-cell | - |
| Held-by-the-gate list: change, reason, rule, who, how cleared, time later, Open change | done | templates/repo/asc_gate.tmpl:104 | - |
| Policy file view, 'What this means', 'Who can write here' table | done | templates/repo/asc_gate.tmpl (file) | - |
| 'Checked and held, per day' chart with table equivalent | done | templates/repo/asc_gate.tmpl (file) | - |
| Rules in force with 'N held of M' per rule | done | see where-cell | - |
| Maintainer overrides (signed, logged exception, reason) | done | templates/repo/asc_gate.tmpl (file) | - |
| Quarantined checks summary with link to the queue | done | templates/repo/asc_gate.tmpl (file) | - |
| Invalid-file fallback, nothing-held state | done | templates/repo/asc_gate.tmpl (file) | - |
| Loading skeleton | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_gate.tmpl |

#### Queue (ASC Hub merge queue)

Area agents-ops. Rows 11: done 10, partial 1, missing 0. Copy found in the hub: 74% of 94 copy strings (mobile twin 74%). Template(s): asc_confirm.tmpl, asc_queue.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: next lands in about 3 min; Refused after dry run; both needed a content merge; Rate limit headers; unit running on merged trunk, about 3 min; Queued by Diego Alvarez.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Branch, running/paused chip, revalidate on/off, Pause/Resume confirm dialogs | done | templates/repo/asc_queue.tmpl:4 | - |
| Stats: Waiting, Landed today, Median wait (was 9 min last week), Refused after dry run | done | templates/repo/asc_queue.tmpl:45 (best match) | - |
| Waiting list: position, title, state chip (Checking/Queued), queued-by, Remove | done | templates/repo/asc_queue.tmpl (file) | - |
| Row detail: 'unit running on merged trunk, about 3 min' and 'starts about 6 min' ETAs | done | templates/repo/asc_queue.tmpl:62 (best match) | - |
| Merge now per row (hover/focus) with confirm | partial (UI) | logged as not built (not re-probed) | templates/repo/asc_queue.tmpl (POST /merge exists) |
| Queue reordering (not on the board: order is queue order, no reorder RPC in mergequeue.proto) | done | templates/repo/asc_confirm.tmpl:4 (best match) | - |
| Quarantined checks with reason, owner, review date | done | templates/repo/asc_queue.tmpl:83 | - |
| Recently finished (merged / removed with who and when) | done | templates/repo/asc_queue.tmpl:123 | - |
| 'Merge a stack' panel: scope radios, dry run, Add to queue / Merge now | done | templates/repo/asc_queue.tmpl (file) | - |
| 'How landing stays safe' card; paused and empty states | done | templates/repo/asc_queue.tmpl:4 | - |
| Live update: list slides when an item lands, Checking pulses | done | routers/web/repo/asc_queue_live.go (file) | - |

#### Security (ASC Hub secret scanning)

Area agents-ops. Rows 9: done 9, partial 0, missing 0. Copy found in the hub: 77% of 115 copy strings (mobile twin 69%). Template(s): asc_security.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: built-in plus 3 custom; secret_scan.mode; Showing 5 findings; Cloud access key - Push refused, nothing stored; Hannah Weiss; Payment provider token - Found by a scan of main.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Summary cards: Open findings, Pushes refused 30 days, Rules count, Allowlist count | done | templates/repo/settings/asc_security.tmpl:2 | - |
| Scan main now with inline progress | done | web_src/js/features/asc-security.js (file) | - |
| Findings list: file:line, rule, who, age, severity, state tabs | done | templates/repo/settings/asc_security.tmpl:74 | - |
| Set state Revoked / False positive / Accept risk with Undo toast | done | web_src/js/features/asc-security.js:36 | - |
| 'File starts with...' filter | done | templates/repo/settings/asc_security.tmpl (file) | - |
| Pushes refused list from the audit log | done | templates/repo/settings/asc_security.tmpl:2 | - |
| Allowlist saved as a proposed change | done | templates/repo/settings/asc_security.tmpl:54 | - |
| Rules table with source and 'Show all 142' | done | templates/repo/settings/asc_security.tmpl (file) | - |
| Blocks-pushes mode from policy secret_scan.mode; unavailable and empty states | done | templates/repo/settings/asc_security.tmpl:2 | - |

#### WorkflowDef (ASC Hub workflow definition)

Area agents-ops. Rows 9: done 9, partial 0, missing 0. Copy found in the hub: 71% of 68 copy strings (mobile twin 76%). Template(s): asc_workflows_def.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: dependency-update; dependency-update; Propose the weekly dependency updates as a reviewed stack. Security fixes first...; at most 1 run at once; Audit lockfiles for advisories; deps-audit.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Header: name, on/off switch, path, Run it now | done | templates/repo/asc_workflows_def.tmpl (file) | - |
| Steps table (kind, after, details, timeout) with state of latest run and connector fill | done | templates/repo/asc_workflows_def.tmpl (file) | - |
| Starts-when: cron, by hand incl. CLI command | done | templates/repo/asc_workflows_def.tmpl (file) | - |
| Run-it-now form with typed inputs and attach-to-change; inline validation | done | templates/repo/asc_workflows_def.tmpl (file) | - |
| Latest runs of this workflow | done | templates/repo/asc_workflows_def.tmpl:5 | - |
| Inputs and budget card (tokens, cost cap, write scope, signing key) | done | templates/repo/asc_workflows_def.tmpl:130 | - |
| Secrets used card (REGISTRY_TOKEN set) | done | templates/repo/asc_workflows_def.tmpl (file) | - |
| File view with Copy button | done | templates/repo/asc_workflows_def.tmpl (file) | - |
| 'Edit as a change' opens the file in the editor as a reviewed change | done | templates/repo/asc_workflows_def.tmpl:107 (best match) | - |

#### WorkflowRun (ASC Hub workflow run)

Area agents-ops. Rows 9: done 5, partial 1, missing 3. Copy found in the hub: 63% of 46 copy strings (mobile twin 71%). Template(s): asc_workflows_run.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: dependency-update; started manually by; This step needs a decision.; Why you decide this way...; Your decision is logged with your signature.; Passed in 48 s.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Run header: state, started by, time, about change; Cancel run, Run again with same inputs | done | templates/repo/asc_workflows_run.tmpl (file) | - |
| Cancel confirmation dialog | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_workflows_run.tmpl |
| Approval banner: comment, Approve / Decline, signed log entry, 'an agent can never decide' | done | templates/repo/asc_workflows_run.tmpl:78 | - |
| Flow diagram with connectors filling as steps finish | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_workflows_run.tmpl |
| Steps list with output line, duration, state | done | templates/repo/asc_workflows_run.tmpl (file) | - |
| Live polling of step state | done | web_src/js/features/asc-agent-run.ts (file) | - |
| 'Proposed by the agent': changes with size, state and Review links | missing (hub) | logged as not built (not re-probed) | asc_workflows.go: resolve the step's agent run to changes via services/ascagentui/links.go |
| Step log (agent events) and 'Open the agent run' link | partial (UI) | logged as not built (not re-probed) | templates/repo/asc_workflows_run.tmpl (log details exist; link missing) |
| 'Used so far' meters (tokens, cost, time, agent steps) | done | templates/repo/asc_workflows_run.tmpl (file) | - |

#### Workflows (ASC Hub workflows)

Area agents-ops. Rows 11: done 8, partial 1, missing 2. Copy found in the hub: 60% of 107 copy strings (mobile twin 64%). Template(s): asc_workflows.tmpl, asc_workflows_runlist.tmpl, asc_workflows_runs.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: A workflow is a file in the repository that chains agent steps, checks and huma...; Find a workflow; dependency-update; Propose the weekly dependency updates as a reviewed stack; Every Monday 06:00 UTC; last Today 06:02.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Definitions list with triggers, description, path, valid/not valid reason | done | templates/repo/asc_workflows.tmpl (file) | - |
| On/off switch (role=switch) and Run now on each list row | done | templates/repo/asc_workflows.tmpl (file) | - |
| Step chain 'scan > update > review' on the card | done | templates/repo/asc_workflows.tmpl (file) | - |
| Last-12-runs history strip and 'last run' time per workflow | done | templates/repo/asc_workflows.tmpl:20 | - |
| State filter (All, On, Off, Not valid), search, counts | done | templates/repo/asc_workflows.tmpl (file) | - |
| 'New workflow' and 'Start from a template' (creates a reviewed change with the file) | missing (UI) | logged as not built (not re-probed) | New workflow is a link to the hub editor on a new branch under .asc/workflows/ (done); Start from a template is not built (no tem... |
| Latest runs list (running, passed, needs approval, failed) with link to all runs | done | templates/repo/asc_workflows_runlist.tmpl (file) | - |
| Secrets and variables panel (names per workflow) with link to settings | done | templates/repo/asc_workflows.tmpl:134 | - |
| Capacity '3 running of 6 allowed' and shared agent budget line | done | templates/repo/asc_workflows.tmpl:141 | - |
| No-workflows state with template and guide buttons | partial (UI) | logged as not built (not re-probed) | templates/repo/asc_workflows.tmpl (text exists, buttons missing) |
| Loading skeleton | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_workflows.tmpl |

### Area: auth

#### AdminAuth (ASC Hub admin: authentication)

Area auth. Rows 2: done 1, partial 0, missing 1. Copy found in the hub: 85% of 47 copy strings (mobile twin 90%). Template(s): edit.tmpl, list.tmpl, new.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Directory (LDAP); ASC Hub 1.4.2; Hub and web UI based on Forgejo; All systems operational; Rendered in 18 ms.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Sources list with edit and add | done | templates/admin/auth/list.tmpl (file) | - |
| People count per source; Local passwords row (admins only option); Test sign-in showing claims... | missing (hub) | logged as not built (not re-probed) | routers/web/admin/auths.go, auth/list.tmpl, new test route |
| Board note: Test sign-in runs the full flow and shows the claims received | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Local passwords can be limited to admins | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, test sign-in result (pass/fail), no sources (setup), remove blocked by people wh... | unverified | not tracked in the gap files | needs a row and a test |

#### Onboarding (ASC Hub sign in and sign up)

Area auth. Rows 19: done 12, partial 3, missing 4. Copy found in the hub: 57% of 111 copy strings (mobile twin 51%). Template(s): activate.tmpl, forgot_passwd.tmpl, grant.tmpl, reset_passwd.tmpl, signin.tmpl, signup.tmpl, twofa.tmpl, webauthn.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Quiet, fast, and honest about errors; Sign in to ASC Hub; Keep me signed in on this device; Sign in with a passkey; Signing in from the CLI?; . This page will ask you to approve the device..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Split layout: form card plus navy brand panel with promise and quote | done | templates/user/auth/shell_start.tmpl (file) | - |
| Sign in form: username or email, Forgot password, password with Show toggle, Keep me signed in... | partial | templates/user/auth/signin_inner.tmpl:36 | Form is there; the CLI hint teaches the up-front asc auth login --prompt with a pasted token, not on-demand per-repository sign-i... |
| "Sign in with a passkey" (discoverable credentials) | missing (hub) | absent: no passkey or discoverable-credential code in routers/web/auth, templates/user/auth | Only 2FA security keys exist (webauthn.tmpl); no passkey login route anywhere in routers. Add begin and finish routes in routers/... |
| Single sign-on button under the primary action | done | templates/user/auth/oauth_container.tmpl (file) | - |
| Rate limit ("after 5 attempts we wait 5 minutes") | missing (hub) | absent: no login attempt counter in routers/web/auth/auth.go, services/auth, modules/setting/securi... | No login throttle in routers/web/auth/auth.go or modules/setting. Counter per user and IP in cache, message with the wait |
| Sign up: username with live "Your handle: @name" and blur validation | done | templates/user/auth/signup_inner.tmpl (file) | - |
| Sign up: optional Full name | done | templates/user/auth/signup_inner.tmpl (file) | - |
| Sign up: strength meter and three live rules (12 characters, letters and numbers, not found in... | missing (hub) | absent: no strength or rules code in asc-signup.js or signup_inner.tmpl | No meter in web_src. Rules and meter in user-auth.js; breach rule is server side: PASSWORD_CHECK_PWN exists (modules/setting/secu... |
| Sign up: "I agree to the Terms and the Privacy policy" | missing (hub) | absent: no Terms and Privacy agreement field in signup_inner.tmpl or services/forms | Not in the form. Needs Terms and Privacy URLs, a required checkbox and a stored acceptance |
| Sign up has no retyped password on the board; the hub form asks for one | done | see where-cell | - |
| Two-factor: six boxes act as one input and auto-advance; recovery code link; "Use a security k... | partial | templates/user/auth/twofa.tmpl; web_src/js/features/asc-codeboxes.js | Six boxes and recovery link exist; the link Use a security key instead is not in twofa.tmpl. |
| CLI device approval: code entry, confirm with device name, Authorise or Deny, expiry, wrong-co... | done | models/auth/asc_device_code.go:20 | - |
| Security key waiting screen with spinner and "Try another way" | partial | templates/user/auth/webauthn.tmpl | Marked done; Try another way is not in the template. |
| Authorise the CLI (device): code, terminal and OS, Authorise and Deny | done | templates/user/auth/asc_device.tmpl; routers/web/auth/asc_device.go; routers/web/web.go:744 | Gap file says not present; the device page and RFC 8628 routes exist (the token it mints is full-scope and has no expiry, see sec... |
| Forgot password: sent page, lifetime, resend limit, no account disclosure | done | templates/user/auth/forgot_passwd.tmpl:2 | - |
| Reset password: new and confirm | done | templates/mail/auth/reset_passwd.tmpl (file) | - |
| Registration closed (invite only) and awaiting approval | done | templates/user/auth/signup_inner.tmpl:5 | - |
| Focus on first invalid field; autocomplete tokens | done | templates/shared/ds/field.tmpl:19 | - |
| Mobile: Sticky primary action above the keyboard-safe area; 48 px fields | done | web_src/css/asc-ds/touch.css (file) | - |

#### UserSettingsApps (ASC Hub settings: applications)

Area auth. Rows 2: done 1, partial 0, missing 1. Copy found in the hub: 74% of 61 copy strings (mobile twin 68%). Template(s): grants_oauth2.tmpl, hooks.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Apps you built, apps you authorized and webhooks.; OAuth2 apps that sign people in with ASC Hub.; Authorized applications; Apps that can act as you. Revoking is instant..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| OAuth2 apps, authorized apps with revoke, client secret shown once, regenerate | done | templates/user/settings/grants_oauth2.tmpl:2 (best match) | - |
| Webhooks table: events column, last delivery status text and time, Replay from the list | missing (UI) | logged as not built (not re-probed) | templates/repo/settings/webhook/base_list.tmpl is old markup (dot + URL only); data from hook_task |
| Board note: Client secrets are shown once and can be regenerated | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Webhook Replay re-sends the last failed delivery | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Revoke removes the grant and every token issued under it | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Left menu is the only navigation for settings: no tabs repeat these pages | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, empty applications (illustration + New application), webhook failing, secret sho... | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Revoked toast | unverified | not tracked in the gap files | needs a row and a test |

#### UserSettingsKeys (ASC Hub settings: ssh and signing keys)

Area auth. Rows 5: done 4, partial 0, missing 1. Copy found in the hub: 70% of 66 copy strings (mobile twin 62%). Template(s): asc_keys.tmpl, keys.tmpl, keys_gpg.tmpl, keys_ssh.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Changesets signed by ASC show a verified badge to reviewers and in the operatio...; Sign my changesets by default; Every changeset you author carries a signature from the key below.; ASC signing key.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| SSH keys (fingerprint, added, last used, delete) and GPG keys | done | templates/user/settings/keys_ssh.tmpl (file) | - |
| ASC signing keys list, add, revoke with state chips | done | templates/user/settings/asc_keys.tmpl (file) | - |
| Rotate action that keeps the old key verifying past signatures | done | services/asctrust/rotate.go (file) | - |
| Sign my changesets by default switch | missing (hub+asc) | logged as not built (not re-probed) | hub pref + asc config the CLI reads (no field in proto/asc/v1/signing.proto) |
| Delete confirm that lists what stops working | done | templates/user/settings/keys_gpg.tmpl:92 (best match) | - |
| Board note: Delete asks to confirm and lists what stops working | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Rotate keeps the old key verifying past signatures | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Last used helps spot keys that can go | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Left menu is the only navigation for settings: no tabs repeat these pages | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default list, empty list with command to generate a key, invalid key error, key added toa... | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Signing on and off | unverified | not tracked in the gap files | needs a row and a test |

#### UserSettingsSecurity (ASC Hub settings: security)

Area auth. Rows 6: done 3, partial 1, missing 2. Copy found in the hub: 65% of 72 copy strings (mobile twin 57%). Template(s): -. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Password, two-factor, passkeys and the devices that are signed in.; At least 12 characters. A passphrase is stronger than symbols.; Strong. Would take centuries to guess.; A second step when you sign in from a new place..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Change password | done | templates/user/settings/account.tmpl:98 | - |
| Password strength meter in words after 300 ms, show/hide buttons | done | see where-cell | - |
| Two-factor enable/disable/re-enroll, passkeys (WebAuthn) add/delete | done | templates/user/settings/security/twofa.tmpl:11 | - |
| Recovery codes: "8 of 10 unused", download as text file, shown once | missing (hub) | logged as not built (not re-probed) | models/auth/twofactor.go (one scratch token only), security/twofa.tmpl |
| Signed-in devices list (browser, place, last active), Sign out one / everywhere else | partial (hub) | logged as not built (not re-probed) | asc-side credentials (tokens, SSH keys, agent tokens) with revoke and end-all are done on /user/settings/asc/sessions (Sessions s... |
| Re-authentication prompt before password change or removing 2FA | missing (hub) | logged as not built (not re-probed) | routers/web/user/setting/security + sudo-mode middleware |

#### UserSettingsTokens (ASC Hub settings: access tokens)

Area auth. Rows 6: done 3, partial 1, missing 2. Copy found in the hub: 62% of 96 copy strings (mobile twin 59%). Template(s): applications.tmpl, authorized_integrations.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Credentials for scripts, CI and the asc CLI. Prefer short-lived sign-in where y...; For scripts and the asc CLI. Give each one the least access that works.; asc CLI on laptop; changes:write.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Token list (name, scopes, created, last used) with revoke and regenerate | done | templates/user/settings/applications.tmpl (file) | - |
| Expiry per token (30 days etc.), Expires column, Expired chip | missing (hub) | models/auth/access_token.go:62-78 AccessToken has no expiry field | models/auth/access_token.go has no expiry; migration + access_token_edit.tmpl + check in services/auth |
| ASC scope set (Repositories, Changesets and stacks, Agent runs, Organizations, Profile) as Non... | missing (hub+asc) | models/auth/access_token_scope.go has Forgejo categories only; no changes, runs or profile scope | hub: models/auth/access_token_scope.go + access_token_edit.tmpl; asc: crates/asc-serve/src/authn.rs must accept and enforce them |
| Token created: copy-once card with Copy and "shown once" text (today a flash message) | done | routers/web/user/setting/access_token.go (file) | - |
| Workload identity (issuer, subject, last used, remove, add) | done | templates/user/settings/authorized_integrations.tmpl (file) | - |
| "Prefer no tokens" card with asc auth login commands | partial | templates/user/settings/applications.tmpl:15 asc auth login card | Card exists; copy differs from the board (no Prefer no tokens heading, no asc auth token --scope line). |

## 5. Gap-file rows corrected by this audit

Every row below was read in the tree; it is applied to the gap files (status and an Audit note in the Where cell). Rows marked done were open or wrongly described and are built; partial rows are built in part, or were marked done and are not complete.

| Row | Board | Was | Now | Evidence |
|---|---|---|---|---|
| rp53 | Repo | needs-hub-backend | done | templates/repo/home_cards.tmpl:12 |
| rv180 | AgentRun | needs-both | done | templates/repo/asc_agents_run.tmpl (run_pause_help); routers/web/repo/asc_agents_ctl.go (PauseRun) |
| rv181 | AgentRun | ui-only-missing | partial | templates/repo/asc_agents_run.tmpl (agents_stop_help) |
| rv191 | AgentRun | needs-both | done | templates/repo/asc_agents_run.tmpl (run_cp_help); asc_agents_ctl.go (ListCheckpoints, RollbackRun) |
| rv193 | AgentRun | needs-both | done | templates/repo/asc_agents_run.tmpl (run_continue) |
| rp237 | Insights | needs-both | done | templates/repo/insights_asc.tmpl (ins_landed, ins_opened, ins_median, ins_agents); routers/web/repo/asc_insig... |
| rp238 | Insights | needs-both | done | templates/repo/insights_asc.tmpl:51 svg chart, table toggle |
| rp239 | Insights | needs-hub-backend | done | templates/repo/insights_asc.tmpl:10-15 ins_who, ins_range segmented controls |
| rp240 | Insights | needs-hub-backend | done | templates/repo/insights_asc.tmpl:76-86 by week, by size of change |
| rp243 | InsightsPeople | needs-both | done | templates/repo/insights_people.tmpl (people_weeks, run_by, people_share) |
| rp244 | InsightsPeople | needs-both | done | templates/repo/insights_people.tmpl (who_writes, who_split, ins_all_approved) |
| rp245 | InsightsPeople | needs-hub-backend | done | templates/repo/insights_people.tmpl:58 review_load, agents_never_review |
| rp247 | InsightsPeople | ui-only-missing | partial | templates/repo/insights_people.tmpl (ins_empty_title) |
| rp318 | RepoSettingsPolicy | needs-both | partial | templates/repo/settings/asc_policy.tmpl:79-88 agents fieldset (scope, denied paths, runtime, changes, tokens,... |
| rp201 | Releases | needs-hub-backend | partial | templates/repo/release/list.tmpl (rel_prov) |
| rp202 | Releases | needs-hub-backend | partial | templates/repo/release/list.tmpl (rel_written_from) |
| rv80 | Review | needs-both | partial | templates/repo/pulls/asc_brief.tmpl (how_made_counts) |
| rv137 | Checks | ui-only-missing | partial | templates/repo/asc_checks.tmpl (checks.nothing_title) |
| rv206 | AgentNew | needs-both | partial | templates/repo/asc_agents_run.tmpl (run_edit_limits_help) |
| rv322 | Audit | ui-only-missing | partial | templates/repo/asc_audit_table.tmpl (chain_broken) |
| st84 | UserSettingsPrivacy | needs-both | partial | templates/user/settings/privacy.tmpl (sp_agents_profile) |
| rp162 | Compare | done | partial | templates/repo/asc_compare.tmpl |
| rp304 | RepoSettingsAccess | done | partial | templates/repo/settings/collaboration.tmpl |
| rp321 | RepoSettingsPolicy | done | partial | templates/repo/settings/asc_policy.tmpl:93 live file |
| sh42 | Shell | needs-asc-backend | partial | routers/web/asc_palette.go:34-60 GET /-/asc/palette (groups changes, people, repos); web_src/js/features/ds-s... |
| sh83 | Onboarding | done | partial | templates/user/auth/twofa.tmpl; web_src/js/features/asc-codeboxes.js |
| sh85 | Onboarding | done | partial | templates/user/auth/webauthn.tmpl |
| sh218 | Errors | done | partial | routers/web/web.go:657 /-/asc-sw.js; web_src/js/features/asc-offline.js |
| st98 | UserSettingsTokens | done | partial | templates/user/settings/applications.tmpl:15 asc auth login card |
| sh57 | Main | done | done | templates/home.tmpl:14,305 (href /download, detected OS in routers/web/asc_download.go) |
| sh56 | Main | done | partial | templates/home.tmpl:9 startpage.pill |
| sh60 | Main | done | partial | templates/home.tmpl:72 |
| sh61 | Main | done | partial | templates/home.tmpl:85-104 startpage.why_* |
| sh66 | Main | done | partial | templates/home.tmpl:338; templates/base/footer_content.tmpl |
| sh74 | Onboarding | done | partial | templates/user/auth/signin_inner.tmpl:36 |
| sh86 | Onboarding | needs-both | done | templates/user/auth/asc_device.tmpl; routers/web/auth/asc_device.go; routers/web/web.go:744 |

Rows with gap-language in the `Where` cell that are in fact built (the cell kept the audit text): shell.md lines 40 (combobox, `head_navbar.tmpl`), 45 (palette empty text `palette_nothing_for`), 57-59 (landing download link, install pills, product window), 78-79 (handle hint, full name), 110 (`aria-current=step`), 151, 181 (`models/user/asc_org_domain.go`), 195 (`asc-docs-toc.js`), 271 (profile tabs). Their `Where` text should be rewritten as evidence; they stay done.

## 6. Cross-cutting rows

| Element | Status | Evidence | Gap |
|---|---|---|---|
| RepoSettingsMenu: One left menu with groups Repository (General, Access N, Rules, Policy, Branches and stac... | missing (UI) | logged as not built (not re-probed) | H:templates/repo/settings/navbar.tmpl has General / Code / Integrations / BYO / Policy groups with Forgejo pages (Units, Collabor... |
| RepoSettingsMenu: Mobile root list with status per row ("1 failing"), "Find a setting" search over names an... | partial (UI) | logged as not built (not re-probed) | snav_* templates have no search or status; new settings index partial |
| Mobile-RepoSettings*: Detail pages with back arrow and same title, one save bar | done () | see where-cell | - |
| Mobile-Changeset, Compare, Re...: Same behaviour as the desktop boards | done () | see where-cell | - |
| Mobile twins: Mobile-Review: actions sheet (Approve, Request changes, queue, Undo restack, Copy asc com... | missing (UI) | logged as not built (not re-probed) | new mobile sheet + web_src/js; templates/repo/diff/box.tmpl |
| Mobile twins: Mobile-Review: Approve / Request changes pinned above the tab bar | partial (UI) | logged as not built (not re-probed) | templates/repo/pulls/files.tmpl (rv-sticky exists on Agents only) |
| Mobile twins: Mobile-Review: long-press a line to comment | missing (UI) | logged as not built (not re-probed) | web_src/js (touch handler on diff lines) |
| Mobile twins: Mobile-ReviewVersions: confirm as a sheet with default focus Cancel; toast with Undo | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_confirm.tmpl (page, not sheet) |
| Mobile twins: Mobile-Checks: failures as cards, chips scroll sideways, Re-run failed pinned | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_checks.tmpl responsive cards |
| Mobile twins: Mobile-Checks: swipe a card left for Acknowledge | missing (UI) | logged as not built (not re-probed) | web_src/js (new gesture) |
| Mobile twins: Mobile-AgentRun: Allow once / Deny pinned; pull to refresh | partial (UI) | logged as not built (not re-probed) | templates/repo/asc_agents_run.tmpl + asc-agent-run.ts (follow logic exists) |
| Mobile twins: Mobile-Ops: undo preview as a bottom sheet | missing (UI) | logged as not built (not re-probed) | templates/repo/asc_ops_undo.tmpl |
| Mobile twins: Other mobile twins (Agents, Workflows, Queue, Policy, Audit, Security, AgentNew, AgentAcc... | done () | see where-cell | - |
| MobileUserSettings*: Settings menu as one list page, Other group (Site administration for admins, Sign out), b... | done () | templates/user/settings/navbar.tmpl (file) | - |
| MobileUserSettings*: Save bar sticks above the bottom nav once a field changes | done () | see where-cell | - |
| MobileAdmin*: Same data as desktop in stacked groups, back arrow to the admin menu, sticky action bar | done () | templates/admin/navbar.tmpl (file) | - |
| MobileIssues/Projects/Package...: Single-column twins; same gaps as the desktop rows above; sticky primary bar on forms | missing (UI) | logged as not built (not re-probed) | per-row work above |

## 7. Prioritized work units

Order is by effect on the requirement (a visitor sees the board; a person can sign in, hold a token and review safely; the flows work end to end), then by size. Row ids (rp = repo.md, rv = review.md, sh = shell.md, st = settings.md, with the line number) point into the tables of section 4. Acceptance checks use the pixel harness (`tests/pixel` on branch `pixel/harness`, to be merged by WU-30), `go test` in the named package, `npx vitest`, and the all-in-one journey (`tests/asc-e2e`).

| Unit | Title | Area | Files | Acceptance check |
|---|---|---|---|---|
| WU-01 | Landing page equals the Main board word for word | shell/settings | `templates/home.tmpl`, `options/locale/locale_en-US.ini` (`startpage`, `asc_land`), `templates/base/footer_content.tmpl`, `web_src/css/asc-ds/landing.css` | A Go test renders `GET /` signed out and asserts every non-sample text node of `Main.dc.html` is in the HTML (today 83%; target 100%); pixel diff of Main and Mobile-Landing at 1440 and 390, light and dark, under the harness threshold |
| WU-02 | Release pipeline and download wired to Jordan-Hall/asc releases | shell/settings | asc repo release workflow (signing key secret `ASC_MINISIGN_KEY`, variable `ASC_RELEASE_PUBKEY`), `modules/setting/asc.go`, `docker/allinone/.../asc-hub-entrypoint`, `services/ascdownloadsync`, `templates/home.tmpl:292`, `templates/download.tmpl` | `gh release view -R Jordan-Hall/asc` lists signed archives and `manifest.json`; a fresh all-in-one container with no download variables shows the landing download section and `/download` with those assets and passing signature checks; empty state text when none |
| WU-03 | Personal access tokens: expiry and ASC scopes | auth | `models/auth/access_token.go` + new migration, `access_token_scope.go`, `routers/web/user/setting/access_token.go`, `templates/user/settings/applications.tmpl`, `access_token_edit.tmpl`, `services/auth`, `routers/private/asc.go:385-472`, asc `crates/asc-serve` authn | Expired token gets 401 on API and on `POST /api/internal/asc/authz`; a token with Changesets and stacks = Read cannot write a change; scope matrix added to `routers/private/asc_parity_test.go`; board rows st94, st95 done |
| WU-04 | Device login mints a scoped, expiring token | auth | `routers/web/auth/asc_device.go:195`, `models/auth/asc_device_code.go`, `templates/user/auth/asc_device.tmpl`, locale `asc_device.grants` | Token made by the device flow lists only the scopes shown on the approval page and expires (board: short-lived); `asc auth token --scope repo:read` works against it; test that `AccessTokenScopeAll` is not used |
| WU-05 | On-demand per-repository CLI sign-in | auth | asc `crates/asc/src/cli/clone.rs`, `fetch.rs`, transport and `auth_device.rs`; hub locale `asc_auth.cli_hint`, `signin_inner.tmpl:36`, `home.tmpl:36,42`, `applications.tmpl:15`, `post-install.tmpl`, Onboarding and GetStarted copy | e2e: `asc clone <private repo url>` with no stored credential shows the code and link, hub approval completes it, the clone succeeds and the credential is kept per host and path; `asc clone <public url>` asks nothing; no-browser path prompts for a token; hub hint text states this model |
| WU-06 | Sign-in and sign-up board parity | auth | `routers/web/auth/auth.go`, `webauthn.go`, `templates/user/auth/*`, `web_src/js/features/asc-signup.js`, `asc-codeboxes.js`, `services/forms/user_form.go`, `models/auth` (terms acceptance) | Passkey login with a virtual authenticator; 6th wrong password in 5 minutes answers the wait message and a correct one is refused until it passes; strength meter and three rules update on input; registration needs the Terms box and stores it; 2FA page has Use a security key instead; key page has Try another way; Playwright for rows sh75, sh77, sh80, sh81 |
| WU-07 | Review threads, suggestions and version compare through asc | review | `services/ascsync` (new review sync), `routers/web/repo/pull_review.go`, `templates/repo/pulls/asc_versions.tmpl`, `templates/repo/diff/*`, call sites for `Review.ListThreads/AddComment/ResolveThread/RequestReview/CompareVersions/PrepareSuggestion/MarkSuggestionApplied/SuggestReviewers/HunkContext` | A comment made in the hub is returned by `asc review threads`; resolve in either place shows in the other; Apply as v5 creates a version, resets approvals and shows the Undo toast; ReviewVersions header shows unchanged files and carried approvals; hunk header names the enclosing function (also for `.tsrx`); rows rv67, rv69, rv65, rv95, rv113 |
| WU-08 | Stacks list and repository rails | repo | `routers/web/repo/pull.go`, `templates/repo/issue/list.tmpl`, `templates/repo/pulls/asc_stack.tmpl`, `templates/repo/stack_row.tmpl`, `home_cards.tmpl` | `/pulls` of an asc repository shows stack rows with status chip, rail with segments, filters with counts, Your turn card, New stack; same data as the Repo home rail; rows rp264-rp273 |
| WU-09 | Release notes, provenance, checksums via asc Releases | repo | new client for `ascv1.NewReleasesClient` in `services/` , `routers/web/repo/release.go`, `templates/repo/release/*.tmpl`, asc `asc release` | Releases page shows built-by run, checks, release key and asset SHA-256 from asc; new-release dialog writes notes from landed changes (`Releases.DraftNotes`); `asc release create v1.0.0` exists; rows rp201-rp211 |
| WU-10 | Dashboard and Inbox in-place actions and live updates | shell/settings | `templates/user/dashboard/asc_home.tmpl`, `asc_inbox.tmpl`, `routers/web/user/asc_inbox*.go`, `asc_dashboard*.go`, `web_src/js/features/asc-keys.js`, `toast.js` | Approve, reply, answer, add to queue and mark done work from the row; Done keeps 30 days; unread dot; bulk bar; snooze menu; undo toast; counts update without reload (SSE or poll); keys j k x e s a r p; rows sh226-sh261 |
| WU-11 | Shell: agents pill, bell popover, palette, footer, theme persistence | shell/settings | `templates/base/head_navbar.tmpl`, `footer_content.tmpl`, `web_src/js/features/ds-shell.js`, `asc-keys.js`, `routers/web/asc_palette.go`, `models/user` (settings), asc cheap run count | Pill shows running agents (one call, no 8-repo fan-out); bell popover with Mark all read; palette groups Changes/Code/People and verbs (undo, restack, merge); footer groups and status line match Main; theme and density saved to the account; rows sh33-sh49 |
| WU-12 | Statistics on the repository home, History, Explore | repo | `routers/web/repo/view_home_cards.go`, `asc_about.tmpl`, `commits.tmpl`, `explore/repo_list.tmpl`, `explore/rail.tmpl` using `Insights.GetStats` | Who writes the code, Activity, Landing rate and trending cards show real numbers from asc; rows rp46, rp49, rp138, sh145-sh152 |
| WU-13 | Repository settings: rules, access, webhooks, secrets scopes, AI caps, storage tests | repo | `templates/repo/settings/*`, `routers/web/repo/setting/*`, `services/ascbyo`, `services/asctrust/*`, `modules/webhook`, asc `Workflows.SetSecret` scopes, `Storage.VerifyStorageTarget`/usage | One Access list with agents; rules side panel; webhook kinds Generic/Chat/ASC agent and ASC events with https-only URL; secret scope chips; per-feature AI caps and cap-reached message; storage test with four steps, usage and migration pause; rows rp280-rp391 |
| WU-14 | New repository flow | repo | `templates/repo/create*.tmpl`, `routers/web/repo/repo.go`, `services/repository`, `web_src/js/features/repo-migration.js` | Visibility Private/Internal/Public cards, working mode, AGENTS.md and policy switches, name check at 300 ms, quota line, import progress with Run in background; rows rp66-rp77 |
| WU-15 | Code, Changeset, History, Refs, Compare, Search leftovers | repo | `templates/repo/view_file.tmpl`, `blame.tmpl`, `commit_page.tmpl`, `commits.tmpl`, `branch/list.tmpl`, `asc_compare.tmpl`, `asc_search.tmpl` | Shortcuts b y l e t [; per-file badges; Not found at this version; intent and where to look in Changeset; files by meaning; history graph + list as one page; Did you mean for unknown ref; search facets, reasons, symbol panel, pagination; rows rp91-rp192 |
| WU-16 | Checks, ChangeChecks, agent run controls | review | `templates/repo/asc_checks.tmpl`, `pulls/asc_checks.tmpl`, `asc_agents_run.tmpl`, `routers/web/repo/asc_checks.go`, `asc_agents_ctl.go`, `Ci.JobLog`, `Ci.CompareWithMain` | Inline log excerpt per failure group, compared-with-main block, Ask coder-2 to fix, version history list; plan block and approvals log; Pause/Stop confirm; rows rv126-rv151, rv180-rv194, rv215-rv217 |
| WU-17 | Account settings: notifications, security, usage, orgs, repos | shell/settings | `templates/user/settings/*`, `routers/web/user/setting/*`, `models/auth/twofactor.go` (recovery codes), sessions client in `services/asctrust/account.go` | Notifications page with event by channel matrix; recovery codes list and download; signed-in devices with revoke; re-auth before password change; pending org invitations; transfer repository; rows st76-st116 |
| WU-18 | Organization pages | shell/settings | `templates/org/*`, `routers/web/org/*`, `services/asctrust/orgsettings.go` | Create permissions, base permission, transfer ownership; people search, roles, pending invitations, agents section; teams detail panel; new org fields; org profile cards; rows st132-st152, sh286-sh292 |
| WU-19 | Admin pages | shell/settings | `templates/admin/*`, `routers/web/admin/*`, `services/asctrust` | Dashboard tiles and recent audit; health gauges and checks; users bulk actions and tabs; runners drain and agent runners; runtime switches; flush confirm and streaming Run now; moderation reasons; auth Test sign-in; rows st154-st198 |
| WU-20 | Profiles, Explore people and orgs | shell/settings | `templates/user/profile.tmpl`, `overview/header.tmpl`, `explore/users.tmpl`, `org/home.tmpl`, `models/user` (presence, pins) | Pinned repositories, presence pill, agent cells in contributions, Agents Priya runs, Knows best, request a review; People and agents filter, role chips, agent rows, sparkline; rows sh145-sh185, sh268-sh279 |
| WU-21 | Issues, Projects, Packages linked to stacks | repo | `templates/repo/issue/*`, `projects/*`, `package/*`, `models/issues/pull_asc_stack.go` | Fixed-by-stack chip, linking closes issue on landing with undo, linked stacks rail, project cards follow stack state, package provenance; rows st204-st225 |
| WU-22 | Install and Get started | shell/settings | `routers/install/install.go`, `templates/install.tmpl`, `post-install.tmpl`, `asc/get_started.tmpl`, `routers/web/asc_home.go` | Test connection with Connected to PostgreSQL text, setup progress page, step 2 ticks without reload, inline import, asc doctor card, sample project; rows sh98-sh119 |
| WU-23 | Docs and Errors | shell/settings | `services/ascdocs`, `templates/asc/docs*.tmpl`, `templates/status/403.tmpl`, `asc-offline.js` | Docs search with groups, article layout, helpful vote; 403 Request access and Switch account; offline draft saving in editors; rows sh192-sh218 |
| WU-24 | Mobile cross-cutting behaviour | shell/settings | `web_src/css/asc-ds/touch.css`, new `web_src/js/features/asc-sheet.js`, `asc-swipe.js`, templates of the Mobile-* rows | Bottom sheets (filters, release, undo preview, confirm), pull to refresh, swipe done/snooze/acknowledge, sticky action bars at 390 px; Playwright touch tests for Mobile-Review, Mobile-Inbox, Mobile-Checks, Mobile-Ops |
| WU-25 | Policy form, rules and merge-queue settings in asc | agents-ops | `templates/repo/settings/asc_policy.tmpl`, `asc_gate.tmpl`, asc `crates/asc-core/src/policy.rs`, `Policy.Propose` | Structured form (approvals stepper, check chips, switches), Changing policy is a change panel with reviewers, batch and post-merge checks keys, stack behaviour switches; rows rp314-rp337 |
| WU-26 | TSRX completeness | repo | `.gitattributes`, `modules/highlight/tsrx`, `services/gitdiff` (hunk header), `templates/repo/diff`, docs | `.tsrx` shows as TSRX in language bar, diff, blame, search and markdown fence; hunk header shows the enclosing component; asc search finds a `.tsrx` symbol by meaning |
| WU-27 | Make the gap files true | all | `docs/asc/feature-gaps/*.md`, `UI-COVERAGE.md`, `FEATURE-ROADMAP.md` | One test per row (or a named reason): CI fails if a row says done and its named test is missing; counts in the roadmap generated from the rows; Where cells rewritten as evidence |
| WU-28 | Pixel and accessibility verification of all 191 boards | all | `tests/pixel` (branch `pixel/harness`), `.github/workflows/asc-pixel.yml`, `tests/e2e` | Report for 191 boards x light/dark x (1440, 768, 1024, 390) with diff scores, axe critical/serious = 0, console errors = 0, committed under `docs/asc/PIXEL-REPORT.md` |
| WU-29 | Re-run live journeys against a real hub and asc | all | `docs/asc/QA-REPORT.md`, `tests/asc-e2e`, `.github/workflows/asc-e2e.yml` | Journeys 1-11 of QA-REPORT pass on current trees (defects O1-O15 closed or re-filed) with sign up, import, push, stack submit, review, queue, agent run, settings, admin, download |
| WU-30 | Forwarded-claim contract per service | review | `docs/asc/ARCHITECTURE.md:157`, asc `crates/asc-serve/src/authz/claim.rs`, `routers/private/asc_parity_test.go` | Doc states which RPCs accept read/write/admin and which subjects; a test table fails when a service ignores a claim; matrix extended to cross-repository lists |

## 8. Top 20 gaps

1. Landing page copy and sections differ from the Main board (pill, Why ASC block, strip heading, CTA, footer groups, extra sections) - WU-01.
2. Landing download section is hidden on a fresh hub: no signed release of Jordan-Hall/asc exists and `DOWNLOAD_SOURCE_URL` defaults to none - WU-02.
3. DONE on feat/wu03-tokens: personal access tokens expire and carry the ASC scopes (WU-03).
4. DONE on feat/wu03-tokens: device login mints a scoped token that expires after 90 days (WU-04).
5. The CLI hint and `asc clone` use the up-front sign-in model, not on-demand per-repository auth - WU-05.
6. No passkey sign-in, no login lockout, no password strength meter, no Terms checkbox - WU-06.
7. Review threads reach asc now (feat/wu07-review-rpcs): comments, replies, resolves and review requests are mirrored, asc-side threads are listed, version compare, suggested reviewers, hunk context, brief editing and the draft from the diff are wired. Open: Apply as vN (PrepareSuggestion, MarkSuggestionApplied) - the hub cannot make an asc version - WU-07.
8. Stacks board: built by the repo pages work (stack_board.tmpl: rail, filters with counts, sort, Your turn card, New stack, states); only the row hover slide-in and chip pulse remain - WU-08.
9. Releases: the hub now has a Releases client (publish on the hub makes the asc record with asset checksums; the list shows provenance and checksums; GET /releases/asc_draft gives the proposed tag and notes). Open: the release dialog that uses the draft endpoint, build run only for releases made through asc - WU-09.
10. Dashboard and Inbox rows cannot act in place; no unread, done, bulk, live updates - WU-10.
11. Header agents pill, notification popover, palette results and commands, footer groups and status line are missing - WU-11.
12. Repository home, History, Explore statistic cards not wired to Insights.GetStats - WU-12.
13. Repository settings: combined Access list, rules side panel, webhook kinds and events, secret scopes, per-feature AI caps, storage tests - WU-13.
14. New repository flow lacks visibility cards, working mode, AGENTS.md and policy switches, live name check, import progress - WU-14.
15. Checks and agent run views lack inline logs, compare with main, Ask agent to fix, plan and approvals log - WU-16.
16. Account settings: no Notifications page and matrix, no recovery codes list, no device list, no re-authentication - WU-17.
17. Mobile twins lack sheets, pull to refresh, swipe and sticky action bars; only responsive CSS exists - WU-24.
18. Admin boards lack bulk actions, tabs, gauges, drain, runtime switches, audit card - WU-19.
19. The gap files and UI-COVERAGE overstate completion; 89 rows are partial, and `done` rows have stale Where text; no pixel report exists - WU-27, WU-28.
20. TSRX: no enclosing-component hunk header, no `.gitattributes` entry, no end-to-end test of diff and fence - WU-26.

## 9. Pixel run of 2026-10-30 (branch fix/pixel-1, harness fd106da904 plus asc mode)

Seven boards and their Mobile twins, light and dark, against a hub built from this tree (the first report was made by a hub built from an older tree). Mismatch is the share of different pixels.

| Board | Before | After | What decides the rest |
|---|---|---|---|
| AdminUser | 0.48 (mobile 0.56) | 0.31 (mobile 0.42) | The page of a person answered 500 in the middle (organizations list took the wrong type): fixed. Open: tabs Overview/Emails/Keys/Sessions/Activity, Usage card, Delete card, Reset 2FA and Suspend as buttons (rows st169-st172) |
| AdminMonitor | 0.69 | 0.68 | The board shows invented rows (4 queues, 3 tasks); the hub shows its 14 real queues and 35 real tasks. Design differences left: tabs (the board has none), Open versus Flush, the Active and Runs columns |
| AdminConfig | 0.79 | 0.79 | The board is a curated page (runtime switches, Server, Storage, Mail with copy buttons); the hub prints the whole server configuration (7000 px). Needs the curated sections with the full list behind a disclosure, and the runtime switches for registration, email confirmation, agents and the default budget (rows st182-st184) |
| Changeset, Review | 0.57, 0.53 | same | The boards are the asc experience (intent, stack position, review rail, versions); the harness hub holds git repositories and has no asc server, so the page that is photographed is the git page. Needs a harness mode with asc serve and a stack seeded through the asc command line |
| Releases | 0.48 | 0.48 | The seed has 2 releases without assets or changes, the board 14 with assets and change lists. Design differences left: sort control, Release health card, per-release changes list and Draft vX in the Next card |
| OrgPeople | 0.56 | 0.56 | The board has roles, 2FA chips, changesets, an agents section, pending invitations and an invite dialog; the hub lists members with Make visible and Remove (rows st140-st145) |

What changed in the run itself: the harness hub gets `[asc] ENABLED = true` with `PIXEL_ASC=1` (the header shows Agents and Download as on a real hub), and its root URL is the loopback address the browser uses (the admin pages no longer carry the wrong-address banner that pushed every admin page down by 70 px).

## 10. Harness with asc serve (branch pixel/asc-mode)

`PIXEL_ASC=serve` starts `asc serve` next to the throw-away hub and seeds the cast with the real `asc` client: four stacks by Priya, Diego and Hannah with their changes, review comments and approvals, five releases with files, five workflow definitions (one not valid, one switched off) and a workflow run, over a main with six landed commits. The 54 boards that needed asc now answer (before: 404 or 503 on every route under `asc/`). Light, desktop unless marked, against asc built from master:

| Board | Before | After | What decides the rest |
|---|---|---|---|
| Changeset (mobile) | 0.63 (0.70) | 0.22 (0.17) | The seed commit is now the 3-file change the board shows; the live page lacks the intent card, stack navigation, the review rail and the "where it is now" card |
| Releases | 0.62 | 0.11 | Sort control, release health card, per-release changes list |
| Review | 0.53 | 0.28 | Review rail and versions on the right; the board lists 4 files, the seed change has 1 |
| Stacks | 0.31 | 0.31 | Row layout (board 9 stacks, harness 4) |
| Workflows, WorkflowDef, WorkflowRun | 404 | 0.16, 0.45, 0.26 | Run history and step chain need runs of agent steps |

Left out of the seed, with the reason: agent runs (a key is used only after its provider answered a test call; the harness has no provider, so Agents, AgentRun and their mobile twins show the empty state or 404), check runs (no CI in the harness), the merge queue contents (nothing has landed through it).

Finding: a release made with `asc release create` is a record in asc only. The hub's Releases page lists the hub's own release rows, which are made from tags it knows, so a release made from the terminal does not show on the hub. The seed therefore publishes releases through the hub (draft, files, publish), which the hub mirrors to asc with sizes and SHA-256. Needs a decision: list asc's releases on the hub page, or have asc tell the hub.

## 11. AdminConfig (branch feat/admin-config)

The page is the board's: Runtime settings (Allow registration, Require email confirmation, Allow agents on this server, Default agent budget), then Server, Storage and Mail as read-only rows with Copy value, and the full dump behind "Show every setting" (which also holds the avatar and editor-app runtime settings). Registration and confirmation are stored in the hub's system settings and applied to `setting.Service` at start and on save (`setting.ApplyRuntimeService`); confirmation is refused when no mailer is set up. The agent switch and the default budget are saved to asc serve with the same call as /admin/asc/agents. Measured: AdminConfig 0.79 to 0.07 (dark 0.07), Mobile-AdminConfig 0.08. What is left is data (the domain and version chip) and the 44 px touch size of the copy button on a phone. The default agent budget is a dollar amount per run (asc's unit); the board shows "20 min", which asc has no setting for. Admin page titles are 26 px (25 px on a phone) as on the boards.

## 12. Audit paging, Download, admin titles (branch feat/download-board)

Audit (repo and server): a page of the log is 10 events by default (10, 25, 50, 100, 200 to choose); the pager shows the number of the page, Older, and First after the first page. The log is cursor-paged by asc, so the pager cannot offer numbered pages or Newer. AdminAudit 0.84 to 0.50, Audit 0.82 to 0.61 (the board shows 5 sample rows; the rest is the board's search/date/action bar against the hub's filter form, and the Verify button that the board does not have). Download: the harness hub offers a release (manifest and files) and the Download boards use a macOS browser; Download 0.68 to 0.46, Mobile-Download 0.80 to 0.60. What is left is layout: the board stacks the three systems with package-manager rows and a direct-downloads table and has Verify, Editor and System requirements cards; the hub shows one system at a time behind tabs. Admin page titles are 26 px: AdminConfig 0.06, AdminUsers 0.07, AdminOrgs 0.05, AdminPackages 0.04, AdminAuth 0.04, AdminNotices 0.04, AdminRunners 0.06, AdminHooks 0.11. Still high: AdminMonitor 0.68 (invented rows), AdminBackups 0.60, AdminUser 0.31 (rows st169-st172), AdminRepos 0.23, AdminDashboard 0.25.
