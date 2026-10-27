# ASC Hub QA walkthrough

Branch `qa/walkthrough` (from `asc/v16` at 98d46e82f2). The hub was built from that tree and run next to a real `asc serve` built from `D:/dev/asc` master (7d30f82), SQLite, loopback `http://localhost:3000`, driven with Playwright (Chromium, 1440x900 and 390x844, light and dark) and the real `asc` and `git` clients.

## How it was run

- Hub: `go build -tags "sqlite sqlite_unlock_notify"` in WSL with cgo. `CGO_ENABLED=0` as in the brief cannot build the SQLite driver, so cgo was used. The first-run installer wrote `app.ini`; `[asc]` was added with `WRITER = asc`, `NEW_REPOSITORIES = asc`, `BRIDGE = eager`, `PUBLIC_URL = http://127.0.0.1:7890`, `DOWNLOAD_DIR` pointing at a fake signed-looking release.
- `asc`: debug build of master (a release build was not in `E:/cgt/shared`). `asc serve` as in `docker/allinone` (sqlite, file store, `[auth] provider = "forgejo"`).
- Everything ran as `nobody` in WSL; temp output in `E:/tmp/hubqa-out` was removed at the end. Key screenshots (contact sheets of the four variants: desktop and mobile, light and dark) are in `D:/Jordan/Temp/claude/d--dev-source-control/ee0bff38-7695-4b75-94ae-583043c58c3f/scratchpad/qa/`; the "Screenshot" column names the file there.
- The `asc` build on master does not implement several services the hub offers pages for (operation log, dependency scan, text search, count_only log). Where that blocked a step it is marked **blocked (asc)**, not as a hub defect.
- Not every step could be driven. Steps that were not exercised are marked **not run** with the reason; nothing is reported as passing that was not run.

## Result by journey

| Journey | Result |
|---|---|
| 1 First run, sign in, sign up, sign out | Pass. Installer, admin bootstrap, sign in (bad and good), sign up (bad and good), sign out (POST only) work. Notes on the installer below. |
| 2 Get started | Pass after seeding a release manifest. Without one the page tells an admin exactly what to set and hides it from others. |
| 3 Org, repo, clone, push, repo pages | Partly. Org and repo creation, git push through the hub, tree, file, history, blame, branches work after fixes F1 to F3. `asc clone` against the hub address did not work (O1, fixed). |
| 4 Change, review, comment, merge queue, undo | Partly. Pull request creation works after F2 and F3; comment works; the review form opens. Approve, merge, queue and undo not run: the change had a real conflict, `asc submit` is refused by the server (O2), and the operation log is not implemented (O4). |
| 5 Checks and CI | Pages render (failures, flaky, trends; workflows list). Workflow run, failures and trends not run: no runner and no data. |
| 6 Agents | Feature switch turns on; repo agent pages (runs, new task, tokens, workflows) render. Token creation and a run with a fake provider not run (no key listed, asc server side not available). |
| 7 Settings | Profile, appearance, account, applications, security, signing keys, AI keys and storage pages render. API token created through the API. BYO key test and S3 target not run (needs `ai-keys.v1` and `storage.v1` on the server). |
| 8 Admin | Dashboard, users, orgs, repos, config, notices, actions render. Audit log answered 403 (O5, fixed). |
| 9 Explore, search, inbox, profile | Explore, inbox, drafts, notifications, profile render (profile fixed, F5). Code search answers 503 (O4). |
| 10 Dependencies | Page says the server does not scan yet (O4). |
| 11 Keyboard, landmarks, errors, long names, slow network | Done on 7 pages; findings O10, O13, O15. |

## Defects

Severity: critical (a main flow fails), high (a feature fails or loses data), medium, low. Status: fixed (own commit and test), open, visual (for the design work, not fixed here), blocked (asc).

