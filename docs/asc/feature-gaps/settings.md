# Feature gaps: settings, organization, admin, drafts, issues, projects, packages

Audit of the design boards in `design/project-v3/` (group "settings") against the hub (`asc/v16`) and the asc server (`master`), done on 2026-10-28 by reading templates, routers, models, `web_src/js` and `proto/asc/v1`. Each board's annotation block (states, interactions, accessibility, maps-to) was read with its content. Mobile twins carry the same content in one column; they are covered by the desktop rows plus the two Mobile rows at the end. `OrgProfile` is outside this group and is not covered.

`docs/asc/UI-COVERAGE.md` reports 0 partial boards, but that measures markup (design-system classes), not behaviour. This file measures behaviour.

## Status words

- **done**: the behaviour exists and was found in code.
- **ui-only-missing**: data and routes exist or are trivial; only template, CSS or JS is missing.
- **needs-hub-backend**: needs a hub model, migration, route or service; asc is not involved.
- **needs-asc-backend**: needs a new asc RPC or field; the hub only renders it.
- **needs-both**: needs asc and hub work.

Size: S under half a day, M one to three days, L more than three days (including tests).

## Counts

| Status | Rows |
| --- | --- |
| done | 87 |
| ui-only-missing | 24 |
| needs-hub-backend | 34 |
| needs-asc-backend | 3 |
| needs-both | 25 |
| total | 173 |

Audit of 2026-10-30: the counts above were recomputed from the table rows (the earlier header counts were not updated as rows were marked). Rows that were read in the tree and corrected carry an Audit note in their Where cell. Per-board counts and the verified status of every board are in `docs/asc/STATUS.md`; the per-board count table below is not maintained.

Open rows by size: S 23, M 52, L 11.

Paths: `templates/`, `routers/`, `models/`, `services/`, `modules/`, `web_src/` are in `D:/dev/asc-hub`; `proto/` and `crates/` are in `D:/dev/asc`.

## Biggest gaps

1. Notifications settings: no page, no per-event channel matrix, quiet hours, digest or push (hub L + asc inbox preferences).
2. Backups: done (Backups service and the admin board).
3. Usage and quota: done (Usage service; account, organization and admin pages).
4. Access tokens: no expiry, and the ASC scope model (None/Read/Write per area) does not exist; asc authn must enforce it.
5. Org agents: switch, spend ceiling, merge defaults and agent identities are done. Open: agent members and path-scoped policy owners.
6. Issue-to-stack links: chips, rail, auto-close on landing, quarantine chip, and stack cards on the Projects board.
7. Signed-in devices and sessions: the asc side (tokens, SSH keys, agent tokens) is done. Open: the hub registry of browser sessions.
8. Account data export: done except the emailed link.
9. AI key limits and sharing: done except the activity tab and the key detail tabs.
10. Admin dashboard and health: changesets/agent-run figures, backup attention, object-storage, mail, TLS, disk and signing-key checks.

## Cross-cutting gaps

- No toast or undo component exists in `web_src/js`; Appearance, Notifications, Privacy and Org people all specify "saves on change with an undo toast".
- Settings forms have a static sticky action bar; the dirty-aware save bar (`initRepoSettingsSaveBar`) is only wired to repository settings.
- Legacy markup remains in webhook lists (`templates/repo/settings/webhook/base_list.tmpl`) used by user settings and admin hooks.

## Table

