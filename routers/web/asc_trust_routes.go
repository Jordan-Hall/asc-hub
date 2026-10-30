// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"forgejo.org/modules/web"
	"forgejo.org/routers/web/admin"
	org_setting "forgejo.org/routers/web/org/setting"
	"forgejo.org/routers/web/repo"
	repo_setting "forgejo.org/routers/web/repo/setting"
	user_setting "forgejo.org/routers/web/user/setting"
	"forgejo.org/services/context"
)

// The trust pages (PLAN unit H3): policy, secret scanning, audit log, signing keys and checks. Each function is
// called from one place in registerRoutes, inside the group whose middleware it relies on. Every handler repeats
// the check of its level (repository administrator, organisation owner, site administrator, writer, reader) and
// fails closed; the middleware is the first check, not the only one.

// registerAscTrustUserRoutes registers, inside the "/user/settings" group:
//
//	GET  /user/settings/asc/keys          the signing keys of the signed-in person
//	POST /user/settings/asc/keys          add a key
//	POST /user/settings/asc/keys/revoke   revoke a key
//
// and the account pages of asc serve (see routers/web/user/setting/asc_account.go): sessions and the data export.
func registerAscTrustUserRoutes(m *web.Route) {
	m.Group("/asc/sessions", func() {
		m.Get("", user_setting.AscSessions)
		m.Post("/revoke", user_setting.AscSessionsRevoke)
		m.Post("/revoke-all", user_setting.AscSessionsRevokeAll)
	})
	m.Group("/asc/export", func() {
		m.Combo("").Get(user_setting.AscExport).Post(user_setting.AscExportRequest)
		m.Get("/{id}", user_setting.AscExportDownload)
		m.Post("/{id}/delete", user_setting.AscExportDelete)
	})
	m.Group("/asc/keys", func() {
		m.Combo("").Get(user_setting.AscKeys).Post(user_setting.AscKeysAdd)
		m.Post("/revoke", user_setting.AscKeysRevoke)
	})
}

// registerAscTrustOrgRoutes registers, inside the "/org/{org}/settings" group (owners only):
//
//	GET  /org/{org}/settings/asc/audit          the audit log of the organisation
//	GET  /org/{org}/settings/asc/audit/export   its export
//	POST /org/{org}/settings/asc/audit/verify   the check of its hash chain
func registerAscTrustOrgRoutes(m *web.Route) {
	m.Group("/asc/audit", func() {
		m.Get("", org_setting.AscAudit)
		m.Get("/export", org_setting.AscAuditExport)
		m.Post("/verify", org_setting.AscAuditVerify)
	})
}

// registerAscTrustAdminRoutes registers, inside the "/admin" group (site administrators only):
//
//	GET  /admin/asc/audit          the audit log of the server
//	GET  /admin/asc/audit/export   its export
//	POST /admin/asc/audit/verify   the check of its hash chain
func registerAscTrustAdminRoutes(m *web.Route) {
	m.Group("/asc/audit", func() {
		m.Get("", admin.AscAudit)
		m.Get("/export", admin.AscAuditExport)
		m.Post("/verify", admin.AscAuditVerify)
	})
}

// registerAscTrustRepoSettingsRoutes registers, inside the "/{username}/{reponame}/settings" group (repository
// administrators only):
//
//	GET  .../settings/asc/policy                 the policy file, what it means, who can write
//	POST .../settings/asc/policy/check           validate a draft
//	POST .../settings/asc/policy/propose         propose a draft as a change
//	POST .../settings/asc/policy/protection      propose a switch of a protection
//	GET  .../settings/asc/security               secret scanning: findings, rules, allowlist
//	POST .../settings/asc/security/finding       set the state of a finding
//	POST .../settings/asc/security/scan          scan the default branch now
//	POST .../settings/asc/security/allowlist     propose a new allowlist
//	GET  .../settings/asc/audit                  the audit log of the repository
//	GET  .../settings/asc/audit/export           its export
//	POST .../settings/asc/audit/verify           the check of its hash chain
func registerAscTrustRepoSettingsRoutes(m *web.Route) {
	m.Group("/asc", func() {
		m.Group("/policy", func() {
			m.Get("", repo_setting.AscPolicy)
			m.Post("/check", context.RepoMustNotBeArchived(), repo_setting.AscPolicyCheck)
			m.Post("/propose", context.RepoMustNotBeArchived(), repo_setting.AscPolicyPropose)
			m.Post("/protection", context.RepoMustNotBeArchived(), repo_setting.AscPolicyProtection)
		})
		m.Group("/security", func() {
			m.Get("", repo_setting.AscSecurity)
			m.Post("/finding", context.RepoMustNotBeArchived(), repo_setting.AscSecurityFinding)
			m.Post("/scan", context.RepoMustNotBeArchived(), repo_setting.AscSecurityScan)
			m.Post("/allowlist", context.RepoMustNotBeArchived(), repo_setting.AscSecurityAllowlist)
		})
		m.Group("/audit", func() {
			m.Get("", repo.AscAudit)
			m.Get("/export", repo.AscAuditExport)
			m.Post("/verify", repo.AscAuditVerify)
		})
	})
}

