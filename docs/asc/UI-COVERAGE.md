# UI coverage: boards, routes and templates

Generated on the merge of the four page groups (work, repo, review, admin) on top of the design system. Source of the board list: `DESIGN-V3.md` (95 desktop boards, 96 mobile boards; the mobile twin of a board is the same page at 390 px). Source of the routes: `routers/web/web.go`. Source of the markup state: a scan of `templates/**` for Fomantic classes (`ui segment`, `ui form`, `ui menu`, `ui grid`, `ui button`, any class list that starts with or holds the word `ui`) against design-system classes (`ds-`, and the page prefixes `rv-` review, `rp-` repository, `wk-` work, `adm-` admin, `ast-` trust, `site-`, `hub-`, `st-` status, `ins` install).

## Status words

| Status | Meaning |
| --- | --- |
| design system | the page template has design-system markup and no Fomantic layout classes (hook classes such as `ui dropdown` may remain, at most 2 and less than one eighth of the design-system classes) |
| partial | the board has several templates and some are design system, some old, or the template has a design-system shell with Fomantic inside |
| old Fomantic markup | the template has Fomantic classes and no design-system classes |
| asc era | markup from before the v3 boards (`asc-` classes on the token CSS), no Fomantic |
| missing | the board has no template at all |

## Summary

Boards (95 desktop boards, each with a mobile twin except the two system boards):

| Status | Boards |
| --- | --- |
| design system | 64 |
| partial | 27 |
| old Fomantic markup | 4 |
| asc era | 0 |
| missing | 0 |

All templates (without mail, swagger, demo):

| Status | Templates |
| --- | --- |
| design system | 268 |
| design system shell with old markup inside | 40 |
| old Fomantic markup | 223 |
| asc era | 8 |
| no class markup (data, includes, emails) | 137 |

Page templates reached by a route of `web.go` (138):

| Status | Pages |
| --- | --- |
| design system | 63 |
| partial | 16 |
| old Fomantic markup | 45 |
| asc era | 0 |
| no class markup | 14 |

Page templates reached by a route with no board: 56.

## Boards