| ID | Journey | Step | Expected | Actual | Severity | Screenshot | Status |
|---|---|---|---|---|---|---|---|
| F1 | 3 | `git push` to an asc repository, then open a file | The branch table follows the push; push activity, Actions and pull request updates fire | asc's outbox calls post-receive naming the hub as pusher (uid 0), and before git moved the ref. Log: `pushUpdate failed: user does not exist [uid: 0]`; branch table kept the old commit; every file page warned "The database representation of this repository is out of synchronization". Fixed in 7f05907bd1: pusher remembered in pre-receive and restored, call waits for the refs. | critical | j3-file.sheet.png | fixed |
| F2 | 4 | Create a pull request | The pull request opens | 500: `repostore/asc: Log: asc answered Unimplemented: count_only, pickaxe and resume_token are not available`. Fixed in 91ec77e599: count by listing when count_only is unavailable. | critical | j4-compare.sheet.png | fixed |
| F3 | 4 | Create a pull request (after F2) | The pull request opens | 500: `fatal: bad revision refs/heads/master...refs/pull/1/head`; with `WRITER = asc` the compare ran before the bridge wrote the pull head. Fixed in b01fdd5eab: wait for the bridge. | critical | j4-pr.sheet.png | fixed |
| F4 | 5 | Open Checks | Four state tabs, Open current | Five tabs: the first labelled with half a key `asc_trust.checks.state.`, "Open" twice, none current. Fixed in cd24915359. | medium | r-checks.sheet.png | fixed |
| F5 | 9 | Open a user profile | Readable texts | `user.asc_profile.working_now`, `user.asc_profile.resume%!(EXTRA string=qa-admin)`, `...total_n%!(EXTRA int64=10)` etc.: 23 keys missing from the English locale. Fixed in 75ac3f606c, with a test that every key used exists. | high | longqaadmin.sheet.png | fixed |
| O1 | 3 | `asc clone http://localhost:3000/qa-admin/demo` (the address the hub shows) | Clone works; docs say the hub is the only public address and "clients reach the ASC API through the hub's address" | The hub has no forwarder for `asc.v1.*`: `POST /asc.v1.Repo/ListRefs` answers 405. Pointing `PUBLIC_URL` at asc serve gives "names server ..., a different origin; no credential is sent there". Only works after setting `remote.<name>.ascServer` by hand. The all-in-one image as documented cannot serve the asc client. Needs a decision (proxy asc.v1 and git through the hub, over h2c for gRPC, or document a second public address). | critical | j3-repo-created.png | fixed (e588695ecf, 7802d35573): the hub forwards POST /asc.v1.* (allowlist, caps, h2c prior knowledge only). Run against a real hub and asc serve: `asc clone` through the hub address and an authenticated push work, an anonymous clone of a private repository is refused, an `Upgrade: h2c` request is answered 400. |
| O2 | 4 | `asc submit -s` of a three change stack | Stack is sent as changes | `remote history: InvalidArgument: that ref namespace is local to a client`: the client sends `refs/for/<trunk>/asc-<change>` in `UpdateRefs`, which asc serve's `LOCAL_ONLY` list refuses (`crates/asc-serve/src/update.rs`). Client and server on master disagree. | high | none (CLI) | open (asc) |
| O3 | 3 | `git push -f` of a rewritten feature branch | Force push to an unprotected branch is accepted | Refused with "The asc server could not take this push; it was refused. Try again later." The real cause is non-fast-forward (`fetch first`); the hub sends no force flag in `ImportGitPush` and the server's FailedPrecondition arrives as `Internal: grpc-status-details-bin mismatch`, so the hub shows a retry text. | high | none | open |
| O4 | 4, 9, 10 | Operations page, Dependencies page, repository text search | Operation log, dependency findings, search results | asc master answers "no operation log yet", "does not scan dependencies yet", and search answers 503 "This kind of search is not available for this repository yet". The hub says so plainly; undo, dependency and search journeys could not run. | high | r-ops, r-deps, r-search sheets | blocked (asc) |
| O5 | 8 | Admin, organization and repository audit log; repository secret scanning | Log for the site administrator and owner | 403 "You do not have the right to do this." for the site administrator (`/admin/asc/audit`), the org owner and the repo owner. Likely the principal the hub forwards; not investigated further. | high | a-audit.sheet.png | fixed (93c91fa147): the hub signs a claim of its own permission decision for the person (no access token); the 403 came from the call token lacking `read:user` and write access. Needs asc serve to accept the claim for roles read and write and for the org, user and instance subjects (docs/asc/ARCHITECTURE.md, "Forwarded identity"). |
| O6 | 3 | `asc --help`, `asc help`, `asc auth login --help` | asc help | Prints git's usage ("usage: git ...", git help text) and `fatal: asc: option '--help' is not supported`. | low | none | open (asc) |
| O7 | 1 | First-run installer | ASC defaults | SSH port 22 and SSH enabled are written (`DISABLE_SSH = false`) although the hub is HTTPS only; the update checker is on and checks Forgejo's release record; slogan field truncated on mobile. | medium | j1-install.sheet.png | open |
| O8 | 3 | New repository form and quick guide | One default branch | Form and guide say `main` (`asc init -b main`), asc creates and the docs say `master`. | low | j3-repo-create.sheet.png | open |
| O9 | 11 | Tab through 7 pages | Skip link, visible focus | No skip link on any page; 2 to 9 of the first 40 tab stops have no outline or shadow (repo home 9, download 8). | medium | none | partly fixed (ff713db25a): skip link added as the first tab stop of every page, target `#main-content`; test. Focus outlines open (visual). |
| O10 | all | axe-core WCAG 2.2 AA on every page | No critical findings | `aria-required-children` (critical) on every page: the footer language menu has `role="menu"` without items. Also: no `<h1>` on sign in, sign up, explore, all settings pages; two on pull request and 404; two `<main>` on the dashboard; no `<main>` on repo Actions; unnamed icon buttons and links in repo and pull request pages; `nested-interactive`, `target-size`, `color-contrast` on repo, blame, pull request pages. | medium | j1-login.sheet.png | critical finding fixed (ff713db25a): a menu dropdown is a menu button that owns menu items. The other findings (headings, landmarks, names, contrast, target size) stay open (shell, visual). |
| O11 | all | Footer | Product footer | "Version: development Page: 6ms Template: 3ms" is shown to every visitor, signed out too. | low | j1-login.sheet.png | visual |
| O12 | 6 | `/-/asc/agents` ("Your agents") | Page with gutters | Content flush left with no gutter (12 px on mobile), a context bar with only a logo and a caret (empty switcher), most of the page empty. The route `/user/settings/asc-agents` named in the brief does not exist in this tree (404); the page is `/-/asc/agents`. | medium | u-agents.sheet.png | visual |
| O13 | 3, 9 | Repo header on mobile, command blocks, profile side column | Fit | Repo tab bar is clipped at "Graph ..."; the `asc auth login ... --prompt` and install commands are cut off without a scroll cue; the profile repository list renders one letter per line. | medium | j3-repo-home.sheet.png, j2-download.sheet.png, longqaadmin.sheet.png | visual |
| O14 | 3 | Start of the hub | No errors | `Unable to get language stats ... object does not exist` and `Stat: asc Unavailable` while the hub runs before asc serve; the entrypoint starts the hub first, so every boot logs them and the language bar stays empty until the next push. | low | none | open |
| O15 | 11 | Load a repository page at 50 KB/s, 800 ms latency | Usable | 42 s to the load event. | medium | none | open |
| O16 | 3 | `asc auth login` hint on every page and the download page | Matches the client | The hub's text says `asc auth login --host ... --prompt`; `docs/GETTING-STARTED.md` in the asc repository says asc has no login command. The binary on master does have it (`--token-stdin`, `--prompt` needs a terminal). The asc doc is stale. | low | j2-download.sheet.png | open (asc docs) |

