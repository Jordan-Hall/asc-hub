# Feature gaps: the shell boards

Audit of the group "shell" against the design boards in `project-v3/` (each board's markup and its annotation block: states, interactions, accessibility, maps-to). Method: the board text was extracted and every visible feature or behaviour was listed, then checked by reading the hub (templates, routers, models, services, `web_src/js`) and the asc protocol (`proto/asc/v1/*.proto`, crates). The dev hub was not run. Date of audit: 2026-10-28, hub branch `asc/v16`.

Boards: Shell (header, menus, palette, notifications, footer), Main (landing), Onboarding, Install, GetStarted, Download, Explore, ExploreUsers, ExploreOrgs, Docs, About, Errors, Dashboard, Inbox, Profile, OrgProfile, and the Mobile- twins (mobile-only behaviour is listed under the board it belongs to or under "Mobile shell").

Status words: **done** (present and behaves as the board), **ui-only-missing** (template, CSS or JS only), **needs-hub-backend** (new or changed hub Go code, model or migration), **needs-asc-backend** (new asc RPC or crate work only), **needs-both**. Size: S under half a day, M one to two days, L three days or more.

"asc has no X" means a search of `proto/asc/v1` found no RPC for it. Existing RPCs a feature can reuse are named. Items marked done were confirmed by reading the template or script; CSS timings and contrast were not re-measured.

## Counts

| Status | Rows |
| --- | --- |
| done | 174 |
| ui-only-missing | 0 |
| needs-hub-backend | 0 |
| needs-asc-backend | 0 |
| needs-both | 21 |
| total | 195 |

Audit of 2026-10-30: the counts above were recomputed from the table rows (the earlier header counts were not updated as rows were marked). Rows that were read in the tree and corrected carry an Audit note in their Where cell. Per-board counts and the verified status of every board are in `docs/asc/STATUS.md`; the per-board count table below is not maintained.

Open rows by size: S 0, M 21, L 14.

## Shell: header, menus, palette, notifications, footer

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Shell | Signed-out header: Explore, Download, Docs, About, quiet Sign in, cyan Create account | done | `templates/base/head_navbar.tmpl` | - |
| Shell | Docs and About links (header and footer) open the hub's own pages (`/-/docs`, `/-/about`, both routed and templated) instead of GitHub blobs | done | `head_navbar.tmpl` (`$docsURL`, `$aboutURL`), `footer_content.tmpl`; board maps About to `GET /-/about` | S |
| Shell | Signed-in header: Changes, Agents, Explore, Download, search field with `/` and Ctrl K hints, Inbox, Create, Account | done | `head_navbar.tmpl` | - |
| Shell | Active link: white label, 2px cyan marker, `aria-current=page` | done | `head_navbar.tmpl`, `web_src/css/asc-ds` | - |
| Shell | Inbox amber count from `/-/asc/inbox/count`, cached 60 s | done | `asc-keys.js` `initAscInboxBadge` | - |
| Shell | "N agents running" pill in the header (cyan dot, links to Agents) and in the mobile top bar | done | `templates/base/head_navbar.tmpl` (desktop and `.ds-topbar`), `GET /-/asc/agents/count` (`routers/web/user/asc_agents_count.go`, one asc `Agents.CountRuns` call, cached 30 s), `initAscAgentsPill` in `asc-keys.js` | - |
| Shell | Notifications bell opens a popover (unread count, Mark all read, items with avatar, text, repo, age, Open inbox) | done | `routers/web/user/asc_bell.go` (`GET /-/asc/bell` fragment, `POST /-/asc/bell/read`, `GET /-/asc/bell/open/{id}`), `templates/user/notification/asc_bell.tmpl`, `#ds-bell` in `head_navbar.tmpl`, `web_src/js/features/asc-bell.js`. Agent, check, queue, restack and policy events from asc serve (`asc_notify_log`, read state added by migration) are listed with hub notifications and counted live by the event source | - |
| Shell | Account menu: identity, profile, inbox with count, agents, settings, Appearance (Light, Dark, Auto), Density (Comfy, Compact), Documentation, Sign out | done | `head_navbar.tmpl` `#ds-account`, `head_style.tmpl` | - |
| Shell | Theme and density persist per person: localStorage, then the account setting | done | `POST /-/asc/prefs` (`routers/web/user/asc_prefs.go`) saves the theme with the account and the density as the `asc.density` setting (`models/user/asc_prefs.go`); `head_style.tmpl` applies the values of the account before the first paint and copies them to `localStorage`; `web_src/js/features/asc-prefs.js` saves each change | - |
| Shell | Create menu: New repository, Import a project, New organization, New changeset, Start an agent run | done | All five are in #ds-create (head_navbar.tmpl); "New changeset" opens `/-/asc/changeset/new` (`H:routers/web/user/asc_changeset_new.go`), the asc repositories the person can write, each opening its editor on the default branch | S |
| Shell | Menus close on Esc or outside click, return focus to trigger, fade and rise, reduced motion | done | `ds-shell.js`, `asc-ds/components.css` | - |
| Shell | Command palette opens from `/`, Ctrl K, header search, mobile search; Esc, arrows, Enter | done | `ds-shell.js`, `#ds-palette` | - |
| Shell | Palette is a combobox: `role=combobox` input, listbox results, `aria-activedescendant` | done | Today `role=dialog` plus `role=search` form and an `.is-sel` class. Change markup in `head_navbar.tmpl` and `select()` in `ds-shell.js` | S |
| Shell | Palette scope chips All, Changes, Code, People, Commands; Tab cycles the scope | done | `ds-palette-bar` has input and Cancel only. Chips in `head_navbar.tmpl`, Tab handling in `onPaletteKeydown` | S |
| Shell | Palette results grouped Changes (title, repo and number), Code (path, symbol), People, fetched as you type | done | `routers/web/asc_palette.go` AscPalette (`GET /-/asc/palette?q=&at=`): Commands, Changes (pull requests the person may read), Code (ascPaletteCode: symbol definitions from asc `CrossSearch.LookupSymbol` over the repositories `ascsearch.Resolve` hands it, each with kind, repository, path and line, linked to the line; 1.5 s budget, at most 8 lookups at once, left out when asc is off, slow or failing), Repositories and People, with the explore rules for visitors; `web_src/js/features/ds-shell.js` waits 180 ms, aborts the previous call and draws each group in its scope chip; tests TestPaletteFindsCodeInReadableRepositories, TestPaletteShowsPeopleOnlyWhereTheExplorePageDoes, ds-shell.test.js | - |
| Shell | Command verbs (undo, restack, merge) switch the list to actions; Commands group with Undo last operation (Cmd Z) and Ask an agent to change this... | done | `routers/web/asc_palette_commands.go` (commands of the page from `at`, writers of the code only; `"verb"` in the answer), `routers/web/repo/asc_ops_last.go` (`GET .../asc/ops/undo/last` to the undo dry run), `ds-shell.js` (`verbScope`, commands scope). Every command opens its confirmation page. No Cmd Z shortcut: documented in `STACK-OPERATIONS.md` | - |
| Shell | Empty query shows Recent items and Shortcuts (Go to inbox: g i) | done | Recents in `localStorage` (visited repos and changes), static shortcuts; `ds-shell.js`, `head_navbar.tmpl` | M |
| Shell | No-results text names the query and suggests a symbol, handle or title | done | `asc_shell.palette_empty` is a different sentence ("Press Enter to search this text") | S |
| Shell | Keyboard shortcuts overlay (`?`, "All shortcuts") | done | No handler in `asc-keys.js`; dialog listing the keys of the page | S |
| Shell | Footer: full footer on public pages (four groups, version, Based on Forgejo, language, theme switch), one-line footer when signed in | done | `templates/base/footer_content.tmpl` | - |
| Shell | Footer links: Stacks and changes, Agent runs, Changelog, CLI reference, REST API, MCP server, Status, Security, Contact, Privacy, Terms, Acceptable use, Subprocessors, Cookies | done | `footer_content.tmpl` links the docs pages `stacks-and-operations`, `agents`, `cli`, `mcp`, the docs "New in ASC" list (`#docs-new`), `/-/status` and the About sections `#security`, `#contact`, `#privacy`, `#terms`, `#acceptable-use`, `#subprocessors`, `#cookies`; test `routers/web/asc_about_test.go` | - |
| Shell | "All systems operational" status in the footer | done | `modules/templates/asc_status.go` (`AscSystemStatus`, set from `asctrust.FooterStatus` in `routers/init.go`), `.ds-footer-status` in `footer_content.tmpl`, `/-/status` page. Audit 2026-11-05: found built | - |
| Shell | "Rendered in 18 ms" in the footer | done | Behind `ShowFooterTemplateLoadTime`; enable it in the asc `app.ini` template | S |

## Main (landing, signed out)

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Main | Hero: announcement pill, headline, lead, Download ASC / Create account / Sign in | done | `templates/asc/land_desktop.tmpl` and `land_mobile.tmpl` (generated from the boards, keys `asc_bl.d*` and `asc_bl.m*`); the pill names the version of the release the hub offers (`ASC 0.1.3: ...`) and drops the version without a release; test `TestAscLandingPillNamesTheRealRelease` | - |
| Main | Download ASC goes to `/download` and preselects the detected OS | done | Hero button is `href="#download"`, a section on the same page;; Audit 2026-10-30 (built): templates/home.tmpl:14,305 (href /download, detected OS in routers/web/asc_download.go) | S |
| Main | Install method pills (Homebrew, Scoop, winget, curl, PowerShell, .deb .rpm) as a labelled group with `aria-pressed`; the command swaps; Copy raises a toast; "1 Install ASC / 2 Connect to this hub" cards | done | Hero has only the connect command. Data exists in `modules/ascdownload/offer.go` (`Command` list per OS). Add pills and swap script (`home.tmpl`, `asc-ui.js`, pattern of `initDownload`) | M |
| Main | Product window in the hero (stack rail, change with intent, comment, whose turn) | done | No such markup in `home.tmpl`; static preview with a text equivalent | M |
| Main | Works-with strip | done | `land_desktop.tmpl` `blds49`, heading `asc_bl.d045` "Works with the editors and agents your team already uses" | - |
| Main | Why ASC, stacks, review, code intelligence, policy cards | done | `land_desktop.tmpl`: Why ASC with the three problem cards (`asc_bl.d052`-`d060`), How stacks work, review, code intelligence and policy sections as the board; the older sections are gone | - |
| Main | Operation log and merge queue demo | done | `home.tmpl` `ls-log` | - |
| Main | Agents band and run preview | done | `home.tmpl` `asc-agents-title` | - |
| Main | Workflow cards (dependency upgrade, flaky triage, large migration) | done | `home.tmpl` `ls-wf` | - |
| Main | Bring your own keys and storage (provider rows, S3 endpoint, bucket, region) | done | `home.tmpl` `ls-byo` | - |
| Main | Final call to action and footer | done | `land_desktop.tmpl` CTA `asc_bl.d173`/`d174` ("Install ASC in a minute, import a repository with its history..."); footer groups Product, Developers, Company, Legal and the status line in `base/footer_content.tmpl` | - |
| Mobile-Landing | Sticky bottom bar with Download ASC and Sign in; install pills scroll sideways; workflow cards snap | done | No sticky bar in `home.tmpl`; `asc-ds/touch.css` | S |

## Onboarding (sign in, sign up, recovery, device)

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Onboarding | Split layout: form card plus navy brand panel with promise and quote | done | `templates/user/auth/shell_start.tmpl`, `shell_end.tmpl` | - |
| Onboarding | Sign in form: username or email, Forgot password, password with Show toggle, Keep me signed in, CLI hint | done | `signin_inner.tmpl`, `ds-shell.js` `onRevealClick`; the CLI hint (`user/auth/cli_hint.tmpl`) teaches `asc clone` with no login step and the device code a private repository asks for | - |
| Onboarding | "Sign in with a passkey" (discoverable credentials) | done | `routers/web/auth/asc_passkey.go` (`GET /user/webauthn/passkey/assertion`, `POST /user/webauthn/passkey/login`, user verification required, one answer per challenge), button in `templates/user/auth/oauth_container.tmpl`, `web_src/js/features/asc-passkey.js`; registration in `routers/web/user/setting/security/webauthn.go` asks for a resident key with ASC on | - |
| Onboarding | Single sign-on button under the primary action | done | `oauth_container.tmpl` renders configured sources | - |
| Onboarding | Rate limit ("after 5 attempts we wait 5 minutes") | done | `services/auth/method/asc_login_limit.go` (`UserSignInFrom`: per typed name and address, counted before the password check, address cap across names; used by sign-in, link account, OpenID connect and basic auth), `modules/ascratelimit/lockout.go`, `[security] LOGIN_MAX_ATTEMPTS` and `LOGIN_WAIT` (`OPERATIONS.md`). In memory per process | - |
| Onboarding | Sign up: username with live "Your handle: @name" and blur validation | done | `signup_inner.tmpl` has a plain field. Add handler in `web_src/js/features/user-auth.js`; a name-availability route does not exist, add one in `routers/web/auth/auth.go` (then needs-hub-backend S) | M |
| Onboarding | Sign up: optional Full name | done | Not in `signup_inner.tmpl` or `forms.RegisterForm`; set `User.FullName` in `SignUpPost` | S |
| Onboarding | Sign up: strength meter and three live rules (12 characters, letters and numbers, not found in breaches) | done | `templates/user/auth/password_meter.tmpl` (rules from `MIN_PASSWORD_LENGTH` and `PASSWORD_COMPLEXITY`, breach rule with `PASSWORD_CHECK_PWN`), `web_src/js/features/asc-password.js` (SHA-1 in the browser, only the first five digits sent), `routers/web/auth/asc_password_range.go` (`GET /-/password/range/{prefix}`, padded range query, 30 per minute per address). `PASSWORD_CHECK_PWN` stays off by default so a hub without internet still signs people up | - |
| Onboarding | Sign up: "I agree to the Terms and the Privacy policy" | done | `signup_inner.tmpl` required box linking `/-/about#terms` and `#privacy` (or `[asc] POLICIES_URL`), `routers/web/auth/asc_terms.go` (refused without it on sign up and link account sign up), `models/user/asc_terms.go` (`asc_terms_acceptance`, migration `v18o`); test `tests/integration/asc_signup_terms_test.go` | - |
| Onboarding | Sign up has no retyped password on the board; the hub form asks for one | done | Drop the retype field (Show replaces it); relax `forms.RegisterForm` | S |
| Onboarding | Two-factor: six boxes act as one input and auto-advance; recovery code link; "Use a security key instead" | done | `twofa.tmpl` six-box field, "Use a recovery code", and "Use a security key instead" to `/user/webauthn` when the person has a key (`HasSecurityKey` in `routers/web/auth/2fa.go`); test `asc_twofa_test.go` | - |
| Onboarding | CLI device approval: code entry, confirm with device name, Authorise or Deny, expiry, wrong-code and rate-limit states; hub device-code store and `POST /login/device/code`, `POST /login/device/token` (RFC 8628) | done | `models/auth/asc_device_code.go`, `routers/web/auth/asc_device.go`, `templates/user/auth/asc_device.tmpl`; asc side is Phase 3 | - |
| Onboarding | Security key waiting screen with spinner and "Try another way" | done | `webauthn.tmpl`: waiting spinner and a "Try another way" disclosure with the authenticator code and recovery code (when the person has an authenticator) and sign in again; test `asc_twofa_test.go` | - |
| Onboarding | Authorise the CLI (device): code, terminal and OS, Authorise and Deny | done | Not present. The CLI signs in with a pasted token (`crates/asc/src/cli/auth.rs`: `--token-stdin`, `--prompt`), no device flow. Hub: device-code endpoints, template, token minting (`models/auth/access_token.go`). asc: `asc auth login` device grant with polling;; Audit 2026-10-30 (built): Gap file says not present; the device page and RFC 8628 routes exist (the token it mints is full-scope and has no expiry, see section 2). | L |
| Onboarding | Forgot password: sent page, lifetime, resend limit, no account disclosure | done | `forgot_passwd.tmpl` | - |
| Onboarding | Reset password: new and confirm | done | `reset_passwd.tmpl` | - |
| Onboarding | Registration closed (invite only) and awaiting approval | done | `signup_inner.tmpl` `DisableRegistration`; `RegisterManualConfirm` setting | - |
| Onboarding | Focus on first invalid field; autocomplete tokens | done | `shared/ds/field.tmpl` | - |
| Mobile-Onboarding | Sticky primary action above the keyboard-safe area; 48 px fields | done | `asc-ds/touch.css` (not device tested) | - |

## Install (first-run setup)

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Install | Setup form: database cards, general settings, administrator, email and service options | done | `templates/install.tmpl`, `install.js` | - |
| Install | Test connection with result ("Connected to PostgreSQL 16.4" or the cause); Install disabled until it passes for server databases | done | `POST /test-db` (`routers/install/asc_dbtest.go`), `install.js` | - |
| Install | Database error card with "Test again" and "Use SQLite instead" | done | `templates/install.tmpl`, `install.js`; the error names the field and the fix | - |
| Install | Sticky summary rail (Database, URL, Registration) and live step progress | done | `templates/install.tmpl` rail, `install.js` | - |
| Install | "Setting up your hub" progress page (Step n of 5) that polls, then redirects | done | `SubmitInstall` counts its steps (tables, repository root, administrator) for `GET /-/install/status` (routers/install/asc_progress.go, outside the session); install.js swaps the form for templates/install_progress.tmpl when it is sent and follows the route, then the post-install page runs the merge queue step until the hub answers and shows the ready state. `TestAscInstallStatus`, `TestAscInstallProgressMarkup`, install.test.js | M |
| Install | Post-install page: signed in as, `asc auth login --host ...` with Copy, Open the hub, Download ASC | done | `templates/post-install.tmpl`, `install.js` | - |
| Install | Skip administrator: first registered user becomes owner | done | The administrator card says so; install.js asks for all four fields once one is filled and refuses a skip with registration closed, as `SubmitInstall` does; `handleUserCreated` (routers/web/auth/auth.go) makes the only account an active administrator. `TestAscInstallAdministratorCard`, `TestAscInstallSkipNeedsOpenRegistration`, `TestAscFirstUserOwnsTheHub`, install.test.js | M |
| Mobile-Install | One section per screen with progress bar, Back and Next | done | `install.tmpl`, `install.js` stepper under 768 px | - |

## GetStarted (empty hub)

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| GetStarted | Four-step checklist with `aria-current=step`, done as a word | done | `templates/asc/get_started.tmpl` at `/-/get-started` | - |
| GetStarted | Shown on `GET /` for a signed-in person with no repositories; dismissible; returns while none exist | done | `routers/web/home.go` (`ascGetStartedOnHome`), `routers/web/asc_public.go` (`ascHasNoRepository`: no own, collaborative or organization repository; `POST /-/get-started/hide`). Hide is kept in the session, so the checklist returns at the next sign-in while no repository exists | - |
| GetStarted | Two parallel first-run pages (`asc/get_started.tmpl`, `user/asc_welcome.tmpl` with old `hub-` classes) | done | Merge into one page that follows the board; delete `user/asc_welcome.tmpl` and `routers/web/asc_welcome.go` | S |
| GetStarted | Step 2 ticks by itself when the first CLI sign-in arrives, no reload | done | `routers/web/asc_public.go` (`ascStartedOf`, `GET /-/get-started/status`, JSON, no-store), `templates/asc/get_started.tmpl` (`data-gs-watch`, `data-gs-part`, `data-gs-live`), `web_src/js/features/asc-welcome.js` (polls every 3 s while the tab is shown, then swaps the steps, progress and health card and announces it) | - |
| GetStarted | Import step with source tabs and progress "Reading history 12,408 commits" | done | Step 3 opens an inline form: tabs From a Git URL (in place), From GitHub or GitLab (`/repo/migrate`), Start empty (`/repo/create`). `POST /-/get-started/import` queues a private history-only migration (https only); the page asks `GET /-/get-started/import/status` until the task ends and shows the commits read (`routers/web/asc_get_started_import.go`, `web_src/js/features/asc-welcome.js`) | L |
| GetStarted | Health check (Git found, signed in, SSH key, shell shim; `asc doctor` on machine and server) | needs-both | Hub card lists email, SSH key and token only. `asc doctor` exists (`crates/asc/src/cli/doctor.rs`) but never reports to the hub: add a report call or show server checks only | L |
| GetStarted | "This hub" card: Storage, AI providers, Email with Change and Add | done | Data exists (`services/ascbyo`, settings); card missing | S |
| GetStarted | Shortcuts card | done | Static card; `t` and `?` need handlers (see Shell) | S |
| GetStarted | "Open the sample project" read-only | done | `services/ascsample` makes the sample on the first `POST /-/get-started/sample` (bot owner `asc-sample`, archived, four commits); `[asc] SAMPLE_PROJECT` turns it off | - |
| GetStarted | Empty-area cards: inbox zero on day one, no repositories, "just you" with Invite | done | Inbox day-one state (`asc_inbox.tmpl`, `DayOne`); dashboard rail cards "No repositories yet" and "It is just you for now" (`asc_home.tmpl`) | - |

## Download

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Download | Detected system card, recommended file, size, truncated sha256, "Not your system?" and OS sections | done | `templates/download.tmpl`, `asc-ui.js` (client hints), `modules/ascdownload` | - |
| Download | Package manager cards per OS with Copy | done | `offer.go` `PackageCommands`, `download.tmpl` | - |
| Download | Files table with size, sha256 (copy whole, full value in title), `.minisig` link | done | `download.tmpl` | - |
| Download | Copy confirms with an inverse-surface toast ("SHA-256 copied") | done | `clipboard.js` shows a tooltip; board uses a toast. Restyle `modules/toast.js` to the board and use it here and on Main, GetStarted, Docs | S |
| Download | Connect card, access-token alternative, Verify your download, release key | done | `download.tmpl` aside | - |
| Download | Editor extensions | done | `download.tmpl` | - |
| Download | System requirements card | done | No `system_req` string; add to the aside | S |
| Download | Release notes link, Older releases | done | "Older releases" links the GitHub releases page, or for a local or fetched release the manifest's `releases_url` (https only, `modules/ascdownload/manifest.go`); test `TestAscDownloadPage` | - |
| Download | States: No release yet (build from source, admin note), system not detected | done | `download.tmpl` | - |
| Download | Loading releases skeleton | done | Not shown by design: the page is server rendered and the release is read and cached on the server (`ascdownload.Cached`, `SharedGitHubSource`), so no client-side loading state exists; a skeleton would show a state that never happens | - |
| Download | "Signature check failed" and "verified" banners | done | Guidance in the Verify card under the command: what a verified and a failed `minisign -V` mean, the failed one says delete and do not run (`download.tmpl` `[data-asc-dl-outcome]`, keys `asc_download.sig_*`); test `TestAscDownloadPage` | - |
| Mobile-Download | Sticky bottom bar "Download for your system"; files as cards | done | `download.tmpl` `[data-asc-dl-sticky]` (Download for the system, More); under 640 px each file row is a card with kind and size, hash with Copy and a signature icon, Download (`site.css`) | - |

## Explore: repositories

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Explore | Tabs Repositories, Users, Organizations with counts | done | `explore/navbar.tmpl` | - |
| Explore | Filter bar: search, Sort, Visibility, kind pills; `/` focuses search | done | `shared/repo_search.tmpl`, `repo-search.js` | - |
| Explore | Language filter menu | done | `.Language` is read from the query but no menu lists languages. Menu from language stats (`models/repo/language_stats.go`) in `repo_search.tmpl` | M |
| Explore | Quick chips All, Has open stacks, Agent activity, Starred, Templates, Archived | done | Built on every repository list that uses `shared/repo_search.tmpl` (Explore, organization, profile, Watching): `open_stacks`, `agents` and `starred` are read by `routers/web/shared/repoquick` and filtered by `AscQuickFilter` (`models/repo/asc_quick_filter.go`): an open pull request; an action of a bot account (an agent) in the last 7 days; starred by the viewer (signed in only). Search, pager and the phone sheet keep them; the no-result state names the chip with a remove link. Forks and Mirrors moved to the sheet only. Docs: AGENTS-UI.md "Agent activity in the repository lists"; tests: TestTheQuickChipsOfTheRepositoryLists, TestTheQuickChipsOfExplore | - |
| Explore | List and Grid layout toggle; layout persists per person | done | No layout toggle anywhere (density exists in the account menu). Toggle, grid CSS, `localStorage` in `repo_search.tmpl`, `repo_list.tmpl` | M |
| Explore | Row: owner avatar, name, visibility chip, description, topics, language, stars, updated, open-stacks chip | done | `explore/repo_list.tmpl` | - |
| Explore | Agent activity chip on a row | done | asc `Agents.CountRuns` answers `per_repo` and counts `since` a time; `routers/web/shared/repoquick/agents.go` asks once per 50 rows for the asc repositories the viewer can read (agent features on), over the 7 days of the Agent activity quick chip, cached 60 s; `templates/explore/repo_list.tmpl` draws **N agent runs** linked to the runs of the repository | M |
| Explore | Changeset count on a row ("3,812 changesets") | needs-both | Row shows stars and forks only. asc has per-repo change data but no batch stats RPC; add `RepoStats` or cache in `services/ascsync` | M |
| Explore | Contributor avatar stack on a row (people and agents) | done | `models/activities/asc_contributors.go` ranks who acted in each repository of the page in the last 90 days (one count per action, agents as their bot accounts); `routers/web/shared/repoquick` hides profiles the viewer may not see and keeps four; `templates/explore/repo_list.tmpl` draws the stack with an agent ring | M |
| Explore | Row actions on hover or focus-within: Copy clone command, Star | done | Neither on the row. Buttons with `data-clipboard-text` (`asc clone host/owner/name`) and the existing star action | S |
| Explore | Right rail: Trending this week (changesets this week), Popular topics, Clone anything you can see | done | `templates/explore/rail.tmpl` (Trending this week, Popular topics, clone card), `models/repo/asc_trending.go` (weekly count, refreshed by a cron task). Audit 2026-11-05: found built | - |
| Explore | States: loading skeleton, empty hub (New repository, Import), no results with removable filter chips and Clear filters, error with request id and Try again | done | `explore/repo_list.tmpl`: "No repositories yet" with New repository and Import a project (signed in); "Nothing matches" with the count of active filters, the removable chips and Clear filters; a failed search keeps the page and shows "We could not load this list" with the request id, Try again and Status page (`routers/web/explore/repo.go`, status 500). No skeleton: the list is server rendered. Tests `TestAscExploreLoadFailed`, `TestAscExploreEmptyHub`, `TestAscExploreNoMatch` | - |
| Explore | Pagination "Showing 1 to 8 of 128" | done | `base/paginate.tmpl` | - |
| Mobile-Explore | Sort and filter bottom sheet with result count on the primary button | done | `repo_search.tmpl` | - |
| Mobile-Explore | Pull to refresh; infinite scroll after 30 rows with Load more fallback | done | `templates/shared/ds/more.tmpl` in explore/repos.tmpl and users.tmpl; `web_src/js/features/asc-explore-more.js` loads the next pages near the list end on a phone (Load more with data saver, no observer, or a failed load); pull to refresh by `asc-pull-refresh.js` | - |

## Explore: users

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| ExploreUsers | "People and agents" header and Invite people button | done | `explore/navbar.tmpl` title `explore.users_title`; Invite people from `inviteHref` in `routers/web/explore/user.go`: Add account for an administrator, a mailto with the sign-up link for others where sign-up is open, none when signed out or sign-up is closed | - |
| ExploreUsers | Search by name, handle or role | done | `explore/users.tmpl` (role not searchable until roles exist) | - |
| ExploreUsers | Show filter (People and agents / People / Agents), Organization filter, Sort "Most active" | done | `templates/explore/search.tmpl` has Show, Organization (any organization the viewer may see, with suggestions) and Most active; `models/user/asc_search.go` (`AscUserFilter`, `AscMostActiveOrder`) | M |
| ExploreUsers | Chips All, People, Agents, Maintainers, Online now | needs-both | None present. Agents are not hub users (they are tokens and runs in `proto/asc/v1/agents.proto`); Maintainers needs a role; Online now needs presence | L |
| ExploreUsers | Role chip (Owner, Maintainer, Member, Agent) and "Roles on this hub" card | done | `models/user/asc_roles.go` reads the role (Owner = site admin, Maintainer = write to a repository of someone else, Agent = bot account, Member) and counts the accounts the viewer may see; chips and the rail card in `user_list.tmpl` and `users.tmpl`, for signed-in viewers only | M |
| ExploreUsers | Agent rows: violet square avatar, `@org/name`, owner, scope, View runs | needs-both | asc has repo-scoped `ListTokens` and `ListRuns`; no hub-wide agent directory. asc: `ListAgents(owner)` RPC; hub: row type in `user_list.tmpl`, links to `/-/asc/agents` | L |
| ExploreUsers | Meta row: location, joined, organizations, public email as own labelled item | done | `user_list.tmpl` meta row with icons; organizations from `loadExploreUserRows` in `routers/web/explore/user.go` | - |
| ExploreUsers | Activity sparkline (12 weeks, text equivalent), counts "changesets, reviews" | done | `models/activities/asc_weekly.go` counts the public actions per week (public repositories of public owners, one count per action), the pull requests opened and the reviews given; `templates/explore/asc_activity.tmpl` draws the line with the same fact as its label; a person who keeps activity private shows none | M |
| ExploreUsers | Hover action Follow; View runs for agents | done | Row Follow/Unfollow form in `user_list.tmpl` posting to the profile action with `redirect_to`; View runs for agents moves with the agent rows (row 169) | - |
| ExploreUsers | Rail "Who is active now" with presence and current activity | needs-both | No presence model. Hub: last-seen heartbeat (`models/user`); asc: run state for agents (`GetRun`) | L |
| ExploreUsers | Grid layout | done | With the layout toggle (Explore) | M |
| ExploreUsers | States: loading, no people match, filter chips, error | done | Error, no-match with chips and empty states in `user_list.tmpl`; loading not shown, server rendered | - |
| Mobile-ExploreUsers | Chips filter People, Agents, Maintainers, Online now; Follow inside the row at 44 px | needs-both | As above | M |

## Explore: organizations

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| ExploreOrgs | Org row: avatar, name, description, repositories and people counts | done | `explore/user_list.tmpl` (`IsOrganization`) | - |
| ExploreOrgs | Verified badge (domain proof) and explainer card | done | No flag. Add a verified field and domain proof (DNS TXT or well-known file) in `models/user`, migration under `models/migrations`, owner settings, a check job; chip in `user_list.tmpl` | L |
| ExploreOrgs | Agents count; member stack with up to four avatars and +N | needs-both | Members: `organization.GetOrgUsersByOrgID` (hub). Agents count: asc, as for Explore users | M |
| ExploreOrgs | 12-week sparkline and "Active 12 min ago" | done | `AscOrgActivity` in `models/activities/asc_weekly.go` counts the actions in the public repositories of the organization and keeps the latest; the row shows the line and "Active" with a relative time | M |
| ExploreOrgs | Chips All, Verified, Has agents, I am a member; Size filter; Sort most active | done | `models/user/asc_org_search.go` adds the filters to the account search (agents count only memberships the viewer may see) and the Most active order (public repositories of public organizations, without people who keep activity private); `routers/web/explore/org.go` reads `only` and `size`; `templates/explore/search.tmpl` shows the chips and the Size select | M |
| ExploreOrgs | View hover action; Start an organization card; grid layout; states | done | View row action and empty, no-match and error states in `explore/user_list.tmpl`; Start an organization and verified explainer cards in `explore/users.tmpl`; List and Grid via `shared/ds/layout_toggle`; loading not shown, server rendered | - |

## Docs

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Docs | Docs home: search, Popular chips, six entry cards, "New in ASC 1.4" list, Need a person card | done | `templates/asc/docs.tmpl` has six cards and a help block; no search, popular or what's new | M |
| Docs | Docs search with grouped results (Commands, Guides), did-you-mean, no results, loading; Ctrl K or `/` opens it | done | `services/ascdocs/commands.go` (commands from the CLI reference, `Suggest` within two edits); `templates/asc/docs_results.tmpl`; `/-/docs?q=&fragment=results` loaded as you type by `asc-docs-search.js`, which also takes Ctrl K and `/`; `TestAscDocsSearch*`, `commands_test.go`, `asc-docs-search.test.js` | - |
| Docs | Article layout: sidebar tree, 720 px column, breadcrumb, steps, callouts, Previous and Next | done | `asc/docs_page.tmpl`: the guides by section in a nav beside the article (`aria-current="page"`, scroll kept per tab by `keepSideScroll` in `asc-docs.js`), a 720 px column with breadcrumb, lead, reading time, last change (`content/updated.json`, written by `make docs-sync`) and hub version; GitHub callouts keep their bold label; a numbered list is a step list; Previous (the docs home on the first guide) and Next; `TestAscDocsArticleLayout` | - |
| Docs | OS code tabs remembered; Copy without the prompt | done | `GET /-/docs/install` (`asc/docs_install.tmpl`): the steps of the release the download page offers, one tab for each system with the visitor's first; `initDocTabs` keeps the choice under `data-doc-remember` in `localStorage` (Home and End keys); every command sits behind an `aria-hidden` prompt and Copy copies the commands alone; verify step from the manifest's key; connect step `asc auth login --host <hub>`; without a release, build from source | - |
| Docs | On-this-page TOC highlighting the section in view; Edit this page; Open in the hub | done | `initAscDocs` in `asc-docs.js` marks the section in view; "On this page" is a nav; Edit this page opens the source file. Open in the hub has no place here: the hub itself serves the docs | M |
| Docs | "Was this page helpful?" Yes and No | done | `asc/docs_page.tmpl` section `#site-feedback` with "Feedback goes to the docs team"; `POST /-/docs/{slug}/feedback` (`routers/web/asc_docs_feedback.go`, ten a minute per address) stores yes or no in `asc_docs_feedback` (migration v18m, no reader kept); `asc-docs-feedback.js` sends without leaving the page and remembers the answer; site administrators see the counts | - |
| Mobile-Docs | Docs menu drawer, sticky "On this page" row, sticky next-page bar | done | `asc/docs_page.tmpl`: Docs menu button before the breadcrumb opens `#site-docs-drawer` (every guide by section from `ascdocs.Sections`, a dialog run by `ds-shell.js` with focus return, `:target` without the script); sticky On this page row and the next-guide link | - |

## About

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| About | Hero, story with before and after table, principles, open source chips, data ownership, contact | done | `templates/asc/about.tmpl`, route `GET /-/about` (`web.go` line 654) | - |
| About | Header About and footer link go to `/-/about` | done | Header still links to the GitHub copy of `WHY.md` (see Shell) | S |
| About | Source code, Security policy and Contact targets | done | Source and security policy link the ASC Hub repository; `[asc] CONTACT_EMAIL`, `SECURITY_EMAIL` and `POLICIES_URL` (`modules/setting/asc.go`, OPERATIONS.md) set the contact, report and policies targets, with the repository issues and private advisories when empty | - |

## Errors

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Errors | 404, 403, 413, 500, 503, offline cards with header and footer | done | `templates/status/*.tmpl` | - |
| Errors | Request id ("Request id 7f3a91c2 · date UTC", selectable) | done | No id in any status template. Generate per request in `services/context`, log it, show in `500.tmpl`, `503.tmpl`, error JSON | M |
| Errors | 500 actions: Try again (same request id) and Status page | done | `500.tmpl` offers Try again and Status page; the status page is /-/status (`AscStatus`, `asc/status.tmpl`, the probe of the footer in `services/asctrust/status.go`), linked from the footer, 500, 503 in maintenance and the package list error | S |
| Errors | 403 actions: Request access and Switch account | done | `403.tmpl` offers Request access (reader to write, writer to admin, with a message) and Switch account; the admins grant or dismiss on Settings > Collaborators (`asc_access_request`, OPERATIONS.md "Requests for access") | L |
| Errors | 404 actions: dashboard, search the hub | done | `404.tmpl` | - |
| Errors | 413 card with limit, artifact storage link and Back | done | `413.tmpl` | - |
| Errors | 503 maintenance: version being upgraded, reads continue through the CLI, window, expected back, status link | done | Hub: Site administration, Maintenance (`routers/web/admin/asc_maintenance.go`, `templates/admin/asc_maintenance.tmpl`) keeps message, window, expected back, status page and the version; `ascMaintenanceGuard` (`routers/web/asc_maintenance.go`) answers browser writes with 503 (`templates/status/503.tmpl` shows "Upgrading to", window, expected back, reads go on, status link) and `/-/status` shows the banner. asc: the Maintenance service (`maintenance.v1`, SPEC §14.5) makes every node read-only; when asc has it the hub switches asc first and changes nothing if asc refuses, so pushes from the command line are refused with the same message; the page shows the state of asc serve and a staged restore; tests TestAscMaintenanceBoard, TestAscMaintenanceSwitchesAscFirst, TestAscMaintenanceRefusesBeforeCallingAsc, TestAscMaintenanceNotLinked, TestMaintenancePageShowsTheVersion | - |
| Errors | Offline page: "your edit is saved on this device", Retry now, served by a service worker | done | `/-/offline` (`status/offline.tmpl`) from the worker `/-/asc-sw.js`, which keeps its styles and scripts; Retry now; the kept-edit line when `asc-drafts.js` holds a draft. Drafts of the markdown editors are kept on the device, and a form sent offline is held with the note (template in `base/head_navbar.tmpl`) and sent when the connection returns | - |

## Dashboard

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Dashboard | Greeting by time of day and summary line | done | `routers/web/user/asc_dashboard.go` | - |
| Dashboard | Work list with counts (Inbox, My stacks, Review requests, Agent runs), repositories, teams | done | `templates/user/dashboard/asc_home.tmpl` | - |
| Dashboard | Pinned repositories per person; counts update live | done | `ascDashRepos` lists the person's profile pins first (read-checked, archived left out), then the latest; `GET /-/asc/dash/counts` refreshes Inbox, My stacks, Review requests and Agent runs with the Today card (`asc-dash-live.js` `refreshCounts`; `TestAscDashReposPutThePinsFirst`, `TestAscProfilePins/Dashboard`) | M |
| Dashboard | Scope chips Everything / org / Personal filter the whole page, remembered | done | None in the template. `?scope=` in `ascDashBuild` filtering inbox rows, agents, stacks; persist as a user setting | M |
| Dashboard | Needs-you rows with chips (Agent, checks, Touches billing), diff size, age | done | `asc_home.tmpl`, `ascDashRowFor` | - |
| Dashboard | Rows act in place: Approve, Reply, Answer (agent question), Add to merge queue, Resolve | needs-both | Buttons are links (`aria-hidden`, tabindex -1) to the review. Hub: POST handlers for approve (review in `routers/web/repo/pull.go`), queue (`asc_queue.go`, asc `Enqueue`), answer (asc `SendInput`), htmx row swap | L |
| Dashboard | Row kinds: reply from a person, agent question, ready to merge with queue position and estimate, trunk moved with restack summary | needs-both | Kinds come from inbox sections; reply quote, "about 4 min" and "ASC restacked 2 of 3" need asc data (`mergequeue.proto` `List`, `stacks.proto` restack result); `services/ascinbox` | L |
| Dashboard | Keys j k, Enter, e (done), g then i, `/` | done | `asc-keys.js`: j k, Enter on the row link, g i and `/`; e on the focused row sends its hidden state form (`user/asc_row_state.tmpl`, `asc-row-swipe.js`), back to the dashboard | - |
| Dashboard | Agents working now with budget bar, paused state, All runs | done | `asc_home.tmpl`, `ascDashAgentsFromServe` | - |
| Dashboard | Stack bars: click a segment opens the review at that segment | done | `wk-segs` items are not links; `ascDashStackFrom` has the pull link per segment | S |
| Dashboard | Merge queue card (Running, median wait, positions, "2 of 3 checks", Open) | needs-both | No such card. Hub aggregates the person's queue entries (`routers/web/repo/asc_queue.go`; asc `mergequeue.proto` `List`, `Get`, `Watch`); median wait needs an asc statistic | L |
| Dashboard | Today feed with Filter | done | Feed present; filter menu missing | S |
| Dashboard | From your terminal card with Copy; g i tip | done | `asc_home.tmpl` | - |
| Dashboard | Live updates | needs-both | Counts load once. Hub: poll `/-/asc/inbox/count` or reuse `eventsource.sharedworker.js`; asc `WatchRefs` / queue `Watch` for pushes | L |
| Dashboard | States: loading skeleton, new account, offline error with retry countdown | done | `user/dashboard/asc_home.tmpl`: the main column is `data-asc-list-main` with a three-row skeleton a scope chip shows (`asc-list-loading.js`); new account and inbox zero empty states; `user/dashboard/asc_today.tmpl` Today card refreshes every minute from `GET /-/asc/dash/today` (`asc-dash-live.js`: not while hidden or in use) and on a failed load keeps its items and shows "Offline. Activity could not load." with Try again and "Retrying in N s" (8, 16, 32, 60 s), the `online` event retries at once | - |
| Dashboard | Undo toast ("Added to merge queue. Undo for 10 s") | needs-both | `toast.js` has no action button; asc `Dequeue` exists for undo; goes with row actions | M |
| Dashboard | Appearance setting hides agent surfaces | done | `ascagentui.FeaturesEnabled` | - |
| Mobile-Dashboard | Pull to refresh; swipe a row left to snooze, right to mark done | done | `asc_home.tmpl`: `data-asc-pull-refresh` with the arrow (`asc-pull-refresh.js`, installed app); `asc-row-swipe.js` on `data-asc-swipe` rows: right marks done, left snoozes 4 h through `POST /-/asc/inbox/state` with `back=dash` | - |

## Inbox

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Inbox | Tabs Needs you, Waiting on others, Agents, Done with counts (hub adds Drafts) | done | `routers/web/user/asc_inbox_page.go`, `asc_inbox.tmpl` | - |
| Inbox | Filters: All repos, Humans and agents, Sorted by | done | Repo select works; "Humans and agents" missing; "Sorted by" is static text (`wk-sort`). Add `kind` and `sort` in `ascInboxBuild` | S |
| Inbox | Unread dot plus bold title | done | `is-unread` rows from the person's unread notifications (`ascInboxMarkUnread`), opening a row reads it; `TestAscInboxUnread` | - |
| Inbox | Row hover actions (Review, Answer, Mark done, Snooze) | done | `templates/user/asc_inbox.tmpl` `[data-asc-row-acts]`: Review, Answer (agent rows that wait), Mark done or Restore, Snooze 4h, each posting to `/-/asc/inbox/state`; tests in `routers/web/user/asc_inbox_test.go` | S |
| Inbox | Select rows (x, checkbox); bulk bar "2 selected: Mark done, Snooze, Unwatch" in a live region | done | `asc_inbox.tmpl` bulk bar: ticked box (clears, as Escape does), count in a polite live region, Mark done, Snooze, Unwatch (`ds-bulk.js`); `AscInboxStatePost` `do=unwatch` turns the issue watch off and moves the item to Done | - |
| Inbox | Mark done (e) with fade and undo toast; Done keeps 30 days and can be restored | done | `done_unix` on `asc_inbox_state` (migration `v18l_asc_inbox_done.go`); `SetAscInboxDone`; `do=done` and `do=restore` in `AscInboxStatePost`; an item comes back when the pull request changes; the Done tab lists it for `AscInboxDoneKeep` (30 days); fade and once-only send in `initAscInboxDone` (`asc-keys.js`); undo toast `[data-asc-undo]` | M |
| Inbox | Snooze menu: Later today 17:00, Tomorrow 09:00, Monday 09:00, When the checks finish, Pick a date | needs-both | Menu has 1 h, 4 h, tomorrow, week. "When the checks finish" needs a check-completion wake (asc `ci.proto` events); date picker is UI | M |
| Inbox | Detail pane: why waiting, changed since last look, policy line (approvals 1 of 2), thread, reply box with Approve and Request changes | done | `asc_inbox.tmpl` | - |
| Inbox | "Ask agent to change" (comment becomes an instruction for the author agent) | needs-both | Reply form has Approve, Request changes, Reply only. Hub: POST to `CreateRun` or `SendInput`; asc: instruction flag on review comments (`review.proto` `AddComment`) | L |
| Inbox | Keys j k, x select, e done, s snooze, a approve, r request changes, p pin, Enter | done | `asc-keys.js`: j k, e (focused row or open item), s, p, g i; x in `ds-bulk.js`; a and r put the focus on Approve and Request changes of the reply form (Enter sends); Enter opens the row link | - |
| Inbox | Pagination "Showing 8 of 23" | done | None in `asc_inbox.tmpl`; list is capped (`ascInboxInfoRows`) | M |
| Inbox | New items slide in from the top without jumping the list | needs-both | No live update. Hub poll or SSE; asc `inbox.proto` `Count` is poll friendly | M |
| Inbox | States: bulk bar, inbox zero with Watch more repositories, loading skeleton | done | Empty exists; the other two missing | S |
| Inbox | Subscriptions link to Settings, Notifications | done | `/notifications/subscriptions` | - |
| Mobile-Inbox | Swipe right to mark done, left to snooze; sticky action bar with Open next | done | `asc-row-swipe.js` on `.wk-ilist` rows: right marks done, left snoozes 4 h; the sticky Open next bar is `ds-sticky-actions` in `asc_inbox.tmpl` | - |

## Profile

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Profile | Header: avatar, name, handle, location, bio, followers, following, joined, website, Follow | done | `templates/user/overview/header.tmpl`, `shared/user/asc_profile_band.tmpl` | - |
| Profile | Presence pill ("Online, local time 14:20") | done | `profile_big_avatar.tmpl` pill from `ascpresence.Of`: online from the last request, local time in the zone of the profile behind its own privacy switch (off by default); OPERATIONS.md "Presence on profiles" | M |
| Profile | Role line ("Staff engineer · Payments team · London") | done | Role is the `asc.role` user setting (Role field in profile settings, 60 characters, trimmed, empty clears it); the line in `profile_big_avatar.tmpl` adds role, the first team the viewer may know about (`AscProfileTeams`) and location; `tests/integration/asc_profile_role_test.go` | S |
| Profile | Request a review (picker with repository and change) and Mention | needs-both | Absent. Hub: picker modal of the viewer's open pull requests (`routers/web/user/profile.go`) and POST to asc `RequestReview` (exists in `review.proto`) via `asc_rpc_proxy.go`; Mention opens a comment composer | L |
| Profile | Tabs Overview, Work (11), Reviews (34), Repositories (9), Activity | done | Tabs today: Overview, Repositories, Projects, Packages, Code, Activity, Stars (`overview/header.tmpl`). Add Work (`ascProfileWork`) and Reviews with counts | M |
| Profile | Working-now list (stack, draft, ready) | done | `profile.tmpl`, `ascProfileWork` | - |
| Profile | Pinned repositories (up to six) with Customize dialog and drag reorder | done | Cards show stacks and changes; the owner drags cards (saved at once) or opens Customize as a dialog with Move up and Move down and an announced position (`templates/user/settings/pins_form.tmpl`, `asc-pins.js`, `POST /user/settings/pins` with `inline=1` answers 204; `TestAscPinSettingsPostInline`, `TestAscProfilePins`, `asc-pins.test.js`) | M |
| Profile | Contributions: year switch, summary (changes, reviews, median turnaround), violet cells for agent-authored changes, cell opens the day, table alternative | needs-both | One-colour grid exists (`asc_heatmap.tmpl`) with a text total. Agent attribution needs asc (changeset author kind via `changes.proto`); turnaround from review timestamps | L |
| Profile | "Agents Priya runs" with New agent | needs-both | Not on the profile. Hub must list the person's agents (see Explore users) with scope and counts | L |
| Profile | Recent activity | done | `profile.tmpl` feed | - |
| Profile | "Knows best" (path share of changes and reviews, six months) | needs-both | No code. Aggregate asc `LastCommits` / blame data per user: new RPC or hub job | L |
| Profile | Teams and Focus chips beside Repositories and Organizations | done | Teams card (`wk-pteams`, members only) and Focus card (`[data-asc-pf-focus]`): the topics and main language of up to 12 readable repositories the person changes, weighted by their pull requests, six at most, each linked to Explore (`ascProfileRepos`, `ascFocusTally`); tests `routers/web/user/asc_profile_focus_test.go`, `tests/integration/asc_profile_focus_test.go` | S |
| Profile | States: owner view (Edit profile, View as visitor), private-profile banner, empty pins and contributions, loading skeleton | done | `shared/user/profile_big_avatar.tmpl` Edit profile and a View as visitor toggle (`?visitor=1` renders the page as signed out, `aria-pressed`); `shared/user/header.go` `AscPrivateLimited` hides the details and stats of a private person from a viewer who is not them or an admin and the private banner shows, every tab shows the shared repositories; one empty row when there are no pins and no contributions, with Pin repositories for the owner; `data-asc-list-main` skeleton (`work-common.css` `wk-list-loading`) | - |

## OrgProfile

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| OrgProfile | Head: avatar, name, description, location, website, Follow, Org settings | done | `templates/org/header.tmpl`, `follow_unfollow.tmpl` | - |
| OrgProfile | Verified badge; "38 people, 4 agents" counts | needs-both | See ExploreOrgs | M |
| OrgProfile | Tabs Repositories, People, Teams, Packages, Projects with counts | done | `org/menu.tmpl` | - |
| OrgProfile | Repo rows with check status text (Passing, 1 failing, No checks) and a Watch toggle with count | done | `repoquick.LoadChecks` and `LoadWatching` (`routers/web/shared/repoquick/rowstate.go`) fill `explore/repo_list.tmpl` from `routers/web/org/home.go`; see OPERATIONS "Checks and Watch on organization rows" | M |
| OrgProfile | Type and Sort menus land in the URL | done | `shared/repo_search.tmpl` | - |
| OrgProfile | Side cards: People, Teams, Agents, Merge policy with "Read the policy" | needs-both | People and Teams exist (`org/home.tmpl`); Agents and Merge policy missing. Policy from asc `GetPolicy` (repo level) or an org policy; agents as above | M |
| OrgProfile | Member view shows Org settings; lock chip for private repositories | done | `org/header.tmpl` | - |
| OrgProfile | Loading skeleton; mobile row menu | done | `org/home.tmpl` (`data-asc-list-main` with a hidden skeleton card; the search, a sort, a kind pill and the pager show it, `asc-list-loading.js`, `repo-search.js`); `explore/repo_list.tmpl` row menu under 768 when `AscRowMenu` is set (`asc-row-menu.js`: one open at a time, Escape and a tap outside close it) | - |

## Mobile shell (Mobile-Menu)

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Mobile-Menu | Top bar, five-item bottom nav (Inbox, Changes, Explore, Search, Me) with counts in the label, drawer, account sheet, full-screen palette | done | `head_navbar.tmpl` (`.ds-topbar`, `.ds-bnav`, `#ds-drawer`) | - |
| Mobile-Menu | Swipe from the left edge opens the drawer | done | web_src/js/features/ds-shell.js edgeSwipe + touch handlers (test: ds-shell.test.js) | - |
| Mobile-Menu | Create as a bottom sheet (drawer footer and list headers) | done | web_src/css/asc-ds/layout.css .ds-pop is a bottom sheet under 1024 px; templates/base/head_navbar.tmpl #ds-create | - |
| Mobile-Menu | Notifications sheet | done | The Inbox tab of `.ds-bnav` (`head_navbar.tmpl`) opens `#ds-bell` as a bottom sheet (ds-shell.js); full-width Open inbox in `layout.css`; tests: ds-shell.test.js, TestThePhoneInboxTabOpensTheNotificationsSheet | M |
| Mobile-Menu | Palette filter chips scroll horizontally | done | With the scope chips | S |

## Cross-group notes

- Palette results, Inbox "Ask agent to change" and Dashboard row actions need the same hub piece: JSON or htmx review actions in front of `asc_rpc_proxy.go`. Build once.
- Agents as hub-listed identities (Explore users, Profile, OrgProfile, Dashboard scope, Explore repo chip) share one missing asc piece: a hub-wide agent directory RPC in `proto/asc/v1/agents.proto`.
- The sparkline (Explore users, orgs, repositories on mobile) is one component: `templates/shared/ds/sparkline.tmpl` plus a cached weekly series.
- The inbox "done" state gates Dashboard `e`, Inbox bulk bar, mobile swipes and the Done tab.
- The dashboard SPA mapping in the board annotation (`web/src/routes/dashboard-desktop.tsx`) points at the asc repo's local web app, not the hub; the hub dashboard is the Go template above.