| Board | File (mobile twin) | Template(s) | Status | Still old |
| --- | --- | --- | --- | --- |
| A1 Design system: tokens and components | `components` | none | design system |  |
| A2 Shell: header, menus, palette, footer | `Shell` | `base/head_navbar.tmpl`, `base/footer_content.tmpl` | design system |  |
| A3 Landing (signed out) | `Main` | `home.tmpl` | design system |  |
| A4 Sign in, sign up, recovery | `Onboarding` (`Mobile-Onboarding`) | `user/auth/signin.tmpl`, `user/auth/signup.tmpl`, `user/auth/twofa.tmpl`, `user/auth/webauthn.tmpl`, `user/auth/forgot_passwd.tmpl`, `user/auth/reset_passwd.tmpl` (+2) | design system |  |
| A5 First-run setup | `Install` (`Mobile-Install`) | `install.tmpl`, `post-install.tmpl` | design system |  |
| A6 Get started (empty hub) | `GetStarted` (`Mobile-GetStarted`) | `user/dashboard/asc_home.tmpl`, `asc/get_started.tmpl`, `home.tmpl` | design system |  |
| A7 Download ASC | `Download` (`Mobile-Download`) | `download.tmpl` | design system |  |
| A8 Explore: repositories | `Explore` (`Mobile-Explore`) | `explore/repos.tmpl`, `explore/repo_list.tmpl`, `explore/navbar.tmpl` | design system |  |
| A9 Explore: people and agents | `ExploreUsers` (`Mobile-ExploreUsers`) | `explore/users.tmpl`, `explore/user_list.tmpl` | design system |  |
| A10 Explore: organizations | `ExploreOrgs` (`Mobile-ExploreOrgs`) | `explore/user_list.tmpl`, `explore/navbar.tmpl` | design system |  |
| A11 Documentation | `Docs` (`Mobile-Docs`) | `asc/docs.tmpl` | design system |  |
| A12 About ASC Hub | `About` (`Mobile-About`) | `asc/about.tmpl` | design system |  |
| A13 Error and status pages | `Errors` (`Mobile-Errors`) | `status/404.tmpl`, `status/413.tmpl`, `status/500.tmpl`, missing: `status/403.tmpl`, `status/503.tmpl`, `status/offline.tmpl` | partial |  |
| B1 Dashboard | `Dashboard` (`Mobile-Dashboard`) | `user/dashboard/asc_home.tmpl` | design system |  |
| B2 Inbox | `Inbox` (`Mobile-Inbox`) | `user/asc_inbox.tmpl`, `user/notification/notification.tmpl` | design system |  |
| B3 Drafts | `Drafts` (`Mobile-Drafts`) | `user/asc_drafts.tmpl` | design system |  |
| B4 Profile | `Profile` (`Mobile-Profile`) | `user/profile.tmpl`, `user/asc_heatmap.tmpl`, `user/asc_agents.tmpl`, `user/overview/header.tmpl` | partial | `user/asc_agents.tmpl` (4 old / 0 ds) |
| B5 Organization profile | `OrgProfile` (`Mobile-OrgProfile`) | `org/home.tmpl`, `org/header.tmpl` | design system |  |
| B6 Organization people | `OrgPeople` (`Mobile-OrgPeople`) | `org/member/members.tmpl`, `org/member/add_org_member.tmpl` | partial | `org/member/add_org_member.tmpl` (4 old / 15 ds) |
| B7 Organization teams | `OrgTeams` (`Mobile-OrgTeams`) | `org/team/teams.tmpl`, `org/team/members.tmpl`, `org/team/repositories.tmpl`, `org/team/new.tmpl` | partial | `org/team/members.tmpl` (4 old / 41 ds), `org/team/repositories.tmpl` (5 old / 33 ds) |
| B8 Organization packages | `Packages` (`Mobile-Packages`) | `package/view.tmpl`, `org/settings/packages_cleanup_rules_edit.tmpl`, `package/shared/list.tmpl`, `package/content/alpine.tmpl`, `package/content/alt.tmpl`, `package/content/arch.tmpl` (+23) | partial | `package/view.tmpl` (6 old / 16 ds), `package/content/alpine.tmpl` (8 old / 0 ds), `package/content/alt.tmpl` (9 old / 0 ds), `package/content/arch.tmpl` (6 old / 0 ds) (+23) |
| B9 Organization projects | `Projects` (`Mobile-Projects`) | `projects/list.tmpl`, `projects/view.tmpl`, `projects/new.tmpl` | partial | `projects/list.tmpl` (4 old / 39 ds), `projects/view.tmpl` (11 old / 19 ds), `projects/new.tmpl` (6 old / 0 ds) |
| B10 Organization settings | `OrgSettings` (`Mobile-OrgSettings`) | `org/settings/options.tmpl`, `org/settings/layout_head.tmpl`, `org/settings/navbar.tmpl` | design system |  |
| B11 New organization | `NewOrg` (`Mobile-NewOrg`) | `org/create.tmpl` | design system |  |
| C1 New repository | `NewRepo` (`Mobile-NewRepo`) | `repo/create.tmpl`, `repo/create_basic.tmpl`, `repo/create_advanced.tmpl`, `repo/create_from_template.tmpl`, `repo/migrate/codebase.tmpl`, `repo/migrate/forgejo.tmpl` (+12) | partial | `repo/migrate/codebase.tmpl` (10 old / 7 ds), `repo/migrate/git.tmpl` (6 old / 7 ds), `repo/migrate/gitbucket.tmpl` (12 old / 7 ds), `repo/migrate/gitea.tmpl` (12 old / 7 ds) (+7) |
| C2 Repository home | `Repo` (`Mobile-Repo`) | `repo/home.tmpl`, `repo/asc_about.tmpl`, `repo/clone_asc_tabs.tmpl`, `repo/header.tmpl` | partial | `repo/asc_about.tmpl` (3 old / 38 ds) |
| C3 Code: browse and blame | `Code` (`Mobile-Code`) | `repo/view_file.tmpl`, `repo/blame.tmpl`, `repo/asc_symbols.tmpl` | partial | `repo/asc_symbols.tmpl` (7 old / 0 ds) |
| C4 Changeset | `Changeset` (`Mobile-Changeset`) | `repo/commit_page.tmpl` | design system |  |
| C5 History and changeset graph | `History` (`Mobile-History`) | `repo/commits.tmpl`, `repo/asc_graph.tmpl`, `repo/asc_ops.tmpl` | partial | `repo/asc_graph.tmpl` (13 old / 0 ds) |
| C6 Refs and tags | `Refs` (`Mobile-Refs`) | `repo/branch/list.tmpl`, `repo/tag/list.tmpl` | design system |  |
| C7 Compare versions | `Compare` (`Mobile-Compare`) | `repo/asc_compare.tmpl` | design system |  |
| C8 Search: text, symbol, meaning | `Search` (`Mobile-Search`) | `repo/asc_search.tmpl`, `repo/asc_symbols.tmpl` | partial | `repo/asc_symbols.tmpl` (7 old / 0 ds) |
| C9 Releases | `Releases` (`Mobile-Releases`) | `repo/release/list.tmpl`, `repo/release/new.tmpl`, `repo/tag/list.tmpl` | partial | `repo/release/new.tmpl` (10 old / 8 ds) |
| C10 Dependencies | `Dependencies` (`Mobile-Dependencies`) | `repo/asc_deps.tmpl`, `repo/asc_deps_plan.tmpl` | design system |  |
| C11 Issues | `Issues` (`Mobile-Issues`) | `repo/issue/list.tmpl`, `repo/issue/filters.tmpl`, `repo/issue/search.tmpl`, `repo/issue/labels.tmpl`, `repo/issue/milestones.tmpl`, `repo/issue/new.tmpl` | partial | `repo/issue/filters.tmpl` (1 old / 2 ds), `repo/issue/search.tmpl` (2 old / 1 ds), `repo/issue/labels.tmpl` (2 old / 0 ds), `repo/issue/milestones.tmpl` (3 old / 0 ds) (+1) |
| C12 One issue | `IssueView` (`Mobile-IssueView`) | `repo/issue/view.tmpl`, `repo/issue/view_content.tmpl`, `repo/issue/view_title.tmpl` | partial | `repo/issue/view.tmpl` (1 old / 7 ds), `repo/issue/view_content.tmpl` (12 old / 0 ds), `repo/issue/view_title.tmpl` (19 old / 0 ds) |
| C13 Insights: Pulse | `Insights` (`Mobile-Insights`) | `repo/pulse.tmpl`, `repo/contributors.tmpl` | partial | `repo/pulse.tmpl` (20 old / 14 ds) |
| C14 Insights: Contributors | `InsightsPeople` (`Mobile-InsightsPeople`) | `repo/pulse.tmpl`, `repo/contributors.tmpl` | partial | `repo/pulse.tmpl` (20 old / 14 ds) |
| C15 Compatibility note | `GitNote` (`Mobile-GitNote`) | `repo/clone_asc_tabs.tmpl`, `repo/clone_buttons.tmpl`, `repo/settings/asc_branchless.tmpl` | partial | `repo/clone_buttons.tmpl` (3 old / 0 ds), `repo/settings/asc_branchless.tmpl` (3 old / 0 ds) |
| D1 Stacks | `Stacks` (`Mobile-Stacks`) | `repo/issue/list.tmpl`, `repo/pulls/asc_stack.tmpl`, `repo/pulls/asc_badges.tmpl` | design system |  |
| D2 Open a change | `NewChange` (`Mobile-NewChange`) | `repo/asc_compare.tmpl` | design system |  |
| D3 Stacked review | `Review` (`Mobile-Review`) | `repo/diff/asc_class_badge.tmpl`, `repo/diff/blob_excerpt.tmpl`, `repo/diff/box.tmpl`, `repo/diff/comment_form.tmpl`, `repo/diff/comment_form_datahandler.tmpl`, `repo/diff/comments.tmpl` (+17) | partial | `repo/diff/box.tmpl` (4 old / 73 ds), `repo/diff/comment_form.tmpl` (6 old / 0 ds), `repo/diff/comments.tmpl` (5 old / 0 ds), `repo/diff/compare.tmpl` (18 old / 14 ds) (+8) |
| D4 Versions and landing | `ReviewVersions` (`Mobile-ReviewVersions`) | `repo/asc_confirm.tmpl`, `repo/asc_compare.tmpl`, `repo/asc_graph.tmpl` | partial | `repo/asc_graph.tmpl` (13 old / 0 ds) |
| D5 Merge queue | `Queue` (`Mobile-Queue`) | `repo/asc_queue.tmpl`, `repo/asc_confirm.tmpl` | design system |  |
| D6 Operation log and undo | `Ops` (`Mobile-Ops`) | `repo/asc_ops.tmpl`, `repo/asc_ops_undo.tmpl` | design system |  |
| D7 Policy gate | `Policy` (`Mobile-Policy`) | `repo/asc_queue.tmpl`, `repo/asc_confirm.tmpl`, `repo/settings/asc_policy.tmpl` | design system |  |
| D8 Checks | `Checks` (`Mobile-Checks`) | `repo/asc_checks.tmpl`, `repo/asc_check_group.tmpl`, `repo/asc_queue.tmpl` | design system |  |
| D9 Checks on a change | `ChangeChecks` (`Mobile-ChangeChecks`) | `repo/asc_checks.tmpl`, `repo/asc_check_group.tmpl` | design system |  |
| D10 Secret scanning | `Security` (`Mobile-Security`) | `repo/settings/asc_security.tmpl` | design system |  |
| D11 Audit log | `Audit` (`Mobile-Audit`) | `repo/asc_audit.tmpl`, `repo/asc_audit_table.tmpl` | design system |  |
| E1 Agent runs | `Agents` (`Mobile-Agents`) | `repo/asc_agents.tmpl` | design system |  |
| E2 Give an agent a task | `AgentNew` (`Mobile-AgentNew`) | `repo/asc_agents_new.tmpl` | design system |  |
| E3 Agent run | `AgentRun` (`Mobile-AgentRun`) | `repo/asc_agents_run.tmpl` | design system |  |
| E4 Agent access and budgets | `AgentAccess` (`Mobile-AgentAccess`) | `repo/asc_agents_tokens.tmpl`, `repo/asc_agents_policy.tmpl` | design system |  |
| E5 Workflows | `Workflows` (`Mobile-Workflows`) | `repo/asc_workflows.tmpl`, `repo/asc_workflows_runs.tmpl`, `repo/asc_workflows_runlist.tmpl` | design system |  |
| E6 Workflow definition | `WorkflowDef` (`Mobile-WorkflowDef`) | `repo/asc_workflows_def.tmpl` | design system |  |
| E7 Workflow run | `WorkflowRun` (`Mobile-WorkflowRun`) | `repo/asc_workflows_run.tmpl` | design system |  |
| F1 General | `RepoSettingsGeneral` (`Mobile-RepoSettingsGeneral`) | `repo/settings/options.tmpl`, `repo/settings/units/issues.tmpl`, `repo/settings/units/overview.tmpl`, `repo/settings/units/pulls.tmpl`, `repo/settings/units/wiki.tmpl`, `repo/settings/asc_branchless.tmpl` | old Fomantic markup | `repo/settings/options.tmpl` (82 old / 0 ds), `repo/settings/units/issues.tmpl` (16 old / 0 ds), `repo/settings/units/overview.tmpl` (9 old / 0 ds), `repo/settings/units/pulls.tmpl` (17 old / 0 ds) (+2) |
| F2 Access | `RepoSettingsAccess` (`Mobile-RepoSettingsAccess`) | `repo/settings/collaboration.tmpl`, `repo/settings/deploy_keys.tmpl` | old Fomantic markup | `repo/settings/collaboration.tmpl` (16 old / 0 ds), `repo/settings/deploy_keys.tmpl` (11 old / 0 ds) |
| F3 Policy | `RepoSettingsPolicy` (`Mobile-RepoSettingsPolicy`) | `repo/settings/asc_policy.tmpl` | design system |  |
| F4 Branches and stacks | `RepoSettingsRules` (`Mobile-RepoSettingsRules`) | `repo/settings/branches.tmpl`, `repo/settings/protected_branch.tmpl`, `repo/settings/tags.tmpl` | old Fomantic markup | `repo/settings/branches.tmpl` (13 old / 0 ds), `repo/settings/protected_branch.tmpl` (15 old / 0 ds), `repo/settings/tags.tmpl` (18 old / 0 ds) |
| F5 Webhooks | `RepoSettingsWebhooks` (`Mobile-RepoSettingsWebhooks`) | `repo/settings/webhook/list.tmpl` | design system |  |
| F6 Secrets and variables | `RepoSettingsSecrets` (`Mobile-RepoSettingsSecrets`) | `shared/secrets/add_list.tmpl`, `shared/variables/variable_list.tmpl` | old Fomantic markup | `shared/secrets/add_list.tmpl` (7 old / 0 ds), `shared/variables/variable_list.tmpl` (5 old / 0 ds) |
| F7 Storage | `RepoSettingsStorage` (`Mobile-RepoSettingsStorage`) | `repo/settings/asc_storage.tmpl` | design system |  |
| F8 AI keys | `RepoSettingsAI` (`Mobile-RepoSettingsAI`) | `repo/settings/asc_ai.tmpl` | design system |  |
| G1 Profile | `UserSettingsProfile` (`Mobile-UserSettingsProfile`) | `user/settings/profile.tmpl` | design system |  |
| G2 Account | `UserSettingsAccount` (`Mobile-UserSettingsAccount`) | `user/settings/account.tmpl`, `user/settings/repos.tmpl` | partial | `user/settings/account.tmpl` (6 old / 88 ds) |
| G3 Appearance | `UserSettingsAppearance` (`Mobile-UserSettingsAppearance`) | `user/settings/appearance.tmpl` | partial | `user/settings/appearance.tmpl` (4 old / 74 ds) |
| G4 Notifications | `UserSettingsNotifications` (`Mobile-UserSettingsNotifications`) | `user/notification/notification_subscriptions.tmpl` | partial | `user/notification/notification_subscriptions.tmpl` (3 old / 22 ds) |
| G5 Privacy | `UserSettingsPrivacy` (`Mobile-UserSettingsPrivacy`) | `user/settings/blocked_users.tmpl` | design system |  |
| G6 Security | `UserSettingsSecurity` (`Mobile-UserSettingsSecurity`) | `user/settings/security/accountlinks.tmpl`, `user/settings/security/openid.tmpl`, `user/settings/security/security.tmpl`, `user/settings/security/twofa.tmpl`, `user/settings/security/twofa_enroll.tmpl`, `user/settings/security/webauthn.tmpl` | design system |  |
| G7 Access tokens | `UserSettingsTokens` (`Mobile-UserSettingsTokens`) | `user/settings/applications.tmpl`, `user/settings/authorized_integrations.tmpl` | partial | `user/settings/authorized_integrations.tmpl` (8 old / 0 ds) |
| G8 SSH and signing keys | `UserSettingsKeys` (`Mobile-UserSettingsKeys`) | `user/settings/keys.tmpl`, `user/settings/keys_ssh.tmpl`, `user/settings/keys_gpg.tmpl`, `user/settings/asc_keys.tmpl` | partial | `user/settings/keys_ssh.tmpl` (3 old / 73 ds), `user/settings/keys_gpg.tmpl` (3 old / 82 ds) |
| G9 Applications | `UserSettingsApps` (`Mobile-UserSettingsApps`) | `user/settings/grants_oauth2.tmpl`, `user/settings/hooks.tmpl`, `user/settings/applications.tmpl`, `user/settings/applications_oauth2.tmpl`, `user/settings/applications_oauth2_edit.tmpl`, `user/settings/applications_oauth2_edit_form.tmpl` (+1) | partial | `user/settings/applications_oauth2_edit_form.tmpl` (3 old / 31 ds) |
| G10 AI keys | `UserSettingsAI` (`Mobile-UserSettingsAI`) | `user/settings/asc_ai.tmpl`, `shared/asc/byo_ai.tmpl`, `shared/asc/byo_ai_fields.tmpl`, `shared/asc/byo_ai_key.tmpl`, `shared/asc/byo_ai_page.tmpl` | partial | `shared/asc/byo_ai.tmpl` (5 old / 0 ds), `shared/asc/byo_ai_key.tmpl` (6 old / 0 ds) |
| G11 Storage | `UserSettingsStorage` (`Mobile-UserSettingsStorage`) | `user/settings/asc_storage.tmpl`, `shared/asc/byo_storage.tmpl`, `shared/asc/byo_storage_cards.tmpl`, `shared/asc/byo_storage_fields.tmpl`, `shared/asc/byo_storage_page.tmpl`, `shared/asc/byo_storage_repo.tmpl` (+1) | partial | `shared/asc/byo_storage.tmpl` (1 old / 0 ds), `shared/asc/byo_storage_cards.tmpl` (4 old / 0 ds), `shared/asc/byo_storage_repo.tmpl` (4 old / 0 ds), `shared/asc/byo_storage_target.tmpl` (6 old / 0 ds) |
| G12 Organizations | `UserSettingsOrgs` (`Mobile-UserSettingsOrgs`) | `user/settings/organization.tmpl` | design system |  |
| G13 Repositories | `UserSettingsRepos` (`Mobile-UserSettingsRepos`) | `user/settings/repos.tmpl` | design system |  |
| G14 Usage and quota | `UserSettingsUsage` (`Mobile-UserSettingsUsage`) | `user/settings/storage_overview.tmpl` | design system |  |
| H1 Dashboard | `AdminDashboard` (`Mobile-AdminDashboard`) | `admin/dashboard.tmpl` | design system |  |
| H2 Health | `AdminHealth` (`Mobile-AdminHealth`) | `admin/self_check.tmpl`, `admin/system_status.tmpl` | design system |  |
| H3 People | `AdminUsers` (`Mobile-AdminUsers`) | `admin/user/list.tmpl`, `admin/user/new.tmpl`, `admin/user/edit.tmpl` | design system |  |
| H4 One person | `AdminUser` (`Mobile-AdminUser`) | `admin/user/view.tmpl`, `admin/user/edit.tmpl` | design system |  |
| H5 Organizations | `AdminOrgs` (`Mobile-AdminOrgs`) | `admin/org/list.tmpl` | design system |  |
| H6 Moderation | `AdminModeration` (`Mobile-AdminModeration`) | `admin/moderation/report_details.tmpl`, `admin/moderation/reports.tmpl` | design system |  |
| H7 Repositories | `AdminRepos` (`Mobile-AdminRepos`) | `admin/repo/list.tmpl`, `admin/repo/unadopted.tmpl` | design system |  |
| H8 Packages | `AdminPackages` (`Mobile-AdminPackages`) | `admin/packages/list.tmpl` | design system |  |
| H9 Runners | `AdminRunners` (`Mobile-AdminRunners`) | `admin/actions.tmpl`, `admin/runners/create.tmpl`, `admin/runners/details.tmpl`, `admin/runners/edit.tmpl`, `admin/runners/setup.tmpl` | design system |  |
| H10 Hooks and applications | `AdminHooks` (`Mobile-AdminHooks`) | `admin/hooks.tmpl`, `admin/hook_new.tmpl`, `admin/applications/list.tmpl` | design system |  |
| H11 Configuration | `AdminConfig` (`Mobile-AdminConfig`) | `admin/config.tmpl`, `admin/config_server.tmpl`, `admin/config_runtime.tmpl` | design system |  |
| H12 Monitor | `AdminMonitor` (`Mobile-AdminMonitor`) | `admin/queue.tmpl`, `admin/stacktrace.tmpl`, `admin/stats.tmpl`, `admin/monitor_tabs.tmpl`, `admin/cron_result.tmpl` | design system |  |
| H13 Audit log | `AdminAudit` (`Mobile-AdminAudit`) | `admin/asc_audit.tmpl`, `org/settings/asc_audit.tmpl` | design system |  |
| H14 Backups | `AdminBackups` (`Mobile-AdminBackups`) | `admin/backups.tmpl` | design system |  |
| H15 Authentication | `AdminAuth` (`Mobile-AdminAuth`) | `admin/auth/list.tmpl`, `admin/auth/new.tmpl`, `admin/auth/edit.tmpl` | design system |  |
| H16 Notices | `AdminNotices` (`Mobile-AdminNotices`) | `admin/notice.tmpl` | design system |  |