## Not exercised

Inline code comments, approve, merge now, merge queue landing, undo (blocked by O2 and O4); workflow file run, failures, flaky, trends with data; agent run with a fake provider and approvals; agent token creation; BYO AI key test and BYO S3 target; notification settings (no such page in this tree); backups and the audit chain. These need an `asc serve` with the matching services, or a runner.

## Commits

| Commit | Change |
|---|---|
| 7f05907bd1 | post-receive of asc's outbox attributes the push and waits for the refs (F1), tests in `services/ascsync` and `routers/private` |
| 91ec77e599 | count commits by listing when asc has no count_only (F2), test in `modules/repostore/asc` |
| b01fdd5eab | new pull request waits for the bridge (F3), test in `services/pull` |
| cd24915359 | checks state tabs (F4), test in `routers/web/repo` |
| 75ac3f606c | profile locale keys (F5), test in `routers/web/user` |

`go build ./...` and `go test` for `services/ascsync`, `routers/private`, `modules/repostore/asc`, `services/pull`, `routers/web/repo` and `routers/web/user` pass (run in WSL as a non-root user with cgo and the `sqlite` tag).

## Re-run after the fixes (hub side)

- Journey 3, clone: run against a real hub and a real `asc serve` (all-in-one layout, loopback h2c). `asc clone` through the hub address works, the authenticated push works, the private repository is refused to an anonymous client, and the upgrade request is refused. Not re-run with Playwright: the journeys of the browser (1, 2, 4 to 11) were not repeated after the fixes; only the Go tests (end to end through the forwarder with a real gRPC client and server, the permission parity matrix, the skip link integration test) stand for them.
- New findings in this run: the commits API answered 500 for an asc repository (no HEAD to read), fixed (71a7092131); a second `asc clone` of the same repository fails on the client with a UNIQUE constraint on `workspaces.path` (asc client, not the hub); `trusted_proxies = []` in the generated asc serve configuration made every proxied client share the address 127.0.0.1, now `["127.0.0.1/32"]` (7b17f010bc).
