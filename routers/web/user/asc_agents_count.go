// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"net/http"
	"strconv"
	"time"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/context"
)

// The "N agents running" pill of the header (board Shell): how many of the person's own agent runs are queued,
// running or waiting, over the repositories of "Your agents" (ascAgentsHomeRepos). One Agents.CountRuns call for
// all of them, cached per person, so the header costs at most one cheap call a person every ascAgentsCountTTL.
//
//	GET /-/asc/agents/count   {"count": N}
const (
	ascAgentsCountTTL     = 30 // seconds
	ascAgentsCountTimeout = 3 * time.Second
	// ascAgentsCountMax bounds the number the pill shows.
	ascAgentsCountMax = 999
)

// AscAgentsCount serves `GET /-/asc/agents/count`. The count is 0 while the person's agent features are off, when
// they have no asc repository and when asc cannot be asked (logged): the pill is a convenience and never an error.
func AscAgentsCount(ctx *context.Context) {
	if !setting.Asc.Enabled || ctx.Doer == nil {
		ctx.NotFound("AscAgentsCount", nil)
		return
	}
	ctx.Resp.Header().Set("Cache-Control", "private, no-store")
	key := "AscAgentsCount_" + strconv.FormatInt(ctx.Doer.ID, 10)
	if ctx.Cache != nil {
		if v, ok := ctx.Cache.Get(key).(int); ok {
			ctx.JSON(http.StatusOK, map[string]int{"count": v})
			return
		}
	}
	n, ok := ascAgentsRunning(ctx)
	if ok && ctx.Cache != nil {
		if err := ctx.Cache.Put(key, n, ascAgentsCountTTL); err != nil {
			log.Warn("asc agents: caching the count of user %d: %v", ctx.Doer.ID, err)
		}
	}
	ctx.JSON(http.StatusOK, map[string]int{"count": n})
}

// ascAgentsRunning counts the active runs of the signed-in person. ok is false when asc could not be asked, so a
// failure is not cached.
func ascAgentsRunning(ctx *context.Context) (int, bool) {
	if !ascagentui.FeaturesEnabled(ctx, ctx.Doer) {
		return 0, true
	}
	repos, err := ascAgentsHomeRepos(ctx)
	if err != nil {
		log.Error("asc agents: the repositories of user %d: %v (request %s)", ctx.Doer.ID, err, ctx.RequestIDForLog())
		return 0, false
	}
	refs := ascAgentsCountRefs(repos)
	if len(refs) == 0 {
		return 0, true
	}
	conn, err := ascDraftsConn()
	if err != nil {
		log.Error("asc agents: the connection to asc serve: %v (request %s)", err, ctx.RequestIDForLog())
		return 0, false
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx.Req.Context()), ascAgentsCountTimeout)
	defer cancel()
	states, _ := ascagentui.RunStateFilter("active")
	resp, err := ascv1.NewAgentsClient(conn).CountRuns(callCtx, &ascv1.CountAgentRunsRequest{
		Repos:     refs,
		States:    states,
		CreatedBy: ctx.Doer.Name, // always the signed-in person
	})
	if err != nil {
		if f := ascagentui.Classify(err); f == nil || !f.Disabled {
			log.Warn("asc agents: CountRuns for user %d: %v (request %s)", ctx.Doer.ID, err, ctx.RequestIDForLog())
		}
		return 0, false
	}
	return int(min(resp.GetTotal(), ascAgentsCountMax)), true
}

// ascAgentsCountRefs names the asc repositories, each once.
func ascAgentsCountRefs(repos []*repo_model.Repository) []*ascv1.RepoRef {
	refs := make([]*ascv1.RepoRef, 0, len(repos))
	seen := map[string]bool{}
	for _, repo := range repos {
		name, err := ascbackend.RepoName(setting.RepoRootPath, repo_model.RepoPath(repo.OwnerName, repo.Name))
		if err != nil || seen[name] {
			continue
		}
		seen[name] = true
		refs = append(refs, &ascv1.RepoRef{Name: name})
	}
	return refs
}