## Routes and their pages

GET routes of `routers/web/web.go` whose handler renders a template (handlers that render through a shared function or redirect are not listed). Paths are relative to the groups they sit in. Board is the board that names the template.

| Template | Route(s) | Status | Board |
| --- | --- | --- | --- |
| `admin/auth/edit.tmpl` | /admin/auths/{authid} | design system | H15 AdminAuth |
| `admin/auth/list.tmpl` | /admin/auths | design system | H15 AdminAuth |
| `admin/auth/new.tmpl` | /admin/auths/new | design system | H15 AdminAuth |
| `admin/backups.tmpl` | /admin/asc/backups | design system | H14 AdminBackups |
| `admin/config.tmpl` | /admin/config | design system | H11 AdminConfig |
| `admin/emails/list.tmpl` | /admin/emails | design system | no board |
| `admin/federation/host.tmpl` | /admin/federation/hosts/{id} | design system | no board |
| `admin/federation/hosts.tmpl` | /admin/federation/hosts | design system | no board |
| `admin/federation/users.tmpl` | /admin/federation/users | design system | no board |
| `admin/hooks.tmpl` | /admin/hooks | no class markup | H10 AdminHooks |
| `admin/moderation/report_details.tmpl` | /admin/moderation/reports/type/{type:1|2|3|4}/id/{id} | design system | H6 AdminModeration |
| `admin/moderation/reports.tmpl` | /admin/moderation/reports | design system | H6 AdminModeration |
| `admin/notice.tmpl` | /admin/notices | design system | H16 AdminNotices |
| `admin/org/list.tmpl` | /admin/orgs | design system | H5 AdminOrgs |
| `admin/queue.tmpl` | /admin/monitor/cron<br>/admin/monitor/queue | design system | H12 AdminMonitor |
| `admin/queue_manage.tmpl` | /admin/monitor/queue/{qid} | design system | no board |
| `admin/repo/list.tmpl` | /admin/repos | design system | H7 AdminRepos |
| `admin/self_check.tmpl` | /admin/self_check | design system | H2 AdminHealth |
| `admin/stacktrace.tmpl` | /admin/monitor/stacktrace | design system | H12 AdminMonitor |
| `admin/stats.tmpl` | /admin/monitor/stats | design system | H12 AdminMonitor |
| `admin/system_status.tmpl` | /admin/system_status | design system | H2 AdminHealth |
| `admin/user/edit.tmpl` | /admin/users/{userid}/edit | design system | H3 AdminUsers, H4 AdminUser |
| `admin/user/list.tmpl` | /admin/users | design system | H3 AdminUsers |
| `admin/user/new.tmpl` | /admin/users/new | design system | H3 AdminUsers |
| `admin/user/view.tmpl` | /admin/users/{userid} | design system | H4 AdminUser |
| `explore/repos.tmpl` | /explore/repos<br>/explore/repos/sitemap-{idx}.xml | design system | A8 Explore |
| `explore/users.tmpl` | /explore/organizations<br>/explore/users<br>/explore/users/sitemap-{idx}.xml | design system | A9 ExploreUsers |
| `moderation/new_abuse_report.tmpl` | /report_abuse | old Fomantic markup (8 old / 0 ds) | no board |
| `org/create.tmpl` | /org/create | design system | B11 NewOrg |
| `org/member/members.tmpl` | /org/{org}/members | design system | B6 OrgPeople |
| `org/settings/blocked_users.tmpl` | /org/{org}/settings/blocked_users | partial (3 old / 14 ds) | no board |
| `org/settings/labels.tmpl` | /org/{org}/settings/labels | old Fomantic markup (1 old / 0 ds) | no board |
| `org/team/invite.tmpl` | /org/invite/{token} | old Fomantic markup (3 old / 0 ds) | no board |
| `org/team/members.tmpl` | /org/{org}/teams/{team} | partial (4 old / 41 ds) | B7 OrgTeams |
| `org/team/new.tmpl` | /org/{org}/teams/new<br>/org/{org}/teams/{team}/edit | design system | B7 OrgTeams |
| `org/team/repositories.tmpl` | /org/{org}/teams/{team}/repositories | partial (5 old / 33 ds) | B7 OrgTeams |
| `org/team/teams.tmpl` | /org/{org}/teams | design system | B7 OrgTeams |
| `package/settings.tmpl` | /{username}/-/packages/{type}/{name}/{version}/settings | old Fomantic markup (11 old / 0 ds) | no board |
| `package/view.tmpl` | /{username}/-/packages/{type}/{name}/{version} | partial (6 old / 16 ds) | B8 Packages |
| `repo/actions/list.tmpl` | /{username}/{reponame}/actions | old Fomantic markup (1 old / 0 ds) | no board |
| `repo/actions/list_inner.tmpl` | /{username}/{reponame}/actions | old Fomantic markup (7 old / 0 ds) | no board |
| `repo/activity.tmpl` | /{username}/{reponame}/activity<br>/{username}/{reponame}/activity/code-frequency<br>/{username}/{reponame}/activity/contributors<br>/{username}/{reponame}/activity/recent-commits<br>/{username}/{reponame}/activity/{period} | design system | no board |
| `repo/asc_compare.tmpl` | /{username}/{reponame}/asc/compare | design system | C7 Compare, D2 NewChange, D4 ReviewVersions |
| `repo/asc_confirm.tmpl` | /{username}/{reponame}/pulls/{index}/asc/confirm | design system | D3 Review, D4 ReviewVersions, D5 Queue, D7 Policy |
| `repo/asc_graph.tmpl` | /{username}/{reponame}/asc/graph | old Fomantic markup (13 old / 0 ds) | C5 History, D3 Review, D4 ReviewVersions |
| `repo/asc_queue.tmpl` | /{username}/{reponame}/asc/queue | design system | D3 Review, D5 Queue, D7 Policy, D8 Checks |
| `repo/asc_search.tmpl` | /{username}/{reponame}/asc/search | design system | C8 Search |
| `repo/asc_symbols.tmpl` | /{username}/{reponame}/asc/symbols | old Fomantic markup (7 old / 0 ds) | C3 Code, C8 Search |
| `repo/branch/list.tmpl` | /{username}/{reponame}/branches | design system | C6 Refs |
| `repo/commit_page.tmpl` | /{username}/{reponame}/commit/{sha:([a-f0-9]{4,64})$}<br>/{username}/{reponame}/wiki/commit/{sha:[a-f0-9]{4,64}} | design system | C4 Changeset |
| `repo/create.tmpl` | /repo/create | design system | C1 NewRepo |
| `repo/diff/blob_excerpt.tmpl` | /{username}/{reponame}/blob_excerpt/{sha} | no class markup | D3 Review |
| `repo/diff/box.tmpl` | /{username}/{reponame}/compare | partial (4 old / 73 ds) | D3 Review |
| `repo/diff/compare.tmpl` | /{username}/{reponame}/compare | partial (18 old / 14 ds) | D3 Review |
| `repo/diff/new_comment.tmpl` | /{username}/{reponame}/pulls/{index}/files/reviews/new_comment | no class markup | D3 Review |
| `repo/editor/cherry_pick.tmpl` | /{username}/{reponame}/_cherrypick/{sha:([a-f0-9]{4,64})}/*<br>/{username}/{reponame}/cherry-pick/{sha:([a-f0-9]{4,64})$} | old Fomantic markup (3 old / 0 ds) | no board |
| `repo/editor/delete.tmpl` | /{username}/{reponame}/_delete/* | old Fomantic markup (2 old / 0 ds) | no board |
| `repo/editor/patch.tmpl` | /{username}/{reponame}/_diffpatch/* | old Fomantic markup (6 old / 0 ds) | no board |
| `repo/find/files.tmpl` | /{username}/{reponame}/find/* | old Fomantic markup (4 old / 0 ds) | no board |
| `repo/flags.tmpl` | /{username}/{reponame}/flags | old Fomantic markup (7 old / 0 ds) | no board |
| `repo/forks.tmpl` | /{username}/{reponame}/forks | old Fomantic markup (2 old / 0 ds) | no board |
| `repo/graph.tmpl` | /{username}/{reponame}/graph | old Fomantic markup (3 old / 0 ds) | no board |
| `repo/graph/div.tmpl` | /{username}/{reponame}/graph | no class markup | no board |
| `repo/home.tmpl` | /{username}/{reponame}/blame/branch/*<br>/{username}/{reponame}/blame/commit/*<br>/{username}/{reponame}/blame/tag/* | no class markup | C2 Repo |
| `repo/issue/choose.tmpl` | /{username}/{reponame}/issues/new/choose | old Fomantic markup (11 old / 0 ds) | no board |
| `repo/issue/labels.tmpl` | /{username}/{reponame}/labels | old Fomantic markup (2 old / 0 ds) | C11 Issues |
| `repo/issue/milestone_issues.tmpl` | /{username}/{reponame}/milestone/{id} | old Fomantic markup (5 old / 0 ds) | no board |
| `repo/issue/milestone_new.tmpl` | /{username}/{reponame}/milestones/new<br>/{username}/{reponame}/milestones/{id}/edit | old Fomantic markup (8 old / 0 ds) | no board |
| `repo/issue/milestones.tmpl` | /{username}/{reponame}/milestones | old Fomantic markup (3 old / 0 ds) | C11 Issues |
| `repo/issue/new.tmpl` | /{username}/{reponame}/issues/new | old Fomantic markup (1 old / 0 ds) | C11 Issues |
| `repo/issue/view.tmpl` | /{username}/{reponame}/pulls/{index}<br>/{username}/{reponame}/{type:^(issues|pulls)$}/{index} | partial (1 old / 7 ds) | C12 IssueView |
| `repo/migrate/migrate.tmpl` | /repo/migrate | design system | C1 NewRepo |
| `repo/projects/list.tmpl` | /{username}/-/projects<br>/{username}/{reponame}/projects | old Fomantic markup (1 old / 0 ds) | no board |
| `repo/projects/new.tmpl` | /{username}/-/projects/new<br>/{username}/-/projects/{id}/edit<br>/{username}/{reponame}/projects/new<br>/{username}/{reponame}/projects/{id}/edit | old Fomantic markup (1 old / 0 ds) | no board |
| `repo/projects/view.tmpl` | /{username}/-/projects/{id}<br>/{username}/{reponame}/projects/{id} | design system | no board |
| `repo/pulls/commits.tmpl` | /{username}/{reponame}/pulls/{index}/commits | old Fomantic markup (1 old / 0 ds) | no board |
| `repo/pulls/fork.tmpl` | /{username}/{reponame}/fork | old Fomantic markup (8 old / 0 ds) | no board |
| `repo/release/list.tmpl` | /{username}/{reponame}/releases/<br>/{username}/{reponame}/releases/tag/* | design system | C9 Releases |
| `repo/release/new.tmpl` | /{username}/{reponame}/releases/edit/* | partial (10 old / 8 ds) | C9 Releases |
| `repo/search.tmpl` | /{username}/{reponame}/search<br>/{username}/{reponame}/search/branch/*<br>/{username}/{reponame}/search/tag/* | old Fomantic markup (1 old / 0 ds) | no board |
| `repo/settings/branches.tmpl` | /{username}/{reponame}/settings/branches/ | old Fomantic markup (13 old / 0 ds) | F4 RepoSettingsRules |
| `repo/settings/deploy_keys.tmpl` | /{username}/{reponame}/settings/keys | old Fomantic markup (11 old / 0 ds) | F2 RepoSettingsAccess |
| `repo/settings/githook_edit.tmpl` | /{username}/{reponame}/settings/hooks/git/{name} | old Fomantic markup (4 old / 0 ds) | no board |
| `repo/settings/githooks.tmpl` | /{username}/{reponame}/settings/hooks/git | old Fomantic markup (3 old / 0 ds) | no board |
| `repo/settings/lfs.tmpl` | /{username}/{reponame}/settings/lfs/ | old Fomantic markup (9 old / 0 ds) | no board |
| `repo/settings/lfs_file.tmpl` | /{username}/{reponame}/settings/lfs/show/{oid} | old Fomantic markup (7 old / 0 ds) | no board |
| `repo/settings/lfs_file_find.tmpl` | /{username}/{reponame}/settings/lfs/find | old Fomantic markup (2 old / 0 ds) | no board |
| `repo/settings/lfs_locks.tmpl` | /{username}/{reponame}/settings/lfs/locks/ | old Fomantic markup (7 old / 0 ds) | no board |
| `repo/settings/lfs_pointers.tmpl` | /{username}/{reponame}/settings/lfs/pointers | old Fomantic markup (9 old / 0 ds) | no board |
| `repo/settings/options.tmpl` | /org/{org}/settings<br>/{username}/{reponame}/settings | old Fomantic markup (82 old / 0 ds) | F1 RepoSettingsGeneral |
| `repo/settings/protected_branch.tmpl` | /{username}/{reponame}/settings/branches/edit | old Fomantic markup (15 old / 0 ds) | F4 RepoSettingsRules |
| `repo/settings/tags.tmpl` | /{username}/{reponame}/settings/tags<br>/{username}/{reponame}/settings/tags/{id} | old Fomantic markup (18 old / 0 ds) | F4 RepoSettingsRules |
| `repo/settings/units.tmpl` | /{username}/{reponame}/settings/units | old Fomantic markup (2 old / 0 ds) | no board |
| `repo/settings/webhook/base.tmpl` | /{username}/{reponame}/settings/hooks | no class markup | no board |
| `repo/tag/list.tmpl` | /{username}/{reponame}/tags | design system | C6 Refs, C9 Releases |
| `repo/view_list.tmpl` | /{username}/{reponame}/lastcommit/* | design system | no board |
| `repo/watchers.tmpl` | /{username}/{reponame}/stars<br>/{username}/{reponame}/watchers | old Fomantic markup (1 old / 0 ds) | no board |
| `repo/wiki/search.tmpl` | /{username}/{reponame}/wiki/search | no class markup | no board |
| `status/413.tmpl` | /-/demo/error/{errcode} | design system | A13 Errors |
| `user/asc_drafts.tmpl` | /-/asc/drafts | design system | B3 Drafts |
| `user/asc_inbox.tmpl` | /-/asc/inbox | design system | B2 Inbox |
| `user/auth/activate.tmpl` | /user/activate | design system | A4 Onboarding |
| `user/auth/change_passwd.tmpl` | /user/settings/change_password | no class markup | no board |
| `user/auth/forgot_passwd.tmpl` | /user/forgot_password | design system | A4 Onboarding |
| `user/auth/grant.tmpl` | /login/oauth/authorize | design system | A4 Onboarding |
| `user/auth/link_account.tmpl` | /user/link_account | partial (2 old / 11 ds) | no board |
| `user/auth/prohibit_login.tmpl` | /user/oauth2/{provider}/callback | design system | no board |
| `user/auth/reset_passwd.tmpl` | /user/recover_account | design system | A4 Onboarding |
| `user/auth/signin.tmpl` | /user/login | no class markup | A4 Onboarding |
| `user/auth/signup.tmpl` | /user/sign_up | no class markup | A4 Onboarding |
| `user/auth/twofa.tmpl` | /user/two_factor | design system | A4 Onboarding |
| `user/auth/twofa_scratch.tmpl` | /user/two_factor/scratch | design system | no board |
| `user/auth/webauthn.tmpl` | /user/webauthn | design system | A4 Onboarding |
| `user/code.tmpl` | /{username}/-/code | partial (1 old / 2 ds) | no board |
| `user/dashboard/dashboard.tmpl` | /admin<br>/org/{org}/dashboard<br>/org/{org}/dashboard/{team} | old Fomantic markup (1 old / 0 ds) | no board |
| `user/dashboard/issues.tmpl` | /{username}/{reponame}/{type:^(issues|pulls)$} | partial (14 old / 14 ds) | no board |
| `user/dashboard/milestones.tmpl` | /milestones<br>/org/{org}/milestones<br>/org/{org}/milestones/{team} | old Fomantic markup (7 old / 0 ds) | no board |
| `user/notification/notification.tmpl` | /notifications | no class markup | B2 Inbox |
| `user/notification/notification_div.tmpl` | /notifications | old Fomantic markup (2 old / 0 ds) | no board |
| `user/notification/notification_subscriptions.tmpl` | /notifications/subscriptions<br>/notifications/watching | partial (3 old / 22 ds) | G4 UserSettingsNotifications |
| `user/overview/package_versions.tmpl` | /{username}/-/packages/{type}/{name}/versions | partial (1 old / 2 ds) | no board |
| `user/overview/packages.tmpl` | /admin/packages<br>/{username}/-/packages<br>/{username}/{reponame}/packages | partial (1 old / 2 ds) | no board |
| `user/settings/account.tmpl` | /user/settings/account | partial (6 old / 88 ds) | G2 UserSettingsAccount |
| `user/settings/appearance.tmpl` | /user/settings/appearance | partial (4 old / 74 ds) | G3 UserSettingsAppearance |
| `user/settings/applications.tmpl` | /admin/applications<br>/org/{org}/settings/applications<br>/user/settings/applications | design system | G7 UserSettingsTokens, G9 UserSettingsApps |
| `user/settings/authorized_integrations.tmpl` | /user/settings/authorized-integrations | old Fomantic markup (8 old / 0 ds) | G7 UserSettingsTokens |
| `user/settings/blocked_users.tmpl` | /user/settings/blocked_users | design system | G5 UserSettingsPrivacy |
| `user/settings/hooks.tmpl` | /org/{org}/settings/hooks<br>/user/settings/hooks | no class markup | G9 UserSettingsApps |
| `user/settings/keys.tmpl` | /user/settings/keys | design system | G8 UserSettingsKeys |
| `user/settings/organization.tmpl` | /user/settings/organization | design system | G12 UserSettingsOrgs |
| `user/settings/packages.tmpl` | /org/{org}/settings/packages<br>/user/settings/packages | old Fomantic markup (4 old / 0 ds) | no board |
| `user/settings/packages_cleanup_rules_edit.tmpl` | /org/{org}/settings/packages/rules/add<br>/org/{org}/settings/packages/rules/{id}<br>/user/settings/packages/rules/add<br>/user/settings/packages/rules/{id} | no class markup | no board |
| `user/settings/packages_cleanup_rules_preview.tmpl` | /org/{org}/settings/packages/rules/{id}/preview<br>/user/settings/packages/rules/{id}/preview | no class markup | no board |
| `user/settings/profile.tmpl` | /user/settings | design system | G1 UserSettingsProfile |
| `user/settings/repos.tmpl` | /user/settings/repos | design system | G2 UserSettingsAccount, G13 UserSettingsRepos |
| `user/settings/security/security.tmpl` | /user/settings/security | design system | G6 UserSettingsSecurity |
| `user/settings/security/twofa_enroll.tmpl` | /user/settings/security/two_factor/enroll<br>/user/settings/security/two_factor/reenroll | design system | G6 UserSettingsSecurity |
| `user/settings/storage_overview.tmpl` | /org/{org}/settings/storage_overview<br>/user/settings/storage_overview | design system | G14 UserSettingsUsage |

## Templates still in old markup, by area

Every template with Fomantic classes, not reached by a board above or reached with old markup. `old` counts class lists that hold the word `ui`; `ds` counts design-system classes.


### `base`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `base/alert.tmpl` | old | 4 old / 0 ds | - |
| `base/modal_actions_confirm.tmpl` | old | 3 old / 0 ds | - |

### `custom`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `custom/signin_mid.tmpl` | old | 1 old / 0 ds | - |
| `custom/signup_top.tmpl` | old | 1 old / 0 ds | - |

### `explore`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `explore/asc_search.tmpl` | old | 10 old / 0 ds | - |
| `explore/code.tmpl` | old | 1 old / 0 ds | - |

### `moderation`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `moderation/new_abuse_report.tmpl` | old | 8 old / 0 ds | - |

### `org/member`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `org/member/add_org_member.tmpl` | partial | 4 old / 15 ds | B6 OrgPeople |

### `org/projects`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `org/projects/list.tmpl` | partial | 1 old / 2 ds | - |
| `org/projects/new.tmpl` | old | 1 old / 0 ds | - |
| `org/projects/view.tmpl` | partial | 1 old / 1 ds | - |

### `org/settings`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `org/settings/blocked_users.tmpl` | partial | 3 old / 14 ds | - |
| `org/settings/delete.tmpl` | old | 6 old / 0 ds | - |
| `org/settings/labels.tmpl` | old | 1 old / 0 ds | - |

### `org/team`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `org/team/invite.tmpl` | old | 3 old / 0 ds | - |
| `org/team/members.tmpl` | partial | 4 old / 41 ds | B7 OrgTeams |
| `org/team/repositories.tmpl` | partial | 5 old / 33 ds | B7 OrgTeams |

### `package`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `package/settings.tmpl` | old | 11 old / 0 ds | - |
| `package/view.tmpl` | partial | 6 old / 16 ds | B8 Packages |

### `package/content`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `package/content/alpine.tmpl` | old | 8 old / 0 ds | B8 Packages |
| `package/content/alt.tmpl` | old | 9 old / 0 ds | B8 Packages |
| `package/content/arch.tmpl` | old | 6 old / 0 ds | B8 Packages |
| `package/content/cargo.tmpl` | old | 11 old / 0 ds | B8 Packages |
| `package/content/chef.tmpl` | old | 8 old / 0 ds | B8 Packages |
| `package/content/composer.tmpl` | old | 12 old / 0 ds | B8 Packages |
| `package/content/composer_dependencies.tmpl` | old | 1 old / 0 ds | B8 Packages |
| `package/content/conan.tmpl` | old | 7 old / 0 ds | B8 Packages |
| `package/content/conda.tmpl` | old | 5 old / 0 ds | B8 Packages |
| `package/content/container.tmpl` | old | 14 old / 0 ds | B8 Packages |
| `package/content/cran.tmpl` | old | 8 old / 0 ds | B8 Packages |
| `package/content/debian.tmpl` | old | 11 old / 0 ds | B8 Packages |
| `package/content/generic.tmpl` | old | 3 old / 0 ds | B8 Packages |
| `package/content/go.tmpl` | old | 3 old / 0 ds | B8 Packages |
| `package/content/helm.tmpl` | old | 10 old / 0 ds | B8 Packages |
| `package/content/maven.tmpl` | old | 8 old / 0 ds | B8 Packages |
| `package/content/npm.tmpl` | old | 12 old / 0 ds | B8 Packages |
| `package/content/npm_dependencies.tmpl` | old | 1 old / 0 ds | B8 Packages |
| `package/content/nuget.tmpl` | old | 10 old / 0 ds | B8 Packages |
| `package/content/pub.tmpl` | old | 6 old / 0 ds | B8 Packages |
| `package/content/pypi.tmpl` | old | 7 old / 0 ds | B8 Packages |
| `package/content/rpm.tmpl` | old | 9 old / 0 ds | B8 Packages |
| `package/content/rubygems.tmpl` | old | 10 old / 0 ds | B8 Packages |
| `package/content/rubygems_dependencies.tmpl` | old | 1 old / 0 ds | B8 Packages |
| `package/content/swift.tmpl` | old | 5 old / 0 ds | B8 Packages |
| `package/content/vagrant.tmpl` | old | 5 old / 0 ds | B8 Packages |

### `package/shared`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `package/shared/cargo.tmpl` | old | 5 old / 0 ds | - |
| `package/shared/versionlist.tmpl` | old | 4 old / 0 ds | - |

### `package/shared/cleanup_rules`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `package/shared/cleanup_rules/edit.tmpl` | old | 12 old / 0 ds | - |
| `package/shared/cleanup_rules/list.tmpl` | old | 5 old / 0 ds | - |
| `package/shared/cleanup_rules/preview.tmpl` | old | 4 old / 0 ds | - |

### `projects`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `projects/list.tmpl` | partial | 4 old / 39 ds | B9 Projects |
| `projects/new.tmpl` | old | 6 old / 0 ds | B9 Projects |
| `projects/view.tmpl` | partial | 11 old / 19 ds | B9 Projects |

### `repo`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/admin_flags.tmpl` | old | 2 old / 0 ds | - |
| `repo/asc_about.tmpl` | partial | 3 old / 38 ds | C2 Repo |
| `repo/asc_graph.tmpl` | old | 13 old / 0 ds | C5 History, D3 Review, D4 ReviewVersions |
| `repo/asc_symbols.tmpl` | old | 7 old / 0 ds | C3 Code, C8 Search |
| `repo/branch_dropdown.tmpl` | old | 2 old / 0 ds | - |
| `repo/clone_buttons.tmpl` | old | 3 old / 0 ds | C15 GitNote |
| `repo/commit_header.tmpl` | partial | 13 old / 85 ds | - |
| `repo/commit_load_branches_and_tags.tmpl` | old | 1 old / 0 ds | - |
| `repo/commits_list_small.tmpl` | old | 1 old / 0 ds | - |
| `repo/empty.tmpl` | old | 10 old / 0 ds | - |
| `repo/flags.tmpl` | old | 7 old / 0 ds | - |
| `repo/forks.tmpl` | old | 2 old / 0 ds | - |
| `repo/graph.tmpl` | old | 3 old / 0 ds | - |
| `repo/icon.tmpl` | old | 1 old / 0 ds | - |
| `repo/packages.tmpl` | old | 2 old / 0 ds | - |
| `repo/pulse.tmpl` | partial | 20 old / 14 ds | C13 Insights, C14 InsightsPeople |
| `repo/search.tmpl` | old | 1 old / 0 ds | - |
| `repo/sub_menu.tmpl` | old | 2 old / 0 ds | - |
| `repo/unicode_escape_prompt.tmpl` | old | 2 old / 0 ds | - |
| `repo/upload.tmpl` | old | 1 old / 0 ds | - |
| `repo/watchers.tmpl` | old | 1 old / 0 ds | - |

### `repo/actions`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/actions/dispatch.tmpl` | old | 7 old / 0 ds | - |
| `repo/actions/list.tmpl` | old | 1 old / 0 ds | - |
| `repo/actions/list_inner.tmpl` | old | 7 old / 0 ds | - |
| `repo/actions/runs_list.tmpl` | old | 3 old / 0 ds | - |

### `repo/diff`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/diff/box.tmpl` | partial | 4 old / 73 ds | D3 Review |
| `repo/diff/comment_form.tmpl` | old | 6 old / 0 ds | D3 Review |
| `repo/diff/comments.tmpl` | old | 5 old / 0 ds | D3 Review |
| `repo/diff/compare.tmpl` | partial | 18 old / 14 ds | D3 Review |
| `repo/diff/conversation.tmpl` | old | 9 old / 0 ds | D3 Review |
| `repo/diff/csv_diff.tmpl` | old | 1 old / 0 ds | D3 Review |
| `repo/diff/image_diff.tmpl` | old | 4 old / 0 ds | D3 Review |
| `repo/diff/options_dropdown.tmpl` | partial | 1 old / 4 ds | D3 Review |
| `repo/diff/section_split.tmpl` | old | 4 old / 0 ds | D3 Review |
| `repo/diff/section_unified.tmpl` | old | 1 old / 0 ds | D3 Review |
| `repo/diff/whitespace_dropdown.tmpl` | partial | 1 old / 7 ds | D3 Review |

### `repo/editor`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/editor/cherry_pick.tmpl` | old | 3 old / 0 ds | - |
| `repo/editor/commit_form.tmpl` | old | 7 old / 0 ds | - |
| `repo/editor/delete.tmpl` | old | 2 old / 0 ds | - |
| `repo/editor/diff_preview.tmpl` | old | 1 old / 0 ds | - |
| `repo/editor/edit.tmpl` | old | 8 old / 0 ds | - |
| `repo/editor/patch.tmpl` | old | 6 old / 0 ds | - |
| `repo/editor/upload.tmpl` | old | 3 old / 0 ds | - |

### `repo/find`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/find/files.tmpl` | old | 4 old / 0 ds | - |

### `repo/graph`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/graph/commits.tmpl` | old | 3 old / 0 ds | - |

### `repo/issue`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/issue/choose.tmpl` | old | 11 old / 0 ds | - |
| `repo/issue/filter_actions.tmpl` | old | 7 old / 0 ds | - |
| `repo/issue/filter_list.tmpl` | old | 9 old / 0 ds | - |
| `repo/issue/filters.tmpl` | partial | 1 old / 2 ds | C11 Issues |
| `repo/issue/labels.tmpl` | old | 2 old / 0 ds | C11 Issues |
| `repo/issue/milestone_issues.tmpl` | old | 5 old / 0 ds | - |
| `repo/issue/milestone_new.tmpl` | old | 8 old / 0 ds | - |
| `repo/issue/milestones.tmpl` | old | 3 old / 0 ds | C11 Issues |
| `repo/issue/new.tmpl` | old | 1 old / 0 ds | C11 Issues |
| `repo/issue/new_form.tmpl` | old | 11 old / 0 ds | - |
| `repo/issue/search.tmpl` | partial | 2 old / 1 ds | C11 Issues |
| `repo/issue/view.tmpl` | partial | 1 old / 7 ds | C12 IssueView |
| `repo/issue/view_content.tmpl` | old | 12 old / 0 ds | C12 IssueView |
| `repo/issue/view_title.tmpl` | old | 19 old / 0 ds | C12 IssueView |

### `repo/issue/fields`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/issue/fields/checkboxes.tmpl` | old | 1 old / 0 ds | - |
| `repo/issue/fields/dropdown.tmpl` | old | 1 old / 0 ds | - |

### `repo/issue/labels`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/issue/labels/edit_delete_label.tmpl` | old | 8 old / 0 ds | - |
| `repo/issue/labels/label_archived.tmpl` | old | 1 old / 0 ds | - |
| `repo/issue/labels/label_list.tmpl` | old | 6 old / 0 ds | - |
| `repo/issue/labels/label_load_template.tmpl` | old | 5 old / 0 ds | - |
| `repo/issue/labels/label_new.tmpl` | old | 6 old / 0 ds | - |
| `repo/issue/labels/labels_selector_field.tmpl` | old | 2 old / 0 ds | - |
| `repo/issue/labels/labels_sidebar.tmpl` | old | 1 old / 0 ds | - |

### `repo/issue/milestone`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/issue/milestone/filter_list.tmpl` | old | 1 old / 0 ds | - |
| `repo/issue/milestone/select_menu.tmpl` | old | 1 old / 0 ds | - |
| `repo/issue/milestone/selected.tmpl` | old | 1 old / 0 ds | - |

### `repo/issue/view_content`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/issue/view_content/add_reaction.tmpl` | old | 1 old / 0 ds | - |
| `repo/issue/view_content/attachments.tmpl` | old | 2 old / 0 ds | - |
| `repo/issue/view_content/comments.tmpl` | old | 10 old / 0 ds | - |
| `repo/issue/view_content/comments_delete_time.tmpl` | old | 2 old / 0 ds | - |
| `repo/issue/view_content/context_menu.tmpl` | old | 1 old / 0 ds | - |
| `repo/issue/view_content/conversation.tmpl` | old | 9 old / 0 ds | - |
| `repo/issue/view_content/pull.tmpl` | old | 7 old / 0 ds | - |
| `repo/issue/view_content/pull_merge_instruction.tmpl` | old | 4 old / 0 ds | - |
| `repo/issue/view_content/reactions.tmpl` | old | 2 old / 0 ds | - |
| `repo/issue/view_content/reference_issue_dialog.tmpl` | old | 4 old / 0 ds | - |
| `repo/issue/view_content/show_role.tmpl` | old | 2 old / 0 ds | - |
| `repo/issue/view_content/sidebar.tmpl` | old | 1 old / 0 ds | - |
| `repo/issue/view_content/update_branch_by_merge.tmpl` | old | 6 old / 0 ds | - |

### `repo/issue/view_content/sidebar`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/issue/view_content/sidebar/actions.tmpl` | old | 8 old / 0 ds | - |
| `repo/issue/view_content/sidebar/assignees.tmpl` | old | 3 old / 0 ds | - |
| `repo/issue/view_content/sidebar/branch_selector_field.tmpl` | old | 3 old / 0 ds | - |
| `repo/issue/view_content/sidebar/dependencies.tmpl` | old | 7 old / 0 ds | - |
| `repo/issue/view_content/sidebar/due_deadline.tmpl` | old | 4 old / 0 ds | - |
| `repo/issue/view_content/sidebar/milestones.tmpl` | old | 1 old / 0 ds | - |
| `repo/issue/view_content/sidebar/participants.tmpl` | old | 1 old / 0 ds | - |
| `repo/issue/view_content/sidebar/projects.tmpl` | old | 3 old / 0 ds | - |
| `repo/issue/view_content/sidebar/pull_maintainer_edits.tmpl` | old | 1 old / 0 ds | - |
| `repo/issue/view_content/sidebar/pull_review.tmpl` | old | 2 old / 0 ds | - |
| `repo/issue/view_content/sidebar/pull_reviewers.tmpl` | old | 7 old / 0 ds | - |
| `repo/issue/view_content/sidebar/reference.tmpl` | old | 3 old / 0 ds | - |
| `repo/issue/view_content/sidebar/timetracking.tmpl` | old | 12 old / 0 ds | - |
| `repo/issue/view_content/sidebar/watch.tmpl` | old | 1 old / 0 ds | - |
| `repo/issue/view_content/sidebar/watching.tmpl` | old | 1 old / 0 ds | - |

### `repo/migrate`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/migrate/codebase.tmpl` | partial | 10 old / 7 ds | C1 NewRepo |
| `repo/migrate/git.tmpl` | partial | 6 old / 7 ds | C1 NewRepo |
| `repo/migrate/gitbucket.tmpl` | partial | 12 old / 7 ds | C1 NewRepo |
| `repo/migrate/gitea.tmpl` | partial | 12 old / 7 ds | C1 NewRepo |
| `repo/migrate/github.tmpl` | partial | 12 old / 7 ds | C1 NewRepo |
| `repo/migrate/gitlab.tmpl` | partial | 12 old / 7 ds | C1 NewRepo |
| `repo/migrate/gogs.tmpl` | partial | 12 old / 7 ds | C1 NewRepo |
| `repo/migrate/migrating.tmpl` | old | 11 old / 0 ds | C1 NewRepo |
| `repo/migrate/onedev.tmpl` | partial | 10 old / 7 ds | C1 NewRepo |
| `repo/migrate/options.tmpl` | old | 2 old / 0 ds | C1 NewRepo |
| `repo/migrate/pagure.tmpl` | partial | 13 old / 7 ds | C1 NewRepo |

### `repo/projects`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/projects/list.tmpl` | old | 1 old / 0 ds | - |
| `repo/projects/new.tmpl` | old | 1 old / 0 ds | - |

### `repo/pulls`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/pulls/commits.tmpl` | old | 1 old / 0 ds | - |
| `repo/pulls/commits_list.tmpl` | old | 1 old / 0 ds | - |
| `repo/pulls/fork.tmpl` | old | 8 old / 0 ds | - |
| `repo/pulls/status.tmpl` | old | 5 old / 0 ds | - |

### `repo/release`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/release/new.tmpl` | partial | 10 old / 8 ds | C9 Releases |

### `repo/settings`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/settings/asc_branchless.tmpl` | old | 3 old / 0 ds | C15 GitNote, F1 RepoSettingsGeneral |
| `repo/settings/branches.tmpl` | old | 13 old / 0 ds | F4 RepoSettingsRules |
| `repo/settings/collaboration.tmpl` | old | 16 old / 0 ds | F2 RepoSettingsAccess |
| `repo/settings/deploy_keys.tmpl` | old | 11 old / 0 ds | F2 RepoSettingsAccess |
| `repo/settings/githook_edit.tmpl` | old | 4 old / 0 ds | - |
| `repo/settings/githooks.tmpl` | old | 3 old / 0 ds | - |
| `repo/settings/lfs.tmpl` | old | 9 old / 0 ds | - |
| `repo/settings/lfs_file.tmpl` | old | 7 old / 0 ds | - |
| `repo/settings/lfs_file_find.tmpl` | old | 2 old / 0 ds | - |
| `repo/settings/lfs_locks.tmpl` | old | 7 old / 0 ds | - |
| `repo/settings/lfs_pointers.tmpl` | old | 9 old / 0 ds | - |
| `repo/settings/options.tmpl` | old | 82 old / 0 ds | F1 RepoSettingsGeneral |
| `repo/settings/protected_branch.tmpl` | old | 15 old / 0 ds | F4 RepoSettingsRules |
| `repo/settings/push_mirror_sync_modal.tmpl` | old | 6 old / 0 ds | - |
| `repo/settings/tags.tmpl` | old | 18 old / 0 ds | F4 RepoSettingsRules |
| `repo/settings/units.tmpl` | old | 2 old / 0 ds | - |

### `repo/settings/units`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/settings/units/issues.tmpl` | old | 16 old / 0 ds | F1 RepoSettingsGeneral |
| `repo/settings/units/overview.tmpl` | old | 9 old / 0 ds | F1 RepoSettingsGeneral |
| `repo/settings/units/pulls.tmpl` | old | 17 old / 0 ds | F1 RepoSettingsGeneral |
| `repo/settings/units/wiki.tmpl` | old | 7 old / 0 ds | F1 RepoSettingsGeneral |

### `repo/settings/webhook`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/settings/webhook/base_list.tmpl` | old | 6 old / 0 ds | - |
| `repo/settings/webhook/delete_modal.tmpl` | old | 1 old / 0 ds | - |
| `repo/settings/webhook/history.tmpl` | old | 12 old / 0 ds | - |

### `repo/tag`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/tag/verification_line.tmpl` | old | 12 old / 0 ds | - |

### `repo/wiki`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `repo/wiki/new.tmpl` | old | 6 old / 0 ds | - |
| `repo/wiki/pages.tmpl` | old | 4 old / 0 ds | - |
| `repo/wiki/revision.tmpl` | old | 11 old / 0 ds | - |
| `repo/wiki/start.tmpl` | old | 3 old / 0 ds | - |
| `repo/wiki/view.tmpl` | old | 19 old / 0 ds | - |

### `shared`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `shared/combomarkdowneditor.tmpl` | old | 8 old / 0 ds | - |
| `shared/label_filter.tmpl` | old | 3 old / 0 ds | - |
| `shared/searchbottom.tmpl` | old | 2 old / 0 ds | - |

### `shared/actions`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `shared/actions/packages_write.tmpl` | old | 6 old / 0 ds | - |
| `shared/actions/runner_create.tmpl` | old | 2 old / 0 ds | - |
| `shared/actions/runner_details.tmpl` | old | 9 old / 0 ds | - |
| `shared/actions/runner_edit.tmpl` | old | 2 old / 0 ds | - |
| `shared/actions/runner_list.tmpl` | old | 13 old / 0 ds | - |
| `shared/actions/runner_setup.tmpl` | old | 6 old / 0 ds | - |

### `shared/asc`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `shared/asc/byo_ai.tmpl` | old | 5 old / 0 ds | G10 UserSettingsAI |
| `shared/asc/byo_ai_key.tmpl` | old | 6 old / 0 ds | G10 UserSettingsAI |
| `shared/asc/byo_head.tmpl` | old | 3 old / 0 ds | - |
| `shared/asc/byo_storage.tmpl` | old | 1 old / 0 ds | G11 UserSettingsStorage |
| `shared/asc/byo_storage_cards.tmpl` | old | 4 old / 0 ds | G11 UserSettingsStorage |
| `shared/asc/byo_storage_repo.tmpl` | old | 4 old / 0 ds | G11 UserSettingsStorage |
| `shared/asc/byo_storage_target.tmpl` | old | 6 old / 0 ds | G11 UserSettingsStorage |
| `shared/asc/byo_usage_table.tmpl` | old | 1 old / 0 ds | - |

### `shared/search`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `shared/search/button.tmpl` | old | 1 old / 0 ds | - |
| `shared/search/combo.tmpl` | old | 1 old / 0 ds | - |
| `shared/search/combo_multi.tmpl` | old | 2 old / 0 ds | - |

### `shared/search/code`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `shared/search/code/results.tmpl` | old | 5 old / 0 ds | - |
| `shared/search/code/search.tmpl` | old | 3 old / 0 ds | - |

### `shared/search/issue`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `shared/search/issue/syntax.tmpl` | old | 1 old / 0 ds | - |

### `shared/secrets`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `shared/secrets/add_list.tmpl` | old | 7 old / 0 ds | F6 RepoSettingsSecrets |

### `shared/user`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `shared/user/authorlink.tmpl` | old | 1 old / 0 ds | - |
| `shared/user/org_profile_avatar.tmpl` | old | 5 old / 0 ds | - |

### `shared/variables`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `shared/variables/variable_list.tmpl` | old | 5 old / 0 ds | F6 RepoSettingsSecrets |

### `user`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `user/asc_agents.tmpl` | old | 4 old / 0 ds | B4 Profile |
| `user/code.tmpl` | partial | 1 old / 2 ds | - |

### `user/auth`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `user/auth/link_account.tmpl` | partial | 2 old / 11 ds | - |

### `user/dashboard`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `user/dashboard/dashboard.tmpl` | old | 1 old / 0 ds | - |
| `user/dashboard/issues.tmpl` | partial | 14 old / 14 ds | - |
| `user/dashboard/milestones.tmpl` | old | 7 old / 0 ds | - |
| `user/dashboard/navbar.tmpl` | old | 12 old / 0 ds | - |

### `user/notification`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `user/notification/notification_div.tmpl` | old | 2 old / 0 ds | - |
| `user/notification/notification_subscriptions.tmpl` | partial | 3 old / 22 ds | G4 UserSettingsNotifications |

### `user/overview`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `user/overview/package_versions.tmpl` | partial | 1 old / 2 ds | - |
| `user/overview/packages.tmpl` | partial | 1 old / 2 ds | - |

### `user/settings`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `user/settings/account.tmpl` | partial | 6 old / 88 ds | G2 UserSettingsAccount |
| `user/settings/appearance.tmpl` | partial | 4 old / 74 ds | G3 UserSettingsAppearance |
| `user/settings/applications_oauth2_edit_form.tmpl` | partial | 3 old / 31 ds | G9 UserSettingsApps |
| `user/settings/authorized_integrations.tmpl` | old | 8 old / 0 ds | G7 UserSettingsTokens |
| `user/settings/keys_gpg.tmpl` | partial | 3 old / 82 ds | G8 UserSettingsKeys |
| `user/settings/keys_ssh.tmpl` | partial | 3 old / 73 ds | G8 UserSettingsKeys |
| `user/settings/packages.tmpl` | old | 4 old / 0 ds | - |

### `user/settings/authorized_integrations`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `user/settings/authorized_integrations/view_footer.tmpl` | old | 14 old / 0 ds | - |
| `user/settings/authorized_integrations/view_head.tmpl` | old | 5 old / 0 ds | - |

### `user/settings/authorized_integrations/actions_local`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `user/settings/authorized_integrations/actions_local/view.tmpl` | old | 9 old / 0 ds | - |

### `user/settings/authorized_integrations/generic`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `user/settings/authorized_integrations/generic/view.tmpl` | old | 2 old / 0 ds | - |

### `webhook`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `webhook/new.tmpl` | old | 3 old / 0 ds | - |
| `webhook/shared-settings.tmpl` | old | 3 old / 0 ds | - |

### `webhook/new`

| Template | State | old / ds | Board |
| --- | --- | --- | --- |
| `webhook/new/dingtalk.tmpl` | old | 1 old / 0 ds | - |
| `webhook/new/discord.tmpl` | old | 1 old / 0 ds | - |
| `webhook/new/feishu.tmpl` | old | 1 old / 0 ds | - |
| `webhook/new/forgejo.tmpl` | old | 3 old / 0 ds | - |
| `webhook/new/gitea.tmpl` | old | 3 old / 0 ds | - |
| `webhook/new/gogs.tmpl` | old | 2 old / 0 ds | - |
| `webhook/new/matrix.tmpl` | old | 2 old / 0 ds | - |
| `webhook/new/msteams.tmpl` | old | 1 old / 0 ds | - |
| `webhook/new/packagist.tmpl` | old | 1 old / 0 ds | - |
| `webhook/new/slack.tmpl` | old | 1 old / 0 ds | - |
| `webhook/new/sourcehut_builds.tmpl` | old | 3 old / 0 ds | - |
| `webhook/new/telegram.tmpl` | old | 1 old / 0 ds | - |
| `webhook/new/wechatwork.tmpl` | old | 1 old / 0 ds | - |
