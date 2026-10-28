# Feature gaps: the shell boards

Audit of the group "shell" against the design boards in `project-v3/` (each board's markup and its annotation block: states, interactions, accessibility, maps-to). Method: the board text was extracted and every visible feature or behaviour was listed, then checked by reading the hub (templates, routers, models, services, `web_src/js`) and the asc protocol (`proto/asc/v1/*.proto`, crates). The dev hub was not run. Date of audit: 2026-10-28, hub branch `asc/v16`.

Boards: Shell (header, menus, palette, notifications, footer), Main (landing), Onboarding, Install, GetStarted, Download, Explore, ExploreUsers, ExploreOrgs, Docs, About, Errors, Dashboard, Inbox, Profile, OrgProfile, and the Mobile- twins (mobile-only behaviour is listed under the board it belongs to or under "Mobile shell").

Status words: **done** (present and behaves as the board), **ui-only-missing** (template, CSS or JS only), **needs-hub-backend** (new or changed hub Go code, model or migration), **needs-asc-backend** (new asc RPC or crate work only), **needs-both**. Size: S under half a day, M one to two days, L three days or more.

"asc has no X" means a search of `proto/asc/v1` found no RPC for it. Existing RPCs a feature can reuse are named. Items marked done were confirmed by reading the template or script; CSS timings and contrast were not re-measured.

## Counts

| Status | Rows |
| --- | --- |
| done | 77 |
| ui-only-missing | 47 |
| needs-hub-backend | 44 |
| needs-asc-backend | 0 |
| needs-both | 26 |
| total | 194 |

Open rows by size: S 45, M 64, L 23.

## Shell: header, menus, palette, notifications, footer

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Shell | Signed-out header: Explore, Download, Docs, About, quiet Sign in, cyan Create account | done | `templates/base/head_navbar.tmpl` | - |
| Shell | Docs and About links (header and footer) open the hub's own pages (`/-/docs`, `/-/about`, both routed and templated) instead of GitHub blobs | done | `head_navbar.tmpl` (`$docsURL`, `$aboutURL`), `footer_content.tmpl`; board maps About to `GET /-/about` | S |
| Shell | Signed-in header: Changes, Agents, Explore, Download, search field with `/` and Ctrl K hints, Inbox, Create, Account | done | `head_navbar.tmpl` | - |
| Shell | Active link: white label, 2px cyan marker, `aria-current=page` | done | `head_navbar.tmpl`, `web_src/css/asc-ds` | - |
| Shell | Inbox amber count from `/-/asc/inbox/count`, cached 60 s | done | `asc-keys.js` `initAscInboxBadge` | - |
| Shell | "N agents running" pill in the header (cyan dot, links to Agents) and in the mobile top bar | needs-both | Not in any template or locale. Hub: `GET /-/asc/agents/count` beside `AscInboxCount`, pill in `head_navbar.tmpl` (desktop and `.ds-topbar`), poll in `asc-keys.js`. `ascAgentsActive` fans out to 8 repos with a 4 s timeout: too heavy for every page, so asc needs a cheap per-user run count (`agents.proto`, `ListRuns` without repo or a `CountRuns`) | M |
| Shell | Notifications bell opens a popover (unread count, Mark all read, items with avatar, text, repo, age, Open inbox) | needs-hub-backend | Bell is a plain link to `/notifications` (`head_navbar.tmpl`). Add a fragment route in `routers/web/user/notification.go` (reuse `NotificationStatusPost` for Mark all read), a popover in `head_navbar.tmpl`, open and close in `ds-shell.js`. Items like "coder-2 finished run 214" need agent and queue events to create hub notifications, which nothing does today | M |
| Shell | Account menu: identity, profile, inbox with count, agents, settings, Appearance (Light, Dark, Auto), Density (Comfy, Compact), Documentation, Sign out | done | `head_navbar.tmpl` `#ds-account`, `head_style.tmpl` | - |
| Shell | Theme and density persist per person: localStorage, then the account setting | needs-hub-backend | Only `localStorage` (`asc-theme-mode`, `asc-density`) in `head_style.tmpl`. Add a user setting (`models/user/setting.go`), save on change (fetch to `routers/web/user/setting`), render `data-density` server side | S |
| Shell | Create menu: New repository, Import a project, New organization, New changeset, Start an agent run | ui-only-missing | `#ds-create` has the first three. Add "New changeset" (repo and stack picker) and "Start an agent run" (`/-/asc/agents`) when `AscEnabled` | S |
| Shell | Menus close on Esc or outside click, return focus to trigger, fade and rise, reduced motion | done | `ds-shell.js`, `asc-ds/components.css` | - |
| Shell | Command palette opens from `/`, Ctrl K, header search, mobile search; Esc, arrows, Enter | done | `ds-shell.js`, `#ds-palette` | - |
| Shell | Palette is a combobox: `role=combobox` input, listbox results, `aria-activedescendant` | done | Today `role=dialog` plus `role=search` form and an `.is-sel` class. Change markup in `head_navbar.tmpl` and `select()` in `ds-shell.js` | S |
| Shell | Palette scope chips All, Changes, Code, People, Commands; Tab cycles the scope | done | `ds-palette-bar` has input and Cancel only. Chips in `head_navbar.tmpl`, Tab handling in `onPaletteKeydown` | S |
| Shell | Palette results grouped Changes (title, repo and number), Code (path, symbol), People, fetched as you type | needs-both | Today only static destinations plus "search for ..." links (`filterPalette`). Hub: JSON route (for example `GET /-/asc/palette?q=&scope=`) in `routers/web/explore/asc_search.go` merging pull requests (`issues_model`), users (`user_model.SearchUsers`) and symbols (asc `CrossSearch.Search`, `LookupSymbol`; `services/ascsearch`), read-checked per repo. JS: debounce, abort, render, live count | L |
| Shell | Command verbs (undo, restack, merge) switch the list to actions; Commands group with Undo last operation (Cmd Z) and Ask an agent to change this... | needs-hub-backend | Static Commands group has New repository and New organization only. asc RPCs exist: `UndoRef`, `Restack`, `RequestMerge`, `CreateRun`. Hub needs a context (current repo, stack) and POST handlers (`routers/web/repo/asc_ops.go`, `asc_restack.go`, `asc_stack_actions.go`, `asc_agents.go`) callable from the palette with a confirm step | L |
| Shell | Empty query shows Recent items and Shortcuts (Go to inbox: g i) | done | Recents in `localStorage` (visited repos and changes), static shortcuts; `ds-shell.js`, `head_navbar.tmpl` | M |
| Shell | No-results text names the query and suggests a symbol, handle or title | done | `asc_shell.palette_empty` is a different sentence ("Press Enter to search this text") | S |
| Shell | Keyboard shortcuts overlay (`?`, "All shortcuts") | done | No handler in `asc-keys.js`; dialog listing the keys of the page | S |
| Shell | Footer: full footer on public pages (four groups, version, Based on Forgejo, language, theme switch), one-line footer when signed in | done | `templates/base/footer_content.tmpl` | - |
| Shell | Footer links: Stacks and changes, Agent runs, Changelog, CLI reference, REST API, MCP server, Status, Security, Contact, Privacy, Terms, Acceptable use, Subprocessors, Cookies | ui-only-missing | Today: Download, Explore, Users, Organizations, Docs, Operations, API, Why, Licenses. Add links (docs pages or `[asc]` settings URLs) in `footer_content.tmpl` | S |
| Shell | "All systems operational" status in the footer | needs-hub-backend | Not present. Cached value from `/healthcheck` plus asc serve health, or a configured status URL; `footer_content.tmpl` | M |
| Shell | "Rendered in 18 ms" in the footer | done | Behind `ShowFooterTemplateLoadTime`; enable it in the asc `app.ini` template | S |

## Main (landing, signed out)

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Main | Hero: announcement pill, headline, lead, Download ASC / Create account / Sign in | done | `templates/home.tmpl` | - |
| Main | Download ASC goes to `/download` and preselects the detected OS | done | Hero button is `href="#download"`, a section on the same page | S |
| Main | Install method pills (Homebrew, Scoop, winget, curl, PowerShell, .deb .rpm) as a labelled group with `aria-pressed`; the command swaps; Copy raises a toast; "1 Install ASC / 2 Connect to this hub" cards | ui-only-missing | Hero has only the connect command. Data exists in `modules/ascdownload/offer.go` (`Command` list per OS). Add pills and swap script (`home.tmpl`, `asc-ui.js`, pattern of `initDownload`) | M |
| Main | Product window in the hero (stack rail, change with intent, comment, whose turn) | ui-only-missing | No such markup in `home.tmpl`; static preview with a text equivalent | M |
| Main | Works-with strip | done | `home.tmpl` `ds-land-strip` | - |
| Main | Why ASC, stacks, review, code intelligence, policy cards | done | `home.tmpl` | - |
| Main | Operation log and merge queue demo | done | `home.tmpl` `ls-log` | - |
| Main | Agents band and run preview | done | `home.tmpl` `asc-agents-title` | - |
| Main | Workflow cards (dependency upgrade, flaky triage, large migration) | done | `home.tmpl` `ls-wf` | - |
| Main | Bring your own keys and storage (provider rows, S3 endpoint, bucket, region) | done | `home.tmpl` `ls-byo` | - |
| Main | Final call to action and footer | done | `home.tmpl` `hub-cta` | - |
| Mobile-Landing | Sticky bottom bar with Download ASC and Sign in; install pills scroll sideways; workflow cards snap | ui-only-missing | No sticky bar in `home.tmpl`; `asc-ds/touch.css` | S |

## Onboarding (sign in, sign up, recovery, device)

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Onboarding | Split layout: form card plus navy brand panel with promise and quote | done | `templates/user/auth/shell_start.tmpl`, `shell_end.tmpl` | - |
| Onboarding | Sign in form: username or email, Forgot password, password with Show toggle, Keep me signed in, CLI hint | done | `signin_inner.tmpl`, `ds-shell.js` `onRevealClick` | - |
| Onboarding | "Sign in with a passkey" (discoverable credentials) | needs-hub-backend | Only 2FA security keys exist (`webauthn.tmpl`); no passkey login route anywhere in `routers`. Add begin and finish routes in `routers/web/auth/webauthn.go` and the button in `signin_inner.tmpl` | M |
| Onboarding | Single sign-on button under the primary action | done | `oauth_container.tmpl` renders configured sources | - |
| Onboarding | Rate limit ("after 5 attempts we wait 5 minutes") | needs-hub-backend | No login throttle in `routers/web/auth/auth.go` or `modules/setting`. Counter per user and IP in cache, message with the wait | M |
| Onboarding | Sign up: username with live "Your handle: @name" and blur validation | ui-only-missing | `signup_inner.tmpl` has a plain field. Add handler in `web_src/js/features/user-auth.js`; a name-availability route does not exist, add one in `routers/web/auth/auth.go` (then needs-hub-backend S) | M |
| Onboarding | Sign up: optional Full name | needs-hub-backend | Not in `signup_inner.tmpl` or `forms.RegisterForm`; set `User.FullName` in `SignUpPost` | S |
| Onboarding | Sign up: strength meter and three live rules (12 characters, letters and numbers, not found in breaches) | needs-hub-backend | No meter in `web_src`. Rules and meter in `user-auth.js`; breach rule is server side: `PASSWORD_CHECK_PWN` exists (`modules/setting/security.go`, default off, runs on submit only). Add `POST /user/password/check` (k-anonymity range query), default it on | M |
| Onboarding | Sign up: "I agree to the Terms and the Privacy policy" | needs-hub-backend | Not in the form. Needs Terms and Privacy URLs, a required checkbox and a stored acceptance | M |
| Onboarding | Sign up has no retyped password on the board; the hub form asks for one | ui-only-missing | Drop the retype field (Show replaces it); relax `forms.RegisterForm` | S |
| Onboarding | Two-factor: six boxes act as one input and auto-advance; recovery code link; "Use a security key instead" | ui-only-missing | `twofa.tmpl` has one field and the scratch link. Six-box component in `user-auth.js` (one real input for screen readers); key link when keys exist | M |
| Onboarding | Security key waiting screen with spinner and "Try another way" | done | `webauthn.tmpl` | - |
| Onboarding | Authorise the CLI (device): code, terminal and OS, Authorise and Deny | needs-both | Not present. The CLI signs in with a pasted token (`crates/asc/src/cli/auth.rs`: `--token-stdin`, `--prompt`), no device flow. Hub: device-code endpoints, template, token minting (`models/auth/access_token.go`). asc: `asc auth login` device grant with polling | L |
| Onboarding | Forgot password: sent page, lifetime, resend limit, no account disclosure | done | `forgot_passwd.tmpl` | - |
| Onboarding | Reset password: new and confirm | done | `reset_passwd.tmpl` | - |
| Onboarding | Registration closed (invite only) and awaiting approval | done | `signup_inner.tmpl` `DisableRegistration`; `RegisterManualConfirm` setting | - |
| Onboarding | Focus on first invalid field; autocomplete tokens | done | `shared/ds/field.tmpl` | - |
| Mobile-Onboarding | Sticky primary action above the keyboard-safe area; 48 px fields | done | `asc-ds/touch.css` (not device tested) | - |

## Install (first-run setup)

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Install | Setup form: database cards, general settings, administrator, email and service options | done | `templates/install.tmpl`, `install.js` | - |
| Install | Test connection with result ("Connected to PostgreSQL 16.4" or the cause); Install disabled until it passes for server databases | needs-hub-backend | No test endpoint; `routers/install/install.go` checks on submit only. Add `POST /install/test-db` reusing `checkDatabase`, JS in `install.js` | M |
| Install | Database error card with "Test again" and "Use SQLite instead" | ui-only-missing | Error shows as a form flash today; card and actions in `install.tmpl` | S |
| Install | Sticky summary rail (Database, URL, Registration) and live step progress | ui-only-missing | `install.tmpl` has the step list (`ins-steps`), static; live summary in `install.js` | S |
| Install | "Setting up your hub" progress page (Step n of 5) that polls, then redirects | needs-hub-backend | `SubmitInstall` runs synchronously. Run steps as a job with a status route, poll with `aria-live=polite` | M |
| Install | Post-install page: signed in as, `asc auth login --host ...` with Copy, Open the hub, Download ASC | ui-only-missing | `templates/post-install.tmpl` exists; compare and add the command card | S |
| Install | Skip administrator: first registered user becomes owner | needs-hub-backend | Installer needs an admin today; add the skip path and "first user is admin" in `routers/web/auth/auth.go` | M |
| Mobile-Install | One section per screen with progress bar, Back and Next | ui-only-missing | `install.tmpl`, `install.js` stepper under 768 px | M |

## GetStarted (empty hub)

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| GetStarted | Four-step checklist with `aria-current=step`, done as a word | done | `templates/asc/get_started.tmpl` at `/-/get-started` | - |
| GetStarted | Shown on `GET /` for a signed-in person with no repositories; dismissible; returns while none exist | needs-hub-backend | Nothing links to `/-/get-started` or `/user/welcome`; `routers/web/home.go` and `asc_dashboard.go` do not branch on `NumRepos == 0`. Branch there; dismiss flag as a user setting | M |
| GetStarted | Two parallel first-run pages (`asc/get_started.tmpl`, `user/asc_welcome.tmpl` with old `hub-` classes) | ui-only-missing | Merge into one page that follows the board; delete `user/asc_welcome.tmpl` and `routers/web/asc_welcome.go` | S |
| GetStarted | Step 2 ticks by itself when the first CLI sign-in arrives, no reload | needs-hub-backend | `GSConnected` is "has an access token" at render time. Add a status route (token last used by the asc CLI) and a poll in a new `asc-welcome.js`; progress bar updates | M |
| GetStarted | Import step with source tabs and progress "Reading history 12,408 commits" | needs-hub-backend | Links to `/repo/migrate` today. Inline form plus migration-task polling (`routers/web/repo/migrate.go`, `services/migrations`) | L |
| GetStarted | Health check (Git found, signed in, SSH key, shell shim; `asc doctor` on machine and server) | needs-both | Hub card lists email, SSH key and token only. `asc doctor` exists (`crates/asc/src/cli/doctor.rs`) but never reports to the hub: add a report call or show server checks only | L |
| GetStarted | "This hub" card: Storage, AI providers, Email with Change and Add | ui-only-missing | Data exists (`services/ascbyo`, settings); card missing | S |
| GetStarted | Shortcuts card | ui-only-missing | Static card; `t` and `?` need handlers (see Shell) | S |
| GetStarted | "Open the sample project" read-only | needs-hub-backend | No sample repository; seed one at first start (`routers/init.go`, `services/repository`) | M |
| GetStarted | Empty-area cards: inbox zero on day one, no repositories, "just you" with Invite | ui-only-missing | `asc_home.tmpl` has an inbox-zero block; add the other two | S |

## Download

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Download | Detected system card, recommended file, size, truncated sha256, "Not your system?" and OS sections | done | `templates/download.tmpl`, `asc-ui.js` (client hints), `modules/ascdownload` | - |
| Download | Package manager cards per OS with Copy | done | `offer.go` `PackageCommands`, `download.tmpl` | - |
| Download | Files table with size, sha256 (copy whole, full value in title), `.minisig` link | done | `download.tmpl` | - |
| Download | Copy confirms with an inverse-surface toast ("SHA-256 copied") | done | `clipboard.js` shows a tooltip; board uses a toast. Restyle `modules/toast.js` to the board and use it here and on Main, GetStarted, Docs | S |
| Download | Connect card, access-token alternative, Verify your download, release key | done | `download.tmpl` aside | - |
| Download | Editor extensions | done | `download.tmpl` | - |
| Download | System requirements card | ui-only-missing | No `system_req` string; add to the aside | S |
| Download | Release notes link, Older releases | ui-only-missing | Release notes and build-from-source exist. "Older releases" missing: `modules/ascdownload/manifest.go` holds one manifest, add a release index | M |
| Download | States: No release yet (build from source, admin note), system not detected | done | `download.tmpl` | - |
| Download | Loading releases skeleton | ui-only-missing | Page is server rendered; only needed if offers load client side | S |
| Download | "Signature check failed" and "verified" banners | ui-only-missing | Board shows guidance copy, not a live check (a browser cannot verify minisign without a library). Decide: explainer card, or a WASM minisign check of a chosen file (M) | S |
| Mobile-Download | Sticky bottom bar "Download for your system"; files as cards | ui-only-missing | `download.tmpl`, `asc-ds/touch.css` | S |

## Explore: repositories

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Explore | Tabs Repositories, Users, Organizations with counts | done | `explore/navbar.tmpl` | - |
| Explore | Filter bar: search, Sort, Visibility, kind pills; `/` focuses search | done | `shared/repo_search.tmpl`, `repo-search.js` | - |
| Explore | Language filter menu | ui-only-missing | `.Language` is read from the query but no menu lists languages. Menu from language stats (`models/repo/language_stats.go`) in `repo_search.tmpl` | M |
| Explore | Quick chips All, Has open stacks, Agent activity, Starred, Templates, Archived | needs-hub-backend | `repo_search` has fork, mirror, template, archived pills only. "Has open stacks" needs an option on `repo_model.SearchRepoOptions` (`routers/web/explore/repo.go`); "Agent activity" needs the agent flag below; "Starred" a per-user filter | M |
| Explore | List and Grid layout toggle; layout persists per person | done | No layout toggle anywhere (density exists in the account menu). Toggle, grid CSS, `localStorage` in `repo_search.tmpl`, `repo_list.tmpl` | M |
| Explore | Row: owner avatar, name, visibility chip, description, topics, language, stars, updated, open-stacks chip | done | `explore/repo_list.tmpl` | - |
| Explore | Agent activity chip on a row | needs-both | Not in `repo_list.tmpl`. Needs agent runs per repo over a recent window: asc `ListRuns` batched for a page, or a hub cache table fed by `services/ascsync` | M |
| Explore | Changeset count on a row ("3,812 changesets") | needs-both | Row shows stars and forks only. asc has per-repo change data but no batch stats RPC; add `RepoStats` or cache in `services/ascsync` | M |
| Explore | Contributor avatar stack on a row (people and agents) | needs-hub-backend | Not present. Top contributors from `models/activities` or `repo_model.GetContributors`, cached per repo; agents depend on the agent directory below | M |
| Explore | Row actions on hover or focus-within: Copy clone command, Star | ui-only-missing | Neither on the row. Buttons with `data-clipboard-text` (`asc clone host/owner/name`) and the existing star action | S |
| Explore | Right rail: Trending this week (changesets this week), Popular topics, Clone anything you can see | needs-hub-backend | `explore/rail.tmpl` has only the clone card. Trending: weekly merged pull count per repo (`models/issues/pull.go` query, cached); topics: `repo_model.FindTopics` (`routers/web/explore/topic.go`) | M |
| Explore | States: loading skeleton, empty hub (New repository, Import), no results with removable filter chips and Clear filters, error with request id and Try again | ui-only-missing | Empty and no-results text exist (`shared/ds/empty`); removable chips, skeleton and error card missing (request id: see Errors) | M |
| Explore | Pagination "Showing 1 to 8 of 128" | done | `base/paginate.tmpl` | - |
| Mobile-Explore | Sort and filter bottom sheet with result count on the primary button | done | `repo_search.tmpl` | - |
| Mobile-Explore | Pull to refresh; infinite scroll after 30 rows with Load more fallback | ui-only-missing | Classic pagination only. New `explore-more.js` plus a list-fragment route | M |

## Explore: users

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| ExploreUsers | "People and agents" header and Invite people button | ui-only-missing | `explore/navbar.tmpl` offers New repository / New organization only; invite needs a hub-level invite flow (org invite exists) | M |
| ExploreUsers | Search by name, handle or role | done | `explore/users.tmpl` (role not searchable until roles exist) | - |
| ExploreUsers | Show filter (People and agents / People / Agents), Organization filter, Sort "Most active" | needs-hub-backend | Sort has Newest, Oldest, A to Z only. Add activity sort and organization filter in `routers/web/explore/user.go` / `user_model.SearchUserOptions` | M |
| ExploreUsers | Chips All, People, Agents, Maintainers, Online now | needs-both | None present. Agents are not hub users (they are tokens and runs in `proto/asc/v1/agents.proto`); Maintainers needs a role; Online now needs presence | L |
| ExploreUsers | Role chip (Owner, Maintainer, Member, Agent) and "Roles on this hub" card | needs-hub-backend | No hub-level role beyond site admin. Define Owner = site admin, Maintainer = write on any repo, Member; compute in the search; `user_list.tmpl`, `rail.tmpl` | M |
| ExploreUsers | Agent rows: violet square avatar, `@org/name`, owner, scope, View runs | needs-both | asc has repo-scoped `ListTokens` and `ListRuns`; no hub-wide agent directory. asc: `ListAgents(owner)` RPC; hub: row type in `user_list.tmpl`, links to `/-/asc/agents` | L |
| ExploreUsers | Meta row: location, joined, organizations, public email as own labelled item | ui-only-missing | `user_list.tmpl` shows location, email, joined; organizations missing | S |
| ExploreUsers | Activity sparkline (12 weeks, text equivalent), counts "changesets, reviews" | needs-hub-backend | No sparkline component anywhere. Weekly series from `models/activities/user_heatmap.go` or pull and review tables, cached; partial `shared/ds/sparkline.tmpl` with a text label | M |
| ExploreUsers | Hover action Follow; View runs for agents | ui-only-missing | Follow endpoint exists (profile htmx); row button in `user_list.tmpl` | S |
| ExploreUsers | Rail "Who is active now" with presence and current activity | needs-both | No presence model. Hub: last-seen heartbeat (`models/user`); asc: run state for agents (`GetRun`) | L |
| ExploreUsers | Grid layout | done | With the layout toggle (Explore) | M |
| ExploreUsers | States: loading, no people match, filter chips, error | ui-only-missing | Empty text exists; rest as in Explore | S |
| Mobile-ExploreUsers | Chips filter People, Agents, Maintainers, Online now; Follow inside the row at 44 px | needs-both | As above | M |

## Explore: organizations

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| ExploreOrgs | Org row: avatar, name, description, repositories and people counts | done | `explore/user_list.tmpl` (`IsOrganization`) | - |
| ExploreOrgs | Verified badge (domain proof) and explainer card | needs-hub-backend | No flag. Add a verified field and domain proof (DNS TXT or well-known file) in `models/user`, migration under `models/migrations`, owner settings, a check job; chip in `user_list.tmpl` | L |
| ExploreOrgs | Agents count; member stack with up to four avatars and +N | needs-both | Members: `organization.GetOrgUsersByOrgID` (hub). Agents count: asc, as for Explore users | M |
| ExploreOrgs | 12-week sparkline and "Active 12 min ago" | needs-hub-backend | Sparkline partial; org activity from the `action` table | M |
| ExploreOrgs | Chips All, Verified, Has agents, I am a member; Size filter; Sort most active | needs-hub-backend | `routers/web/explore/org.go`; membership filter is simple, the others depend on rows above | M |
| ExploreOrgs | View hover action; Start an organization card; grid layout; states | ui-only-missing | `user_list.tmpl`, `explore/rail.tmpl`, layout toggle | S |

## Docs

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Docs | Docs home: search, Popular chips, six entry cards, "New in ASC 1.4" list, Need a person card | ui-only-missing | `templates/asc/docs.tmpl` has six cards and a help block; no search, popular or what's new | M |
| Docs | Docs search with grouped results (Commands, Guides), did-you-mean, no results, loading; Ctrl K or `/` opens it | needs-hub-backend | Index over `docs/asc/*.md` and the CLI reference built at start (new `modules/ascdocs`) plus `GET /-/docs/search?q=`; JS list | L |
| Docs | Article layout: sidebar tree, 720 px column, breadcrumb, steps, callouts, Previous and Next | needs-hub-backend | Docs live on GitHub (`$docsURL`). Render `docs/asc/**` with the hub markdown renderer: `AscDocs` in `routers/web/asc_public.go`, new `templates/asc/docs_article.tmpl`, sidebar manifest | L |
| Docs | OS code tabs remembered; Copy without the prompt | ui-only-missing | Reuse the `initDownload` tab code; `localStorage` key | M |
| Docs | On-this-page TOC highlighting the section in view; Edit this page; Open in the hub | ui-only-missing | Intersection observer in a new `docs.js`; links to the repository file view | M |
| Docs | "Was this page helpful?" Yes and No | needs-hub-backend | `POST /-/docs/feedback`, stored (new table or issue on the docs repo), rate limited | M |
| Mobile-Docs | Docs menu drawer, sticky "On this page" row, sticky next-page bar | ui-only-missing | Same templates under 768 px | M |

## About

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| About | Hero, story with before and after table, principles, open source chips, data ownership, contact | done | `templates/asc/about.tmpl`, route `GET /-/about` (`web.go` line 654) | - |
| About | Header About and footer link go to `/-/about` | done | Header still links to the GitHub copy of `WHY.md` (see Shell) | S |
| About | Source code, Security policy and Contact targets | ui-only-missing | Targets come from `[asc]` settings; confirm they are set | S |

## Errors

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Errors | 404, 403, 413, 500, 503, offline cards with header and footer | done | `templates/status/*.tmpl` | - |
| Errors | Request id ("Request id 7f3a91c2 · date UTC", selectable) | needs-hub-backend | No id in any status template. Generate per request in `services/context`, log it, show in `500.tmpl`, `503.tmpl`, error JSON | M |
| Errors | 500 actions: Try again (same request id) and Status page | ui-only-missing | `500.tmpl` offers Home only | S |
| Errors | 403 actions: Request access and Switch account | needs-hub-backend | `403.tmpl` has Sign in, Dashboard, Search. Request access needs an access-request flow to the owners of the org | L |
| Errors | 404 actions: dashboard, search the hub | done | `404.tmpl` | - |
| Errors | 413 card with limit, artifact storage link and Back | done | `413.tmpl` | - |
| Errors | 503 maintenance: version being upgraded, reads continue through the CLI, window, expected back, status link | needs-both | `503.tmpl` is a generic card and nothing serves it for maintenance. Hub: maintenance setting with window text and a middleware returning 503 for writes (`routers/common/middleware.go`); asc: read-only flag on serve | L |
| Errors | Offline page: "your edit is saved on this device", Retry now, served by a service worker | needs-hub-backend | `/-/offline` page exists but no service worker is registered anywhere in `web_src` or `public`. Add `public/assets/sw.js`, registration in `index.js`, draft saving in editors | L |

## Dashboard

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Dashboard | Greeting by time of day and summary line | done | `routers/web/user/asc_dashboard.go` | - |
| Dashboard | Work list with counts (Inbox, My stacks, Review requests, Agent runs), repositories, teams | done | `templates/user/dashboard/asc_home.tmpl` | - |
| Dashboard | Pinned repositories per person; counts update live | needs-hub-backend | `ascDashRepos` takes the five latest. Pin table (`models/user`) and a star-like action; counts need the live update below | M |
| Dashboard | Scope chips Everything / org / Personal filter the whole page, remembered | needs-hub-backend | None in the template. `?scope=` in `ascDashBuild` filtering inbox rows, agents, stacks; persist as a user setting | M |
| Dashboard | Needs-you rows with chips (Agent, checks, Touches billing), diff size, age | done | `asc_home.tmpl`, `ascDashRowFor` | - |
| Dashboard | Rows act in place: Approve, Reply, Answer (agent question), Add to merge queue, Resolve | needs-both | Buttons are links (`aria-hidden`, tabindex -1) to the review. Hub: POST handlers for approve (review in `routers/web/repo/pull.go`), queue (`asc_queue.go`, asc `Enqueue`), answer (asc `SendInput`), htmx row swap | L |
| Dashboard | Row kinds: reply from a person, agent question, ready to merge with queue position and estimate, trunk moved with restack summary | needs-both | Kinds come from inbox sections; reply quote, "about 4 min" and "ASC restacked 2 of 3" need asc data (`mergequeue.proto` `List`, `stacks.proto` restack result); `services/ascinbox` | L |
| Dashboard | Keys j k, Enter, e (done), g then i, `/` | needs-hub-backend | j, k, g i, `/` done in `asc-keys.js`; `e` needs the done state (Inbox) | S |
| Dashboard | Agents working now with budget bar, paused state, All runs | done | `asc_home.tmpl`, `ascDashAgentsFromServe` | - |
| Dashboard | Stack bars: click a segment opens the review at that segment | done | `wk-segs` items are not links; `ascDashStackFrom` has the pull link per segment | S |
| Dashboard | Merge queue card (Running, median wait, positions, "2 of 3 checks", Open) | needs-both | No such card. Hub aggregates the person's queue entries (`routers/web/repo/asc_queue.go`; asc `mergequeue.proto` `List`, `Get`, `Watch`); median wait needs an asc statistic | L |
| Dashboard | Today feed with Filter | ui-only-missing | Feed present; filter menu missing | S |
| Dashboard | From your terminal card with Copy; g i tip | done | `asc_home.tmpl` | - |
| Dashboard | Live updates | needs-both | Counts load once. Hub: poll `/-/asc/inbox/count` or reuse `eventsource.sharedworker.js`; asc `WatchRefs` / queue `Watch` for pushes | L |
| Dashboard | States: loading skeleton, new account, offline error with retry countdown | ui-only-missing | Inbox zero exists; the others missing | M |
| Dashboard | Undo toast ("Added to merge queue. Undo for 10 s") | needs-both | `toast.js` has no action button; asc `Dequeue` exists for undo; goes with row actions | M |
| Dashboard | Appearance setting hides agent surfaces | done | `ascagentui.FeaturesEnabled` | - |
| Mobile-Dashboard | Pull to refresh; swipe a row left to snooze, right to mark done | needs-hub-backend | New touch module; done state (Inbox) | M |

## Inbox

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Inbox | Tabs Needs you, Waiting on others, Agents, Done with counts (hub adds Drafts) | done | `routers/web/user/asc_inbox_page.go`, `asc_inbox.tmpl` | - |
| Inbox | Filters: All repos, Humans and agents, Sorted by | ui-only-missing | Repo select works; "Humans and agents" missing; "Sorted by" is static text (`wk-sort`). Add `kind` and `sort` in `ascInboxBuild` | S |
| Inbox | Unread dot plus bold title | needs-hub-backend | No unread state (no `unread` in the template). `last_seen` per pull and user in `models/issues/asc_inbox_state.go` (migration) | M |
| Inbox | Row hover actions (Review, Answer, Mark done, Snooze) | ui-only-missing | Rows are a link only; actions posting to `/-/asc/inbox/state` | S |
| Inbox | Select rows (x, checkbox); bulk bar "2 selected: Mark done, Snooze, Unwatch" in a live region | needs-hub-backend | None. `AscInboxStatePost` takes one pull; accept many, add `unwatch`, selection JS in `asc-keys.js` | M |
| Inbox | Mark done (e) with fade and undo toast; Done keeps 30 days and can be restored | needs-hub-backend | Only `pin`, `unpin`, `snooze`, `wake` exist; Done tab lists merged items. Add `done` and `restore` with expiry in `asc_inbox_state.go`; asc `inbox.proto` `SetState` covers pin and snooze only, extend it if state must live in asc | M |
| Inbox | Snooze menu: Later today 17:00, Tomorrow 09:00, Monday 09:00, When the checks finish, Pick a date | needs-both | Menu has 1 h, 4 h, tomorrow, week. "When the checks finish" needs a check-completion wake (asc `ci.proto` events); date picker is UI | M |
| Inbox | Detail pane: why waiting, changed since last look, policy line (approvals 1 of 2), thread, reply box with Approve and Request changes | done | `asc_inbox.tmpl` | - |
| Inbox | "Ask agent to change" (comment becomes an instruction for the author agent) | needs-both | Reply form has Approve, Request changes, Reply only. Hub: POST to `CreateRun` or `SendInput`; asc: instruction flag on review comments (`review.proto` `AddComment`) | L |
| Inbox | Keys j k, x select, e done, s snooze, a approve, r request changes, p pin, Enter | needs-hub-backend | j k s p g i present; x, e need the new states; a and r submit the form with confirm | S |
| Inbox | Pagination "Showing 8 of 23" | done | None in `asc_inbox.tmpl`; list is capped (`ascInboxInfoRows`) | M |
| Inbox | New items slide in from the top without jumping the list | needs-both | No live update. Hub poll or SSE; asc `inbox.proto` `Count` is poll friendly | M |
| Inbox | States: bulk bar, inbox zero with Watch more repositories, loading skeleton | done | Empty exists; the other two missing | S |
| Inbox | Subscriptions link to Settings, Notifications | done | `/notifications/subscriptions` | - |
| Mobile-Inbox | Swipe right to mark done, left to snooze; sticky action bar with Open next | needs-hub-backend | New touch JS; done state | M |

## Profile

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Profile | Header: avatar, name, handle, location, bio, followers, following, joined, website, Follow | done | `templates/user/overview/header.tmpl`, `shared/user/asc_profile_band.tmpl` | - |
| Profile | Presence pill ("Online, local time 14:20") | needs-hub-backend | None. Last-seen model plus the person's time zone; pill in `header.tmpl` with text | M |
| Profile | Role line ("Staff engineer · Payments team · London") | ui-only-missing | Combine existing description, location and team names | S |
| Profile | Request a review (picker with repository and change) and Mention | needs-both | Absent. Hub: picker modal of the viewer's open pull requests (`routers/web/user/profile.go`) and POST to asc `RequestReview` (exists in `review.proto`) via `asc_rpc_proxy.go`; Mention opens a comment composer | L |
| Profile | Tabs Overview, Work (11), Reviews (34), Repositories (9), Activity | ui-only-missing | Tabs today: Overview, Repositories, Projects, Packages, Code, Activity, Stars (`overview/header.tmpl`). Add Work (`ascProfileWork`) and Reviews with counts | M |
| Profile | Working-now list (stack, draft, ready) | done | `profile.tmpl`, `ascProfileWork` | - |
| Profile | Pinned repositories (up to six) with Customize dialog and drag reorder | needs-hub-backend | None. Pin table, `POST /user/settings/pins`, keyboard dialog, htmx | M |
| Profile | Contributions: year switch, summary (changes, reviews, median turnaround), violet cells for agent-authored changes, cell opens the day, table alternative | needs-both | One-colour grid exists (`asc_heatmap.tmpl`) with a text total. Agent attribution needs asc (changeset author kind via `changes.proto`); turnaround from review timestamps | L |
| Profile | "Agents Priya runs" with New agent | needs-both | Not on the profile. Hub must list the person's agents (see Explore users) with scope and counts | L |
| Profile | Recent activity | done | `profile.tmpl` feed | - |
| Profile | "Knows best" (path share of changes and reviews, six months) | needs-both | No code. Aggregate asc `LastCommits` / blame data per user: new RPC or hub job | L |
| Profile | Teams and Focus chips beside Repositories and Organizations | ui-only-missing | Repositories and Organizations cards exist; teams via `organization.GetUserTeams`, focus from repo topics | S |
| Profile | States: owner view (Edit profile, View as visitor), private-profile banner, empty pins and contributions, loading skeleton | ui-only-missing | "View as visitor", the private banner (name and shared repositories only) and the empty rows missing | M |

## OrgProfile

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| OrgProfile | Head: avatar, name, description, location, website, Follow, Org settings | done | `templates/org/header.tmpl`, `follow_unfollow.tmpl` | - |
| OrgProfile | Verified badge; "38 people, 4 agents" counts | needs-both | See ExploreOrgs | M |
| OrgProfile | Tabs Repositories, People, Teams, Packages, Projects with counts | done | `org/menu.tmpl` | - |
| OrgProfile | Repo rows with check status text (Passing, 1 failing, No checks) and a Watch toggle with count | needs-hub-backend | Rows are the shared `explore/repo_list.tmpl`. Default-branch check status (commit status or asc `ci.proto`) and a row watch toggle missing | M |
| OrgProfile | Type and Sort menus land in the URL | done | `shared/repo_search.tmpl` | - |
| OrgProfile | Side cards: People, Teams, Agents, Merge policy with "Read the policy" | needs-both | People and Teams exist (`org/home.tmpl`); Agents and Merge policy missing. Policy from asc `GetPolicy` (repo level) or an org policy; agents as above | M |
| OrgProfile | Member view shows Org settings; lock chip for private repositories | done | `org/header.tmpl` | - |
| OrgProfile | Loading skeleton; mobile row menu | ui-only-missing | With the row actions | S |

## Mobile shell (Mobile-Menu)

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| Mobile-Menu | Top bar, five-item bottom nav (Inbox, Changes, Explore, Search, Me) with counts in the label, drawer, account sheet, full-screen palette | done | `head_navbar.tmpl` (`.ds-topbar`, `.ds-bnav`, `#ds-drawer`) | - |
| Mobile-Menu | Swipe from the left edge opens the drawer | ui-only-missing | No touch handler in `ds-shell.js` | S |
| Mobile-Menu | Create as a bottom sheet (drawer footer and list headers) | ui-only-missing | `#ds-create` is a popover; sheet variant on phones | S |
| Mobile-Menu | Notifications sheet | needs-hub-backend | With the popover (Shell) | M |
| Mobile-Menu | Palette filter chips scroll horizontally | done | With the scope chips | S |

## Cross-group notes

- Palette results, Inbox "Ask agent to change" and Dashboard row actions need the same hub piece: JSON or htmx review actions in front of `asc_rpc_proxy.go`. Build once.
- Agents as hub-listed identities (Explore users, Profile, OrgProfile, Dashboard scope, Explore repo chip) share one missing asc piece: a hub-wide agent directory RPC in `proto/asc/v1/agents.proto`.
- The sparkline (Explore users, orgs, repositories on mobile) is one component: `templates/shared/ds/sparkline.tmpl` plus a cached weekly series.
- The inbox "done" state gates Dashboard `e`, Inbox bulk bar, mobile swipes and the Done tab.
- The dashboard SPA mapping in the board annotation (`web/src/routes/dashboard-desktop.tsx`) points at the asc repo's local web app, not the hub; the hub dashboard is the Go template above.
