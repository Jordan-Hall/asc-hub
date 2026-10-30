// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"forgejo.org/modules/web"
	"forgejo.org/routers/web/repo"
	"forgejo.org/routers/web/user"
	"forgejo.org/services/context"
)

// registerAscAgentsUserRoutes registers the person-level agent pages (PLAN unit H5).
//
//	GET  /-/asc/agents            the agent features switch, the person's runs, their repositories
//	POST /-/asc/agents/features   turn the switch on or off
func registerAscAgentsUserRoutes(m *web.Route, reqSignIn any) {
	m.Get("/-/asc/agents", reqSignIn, user.AscAgentsHome)
	m.Post("/-/asc/agents/features", reqSignIn, user.AscAgentsFeaturesPost)
}

// registerAscAgentsRepoRoutes registers the agent and workflow pages of a repository. It must be called inside the
// "/{username}/{reponame}" group, after context.RepoAssignment. Every route needs a signed-in person who can read
// the code; each handler repeats the check and adds its own (write for actions, admin for tokens, and the agent
// features switch), see routers/web/repo/asc_agents_common.go.
func registerAscAgentsRepoRoutes(m *web.Route, reqSignIn, reqRepoCodeReader any) {
	m.Group("/asc", func() {
		m.Group("/agents", func() {
			m.Get("", repo.AscAgents)
			m.Combo("/new").Get(repo.AscAgentsNew).Post(repo.AscAgentsCreate)
			m.Group("/runs/{id}", func() {
				m.Get("", repo.AscAgentsRun)
				m.Get("/events", repo.AscAgentsEvents)
				m.Post("/cancel", repo.AscAgentsCancel)
				m.Post("/input", repo.AscAgentsInput)
				m.Post("/pause", repo.AscAgentsPause)
				m.Post("/limits", repo.AscAgentsLimits)
				m.Post("/continue", repo.AscAgentsContinue)
				m.Post("/rollback", repo.AscAgentsRollback)
				m.Post("/pulls", repo.AscAgentsOpenPulls)
				m.Get("/log", repo.AscAgentsLog)
			})
			m.Post("/requests/{id}/decide", repo.AscAgentsDecide)
			m.Get("/estimate", repo.AscAgentsEstimate)
			m.Get("/spend", repo.AscAgentsSpend)
			m.Post("/spend/budget", repo.AscAgentsSpendBudget)
			m.Group("/tokens", func() {
				m.Combo("").Get(repo.AscAgentsTokens).Post(repo.AscAgentsTokenCreate)
				m.Post("/{id}/revoke", repo.AscAgentsTokenRevoke)
				m.Post("/{id}/budget", repo.AscAgentsTokenBudget)
			})
		})
		m.Group("/workflows", func() {
			m.Get("", repo.AscWorkflows)
			m.Get("/runs", repo.AscWorkflowRuns)
			m.Group("/runs/{id}", func() {
				m.Get("", repo.AscWorkflowRun)
				m.Get("/state", repo.AscWorkflowRunState)
				m.Post("/cancel", repo.AscWorkflowRunCancel)
				m.Post("/decide", repo.AscWorkflowRunDecide)
				m.Post("/rerun", repo.AscWorkflowRunRerun)
			})
			m.Group("/{name}", func() {
				m.Get("", repo.AscWorkflowDef)
				m.Post("/trigger", repo.AscWorkflowTrigger)
				m.Post("/enable", repo.AscWorkflowEnable)
			})
		})
	}, reqSignIn, repo.MustBeNotEmpty, context.RepoRef(), reqRepoCodeReader)
}