// registerAscTrustRepoRoutes registers, inside the group of the code pages of a repository (readers):
//
//	GET  .../asc/checks                       failure groups and flaky tests
//	GET  .../asc/checks/groups/{id}           one group
//	POST .../asc/checks/groups/{id}/state     acknowledge, quarantine, resolve, reopen (writers)
//	POST .../asc/checks/rerun                 re-run a run (writers of the repository's Actions)
//	GET  .../asc/gate                         the policy gate: rules in force, refusals, who can write (signed-in readers)
//	GET  .../asc/ops                          the operation log (signed-in readers)
//	GET  .../asc/ops/undo                     the dry run and confirmation of an undo (signed-in writers)
//	POST .../asc/ops/undo                     undo one operation of the signed-in person (CSRF-checked like every POST)
//	GET  .../asc/deps                         dependencies: findings, newer versions, settings (signed-in readers)
//	POST .../asc/deps/scan                    scan trunk (administrators)
//	POST .../asc/deps/propose                 preview or create update proposals (administrators)
//	POST .../asc/deps/findings/{id}/dismiss   dismiss or reopen a finding (administrators)
func registerAscTrustRepoRoutes(m *web.Route, reqSignIn any) {
	m.Group("/asc/deps", func() {
		m.Get("", repo.AscDeps)
		m.Post("/scan", context.RepoMustNotBeArchived(), repo.AscDepsScan)
		m.Post("/propose", context.RepoMustNotBeArchived(), repo.AscDepsPropose)
		m.Post("/findings/{id}/dismiss", context.RepoMustNotBeArchived(), repo.AscDepsDismiss)
	}, reqSignIn)
	m.Group("/asc/gate", func() {
		m.Get("", repo.AscGate)
		m.Post("/overrides", context.RepoMustNotBeArchived(), repo.AscGateOverrideGrant)
		m.Post("/overrides/{id}/revoke", context.RepoMustNotBeArchived(), repo.AscGateOverrideRevoke)
	}, reqSignIn)
	m.Group("/asc/ops", func() {
		m.Get("", repo.AscOps)
		m.Get("/export", repo.AscOpsExport)
		m.Combo("/undo").Get(repo.AscOpsUndoPreview).Post(context.RepoMustNotBeArchived(), repo.AscOpsUndo)
	}, reqSignIn)
	m.Group("/asc/checks", func() {
		m.Get("", repo.AscChecks)
		m.Get("/groups/{id}", repo.AscCheckGroup)
		m.Post("/groups/{id}/state", reqSignIn, context.RepoMustNotBeArchived(), repo.AscCheckGroupState)
		m.Post("/rerun", reqSignIn, context.RepoMustNotBeArchived(), repo.AscChecksRerun)
		m.Post("/rerun_failed", reqSignIn, context.RepoMustNotBeArchived(), repo.AscChecksRerunFailed)
	})
}

// registerAscTrustPullRoutes registers, inside the "/pulls/{index}" group:
//
//	GET  .../pulls/{index}/asc/checks         the checks of the head of the pull request
//	POST .../pulls/{index}/asc/checks/rerun   re-run its runs, all or only the failed ones
func registerAscTrustPullRoutes(m *web.Route, reqSignIn any) {
	m.Group("/asc/checks", func() {
		m.Get("", repo.AscPullChecks)
		m.Post("/rerun", reqSignIn, context.RepoMustNotBeArchived(), repo.AscPullChecksRerun)
	})
}
