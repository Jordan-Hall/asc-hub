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

After this audit, as synced from the gap rows on 2026-11-09 (every element row whose gap row is done now says done and cites the gap row's evidence; the board notes marked unverified are not counted):

| Area | Boards | Rows | Done | Partial | Missing | Done % |
|---|---|---|---|---|---|---|
| shell/settings | 47 | 317 | 317 | 0 | 0 | 100% |
| repo | 24 | 274 | 274 | 0 | 0 | 100% |
| review | 5 | 88 | 88 | 0 | 0 | 100% |
| agents-ops | 12 | 126 | 126 | 0 | 0 | 100% |
| auth | 6 | 40 | 40 | 0 | 0 | 100% |
| **all** | 94 | 845 | 845 | 0 | 0 | 100% |

Missing rows by kind of work at the audit: UI 140, hub 130, asc 14, hub+asc 79; all are done now. Rows that need asc work are fewer than the logs say: asc `origin/master` has 272 RPCs and the hub's generated stubs have the same 272; 52 of them are newer than the local asc checkout of 2026-10-28 (Insights, Releases, Backups, Sessions, Usage, OrgSettings, DataExport, AiKeyShares, SecretScan, Review brief and suggestions, Agents pause and checkpoints) exist on both sides.

Copy match across all 95 desktop boards (copy-like strings found in the hub): NewChange 35%, ReviewVersions 49%, Download 50%, Dependencies 51%, Drafts 51%, Inbox 53%, RepoSettingsStorage 53%, GetStarted 54%, Projects 54%, Checks 55%, ChangeChecks 56%, Refs 56% are the lowest; no board is at 100%, the highest is About at 90%.

## 2. Requirements you listed, checked one by one

| Check | Verdict | Evidence | Gap |
|---|---|---|---|
| Tokens are the platform's personal access tokens | done (2026-10-30, branch feat/wu03-tokens) | One token system: `models/auth/access_token.go`. New `ExpiresUnix` (0 = never); `GetAccessTokenBySHA` refuses an expired token for API, git, packages and asc alike; the form offers 7/30/90/365 days or never (default 30), the list shows Expires and an Expired chip; regenerating gives the same lifetime again. New scopes `read/write:changes` and `read/write:runs` (`access_token_scope.go`, migration `v17z_asc_token_expiry.go` gives existing repository tokens the matching changes and runs scopes). The asc forwarder holds a personal token to the scope of the part of asc it calls (`services/ascforward/scope.go`: Changes, Stacks, Review, MergeQueue, Inbox, Projects need changes; Agents, AgentTokens need runs; read-only method names need read, any other name needs write). `routers/private/asc.go` accepts an `area` for asc's own authz calls. Device login (`asc_device.go`) now takes the scope the terminal asks for (`scope` parameter, only repository, changes, runs, read:organization, read:user; default write repository/changes/runs + read org/user), shows it on the approval page and mints a token that expires after 90 days; it never mints `all`. | Open: asc does not send `area` yet (`crates/asc-serve/src/forgejo.rs`), so only calls through the hub's forwarder are held to changes/runs (that is every public client). Tokens made through the REST API (`POST /users/{u}/tokens`) take no expiry option yet. |
| Signed forwarded claim to asc | done | services/ascidentity/ascidentity.go signs asc-forwarded-v1 with HMAC-SHA256 (user, role, subject, ts, 60 s skew); services/asctrust/usercall.go:64,129 and services/ascagentui decide with the hub's permission model first and attach the claim; services/ascforward/forward.go strips client-supplied x-asc-forwarded-* and x-asc-hub-*. asc verifies in crates/asc-rpc/src/forwarded.rs and uses it in changes/rpc.rs:812, cloud/rpc.rs:824, review/mod.rs:259, review/acting.rs:64. | ARCHITECTURE.md:157 still says asc honours only the admin role; asc master has read and write roles and the org/user/instance subjects. The statement is stale; add a per-service test list of which RPCs accept which claim. |
| Row-level visibility equals the platform | done | routers/private/asc_parity_test.go compares asc's authz answer, the hub's page decisions and git storage for the same grants (repository read/write/admin); tests/integration/asc_visibility_matrix_test.go is the one matrix for the cross-repository lists: explore, repository and user search API, profile repositories and activity heatmap, organization page, palette, dashboard, inbox, notifications and admin, plus the private and limited organization and the private profile, for a signed-out viewer and a signed-in one without access, with the owner as the control. | - |
| Landing download section wired to real GitHub releases of Jordan-Hall/asc | partial | The section is real: templates/home.tmpl:292-320 renders AscDownload from the signed manifest (routers/web/asc_download.go:67, modules/ascdownload); services/ascdownloadsync fetches and verifies a GitHub release API address (ASC_HUB_DOWNLOAD_SOURCE_URL, docker/allinone/usr/local/bin/asc-hub-entrypoint:217-236). | Nothing points it at Jordan-Hall/asc: [asc] DOWNLOAD_SOURCE_URL defaults to none (modules/setting/asc.go:56) and needs DOWNLOAD_PUBLIC_KEY; gh api repos/Jordan-Hall/asc/releases/latest answers 404 (no release is published, signing key not set). So the landing download section and the Download board are hidden on a fresh hub (home.tmpl:14 and 292 test .AscDownload). |
| Landing content word for word against the Main board | done (2026-11-09) | The landing was rebuilt from the boards (commits 03b43a6654 and 437e5acf31): `templates/asc/land_desktop.tmpl` (Main) and `land_mobile.tmpl` (Mobile-Landing) carry the board copy (`asc_bl.*`: the pill naming the offered release, the Works with strip heading, the Why ASC block with its three problem cards), and `templates/base/footer_content.tmpl` the Product, Developers, Company and Legal groups with the status line; TestAscLandingCarriesTheBoardCopy and the pill tests in routers/web/asc_about_test.go. | - |
| Sign-in and sign-up per the Onboarding board | done (2026-11-09) | Done: split layout (user/auth/shell_start.tmpl), password Show toggle, remember me, forgot and reset pages, device approval (user/auth/asc_device.tmpl, routers/web/auth/asc_device.go, RFC 8628 routes at routers/web/web.go:744), live Your handle hint with availability check (web_src/js/features/asc-signup.js, routers/web/auth/signup_name.go, rate limited 20 per minute), optional full name, six-box code input (asc-codeboxes.js), sign-up strength meter with the three live rules (password_meter.tmpl, asc-password.js, breach rule through GET /-/password/range/{prefix}), Sign in with a passkey (asc_passkey.go, asc-passkey.js), invite-only banner (signup_inner.tmpl:5). Use a security key instead on the two-factor page (twofa.tmpl `twofa-use-key`, shown when the account has a key); Try another way on the key page (webauthn.tmpl `webauthn-other`: authenticator code, recovery code, start again); an OpenID Connect source is labelled Single sign-on (its name) (oauth_container.tmpl, `asc_auth.signin_sso`; TestSignInSingleSignOnLabel); the states frame copy is `asc_auth.signin_title` and `signin_lead` (Sign in to ASC Hub, Pick up your stacks, reviews and agent runs where you left them). | - |
| CLI hint reflects on-demand per-repo auth | done (2026-11-09) | asc signs in per repository on demand: `crates/asc/src/remote/demand.rs` (a refused clone, fetch, pull or push tries the credential helpers, then the device sign-in at a terminal, or a pasted token with `ASC_NO_BROWSER`), called from `remote/server.rs` `ask` and `reauth`. The hub shows no up-front step: the sign-in page hint is `asc clone <hub>/owner/repo` with `asc_auth.cli_text`; the clone sheet shows only `asc clone` and `repo.clone_signin_help` (no sign-in step, a private repository asks for a device code); the empty-repository page starts at `asc init` / `asc remote add`; `TestAscCloneNeedsNoSignInStep`. | The tokens page and the install and download pages keep `asc auth login` as the optional way to store a token ahead (for CI, `--token-stdin`). |
| TSRX first-class | done (2026-11-09) | Hub: .tsrx detected as TSRX (modules/analyze/code_language.go:13-39, so it counts in the language bar without a linguist entry), Chroma lexer registered (modules/highlight/highlight.go:20, modules/highlight/tsrx/tsrx.go, golden tests) and imported by the Markdown renderer for ```tsrx fences (modules/markup/markdown/markdown.go; TestRenderTSRXFence), diffs highlighted by it (TestDiffOfTSRXIsHighlighted), CodeMirror language (web_src/js/features/codemirror-lang.ts:153, codemirror-tsrx.ts), hunk header with the chain of definitions from Review.HunkContext also for .tsrx (review.md row Hunk header). asc: crates/asc-codeintel/src/tsrx.rs, tags/tsrx.rs, chunk.rs:471 (template regions blanked), asc-serve/src/deps/scan.rs:229 (imports); language server use in asc docs/TSRX.md. | - |

Other findings that cross boards:

- Resolved since the audit (re-checked 2026-11-09): the hub calls every Review RPC the audit listed (`ListThreads`, `AddComment`, `ResolveThread`, `RequestReview`, `SetBrief`, `CompareVersions`, `PrepareSuggestion`, `MarkSuggestionApplied`, `SuggestReviewers`, `DraftContext`, `HunkContext`), the asc `Releases` service (services/asctrust/releases.go), `Ci.CompareWithMain` (services/asctrust/cistats.go) and `DataExport.GetExport` (services/asctrust/account.go).
- Not called on purpose, because the hub owns the data: `Ci.JobLog` (checks run on the hub, which serves their logs and reports each run to asc with `Ci.ReportRun`), `MergeQueue.SetPaused` (the queue is the hub's, services/automerge/asc_stack_queue.go `AscQueueSetPaused`), `DepsService.SetConfig` (the settings are a file in the repository; the page shows its source) and `Agents.GetPerson` (asc serves it and asks the hub, routers/private/asc_agent_profile.go).
- `UI-COVERAGE.md` claims 95 of 95 boards are design system and 0 partial. That measures class names. By behaviour and copy no board is complete (section 3).
- `FEATURE-ROADMAP.md` counts table shows zeros and must be replaced by the table in section 1 (it is outside the files I may edit here).
- `QA-REPORT.md` lists open defects O1 to O15 from an older tree; re-run the journeys (work unit WU-30).

## 3. Per-area counts and boards

- **shell/settings**: 47 boards, 317 rows, done 317, partial 0, missing 0. Boards: About, AdminAudit, AdminBackups, AdminConfig, AdminDashboard, AdminHealth, AdminHooks, AdminModeration, AdminMonitor, AdminNotices, AdminOrgs, AdminPackages, AdminRepos, AdminRunners, AdminUser, AdminUsers, Dashboard, Docs, Download, Drafts, Errors, Explore, ExploreOrgs, ExploreUsers, GetStarted, Inbox, Install, Main, NewOrg, OrgPeople, OrgProfile, OrgSettings, OrgTeams, Packages, Profile, Projects, Shell, UserSettingsAI, UserSettingsAccount, UserSettingsAppearance, UserSettingsNotifications, UserSettingsOrgs, UserSettingsPrivacy, UserSettingsProfile, UserSettingsRepos, UserSettingsStorage, UserSettingsUsage.
- **repo**: 24 boards, 274 rows, done 274, partial 0, missing 0. Boards: Changeset, Code, Compare, Dependencies, GitNote, History, Insights, InsightsPeople, IssueView, Issues, NewRepo, Refs, Releases, Repo, RepoSettingsAI, RepoSettingsAccess, RepoSettingsGeneral, RepoSettingsPolicy, RepoSettingsRules, RepoSettingsSecrets, RepoSettingsStorage, RepoSettingsWebhooks, Search, Stacks.
- **review**: 5 boards, 88 rows, done 88, partial 0, missing 0. Boards: ChangeChecks, Checks, NewChange, Review, ReviewVersions.
- **agents-ops**: 12 boards, 126 rows, done 126, partial 0, missing 0. Boards: AgentAccess, AgentNew, AgentRun, Agents, Audit, Ops, Policy, Queue, Security, WorkflowDef, WorkflowRun, Workflows.
- **auth**: 6 boards, 40 rows, done 40, partial 0, missing 0. Boards: AdminAuth, Onboarding, UserSettingsApps, UserSettingsKeys, UserSettingsSecurity, UserSettingsTokens.

Cross-cutting mobile rows (not tied to one board) are counted in the area they belong to and listed in section 6.

## 4. Per-board tables

Each Mobile-* twin is the same page at 390 px; its rows are prefixed Mobile. Boards `Mobile-Menu`, `Mobile-RepoSettingsMenu` and `Mobile-UserSettingsMenu` are mobile-only index pages. Board `components` (A1, design system) has no page; its tokens live in `web_src/css/asc-ds/tokens.css`.

### Area: shell/settings

#### About (ASC Hub about)

Area shell/settings. Rows 3: done 3, partial 0, missing 0. Copy found in the hub: 90% of 88 copy strings (mobile twin 79%). Template(s): -. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Contact us; Stacks and changes; CLI reference; MCP server; Acceptable use; Subprocessors.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Hero, story with before and after table, principles, open source chips, data ownership, contact | done | templates/asc/about.tmpl:1 | - |
| Header About and footer link go to /-/about | done | services/ascdocs/content/WHY.md (file) | - |
| Source code, Security policy and Contact targets | done | Source and security policy link the ASC Hub repository; `[asc] CONTACT_EMAIL`, `SECURITY_EMAIL` and `POLICIES_URL` (`modules/setting/asc.go`, OPERATIONS.md) set the contact, report and policies targets, with the ... | - |
| Board note: Calls to action: Download ASC (/download), Read the docs | done | templates/asc/about.tmpl:10-11; test routers/web/asc_about_board_test.go TestAscAboutBoardNotes | - |
| Board note: Principle cards are not links; their content is the content | done | templates/asc/about.tmpl:46-49 (article cards, no links); test TestAscAboutBoardNotes | - |
| Board note: Hero, story with before and after table, four principles, open source and data ownership,... | done | templates/asc/about.tmpl (hero, why with before and after table, principles, open source chips, data ownership); test TestAscAboutBoardNotes | - |
| Board note: Light and dark; hero stays navy | done | web_src/css/asc-ds/landing.css:9 hero on var(--navy), one --navy in tokens.css:74 for both themes; test TestAscAboutBoardNotes | - |

#### AdminAudit (ASC Hub admin: audit log)

Area shell/settings. Rows 2: done 2, partial 0, missing 0. Copy found in the hub: 72% of 69 copy strings (mobile twin 65%). Template(s): asc_audit.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Search actor, action or target; All actions; Today 09:41; Runner registered; Runner registered.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Hash-chain banner, filters, details, export, verify | done | routers/web/admin/asc_audit.go (file) | - |
| Signed export file for the range | done | asc: Audit.Export ends with an SSHSIG over the file in the namespace asc-audit-export when `[signing] server_key_file` is set (crates/asc-serve/src/audit/rpc.rs, proto/asc/v1/audit.proto fields 6 and 7); hub: services/asctrust/audit.go checks the signature against the file and routers/web/shared/trustweb/audit.go serves a zip of the file, its .sig, allowed_signers and VERIFY.txt with the ssh-keygen -Y verify command; an unsigned export is the plain file; tests an_export_ends_with_the_server_signature_over_the_file, TestCopyExportKeepsTheSignatureAndItVerifies, TestWriteSignedExport, TestAscAuditExportIsSignedWhenTheServerSignsIt | - |
| Board note: Rows expand in place to show the full record, request id and the diff | done | templates/admin/asc_audit.tmpl (details asc-audit-record: event, id, time, request id, outcome, address, client, hash, diff of old and new); services/asctrust/audit.go AuditChange; tests TestAscAdminAuditBoardNotes, TestAuditPageOfBuildsTheRecordAndTheDiff | - |
| Board note: Export downloads a signed file for the range | done | the signed export row above; TestAscAuditExportIsSignedWhenTheServerSignsIt | - |
| Board note: Default, filtered, no results, chain verification failed (error banner), export in progre... | done | default and filtered (filters open), no results (asc-audit-empty), chain failed (ds-banner-err role alert), export in progress (Export button is-loading and a polite status); tests TestAscAdminAuditBoardNotes, asc-audit.test.js showExportBusy | - |

#### AdminBackups (ASC Hub admin: backups)

Area shell/settings. Rows 2: done 2, partial 0, missing 0. Copy found in the hub: 64% of 75 copy strings (mobile twin 54%). Template(s): -. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Schedule, check and restore copies of this server.; Backups need attention.; The last backup failed because the target timed out. Your newest good copy is 2...; Every day at 02:00; UTC. Kept 30 days, then weekly for a year..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Page with how to run backups (read-only text) | done | templates/admin/backups.tmpl (file) | - |
| Schedule, target, next backup, Back up now, history with Retry/Restore/Verify, in-progress met... | done | routers/web/admin/asc_backups.go:24 | - |
| Board note: Restore needs RESTORE typed and puts the server read only | done | TestAscBackupsRestore, TestAscBackupsBoardNotes/restore_confirmation; asc-serve backups::tests staging sets read-only | - |
| Board note: Verify restores into a scratch area and checks the result | done | asc-serve backup::restore_test; tests verify_restores_into_a_scratch_area_and_checks_the_result, the_restore_test_opens_a_scratch_copy_and_checks_it | - |
| Board note: Retention is shown in plain words | done | templates/admin/asc_backups_board.tmpl bk_kept; TestAscBackupsBoardNotes/healthy | - |
| Board note: Healthy, failed (banner + Retry), running (progress), restore confirmation, never run (se... | done | TestAscBackupsBoardNotes (healthy, failed, running, never run, restore confirmation) | - |

#### AdminConfig (ASC Hub admin: configuration)

Area shell/settings. Rows 4: done 4, partial 0, missing 0. Copy found in the hub: 58% of 69 copy strings (mobile twin 40%). Template(s): config.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Runtime settings you can change, and server settings you can read.; Change without a restart. Each change is audited.; People can create accounts themselves.; Accounts stay inactive until the address is confirmed.; Allow agents on this server.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Server settings read only (domain, protocol, SSH, storage, mail) | done | templates/admin/config_server.tmpl (file) | - |
| Runtime switches: Allow registration, Require email confirmation (runtime set has only avatar... | done | templates/admin/config_runtime.tmpl | - |
| Allow agents on this server; Default agent budget | done | routers/web/admin/asc_config.go | - |
| Restart-required banner, invalid value error, file and line shown, Copy key/value | done | modules/setting/asc_config_file.go, templates/admin/config.tmpl, web_src/js/features/admin/config.js | - |
| Board note: Runtime settings save on change; server settings are read only and show the file and line | done | tests/integration/admin_config_test.go TestAdminConfigFileAndRestart, TestAdminConfigRuntimeSwitches | - |
| Board note: Copy copies the key and value | done | templates/admin/config_curated.tmpl data-clipboard-text | - |
| Board note: Default, unsaved change, restart required banner, invalid value error | done | web_src/js/features/admin/config.test.js, TestPlaceConfigFile | - |

#### AdminDashboard (ASC Hub admin: dashboard)

Area shell/settings. Rows 7: done 7, partial 0, missing 0. Copy found in the hub: 70% of 79 copy strings (mobile twin 63%). Template(s): dashboard.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; +38 this week; Changesets today; Agent runs today; Last backup is 26 hours old; Nightly backup missed at 02:00. The target was unreachable..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Stat tiles linking to pages (people, repos, orgs, issues) | done | templates/admin/dashboard.tmpl (file) | - |
| Tiles "Changesets today (31% by agents)", "Agent runs today (96.4% finished)", "+38 this week" | done | overview.go todaysActivity, newPeopleSince; asc Usage.GetActivity; TestTheDashboardShowsTodaysActivity | - |
| Needs attention: runners offline, reports, notices | done | see where-cell | - |
| Needs attention: backup overdue with "Back up now" | done | routers/web/admin/overview.go backupAttention, TestTheDashboardOffersABackupWhenBackupsAreOverdue | - |
| System card rows: object storage usage and mail queue age (database, queues, runners exist) | done | overview.go storageRow, mailRow; TestTheSystemCardShowsObjectStorage, TestTheSystemCardShowsTheMailQueue | - |
| Recent audit entries card | done | routers/web/admin/asc_audit_recent.go; TestDashboardShowsTheNewestAuditEntries | - |
| Numbers refresh every 30 s without layout shift | done | see where-cell | - |

#### AdminHealth (ASC Hub admin: health)

Area shell/settings. Rows 4: done 4, partial 0, missing 0. Copy found in the hub: 75% of 64 copy strings (mobile twin 69%). Template(s): self_check.tmpl, system_status.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Primary reachable, 4 ms. Migrations are up to date.; Disk usage; Data volume is 78% full. At the current rate it fills in about 41 days.; Outgoing mail; Runner capacity.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Self check list with passed/warn/fail chips, Copy report, Run again | done | templates/admin/self_check.tmpl (file) | - |
| Checks for disk fill rate, object storage probe, signing key validity, outgoing mail queue, ti... | done | asc_health_more.go; asc Maintenance.CheckHealth; TestTheAscChecksAskAscServeToProbeItself, TestTheDiskCheckSaysHowSoonTheVolumeFills | - |
| Gauges CPU, Memory, Disk, Object storage with percent text (hub shows uptime, memory, goroutin... | done | asc_gauges.go (CPU, Disk), self_check.tmpl; TestLoadPercent, TestDiskPercent | - |
| Running state (steps appear one by one), could-not-run state | done | self_check.tmpl data-health-steps, SelfCheckStep; TestAscHealthSteps | - |
| Board note: Run again re-runs every check and keeps the old result until it finishes | done | health.js markPending; health.test.js 'a row keeps its old result, its label and its fix link while its step runs' | - |
| Board note: Fix links to the page that solves it: Backups, Runners, Configuration | done | cache/mail -> /admin/config, runners -> /admin/actions/runners, checkBackups -> /admin/asc/backups; TestTheBackupsCheckLinksAProblemToTheBackupsPage | - |
| Board note: All passed, warnings, failed, running (steps appear one by one), check could not run | done | checksTone, self_check_row.tmpl; TestAscHealthStates, TestAscHealthNoRun, health.test.js | - |

#### AdminHooks (ASC Hub admin: hooks and applications)

Area shell/settings. Rows 2: done 2, partial 0, missing 0. Copy found in the hub: 80% of 59 copy strings (mobile twin 78%). Template(s): hook_new.tmpl, hooks.tmpl, list.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Webhooks and OAuth applications across the site.; System hooks fire for every repository. Default hooks seed new repositories.; Audit sink; Default for new repos; Apps registered by people and by the site..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| System and default webhooks, OAuth applications list | done | templates/admin/hooks.tmpl:5 | - |
| Columns scope, events, last delivery with status text; Replay from the list; secret regenerate... | done | templates/admin/hooks.tmpl, templates/shared/webhook/rows.tmpl; routers/web/admin/asc_hooks_test.go; pixel 0.049 (mobile 0.269) | - |
| Board note: Replay re-sends the last failed delivery | done | templates/shared/webhook/rows.tmpl; tests/integration/asc_admin_hooks_test.go TestAscAdminHooksBoardNotes | - |
| Board note: Secrets are shown once | done | routers/web/repo/setting/webhook.go WebhookRegenerateSecret, templates/webhook/kind_fields.tmpl; TestAscAdminHooksBoardNotes | - |
| Board note: Default, failing delivery, empty (no hooks), secret regenerated toast | done | templates/admin/hooks.tmpl; TestAdminHooksOneList, TestAscAdminHooksBoardNotes | - |

#### AdminModeration (ASC Hub admin: moderation)

Area shell/settings. Rows 2: done 2, partial 0, missing 0. Copy found in the hub: 75% of 52 copy strings (mobile twin 65%). Template(s): -. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; README of casey/promo-kit; Reported by 3 people. The README contains a phishing link.; Comment on change 88; Reported by 1 person and 1 agent. The comment is hostile toward a reviewer.; Profile of @sam.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Reports queue with Dismiss, mark handled, content actions | done | templates/admin/moderation/reports.tmpl:28 | - |
| Reason per action written to the audit log; reports grouped by target; appeal state; agent rep... | done | routers/web/admin/asc_moderation.go, services/ascmoderation, services/ascagentreport, routers/api/ascagent | - |
| Board note: Dismiss, Hide and Suspend are separate, each with a reason that goes to the audit log | done | templates/admin/moderation/reports.tmpl, services/ascmoderation/hide.go; tests/integration/asc_admin_moderation_test.go TestAscAdminModerationBoardNotes | - |
| Board note: Reports group by target | done | models/moderation GetOpenReports (one item per content, reported_times) | - |
| Board note: Queue with items, empty (all caught up), item hidden toast, appeal received | done | templates/admin/moderation/reports.tmpl; tests/integration/asc_admin_moderation_test.go TestAscAdminModerationBoardNotes, TestAscHiddenContentStaysHidden | - |

#### AdminMonitor (ASC Hub admin: monitor)

Area shell/settings. Rows 2: done 2, partial 0, missing 0 (the board note row is not counted). Copy found in the hub: 80% of 64 copy strings (mobile twin 84%). Template(s): queue.tmpl, stacktrace.tmpl, stats.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Queues, scheduled tasks and stack traces.; Work waiting to be done. Flushing deletes waiting items.; Cron-style jobs.; Clean old sessions.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Queues table with Flush, cron table with Run now, stack traces | done | templates/admin/queue.tmpl (file) | - |
| Flush confirmation with count; Run now streams output; "Verify audit chain" cron task | done | templates/admin/queue_manage.tmpl, routers/web/admin/queue.go, templates/admin/queue.tmpl (cron-run), web_src/js/features/admin/cronrun.js, modules/taskout, services/asctrust/auditcheck.go (cron verify_asc_audit_chain); tests TestRunNowShowsTheOutputOfTheRun, TestRunAuditCheckFailsOnABrokenChain, cronrun.test.js | - |
| Board note: Flush asks for confirmation and counts what will be removed | done | templates/admin/queue_manage.tmpl (confirm names the waiting count), QueueRemoveAllItems flash counts the removed items; test TestQueueFlushNamesTheCount | - |
| Board note: Run now runs a task immediately and streams its output | done | templates/admin/queue.tmpl (cron-run), web_src/js/features/admin/cronrun.js, routers/web/admin/admin.go CronOutput; tests TestRunNowShowsTheOutputOfTheRun, cronrun.test.js | - |
| Board note: Default, backed-up queue, failed cron task, empty queue, flush confirmation | done | templates/admin/queue.tmpl, queue_table.tmpl, queue_manage.tmpl; tests TestMonitorShowsABackedUpQueueAFailedTaskAndTheEmptyState, TestQueueFlushNamesTheCount | - |

#### AdminNotices (ASC Hub admin: notices)

Area shell/settings. Rows 1: done 1, partial 0, missing 0. Copy found in the hub: 77% of 47 copy strings (mobile twin 67%). Template(s): notice.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Disk usage crossed 75%; Data volume is 78% full. See Health.; Cron: verify audit chain finished; Mail delivery failed 3 times; Notices older than 30 days are removed automatically..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Notices list, dismiss, dismiss all with confirm | done | templates/admin/notice.tmpl:31 | - |
| Board note: Dismiss removes one; Dismiss all asks to confirm | done | templates/admin/notice.tmpl, routers/web/admin/notice.go, services/context/base.go FormStrings; tests TestAscAdminNoticesBoardNotes, TestFormStringsReadsAURLEncodedBody | - |
| Board note: Default, empty (nothing to report) | done | templates/admin/notice.tmpl; test TestAscAdminNoticesBoardNotes | - |

#### AdminOrgs (ASC Hub admin: organizations)

Area shell/settings. Rows 2: done 2, partial 0, missing 0. Copy found in the hub: 80% of 49 copy strings (mobile twin 82%). Template(s): list.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; garage-eu; Rail Guild; rail-guild; r2-backup; ASC Hub 1.4.2.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Organizations table | done | templates/admin/org/list.tmpl (file) | - |
| Storage column (garage-eu / Server default) and Open button | done | templates/admin/org/list.tmpl and storage.tmpl: Storage column in place of Teams, the enabled storage targets of the organization from asc Storage.ListTargets (8 asked at a time), "Server default" without one or without asc, "Not known" when asc cannot be asked; Open goes to the organization page (routers/web/admin/asc_org_storage.go); tests TestLoadOrgStorage, TestAdminOrgsStorageColumn | - |
| Board note: Open goes to the organization page; admin actions are inside organization settings | done | list.tmpl Open href .HomeLink; TestAdminOrgsStorageColumn | - |
| Board note: Default, empty fresh install, search with no match | done | templates/admin/org/list.tmpl; test TestAscAdminOrgsBoardNotes | - |

#### AdminPackages (ASC Hub admin: packages)

Area shell/settings. Rows 2: done 2, partial 0, missing 0. Copy found in the hub: 85% of 48 copy strings (mobile twin 91%). Template(s): list.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; asc-ui-tokens; ASC Hub 1.4.2; Hub and web UI based on Forgejo; All systems operational; Rendered in 18 ms.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Packages table and delete | done | templates/admin/packages/list.tmpl:54 | - |
| Run cleanup rules with a preview of counts (hub runs CleanupExpiredData directly) | done | `services/packages/cleanup/preview.go` PreviewRules (per enabled rule: owner, type, versions and packages it would delete; deletes nothing), `routers/web/admin/asc_packages_cleanup.go` (`GET/POST ... | - |
| Board note: Cleanup shows exactly what it will delete before it runs | done | templates/admin/packages/cleanup_rules.tmpl, routers/web/admin/asc_packages_cleanup.go; test TestAscAdminPackagesBoardNotes | - |
| Board note: Default, empty, cleanup preview with counts | done | templates/admin/packages/list.tmpl, cleanup_rules.tmpl; test TestAscAdminPackagesBoardNotes | - |

#### AdminRepos (ASC Hub admin: repositories)

Area shell/settings. Rows 2: done 2, partial 0, missing 0. Copy found in the hub: 84% of 61 copy strings (mobile twin 88%). Template(s): list.tmpl, unadopted.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Every repository, its size and where its objects live.; garage-eu; garage-eu; Repositories found on disk without an owner appear here.; ASC Hub 1.4.2.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Repository table, unadopted list, delete | done | templates/admin/repo/list.tmpl:2 | - |
| Storage column and "storage mismatch" warning; delete needs full name typed | done | asc: Storage.ListRepoPlacements (site admin, 100 a call); hub: ascbyo.RepoPlacements, routers/web/admin/asc_repo_storage.go, templates/admin/repo/storage.tmpl (target name, Server default, Not known, Moving, and a warning when the target is gone or not enabled or objects remain in an earlier place with no migration moving them); the delete dialog asks for the full name (delete-button.js bindConfirmName) and DeleteRepo refuses another; TestRepoPlacements, TestAdminReposStorageColumn, TestAdminRepoDeleteNeedsName, delete-button.test.js | - |
| Board note: Manage opens the repository settings with admin rights | done | list.tmpl Manage links to the repository settings; a site administrator has every right there | - |
| Board note: Delete needs the full name typed | done | TestAdminRepoDeleteNeedsName | - |
| Board note: Default, unadopted empty, delete confirmation, storage mismatch warning | done | TestAdminReposStorageColumn (default and warning), TestAdminRepoDeleteNeedsName (confirmation); the unadopted card is in list.tmpl | - |

#### AdminRunners (ASC Hub admin: runners)

Area shell/settings. Rows 4: done 4, partial 0, missing 0. Copy found in the hub: 73% of 60 copy strings (mobile twin 65%). Template(s): actions.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; runner-eu-1; linux/amd64, docker; Running changeset checks, 4 min; runner-eu-2; linux/amd64, docker.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Runner list, status, labels, version, register, details | done | templates/admin/actions.tmpl:5 | - |
| Drain action and draining state | done | models/actions/task.go CreateTaskForRunner, routers/web/shared/actions/runners.go RunnerDrainPost; tests TestDrainingRunnerIsGivenNoNewTask, runner_drain_test.go | - |
| Agent runners (agent-box-1, "Agent run #214", vCPU) | done | routers/web/shared/actions/asc_agent_runners.go, templates/admin/actions.tmpl (agent-runners); asc Maintenance.ListAgentRunners; tests TestTheAdminRunnersPageListsTheAgentRunners, TestLoadAgentRunnersRefusesABadAnswer | - |
| Registration token that expires in one hour, shown once, in a copy block | done | templates/admin/actions.tmpl (adm-reg), routers/web/shared/actions/runners.go RunnerResetRegistrationToken | - |
| Board note: Details shows labels, jobs and logs; Drain stops new jobs | done | tests TestDrainingRunnerIsGivenNoNewTask, TestRunnerDrainPostSetsAndClearsTheSwitchForAnAdministrator | - |
| Board note: Registration token expires and is shown once | done | tests/integration/asc_runner_token_test.go TestRunnerRegistrationTokenShownOnce | - |
| Board note: Online, idle, offline (with since), draining, no runners (illustration + register command) | done | templates/admin/runner_state.tmpl (Draining chip), templates/admin/actions.tmpl; test TestAscAdminRunnersBoardNotes | - |

#### AdminUser (ASC Hub admin: dana lee)

Area shell/settings. Rows 5: done 5, partial 0, missing 0. Copy found in the hub: 79% of 62 copy strings (mobile twin 73%). Template(s): view.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Dana Lee; Account facts, usage and administrative actions for one person.; Dana Lee; Password and one passkey; None owned.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Single page with account facts, emails, repos, orgs, delete | done | templates/admin/user/view.tmpl:87 | - |
| Tabs Overview/Emails/Keys/Sessions/Activity | done | routers/web/admin/user_tabs.go, templates/admin/user/view.tmpl (`?tab=`) | - |
| Reset 2FA and Suspend as separate buttons that ask for a reason written to the audit log (toda... | done | routers/web/admin/users.go, Audit.Append host.user.* (section 17) | - |
| Usage block (storage, agent minutes) and agents owned | done | user_tabs.go loadUserUsage, loadUserAgents; view.tmpl `admin-user-agents`; tests/integration/asc_admin_user_agents_test.go | - |
| Sessions tab (devices, revoke) | done | view.tmpl Sessions tab with Revoke (section 17) | - |
| Board note: Tabs are views of one person: Overview, Emails, Keys, Sessions, Activity | done | routers/web/admin/user_tabs.go, templates/admin/user/view.tmpl; test TestAdminUserTabs | - |
| Board note: Reset 2FA and Suspend ask for a reason, which goes to the audit log | done | routers/web/admin/users.go UserSuspend, UserReset2FA; test TestAdminUserResetAndSuspendWantAReason | - |
| Board note: No impersonation: support acts through documented actions | done | routers/api/v1/api.go sudo refused unless [api] ENABLE_SUDO; test TestAPISudoIsOffByDefault | - |
| Board note: Default, suspended (banner), pending verification, deletion blocked by owned repositories | done | templates/admin/user/view.tmpl banners and delete-blocked; test TestAscAdminUserBoardNotes | - |

#### AdminUsers (ASC Hub admin: people)

Area shell/settings. Rows 4: done 4, partial 0, missing 0. Copy found in the hub: 77% of 62 copy strings (mobile twin 79%). Template(s): edit.tmpl, list.tmpl, new.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Admins 6; Suspended 3; Require 2FA; Dana Lee; Casey Brandt.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Search, filter chips (Admins, 2FA off, Inactive, Suspended), table with role/2FA/repos/last ac... | done | templates/admin/user/list.tmpl:30 | - |
| Counts on the filter chips (Admins 6, 2FA off 212 ...) | done | `routers/web/admin/asc_user_counts.go` adminUserCounts (one count per chip, whatever the search or chip in use), shown by the existing pill count in `templates/admin/user/list.tmpl`; a zero is not drawn; tests: ... | - |
| Row checkboxes, "2 selected" bar, Require 2FA and Suspend bulk actions with affected-people di... | done | ticks and the bar on list.tmpl (`asc-bulk.js`), `POST /admin/users/bulk` (`routers/web/admin/asc_users_bulk.go`): a confirm page with the affected people and a reason, then each change in the audit log ... | - |
| New person, Edit | done | templates/admin/user/new.tmpl (file) | - |
| Board note: Filter chips narrow in place and sync to the address | done | templates/admin/user/list.tmpl chips (status_filter in the address), data-asc-list-main skeleton; test TestAscAdminUsersBoardNotes | - |
| Board note: Row click opens the person; Edit opens the edit form | done | templates/admin/user/list.tmpl adm-rowlink, admin-base.css; test TestAscAdminUsersBoardNotes | - |
| Board note: Bulk actions confirm with a dialog that lists who is affected | done | routers/web/admin/asc_users_bulk.go, templates/admin/user/bulk.tmpl; test TestAdminUsersBulkActions | - |
| Board note: Default, bulk selection bar, suspended user, no matches, loading skeleton, empty (fresh i... | done | templates/admin/user/list.tmpl; tests TestAscAdminUsersBoardNotes, TestAdminUsersBulkActions (a fresh install lists its first administrator) | - |
| Board note: Suspended people stay visible with a red chip | done | templates/admin/user/role_chip.tmpl; test TestAscAdminUsersBoardNotes | - |

#### Dashboard (ASC Hub dashboard)

Area shell/settings. Rows 18: done 18, partial 0, missing 0. Copy found in the hub: 61% of 102 copy strings (mobile twin 60%). Template(s): asc_home.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; j/k to move - Enter to open - e to done; "Can we keep the old cursor format for one release? Mobile clients pin to it."; Agent asked a question: which currency table wins?; Blocking 2 changes; Ready to merge: Rate limit headers.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Greeting by time of day and summary line | done | routers/web/user/asc_dashboard.go (file) | - |
| Work list with counts (Inbox, My stacks, Review requests, Agent runs), repositories, teams | done | templates/user/dashboard/asc_home.tmpl:17 | - |
| Pinned repositories per person; counts update live | done | routers/web/user/asc_dashboard.go `ascDashRepos`, `AscDashCounts`; web_src/js/features/asc-dash-live.js `refreshCounts` | Profile pins lead the work switcher; the counts load again with the Today card every minute (TestAscDashReposPutThePinsFirst) |
| Scope chips Everything / org / Personal filter the whole page, remembered | done | templates/user/dashboard/asc_home.tmpl:48 (best match) | - |
| Needs-you rows with chips (Agent, checks, Touches billing), diff size, age | done | templates/user/dashboard/asc_home.tmpl (file) | - |
| Rows act in place: Approve, Reply, Answer (agent question), Add to merge queue, Resolve | done | routers/web/repo/asc_dash_act.go, templates/user/dashboard/asc_home.tmpl | Answer is the box of a waiting run in Agents working now |
| Row kinds: reply from a person, agent question, ready to merge with queue position and estimat... | done | routers/web/user/asc_dashboard_kinds.go, templates/user/dashboard/asc_dash_work.tmpl | Reply quote, queue place with the median wait, Trunk moved with "asc restacked 2 of 3", the question of a waiting run (asc_dashboard_kinds_test.go) |
| Keys j k, Enter, e (done), g then i, / | done | `asc-keys.js` e on the focused row; `asc_dashboard_test.go` | - |
| Agents working now with budget bar, paused state, All runs | done | templates/user/dashboard/asc_home.tmpl (file) | - |
| Stack bars: click a segment opens the review at that segment | done | see where-cell | - |
| Merge queue card (Running, median wait, positions, "2 of 3 checks", Open) | done | routers/web/user/asc_dashboard_queue.go, templates/user/dashboard/asc_dash_work.tmpl | median and estimates from asc MergeQueue.List |
| Today feed with Filter | done | see where-cell | - |
| From your terminal card with Copy; g i tip | done | templates/user/dashboard/asc_home.tmpl (file) | - |
| Live updates | done | routers/web/user/asc_dashboard.go `AscDashWork`, web_src/js/features/asc-dash-live.js `refreshWork` | every minute with the Today card, not while in use |
| States: loading skeleton, new account, offline error with retry countdown | done | `user/dashboard/asc_home.tmpl`: the main column is `data-asc-list-main` with a three-row skeleton a scope chip shows (`asc-list-loading.js`); new account and inbox zero empty states; `user/dashboard/asc_today.tmpl` ... | - |
| Undo toast ("Added to merge queue. Undo for 10 s") | done | templates/user/dashboard/asc_home.tmpl `data-asc-dash-undo`, web_src/js/features/asc-applied-toast.js | - |
| Appearance setting hides agent surfaces | done | see where-cell | - |
| Mobile: Pull to refresh; swipe a row left to snooze, right to mark done | done | `asc-row-swipe.js`, `data-asc-pull-refresh` on the dashboard; `asc-row-swipe.test.js` | - |

#### Docs (ASC Hub documentation)

Area shell/settings. Rows 7: done 7, partial 0, missing 0. Copy found in the hub: 57% of 159 copy strings (mobile twin 62%). Template(s): -. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: A short path to the right page: search first, six entry points, and the latest...; Install ASC, connect to a hub and open your first stack in five minutes.; Stacks and restack; How a feature becomes four reviewable changes, and how they stay in order.; Undo and the operation log; Recover from any merge, restack or agent apply..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Docs home: search, Popular chips, six entry cards, "New in ASC 1.4" list, Need a person card | done | templates/asc/docs.tmpl (file) | - |
| Docs search with grouped results (Commands, Guides), did-you-mean, no results, loading; Ctrl K... | done | services/ascdocs/commands.go, templates/asc/docs_results.tmpl, web_src/js/features/asc-docs-search.js | Results load as you type from /-/docs?q=&fragment=results; Ctrl K or / focuses the box (TestAscDocsSearch*) |
| Article layout: sidebar tree, 720 px column, breadcrumb, steps, callouts, Previous and Next | done | templates/asc/docs_page.tmpl, routers/web/asc_public.go `docsLead`, web_src/js/features/asc-docs.js `keepSideScroll` | Guides by section beside the article, lead, reading time, last change, callouts, steps, Previous and Next (TestAscDocsArticleLayout) |
| OS code tabs remembered; Copy without the prompt | done | `GET /-/docs/install` (`asc/docs_install.tmpl`): the steps of the release the download page offers, one tab for each system with the visitor's first; `initDocTabs` keeps the choice under `data-doc-remember` in ... | - |
| On-this-page TOC highlighting the section in view; Edit this page; Open in the hub | done | web_src/js/features/asc-docs.js `initAscDocs` | Open in the hub has no place: the hub serves the docs |
| "Was this page helpful?" Yes and No | done | `POST /-/docs/{slug}/feedback`, table `asc_docs_feedback`, ten a minute per address; counts for site administrators | `asc_docs_feedback_test.go`, `asc-docs-feedback.test.js` |
| Mobile: Docs menu drawer, sticky "On this page" row, sticky next-page bar | done | `asc/docs_page.tmpl`: Docs menu button before the breadcrumb opens `#site-docs-drawer` (every guide by section from `ascdocs.Sections`, a dialog run by `ds-shell.js` with focus return, `:target` without the script); ... | - |

#### Download (ASC Hub download)

Area shell/settings. Rows 12: done 12, partial 0, missing 0. Copy found in the hub: 50% of 167 copy strings (mobile twin 42%). Template(s): download.tmpl. Mobile twin: yes.

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
| Release notes link, Older releases | done | "Older releases" links the GitHub releases page, or for a local or fetched release the manifest's `releases_url` (https only, `modules/ascdownload/manifest.go`); test `TestAscDownloadPage` | - |
| States: No release yet (build from source, admin note), system not detected | done | templates/download.tmpl (file) | - |
| Loading releases skeleton | done | Not shown by design: the page is server rendered and the release is read and cached on the server (`ascdownload.Cached`, `SharedGitHubSource`), so no client-side loading state exists; a skeleton would show a state ... | - |
| "Signature check failed" and "verified" banners | done | Guidance in the Verify card under the command: what a verified and a failed `minisign -V` mean, the failed one says delete and do not run (`download.tmpl` `[data-asc-dl-outcome]`, keys `asc_download.sig_*`); test ... | - |
| Mobile: Sticky bottom bar "Download for your system"; files as cards | done | `download.tmpl` `[data-asc-dl-sticky]` (Download for the system, More); under 640 px each file row is a card with kind and size, hash with Copy and a signature icon, Download (`site.css`) | - |

#### Drafts (ASC Hub drafts)

Area shell/settings. Rows 3: done 3, partial 0, missing 0. Copy found in the hub: 51% of 39 copy strings (mobile twin 69%). Template(s): asc_drafts.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Add rate limit headers; - 3 changes - On this laptop and on the hub; Spike: streaming exports; - 1 change - Only on this laptop; Saved yesterday.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| List of drafts from the hub (name, repo, change count, saved time), terminal card, empty and e... | done | routers/web/user/asc_drafts.go:32 | - |
| Location line "On this laptop and on the hub / only on this laptop" | done | routers/web/user/asc_drafts.go (ascDraftsDevices); TestAscDraftsLocation | - |
| Continue (open in editor or show asc stack resume) and Discard buttons, 30-day undo, discarded... | done | routers/web/user/asc_drafts.go (ascDraftsAct); TestAscDraftsDiscardAndUndo | - |
| Board note: Continue opens the draft in the editor or shows the asc command to resume | done | templates/user/asc_drafts.tmpl (wk-draft-resume); TestAscDraftsDiscardAndUndo | Shows the command; there is no editor URI handler to open |
| Board note: Discard asks nothing: it is in the operation log and can be undone for 30 days | done | routers/web/user/asc_drafts.go (ascDraftsUndoDays); TestAscDraftsDiscardAndUndo | - |
| Board note: Drafts sync when you opt in; local-only drafts say so | done | templates/user/asc_drafts.tmpl (drafts-sync callout, wk-draft-where); TestAscDraftsPage, TestAscDraftsLocation | - |
| Board note: Drafts from the terminal and the hub, where each lives, empty, discarded with undo, loadi... | done | templates/user/asc_drafts.tmpl; TestAscDraftsPage, TestAscDraftsLocation, TestAscDraftsDiscardAndUndo | - |

#### Errors (ASC Hub error and status pages)

Area shell/settings. Rows 8: done 8, partial 0, missing 0. Copy found in the hub: 61% of 38 copy strings (mobile twin 82%). Template(s): 404.tmpl, 413.tmpl, 500.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Error and system pages; When something goes wrong, say what and what next.; Each page keeps the header and footer, uses the same illustration language, off...; Switch account; Learn about artifact storage; Nothing you did caused this, and nothing was changed. Our team has been told..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| 404, 403, 413, 500, 503, offline cards with header and footer | done | templates/status/*.tmpl (not found in tree) | the where-cell cites templates/status/*.tmpl, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Request id ("Request id 7f3a91c2 - date UTC", selectable) | done | templates/status/500.tmpl (file) | - |
| 500 actions: Try again (same request id) and Status page | done | templates/status/500.tmpl, templates/asc/status.tmpl (/-/status) | - |
| 403 actions: Request access and Switch account | done | templates/status/403.tmpl, routers/web/repo/asc_access_request.go, routers/web/auth/asc_switch_account.go | - |
| 404 actions: dashboard, search the hub | done | templates/status/404.tmpl (file) | - |
| 413 card with limit, artifact storage link and Back | done | templates/status/413.tmpl (file) | - |
| 503 maintenance: version being upgraded, reads continue through the CLI, window, expected back... | done | routers/web/admin/asc_maintenance.go, routers/web/asc_maintenance.go, templates/status/503.tmpl; asc Maintenance service (maintenance.v1) | - |
| Offline page: "your edit is saved on this device", Retry now, served by a service worker | done | `/-/offline` (`status/offline.tmpl`) from the worker `/-/asc-sw.js`, which keeps its styles and scripts; Retry now; the kept-edit line when `asc-drafts.js` holds a draft. Drafts of the markdown editors are kept on ... | - |

#### Explore (ASC Hub explore repositories)

Area shell/settings. Rows 15: done 15, partial 0, missing 0. Copy found in the hub: 68% of 144 copy strings (mobile twin 77%). Template(s): navbar.tmpl, repo_list.tmpl, repos.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Filter repositories; Has open stacks; repositories, sorted by recent activity; The hosted checkout: cart, address, payment and confirmation, built for sub-sec...; Updated Yesterday; design-tokens.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Tabs Repositories, Users, Organizations with counts | done | templates/explore/navbar.tmpl (file) | - |
| Filter bar: search, Sort, Visibility, kind pills; / focuses search | done | templates/shared/repo_search.tmpl:2 | - |
| Language filter menu | done | models/repo/language_stats.go:37 | - |
| Quick chips All, Has open stacks, Agent activity, Starred, Templates, Archived | done | templates/shared/repo_search.tmpl, models/repo/asc_quick_filter.go | - |
| List and Grid layout toggle; layout persists per person | done | templates/shared/repo_search.tmpl (file) | - |
| Row: owner avatar, name, visibility chip, description, topics, language, stars, updated, open-... | done | templates/explore/repo_list.tmpl (file) | - |
| Agent activity chip on a row | done | routers/web/shared/repoquick/agents.go, templates/explore/repo_list.tmpl, asc Agents.CountRuns per_repo and since | - |
| Changeset count on a row ("3,812 changesets") | done | models/repo/asc_changesets.go, modules/indexer/stats/db.go, routers/web/shared/repoquick/changesets.go, templates/explore/repo_list.tmpl | - |
| Contributor avatar stack on a row (people and agents) | done | templates/explore/repo_list.tmpl, models/activities/asc_contributors.go | - |
| Row actions on hover or focus-within: Copy clone command, Star | done | see where-cell | the where-cell cites asc clone host/owner/name, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Right rail: Trending this week (changesets this week), Popular topics, Clone anything you can... | done | templates/explore/rail.tmpl, models/repo/asc_trending.go | - |
| States: loading skeleton, empty hub (New repository, Import), no results with removable filter... | done | `explore/repo_list.tmpl`: "No repositories yet" with New repository and Import a project (signed in); "Nothing matches" with the count of active filters, the removable chips and Clear filters; a failed search keeps ... | - |
| Pagination "Showing 1 to 8 of 128" | done | templates/base/paginate.tmpl (file) | - |
| Mobile: Sort and filter bottom sheet with result count on the primary button | done | templates/shared/repo_search.tmpl:138 | - |
| Mobile: Pull to refresh; infinite scroll after 30 rows with Load more fallback | done | templates/shared/ds/more.tmpl, web_src/js/features/asc-explore-more.js, asc-pull-refresh.js | - |

#### ExploreOrgs (ASC Hub explore organizations)

Area shell/settings. Rows 6: done 6, partial 0, missing 0. Copy found in the hub: 67% of 109 copy strings (mobile twin 47%). Template(s): user_list.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Teams that own repositories, with who is in them.; Has agents; I am a member; Checkout, design system and storefront tooling.; Active Yesterday; Agent workflows, evaluations and the playbook library..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Org row: avatar, name, description, repositories and people counts | done | templates/explore/user_list.tmpl:6 | - |
| Verified badge (domain proof) and explainer card | done | templates/admin/federation/user_list.tmpl (file) | - |
| Agents count; member stack with up to four avatars and +N | done | routers/web/explore/org_rows.go, services/ascagentdir/dir.go, templates/explore/user_list.tmpl | - |
| 12-week sparkline and "Active 12 min ago" | done | models/activities/asc_weekly.go, templates/explore/asc_activity.tmpl | - |
| Chips All, Verified, Has agents, I am a member; Size filter; Sort most active | done | models/user/asc_org_search.go, routers/web/explore/org.go, templates/explore/search.tmpl | - |
| View hover action; Start an organization card; grid layout; states | done | View row action and empty, no-match and error states in `explore/user_list.tmpl`; Start an organization and verified explainer cards in `explore/users.tmpl`; List and Grid via `shared/ds/layout_toggle`; loading not ... | - |

#### ExploreUsers (ASC Hub explore people and agents)

Area shell/settings. Rows 13: done 13, partial 0, missing 0. Copy found in the hub: 67% of 145 copy strings (mobile twin 80%). Template(s): user_list.tmpl, users.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: The people and agents who write and review here, with how much they do.; Search by name, handle or role; Online now; accounts: 34 people and 8 agents; Staff engineer. Design reviews, flaky tests and anything that touches checkout.; Bengaluru.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| "People and agents" header and Invite people button | done | templates/explore/navbar.tmpl, routers/web/explore/user.go `inviteHref` | - |
| Search by name, handle or role | done | templates/explore/users.tmpl (file) | - |
| Show filter (People and agents / People / Agents), Organization filter, Sort "Most active" | done | templates/explore/search.tmpl, routers/web/explore/user.go, models/user/asc_search.go | - |
| Chips All, People, Agents, Maintainers, Online now | done | templates/explore/search.tmpl, routers/web/explore/user_agents.go `peopleChips` | Maintainers for signed-in viewers; Online now follows the presence switches (TestTheExplorePeopleHaveChipsAgentRowsAndWhoIsActive) |
| Role chip (Owner, Maintainer, Member, Agent) and "Roles on this hub" card | done | templates/explore/user_list.tmpl, templates/explore/users.tmpl, models/user/asc_roles.go | - |
| Agent rows: violet square avatar, @org/name, owner, scope, View runs | done | templates/explore/users.tmpl, services/ascagentdir | Agents that ran in the asc repositories the viewer can read, five on All, all on Agents |
| Meta row: location, joined, organizations, public email as own labelled item | done | templates/explore/user_list.tmpl, routers/web/explore/user.go `loadExploreUserRows` | - |
| Activity sparkline (12 weeks, text equivalent), counts "changesets, reviews" | done | models/activities/asc_weekly.go, templates/explore/asc_activity.tmpl, templates/explore/user_list.tmpl | - |
| Hover action Follow; View runs for agents | done | templates/explore/user_list.tmpl, templates/explore/users.tmpl | - |
| Rail "Who is active now" with presence and current activity | done | routers/web/explore/user_agents.go `activeNow`, templates/explore/users.tmpl | People online with their last public action of the hour; agents with runs going |
| Grid layout | done | see where-cell | - |
| States: loading, no people match, filter chips, error | done | templates/explore/user_list.tmpl | Server rendered, no loading state |
| Mobile: Chips filter People, Agents, Maintainers, Online now; Follow inside the row at 44 px | done | templates/explore/search.tmpl, web_src/css/asc-ds/touch.css | - |

#### GetStarted (ASC Hub get started)

Area shell/settings. Rows 10: done 10, partial 0, missing 0. Copy found in the hub: 54% of 85 copy strings (mobile twin 57%). Template(s): home.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Your hub is empty. Let's fix that.; Four steps from nothing to your first reviewed stack. Skip any step; this page...; Sign the command line in and trust this device. The command line is how you wor...; Start a new repository; Import an existing project; Invite teammates.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Four-step checklist with aria-current=step, done as a word | done | templates/asc/get_started.tmpl:1 | the where-cell cites aria-current=step, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Shown on GET / for a signed-in person with no repositories; dismissible; returns while none ex... | done | routers/web/home.go, routers/web/asc_public.go (ascHasNoRepository, POST /-/get-started/hide), templates/asc/get_started.tmpl | Hide lasts until the next sign-in (session) |
| Two parallel first-run pages (asc/get_started.tmpl, user/asc_welcome.tmpl with old hub- classe... | done | user/asc_welcome.tmpl (not found in tree) | the where-cell cites user/asc_welcome.tmpl, routers/web/asc_welcome.go, not in the tree (renamed or stale text); behaviour taken... |
| Step 2 ticks by itself when the first CLI sign-in arrives, no reload | done | routers/web/asc_public.go (GET /-/get-started/status), web_src/js/features/asc-welcome.js | - |
| Import step with source tabs and progress "Reading history 12,408 commits" | done | routers/web/asc_get_started_import.go (POST /-/get-started/import, GET /-/get-started/import/status), templates/asc/get_started.tmpl, web_src/js/features/asc-welcome.js | https addresses only; the import is private |
| Health check (Git found, signed in, SSH key, shell shim; asc doctor on machine and server) | done | services/ascdoctor, routers/api/v1/user/asc_doctor.go (POST /api/v1/user/asc/doctor), templates/asc/get_started.tmpl, asc crates/asc/src/cli/doctor/report.rs | the machine sends names and levels only, no paths |
| "This hub" card: Storage, AI providers, Email with Change and Add | done | see where-cell | - |
| Shortcuts card | done | see where-cell | - |
| "Open the sample project" read-only | done | services/ascsample, POST /-/get-started/sample | - |
| Empty-area cards: inbox zero on day one, no repositories, "just you" with Invite | done | Inbox day-one state (`asc_inbox.tmpl`, `DayOne`); dashboard rail cards "No repositories yet" and "It is just you for now" (`asc_home.tmpl`) | - |

#### Inbox (ASC Hub inbox)

Area shell/settings. Rows 15: done 15, partial 0, missing 0. Copy found in the hub: 53% of 79 copy strings (mobile twin 45%). Template(s): asc_inbox.tmpl, notification.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; for shortcuts; Sorted by: oldest waiting; Mark done; "Can we keep the old cursor format for one release? Mobile clients pin to it."; Mark done.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Tabs Needs you, Waiting on others, Agents, Done with counts (hub adds Drafts) | done | routers/web/user/asc_inbox_page.go:26 | - |
| Filters: All repos, Humans and agents, Sorted by | done | see where-cell | - |
| Unread dot plus bold title | done | templates/user/asc_inbox.tmpl (`is-unread`), routers/web/user/asc_inbox_page.go `ascInboxMarkUnread` | Unread comes from the person's notifications; opening a row reads it (TestAscInboxUnread) |
| Row hover actions (Review, Answer, Mark done, Snooze) | done | `templates/user/asc_inbox.tmpl` `[data-asc-row-acts]`: Review, Answer (agent rows that wait), Mark done or Restore, Snooze 4h, each posting to `/-/asc/inbox/state`; tests in `routers/web/user/asc_inbox_test.go` | - |
| Select rows (x, checkbox); bulk bar "2 selected: Mark done, Snooze, Unwatch" in a live region | done | Mark done, Snooze, Unwatch; `do=unwatch`; `ds-bulk.test.js`, `TestAscInboxUnwatch` | - |
| Mark done (e) with fade and undo toast; Done keeps 30 days and can be restored | done | `done_unix` on `asc_inbox_state` (migration `v18l_asc_inbox_done.go`); `SetAscInboxDone`; `do=done` and `do=restore` in `AscInboxStatePost`; an item comes back when the pull request changes; the Done tab lists it for ... | - |
| Snooze menu: Later today 17:00, Tomorrow 09:00, Monday 09:00, When the checks finish, Pick a d... | done | templates/user/asc_inbox.tmpl (.wk-snooze-menu), routers/web/user/asc_inbox_page.go ascInboxSnoozeUntil, models/issues/asc_inbox_state.go SetAscInboxSnoozeChecks, services/ascinbox ChecksRunning | - |
| Detail pane: why waiting, changed since last look, policy line (approvals 1 of 2), thread, rep... | done | templates/user/asc_inbox.tmpl:167 | - |
| "Ask agent to change" (comment becomes an instruction for the author agent) | done | routers/web/user/asc_inbox_ask.go, templates/user/asc_inbox.tmpl (reply form) | - |
| Keys j k, x select, e done, s snooze, a approve, r request changes, p pin, Enter | done | a and r on the reply form; `asc-keys.test.js` | - |
| Pagination "Showing 8 of 23" | done | templates/user/asc_inbox.tmpl (file) | - |
| New items slide in from the top without jumping the list | done | routers/web/user/asc_inbox_page.go `AscInboxRows` (GET /-/asc/inbox/rows), templates/user/asc_inbox_rows.tmpl, web_src/js/features/asc-inbox-live.js | - |
| States: bulk bar, inbox zero with Watch more repositories, loading skeleton | done | templates/user/asc_inbox.tmpl:30 (best match) | - |
| Subscriptions link to Settings, Notifications | done | templates/user/asc_inbox.tmpl:110 (best match) | - |
| Mobile: Swipe right to mark done, left to snooze; sticky action bar with Open next | done | `asc-row-swipe.js` on inbox rows; `TestAscInboxRowState` | - |

#### Install (ASC Hub first-run setup)

Area shell/settings. Rows 8: done 8, partial 0, missing 0. Copy found in the hub: 57% of 84 copy strings (mobile twin 47%). Template(s): install.tmpl, install_progress.tmpl, post-install.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Installation guide; SQLite is fine for a team of ten. Choose PostgreSQL for more.; One file. No server to run.; For teams and production.; Test connection; Connected to PostgreSQL 16.4.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Setup form: database cards, general settings, administrator, email and service options | done | templates/install.tmpl:54 | - |
| Test connection with result ("Connected to PostgreSQL 16.4" or the cause); Install disabled until it passes | done | `POST /test-db` (routers/install/routes.go), install.js initDbTest; routers/install/asc_dbtest_test.go | - |
| Database error card with "Test again" and "Use SQLite instead" | done | install.tmpl data-ins-db-error card and actions; `TestAscInstallDatabaseErrorCard` | - |
| Sticky summary rail (Database, URL, Registration) and live step progress | done | install.tmpl ins-rail, install.js initProgress; install.test.js | - |
| "Setting up your hub" progress page (Step n of 5) that polls, then redirects | done | `GET /-/install/status` (routers/install/asc_progress.go), templates/install_progress.tmpl, install.js setRun; `TestAscInstallStatus`, `TestAscInstallProgressMarkup` | - |
| Post-install page: signed in as, asc auth login --host ... with Copy, Open the hub, Download ASC | done | templates/post-install.tmpl; `TestAscPostInstallPage` | - |
| Skip administrator: first registered user becomes owner | done | templates/install.tmpl data-ins-admin, install.js initAdmin, routers/web/auth/auth.go handleUserCreated; `TestAscFirstUserOwnsTheHub` | - |
| Mobile: One section per screen with progress bar, Back and Next | done | install.js initPhoneStepper under 768 px; install.test.js | - |

#### Main (ASC Hub landing)

Area shell/settings. Rows 12: done 12, partial 0, missing 0. Copy found in the hub: 83% of 220 copy strings (mobile twin 74%). Template(s): home.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: ASC 1.4: agent runs and the merge queue are here; .deb .rpm; signature verified by the installer; brew install asc; asc 1.4.2 (minisign verified); macOS, Linux and Windows. This hub also serves a git-compatible remote over HTT....

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Hero: announcement pill, headline, lead, Download ASC / Create account / Sign in | done | `templates/asc/land_desktop.tmpl` and `land_mobile.tmpl` (generated from the boards, keys `asc_bl.d*` and `asc_bl.m*`); the pill names the version of the release the hub offers (`ASC 0.1.3: ...`) and drops the ... | - |
| Download ASC goes to /download and preselects the detected OS | done | templates/home.tmpl:14,305 (href /download, detected OS in routers/web/asc_download.go) | - |
| Install method pills (Homebrew, Scoop, winget, curl, PowerShell, .deb .rpm) as a labelled grou... | done | modules/ascdownload/offer.go:60 | - |
| Product window in the hero (stack rail, change with intent, comment, whose turn) | done | templates/home.tmpl:58 | - |
| Works-with strip | done | `land_desktop.tmpl` `blds49`, heading `asc_bl.d045` "Works with the editors and agents your team already uses" | - |
| Why ASC, stacks, review, code intelligence, policy cards | done | `land_desktop.tmpl`: Why ASC with the three problem cards (`asc_bl.d052`-`d060`), How stacks work, review, code intelligence and policy sections as the board; the older sections are gone | - |
| Operation log and merge queue demo | done | templates/home.tmpl:160 | - |
| Agents band and run preview | done | templates/home.tmpl:210 | - |
| Workflow cards (dependency upgrade, flaky triage, large migration) | done | templates/home.tmpl:226 | - |
| Bring your own keys and storage (provider rows, S3 endpoint, bucket, region) | done | templates/home.tmpl:264 | - |
| Final call to action and footer | done | `land_desktop.tmpl` CTA `asc_bl.d173`/`d174` ("Install ASC in a minute, import a repository with its history..."); footer groups Product, Developers, Company, Legal and the status line in `base/footer_content.tmpl` | - |
| Mobile: Sticky bottom bar with Download ASC and Sign in; install pills scroll sideways; workflow cards... | done | templates/home.tmpl:31 | - |

#### NewOrg (ASC Hub new organization)

Area shell/settings. Rows 3: done 3, partial 0, missing 0. Copy found in the hub: 62% of 29 copy strings (mobile twin 70%). Template(s): create.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Who owns it; Create a first team; A team called Everyone, with you as lead.; You can change this in organization settings later.; ASC Hub 1.4.2.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Handle field, visibility, create | done | templates/org/create.tmpl (file) | - |
| Display name field, "Who owns it", "Create a first team Everyone", "Allow agents" switch | done | templates/org/create.tmpl, routers/web/org/org.go CreatePost, routers/web/org/org_create_test.go | - |
| Handle availability check as you type, reserved-name error, live preview card, loading button | done | routers/web/org/org.go CreateCheck, web_src/js/features/org-new.js | - |
| Board note: Handle availability checks as you type | done | tests/integration/new_org_test.go TestNewOrganizationHandleCheck | - |
| Board note: Preview updates live | done | web_src/js/features/org-new.test.js | - |
| Board note: Default, handle taken (error), handle reserved, creating (button loading), created toast | done | org-new.test.js, TestNewOrganizationHandleCheck, org.go CreatePost flash | - |

#### OrgPeople (ASC Hub organization people)

Area shell/settings. Rows 7: done 7, partial 0, missing 0. Copy found in the hub: 62% of 80 copy strings (mobile twin 61%). Template(s): add_org_member.tmpl, members.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; London and remote; Everyone 38; Owners 3; Pending 2; Change role.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Member rows with avatar, owner/member chips, 2FA chip, remove/leave, public/private toggle | done | templates/org/member/members.tmpl:47 | - |
| Search box, filter chips with counts (Everyone, Owners, 2FA off done; Pending needs the invita... | done | templates/org/member/members.tmpl, routers/web/org/members.go | - |
| Change role menu with Maintainer role and undo toast; last owner cannot be demoted | done | routers/web/org/people.go changeRole, templates/org/member/members.tmpl role-undo | - |
| Row detail: teams list, last active, changesets count | done | routers/web/org/people.go landedByPerson, services/asctrust/ownercontributors.go (Insights.ListOwnerContributors) | - |
| Agents section (agent identity, run by, scoped to paths, Manage) | done | routers/web/org/people.go orgAgents | - |
| Pending invitations (email, role, invited by, Resend, Revoke) | done | routers/web/org/people.go pendingInvites | - |
| Invite dialog accepting several emails and a role | done | templates/org/member/add_org_member.tmpl, routers/web/org/people.go inviteSeveral | - |

#### OrgProfile (ASC Hub organization profile)

Area shell/settings. Rows 8: done 8, partial 0, missing 0. Copy found in the hub: 64% of 58 copy strings (mobile twin 62%). Template(s): header.tmpl, home.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; London and remote; Find a repository; Sort: Recently updated; Customer dashboard and checkout.; Terraform, queues and rate limits..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Head: avatar, name, description, location, website, Follow, Org settings | done | templates/org/header.tmpl:1 | - |
| Verified badge; "38 people, 4 agents" counts | done | templates/org/header.tmpl, services/context/org.go (AscOrgDomain, OrgAgentCount), routers/web/org/people.go | - |
| Tabs Repositories, People, Teams, Packages, Projects with counts | done | templates/org/menu.tmpl:1 | - |
| Repo rows with check status text (Passing, 1 failing, No checks) and a Watch toggle with count | done | routers/web/shared/repoquick/rowstate.go:1 | - |
| Type and Sort menus land in the URL | done | templates/shared/repo_search.tmpl (file) | - |
| Side cards: People, Teams, Agents, Merge policy with "Read the policy" | done | templates/org/home.tmpl, routers/web/org/asc_profile_cards.go | - |
| Member view shows Org settings; lock chip for private repositories | done | templates/org/header.tmpl:11 | - |
| Loading skeleton; mobile row menu | done | `org/home.tmpl` (`data-asc-list-main` with a hidden skeleton card; the search, a sort, a kind pill and the pager show it, `asc-list-loading.js`, `repo-search.js`); `explore/repo_list.tmpl` row menu under 768 when ... | - |

#### OrgSettings (ASC Hub organization settings)

Area shell/settings. Rows 8: done 8, partial 0, missing 0. Copy found in the hub: 63% of 70 copy strings (mobile twin 57%). Template(s): layout_head.tmpl, navbar.tmpl, options.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Runners and secrets; Upload logo; Lisbon and remote.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Profile fields, logo upload/remove, visibility, repo-admin team access, repository creation li... | done | templates/org/settings/options.tmpl:52 | - |
| Who can create repositories (owners / maintainers), base permission for members, default repos... | done | models/organization/asc_repo_defaults.go:38, templates/org/settings/options.tmpl | - |
| Agents card: allow agents, agent changesets need a person, monthly agent spend ceiling | done | routers/web/org/setting/asc_agents.go:23 | - |
| Default merge policy shown + Edit policy (repositories inherit and can only tighten) | done | routers/web/org/setting/asc_agents.go:28 | - |
| Transfer ownership | done | `services/org/transfer.go` TransferOwnership (an owner makes a member an owner; optionally leaves the Owners team in the same transaction, so there is never a moment without an owner), ... | - |
| Delete organization with typed handle | done | templates/org/settings/delete.tmpl:1 | - |
| Owner-only banner and disabled fields for non-owners (settings routes require owner today) | done | templates/org/settings/options.tmpl, routers/web/web.go | - |
| Left menu: AI keys, Storage, Audit, Packages, Webhooks, Applications, Runners, Blocked users,... | done | templates/org/settings/navbar.tmpl (file) | - |

#### OrgTeams (ASC Hub organization teams)

Area shell/settings. Rows 4: done 4, partial 0, missing 0. Copy found in the hub: 68% of 53 copy strings (mobile twin 66%). Template(s): members.tmpl, new.tmpl, repositories.tmpl, teams.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; London and remote; Infra, queues, rate limits and runners.; Cross-team reviewers for money-touching paths.; Tokens, components and docs.; Edit team.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Team cards (name, access, description, avatars, member and repo counts), create/edit/delete te... | done | templates/org/team/teams.tmpl (file) | - |
| Selecting a card loads detail below without leaving (members + repositories with access) | done | web_src/js/features/org-team.js pickTeam | - |
| Policy owner note: team linked to a path pattern in merge policy | done | services/ascteams (team sync and OwnershipOf), templates/org/team/teams.tmpl (Policy owner) | - |
| Agents as team members with a path scope | done | services/ascteams/agents.go, teams.tmpl (Agents), TestAgentsAreTeamMembers, TestTheTeamPageManagesAgentMembers | - |
| Board note: Teams is a view of the organization; selecting a card loads detail below without leaving | done | web_src/js/features/org-team.test.js | - |
| Board note: Policy owner links a team to a path pattern in merge policy | done | TestTheTeamPageShowsWhatPoliciesGiveTheTeam | - |
| Board note: Agents can be team members with a path scope | done | TestTheTeamPageManagesAgentMembers | - |
| Board note: Default with selected team, empty team (add member prompt), team without repositories, de... | unverified | not tracked in the gap files | needs a row and a test |

#### Packages (ASC Hub packages)

Area shell/settings. Rows 7: done 7, partial 0, missing 0. Copy found in the hub: 62% of 61 copy strings (mobile twin 64%). Template(s): list.tmpl, packages_cleanup_rules_edit.tmpl, view.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; London and remote; Filter packages; - published Today; - published Today; design-tokens.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Package list with type, version, downloads, filter by type | done | templates/package/shared/list.tmpl:1 | - |
| Type chips with counts (a select today) | done | templates/package/shared/list.tmpl, models/packages/asc_package_type_count.go | - |
| Visibility chip per package (Public/Private/Internal) | done | templates/package/shared/list.tmpl, routers/web/shared/packages/asc_list.go | - |
| Package page views Install/Versions/Dependencies/Settings as tabs | done | templates/package/tabs.tmpl, routers/web/user/package_tabs.go | - |
| "Published from acme/payments by a landed stack", "Signed with release key" | done | TestAscPackageProvenance, TestPackageViewShowsProvenance | - |
| Cleanup rule preview ("removes 14 versions of 6 packages, frees 1.3 GB") | done | templates/org/settings/packages_cleanup_rules_preview.tmpl:3 | - |
| Error state with request id and Status page link | done | templates/package/shared/list.tmpl, routers/web/shared/packages/asc_list.go | - |

#### Profile (ASC Hub profile)

Area shell/settings. Rows 13: done 13, partial 0, missing 0. Copy found in the hub: 58% of 73 copy strings (mobile twin 56%). Template(s): asc_agents.tmpl, asc_heatmap.tmpl, header.tmpl, profile.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Online, local time 14:20; Draft: provider failover table; Fix clock skew in token expiry; Customer dashboard and checkout. Cursor pagination for every list..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Header: avatar, name, handle, location, bio, followers, following, joined, website, Follow | done | templates/user/overview/header.tmpl (file) | - |
| Presence pill ("Online, local time 14:20") | done | templates/shared/user/profile_big_avatar.tmpl, services/ascpresence | - |
| Role line ("Staff engineer - Payments team - London") | done | Role is the `asc.role` user setting (Role field in profile settings, 60 characters, trimmed, empty clears it); the line in `profile_big_avatar.tmpl` adds role, the first team the viewer may know about ... | - |
| Request a review (picker with repository and change) and Mention | done | built | Request a review and Mention beside Follow, with a picker of the viewer's open pull requests (routers/web/user/asc_profile_ask.go) |
| Tabs Overview, Work (11), Reviews (34), Repositories (9), Activity | done | templates/user/overview/header.tmpl (file) | - |
| Working-now list (stack, draft, ready) | done | templates/user/profile.tmpl (file) | - |
| Pinned repositories (up to six) with Customize dialog and drag reorder | done | templates/user/settings/pins_form.tmpl, web_src/js/features/asc-pins.js, routers/web/user/asc_profile_tabs.go `ascPinRows` | Cards show stacks and changes; drag the cards, or Customize in a dialog with Move up and Move down (TestAscProfilePins) |
| Contributions: year switch, summary (changes, reviews, median turnaround), violet cells for ag... | done | routers/web/user/asc_profile_contrib.go, templates/user/asc_heatmap.tmpl | Hub only: agent days from pull_request_asc_run, reviews and median turnaround from review rows (TestAscProfileContributions) |
| "Agents Priya runs" with New agent | done | routers/web/user/asc_profile_agents.go `ascProfileAgents`, templates/user/profile.tmpl | Runs the person created, at most five, New agent on one's own profile (TestTheProfileHasTheAgentsThePersonRuns) |
| Recent activity | done | templates/user/profile.tmpl:76 | - |
| "Knows best" (path share of changes and reviews, six months) | done | routers/web/user/asc_profile_knows.go, templates/user/profile.tmpl | Hub only: files of readable pull requests changed or reviewed, kept per head (TestAscProfileKnows) |
| Teams and Focus chips beside Repositories and Organizations | done | Teams card (`wk-pteams`, members only) and Focus card (`[data-asc-pf-focus]`): the topics and main language of up to 12 readable repositories the person changes, weighted by their pull requests, six at most, each ... | - |
| States: owner view (Edit profile, View as visitor), private-profile banner, empty pins and con... | done | `shared/user/profile_big_avatar.tmpl` Edit profile and a View as visitor toggle (`?visitor=1` renders the page as signed out, `aria-pressed`); `shared/user/header.go` `AscPrivateLimited` hides the details and stats ... | - |

#### Projects (ASC Hub projects)

Area shell/settings. Rows 5: done 5, partial 0, missing 0. Copy found in the hub: 54% of 48 copy strings (mobile twin 56%). Template(s): list.tmpl, new.tmpl, view.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; London and remote; Release 2.19; Everything planned for the next release, with the real state of each stack.; Add card; Webhook receivers back off per host.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Kanban board with columns, drag, edit/close/delete | done | templates/projects/view.tmpl:82 | - |
| Cards that are stacks following real state; Landed column by stack state | done | services/pull/asc_project_cards.go, templates/repo/issue/card.tmpl, templates/projects/view.tmpl (Landed column), models/project/column.go SetLandedColumn | - |
| Move menu (keyboard alternative) announcing the new column | done | templates/projects/view.tmpl, web_src/js/features/project-move.js | - |
| Add card, Switch project select, rename and hide columns | done | templates/projects/view.tmpl, services/issue/asc_project_card.go, web_src/js/features/project-board.js | - |
| Empty, loading and error ("request id") states | done | templates/projects/view.tmpl (empty, error banner, status), project-move.js | - |
| Board note: Cards move between columns by drag or by the Move menu; a stack card follows its real sta... | done | settings.md row 217; services/pull/asc_project_cards_test.go, routers/web/repo/asc_project_landed_test.go | - |
| Board note: Cards link to the stack or the issue | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Columns can be renamed and hidden | done | settings.md row "Add card, Switch project select, rename and hide columns"; TestProjectBoardCardsAndColumns | - |
| Board note: Project board with four columns of stacks and issues; empty; loading; error | unverified | not tracked in the gap files | needs a row and a test |

#### Shell (ASC Hub shell: header, menus, palette, footer)

Area shell/settings. Rows 23: done 23, partial 0, missing 0. Copy found in the hub: 64% of 171 copy strings. Template(s): footer_content.tmpl, head_navbar.tmpl. Mobile twin: no.

Board copy not found word for word in locale, templates or scripts: Four links, one clear way in; Search always one key away, Inbox always one click away; Account, create and notifications; New changeset; Mark all read; approved Backoff with jitter.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Signed-out header: Explore, Download, Docs, About, quiet Sign in, cyan Create account | done | templates/base/head_navbar.tmpl:5 | - |
| Docs and About links (header and footer) open the hub's own pages (/-/docs, /-/about, both rou... | done | templates/base/head_navbar.tmpl:14 | - |
| Signed-in header: Changes, Agents, Explore, Download, search field with / and Ctrl K hints, In... | done | templates/base/head_navbar.tmpl:3 | - |
| Active link: white label, 2px cyan marker, aria-current=page | done | templates/base/head_navbar.tmpl:85 | the where-cell cites aria-current=page, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Inbox amber count from /-/asc/inbox/count, cached 60 s | done | web_src/js/features/asc-keys.js:196 | - |
| "N agents running" pill in the header (cyan dot, links to Agents) and in the mobile top bar | done | templates/base/head_navbar.tmpl, routers/web/user/asc_agents_count.go (Agents.CountRuns) | - |
| Notifications bell opens a popover (unread count, Mark all read, items with avatar, text, repo... | done | routers/web/user/asc_bell.go, templates/user/notification/asc_bell.tmpl, web_src/js/features/asc-bell.js | - |
| Account menu: identity, profile, inbox with count, agents, settings, Appearance (Light, Dark,... | done | templates/base/head_navbar.tmpl:97 | the where-cell cites #ds-account, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Theme and density persist per person: localStorage, then the account setting | done | routers/web/user/asc_prefs.go, models/user/asc_prefs.go, templates/base/head_style.tmpl, web_src/js/features/asc-prefs.js | - |
| Create menu: New repository, Import a project, New organization, New changeset, Start an agent... | done | All five are in #ds-create (head_navbar.tmpl); "New changeset" opens `/-/asc/changeset/new` (`H:routers/web/user/asc_changeset_new.go`), the asc repositories the person can write, each opening its editor on the ... | - |
| Menus close on Esc or outside click, return focus to trigger, fade and rise, reduced motion | done | web_src/js/features/ds-shell.js (file) | - |
| Command palette opens from /, Ctrl K, header search, mobile search; Esc, arrows, Enter | done | web_src/js/features/ds-shell.js:611 | - |
| Palette is a combobox: role=combobox input, listbox results, aria-activedescendant | done | templates/base/head_navbar.tmpl:275 | the where-cell cites role=dialog, role=search, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Palette scope chips All, Changes, Code, People, Commands; Tab cycles the scope | done | templates/base/head_navbar.tmpl:271 | - |
| Palette results grouped Changes (title, repo and number), Code (path, symbol), People, fetched... | done | `routers/web/asc_palette.go` AscPalette (`GET /-/asc/palette?q=&at=`): Commands, Changes (pull requests the person may read), Code (ascPaletteCode: symbol definitions from asc `CrossSearch.LookupSymbol` over the ... | - |
| Command verbs (undo, restack, merge) switch the list to actions; Commands group with Undo last... | done | routers/web/asc_palette_commands.go, routers/web/repo/asc_ops_last.go, web_src/js/features/ds-shell.js (verbScope) | No Cmd Z shortcut; the undo is reached from the palette (STACK-OPERATIONS.md) |
| Empty query shows Recent items and Shortcuts (Go to inbox: g i) | done | web_src/js/features/ds-shell.js:186 | - |
| No-results text names the query and suggests a symbol, handle or title | done | see where-cell | the where-cell cites asc_shell.palette_empty, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Keyboard shortcuts overlay (?, "All shortcuts") | done | web_src/js/features/asc-keys.js (file) | - |
| Footer: full footer on public pages (four groups, version, Based on Forgejo, language, theme s... | done | templates/base/footer_content.tmpl:1 | - |
| Footer links: Stacks and changes, Agent runs, Changelog, CLI reference, REST API, MCP server,... | done | `footer_content.tmpl` links the docs pages `stacks-and-operations`, `agents`, `cli`, `mcp`, the docs "New in ASC" list (`#docs-new`), `/-/status` and the About sections `#security`, `#contact`, `#privacy`, `#terms`, ... | - |
| "All systems operational" status in the footer | done | modules/templates/asc_status.go, routers/init.go, templates/base/footer_content.tmpl (.ds-footer-status) | - |
| "Rendered in 18 ms" in the footer | done | see where-cell | - |

#### UserSettingsAI (ASC Hub settings: ai keys)

Area shell/settings. Rows 8: done 8, partial 0, missing 0. Copy found in the hub: 64% of 114 copy strings (mobile twin 61%). Template(s): asc_ai.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Bring your own API keys. Agents use the keys of their own scope, and the cost g...; Personal Anthropic; Embeddings key; - text-embedding-3-large - tested yesterday.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Key list with provider, hint, model, tested time, spend meter, Test/Details; add form; state c... | done | routers/web/shared/byoweb/aikeys.go (file) | - |
| Test connection result with latency (ms), polite live region | done | see where-cell | - |
| Test result lists model count ("9 models available") | done | routers/web/shared/byoweb/aikeys.go (file) | - |
| Key detail tabs (Usage, Limits, Models, Activity) and spend-by-day chart | done | templates/shared/asc/byo_ai_key.tmpl (tabs with ?tab=, asc-byo-bar chart) | - |
| Warn me at 80% (inbox + email), Stop agents at the cap (pause with a question) | done | routers/web/shared/byoweb/aishares.go (file) | - |
| Who can use it: allow my agents, share with organization (org owners attach it, see spend not... | done | routers/web/shared/byoweb/aishares.go (file) | - |
| Activity tab (who used the key, runs, tokens) | done | services/ascbyo/uses.go, routers/web/shared/byoweb/uses.go (AiProviders.ListActivity) | - |
| Masked key field with Show button; remove needs a ticked box | done | see where-cell | - |

#### UserSettingsAccount (ASC Hub settings: account)

Area shell/settings. Rows 5: done 5, partial 0, missing 0. Copy found in the hub: 69% of 68 copy strings (mobile twin 61%). Template(s): account.tmpl, repos.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Email addresses, sign-in methods and what happens to your data.; Notifications and sign-in alerts go to your primary address.; Receives notifications and sign-in alerts; Waiting for you to click the link we sent.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Email list, primary, verify/resend, add, delete | done | templates/user/settings/account.tmpl (file) | - |
| Resend rate limit that says when to retry | done | `routers/web/user/setting/asc_resend.go`: the time of the last confirmation mail is kept next to the 180 s limit and the refusal now says "You can ask for another in 45 seconds" (a flash) instead of redirecting ... | - |
| Sign-in methods card (SSO connected, link to Security for passkeys/password) | done | templates/user/settings/security/accountlinks.tmpl (file) | - |
| Export your data (changesets, reviews, comments, settings; emailed link; requested/ready states) | done | routers/web/user/setting/asc_account.go AscExport, AscExportRequest; services/ascexportmail (the emailed link); tests TestAscExportRequestWatchesTheExportForTheEmail, TestAReadyOrFailedExportIsEmailedOnce | - |
| Delete account: typed username, 2FA code, blocked state when repos are owned, dialog focus return | done | routers/web/user/setting/account.go DeleteAccount, deleteTwoFactorPassed; web_src/js/features/delete-button.js dialogKeydown; tests TestUserDeleteAccountAsksTwoFactorCode, TestUserDeleteAccountWithRecoveryCode, delete-button.test.js | - |
| Board note: Primary address is changed from a menu on the verified row | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Delete requires typing the username and, with 2FA on, a code | done | the delete account row above | - |
| Board note: Resend is rate limited and says when you can try again | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Left menu is the only navigation for settings: no tabs repeat these pages | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, unverified address, address added toast, delete confirmation dialog (typed usern... | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Export requested and ready states | unverified | not tracked in the gap files | needs a row and a test |

#### UserSettingsAppearance (ASC Hub settings: appearance)

Area shell/settings. Rows 8: done 8, partial 0, missing 0. Copy found in the hub: 68% of 63 copy strings (mobile twin 58%). Template(s): appearance.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Make ASC Hub comfortable. Changes apply immediately on every device you are sig...; Choose how ASC Hub looks. System follows your device and switches at sunset.; Small switches that change how the interface feels.; Agent runs, the running-agents chip and Ask an agent. Off hides them everywhere....

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Theme cards (light, dark, system) with preview | done | templates/user/settings/appearance.tmpl (file) | - |
| Density (comfortable, compact) | done | templates/base/head_style.tmpl:4 | - |
| Language select; hints switch; hidden comment types | done | templates/user/settings/appearance.tmpl:112 (best match) | - |
| Show agent features switch on this page (exists on /-/asc/agents only) | done | templates/user/settings/appearance.tmpl:59 (best match) | - |
| Reduce motion and Keyboard shortcuts switches | done | web_src/css/asc-ds/base.css:249 | - |
| Hide resolved comments switch | done | user setting `asc.hide_resolved` (default on = as before): switch on the Appearance page (`POST /user/settings/appearance/hide_resolved`), read in `routers/web/repo/issue.go` prepareHiddenCommentType and applied by ... | - |
| Date and time format (12/24 h, order) | done | models/user/asc_prefs.go AscTimeFormat, templates/user/settings/appearance.tmpl, web_src/js/features/local-time.js | - |
| Apply instantly with an undo toast (no Save step); an app-wide toast/undo component does not e... | done | web_src/js/features/ds-toast.js (file) | - |

#### UserSettingsNotifications (ASC Hub settings: notifications)

Area shell/settings. Rows 6: done 6, partial 0, missing 0. Copy found in the hub: 78% of 74 copy strings (mobile twin 78%). Template(s): notification_subscriptions.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Choose a channel for each kind of event. Needs-you items always land in the inb...; Rules for repos you can write to; Digest and rate limits.; Right away, at most one per 5 minutes per thread.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Page itself (not in the settings menu; only the Forgejo email preference on Account and /notif... | done | templates/user/settings/notifications.tmpl; the Notifications entry of templates/user/settings/navbar.tmpl | - |
| Event x channel matrix (Inbox/Email/Push for review requested, mention, agent question, run fi... | done | hub: services/ascnotify (per-event email and push choices in the user setting asc.notify), routers/private/asc_event.go (POST /api/internal/asc/event records and sends each event asc serve reports), notifications.tmpl (inbox locked on for every event); asc: crates/asc-serve/src/notify sends all eight events; TestNotificationMatrix, routers/private/asc_event_test.go | - |
| Email frequency (right away / per-thread limit) and daily summary at a time | done | see where-cell | - |
| Quiet hours (from/until, time zone aware) | done | see where-cell | - |
| Push channel, permission prompt and denied state | done | see where-cell | - |
| Watching table with level and last notified, Unwatch with undo | done | templates/user/notification/notification_subscriptions.tmpl:13 (best match) | - |

#### UserSettingsOrgs (ASC Hub settings: organizations)

Area shell/settings. Rows 3: done 3, partial 0, missing 0. Copy found in the hub: 75% of 52 copy strings (mobile twin 68%). Template(s): organization.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Teams you belong to and invitations waiting for you.; Where you are a member or owner.; Rail Guild; Join with one click..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Organization rows with role chip, "38 people - 14 repositories", Settings for owners | done | templates/user/settings/organization.tmpl:1 | - |
| Pending invitations with Accept and Decline | done | templates/user/settings/organization.tmpl (Invitations card: inviter, role, age, Decline, Accept); routers/web/user/setting/asc_org_invites.go (orgInviteRows, inviteRole, DeclineOrgInvite) | - |
| Last owner cannot leave (explains transfer first) | done | an owner leaves from the People page, which refuses the last owner (routers/web/org/people.go) | - |
| Board note: Leave asks to confirm; the last owner must transfer first | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Left menu is the only navigation for settings: no tabs repeat these pages | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, pending invitation, empty (illustration + New organization), last owner cannot l... | unverified | not tracked in the gap files | needs a row and a test |

#### UserSettingsPrivacy (ASC Hub settings: privacy)

Area shell/settings. Rows 5: done 5, partial 0, missing 0. Copy found in the hub: 75% of 57 copy strings (mobile twin 70%). Template(s): blocked_users.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Agents in your organizations can see your name, role and focus.; Casey Brandt; promo-bot-77; @promo77.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Page itself; Show activity and Hide email switches live on Profile instead | done | see where-cell | - |
| Show when I am online (green presence dot) | done | see where-cell | - |
| Allow agents to read my profile | done | privacy.tmpl switch (sp_agents_profile) over models/user/asc_agent_profile.go (default yes, no when the choice cannot be read); the hub API hides the profile from Actions task tokens (services/convert/user.go); asc serve Agents.GetPerson asks the hub on every call through `POST /api/internal/asc/agent-profile` (routers/private/asc_agent_profile.go) and audits agent.profile.read; tests asc_agent_profile_test.go (convert, private), an_agent_gets_the_profile_of_a_person_only_when_the_hub_says_the_person_allows_it | - |
| Blocked people list, block by username, unblock | done | templates/user/settings/blocked_users.tmpl (file) | - |
| Block confirm dialog, 10 s undo on unblock; blocking also drops pending review requests (only... | done | `web_src/js/features/asc-confirm.js` (data-confirm-modal: the hub's styled confirm modal `block-user-modal` in `privacy.tmpl` opens before the block form is sent, also for the Enter key); Unblock redirects to the ... | - |
| Board note: Unblock asks no questions and is undoable for 10 s | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Blocking someone removes their follows and pending review requests | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Left menu is the only navigation for settings: no tabs repeat these pages | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, empty blocked list, block confirmation dialog, username not found | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Switches save immediately with an undo toast | unverified | not tracked in the gap files | needs a row and a test |

#### UserSettingsProfile (ASC Hub settings: profile)

Area shell/settings. Rows 8: done 8, partial 0, missing 0. Copy found in the hub: 79% of 75 copy strings (mobile twin 78%). Template(s): profile.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; PNG, JPG or SVG, up to 1 MB. Shown at 24 to 96 px.; This is what people find when they look you up.; Changing it redirects the old profile address for 90 days.; (UTC+00:00) Lisbon.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Photo upload and remove (initials placeholder) | done | templates/user/settings/profile.tmpl (file) | - |
| Photo preview before saving; upload-failed error text | done | web_src/js/features/asc-ui.js (file) | - |
| Display name, username (redirect hint), bio, pronouns, location, website, visibility radio | done | templates/user/settings/profile.tmpl:53 (best match) | - |
| Username availability check after 400 ms of no typing | done | `routers/web/user/setting/asc_username.go` UsernameCheck (`GET /user/settings/username_check?name=`: own name, free, taken without case, reserved or bad pattern, rename not allowed, cooldown of someone else's old ... | - |
| "How others see you" live preview card | done | see where-cell | - |
| Public email select (verified addresses), time zone select | done | `templates/user/settings/profile.tmpl` public email and time zone selects. The zone is the person's for dates: every page of a signed-in person carries it in `<html data-tz>` (`services/context` HTML, ... | - |
| Save bar that turns active on edit, names the change count, Discard restores, "Saved" confirma... | done | web_src/js/features/repo-settings.js:10 | - |
| Loading skeleton inputs | done | web_src/js/features/ds-settings-loading.js (a link of the settings menu shows skeleton inputs until the next page loads); tests ds-settings-loading.test.js, TestSettingsPagesCarryTheLoadingWords | - |

#### UserSettingsRepos (ASC Hub settings: repositories)

Area shell/settings. Rows 2: done 2, partial 0, missing 0. Copy found in the hub: 74% of 53 copy strings (mobile twin 65%). Template(s): repos.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Repositories you own and ones that need adopting.; Move, rename or delete from each repository's own settings.; Shell and editor setup.; garage-eu.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Owned repositories list, unadopted adopt/delete | done | templates/user/settings/repos.tmpl:47 | - |
| Stack count and storage target per row; Transfer dialog (new owner + repository name) | done | hub: issues_model.CountAscOpenStacks (open pull requests that are not the stack parent of another open one, as the Stacks page counts), byoweb.LoadRepoStorage (asc Storage.ListRepoPlacements), templates/user/settings/repos.tmpl (letter tile, Public or Private, description, "N stacks", storage, Transfer dialog posting to the repository's own transfer; Transfer pending links to it); TestCountAscOpenStacks, TestUserSettingsReposOwned | - |
| Board note: Transfer asks for the new owner and the repository name | done | TestUserSettingsReposOwned | - |
| Board note: Left menu is the only navigation for settings: no tabs repeat these pages | done | repos.tmpl has no tabs; the settings menu is the layout's | - |
| Board note: Default, empty unadopted, transfer dialog | done | TestUserSettingsReposOwned (default and transfer), TestUserSettingsReposUnadopted (empty and one directory) | - |

#### UserSettingsStorage (ASC Hub settings: storage)

Area shell/settings. Rows 6: done 6, partial 0, missing 0. Copy found in the hub: 71% of 108 copy strings (mobile twin 68%). Template(s): asc_storage.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Keep your repository objects in an S3-compatible bucket that you own.; Keep your repository objects in a bucket you own. ASC writes a probe object, re...; S3-compatible buckets you own: Garage, MinIO, Backblaze B2, Cloudflare R2 or Am...; garage-eu.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Targets list and state chips, add form (name, region, endpoint, bucket, prefix, addressing, ke... | done | routers/web/shared/byoweb/storage.go:101 | - |
| Verify handshake: five steps with latency, failure message in words | done | see where-cell | - |
| Per repository assignment ("where new objects go") | done | see where-cell | - |
| Migration progress (copied/total) and cancel | done | web_src/js/features/asc-byo.js:5 | - |
| Migration Pause/resume, bytes and ETA, mismatch count, paused state | done | asc: Storage.PauseMigration/ResumeMigration, StorageMigration.paused, bytes_total and mismatched (the check counts the copies it redid); hub: bytes moved, the time left (estimated in the hub from the pace so far) and the redone copies in byo_storage_repo.tmpl, kept current by asc-byo.js; TestMigrationETA, TestRepoStorageMigrationBytesAndETA | - |
| Card text "Used by N repositories - verified 2 days ago" | done | templates/shared/asc/byo_storage_cards.tmpl | - |

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

Area repo. Rows 14: done 14, partial 0, missing 0. Copy found in the hub: 67% of 81 copy strings (mobile twin 66%). Template(s): commit_page.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: main@c8a02f1; Browse files; Spread retries so a provider outage does not turn into a synchronized stampede...; The cap on line 23 and the new bounds test. Everything else is mechanical.; , attempt: u32) ->; .base * 2u32..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Page shell with breadcrumb History / id, title, Landed chip, Browse files, Copy id | done | templates/repo/commit_page.tmpl (file) | - |
| "Why this exists" (intent) block | done | `H:templates/repo/change_facts.tmpl` (ChangeIntent from the message body of the change, `routers/web/repo/asc_change_facts.go`); tests: asc_change_facts_test.go | - |
| "Where to look" and "Risk" lines | done | routers/web/repo/asc_change_brief.go, templates/repo/change_facts.tmpl | - |
| Facts strip: Size, Files, Checks 14 of 14, Risk | done | templates/repo/commit_header.tmpl (file) | - |
| Stack position bar: previous / next change, "Change 2 in retry-with-backoff" | done | templates/repo/commit_header.tmpl:23 | the where-cell cites repo.walk_commits, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Files grouped by meaning (Behaviour, Tests, Generated) with counts, collapsible groups, genera... | done | `H:routers/web/repo/asc_change_files.go` prepareAscChangeFiles (Repo.Classify of the change against its parent): groups Behaviour, Tests, Docs and comments, Renames and formatting, Generated with counts and +/-, ... | - |
| Collapsed file buttons with aria-expanded, +/- signs | done | templates/repo/diff/box.tmpl:15 | - |
| Review block "2 of 2" with approvers and dates | done | models/issues/review.go:96 | - |
| Checks list with durations | done | templates/repo/commit_statuses.tmpl (file) | - |
| "Where it is now": On main, In release v2.18.0, Stack and position | done | see where-cell | - |
| Revert as a new change (review required) | done | see where-cell | - |
| Undo from the operation log while recent (replaces Revert) | done | `H:routers/web/repo/asc_change_undo.go`: on a landed change that is still the newest on the default branch, the rail offers "Undo from the operation log" (`/asc/ops/undo` with its seq) in place of the revert; once ... | - |
| Large change state: "412 files changed, showing the 20 that matter most", Show 392 more | done | same file: over 40 files the diff shows the 20 that matter most (Behaviour first, then Tests, Docs, Renames, Generated; larger first), "N files changed, showing the 20 that matter most" and "Show N more" (`?all=1`); ... | - |
| Loading diff state | done | `H:templates/repo/diff/box.tmpl`: the per-file skeleton and Retry now show on the change (commit) page too, not only on a pull request; a failed load offers Retry (`repo-diff.js`); `asc_review_files_test.go` | - |

#### Code (ASC Hub code)

Area repo. Rows 21: done 21, partial 0, missing 0. Copy found in the hub: 61% of 112 copy strings (mobile twin 62%). Template(s): asc_symbols.tmpl, blame.tmpl, view_file.tmpl. Mobile twin: yes.

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
| Shortcuts b toggle blame, y permalink, l go to line, e propose change, t find file, [ collapse... | done | `H:templates/repo/code_keys.tmpl` lists the keys of the view; `asc-goto-line.js` adds `l` (a dialog that goes to and marks the line) next to the `t`, `b`, `y`, `e` and `[` bindings; `asc_code_keys_test.go`, ... | - |
| File tree with aria-current and "N files touched by open stacks" badges per file | done | `H:routers/web/repo/view_asc_tree_stacks.go`: open pull requests onto the shown branch (at most 50, newest first), their changed paths cached per head, a count per file titled "Touched by open stacks" and the line ... | - |
| Tree collapse with [ and Files sheet on mobile | done | templates/repo/code_view.tmpl:10 | - |
| Outline panel (struct / impl / fn with line numbers) | done | routers/web/repo/asc_symbols.go:35 | the where-cell cites GET /{owner}/{repo}/asc/symbols?op=outline&path=, not in the tree (renamed or stale text); behaviour taken f... |
| "Used in / 4 references" code-intelligence panel on the file | done | services/ascsearch/panel.go (file) | - |
| "Open changes touching this file" card (stack, change 3 of 4, lines changed) | done | templates/repo/blame.tmpl:1 (best match) | the where-cell cites H:services/pull, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Shortcuts legend card | done | templates/repo/code_view.tmpl (file) | - |
| Propose change bar ("Changes go into a new change on top of main"), Start a new change / Add t... | done | `H:routers/web/repo/view_asc_my_stacks.go`; Start a new change opens the editor on a new branch (a new stack); Add to my stack opens it on the head of the person's own open stack onto the shown branch (a menu when ... | - |
| Directory view columns: Last change, In open stack chip, Author, Size, Updated | done | templates/repo/view_list.tmpl:1 | - |
| Loading state (lines stream in) | done | `H:templates/repo/code_loading.tmpl`: while the next file, view or tree link loads, lines of code of different lengths take the place of the file (`asc-page-loading.js`, reset on back); `asc_code_keys_test.go`, ... | - |
| Not found at this version ("It was added in change mzu2d. You are looking at main@8d1c7e0.", G... | done | `H:routers/web/repo/view_asc_not_found.go` on the 404 of `HandleGitError`: names the change of the default branch that added the path (or removed it, or says main never had it), the version being looked at, Go to ... | - |
| Binary preview card | done | templates/repo/view_file.tmpl (file) | - |
| Long-press line number to select and Share (mobile), blame as avatar column with change sheet | done | `repo/blame.tmpl`: under 1024px the avatar is a button that opens the change in a bottom sheet, and a long press on a line number selects it (or extends the selection) and opens Share, Copy permalink and Reference in ... | - |
| Mobile: Avatar column for blame, tap opens the change sheet (swipe down closes), Files sheet, long-pre... | done | `blame.tmpl` + `asc-blame-sheet.js` (row 103); drag-down close in `asc-sheet.js`; the Files sheet is the `rp-tree` panel | - |

#### Compare (ASC Hub compare)

Area repo. Rows 6: done 6, partial 0, missing 0. Copy found in the hub: 72% of 50 copy strings (mobile twin 71%). Template(s): asc_compare.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Only what the stack adds; . The stack restacks onto main when you land it.; Ahead of main; Only on main; - Diego Alvarez; - Diego Alvarez.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| From / To, swap, "only what the stack adds" checkbox, ahead / behind chips, common ancestor | done | `H:templates/repo/asc_compare.tmpl`: From / To selects, swap, ahead / behind chips and the common ancestor; "Only what the stack adds" is the three-dot compare (from the common ancestor) with a hint; ... | - |
| From / To as labelled selects listing shared refs, stacks and tags (text inputs today) | done | templates/repo/asc_compare.tmpl (file) | - |
| Per-change and per-file +/- counts and avatars | done | see where-cell | - |
| Same version state ("Both sides are main@a41f9c2") | done | templates/repo/asc_compare.tmpl (file) | - |
| Unknown ref with "Did you mean retry-with-backoff?" | done | `H:routers/web/repo/asc_compare_hints.go` suggests the nearest ref name for an unknown one | - |
| Open any change or file from the lists, both sides kept in the address | done | see where-cell | - |

#### Dependencies (ASC Hub dependencies)

Area repo. Rows 14: done 14, partial 0, missing 0. Copy found in the hub: 51% of 132 copy strings (mobile twin 46%). Template(s): asc_deps.tmpl, asc_deps_plan.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: What this repository stands on; Refresh advisory data; Findings by severity; Complete, 3 of 3 sources; Critical 0; Medium 1.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Views Findings / Updates / Configuration (?view=) | done | templates/repo/asc_deps.tmpl (file) | - |
| Scan now, refresh advisory data, last-scan line, lookup-incomplete banner | done | templates/repo/asc_deps.tmpl:38 | - |
| Summary tiles: findings by severity bar chart with text table, Outdated 11 (3 with security fi... | done | `DepsService.ListFindings/ListUpdates` give the counts; add tile row to `asc_deps.tmpl` | - |
| Severity chips All / Critical / High / Medium / Low with counts, Show dismissed | done | see where-cell | - |
| Finding row expands (aria-expanded): "Why it matters", "How it gets in" text tree, Propose fix | done | rows are flat in `asc_deps.tmpl` (path is one line); render the path as a tree and add per-finding Propose fix posting to `/asc/deps/propose` | - |
| Dismiss with required reason and note, Reopen, "until Dec 1" expiry | done | templates/repo/asc_deps.tmpl:93 (best match) | - |
| Updates table: package, current, latest, kind, security, proposal link (Change kq2nv) or Propo... | done | templates/repo/asc_deps.tmpl:147 | - |
| Propose updates panel: Security fixes only, Largest jump, Most proposals, 5-stack limit, Previ... | done | templates/repo/asc_deps.tmpl:170 | - |
| Configuration view: ecosystems, schedule ("Weekly, Mondays 06:00 UTC, next run in 4 days"), th... | done | templates/repo/asc_deps.tmpl:219 | the where-cell cites .asc/deps.toml, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Scan running state with real progress bar ("About 20 seconds left") | done | `DepsService.ScanStatus` (`A:proto/asc/v1/deps.proto`, `A:crates/asc-serve/src/deps/progress.rs`) says the phase, the advisory steps done of total, a bar in thousandths and an estimate; the hub polls ... | - |
| No findings state ("Checked 214 packages 2 hours ago against 3 sources") | done | `asc_deps.tmpl` prints a short empty line | - |
| Never scanned state with package count | done | templates/repo/asc_deps.tmpl:33 (best match) | - |
| Unsupported ecosystem ("2 files are not scanned. tools/build.zig.zon") | done | see where-cell | - |
| Advisory text plain, https-only links | done | templates/repo/asc_deps.tmpl:5 (best match) | the where-cell cites H:services/asctrust, not in the tree (renamed or stale text); behaviour taken from the gap file |

#### GitNote (ASC Hub Git compatibility note)

Area repo. Rows 4: done 4, partial 0, missing 0. Copy found in the hub: 61% of 56 copy strings (mobile twin 79%). Template(s): asc_branchless.tmpl, clone_asc_tabs.tmpl, clone_buttons.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Collapsed (default); Bridge switched off by an admin; Inline, repository home (hero card); Put the asc command first, in its own block; Keep the note to one line when collapsed; Say what Git sees: a read-write view of the same changes.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| One-line collapsed disclosure after the asc command, aria-expanded, URL shown only after expan... | done | templates/repo/clone_asc_tabs.tmpl:1 | - |
| Body copy: "A push arrives as changes and goes through policy... Stacks appear as branches nam... | done | see where-cell | - |
| Bridge switched off by admin: one sentence "The Git remote is off on this hub. Everything work... | done | templates/repo/clone_asc_tabs.tmpl:1 | - |
| Inline variant in the repository home hero card; also in settings Working mode when branches a... | done | `home_dir.tmpl`, `settings/asc_branchless.tmpl`, `empty.tmpl` | - |
| Board note: The whole row is one disclosure button; Enter and Space open it | unverified | not tracked in the gap files | needs a row and a test |
| Board note: The URL is copyable only after expanding | unverified | not tracked in the gap files | needs a row and a test |
| Board note: When the admin switches the Git remote off the row says so in a single sentence and has n... | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Collapsed, expanded, switched off and inline | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Dark and light through the Theme tweak | unverified | not tracked in the gap files | needs a row and a test |

#### History (ASC Hub history and graph)

Area repo. Rows 16: done 16, partial 0, missing 0. Copy found in the hub: 57% of 88 copy strings (mobile twin 70%). Template(s): asc_graph.tmpl, asc_ops.tmpl, commits.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Filter by message, id or author; Open on top of main; writing now; Needs review; Diego Alvarez; In the merge queue, lands next.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| List view with commit rows, copy hash, view at point, search by message and author, "Open oper... | done | templates/repo/commits.tmpl (file) | - |
| Lane graph from Repo.CommitGraph (colour, lanes, refs, review chip) | done | routers/web/repo/asc_graph.go (file) | - |
| Graph and List as two views of one page with the same rows; graph beside the rows | done | `commits.tmpl`: the List / Graph segment switches `?view=graph` on the same page; the graph draws the lines between the page's rows beside them (`asc_history_graph.go`, any backend), and "All branches" opens the full ... | - |
| Node state shape plus chip (Needs review, Queued, Agent writing); agent vs person nodes; stack... | done | open rows: a shape per state (dashed agent circle writing, hollow warn needs review, ok circle with a check queued, filled ok approved, err bar changes asked, dashed grey draft) plus a chip; landed agent changes get ... | - |
| "Open on top of main" section listing open changes with stack position, "Waiting on Priya Rama... | done | `prepareAscOpenChanges`: up to 10 open pulls onto the branch on page 1 with no filter, size (+/-), author, change id, "stack k of n", and a status line (agent run and minutes for people who may read runs, merge queue ... | - |
| Comfy / Compact density switch | done | templates/repo/commits.tmpl:16 | - |
| Filter bar: Everyone / People / Agents, Path, Stack, date range, in the URL, sticky | done | author kind needs agent identity; path and date filters exist in Forgejo `RefCommits` partly; `commits.tmpl`, `routers/web/repo/commit.go` | - |
| Export | done | routers/web/repo/commit.go (file) | - |
| Keyboard: j/k move, Enter opens, c copies id | done | see where-cell | the where-cell cites H:web_src/js, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Row hover actions (copy id, browse files) and hover lift | done | templates/repo/commits_list.tmpl (file) | - |
| "Landing rate, last 12 weeks" card with this-week delta | done | routers/web/repo/commits_landing.go, templates/repo/commits.tmpl | Hub only: one git log per week on the viewed ref (TestAscHistoryLandingRate) |
| Graph key legend | done | templates/repo/asc_graph.tmpl (file) | - |
| "Show 25 older changes" on-demand load (44 px button on mobile) | done | see where-cell | - |
| States: loading skeleton, no results with explanation and Clear filters, partial-data banner w... | done | `H:templates/repo/commits_table.tmpl`: a skeleton while the next page, filter or search loads (`commits_loading.tmpl`); no results under the people / agents filter says so and offers Clear filters; the partial banner ... | - |
| Mobile rail-and-node list, chips open a bottom sheet for Path, Stack, date | done | `commits.tmpl`: under 640px a chip row (who, Path, Stack, Date) replaces the bar; each chip opens its part of one bottom sheet (`asc-history-sheet.js`); Date searches with `after:` / `before:` and keeps the other ... | - |
| Mobile: Rail-and-node list, chips open a bottom sheet, 44 px older-changes button | done | rows 130, 140 and 142 | - |

#### Insights (ASC Hub insights: Pulse)

Area repo. Rows 7: done 7, partial 0, missing 0. Copy found in the hub: 76% of 71 copy strings (mobile twin 75%). Template(s): contributors.tmpl, pulse.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: +12% on the previous 30 days; Oct 1 to Oct 30, main; People 68; Agents 80; Median time to the first review, by size of change.; provider-timeouts.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Pulse and Contributors pages exist (Forgejo activity, git contribution graph) | done | templates/repo/pulse.tmpl (file) | - |
| Four tiles: Changes landed (vs previous 30 days), Stacks opened, Median review time, Written b... | done | templates/repo/insights_asc.tmpl (ins_landed, ins_opened, ins_median, ins_agents); routers/web/repo... | Insights service exists in asc master and the hub. |
| Changes landed per day, people vs agents, chart with direct labels, Table toggle | done | templates/repo/insights_asc.tmpl:51 svg chart, table toggle | - |
| Seg controls Everyone / People / Agents and 7 days / 30 days / Year that update chart and tile... | done | templates/repo/insights_asc.tmpl:10-15 ins_who, ins_range segmented controls | - |
| Median review time by week ("Getting faster") and by size of change (Under 50 / 50 to 300 / Ov... | done | templates/repo/insights_asc.tmpl:76-86 by week, by size of change | - |
| Recently landed stacks and "Waiting the longest" list | done | see where-cell | - |
| States: new repository, loading, "Index behind" banner | done | `pulse.tmpl` | - |

#### InsightsPeople (ASC Hub insights: Contributors)

Area repo. Rows 5: done 5, partial 0, missing 0. Copy found in the hub: 69% of 68 copy strings (mobile twin 75%). Template(s): contributors.tmpl, pulse.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: People and agents who changed this repository, side by side and labelled.; Changes on main, last 30 days; Diego Alvarez; Hannah Weiss; Every agent change was approved by a person before landing. Median wait for tha...; People who reviewed the most. Agents never review..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Contributors table: landed, reviews given, median review, 12-week sparkline, share, role, "Age... | done | templates/repo/insights_people.tmpl (people_weeks, run_by, people_share) | - |
| Who writes the code (46/54) with "every agent change approved by a person" sentence | done | templates/repo/insights_people.tmpl (who_writes, who_split, ins_all_approved) | - |
| Review load list ("Agents never review") | done | templates/repo/insights_people.tmpl:58 review_load, agents_never_review | - |
| Rows open the person or the agent run | done | links in new template | - |
| Empty, loading, partial-data states | done | new template;; Audit 2026-10-30 (part built): Empty state is there; loading and partial-data states were not found. | - |
| Board note: Seg controls change who and period; the chart and tiles update together | unverified | not tracked in the gap files | needs a row and a test |
| Board note: The Table button shows the same numbers as a table | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Rows open the person or the agent run | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Pulse: four tiles, daily landings by people and agents, review time trend, recent and wai... | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Contributors: table with share and 12-week activity, review load, who writes the code; em... | unverified | not tracked in the gap files | needs a row and a test |

#### IssueView (ASC Hub issue)

Area repo. Rows 5: done 5, partial 0, missing 0. Copy found in the hub: 69% of 54 copy strings (mobile twin 69%). Template(s): view.tmpl, view_content.tmpl, view_title.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Retries stampede the provider after an outage; Agreed. Keep the ceiling in config and add a metric for exhausted retries so we...; Leave a comment. Markdown works; type @ to mention a person or an agent.; Due Nov 14; Linked stacks.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Timeline, comment box, close/reopen, assignees, labels, milestone, participants, lock | done | templates/repo/issue/view_content.tmpl:10 (best match) | - |
| Agent chip on comments and assignees; agents cannot approve or merge | done | templates/shared/user/authorlink.tmpl, templates/repo/issue/view_content/sidebar/assignees.tmpl, models/issues/review.go (ErrAgentMayNotApprove), services/pull/merge.go (IsUserAllowedToMerge) | - |
| Linked stacks rail ("1 of 4 landed") and timeline event "coder-2 opened stack" | done | Linked stacks card in the sidebar of an issue (`templates/repo/issue/view_content/sidebar.tmpl`): the stacks that name the issue with "1 of 4 landed", each entry with its state, and a note when the stack closes the ... | - |
| Closed by a landed stack banner with Reopen | done | Banner above the timeline of a closed issue that a landed stack fixed (`AscClosingStackLanded`: the issue is closed, a pull request of a stack names it with a closing word, and that pull request has merged), with a ... | - |
| Comment draft kept; Cmd+Enter posts | done | templates/repo/issue/view_content.tmpl:23 (best match) | - |
| Board note: Comment box keeps a draft; Cmd+Enter posts | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Agent comments carry an Agent chip and cannot approve or merge | done | TestAuthorLinkMarksAgents, TestAssigneesMarkAgents, TestSubmitReviewRefusesAnAgentApproval, TestAnAgentIsNotAllowedToMerge | - |
| Board note: Linked stack shows the rail; landing closes the issue with an undo | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Open issue with discussion, agent comment, linked stack event; closed by stack; locked; l... | unverified | not tracked in the gap files | needs a row and a test |

#### Issues (ASC Hub issues)

Area repo. Rows 6: done 6, partial 0, missing 0. Copy found in the hub: 59% of 71 copy strings (mobile twin 57%). Template(s): filters.tmpl, labels.tmpl, list.tmpl, milestones.tmpl, new.tmpl, search.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Retries stampede the provider after an outage; Document the failover order for card providers; - opened Yesterday by Hannah Weiss; Webhook retries should back off per receiver; Timeout in sandbox gateway test is flaky.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| List with filters, sort, open/closed counts, label rail, milestone rail | done | templates/repo/issue/list.tmpl:110 | - |
| Search accepts label: and author: filters and chips apply the same | done | `routers/web/repo/asc_issue_search.go` (parseIssueQuery takes `label:name`, `label:"two words"` and `author:login` out of the search box; the label ids and the poster feed the same variables as the label and author ... | - |
| Row chips "Fixed by stack X", linked stack with state, "Quarantined by" flaky chip | done | routers/web/repo/asc_issue_stacks.go, routers/web/repo/asc_issue_quarantine.go, services/asctrust/issuegroups.go, templates/shared/issuelist.tmpl | - |
| Linking a stack closes the issue when it lands, with undo | done | Checked in the code and now covered by a test: every way the hub learns of a landed pull request runs `handleCloseCrossReferences` (the issues named with a closing word are closed): the stack merge through asc ... | - |
| New issue as a dialog that keeps a draft ("Save as draft") | done | repo/issue/new_dialog.tmpl on the issue list (repo-issue-dialog.js); title and body kept as a server draft (POST issues/new/draft) shared with the new issue page; Save as draft, Discard, Open full form | - |
| No results with "Clear filters", loading skeleton | done | templates/shared/issuelist.tmpl, web_src/js/features/issue-list-loading.js, TestIssueListNoResults | - |

#### NewRepo (ASC Hub new repository)

Area repo. Rows 15: done 15, partial 0, missing 0. Copy found in the hub: 65% of 91 copy strings (mobile twin 61%). Template(s): create.tmpl, create_advanced.tmpl, create_basic.tmpl, create_from_template.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Second-generation payment service; Import brings history, refs and tags from another server. It never changes the...; Only people and agents you add can see it.; Everyone signed in to this hub can read it.; Anyone can read it. Only collaborators can change it.; Working mode.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Page shell, owner select, name, description, auto-init options (Forgejo flow in design-system... | done | templates/repo/create*.tmpl (not found in tree) | the where-cell cites H:templates/repo/create*.tmpl, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Name availability check as you type (300 ms), tick and inline "acme/ledger already exists" | done | `H:routers/web/repo/asc_newrepo.go` CheckName (`GET /repo/check_name?uid=&name=`: free, taken as "owner/name already exists", reserved or bad characters), `H:web_src/js/features/repo-new.js` (300 ms, text only, ... | - |
| "Start from" segment: Empty / Template / Import that keeps typed values | done | `H:templates/repo/create_start.tmpl` (Empty / Template / Import, `aria-pressed`) and `create_import.tmpl` (one link per importer, hidden when migrations are off); `repo-new.js` keeps the owner, name, description and ... | - |
| Visibility as three radio cards: Private, Internal, Public | done | `H:models/repo/repo.go` has `IsPrivate` only; asc already returns `visibility` "private / internal / public" in `RepoConfig`; add an internal level (signed-in users read) in models, permission checks and the form | - |
| Working mode radio: Branchless (recommended) / With branches at creation | done | asc: `RepoAdmin.set_branchless` (new op 9 of RepoAdminRequest in `proto/asc/v1/repo.proto`; repo admin, x-asc-op-id, audited as `repo.branchless`; asc refuses to strand a ref: branchless on only when every branch is ... | - |
| Add AGENTS.md switch | done | `H:templates/repo/create_asc.tmpl`, `H:services/repository/asc_init_files.go` (AgentsFileText; a first commit even without the README switch), `forms.CreateRepoForm.AgentsFile`; checked on a live asc: the file is in ... | - |
| Add a policy file switch (.asc/policy.toml, 2 approvals, picked checks) | done | `H:templates/repo/create_asc.tmpl` (approvals 0 to 5, default 2, required checks by name), `H:services/repository/asc_init_files.go` (PolicyFileText: `version = 1`, you as maintainer, `[trunk]` approvals and checks; ... | - |
| License choice | done | templates/repo/create_init.tmpl (file) | - |
| Preview card ("acme / payments-v2, Private, Policy on") and "Then, from your terminal" commands | done | `H:templates/repo/create_preview.tmpl`: owner / name, visibility and policy follow the form (`repo-new.js`), then the terminal commands; `repo-new.test.js` | - |
| Quota line "41 of 100 used on the Team plan" and quota-reached state | done | `H:routers/web/repo/asc_newrepo.go` prepareAscNewRepo: "N of M repositories used." and "The limit is reached." from the owner's repository count and limit (shown only when there is a limit). The hub has no plans, so ... | - |
| Creating state ("Creating acme/payments-v2 and its first policy...") | done | `repo-new.js`: on submit the button is disabled and says "Creating owner/name…" (or "…and its first policy"); `repo-new.test.js` | - |
| Import running: progress "3,412 of 9,880 history entries", Run in background, Cancel | done | services/repository/migrate_asc.go (reportImportProgress), routers/web/user/task.go, web_src/js/features/repo-migrate.js | - |
| "Signed with your key mo_41c9" line | done | `H:routers/web/repo/asc_newrepo.go` prepareAscNewRepo reads Signing.ListKeys as the person (asctrust.LoadKeys): "Signed with your key <id>", or "You have no signing key yet" with a link to add one; left out when asc ... | - |
| Sticky primary action on mobile | done | templates/admin/runners/create.tmpl:18 | - |
| Mobile: Name check after 300 ms, Start from keeps typed values, sticky primary | done | `H:templates/repo/create.tmpl`, `web_src/js/features/repo-new.js`, `web_src/css/asc-ds/layout.css`, `asc-pages/repo-new.css` | - |

#### Refs (ASC Hub refs and tags)

Area repo. Rows 9: done 9, partial 0, missing 0. Copy found in the hub: 56% of 73 copy strings (mobile twin 58%). Template(s): list.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Shared refs and tags; Branchless repository: stacks are the unit of work, so there are only three sha...; Filter refs and tags; Shared 3; Stacks 3; These are the refs other people and tools can rely on. Nobody can rewrite them..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Branches page with filter, default branch card, ahead/behind, Protected chip, Compare | done | templates/repo/branch/list.tmpl:13 | - |
| One page with Shared refs, Stacks in progress and Tags sections | done | templates/admin/applications/list.tmpl (file) | - |
| Filter pills All / Shared 3 / Stacks 3 / Tags 14 | done | `H:templates/repo/branch/list.tmpl` pills All / Shared / Stacks / Tags with counts show one kind at a time (`asc-ui.js` `data-ref-filter`, `asc-ui.test.js`); found done in the tree | - |
| Branchless wording ("only three shared refs"), RepoConfig.shared_refs | done | a branchless asc repository (asc `RepoConfig`) titles the page "Shared refs and tags", says how many shared refs there are, and lists only the shared refs in one "Shared refs" section ... | - |
| Stack cleanup note ("clean themselves up 7 days after the last change lands") | done | `H:routers/web/repo/asc_ref_sections.go`, `H:templates/repo/branch/list.tmpl`, `H:services/asctrust/ops.go` (`PolicySummary.CleanupDays`); `A:crates/asc-serve/src/reftl.rs` (`LandedReaper`); ... | - |
| Tags section with release title, change count, author, date | done | `H:templates/repo/branch/ref_tags.tmpl`: the newest tags with release title, how many changes each adds to the tag before it (from the stored commit counts, `refTagsOf`), author and age; `asc_ref_tags_test.go` | - |
| Row More menu names its target; no delete on protected refs | done | templates/repo/branch/row.tmpl:53 | - |
| State: nothing matches with "Search stacks" link | done | templates/admin/applications/list.tmpl (file) | - |
| State: delete protected ref refused with "Open rule" | done | `H:routers/web/repo/asc_rule_link.go`: the refusal to delete a protected branch (`branch.go`) or tag (`release.go`) ends with "Open rule", a link to the matching rule, for a repository admin; `asc_rule_link_test.go` | - |

#### Releases (ASC Hub releases)

Area repo. Rows 15: done 15, partial 0, missing 0. Copy found in the hub: 57% of 115 copy strings (mobile twin 61%). Template(s): list.tmpl, new.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Stable 11; Pre-release 2; Most downloaded; main@a41f9c2; Signed with release key 7F3A...C21B; Written from the intent of each change; edit before publishing..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Timeline of release cards, tag, stable / pre-release / draft chips, Compare, edit, assets list... | done | templates/repo/release/list.tmpl:96 | - |
| Releases / Tags segment, search filter, feed | done | templates/repo/release_tag_header.tmpl:28 | - |
| Filter pills All 14 / Stable 11 / Pre-release 2 / Draft 1 and Sort (Newest, Most downloaded) | done | templates/repo/release_tag_header.tmpl:27 | - |
| Provenance strip ("Built by run #4218 from main@a41f9c2, 14 of 14 checks passed, signed with r... | done | `tag/verification_line.tmpl` gives the signature; add the building Actions run and check summary, link release to run (`H:models/actions`);; Audit 2026-10-30 (part built): Provenance line exists from Forgejo data; ... | - |
| Notes grouped Added / Changed / Fixed, "written from the intent of each change" | done | the New release dialog writes the notes from asc Releases.DraftNotes (`H:routers/web/repo/asc_release_draft.go`, `web_src/js/features/asc-release-new.js`); a release without notes shows notes grouped Added, Changed, ... | - |
| "N changes in this release" list with author, change id, category | done | each card counts its changes in the meta line and lists them newest first with the avatar, author, a category chip and the asc change id the commit landed as (the short commit id otherwise); chips All, Added, ... | - |
| Asset table: size, SHA-256 checksum (copy on click), downloads | done | `H:models/repo/attachment.go` has no checksum column; compute at upload; template shows name, count, age only; Audit 2026-10-30 (built): the SHA-256 of each asset is computed when the release is published and ... | - |
| Next release summary ("6 changes since v2.18.0", Draft v2.19.0 View) | done | templates/repo/branch/list.tmpl:105 (best match) | the where-cell cites Compare(latest tag, default branch), not in the tree (renamed or stale text); behaviour taken from the gap f... |
| "Download the latest" with asc release download v2.18.0 and per-platform line | done | right rail in `list.tmpl`; the `asc release` CLI command must exist (check `A:crates/asc/src/cli`) | - |
| Release health: cadence, rollbacks | done | the rail card shows the median gap between published releases, the rollbacks among the newest six (a release cut from a commit older than the commit of the release before it) and releases per week over 12 weeks ... | - |
| Get notified: Watch releases, Atom feed | done | see where-cell | - |
| New release dialog: proposes next tag, "Write from the N landed changes", assets drop zone, pr... | done | `H:templates/repo/release/new.tmpl` is a full page (no dialog, no generated notes, no tag proposal); Audit 2026-10-30: the data for it exists (GET /releases/asc_draft: proposed tag, notes written from the landed ... | - |
| States: no releases (shows asc release create v1.0.0), loading, storage error with Retry | done | the command does not exist in the CLI (see above); `list.tmpl` empty state has no command, loading skeleton or storage-error banner | - |
| Mobile: new release as bottom sheet, 44 px downloads | done | `new.tmpl` | - |
| Mobile: New release as a bottom sheet; channel chips | done | see Releases rows | - |

#### Repo (ASC Hub repository home)

Area repo. Rows 29: done 29, partial 0, missing 0. Copy found in the hub: 57% of 158 copy strings (mobile twin 60%). Template(s): asc_about.tmpl, clone_asc_tabs.tmpl, header.tmpl, home.tmpl. Mobile twin: yes.

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
| "Who writes the code" card: 46% people / 54% agents, "100% reviewed by a human", median review... | done | routers/web/repo/asc_insights_home.go, templates/repo/asc_about.tmpl | Existing Insights.GetStats; no new RPC (TestHomeAuthorsOf) |
| Policy card: Valid / invalid chip, rule summary, link to .asc/policy.toml | done | routers/web/repo/setting/asc_policy.go (file) | the where-cell cites .asc/policy.toml, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Policy card lines that need new policy keys (budget "20 min and 200k tokens", merge queue on) | done | `H:services/asctrust/ops.go` (`LoadPolicySummary`, `RunBudgetWords`), `H:templates/repo/asc_about.tmpl`; `asc_home_policy_test.go`, `ops_test.go` | - |
| Activity card: landed per week over 12 weeks, Contributors list with agent marks | done | routers/web/repo/view_home_cards.go, asc_insights_home.go, templates/repo/asc_about.tmpl | Weeks from git log (hub only); agent marks from the existing Insights.ListContributors (TestHomeContributorsOf) |
| Languages bar with legend and aria-label | done | templates/repo/asc_about.tmpl (file) | - |
| Latest release card ("9 changes, 3 by agents. Published 6 days ago") | done | templates/repo/asc_about.tmpl:106 | - |
| Loading skeleton that keeps row geometry | done | `H:templates/repo/view_list.tmpl`: while the last commit loads, the author and age cells hold `rp-sk-word` blocks of their usual width, so the columns keep their size; `routers/web/repo/asc_files_skeleton_test.go` | - |
| Error state: "Stacks did not load", request id, Try again | done | templates/repo/home_cards.tmpl:12 | Gap file says open; the template has the Stacks did not load error with request id and Try again. |
| Empty repository state with asc init and asc push commands | done | templates/repo/empty.tmpl (file) | - |
| Clone popover: asc login and clone, Install asc, Open in IDE, Copy | done | templates/repo/clone_dialog.tmpl (file) | - |
| Git compatibility note collapsed and expanded in the clone sheet | done | templates/repo/clone_asc_tabs.tmpl:1 | - |
| "Switch version" popover that lists shared refs, stacks and tags with labels | done | `H:routers/web/repo/asc_refs_menu.go` (`GET /{owner}/{repo}/asc/refs`: shared refs, open stacks and tags with their kind; in a branchless repository the shared refs are asc's `RepoConfig.shared_refs`), the asc mode ... | - |
| Pull to refresh and sticky Clone / New stack buttons (Mobile-Repo) | done | sticky buttons `rp-sticky-m` in `home_dir.tmpl`; pull to refresh by `data-asc-pull-refresh` on `.rp-body` and the `ds-ptr` arrow, run by `web_src/js/features/asc-pull-refresh.js` in the installed app | - |
| Clone card shown before first clone ("Work on it locally", asc stack new fix-refund-timeout) | done | `H:templates/repo/home_cards.tmpl` "Work on it locally" with the clone, create and submit commands to copy, shown to a person who may write code here and has sent no change yet; Hide posts `/asc/clone-card/hide`, ... | - |
| Mobile: Stack cards replacing the table; filter chips that snap; pull to refresh; sticky Clone and New... | done | below 640 px `stack_row.tmpl` rows wrap into cards with the rail under the title; the `rp-stack-filter` chips scroll and snap (`repo-home.css`); pull to refresh by `data-asc-pull-refresh` and `asc-pull-refresh.js`; ... | - |

#### RepoSettingsAI (ASC Hub repository settings: AI keys)

Area repo. Rows 6: done 6, partial 0, missing 0. Copy found in the hub: 59% of 106 copy strings (mobile twin 55%). Template(s): asc_ai.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Your keys, your providers, your caps.; Add a key once and choose where each feature uses it.; sk-ant-...c4fA; Agent runs, review summaries; Replace key; sk-proj-...9tLq.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Provider list (Anthropic, OpenAI, own endpoint) with key hint, added by, verified time, Replac... | done | services/ascbyo/aikeys.go:96 | the where-cell cites H:templates/shared/asc/byo_ai*.tmpl, not in the tree (renamed or stale text); behaviour taken from the gap f... |
| Replace key dialog that verifies before swapping and shows "Anthropic rejected this key (401)" | done | AiProviders.ReplaceKey tries the new key first and keeps the old one when the provider refuses; byoweb AIReplace and replaceRefusal name the provider ("Anthropic rejected this key (401 or 403)"); TestAIReplace | - |
| "What uses AI" table: Agent runs, Review summaries, Meaning search, each with its provider and... | done | AiProviders.ListFeatures/SetFeature; byo_ai_features.tmpl with the meters; meaning search runs on the asc server, so its row says "This server" and "No spend"; TestUserAIPageShowsFeatures | - |
| Cap reached: feature pauses with "until 1 Nov or you raise it" | done | ListAiFeatures paused, resumes_at, paused_by; byo_ai_features.tmpl | - |
| No keys: "AI features are off" empty state | done | see where-cell | - |
| Repository-level page (keys are owner scope in asc, page lives under the repository menu) | done | byo_ai_repo.tmpl at /settings/asc/ai; TestRepoAIKeysPage | - |

#### RepoSettingsAccess (ASC Hub repository settings: Access)

Area repo. Rows 8: done 8, partial 0, missing 0. Copy found in the hub: 65% of 110 copy strings (mobile twin 54%). Template(s): collaboration.tmpl, deploy_keys.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Who and what can work in this repository: people, teams, agents and deploy keys.; People, teams and agents; Who can open stacks here. What an agent may change is limited again by the repo...; Search people, teams or agents; Add to repository; All repository settings.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| One list of people, teams and agents with role select, last active, Show All / People / Agents | done | routers/web/repo/setting/asc_access_list.go; Maintain role in services/context/permission.go (RequireRepoSettings); tests TestAscAccessMaintain, TestAscAccessList | - |
| Combined search (people, teams, agents) with role chosen before adding | done | routers/web/repo/setting/asc_access_list.go (AscAccessSearch); collaboration.go CollaborationPost (role, kind) | - |
| Role select changes access at once with an Undo toast | done | routers/web/repo/setting/collaboration.go:134 | - |
| Agent rows: Agent chip, violet mark, "Limited by policy to src/ and tests/" | done | policy lookup per agent identity;; Audit 2026-10-30 (was marked done, part missing): Marked done; the text Limited by policy to ... was not found. | - |
| Remove agent confirm that says the running run stops and open changes stay with the owner | done | services/ascagentui/removeagent.go, routers/web/repo/setting/collaboration.go, templates/repo/settings/collaboration.tmpl | Existing Agents.ListRuns and CancelRun, no new RPC (TestAscAccessRemoveAgent) |
| Invite that already has access ("Priya Raman already has Maintain access") | done | routers/web/repo/setting/collaboration.go:86 | - |
| Deploy keys on the same page: title, fingerprint, read-only or read-write, added by, last used... | done | `/settings/collaboration` and `/settings/keys` both render the access page; `H:templates/repo/settings/deploy_keys_section.tmpl` with the Add dialog (`asc-deploy-key-add.js`); `deploy_key.added_by_id` (migration ... | - |
| No deploy keys empty state | done | see where-cell | - |

#### RepoSettingsGeneral (ASC Hub repository settings: General)

Area repo. Rows 9: done 9, partial 0, missing 0. Copy found in the hub: 65% of 115 copy strings (mobile twin 58%). Template(s): asc_branchless.tmpl, options.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Name, visibility, features and how this repository works.; How people find this repository.; Changing the name redirects the old address for 90 days.; One sentence, shown in search and on the home page.; Up to 20. Used by Explore.; failover.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Name, description, one save bar with "2 unsaved changes", Discard, Save | done | templates/repo/settings/options.tmpl (file) | - |
| Name rename note "redirects the old address for 90 days" and taken-name error | done | `H:models/repo/redirect.go` (`ExpiresUnix`, `RedirectKeepDays`, migration v18p), `H:templates/repo/settings/options.tmpl` hint and inline taken-name error, saved flash | - |
| Topics field in General (chips, up to 20, Add) | done | templates/org/settings/options.tmpl (file) | - |
| Visibility as radio cards (Private / Internal / Public) with confirmation when changing | done | `options.tmpl` uses a Private checkbox; Internal does not exist in the model (see NewRepo) | - |
| Features switches: Stacks and change review (locked, Required), Agent runs, Dependencies, Rele... | done | `H:models/repo/asc_feature.go`, `H:services/context/asc_features.go`, `H:routers/web/repo/setting/asc_features.go`, `H:templates/repo/settings/options.tmpl` | - |
| Working mode radio cards and shared refs (main, release/*), Cloud sync switch | done | templates/repo/settings/asc_branchless.tmpl (file) | - |
| Danger zone: Transfer, Archive, Delete with typed full name acme/payments and counts ("Removes... | done | templates/org/settings/options.tmpl:99 | - |
| "An owner can undo the delete from the operation log for 30 days" | done | see where-cell | - |
| Saving state, saved toast (5 s), unsaved-bar motion | done | `repo-settings.js` | - |

#### RepoSettingsPolicy (ASC Hub repository settings: Policy)

Area repo. Rows 12: done 12, partial 0, missing 0. Copy found in the hub: 75% of 142 copy strings (mobile twin 73%). Template(s): asc_policy.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: The rules every change and every agent run follows in this repository.; , so they are reviewed like code and travel with a fork. Approvals and checks a...; What a change needs before it can land on main.; People who must approve each change. Agents never count.; A change cannot land while any of these is failing or missing.; Authors may approve their own change.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| File header: .asc/policy.toml, Valid chip, "Last changed by Priya Raman, 5 days ago, in change... | done | `H:templates/repo/settings/asc_policy.tmpl` header: the Valid chip, the changeset it was read from and "Last changed by …, … ago, in change …" from the newest default-branch change touching `.asc/policy.toml` ... | - |
| TOML editor with Check (validate), diagnostics with line, Propose change | done | routers/web/repo/setting/asc_policy.go:77 | - |
| Seg Form / TOML / Changes (three views of one file) | done | templates/repo/settings/asc_policy.tmpl (file) | - |
| Form: approvals stepper, check chips with Add, authors-may-approve switch, keep-approvals swit... | done | templates/repo/settings/asc_policy.tmpl (stepper, switches), asc_policy_chips.tmpl, web_src/js/features/asc-policy-form.js; TestAscPolicyFormControls | - |
| Agents section: write paths, never-touch paths, budget minutes and tokens, locked "A person ap... | done | templates/repo/settings/asc_policy.tmpl agents fieldset (path chips, minutes, tokens, locked human approval switch); services/asctrust/policyform.go RuntimeSecsOf, HumanApproval; TestRuntimeMinutes, TestFormHumanApprovalIsReadAndKept | - |
| Merge queue section: use queue, batch size, checks after merge | done | proto/asc/v1/mergequeue.proto (not found in tree) | the where-cell cites A:crates/asc-serve, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Live file beside the form with changed lines flashing (+/-) | done | see where-cell | the where-cell cites GetSource.text, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Changes tab: diff of the one pending edit; "Changing policy is a change" panel naming reviewer... | done | `H:templates/repo/settings/asc_policy.tmpl` Changes view: the line diff of the pending edit and a "Changing policy is a change" panel listing the maintainers named in the policy as reviewers (or saying none is ... | - |
| TOML error state: "Line 11: max_minutes must be a whole number", form disabled while invalid | done | see where-cell | - |
| Read-only state for non-maintainers with names to ask | done | see where-cell | - |
| After-propose state: "Proposed as change kz9fw. Waiting for one approval" with Open change | done | templates/repo/settings/asc_policy.tmpl:171 (best match) | - |
| Loading state | done | `repo/asc_search_loading.tmpl`: a new search, a facet or an example query shows three result cards while the page loads (`asc-page-loading.js`); test `TestSearchLoadingState` | - |

#### RepoSettingsRules (ASC Hub repository settings: Branches and stacks)

Area repo. Rows 8: done 8, partial 0, missing 0. Copy found in the hub: 56% of 108 copy strings (mobile twin 44%). Template(s): branches.tmpl, protected_branch.tmpl, tags.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Which refs are protected, how stacks move and how they land.; Approvals and required checks are set in; . This page covers which refs are protected and how stacks behave.; Nobody can rewrite or delete these, whatever their role.; Everything lands here through the merge queue.; Cherry-picked fixes only..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Protected refs table: pattern, kind (Trunk, Release lines, Tags), can land (Merge queue, Maint... | done | templates/repo/settings/branches.tmpl:43 | - |
| Add / Edit rule side panel with Who can land checkboxes (Merge queue, Maintainers, Release key... | done | routers/web/repo/setting/asc_refrules.go, templates/repo/settings/branches.tmpl (`[refs]` rules, proposed as policy changes) | - |
| Pattern overlap error ("Rules cannot overlap") and "2 open stacks target a ref this pattern ma... | done | `H:models/git/asc_rule_overlap.go`, `H:models/issues/asc_open_pulls_base.go`, `H:routers/web/repo/setting/asc_rule_check.go` (`GET settings/branches/check`), `H:web_src/js/features/asc-rule-check.js`, ... | - |
| Rule removed with Undo for 30 minutes from the operation log | done | `H:models/git/asc_removed_rule.go`: removing a rule (page or `DELETE /branch_protections/{name}`) keeps a copy in `asc_removed_rule` (migration v18s) for 30 minutes; the rules page lists each removal with Undo until ... | - |
| Stack behaviour switches: restack when main moves, keep approvals when only the base changed,... | done | `H:services/pull/asc_auto_restack.go`, `H:services/pull/asc_restore_version.go` (`ascCollaboratorsMayPush`), `H:services/ascbridge/stackrules.go`, `H:services/asctrust/policyform.go`, ... | - |
| Longest stack (8) and clean-up after landing (7 days) | done | `H:services/asctrust/policyform.go`, `H:services/asctrust/toml.go` (`removeKey`), `H:routers/web/repo/setting/asc_rules.go`, `H:templates/repo/settings/branches.tmpl`, `H:templates/repo/settings/asc_policy.tmpl`; ... | - |
| How stacks land: keep every change / squash into one change | done | `H:routers/web/repo/setting/asc_rules.go`, `H:templates/repo/settings/branches.tmpl`, `H:services/asctrust/policyform.go`, `H:services/asctrust/orgsettings.go`, `A:crates/asc-serve/src/land.rs`; ... | - |
| Empty state "Nothing is protected" with Protect main | done | `H:templates/repo/settings/rules_empty.tmpl` on `branches.tmpl` when there is no rule: the sentence and "Protect <default branch>", which opens a new rule for it (not on an empty repository); `asc_rule_link_test.go` | - |

#### RepoSettingsSecrets (ASC Hub repository settings: Secrets and variables)

Area repo. Rows 9: done 9, partial 0, missing 0. Copy found in the hub: 68% of 95 copy strings (mobile twin 58%). Template(s): -. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Values your checks, agent runs and releases need.; Secret values are encrypted and shown only once. Agents get a secret only when...; for it, and a change cannot print one: the log redacts it.; Values for checks, agent runs and releases. Names are fixed once created.; CARD_PROVIDER_API_KEY; Update value.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Secrets and variables on one page | done | templates/shared/secrets/add_list.tmpl (file) | - |
| Scope chips per secret (Checks, Agent runs, Releases) and "Agents only receive it when Agent r... | done | templates/shared/secrets/add_list.tmpl, scope_boxes.tmpl, scope_chips.tmpl; models/actions/asc_run_scope.go | - |
| "Updated by Maya Okafor, 12 d ago" | done | `H:models/secret/secret.go`, `services/secrets/secrets.go`, `routers/web/shared/secrets/secrets.go`, `templates/shared/secrets/add_list.tmpl` | - |
| Update value dialog with name locked; value never shown | done | `H:routers/web/repo/setting/secrets.go`, `add_list.tmpl` | - |
| Delete confirmation that lists which scopes lose it | done | templates/shared/secrets/add_list.tmpl (secrets.deletion.named) | - |
| Name rule (capitals, digits, underscores) validation in dialog | done | see where-cell | - |
| Variables table with "Available to" scope and updated-by | done | templates/shared/variables/variable_list.tmpl; models/actions/variable.go | - |
| Secret-redaction in change logs ("the log redacts it") | done | `A:crates/asc-serve/src/ci/rpc.rs` masks every value of the repository's secrets and recognised credentials with `***` on `AppendJobLog` and `JobLog`, with stored context around each window, and refuses both while ... | - |
| Saved toast and empty state | done | `add_list.tmpl` | - |

#### RepoSettingsStorage (ASC Hub repository settings: Storage)

Area repo. Rows 8: done 8, partial 0, missing 0. Copy found in the hub: 53% of 96 copy strings (mobile twin 42%). Template(s): asc_storage.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Where changes, large files and indexes are kept.; Storage holds changes, large files and search indexes. Changing it never touche...; Where this repository's data lives; Pick one. Moving data is a copy first and a switch after.; ASC Hub storage; Managed by us. Nothing to set up; counted against your plan..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Choose ASC Hub storage or own S3-compatible bucket (endpoint https only, region, bucket, prefi... | done | templates/shared/asc/byo_storage.tmpl:45 | - |
| Test connection with four steps (reachable, can write, can read, versioning on) and AccessDeni... | done | VerifyStorageTarget steps with the versioning advisory and needs; byo_storage_target.tmpl | - |
| Usage: 38.2 GB split into changes, large files, search indexes plus objects and "read this mon... | done | Storage.GetRepoUsage; services/ascbyo/storage_usage.go; byo_storage_repo.tmpl; TestRepoStorageUsage | - |
| Moving data: 64% progress, Pause, Cancel and keep current | done | Storage.PauseMigration and ResumeMigration in asc (the worker holds the cursor and the mirror); Pause, Resume and Cancel and keep current in byo_storage_repo.tmpl; TestRepoStorageMigrationPause | - |
| Managed storage selected: "38.2 GB of 100 GB on your plan", Use my own bucket | done | the usage answer names where new objects go (GetRepoStorageUsageResponse target_id, fallback); on the server's storage the usage card shows the owner's quota on repository data (models/quota AllowanceFor) with a meter and Use my own bucket; asc's count is recorded as the repository size so the quota counts it; routers/web/shared/byoweb/plan.go; TestRepoStoragePlan, TestAllowanceFor | - |
| Local-option and http refused message | done | services/ascbyo/validate.go (file) | - |
| Bucket settings changed bar with Discard / Save and test | done | byo_storage_target.tmpl savebar; StorageUpdate saves and tests | - |
| Meaning-search index enabling link ("An admin can enable the index in storage settings") | done | hub only: Search indexes card of byo_storage_repo.tmpl from CodeIntel.IndexStatus (services/ascbyo/searchindex.go); a refused meaning search says asc_rp.s_err_503_meaning and links admins to storage; TestRepoStorageSearchIndexes, TestAscSearchMeaningIndexOff | - |

#### RepoSettingsWebhooks (ASC Hub repository settings: Webhooks)

Area repo. Rows 10: done 10, partial 0, missing 0. Copy found in the hub: 75% of 116 copy strings (mobile twin 72%). Template(s): -. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Send events to other systems.; Tell another system when something happens here. Each request is signed with th...; stack opened; One webhook, two views; Settings and recent deliveries belong to this hook, so they are tabs here.; Delivery d_7q2m1.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Hook list with URL, kind, events, last delivery, Active switch, Failing banner | done | templates/shared/webhook/list.tmpl; services/webhook/rows.go; models/webhook/hooktask.go FailingStreaks | - |
| One hook with tabs Settings / Recent deliveries (12), delivery detail with Request / Response,... | done | templates/webhook/hook.tmpl; templates/webhook/deliveries.tmpl; services/webhook/deliveries.go | - |
| Hook kinds Generic, Chat, ASC agent (starts an agent run on an event) | done | modules/webhook/type.go ASCAGENT; services/webhook/asc_agent.go; templates/webhook/settings.tmpl; services/repository/hooks.go | - |
| ASC events: Changes (Landed, Opened, Review requested), Agents (Run started, Run finished), Re... | done | modules/webhook/type.go AscHookEvents; services/webhook/asc_events.go; routers/private/asc_hook_event.go; services/webhook/send_me.go | - |
| Payload URL https-only with certificate check; loopback http only with local option | done | models/webhook/payload_url.go CheckPayloadURL ([webhook] ALLOW_LOCAL_HTTP); services/webhook/deliver.go skipTLSVerify; templates/webhook/payload_url_help.tmpl | - |
| Secret shown masked with Replace, never shown again | done | templates/webhook/secret.tmpl; routers/web/repo/setting/asc_webhook_list_test.go | - |
| Send test delivery | done | templates/webhook/test_delivery.tmpl | - |
| Failing-hook alert banner role=alert and menu count "Webhooks 3" | done | routers/web/repo/setting/asc_menu_status.go; templates/webhook/deliveries.tmpl (role=alert banner) | - |
| Add webhook dialog with http address refused | done | templates/repo/settings/webhook/add_dialog.tmpl; web_src/js/features/asc-webhook-add.js; routers/web/repo/setting/asc_webhook_add.go | - |
| Empty state | done | templates/repo/settings/webhook/base_list.tmpl:10 | - |

#### Search (ASC Hub search)

Area repo. Rows 21: done 21, partial 0, missing 0. Copy found in the hub: 59% of 124 copy strings (mobile twin 65%). Template(s): asc_search.tmpl, asc_symbols.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: In: all repositories; This version (main); A change or stack; Only files the stack changes; Indexed at e91b0d4, 99.2% of files. 4 files are too large for search and are li...; Sort: best match.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Text, Symbol and Meaning modes as radio group (lexical / symbol / semantic) | done | templates/repo/asc_search.tmpl:15 | the where-cell cites H:services/ascsearch, not in the tree (renamed or stale text); behaviour taken from the gap file |
| Scope radios: this version, change or stack, all branches, all repositories | done | routers/web/repo/asc_search_scope.go:42 | - |
| Cross-repository results grouped by repository, URL /-/asc/search | done | `GET /-/asc/search` serves the search of every repository, grouped by repository, while asc is on and code is not turned off site-wide (`H:routers/web/explore/asc_search.go` `AscGlobalSearch`, `routers/web/web.go`); ... | - |
| Facets: Repository (21 / 9 / 5, "11 more"), Language, Path, Branch or commit, "Only files the... | done | Repository facet on the all-repositories search (`H:routers/web/explore/asc_search_repos.go`, three shown and "N more"), Branch facet on an all-branches search (`ascSearchBranchFacets`); tests ... | - |
| Result card: path, language, branch list, snippet with highlights | done | templates/explore/asc_search.tmpl:189 | - |
| Relevance percentage with a bar (grows over 300 ms) | done | templates/explore/asc_search.tmpl:168 | - |
| "Why it matched" reason sentence per result | done | `H:services/ascsearch/reason.go`, `H:templates/repo/asc_search_why.tmpl`, `H:routers/web/repo/asc_search.go`, `H:routers/web/explore/asc_search.go`, `H:modules/structs/asc_search.go`; ... | - |
| Symbol chips under a result ("RetryPolicy::delay", "14 references elsewhere", "changed 3 h ago... | done | `H:routers/web/repo/asc_search_defs.go`: DEF and REFS per word of a text search of one version, last change from `Repo.LastCommits`; test `TestAscSearchSymbolsMatched` | - |
| "Symbols matched: RetryPolicy, retry" line | done | repository search (`ascSearchDefinitions`) and the all-repositories search (`definitions`) | - |
| Code intelligence side panel (definition, signature, Go to definition, references, Called from... | done | `H:routers/web/repo/asc_search_panel.go`: `GET /{repo}/asc/search/panel` fragment from `ascsearch.Panel` (CodeIntel fallback), cross-repo rows only for readable repositories; `asc-search-panel.js` loads it into the ... | - |
| Coverage line ("Indexed at e91b0d4, 99.2% of files; 4 files too large, listed in settings") | done | templates/explore/asc_search.tmpl (file) | - |
| Sort: best match, pagination "Showing 4 of 38", page links | done | `H:routers/web/repo/asc_search_page.go`: Best match or Path sort, pages of 10 over the bounded answer with "Showing 11–20 of 38" (of more than N when asc cut it); facets count the whole answer | - |
| "The other two kinds of search" teaser (Text and Symbol hits for the same query) | done | `H:routers/web/repo/asc_search_teaser.go`: Text and Symbol lookups counted with their first paths; Meaning offered as a link without a model call; a failed or empty lookup hides its line | - |
| First-visit state with example queries | done | templates/explore/asc_search.tmpl (file) | - |
| No-results state with "Search all branches / All repositories" actions | done | templates/explore/asc_search.tmpl:49 | - |
| Not available (503) for meaning search with "Use Text search" and "Open storage settings" | done | templates/repo/asc_search.tmpl:85 (best match) | - |
| Rate limited (429) keeps the query | done | see where-cell | - |
| Loading state | done | `repo/asc_search_loading.tmpl`: a new search, a facet or an example query shows three result cards while the page loads (`asc-page-loading.js`); test `TestSearchLoadingState` | - |
| / focuses search in the repository | done | web_src/js/features/ds-shell.js (file) | - |
| Mobile: sticky search with mode and scope, Filters bottom sheet, results as cards | done | below 900 px the form (query, kind, scope) is sticky and the narrowing fields sit under it (`form=` attribute); `asc-search-filters.js` moves the facets into a `dialog.rp-sheet` opened by Filters (count of filters ... | - |
| Mobile: Sticky search, Filters bottom sheet, cards with reason first | done | as the Search mobile row; the reason line on each card comes from the Search "Why it matched" row (done) | - |

#### Stacks (ASC Hub stacks)

Area repo. Rows 13: done 13, partial 0, missing 0. Copy found in the hub: 66% of 107 copy strings (mobile twin 61%). Template(s): asc_badges.tmpl, asc_stack.tmpl, list.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Agents 2; Needs me 2; - Diego Alvarez - 3 changes -; - Hannah Weiss - 5 changes -; No reviewers yet; Raise tokio to 1.41 and drop the unused rt-multi-thread feature.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Area sub-navigation Stacks / Merge queue / Policy gate / Operation log | done | templates/repo/asc_stack_nav.tmpl:6 | - |
| Merge queue page with pause and resume | done | routers/web/repo/asc_queue.go (file) | - |
| Stack row: avatar, title, stack name, author, change count, +/- size, checks, age, status chip... | done | `/pulls` renders the Forgejo issue list (`H:templates/repo/issue/list.tmpl`, `shared/issuelist.tmpl`); build a stack-grouped list from stacked pulls (`H:services/pull`, `BuildAscStackPanel`); Audit 2026-10-30 (read): ... | - |
| Rail with segments per change and text alternative "Stack of 4 changes"; legend (Landed, Appro... | done | same data as the Repo progress rail; one shared template; Audit 2026-10-30 (read): templates/repo/stack_board.tmpl legend and the row rail | - |
| Filters Open / Mine / Agents / Needs me / Closed with counts; sticky filter bar | done | Needs me = review requested or queue-ready for the viewer; Agents needs an agent flag; `H:routers/web/repo/issue.go` (`Pulls`); Audit 2026-10-30 (read): stack_board.tmpl filter group with counts (f_open, f_mine, ... | - |
| Sort Newest / Needs attention | done | attention score from status; Audit 2026-10-30 (read): stack_board.tmpl sort links | - |
| "Your turn" card with jump to the exact change | done | pulls awaiting the viewer's review or enqueue; Audit 2026-10-30 (read): stack_board.tmpl your_turn card with open_change | - |
| "From your terminal" card (asc stack list, asc stack submit) | done | templates/repo/issue/list.tmpl:84 (best match) | - |
| New stack button | done | no web create-stack flow; `H:routers/web/repo/pull.go` creates pulls by compare; agents or terminal create stacks; Audit 2026-10-30 (read): stacks_new links to the compare page in the empty state and the header | - |
| Keyboard j/k and Enter | done | see where-cell | - |
| States: empty (Nothing is in flight, New stack, Open history), skeleton, error with request id | done | list template; Audit 2026-10-30 (read): stacks_empty_title, stacks_failed with request id and Try again in stack_board.tmpl | - |
| Row hover slide-in, status chip pulses once on change | done | CSS + SSE event (`H:services/ascinbox` / `eventsource.sharedworker.js`) | - |
| Mobile: 44 px rows, chips scroll | done | see where-cell | - |

### Area: review

#### ChangeChecks (ASC Hub checks on a change)

Area review. Rows 10: done 10, partial 0, missing 0. Copy found in the hub: 56% of 52 copy strings (mobile twin 62%). Template(s): asc_check_group.tmpl, asc_checks.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Version 4 - commit; Security scan is stale.; Not seen on main; Both failures hit the same line.; divides by; , which is 0 in these tests. Your change touched this function in.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Headline, pill, version/commit/started/took, stale banner, Re-run failed only / Re-run all | done | templates/repo/pulls/asc_checks.tmpl:18 | - |
| Cause cards: title, test count, New in v4 / seen before, log excerpt, Show full log, group link | done | templates/repo/pulls/asc_checks.tmpl (file) | - |
| 'Open the line' jumps to the diff at file:line | done | services/asctrust/pullchecks.go (file) | - |
| 'Copy a repro command' (an asc command, not git) | done | templates/repo/pulls/asc_checks.tmpl button on each cause card copies 'asc ci checkout --repo R --rev SHA --dir repro && cd repro' (ascReproCommand; test: TestAscPullChecksRunsAndQuarantine) | - |
| 'Ask coder-2 to fix' (start an agent run on this failure) | done | templates/repo/pulls/asc_checks.tmpl 'Ask an agent to fix' on each cause: opens the agent form (asc/agents/new) filled with the failure as title and task and the pull request's branch as target; the form's CreateRun ... | - |
| Flaky card with 20-run strip, Quarantine this test, Open an issue | done | templates/repo/pulls/asc_checks.tmpl (file) | - |
| All checks list with required flag, duration; 'Run security scan on v4' per row | done | templates/repo/pulls/asc_checks.tmpl: Took per row (asctrust.StatusRow.Took) and 'Run on vN' per row, which re-runs that Actions run through AscPullChecksRerun with the field run (tests: ... | - |
| Compared with main: main green, 'these 2 failures are new', slowest test 41 s (was 12 s) | done | Done: vs-main card on the pull checks page (asctrust.LoadMainCompare, ascVsMainOf; tests TestAscPullVsMainCard, TestAscVsMainOf) | - |
| Versions history list (v4 2 fail, v3 pass, v2 pass) | done | templates/repo/pulls/asc_checks.tmpl: one line per version, newest first, from the runs of the change (ascVersionHistoryOf) (test: TestAscVersionHistory) | - |
| Trust text: results belong to the exact version | done | templates/repo/pulls/asc_checks.tmpl (file) | - |

#### Checks (ASC Hub checks)

Area review. Rows 13: done 13, partial 0, missing 0. Copy found in the hub: 55% of 120 copy strings (mobile twin 61%). Template(s): asc_check_group.tmpl, asc_checks.tmpl, asc_queue.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Failures grouped by cause, the jobs they happen in, and the tests that flip bet...; Pass rate; up 1.1 pts vs previous 30 days; Failure groups open; Median duration; slowest job: integration, 11m.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Window selector, state tabs, job/test/flaky filters persisted in URL | done | templates/repo/asc_checks.tmpl:110 | - |
| Headline stats: pass rate vs previous period, failure groups open, flaky above threshold, medi... | done | asctrust.LoadCiStats (Ci.Stats) + ascChecksStats cards in templates/repo/asc_checks.tmpl; window seg 7/14/30/90 | - |
| State tab counts (Open 3, Acknowledged 1, Quarantined 2, Resolved 14) | done | routers/web/repo/asc_checks.go + asctrust.LoadChecksSide: each tab shows its count (up to 100, then 100+) (test: TestAscChecksStateTabsCountAndSideCards) | - |
| Failure groups list with cause, jobs, seen N times in M changes, new-in-version chip | done | templates/repo/asc_checks.tmpl:3 | - |
| Inline expand of a group: log excerpt with line numbers, occurrences, note + Acknowledge / Qua... | done | templates/repo/asc_checks.tmpl: the first group (or ?open=ID) opens in the list, read on the server (asctrust.LoadGroupDetail, no JS): the last 40 lines of the newest message numbered, the newest 5 occurrences with ... | - |
| Row hover actions Acknowledge / Quarantine / More | done | templates/repo/asc_checks.tmpl: for a writer each row has Acknowledge, Quarantine (policy permitting) and a More menu (show the log here, open the group, Resolve or Reopen), shown on hover and focus and always on ... | - |
| Quarantine hidden unless policy allows [ci] allow_quarantine | done | templates/repo/pulls/asc_checks.tmpl flaky card: the button shows only when AllowQuarantine (CanWrite and asctrust.QuarantineAllowed); the group page and the state handler already checked it | - |
| Acknowledge / quarantine / resolve / reopen with optional note, logged | done | routers/web/repo/asc_checks.go:34 | - |
| Repo-level 'Re-run failed' button | done | routers/web/repo/asc_checks.go:496 | - |
| Flaky tests view: flake rate, runs, flips, 14-run heat strip, last flip, state | done | templates/repo/asc_checks.tmpl (file) | - |
| Trends view per job with pass rate | done | templates/repo/asc_checks.tmpl (file) | - |
| Side cards: 'Main is green', failing jobs bars, Quarantined list with Manage | done | templates/repo/asc_checks.tmpl: Main is green / failing / running (newest commit of the default branch), a bar per failing job (JobStat.Share), the quarantined groups with Manage (test: ... | - |
| Empty 'Nothing needs attention', error with Try again, loading skeleton | done | templates/repo/asc_checks.tmpl: Nothing needs attention with See trends for the open list, No failures match with Clear filters for a filtered list, a titled error with Try again when asc fails (not for a bad ... | - |

#### NewChange (ASC Hub open a change)

Area review. Rows 13: done 13, partial 0, missing 0. Copy found in the hub: 35% of 75 copy strings (mobile twin 41%). Template(s): asc_compare.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: You are on a stack of 3 changes. This one goes on top. Title it for the person...; Stack: idempotency-keys; idempotency-keys; in 9 files; Ahead 3, behind 0; Intent and description.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Open-change form: title, description, reviewers, draft | done | templates/repo/diff/compare.tmpl (file) | - |
| Compare bar: base/head, +212 -48 in 9 files, no-conflict chip, ahead/behind, stack of N | done | templates/repo/diff/compare.tmpl: size and files, No conflicts (or Conflicts with the base) from git merge-tree, and Ahead N, behind M from git rev-list (only when both sides resolve); compare_state.go, ... | - |
| Title counter '68 of 120', 'Start with a verb' hint, inline validation | done | templates/repo/diff/compare.tmpl (file) | - |
| Intent editor with What/Why/How to check/Risk template and Write/Preview | done | templates/repo/diff/compare.tmpl (file) | - |
| 'Draft from the diff' (AI writes What/Why/How/Risk from the diff) | done | Review.DraftContext fills title and description without a model; with a review-summaries key the BYO model writes What/Why/How/Where (routers/web/repo/asc_summary.go, POST asc/summarize and asc/brief/summarize, ... | - |
| 'Where to look' pointers editor | done | templates/repo/issue/new_form.tmpl Where to look field: pills linked to the changed files, add with path (datalist of the diff files), line and note, remove; kept in the description's Where to look first section that ... | - |
| Reviewer suggestions from ownership and review history, out-of-office note, agent reviewers la... | done | asc: Review.SuggestReviewers takes the paths of a comparison before the change exists and says who owns them (the literal people of the policy's [paths] rules), what they know (the directory and share of the threads ... | - |
| Options: open as draft, keep as part of stack, land when approved and green, ask agent to baby... | done | Options card (asc_newchange_options): draft prefix, stack row when changes sit below, land via merge queue (stacked) or scheduled merge, babysit (services/ascbabysit: one run per failed head, 3 max, Stop on the ... | - |
| Local checks start on open; 'signed with your key' note | done | templates/repo/issue/new_form.tmpl note under the intent for an asc repository: checks start on open, signed with your key when one is added (test: TestAscCompareNotesOnOpen) | - |
| Draft autosave on blur (1 s debounce), 'Save as draft', Ctrl+Enter opens | done | routers/web/repo/asc_newchange_draft.go: POST /{repo}/asc/newchange_draft keeps the title and description per person and comparison in one user setting (at most 10 drafts, 255 bytes of title and 32 KiB of description ... | - |
| Right rail: stack after this change, file list with Show all, 'What policy will ask for' previ... | done | templates/repo/issue/asc_newchange_rail.tmpl at the top of the form's right column (new_form.tmpl); routers/web/repo/asc_newchange_rail.go prepareAscNewChangeRail (called from CompareDiff for a signed person in an ... | - |
| 'Review my diff first' self-review shortcut | done | templates/repo/diff/compare.tmpl (file) | - |
| Blocked-by-secret-scan banner before open; nothing-to-compare; opened/draft toasts | done | asc SecretScan.Check over the compare range blocks the open and names the files; nothing-to-compare and the toasts exist | - |

#### Review (ASC Hub review)

Area review. Rows 30: done 30, partial 0, missing 0. Copy found in the hub: 59% of 150 copy strings (mobile twin 49%). Template(s): asc_confirm.tmpl, asc_graph.tmpl, asc_queue.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Stack - 4 changes; main @ 4be19a2; Merge order is enforced:; Merge 1 and 2 now; based on change 2; . Everything else is mechanical..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Stack rail: 4 changes, current marker, per-change state, trunk line, merge-order note, asc sta... | done | templates/repo/pulls/asc_stack.tmpl:12 | - |
| Stack rail rows: author avatar with agent mark, +add/-del size, text like 'Approved, restacked' | done | templates/repo/pulls/asc_stack.tmpl + routers/web/repo/pull_asc_stack.go enrichAscStackPanel: author avatar with the Agent mark, +add/-del, and Approved / Changes requested / Draft on each open row (the 'restacked' ... | - |
| Stack rail click keeps scroll position per change | done | web_src/js/features/asc-review.js (initRailScroll: the position is saved in sessionStorage when a rail link is clicked and restored when the change is opened again; test: asc-review.test.js) | - |
| Rail action 'Merge 1 and 2 now' (merge bottom of stack, with dry run) | done | templates/repo/pulls/asc_stack.tmpl (file) | - |
| Change header: agent banner (waiting on you, 42 tests pass), change id + short hash, Signed ba... | done | templates/repo/pulls/asc_change_header.tmpl under the title (view_title.tmpl); routers/web/repo/asc_change_header.go prepareAscChangeHeader: change id, newest version and its short hash, the Signed badge of that ... | - |
| Tab strip: Overview, Files, Versions v1>v4, Checks, How it was made | done | templates/repo/pulls/tab_menu.tmpl: a stacked change reads Overview, Files, Versions v1>vN, Checks, How it was made, then Commits; anchors #asc-versions and #asc-how-made; the newest version comes from ... | - |
| Intent, 'Where to look' pointers (file:line), Size, Files, Risk summary block | done | templates/repo/pulls/asc_brief.tmpl: What/Why/How, Where to look pointers, size, files and risk note from the change brief (routers/web/repo/asc_brief.go, asc_brief_edit.go; tests: TestAscBriefOf*) | - |
| Files by meaning: per-file Behavior/Tests/Generated badge | done | templates/repo/diff/asc_class_badge.tmpl (file) | - |
| Files by meaning: group counts and filter above the diff | done | templates/repo/diff/box.tmpl class bar (counts from pull_service.AscFileClassCounts) filtered by asc-review.js; every file box carries data-asc-class (tests: TestAscDiffBoxClassBarAndStates, asc-review.test.js) | - |
| 'Changed since you looked' count, 'Since my last review v3>v4' selector, 'Changed in v4' badge... | done | templates/repo/pulls/asc_versions.tmpl count and 'Show only what changed since my review' toggle (files, not single hunks: the interdiff link shows only the changed hunks); templates/repo/diff/box.tmpl 'Changed in ... | - |
| Unified/Split layout, collapse file, Viewed checkbox with progress | done | templates/repo/diff/box.tmpl:32 | - |
| Hunk header shows enclosing function (impl PaymentsClient > async fn) | done | services/gitdiff hunk header; Audit 2026-10-30 (built): the hunk header shows the chain of definitions from Review.HunkContext (routers/web/repo/asc_hunk_context.go, section_unified.tmpl and section_split.tmpl), also ... | - |
| Inline comment threads: open/resolved, replies, Resolve, markdown composer | done | routers/web/repo/pull_review.go:188 | - |
| Threads visible to asc clients: hub comments are not written to the asc Review service (ListTh... | done | proto/asc/v1/review.proto; services/ascsync (new review-thread sync); pull_review.go; Audit 2026-10-30 (built): hub comments, replies, resolve and review requests are mirrored to asc as the person ... | - |
| Agent reply in a thread labelled 'Agent' with avatar | done | templates/shared/user/authorlink.tmpl: every author link of an agent account (type bot) carries the Agent chip, in comments, reviews, timeline and the pull request header (test: author_agent_mark_test.go; an agent ... | - |
| Suggested change block in a comment, 'Apply as v5' (creates a version, undoable op, resets app... | done | a suggestion fence in a hub comment is the suggestion of the asc thread; the line comment form is the composer of the board ("/" puts the lines in a suggestion block, templates/repo/diff/comment_form.tmpl, ... | - |
| Keyboard review shortcuts j/k file, a approve, r request changes, c comment, v viewed, n next... | done | web_src/js/features/asc-keys.js:3 | - |
| 'Your review' checklist (read intent, opened 2 of 3 files, N open comments) with Approve / Req... | done | templates/repo/pulls/asc_panel.tmpl 'Your review' card: read the intent (kept in localStorage), files opened, open comments (live, from the page), Approve, Request changes, Comment; the last two open the review box ... | - |
| Activity timeline: version pushed, comment, approval carried, restacked (14 events, Show all) | done | templates/repo/pulls/asc_activity.tmpl (+ asc_activity_item.tmpl) after the brief card; routers/web/repo/asc_activity.go merges the hub's comments and reviews with asc's versions, the carried approvals of ... | - |
| Checks on v4 summary card (unit, lint, integration running, security scan stale) with 'Open ch... | done | templates/repo/pulls/asc_panel.tmpl: chips of the version being read (AscVersionChecks) with 'Open checks on vN'; a stale mark is not shown (asc does not say which result is stale) (test: TestAscPanelChecksCard) | - |
| Policy gate card: approvals 1 of 2, carried approval note, required checks, signed with run ke... | done | templates/repo/asc_gate.tmpl (file) | - |
| Approval carry-over: approval on an unchanged diff survives a new version and says so | done | templates/repo/pulls/asc_brief.tmpl:50 | - |
| Landing card: queue position, estimated start, 'Add changes 1 and 2 to queue', dry-run line | done | routers/web/repo/asc_landing.go, templates/repo/pulls/asc_panel.tmpl (Landing card); estimate is asc MergeQueue timing | - |
| Restack banner + action when trunk moved | done | templates/repo/pulls/asc_stack.tmpl:2 | - |
| 'Restacked onto main, automatic, 3 min ago' line with Undo | done | templates/repo/pulls/asc_stack.tmpl + routers/web/repo/asc_undo_links.go: the newest operation on the change's branch, when it is a restack, with the actor, the time and Undo to the undo preview (tests: ... | - |
| Reviewers list with status, request, suggested reviewers, agent reviewer (reviewer-1) | done | templates/repo/asc_confirm.tmpl:70 (best match) | - |
| 'How it was made': run id, task, tool calls, versions, files read, 'never touched infra/', lin... | done | the brief names the run and its agent, the task, the files read, the test runs and the untouched directories, with a link to the run (asc run provenance) | - |
| Conflict with main: stays open, other changes unblocked, markers, 'Resolve in editor', 'Show b... | done | Banner with first-hunk preview, both-sides page, in-browser editor that restacks with the resolutions (Stacks.Restack resolutions, Repo.ReadFile of the marked text), ask the agent that made the change; ... | - |
| Loading skeleton keeping layout; load error with 'Try again' and 'Open previous version'; comm... | done | templates/repo/diff/box.tmpl [data-asc-diff-state]: skeleton while more files load, error with Try again, Open previous version (AscPrevVersion) and 'Comments you wrote are saved'; repo-diff.js loadMoreFiles drives ... | - |
| Applied-as-v5 toast with Undo (8 s, role=status) | done | templates/repo/pulls/asc_applied_toast.tmpl after Apply (routers/web/repo/pull_asc_suggestion.go setAscAppliedToast, shown once): Applied as vN, the file and line, "Approvals reset for this change." when the branch ... | - |

#### ReviewVersions (ASC Hub versions and landing)

Area review. Rows 13: done 13, partial 0, missing 0. Copy found in the hub: 49% of 78 copy strings (mobile twin 64%). Template(s): asc_compare.tmpl, asc_confirm.tmpl, asc_graph.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Stack - 4 changes; main @ 4be19a2; Merge order is enforced:; Merge 1 and 2 now; Change 3 of 4; Every push is kept as a version. Compare any two, restore one as a new version,....

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Version timeline with Base/Head radios per column, current badge, signature badge, actor and a... | done | templates/repo/pulls/asc_versions.tmpl:9 | - |
| Version row detail: commit message, short hash, +add/-del, per-version check chips (unit pass,... | done | templates/repo/pulls/asc_versions.tmpl:10 | - |
| 'Approved by Priya' marker on the version she approved | done | templates/repo/pulls/asc_versions.tmpl:12 | - |
| Restack rows shown as 'ASC, automatic ... no diff change' | done | services/pull/asc_version_details.go (file) | - |
| Restore as new version (never rewrites history) | done | routers/web/repo/pull_asc_restore.go:16 | - |
| Compare any two versions (interdiff), split/unified | done | routers/web/repo/pull_asc_versions.go:20 | - |
| Compare header: '+2 -1 in 1 of 3 files, Priya's approval still applies to the other 2' | done | asc: per-file unchanged set; hub: pulls/asc_versions.tmpl; Audit 2026-10-30 (built): Review.CompareVersions gives the +added -removed in N of M files and which approvals still apply (asc_compare_versions.go, ... | - |
| Restack-available banner 'Trunk moved 3 commits since v2' with Restack | done | templates/repo/pulls/asc_stack.tmpl:2 | - |
| Land-the-stack panel: scope radios (Bottom 2 / All 4 / Just 1), dry run re-run on scope change | done | templates/repo/asc_confirm.tmpl (file) | - |
| Land panel summary: merge order, signed-by list, queue position and ETA, undo window 30 min | done | templates/repo/asc_confirm.tmpl asc-land-summary + routers/web/repo/asc_land_summary.go: the order the pull requests land in, the verified signers and the count without one (asc trust), the place in the merge queue ... | - |
| Confirm for Merge now names branch, current hash and result hash; default focus Cancel | done | templates/repo/asc_confirm.tmpl AscMergeFacts: branch, where it is now, where it will be (the head of the change merged); Cancel has the focus for merge and revert (test: ... | - |
| Result toast 'Merged 2 changes... Undo is open for 30 minutes' with Undo; undone toast | done | routers/web/repo/asc_queue.go ascMergedMessage: the banner after Merge now carries an Undo link to the undo preview while the merge is the newest operation on the branch (a banner, not a toast; the 30 minute window ... | - |
| Merge refused because trunk moved, 'Reload dry run' | done | templates/repo/asc_confirm.tmpl (file) | - |

### Area: agents-ops

#### AgentAccess (ASC Hub agent access)

Area agents-ops. Rows 8: done 8, partial 0, missing 0. Copy found in the hub: 77% of 111 copy strings (mobile twin 81%). Template(s): asc_agents_policy.tmpl, asc_agents_tokens.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Each agent works with its own token. A token says which paths it may write, how...; Limits per run; reviewer-1; Expires in 2 days; Permission and plan requests from agents; Plan: rewrite 12 docs pages.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Tokens: name, hint, scope paths, per-run limits, status, expiry, last used, meters; Make token... | done | templates/repo/asc_agents_tokens.tmpl:60 | - |
| Secret shown once with copy and ASC_TOKEN export line | done | templates/repo/asc_agents_tokens.tmpl (file) | - |
| Spend this month per token ($58 of $120) as a monthly budget | done | the monthly budget is per agent; a token's row shows its agent's month spend of the budget and names the agent it counts for, with a budget form | - |
| Approvals log (permission and plan requests with decision and decider) | done | the tokens page lists the last 20 decided requests: what, agent, decider, when | - |
| Policy defaults card: write scope, never trunk, changes per run, cost cap, token lifetime, who... | done | templates/repo/asc_agents_defaults.tmpl on the tokens page: [agents] default_scope, trunk and denied_paths, max_changes_per_run and max_cost_micro_usd from the policy (No limit when unset), the longest token lifetime ... | - |
| Spend this month panel: cap, daily spend chart, per-agent totals, 'people using AI keys' | done | see where-cell | - |
| Revoke confirm dialog naming the agent; Budget dialog | done | templates/repo/asc_agents_tokens.tmpl (file) | - |
| Empty state 'No tokens yet' | done | templates/repo/asc_agents_tokens.tmpl (file) | - |

#### AgentNew (ASC Hub give an agent a task)

Area agents-ops. Rows 8: done 8, partial 0, missing 0. Copy found in the hub: 61% of 46 copy strings (mobile twin 67%). Template(s): asc_agents_new.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: What to do, and what to leave alone. Keep changes small and stacked. The agent...; Plain language. Mention files or tests it should use, and anything it must not...; Limits for this run; Add a path, one per line; Leave empty for the token scope. A run can only narrow what its token allows.; Starting is logged. You can stop the run at any time and keep the work..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Task, target branch, model/key, limits (changes, tokens, cost, minutes), narrow write scope, s... | done | templates/repo/asc_agents_new.tmpl (file) | - |
| Agent picker showing each agent's scope (coder-2 src/**, tests/**) | done | templates/repo/asc_agents_new.tmpl (file) | - |
| 'Ask me to approve the plan first' switch | done | templates/repo/asc_agents_new.tmpl:82 (best match) | - |
| Right rail 'What this run may do', computed from token and policy, updates as scope narrows | done | templates/repo/asc_agents_maydo.tmpl above the estimate card; routers/web/repo/asc_agents_maydo.go: Policy.Evaluate for the agent principal, called as the signed-in person because a real asc answers another principal ... | - |
| Estimate 'About 10 min and $1.60' from similar runs | done | templates/repo/asc_agents_new.tmpl:134 (best match) | - |
| Recent tasks with Reuse | done | templates/repo/asc_agents_new.tmpl:143 (best match) | - |
| Limits above the policy cap are lowered with a message, not rejected | done | services/ascagentui/budget.go LoweredText: after New agent, Change limits and Continue the flash says "Limit lowered. Your policy caps a run at $5.00. The limit was set to $8.00, so it was lowered." from the lowered ... | - |
| Started toast 'Run 216 started' with Open; can-start right required | done | templates/repo/asc_agents_new.tmpl:124 (best match) | - |

#### AgentRun (ASC Hub agent run)

Area agents-ops. Rows 18: done 18, partial 0, missing 0. Copy found in the hub: 58% of 101 copy strings (mobile twin 57%). Template(s): asc_agents_run.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: reviewer-1 -; done, 2 changes; sonnet-5; It wants to write; to raise the gateway timeout. This is outside its write scope (; You approved at 10:02.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Runs side list / switcher (runs 214, 213, 212 ...) with New task | done | templates/repo/asc_agents_run.tmpl (file) | - |
| Task quote ('Task by Maya Okafor: ...') on the run page | done | see where-cell | - |
| Header: state chip, model, started-by, age, target, workflow link | done | templates/repo/asc_agents_run.tmpl:4 | - |
| Pause (holds the run at the next tool call) | done | templates/repo/asc_agents_run.tmpl (run_pause_help); routers/web/repo/asc_agents_ctl.go (PauseRun) | Gap file says asc has no PauseRun; asc master has Agents.PauseRun and the hub calls it. |
| Stop with confirmation and reason; keeps work as drafts | done | templates/repo/asc_agents_run.tmpl (Stop popover: help, reason, confirm), AscAgentsCancel sends the reason and flashes asc_review.agents_stopped; TestAscAgentsCancelAndInput | - |
| Plan block with step states and 'You approved at 10:02' | done | the plan card says You approved at (time) or You rejected when the viewer decided the plan | - |
| Permission request banner: Allow once / Deny and continue / Answer in own words; records who a... | done | templates/repo/asc_agents_run.tmpl:91 (best match) | - |
| Live activity log: poll, follow unless scrolled, 'Show everything' toggle, error handling | done | web_src/js/features/asc-agent-run.ts:334 | - |
| 'Reconnecting: showing the log as of 10:14' banner | done | templates/repo/asc_agents_run.tmpl (data-reconnect chip + as-of line), asc-agent-run.ts reconnectText/showReconnect (asc-agent-run.test.js), .ar-reconnect in review-ctl.css | - |
| Steer this run / answer the agent | done | templates/repo/asc_agents_run.tmpl (file) | - |
| Result: stack of changes with state and Review links; 'agent cannot merge' note | done | templates/repo/asc_agents_run.tmpl:182 | - |
| Budget meters: tokens, cost, time, changes with numbers | done | templates/repo/asc_agents_meters.tmpl:2 | - |
| Budget 'Edit limits' on a live run | done | see where-cell | - |
| Access for this run: can/cannot list, token id and expiry, signed by run key | done | templates/repo/asc_agents_run_token.tmpl: for a repository administrator, the run's token id, state, expiry, write paths and changes per run (AgentTokens.GetToken); 'signed by run key' is not shown, asc has no such ... | - |
| Checkpoints list with Roll back (logged) | done | templates/repo/asc_agents_run.tmpl (run_cp_help); asc_agents_ctl.go (ListCheckpoints, RollbackRun) | Gap file says asc has no checkpoint RPC; it has, and the hub calls it. |
| Audit note and 'Download the audit log' (90 days, linked from each change) | done | templates/repo/asc_agents_run.tmpl:225 (best match) | - |
| Failed state 'hit its model token limit' with 'Raise the limit and continue' and Review the 3... | done | templates/repo/asc_agents_run.tmpl (run_continue) | Raise the limit and continue is in the template. |
| Done summary (38 tool calls, 11 min, $2.31) and Stopped notes | done | templates/repo/asc_agents_run.tmpl done banner: changes ready, tool calls, time, cost and Open the stack (top review, else the result card); stopped banner keeps the work as drafts. asc counts tool calls on the run ... | - |

#### Agents (ASC Hub agent runs)

Area agents-ops. Rows 14: done 14, partial 0, missing 0. Copy found in the hub: 71% of 93 copy strings (mobile twin 62%). Template(s): asc_agents.tmpl. Mobile twin: yes.

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
| Numbered pagination and 'Showing 5 of 24' | done | asc: ListAgentRunsRequest.offset + ListAgentRunsResponse.total (crates/asc-serve/src/agents/runs.rs page/count, tests_flow.rs); hub: ascAgentsPageNum, ascAgentsPagerOf (asc_agents_common.go), .ag-pages nav in ... | - |
| Sub-navigation Runs / Workflows / Access / Spend | done | templates/repo/asc_agents_nav.tmpl:2 | - |
| Agent features off state with 'Turn on agent features' | done | templates/repo/asc_agents_notice.tmpl (file) | - |
| Empty state; loading skeleton | done | templates/repo/asc_agents.tmpl (data-asc-agents-loading card, three skeleton rows), web_src/js/features/asc-agents-loading.js (+ .test.js), .ag-sk-* in review-ctl.css; TestAscAgentsList | - |
| Header pill 'N agents running' on every page | done | templates/base/head_navbar.tmpl (`ds-agents-pill`; asc_agents_pill.tmpl is the run-state pill, not this) | - |

#### Audit (ASC Hub audit log)

Area agents-ops. Rows 6: done 6, partial 0, missing 0. Copy found in the hub: 85% of 96 copy strings (mobile twin 81%). Template(s): asc_audit.tmpl, asc_audit_table.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: main, ag_, .asc/; change.approve; agent.token.use; Hannah Weiss; secret scan: cloud access key; reason: release freeze.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Chain status, Verify, filters (who, what, outcome, target, from/to, order, rows), table, older... | done | templates/repo/asc_audit_table.tmpl:112 | - |
| Repository, organization and site scope from one page | done | templates/repo/asc_audit_table.tmpl:2 (best match) | - |
| Verify shows a determinate progress bar | done | web_src/js/features/asc-audit.js (verifySlices, slices of 2,000 events, last slice checks the head), routers/web/shared/trustweb/audit.go (step=1 JSON, done), asc VerifyAuditChainResponse.first_seq/head_seq; ... | - |
| Broken chain: 'Show event 9,204' and 'Export evidence' | done | templates/repo/asc_audit_table.tmpl (board text, Export evidence then Show event); TestAscAuditVerifyChainBrokenOffersTheEvent | - |
| Export-ready card (file name, size, event count, Download) | done | web_src/js/features/asc-audit.js (initExports), X-Asc-Audit-Events header in trustweb/audit.go; repo and admin templates; asc-audit.test.js, TestAscAuditExportIsSignedWhenTheServerSignsIt | - |
| Retention label 'Kept 365 days'; no-match state with Clear filters | done | templates/repo/asc_audit_table.tmpl (retention pill, rv-empty with the kept days); TestAscAuditListsEventsWithFiltersAndPaging | - |

#### Ops (ASC Hub operation log)

Area agents-ops. Rows 13: done 13, partial 0, missing 0. Copy found in the hub: 74% of 96 copy strings (mobile twin 87%). Template(s): asc_ops.tmpl, asc_ops_undo.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Every change to a branch of this repository, newest first. You can undo your ow...; Branch or ref: any; Trunk moved 3 commits. Your stack restacked cleanly.; See what changed; ASC, automatic; undo is open for 30 min.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Operation list: kind pill, ref, actor, age, hash pair, Undo | done | templates/repo/asc_ops.tmpl (file) | - |
| Blocked reasons beside a lock icon (not yours, branch changed, too old, protected needs mainta... | done | templates/repo/asc_ops.tmpl:108 | - |
| Branch filter, Only my operations; filters in URL; older operations pager | done | templates/repo/asc_ops.tmpl:29 | - |
| Kind filter chips (Push, Landed, Restack, Undo, Agent) and '6 of 214' count | done | templates/repo/asc_ops.tmpl (file) | - |
| Restack banner with 'See what changed' and 'Undo restack' | done | templates/repo/asc_ops.tmpl: the banner of the newest own restack leads with the restack icon, says the changes it rewrote and the approvals it kept (asc's dry run of its undo, asctrust.PreviewUndo), links See what ... | - |
| Export of operations | done | routers/web/repo/asc_ops.go (file) | - |
| 'Who changed branches this week' actor bars with 7/30 window | done | see where-cell | - |
| Selecting a row loads the undo preview in a right panel without navigating | done | asc-ops.js: a click on an undoable row, its Undo or Undo restack loads the dry run into the panel, marks the row, 120 ms crossfade (reduced motion off); Cancel restores | - |
| Undo preview (now at / goes back to / who / reason) and undo is itself logged | done | templates/repo/asc_ops_undo.tmpl (file) | - |
| Preview lines '4 changes go back', 'approvals on 2 unchanged diffs are kept' | done | asc UndoRef dry run answers changes_back and approvals_kept; templates/repo/asc_ops_undo.tmpl asc-undo-effects; asc_ops_test.go TestAscOpsUndoPreviewIsADryRunAndCarriesTheHeads | - |
| Undone toast with Redo | done | templates/repo/asc_ops.tmpl: after an undo the log shows a toast that names the branch, says where it is back at and links Redo to the preview of undoing the undo; asc_ops_test.go TestAscOpsOffersTheRedoOfAnUndo | - |
| No-match state with Clear filters; loading skeleton | done | templates/repo/asc_ops.tmpl: a filtered empty log says no operations match with Clear filters, an empty log says so on its own; a chip, the filter form or the pager shows skeleton rows (asc-list-loading.js); ... | - |
| Refused state with Reload | done | templates/repo/asc_ops.tmpl (file) | - |

#### Policy (ASC Hub policy gate)

Area agents-ops. Rows 10: done 10, partial 0, missing 0. Copy found in the hub: 65% of 121 copy strings (mobile twin 57%). Template(s): asc_confirm.tmpl, asc_policy.tmpl, asc_queue.tmpl. Mobile twin: yes.

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
| Loading skeleton | done | templates/repo/asc_workflows.tmpl: three board skeleton rows in place of the definitions while a state chip or the search loads (web_src/js/features/asc-list-loading.js, shared with Agents); asc-list-loading.test.js, ... | - |

#### Queue (ASC Hub merge queue)

Area agents-ops. Rows 11: done 11, partial 0, missing 0. Copy found in the hub: 74% of 94 copy strings (mobile twin 74%). Template(s): asc_confirm.tmpl, asc_queue.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: next lands in about 3 min; Refused after dry run; both needed a content merge; Rate limit headers; unit running on merged trunk, about 3 min; Queued by Diego Alvarez.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Branch, running/paused chip, revalidate on/off, Pause/Resume confirm dialogs | done | templates/repo/asc_queue.tmpl:4 | - |
| Stats: Waiting, Landed today, Median wait (was 9 min last week), Refused after dry run | done | templates/repo/asc_queue.tmpl:45 (best match) | - |
| Waiting list: position, title, state chip (Checking/Queued), queued-by, Remove | done | templates/repo/asc_queue.tmpl (file) | - |
| Row detail: 'unit running on merged trunk, about 3 min' and 'starts about 6 min' ETAs | done | templates/repo/asc_queue.tmpl:62 (best match) | - |
| Merge now per row (hover/focus) with confirm | done | templates/repo/asc_queue.tmpl: each waiting row has Remove and Merge now (icons as on the board), shown on hover and focus and always on touch, each through the confirmation page; Merge now follows the merge ... | - |
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

Area agents-ops. Rows 9: done 9, partial 0, missing 0. Copy found in the hub: 63% of 46 copy strings (mobile twin 71%). Template(s): asc_workflows_run.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: dependency-update; started manually by; This step needs a decision.; Why you decide this way...; Your decision is logged with your signature.; Passed in 48 s.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Run header: state, started by, time, about change; Cancel run, Run again with same inputs | done | templates/repo/asc_workflows_run.tmpl (file) | - |
| Cancel confirmation dialog | done | templates/repo/asc_workflows_run.tmpl: "Cancel run" opens a popover with what cancelling keeps and a confirm button, beside "Run again"; asc_workflows_test.go TestAscWorkflowRunPage | - |
| Approval banner: comment, Approve / Decline, signed log entry, 'an agent can never decide' | done | templates/repo/asc_workflows_run.tmpl:78 | - |
| Flow diagram with connectors filling as steps finish | done | asc WorkflowStepRun.kind and approvers from the run's definition; templates/repo/asc_workflows_run.tmpl "Flow" card: id, state, a line per state (passed in, changes and cost, waiting for whom, approved by), kind ... | - |
| Steps list with output line, duration, state | done | templates/repo/asc_workflows_run.tmpl (file) | - |
| Live polling of step state | done | web_src/js/features/asc-agent-run.ts (file) | - |
| 'Proposed by the agent': changes with size, state and Review links | done | routers/web/repo/asc_workflows_proposed.go ascWorkflowStepProposed (called from AscWorkflowRun for readable agent steps when the agent switch is on): the step's agent run (Agents.GetRun) resolved by ... | - |
| Step log (agent events) and 'Open the agent run' link | done | templates/repo/asc_workflows_run.tmpl: a "Log of step <id>" card per readable agent step under the steps, headed by the step, its agent, how long it took and the older lines left out, with "Open the agent run" when ... | - |
| 'Used so far' meters (tokens, cost, time, agent steps) | done | templates/repo/asc_workflows_run.tmpl (file) | - |

#### Workflows (ASC Hub workflows)

Area agents-ops. Rows 11: done 11, partial 0, missing 0. Copy found in the hub: 60% of 107 copy strings (mobile twin 64%). Template(s): asc_workflows.tmpl, asc_workflows_runlist.tmpl, asc_workflows_runs.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: A workflow is a file in the repository that chains agent steps, checks and huma...; Find a workflow; dependency-update; Propose the weekly dependency updates as a reviewed stack; Every Monday 06:00 UTC; last Today 06:02.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Definitions list with triggers, description, path, valid/not valid reason | done | templates/repo/asc_workflows.tmpl (file) | - |
| On/off switch (role=switch) and Run now on each list row | done | templates/repo/asc_workflows.tmpl (file) | - |
| Step chain 'scan > update > review' on the card | done | templates/repo/asc_workflows.tmpl (file) | - |
| Last-12-runs history strip and 'last run' time per workflow | done | templates/repo/asc_workflows.tmpl:20 | - |
| State filter (All, On, Off, Not valid), search, counts | done | templates/repo/asc_workflows.tmpl (file) | - |
| 'New workflow' and 'Start from a template' (creates a reviewed change with the file) | done | asc_workflows.go AscWorkflowTemplates (3 templates), editor.go asc_template: the editor opens a new file with the template on a new branch | - |
| Latest runs list (running, passed, needs approval, failed) with link to all runs | done | templates/repo/asc_workflows_runlist.tmpl (file) | - |
| Secrets and variables panel (names per workflow) with link to settings | done | templates/repo/asc_workflows.tmpl:134 | - |
| Capacity '3 running of 6 allowed' and shared agent budget line | done | templates/repo/asc_workflows.tmpl:141 | - |
| No-workflows state with template and guide buttons | done | templates/repo/asc_workflows.tmpl: board text, "Start from a template" (plus icon, writers only) and "Read the guide" (book icon) to /-/docs/workflows; docs/asc/WORKFLOWS.md shipped in services/ascdocs; ... | - |
| Loading skeleton | done | templates/repo/asc_workflows.tmpl: three board skeleton rows in place of the definitions while a state chip or the search loads (web_src/js/features/asc-list-loading.js, shared with Agents); asc-list-loading.test.js, ... | - |

### Area: auth

#### AdminAuth (ASC Hub admin: authentication)

Area auth. Rows 2: done 2, partial 0, missing 0. Copy found in the hub: 85% of 47 copy strings (mobile twin 90%). Template(s): edit.tmpl, list.tmpl, new.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Directory (LDAP); ASC Hub 1.4.2; Hub and web UI based on Forgejo; All systems operational; Rendered in 18 ms.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Sources list with edit and add | done | templates/admin/auth/list.tmpl (file) | - |
| People count per source; Local passwords row (admins only option); Test sign-in showing claims... | done | Built: the people per source (`routers/web/admin/asc_auth_counts.go`, a People column and the built-in Local passwords row, which is listed even when it is the only source) and the refusal to remove a source that ... | - |
| Board note: Test sign-in runs the full flow and shows the claims received | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Local passwords can be limited to admins | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, test sign-in result (pass/fail), no sources (setup), remove blocked by people wh... | unverified | not tracked in the gap files | needs a row and a test |

#### Onboarding (ASC Hub sign in and sign up)

Area auth. Rows 19: done 19, partial 0, missing 0. Copy found in the hub: 57% of 111 copy strings (mobile twin 51%). Template(s): activate.tmpl, forgot_passwd.tmpl, grant.tmpl, reset_passwd.tmpl, signin.tmpl, signup.tmpl, twofa.tmpl, webauthn.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Quiet, fast, and honest about errors; Sign in to ASC Hub; Keep me signed in on this device; Sign in with a passkey; Signing in from the CLI?; . This page will ask you to approve the device..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Split layout: form card plus navy brand panel with promise and quote | done | templates/user/auth/shell_start.tmpl (file) | - |
| Sign in form: username or email, Forgot password, password with Show toggle, Keep me signed in... | done | `signin_inner.tmpl`, `ds-shell.js` `onRevealClick`; the CLI hint (`user/auth/cli_hint.tmpl`) teaches `asc clone` with no login step and the device code a private repository asks for | - |
| "Sign in with a passkey" (discoverable credentials) | done | routers/web/auth/asc_passkey.go, templates/user/auth/oauth_container.tmpl, web_src/js/features/asc-passkey.js | GET /user/webauthn/passkey/assertion and POST /user/webauthn/passkey/login; the key must verify the person; keys registered with ASC on are asked to be passkeys |
| Single sign-on button under the primary action | done | templates/user/auth/oauth_container.tmpl (file) | - |
| Rate limit ("after 5 attempts we wait 5 minutes") | done | services/auth/method/asc_login_limit.go, modules/ascratelimit/lockout.go, routers/web/auth/asc_login_limit.go | Counts are in memory per process |
| Sign up: username with live "Your handle: @name" and blur validation | done | templates/user/auth/signup_inner.tmpl (file) | - |
| Sign up: optional Full name | done | templates/user/auth/signup_inner.tmpl (file) | - |
| Sign up: strength meter and three live rules (12 characters, letters and numbers, not found in breaches) | done | templates/user/auth/password_meter.tmpl, web_src/js/features/asc-password.js, routers/web/auth/asc_password_range.go | The breach rule shows with [security] PASSWORD_CHECK_PWN; the browser sends only the first five digits of the SHA-1 |
| Sign up: "I agree to the Terms and the Privacy policy" | done | templates/user/auth/signup_inner.tmpl, routers/web/auth/asc_terms.go, models/user/asc_terms.go | Required with ASC on; acceptance time kept in asc_terms_acceptance |
| Sign up has no retyped password on the board; the hub form asks for one | done | see where-cell | - |
| Two-factor: six boxes act as one input and auto-advance; recovery code link; "Use a security k... | done | `twofa.tmpl` six-box field, "Use a recovery code", and "Use a security key instead" to `/user/webauthn` when the person has a key (`HasSecurityKey` in `routers/web/auth/2fa.go`); test `asc_twofa_test.go` | - |
| CLI device approval: code entry, confirm with device name, Authorise or Deny, expiry, wrong-co... | done | models/auth/asc_device_code.go:20 | - |
| Security key waiting screen with spinner and "Try another way" | done | `webauthn.tmpl`: waiting spinner and a "Try another way" disclosure with the authenticator code and recovery code (when the person has an authenticator) and sign in again; test `asc_twofa_test.go` | - |
| Authorise the CLI (device): code, terminal and OS, Authorise and Deny | done | templates/user/auth/asc_device.tmpl; routers/web/auth/asc_device.go; routers/web/web.go:744 | Gap file says not present; the device page and RFC 8628 routes exist (the token it mints is full-scope and has no expiry, see sec... |
| Forgot password: sent page, lifetime, resend limit, no account disclosure | done | templates/user/auth/forgot_passwd.tmpl:2 | - |
| Reset password: new and confirm | done | templates/mail/auth/reset_passwd.tmpl (file) | - |
| Registration closed (invite only) and awaiting approval | done | templates/user/auth/signup_inner.tmpl:5 | - |
| Focus on first invalid field; autocomplete tokens | done | templates/shared/ds/field.tmpl:19 | - |
| Mobile: Sticky primary action above the keyboard-safe area; 48 px fields | done | web_src/css/asc-ds/touch.css (file) | - |

#### UserSettingsApps (ASC Hub settings: applications)

Area auth. Rows 2: done 2, partial 0, missing 0. Copy found in the hub: 74% of 61 copy strings (mobile twin 68%). Template(s): grants_oauth2.tmpl, hooks.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Apps you built, apps you authorized and webhooks.; OAuth2 apps that sign people in with ASC Hub.; Authorized applications; Apps that can act as you. Revoking is instant..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| OAuth2 apps, authorized apps with revoke, client secret shown once, regenerate | done | templates/user/settings/grants_oauth2.tmpl:2 (best match) | - |
| Webhooks table: events column, last delivery status text and time, Replay from the list | done | templates/shared/webhook/rows.tmpl (host over URL, events as chips with "+N more", last delivery dot with status and time, Edit or Replay when the last delivery failed), services/webhook/rows.go ListRows, ... | - |
| Board note: Client secrets are shown once and can be regenerated | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Webhook Replay re-sends the last failed delivery | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Revoke removes the grant and every token issued under it | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Left menu is the only navigation for settings: no tabs repeat these pages | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default, empty applications (illustration + New application), webhook failing, secret sho... | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Revoked toast | unverified | not tracked in the gap files | needs a row and a test |

#### UserSettingsKeys (ASC Hub settings: ssh and signing keys)

Area auth. Rows 5: done 5, partial 0, missing 0. Copy found in the hub: 73% of 66 copy strings (mobile twin 62%). Template(s): asc_keys.tmpl, keys.tmpl, keys_gpg.tmpl, keys_ssh.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Changesets signed by ASC show a verified badge to reviewers and in the operatio...; ASC signing key.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| SSH keys (fingerprint, added, last used, delete) and GPG keys | done | templates/user/settings/keys_ssh.tmpl (file) | - |
| ASC signing keys list, add, revoke with state chips | done | templates/user/settings/asc_keys.tmpl (file) | - |
| Rotate action that keeps the old key verifying past signatures | done | services/asctrust/rotate.go (file) | - |
| Sign my changesets by default switch | done | templates/user/settings/asc_keys.tmpl (asc-keys-default card), routers/web/user/setting/asc_keys.go AscKeysDefault (POST /user/settings/asc/keys/default), services/asctrust/ops.go SetSigningDefault; asc keeps the value with the account (Signing.GetSigningDefaults/SetSigningDefaults, audit signing.defaults.set) and the CLI follows it through asc signing default and asc.signByDefault | - |
| Delete confirm that lists what stops working | done | templates/user/settings/keys_gpg.tmpl:92 (best match) | - |
| Board note: Delete asks to confirm and lists what stops working | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Rotate keeps the old key verifying past signatures | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Last used helps spot keys that can go | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Left menu is the only navigation for settings: no tabs repeat these pages | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Default list, empty list with command to generate a key, invalid key error, key added toa... | unverified | not tracked in the gap files | needs a row and a test |
| Board note: Signing on and off | unverified | not tracked in the gap files | needs a row and a test |

#### UserSettingsSecurity (ASC Hub settings: security)

Area auth. Rows 6: done 6, partial 0, missing 0. Copy found in the hub: 65% of 72 copy strings (mobile twin 57%). Template(s): -. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Password, two-factor, passkeys and the devices that are signed in.; At least 12 characters. A passphrase is stronger than symbols.; Strong. Would take centuries to guess.; A second step when you sign in from a new place..

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Change password | done | templates/user/settings/account.tmpl:98 | - |
| Password strength meter in words after 300 ms, show/hide buttons | done | see where-cell | - |
| Two-factor enable/disable/re-enroll, passkeys (WebAuthn) add/delete | done | templates/user/settings/security/twofa.tmpl:11 | - |
| Recovery codes: "8 of 10 unused", download as text file, shown once | done | models/auth/twofactor_recovery.go (salted hashes, each spent once), POST /user/settings/security/two_factor/recovery_codes, recovery_codes.tmpl; tests/integration/asc_recovery_codes_test.go | - |
| Signed-in devices list (browser, place, last active), Sign out one / everywhere else | done | browser sessions: models/auth/browser_session.go (hashed session IDs, checked on each request by the session auth method; signing out also ends the remember-me token), security/sessions.tmpl; asc credentials on ... | - |
| Re-authentication prompt before password change or removing 2FA | done | password change asks the current password; removing or replacing 2FA, a passkey, recovery codes and signing other browsers out pass security.RequireReauth (password within 10 minutes, sign-in counts), reauth.tmpl; ... | - |

#### UserSettingsTokens (ASC Hub settings: access tokens)

Area auth. Rows 6: done 6, partial 0, missing 0. Copy found in the hub: 62% of 96 copy strings (mobile twin 59%). Template(s): applications.tmpl, authorized_integrations.tmpl. Mobile twin: yes.

Board copy not found word for word in locale, templates or scripts: Skip to content; Personal settings; Credentials for scripts, CI and the asc CLI. Prefer short-lived sign-in where y...; For scripts and the asc CLI. Give each one the least access that works.; asc CLI on laptop; changes:write.

| Element | Status | Evidence | Gap |
|---|---|---|---|
| Token list (name, scopes, created, last used) with revoke and regenerate | done | templates/user/settings/applications.tmpl (file) | - |
| Expiry per token (30 days etc.), Expires column, Expired chip | done | models/auth/access_token.go has no expiry; migration + access_token_edit.tmpl + check in services/auth; Audit 2026-10-30 (built): models/auth/access_token.go ExpiresUnix, GetAccessTokenBySHA refuses an expired token, ... | - |
| ASC scope set (Repositories, Changesets and stacks, Agent runs, Organizations, Profile) as Non... | done | hub: models/auth/access_token_scope.go + access_token_edit.tmpl; asc: crates/asc-serve/src/authn.rs must accept and enforce them; Audit 2026-10-30 (hub built): scopes read/write changes and runs in ... | - |
| Token created: copy-once card with Copy and "shown once" text (today a flash message) | done | routers/web/user/setting/access_token.go (file) | - |
| Workload identity (issuer, subject, last used, remove, add) | done | templates/user/settings/authorized_integrations.tmpl (file) | - |
| "Prefer no tokens" card with asc auth login commands | done | templates/user/settings/applications.tmpl "Prefer no tokens" card: browser sign in, --token-stdin for CI, auth status; tests/integration/user_test.go TestAccessTokensPreferNoTokens | - (the board's asc auth token --scope line names no asc command; the browser sign in replaces it) |

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
| st84 | UserSettingsPrivacy | needs-both | done | templates/user/settings/privacy.tmpl (sp_agents_profile), routers/private/asc_agent_profile.go |
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
| RepoSettingsMenu: One left menu with groups Repository (General, Access N, Rules, Policy, Branches and stac... | done | `H:templates/repo/settings/navbar.tmpl` has General / Code / Integrations / BYO / Policy groups with Forgejo pages (Units, Collaboration, Git hooks, LFS, Actions runners) and no counts | - |
| RepoSettingsMenu: Mobile root list with status per row ("1 failing"), "Find a setting" search over names an... | done | `snav_*` templates have no search or status; new `settings` index partial | - |
| Mobile-RepoSettings*: Detail pages with back arrow and same title, one save bar | done () | see where-cell | - |
| Mobile-Changeset, Compare, Re...: Same behaviour as the desktop boards | done () | see where-cell | - |
| Mobile twins: Mobile-Review: actions sheet (Approve, Request changes, queue, Undo restack, Copy asc com... | done | templates/repo/pulls/asc_panel.tmpl (pinned bar, actions sheet), web_src/js/features/asc-actions-sheet.js | - |
| Mobile twins: Mobile-Review: Approve / Request changes pinned above the tab bar | done | asc_panel.tmpl .rv-review-pinned: Approve and Request changes fixed above the tab bar under 900, the review box opens above it; review-diff.css | - |
| Mobile twins: Mobile-Review: long-press a line to comment | done | asc-review.js initLongPress: a 500 ms touch on a diff line opens its comment form (touched side first), drift, lift or scroll cancels, the menu and trailing tap are swallowed | - |
| Mobile twins: Mobile-ReviewVersions: confirm as a sheet with default focus Cancel; toast with Undo | done | templates/repo/pulls/asc_stack.tmpl (sheet), web_src/js/features/asc-confirm-sheet.js (sheet, Cancel focus, Undo toast) | - |
| Mobile twins: Mobile-Checks: failures as cards, chips scroll sideways, Re-run failed pinned | done | under 640 each failure group is its own card with 44px buttons; the state chips already scroll sideways (.rv-chips); Re-run failed pinned above the tab bar (asc-checks-pinned) | - |
| Mobile twins: Mobile-Checks: swipe a card left for Acknowledge | done | asc-checks-swipe.js: a touch swipe of 80 px left opens the card Acknowledge (the button stays); pan-y keeps scrolling, the card springs back, reduced motion off | - |
| Mobile twins: Mobile-AgentRun: Allow once / Deny pinned; pull to refresh | done | asc_agents_run.tmpl pins Allow once / Deny under 900 when a permission request is pending; asc-pull-refresh.js pulls to refresh in the installed app (browsers keep their own) | - |
| Mobile twins: Mobile-Ops: undo preview as a bottom sheet | done | asc_ops.tmpl dialog.rv-ops-sheet: under 640 the undo preview opens as a bottom sheet (Cancel focused, backdrop and Esc close, 44px buttons); asc-ops.js | - |
| Mobile twins: Other mobile twins (Agents, Workflows, Queue, Policy, Audit, Security, AgentNew, AgentAcc... | done () | see where-cell | - |
| MobileUserSettings*: Settings menu as one list page, Other group (Site administration for admins, Sign out), b... | done () | templates/user/settings/navbar.tmpl (file) | - |
| MobileUserSettings*: Save bar sticks above the bottom nav once a field changes | done () | see where-cell | - |
| MobileAdmin*: Same data as desktop in stacked groups, back arrow to the admin menu, sticky action bar | done () | templates/admin/navbar.tmpl (file) | - |
| MobileIssues/Projects/Package...: Single-column twins; same gaps as the desktop rows above; sticky primary bar on forms | done | issue, change (compare) and project forms end in .ds-form-bar (asc-ds/components.css), which sticks above the bottom under 768px; settings.md row MobileIssues/Projects/Packages/Drafts | - |

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
6. DONE: passkey sign-in, the wait after wrong passwords, the password strength meter with the breach rule and the Terms checkbox (WU-06).
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

## 13. OrgPeople st140 to st145 (branch feat/org-people)

The People page has the board's toolbar (search, chips Everyone, Owner, 2FA off, Pending, Invite people), rows with name, handle, role, 2FA, teams, last active, the changes landed in the last year and a Change role menu next to a more-actions menu, the page counter, an Agents section from the organization's asc agents, and Pending invitations with Resend and Revoke. A role is read from the teams: Owner is the owners team, Maintainer is a team with the administrator permission (a team called Maintainers is made when no such team exists), Member is anyone else. Changing a role adds the person to the team of the new role before taking them out of the old one, so nobody leaves the organization on the way; the last owner stays an owner, and a Maintainer with no other team cannot be made a Member (the page says so). The changes count is one Insights.ListOwnerContributors call (feature insights-owner.v1) over up to 64 of the organization's asc repositories the viewer can read, signed with an organisation read claim, for 366 days, cached a minute (per-repository Insights.ListContributors, 24 at most, when asc lacks the feature): it counts changes that landed, not all changes a person made. Measured: OrgPeople 0.56 to 0.29 (dark 0.30), Mobile-OrgPeople 0.15. What is left is the organization header (OrgProfile rows), the clock (the harness freezes the browser, not the server), and the hub's alphabetical order against the board's owners first.

## 14. Organization head (branch feat/org-header)

The head of an organization page (OrgProfile, OrgPeople, OrgTeams and the mobile twins) now follows the boards: an organization without a picture of its own gets its first letter on a brand-coloured 88 px tile, the name is 30 px with the Verified chip, and the place, address, people and agents are one line (the people count had a row of its own). The agents count comes from asc (the organization's agents) and is shown to members only; it is cached for a minute per viewer. The harness cast now has the organization named Acme Payments with the board's description and its domain verified. Measured, light: OrgPeople 0.31 (it was 0.29 before the head moved the sections below it by 3 to 26 px; the head itself now matches), Mobile-OrgPeople 0.16, OrgProfile 0.29, Mobile-OrgProfile 0.45. Organizations created on a real hub get a generated picture, so the tile shows only for those without one; the boards' tile for every organization would need the generated pictures dropped. What is left on OrgProfile is the page body (pinned repository card, People and Teams cards), not the head.

## 15. Download layout (branch feat/download-layout)

The page is the boards': breadcrumb, the lead with the version on its right, a two-column hero (the build that fits, its size and checksum, the button, the first package-manager command), the systems as links, then macOS, Linux and Windows stacked (the order of the boards) each with a line about it, its package managers, and one table of files (file with kind, processor and the Fits chip, size, a cut checksum with a copy button, signature, Download). The per-processor install scripts are behind a disclosure under each system. The tabs script is gone: every system is on the page for everybody, and the browser's hints only mark the build that fits. Measured with a macOS browser: Download 0.46 to 0.18 (dark 0.12), Mobile-Download 0.60 to 0.27 (dark 0.11). Left: the board's install-script row and the Debian, Fedora, Arch and PowerShell rows (the manifest names only brew, winget and scoop), sizes in MB where the hub prints MiB, and the 44 px copy buttons on a phone. Two tests of the landing page and header, `TestAscDownloadHiddenWithoutRelease` (the `#navbar` link) and `TestAscDownloadLandingAndHeader`, fail on asc/v16 without this change and need their selectors updated by whoever changed the header.

## 16. The manifest of asc's release (branch feat/download-manifest)

Found while adding the package rows of the Download boards: the `manifest.json` that asc's release process writes (schema 1: `file`, `kind` cli, `format`, a full release time, aarch64) was not readable by the hub, whose reader wanted its own shape (`name`, kind installer, archive or script, a date, arm64). A real release, mirrored with `publish-to-hub.sh` or read from GitHub, would have shown no download at all. The reader now takes schema 1: the command line archives of every platform with their signatures, SHA256SUMS and its signature. Not taken over, because they would offer something that does not exist: the Homebrew formula, Scoop and winget manifests (rendered, but published to no tap, bucket or index), the nfpm configurations and PKGBUILD (no .deb or .rpm is built or published, nothing is uploaded to the AUR), the editor packages (release files, not marketplace addresses) and the SBOM. What the boards show and asc does not publish: Debian, Fedora and Arch rows, a PowerShell row and the install-script rows (`install.sh` and `install.ps1` are in asc's repository, in no release and not mirrored by `publish-to-hub.sh`), winget, Scoop and Homebrew names, the MSI of Windows, musl tarballs. The hub builds its own `curl | sh` verified install command from each archive. The two download integration tests now follow the redesigned landing and header.

The pixel harness now writes the release in asc's own shape (schema 1, five targets, archives in tar.gz and zip, the editor packages, SBOM and package manager files, SHA256SUMS). Measured again with it: Download 0.20 (dark 0.15), Mobile-Download 0.37 (dark 0.25): the board's Debian, Fedora, Arch, PowerShell, Windows installer and install-script rows are not in what asc publishes.

## 17. AdminUser (branch feat/admin-user)

The page of a person has the board's head (Edit, Reset 2FA, Suspend or Unsuspend), the five tabs (Overview, Emails, Keys, Sessions, Activity as `?tab=`), on Overview the account facts (organizations, repositories owned and shared, sign-in methods) and the Usage card (storage and agent minutes, asked of asc as the site administrator, left out when asc cannot say), and the Delete card. Reset 2FA and Suspend are their own actions: a reason is required (at most 500 characters), they refuse your own account, and the reason is written to the server log, to the notices the administrators see under Notices, and to asc's audit log through Audit.Append (host.user.suspended, host.user.unsuspended, host.user.2fa_reset, each with its reason; the actions are in asc's catalogue on branch feat/audit-host-user). Keys lists the SSH and GPG keys; Sessions lists the tokens, SSH keys and agent tokens asc keeps for the person, with Revoke (asc lets the site administrator do this from the Admin user page). Activity reuses the feed of the profile. Measured: AdminUser 0.31 to 0.14 (dark 0.14), Mobile-AdminUser 0.42 to 0.32. Left: the board's Reset 2FA button shows even when the person has no second factor (here it shows only when there is one to reset), the mobile twin's head card, and the full-width Delete button on a phone.

## 18. AdminBackups (branch feat/admin-backups)

The page is the board's: a summary card (schedule in words, target with its encryption note, next backup, Back up now and Edit), the history as one table (Backup, Size, Status, Note, and Retry for a failed run, Check for an unchecked one, Restore for a restorable one), an In progress card while a backup runs, and a banner when the last attempt failed (with the note of the failure and the age of the newest good copy). The schedule form, and Back up to another target, are in a closed "Schedule and run options" card (open when backups are not set up; Edit jumps to it). The hub's own volume instructions (docker commands) stay, in a closed block at the end. The pixel harness configures a local backup target `r2-backup`, saves the 24-hour schedule and makes one backup. Measured: AdminBackups 0.60 to 0.26 (dark 0.26), Mobile-AdminBackups 0.51 to 0.46. Left: the board's history rows are invented (a failed run, three verified ones, "Restore test passed" notes) and the harness has one real, unchecked backup; the board's Restore confirmation panel is a picture of the modal the Restore page shows (it is not drawn on the page); the date format (board: "27 Oct 02:00", live: "Oct 30, 2026").

Backups, second pass: the history shows the design's day-first time ("27 Oct 02:00", UTC, new `DateUtils.DayTime`). The harness now makes a backup to `r2-backup`, checks it (Verified), then makes one to `offsite`, a target that cannot be written (Failed, newest), so the page shows the attention banner, a Failed row with Retry and a Verified row with Restore. Measured: AdminBackups 0.26 (dark 0.25), Mobile-AdminBackups 0.33 (dark 0.34).

## 19. Org boards and the day-first date (branch feat/org-boards)

Teams: the board's layout. A narrow body, "N teams" and New team, cards that are links (the whole card selects the team, ?team=), each with the avatars and the repository count in its foot, and below them the detail of the selected team (people, repositories with the team's access, Edit team, Add member, Join or Leave). The View, Join and Leave buttons moved from the cards into that panel. Organization page: the side cards are the board's (People with five avatars, "+N" and the agents of the organization, Teams with "N people", Merge policy with a link to the policy page); the People page body is narrow. Dates: the design writes dates day first, so `DateUtils.Day` ("3 Feb 2026") and `DateUtils.DayTime` ("27 Oct 02:00") replace `AbsoluteShort` on the settings pages for keys, tokens, applications and security, on the admin people list and page, on the backup restore page, and the download page's release date. Left on OrgProfile: the repository rows (stacks, checks and Watch), the Type menu in place of the filter pills, and the board's header counts (38 people). Left on OrgSettings (0.16): the board is a different page (cards for Profile, Defaults and access, Agents, Policy and Danger zone with its own sidebar); the live page is Forgejo's settings form.

## 20. The 14 boards that did not answer 200 (branch feat/triage-404)

Seven boards, with their mobile twins, answered 404 or 503 in the pixel run. None was a hub defect; the harness did not set up what the page needs. Fixed in the harness: the moderation queue and quota are switched on (AdminModeration, UserSettingsUsage); each asc stack gets a branch of its key on the hub, because a stack is only `refs/pull/N/head` (Compare, NewChange); asc serves CodeIntel (`[codeintel] enabled`), so code search answers (Search); and an agent run exists as run 1 (AgentRun): a key of the team points at a small model server started next to asc (python, on the asc port + 50, allowed by `[netguard] allow_loopback_http` with that one port), the model has a price in `[aikeys] price_file`, the key is tested, and one run is started on the payments repository. Errors is a 404 page by design (its route does not exist), so its 404 is right. Measured (light): AgentRun 0.44 to 0.06, Search 0.40 to 0.13, NewChange 0.43 to 0.25, UserSettingsUsage 0.15 to 0.06, AdminModeration 0.17 to 0.04; Compare now answers 200 and is 0.44 (it was a 404 page before).