| Board | Feature | Status | Where to implement | Size |
| --- | --- | --- | --- | --- |
| UserSettingsProfile | Photo upload and remove (initials placeholder) | done | templates/user/settings/profile.tmpl, routers/web/user/setting/profile.go | - |
| UserSettingsProfile | Photo preview before saving; upload-failed error text | done | profile.tmpl + web_src/js/features/asc-ui.js (FileReader preview) | S |
| UserSettingsProfile | Display name, username (redirect hint), bio, pronouns, location, website, visibility radio | done | profile.tmpl (bio limit is 255, board says 160) | - |
| UserSettingsProfile | Username availability check after 400 ms of no typing | done | `routers/web/user/setting/asc_username.go` UsernameCheck (`GET /user/settings/username_check?name=`: own name, free, taken without case, reserved or bad pattern, rename not allowed, cooldown of someone else's old name), `web_src/js/features/name-check.js` (400 ms, text only, shares applyNameCheck with the new-repository page), status line in `user/settings/profile.tmpl`; tests: TestUsernameCheckAnswersFreeTakenOwnAndReserved (Go, not yet run: WSL was read-only), vitest. | - |
| UserSettingsProfile | "How others see you" live preview card | done | profile.tmpl + asc-ui.js | S |
| UserSettingsProfile | Public email select (verified addresses), time zone select | done | `templates/user/settings/profile.tmpl` public email and time zone selects. The zone is the person's for dates: every page of a signed-in person carries it in `<html data-tz>` (`services/context` HTML, `services/ascnotify` TimeZoneName), `web_src/js/features/local-time.js` redraws the times DayTime and Day write (`<time data-local>`), `webcomponents/relative-time.js` draws its tooltip in it, and quiet hours use it; the server text is the fallback without script. `absolute-date` shows the date only and is not moved by a zone by design; tests: TestTimeZoneNameIsTheZoneOfTheProfile, vitest local-time and relative-time | - |
| UserSettingsProfile | Save bar that turns active on edit, names the change count, Discard restores, "Saved" confirmation | done | profile.tmpl has a static .ds-sticky-actions; reuse initRepoSettingsSaveBar (web_src/js/features/repo-settings.js) via data-savebar | S |
| UserSettingsProfile | Loading skeleton inputs | ui-only-missing | profile.tmpl + CSS (page is server rendered, low value) | S |
| UserSettingsAccount | Email list, primary, verify/resend, add, delete | done | templates/user/settings/account.tmpl, routers/web/user/setting/account.go | - |
| UserSettingsAccount | Resend rate limit that says when to retry | done | `routers/web/user/setting/asc_resend.go`: the time of the last confirmation mail is kept next to the 180 s limit and the refusal now says "You can ask for another in 45 seconds" (a flash) instead of redirecting silently; used by the account page (add address, resend); the forgot-password and sign-up paths keep their own limit; tests: TestMailResendSaysWhenToAskAgain | - |
| UserSettingsAccount | Sign-in methods card (SSO connected, link to Security for passkeys/password) | done | account.tmpl; data already in security/accountlinks.tmpl (AccountLinks) | S |
| UserSettingsAccount | Export your data (changesets, reviews, comments, settings; emailed link; requested/ready states) | needs-hub-backend | done: Sessions-style page /user/settings/asc/export (request, states, download, delete) over DataExport (routers/web/user/setting/asc_account.go). Open: the emailed link when the export is ready (hub mail on state change) | S |
| UserSettingsAccount | Delete account: typed username, 2FA code, blocked state when repos are owned, dialog focus return | ui-only-missing | Typed username and the blocked-by-repositories state are done (account.go DeleteAccount, account.tmpl). Open: 2FA code step and dialog focus return | S |
| UserSettingsAppearance | Theme cards (light, dark, system) with preview | done | templates/user/settings/appearance.tmpl | - |
| UserSettingsAppearance | Density (comfortable, compact) | done | appearance.tmpl, templates/base/head_style.tmpl (kept in localStorage, not per account; board says every device) | S |
| UserSettingsAppearance | Language select; hints switch; hidden comment types | done | appearance.tmpl | - |
| UserSettingsAppearance | Show agent features switch on this page (exists on /-/asc/agents only) | done | appearance.tmpl; handler routers/web/user AscAgentsFeaturesPost | S |
| UserSettingsAppearance | Reduce motion and Keyboard shortcuts switches | done | appearance.tmpl; gate in asc-keys.js and asc-ds/base.css (only the media query exists) | S |
| UserSettingsAppearance | Hide resolved comments switch | done | user setting `asc.hide_resolved` (default on = as before): switch on the Appearance page (`POST /user/settings/appearance/hide_resolved`), read in `routers/web/repo/issue.go` prepareHiddenCommentType and applied by `templates/repo/issue/view_content/conversation.tmpl` (resolved conversations on a change start collapsed with their Show button, or open when the switch is off); tests: TestHideResolvedSwitchIsSavedPerPerson, TestResolvedConversationStartsCollapsedUnlessTheSwitchIsOff. The conversations on the files-changed diff page are unchanged | - |
| UserSettingsAppearance | Date and time format (12/24 h, order) | needs-hub-backend | user column, modules/timeutil, DateUtils in modules/templates | M |
| UserSettingsAppearance | Apply instantly with an undo toast (no Save step); an app-wide toast/undo component does not exist | done | new web_src/js/features/ds-toast.js; reused by Appearance, Notifications, Privacy, Org people | M |
| UserSettingsNotifications | Page itself (not in the settings menu; only the Forgejo email preference on Account and /notifications/subscriptions exist) | done | `templates/user/settings/notifications.tmpl` and the Notifications entry of the settings menu (`routers/web/user/setting/notifications.go`): email per event, email mode, quiet hours with time zone, watched repositories, web push (found built during the audit of 2026-10-31; the row was out of date) | - |
| UserSettingsNotifications | Event x channel matrix (Inbox/Email/Push for review requested, mention, agent question, run finished, checks failed, restack conflict, queue result, policy change); needs-you rows locked on | needs-both | hub: new model asc_notify_pref + migration + dispatch in services/notify and routers/web/user/asc_inbox.go; asc: proto/asc/v1/inbox.proto has no preference or channel field | L |
| UserSettingsNotifications | Email frequency (right away / per-thread limit) and daily summary at a time | done | services/mailer (digest job in services/cron), user prefs | M |
| UserSettingsNotifications | Quiet hours (from/until, time zone aware) | done | prefs + services/mailer + push; needs time zone from Profile | M |
| UserSettingsNotifications | Push channel, permission prompt and denied state | done | web push (VAPID keys, service worker in public/, subscription table) | L |
| UserSettingsNotifications | Watching table with level and last notified, Unwatch with undo | done | exists as /notifications/subscriptions (Forgejo); move into the settings page, add level and last-notified columns | M |
| UserSettingsPrivacy | Page itself; Show activity and Hide email switches live on Profile instead | done | navbar.tmpl, new privacy.tmpl, move keep_activity_private and keep_email_private out of profile.tmpl | S |
| UserSettingsPrivacy | Show when I am online (green presence dot) | done | user column + presence from session activity; avatar template | M |
| UserSettingsPrivacy | Allow agents to read my profile | needs-both | hub user column; asc must honour it when agents read profile data;; Audit 2026-10-30 (part built): Switch exists; asc honouring it when an agent reads the profile was not found. | S |
| UserSettingsPrivacy | Blocked people list, block by username, unblock | done | templates/user/settings/blocked_users.tmpl, routers/web/user/setting/blocked_users.go | - |
| UserSettingsPrivacy | Block confirm dialog, 10 s undo on unblock; blocking also drops pending review requests (only follows are removed today) | done | `web_src/js/features/asc-confirm.js` (data-confirm-modal: the hub's styled confirm modal `block-user-modal` in `privacy.tmpl` opens before the block form is sent, also for the Enter key); Unblock redirects to the privacy page with an Undo that blocks the person again for 10 s (`undoUnblockOffer`: the address names a visible, now unblocked person and the time, judged again on the page; the offer is removed by script when the time is over); `services/user/block.go` dropPendingReviewRequests: blocking withdraws the open review requests the blocked person had on the blocker's repositories and the blocker had on the blocked person's pull requests; tests: TestBlockUser (Review requests), TestUndoUnblockOfferLastsTenSecondsAndNamesAVisibleUnblockedPerson, vitest asc-confirm; models/issues TestReviewRequestsWithNothingToDoKeepTheCallersTransaction (RemoveReviewRequest, AddTeamReviewRequest and RemoveTeamReviewRequest now commit when they have nothing to do, so a caller's transaction is no longer rolled back); models/issues TestReviewRequestsWithNothingToDoKeepTheCallersTransaction (RemoveReviewRequest, AddTeamReviewRequest and RemoveTeamReviewRequest now commit when they have nothing to do, so a caller's transaction is no longer rolled back). | - |
| UserSettingsSecurity | Change password | done | templates/user/settings/account.tmpl (password lives on Account, not Security) | - |
| UserSettingsSecurity | Password strength meter in words after 300 ms, show/hide buttons | done | account.tmpl + new JS | S |
| UserSettingsSecurity | Two-factor enable/disable/re-enroll, passkeys (WebAuthn) add/delete | done | templates/user/settings/security/twofa.tmpl, webauthn.tmpl | - |
| UserSettingsSecurity | Recovery codes: "8 of 10 unused", download as text file, shown once | needs-hub-backend | models/auth/twofactor.go (one scratch token only), security/twofa.tmpl | M |
| UserSettingsSecurity | Signed-in devices list (browser, place, last active), Sign out one / everywhere else | needs-hub-backend | asc-side credentials (tokens, SSH keys, agent tokens) with revoke and end-all are done on /user/settings/asc/sessions (Sessions service). Open: browser sessions (hub per-user session registry: browser, place, last active) | L |
| UserSettingsSecurity | Re-authentication prompt before password change or removing 2FA | needs-hub-backend | routers/web/user/setting/security + sudo-mode middleware | M |
| UserSettingsTokens | Token list (name, scopes, created, last used) with revoke and regenerate | done | templates/user/settings/applications.tmpl, routers/web/user/setting/access_token.go | - |
| UserSettingsTokens | Expiry per token (30 days etc.), Expires column, Expired chip | done | models/auth/access_token.go has no expiry; migration + access_token_edit.tmpl + check in services/auth; Audit 2026-10-30 (built): models/auth/access_token.go ExpiresUnix, GetAccessTokenBySHA refuses an expired token, access_token_edit.tmpl lifetime select, applications.tmpl Expires and Expired chip, migration v17z_asc_token_expiry | M |
| UserSettingsTokens | ASC scope set (Repositories, Changesets and stacks, Agent runs, Organizations, Profile) as None/Read/Write segmented group; hub shows Forgejo categories in selects | needs-asc-backend | hub: models/auth/access_token_scope.go + access_token_edit.tmpl; asc: crates/asc-serve/src/authn.rs must accept and enforce them; Audit 2026-10-30 (hub built): scopes read/write changes and runs in models/auth/access_token_scope.go, the form lists Changesets and stacks and Agent runs, services/ascforward/scope.go holds a token to them for every forwarded call, routers/private/asc.go takes an area. Open: asc should send area on its own authz calls (crates/asc-serve/src/forgejo.rs) so a direct asc connection is held to them too | L |
| UserSettingsTokens | Token created: copy-once card with Copy and "shown once" text (today a flash message) | done | routers/web/user/setting/access_token.go lines 313 and 341, applications.tmpl | S |
| UserSettingsTokens | Workload identity (issuer, subject, last used, remove, add) | done | templates/user/settings/authorized_integrations.tmpl | - |
| UserSettingsTokens | "Prefer no tokens" card with asc auth login commands | ui-only-missing | applications.tmpl static card;; Audit 2026-10-30 (was marked done, part missing): Card exists; copy differs from the board (no Prefer no tokens heading, no asc auth token --scope line). | S |
| UserSettingsKeys | SSH keys (fingerprint, added, last used, delete) and GPG keys | done | templates/user/settings/keys_ssh.tmpl, keys_gpg.tmpl | - |
| UserSettingsKeys | ASC signing keys list, add, revoke with state chips | done | templates/user/settings/asc_keys.tmpl, routers/web/user/setting/asc_keys.go | - |
| UserSettingsKeys | Rotate action that keeps the old key verifying past signatures | done | services/asctrust/rotate.go RotateKey (add, then revoke with its own op id), routers/web/user/setting/asc_keys.go AscKeysRotate, asc_keys.tmpl details form | - |
| UserSettingsKeys | Sign my changesets by default switch | needs-both | hub pref + asc config the CLI reads (no field in proto/asc/v1/signing.proto) | M |
| UserSettingsKeys | Delete confirm that lists what stops working | done | keys_ssh.tmpl modal text | S |
| UserSettingsApps | OAuth2 apps, authorized apps with revoke, client secret shown once, regenerate | done | templates/user/settings/applications_oauth2*.tmpl, grants_oauth2.tmpl | - |
| UserSettingsApps | Webhooks table: events column, last delivery status text and time, Replay from the list | ui-only-missing | templates/repo/settings/webhook/base_list.tmpl is old markup (dot + URL only); data from hook_task | M |
| UserSettingsOrgs | Organization rows with role chip, "38 people - 14 repositories", Settings for owners | done | templates/user/settings/organization.tmpl (only avatar, description, Open, Leave) | S |
| UserSettingsOrgs | Pending invitations with Accept and Decline | done | `models/organization/team_invite_pending.go` (GetPendingInvitesForUser: invitations made for the account or for an address of theirs that they confirmed, not for an unconfirmed one; DeclineInviteForUser answers "not found" for an invitation that is not theirs), `routers/web/user/setting/asc_org_invites.go`, Invitations card with Accept (the existing invitation page) and Decline (`POST /user/settings/organization/decline`) in `templates/user/settings/organization.tmpl`; tests: TestPendingInvitesForUser | - |
| UserSettingsOrgs | Last owner cannot leave (explains transfer first) | done | org members leave action message and organization.tmpl modal | S |
| UserSettingsRepos | Owned repositories list, unadopted adopt/delete | done | templates/user/settings/repos.tmpl | - |
| UserSettingsRepos | Stack count and storage target per row; Transfer dialog (new owner + repository name) | needs-both | hub: repos.tmpl (repo transfer exists in repo settings); asc: stack counts (Stacks service) and Storage.AssignRepo target | M |
| UserSettingsStorage | Targets list and state chips, add form (name, region, endpoint, bucket, prefix, addressing, keys), enable, disable, delete | done | routers/web/shared/byoweb/storage.go, templates/shared/asc/byo_storage*.tmpl | - |
| UserSettingsStorage | Verify handshake: five steps with latency, failure message in words | done | byo_storage_target.tmpl (checks from Storage.VerifyTarget) | - |
| UserSettingsStorage | Per repository assignment ("where new objects go") | done | byoweb StorageAssign, byo_storage_repo.tmpl | - |
| UserSettingsStorage | Migration progress (copied/total) and cancel | done | byoweb MigrationStart/Cancel/Status, web_src/js/features/asc-byo.js | - |
| UserSettingsStorage | Migration Pause/resume, bytes and ETA, mismatch count, paused state | needs-both | asc: proto/asc/v1/storage.proto has no pause RPC, no bytes_total, no mismatch counter; hub: byo_storage_repo.tmpl + asc-byo.js | M |
| UserSettingsStorage | Card text "Used by N repositories - verified 2 days ago" | ui-only-missing | byo_storage_cards.tmpl (usage count from the repo list) | S |
| UserSettingsAI | Key list with provider, hint, model, tested time, spend meter, Test/Details; add form; state chips | done | routers/web/shared/byoweb/aikeys.go, templates/shared/asc/byo_ai*.tmpl | - |
| UserSettingsAI | Test connection result with latency (ms), polite live region | done | byo_ai_key.tmpl, AITest | - |
| UserSettingsAI | Test result lists model count ("9 models available") | done | routers/web/shared/byoweb/aikeys.go renderAIKey + byo_ai_key.tmpl (asc-test-models) | - |
| UserSettingsAI | Key detail tabs (Usage, Limits, Models, Activity) and spend-by-day chart | ui-only-missing | byo_ai_key.tmpl is one long page with a by-day table; split into tabs, add a small bar chart | M |
| UserSettingsAI | Warn me at 80% (inbox + email), Stop agents at the cap (pause with a question) | done | AiKeyShares.GetKeyCaps/SetKeyCaps: byoweb/aishares.go AICaps, templates/shared/asc/byo_ai_sharing.tmpl. The warning goes out from asc (audit + notification event) | - |
| UserSettingsAI | Who can use it: allow my agents, share with organization (org owners attach it, see spend not key) | done | AiKeyShares.ShareKey/ListShares/RevokeShare/ShareUsage/ListSharedWithMe: byoweb/aishares.go, byo_ai_sharing.tmpl (key page), byo_ai_shared.tmpl (list) | - |
| UserSettingsAI | Activity tab (who used the key, runs, tokens) | needs-both | asc: AiProviders.Usage groups by day/model/repo/agent only, no event list; hub: new tab | M |
| UserSettingsAI | Masked key field with Show button; remove needs a ticked box | done | byo_field.tmpl, byo_ai_key.tmpl delete confirm | - |
| UserSettingsUsage | Four tiles (object storage, agent minutes, AI spend, bandwidth) with meters and "of N" text | done | Usage service: routers/web/shared/usageweb, templates/shared/asc_usage.tmpl; /user/settings/asc/usage, /org/{org}/settings/asc/usage, /admin/asc/usage | - |
| UserSettingsUsage | By repository table (storage, agent minutes, AI spend) | done | usageweb + asc_usage.tmpl repository table (largest AI spend first, 200 rows) | - |
| UserSettingsUsage | Over-quota banner, empty month, "BYO does not count" note | done | asc_usage.tmpl over-quota banner, empty month, BYO note (byo_ai_spend, byo_storage) | - |
| UserSettingsUsage | Menu entry always shown (hub hides it unless quota is enabled) | done | navbar.tmpl: Usage shown whenever [asc] is on (the Forgejo quota entry keeps its own guard) | - |
| MobileUserSettings* | Settings menu as one list page, Other group (Site administration for admins, Sign out), bottom nav Inbox/Changes/Explore/Search/Me | done | templates/user/settings/navbar.tmpl, templates/base/head_navbar.tmpl | - |
| MobileUserSettings* | Save bar sticks above the bottom nav once a field changes | done | same savebar work as Profile | S |
| OrgSettings | Profile fields, logo upload/remove, visibility, repo-admin team access, repository creation limit | done | templates/org/settings/options.tmpl, routers/web/org/setting.go | - |
| OrgSettings | Who can create repositories (owners / maintainers), base permission for members, default repository visibility as org default | needs-hub-backend | models/organization columns, options.tmpl, repo create permission checks | M |
| OrgSettings | Agents card: allow agents, agent changesets need a person, monthly agent spend ceiling | done | routers/web/org/setting/asc_agents.go, templates/org/settings/asc_agents.tmpl (OrgSettings.SetAgentPolicy, agent identities); site switch and default budget at /admin/asc/agents | - |
| OrgSettings | Default merge policy shown + Edit policy (repositories inherit and can only tighten) | done | routers/web/org/setting/asc_agents.go AscMerge, templates/org/settings/asc_merge.tmpl (OrgSettings.SetMergeDefaults). Open: preview on a repository page with ResolveMergePolicy (S) | - |
| OrgSettings | Transfer ownership | done | `services/org/transfer.go` TransferOwnership (an owner makes a member an owner; optionally leaves the Owners team in the same transaction, so there is never a moment without an owner), `routers/web/org/asc_transfer.go`, card on the Delete page `templates/org/settings/delete.tmpl` with the organization name typed to confirm; the organization has no single owner field, owners are the Owners team; tests: TestTransferOwnership | - |
| OrgSettings | Delete organization with typed handle | done | templates/org/settings/delete.tmpl | - |
| OrgSettings | Owner-only banner and disabled fields for non-owners (settings routes require owner today) | ui-only-missing | routers/web/web.go org settings group, options.tmpl | S |
| OrgSettings | Left menu: AI keys, Storage, Audit, Packages, Webhooks, Applications, Runners, Blocked users, Usage | done | templates/org/settings/navbar.tmpl | - |
| OrgPeople | Member rows with avatar, owner/member chips, 2FA chip, remove/leave, public/private toggle | done | templates/org/member/members.tmpl | - |
| OrgPeople | Search box, filter chips with counts (Everyone, Owners, 2FA off done; Pending needs the invitation list), pagination | done | templates/org/member/members.tmpl, routers/web/org/members.go (search, Everyone, Owner, 2FA off, Pending, pager "n of m people") | M |
| OrgPeople | Change role menu with Maintainer role and undo toast; last owner cannot be demoted | partial | routers/web/org/people.go: Owner, Maintainer (a team with the administrator permission; "Maintainers" is made when none exists) and Member; the last owner is protected. No undo toast | M |
| OrgPeople | Row detail: teams list, last active, changesets count | partial | teams, last active and the changes landed in the last year (Insights.ListContributors per repository, cached a minute). Needs asc: a per-person total across organization repositories | M |
| OrgPeople | Agents section (agent identity, run by, scoped to paths, Manage) | done | routers/web/org/people.go orgAgents (OrgSettings.ListAgents), members.tmpl section | L |
| OrgPeople | Pending invitations (email, role, invited by, Resend, Revoke) | done | routers/web/org/people.go pendingInvites, Resend and Revoke actions, members.tmpl section | M |
| OrgPeople | Invite dialog accepting several emails and a role | done | add_org_member.tmpl: names and addresses separated by commas, spaces or lines, a role, extra teams under a disclosure | M |
| OrgTeams | Team cards (name, access, description, avatars, member and repo counts), create/edit/delete team | done | templates/org/team/teams.tmpl, routers/web/org/teams.go | - |
| OrgTeams | Selecting a card loads detail below without leaving (members + repositories with access) | ui-only-missing | teams.tmpl links to separate pages; add htmx panel | M |
| OrgTeams | Policy owner note: team linked to a path pattern in merge policy | needs-both | asc: policy path-owner rule naming a hub team (crates/asc-core/src/policy.rs); hub: team page card | L |
| OrgTeams | Agents as team members with a path scope | needs-both | asc: agent identity + path scope; hub: team members list | L |
| NewOrg | Handle field, visibility, create | done | templates/org/create.tmpl, routers/web/org/org.go | - |
| NewOrg | Display name field, "Who owns it", "Create a first team Everyone", "Allow agents" switch | needs-both | hub: forms.CreateOrgForm + create.tmpl; asc: agents flag per org (see OrgSettings) | M |
| NewOrg | Handle availability check as you type, reserved-name error, live preview card, loading button | ui-only-missing | create.tmpl + JS (availability endpoint is hub S) | M |
| AdminDashboard | Stat tiles linking to pages (people, repos, orgs, issues) | done | templates/admin/dashboard.tmpl, routers/web/admin/overview.go | - |
| AdminDashboard | Tiles "Changesets today (31% by agents)", "Agent runs today (96.4% finished)", "+38 this week" | needs-both | asc: new admin stats RPC; hub: overview.go dashboardFacts | M |
| AdminDashboard | Needs attention: runners offline, reports, notices | done | overview.go dashboardFacts | - |
| AdminDashboard | Needs attention: backup overdue with "Back up now" | needs-both | asc: backup status RPC (only CLI asc serve backup exists, crates/asc-serve/src/backup.rs); hub: attention item | M |
| AdminDashboard | System card rows: object storage usage and mail queue age (database, queues, runners exist) | needs-both | asc: storage usage; hub: overview.go system rows | M |
| AdminDashboard | Recent audit entries card | done | `routers/web/admin/asc_audit_recent.go` ascRecentAudit (Audit.List for the whole server as the signed-in site administrator, five newest, left out without asc or the audit service), card in `templates/admin/dashboard.tmpl`; tests: TestDashboardShowsTheNewestAuditEntries, TestDashboardLeavesTheCardOutWithoutTheAuditService | - |
| AdminDashboard | Numbers refresh every 30 s without layout shift | done | dashboard.tmpl + htmx polling on /admin | S |
| AdminHealth | Self check list with passed/warn/fail chips, Copy report, Run again | done | templates/admin/self_check.tmpl, overview.go healthFacts | - |
| AdminHealth | Checks for disk fill rate, object storage probe, signing key validity, outgoing mail queue, time sync, TLS expiry (hub checks only DB, collation, cache, runners, notices) | needs-both | hub: overview.go healthFacts; asc: health RPC (object store probe, signing key expiry) | M |
| AdminHealth | Gauges CPU, Memory, Disk, Object storage with percent text (hub shows uptime, memory, goroutines, GC) | done (CPU and Disk; Object storage is not drawn: the hub has no number for it) | `routers/web/admin/asc_gauges.go`: CPU is the one-minute load per core from /proc/loadavg, Disk is how full the file system of the hub's data directory is (statfs, as df); a host that does not give the number has no gauge; `templates/admin/self_check.tmpl`; tests: TestLoadPercent, TestDiskPercent, TestDiskGaugeOfTheWorkingDirectory | S |
| AdminHealth | Running state (steps appear one by one), could-not-run state | ui-only-missing | self_check.tmpl + htmx streaming | M |
| AdminUsers | Search, filter chips (Admins, 2FA off, Inactive, Suspended), table with role/2FA/repos/last active/joined, sort, pagination | done | templates/admin/user/list.tmpl, routers/web/admin/users.go | - |
| AdminUsers | Counts on the filter chips (Admins 6, 2FA off 212 ...) | done | `routers/web/admin/asc_user_counts.go` adminUserCounts (one count per chip, whatever the search or chip in use), shown by the existing pill count in `templates/admin/user/list.tmpl`; a zero is not drawn; tests: TestAdminUserCountsMatchTheFilters | - |
| AdminUsers | Row checkboxes, "2 selected" bar, Require 2FA and Suspend bulk actions with affected-people dialog | needs-hub-backend | list.tmpl, new POST /admin/users/bulk in web.go + users.go, audit entries | M |
| AdminUsers | New person, Edit | done | templates/admin/user/new.tmpl, edit.tmpl | - |
| AdminUser | Single page with account facts, emails, repos, orgs, delete | done | templates/admin/user/view.tmpl | - |
| AdminUser | Tabs Overview/Emails/Keys/Sessions/Activity | ui-only-missing | view.tmpl is one scroll; keys, sessions and activity panels are missing | M |
| AdminUser | Reset 2FA and Suspend as separate buttons that ask for a reason written to the audit log (today checkboxes inside the edit form) | done | `routers/web/admin/user_tabs.go` UserReset2FA and UserSuspend: separate actions that ask for a reason, which is appended to asc's audit log (host.user.2fa_reset, host.user.suspended, host.user.unsuspended) (found built during the audit of 2026-10-31) | - |
| AdminUser | Usage block (storage, agent minutes) and agents owned | needs-both | asc usage RPC (see UserSettingsUsage); view.tmpl | M |
| AdminUser | Sessions tab (devices, revoke) | needs-both | same session registry as UserSettingsSecurity | L |
| AdminOrgs | Organizations table | done | templates/admin/org/list.tmpl | - |
| AdminOrgs | Storage column (garage-eu / Server default) and Open button | needs-both | asc Storage.ListTargets/AssignRepo for the owner; hub list.tmpl (shows a Teams column instead) | S |
| AdminRepos | Repository table, unadopted list, delete | done | templates/admin/repo/list.tmpl, unadopted.tmpl | - |
| AdminRepos | Storage column and "storage mismatch" warning; delete needs full name typed | needs-both | asc storage assignment per repo; list.tmpl; delete modal in admin/repo | M |
| AdminRunners | Runner list, status, labels, version, register, details | done | templates/admin/actions.tmpl, runners/*.tmpl | - |
| AdminRunners | Drain action and draining state | done | `action_runner.draining` (migration `v18a_asc_runner_drain`), `CreateTaskForRunner` offers a draining runner nothing (`models/actions/task.go`), `POST .../runners/{id}/drain` (field drain 1 or 0) for every level that edits runners (`routers/web/shared/actions/runners.go` RunnerDrainPost), a Drain / Take jobs again button and a Draining chip on the runner page and in the list; a draining runner finishes the job it has; tests: TestDrainingRunnerIsGivenNoNewTask, TestRunnerDrainPostSetsAndClearsTheSwitchForAnAdministrator, TestRunnerDrainPostRefusesARunnerOfAnotherOwner | - |
| AdminRunners | Agent runners (agent-box-1, "Agent run #214", vCPU) | needs-both | asc agent runs on runners; hub shows only Forgejo actions runners | L |
| AdminRunners | Registration token that expires in one hour, shown once, in a copy block | ui-only-missing | runners/create.tmpl, setup.tmpl (token is long lived in Forgejo) | S |
| AdminConfig | Server settings read only (domain, protocol, SSH, storage, mail) | done | templates/admin/config_server.tmpl (a much longer Forgejo set) | - |
| AdminConfig | Runtime switches: Allow registration, Require email confirmation (runtime set has only avatar settings and editor apps) | done | `templates/admin/config_runtime.tmpl`: Allow registration and Require email confirmation (the latter refused while no mailer is set) over the runtime config (`routers/web/admin/config.go`) (found built during the audit of 2026-10-31) | - |
| AdminConfig | Allow agents on this server; Default agent budget | needs-both | asc: instance flag and default budget setter (agents.proto has AgentBudgetLimits only); hub: switch + field | M |
| AdminConfig | Restart-required banner, invalid value error, file and line shown, Copy key/value | ui-only-missing | config_server.tmpl + config.js | S |
| AdminMonitor | Queues table with Flush, cron table with Run now, stack traces | done | templates/admin/queue.tmpl, cron_result.tmpl, stacktrace.tmpl | - |
| AdminMonitor | Flush confirmation with count; Run now streams output; "Verify audit chain" cron task | needs-both | hub: queue.tmpl modal and streaming; asc: scheduled audit verify (Audit.VerifyChain exists, no schedule) | M |
| AdminAudit | Hash-chain banner, filters, details, export, verify | done | routers/web/admin/asc_audit.go, templates/admin/asc_audit.tmpl, routers/web/shared/trustweb | - |
| AdminAudit | Signed export file for the range | needs-asc-backend | Audit.Export streams a BLAKE3 digest but no signature (proto/asc/v1/audit.proto) | S |
| AdminBackups | Page with how to run backups (read-only text) | done | templates/admin/backups.tmpl | - |
| AdminBackups | Schedule, target, next backup, Back up now, history with Retry/Restore/Verify, in-progress meter, RESTORE typed confirmation | done | routers/web/admin/asc_backups.go + templates/admin/asc_backups_board.tmpl, asc_backup_restore.tmpl (Backups service: schedule, run, history, verify, staged restore with typed RESTORE). Retry of a failed run is Back up now | - |
| AdminModeration | Reports queue with Dismiss, mark handled, content actions | done | templates/admin/moderation/reports.tmpl, routers/web/admin/reports.go | - |
| AdminModeration | Reason per action written to the audit log; reports grouped by target; appeal state; agent reporters | needs-hub-backend | Built: the reason per action (a Reason box on every report, required to remove content or an account; `routers/web/admin/asc_moderation.go` appends host.moderation.acted with the content, the actions taken and the reason to the audit log of asc, only for an action that ended without a warning; asc: branch feat/audit-host-moderation adds the action to the catalogue; tests: TestPerformActionNeedsAReasonToRemoveAndRecordsWhatItDid). Open: reports grouped by target (the detail page lists them per target already), the appeal state (no model), agent reporters (no model). reports.go PerformAction, moderation service, reports.tmpl | M |
| AdminAuth | Sources list with edit and add | done | templates/admin/auth/list.tmpl | - |
| AdminAuth | People count per source; Local passwords row (admins only option); Test sign-in showing claims; remove blocked while people only use it | needs-hub-backend | Built: the people per source (`routers/web/admin/asc_auth_counts.go`, a People column and the built-in Local passwords row, which is listed even when it is the only source) and the refusal to remove a source that people still use (found built: `ErrSourceInUse` in `DeleteAuthSource`); tests: TestAuthSourceCountsPeoplePerSource, TestAuthenticationsPageShowsTheLocalRowAndThePeopleOfEachSource. Open: the admins-only option of the Local passwords row and the Test sign-in that shows the claims (needs a probe of LDAP, SMTP and OAuth sources) | M |
| AdminHooks | System and default webhooks, OAuth applications list | done | templates/admin/hooks.tmpl, applications/list.tmpl | - |
| AdminHooks | Columns scope, events, last delivery with status text; Replay from the list; secret regenerated toast | ui-only-missing | hooks_list.tmpl uses the old base_list markup | M |
| AdminPackages | Packages table and delete | done | templates/admin/packages/list.tmpl | - |
| AdminPackages | Run cleanup rules with a preview of counts (hub runs CleanupExpiredData directly) | done | `services/packages/cleanup/preview.go` PreviewRules (per enabled rule: owner, type, versions and packages it would delete; deletes nothing), `routers/web/admin/asc_packages_cleanup.go` (`GET/POST /admin/packages/cleanup/rules`: the preview page with a Run button that runs ExecuteCleanupRules), link from the packages list; tests: TestPreviewRulesCountsWhatARuleWouldDeleteAndDeletesNothing | - |
| AdminNotices | Notices list, dismiss, dismiss all with confirm | done | templates/admin/notice.tmpl | - |
| Drafts | List of drafts from the hub (name, repo, change count, saved time), terminal card, empty and error states | done | routers/web/user/asc_drafts.go, templates/user/asc_drafts.tmpl | - |
| Drafts | Location line "On this laptop and on the hub / only on this laptop" | needs-asc-backend | Cloud.GetWorkspace returns hub heads only; local-only drafts need a client report RPC | M |
| Drafts | Continue (open in editor or show asc stack resume) and Discard buttons, 30-day undo, discarded banner | needs-both | asc: Cloud.Purge deletes for good, add soft discard + undo; hub: POST /-/asc/drafts/discard and undo, buttons in asc_drafts.tmpl | M |
| Issues | List with filters, sort, open/closed counts, label rail, milestone rail | done | templates/repo/issue/list.tmpl, filters.tmpl | - |
| Issues | Search accepts label: and author: filters and chips apply the same | done | `routers/web/repo/asc_issue_search.go` (parseIssueQuery takes `label:name`, `label:"two words"` and `author:login` out of the search box; the label ids and the poster feed the same variables as the label and author menus, so the chips and menus show them chosen; a name that matches nothing matches nothing; labels of the repository and of its organization; at most 10 of each), used by `issues()` in `routers/web/repo/issue.go`; tests: TestParseIssueQuery, TestQueryFiltersResolveToIdsAndUnknownNamesMatchNothing, integration TestIssueSearchLabelAndAuthorFilters. The indexer is not asked for the filters: the label and poster filters were already there | - |
| Issues | Row chips "Fixed by stack X", linked stack with state, "Quarantined by" flaky chip | needs-hub-backend | link table issue to PullRequestAscStack (models/issues/pull_asc_stack.go), list.tmpl, queue quarantine data in routers/web/repo/asc_queue.go | L |
| Issues | Linking a stack closes the issue when it lands, with undo | needs-hub-backend | landing path in routers/private/asc_request_merge.go + services/issue close; reopen action | M |
| Issues | New issue as a dialog that keeps a draft ("Save as draft") | ui-only-missing | templates/repo/issue/new.tmpl is a page; localStorage draft in repo-issue.js | M |
| Issues | No results with "Clear filters", loading skeleton | ui-only-missing | list.tmpl | S |
| IssueView | Timeline, comment box, close/reopen, assignees, labels, milestone, participants, lock | done | templates/repo/issue/view*.tmpl | - |
| IssueView | Agent chip on comments and assignees; agents cannot approve or merge | needs-both | asc agent identity surfaced to hub users (no agent principal in hub models/user); view_content/show_role.tmpl | M |
| IssueView | Linked stacks rail ("1 of 4 landed") and timeline event "coder-2 opened stack" | needs-hub-backend | same link table; templates/repo/issue/view_content/sidebar, comments.tmpl | M |
| IssueView | Closed by a landed stack banner with Reopen | needs-hub-backend | view_content.tmpl + close reason on the issue | S |
| IssueView | Comment draft kept; Cmd+Enter posts | done | ComboMarkdownEditor.js has Ctrl+Enter but no draft store | S |
| Projects | Kanban board with columns, drag, edit/close/delete | done | templates/projects/view.tmpl | - |
| Projects | Cards that are stacks following real state; Landed column by stack state | needs-hub-backend | project_issue model holds issues/PRs; map PullRequestAscStack state to a column, issue/card.tmpl | L |
| Projects | Move menu (keyboard alternative) announcing the new column | ui-only-missing | view.tmpl + web_src/js/features/repo-projects.js | M |
| Projects | Add card, Switch project select, rename and hide columns | ui-only-missing | view.tmpl, column hide flag (small migration) | M |
| Projects | Empty, loading and error ("request id") states | ui-only-missing | projects/list.tmpl | S |
| Packages | Package list with type, version, downloads, filter by type | done | templates/package/shared/list.tmpl | - |
| Packages | Type chips with counts (a select today) | ui-only-missing | shared/list.tmpl | S |
| Packages | Visibility chip per package (Public/Private/Internal) | needs-hub-backend | packages inherit owner visibility; per-package flag in models/packages | M |
| Packages | Package page views Install/Versions/Dependencies/Settings as tabs | ui-only-missing | templates/package/view.tmpl (single page + /versions) | M |
| Packages | "Published from acme/payments by a landed stack", "Signed with release key" | needs-both | asc: publish provenance and release signature record; hub: package metadata + view.tmpl | L |
| Packages | Cleanup rule preview ("removes 14 versions of 6 packages, frees 1.3 GB") | done | templates/org/settings/packages_cleanup_rules_preview.tmpl | - |
| Packages | Error state with request id and Status page link | ui-only-missing | package list handler error render | S |
| MobileAdmin* | Same data as desktop in stacked groups, back arrow to the admin menu, sticky action bar | done | templates/admin/navbar.tmpl (shared settings-nav shell) | - |
| MobileIssues/Projects/Packages/Drafts/Org* | Single-column twins; same gaps as the desktop rows above; sticky primary bar on forms | ui-only-missing | per-row work above | S |
