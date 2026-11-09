// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package byoweb

import (
	"strings"
	"time"

	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/services/ascagentui"
	svc "forgejo.org/services/ascbyo"
	"forgejo.org/services/context"
)

// UseRow is one line of the "Who used this key" table: what the key was used for, by whom, where and what it cost.
type UseRow struct {
	// KindKey is the locale key of the kind of use.
	KindKey string
	// Subject is the run id, the change id or the day; SubjectShort is what the table shows, SubjectURL where it
	// links ("" when the viewer cannot open it).
	Subject, SubjectShort, SubjectURL string
	Actor                             string
	// Repo is "owner/name"; RepoURL is set when the viewer can read the repository.
	Repo, RepoURL, Agent, Model string
	LastAt                      time.Time
	Requests, InputTokens       uint64
	OutputTokens, Unpriced      uint64
	Cost                        string
}

// usesTokenParam is the query parameter that carries the page of the uses of a key.
const usesTokenParam = "uses"

func useKindKey(k ascv1.AiActivityKind) string {
	switch k {
	case ascv1.AiActivityKind_AI_ACTIVITY_KIND_AGENT_RUN:
		return "ascbyo.ai.uses.kind_run"
	case ascv1.AiActivityKind_AI_ACTIVITY_KIND_REVIEW_SUMMARY:
		return "ascbyo.ai.uses.kind_summary"
	case ascv1.AiActivityKind_AI_ACTIVITY_KIND_KEY_TEST:
		return "ascbyo.ai.uses.kind_test"
	}
	return "ascbyo.ai.uses.kind_other"
}

// repoAccess is what the viewer may open in a repository named by a use.
type repoAccess struct {
	repo        *repo_model.Repository
	code, pulls bool
	agents      bool
}

// accessOf looks up each repository once. A repository that does not exist, or that the viewer cannot read, gives
// no links: the row still shows its name as asc recorded it.
func accessOf(ctx *context.Context, cache map[string]*repoAccess, full string) *repoAccess {
	if a, ok := cache[full]; ok {
		return a
	}
	a := &repoAccess{}
	cache[full] = a
	owner, name, ok := strings.Cut(full, "/")
	if !ok || ctx.Doer == nil {
		return a
	}
	repo, err := repo_model.GetRepositoryByOwnerAndName(ctx, owner, name)
	if err != nil {
		return a
	}
	perm, err := access_model.GetUserRepoPermission(ctx, repo, ctx.Doer)
	if err != nil || !perm.CanRead(unit.TypeCode) {
		return a
	}
	a.repo, a.code, a.pulls = repo, true, perm.CanRead(unit.TypePullRequests)
	if on, err := repo_model.AscFeatureEnabled(ctx, repo.ID, repo_model.AscFeatureAgents); err == nil {
		a.agents = on
	}
	return a
}

// useRowsOf turns cleaned uses into table rows, with links the viewer may follow.
func useRowsOf(ctx *context.Context, uses []svc.KeyUse) []UseRow {
	cache := map[string]*repoAccess{}
	out := make([]UseRow, 0, len(uses))
	for _, u := range uses {
		r := UseRow{
			KindKey: useKindKey(u.Kind), Subject: u.Subject, SubjectShort: u.Subject, Actor: u.Actor, Repo: u.Repo,
			Agent: u.Agent, Model: u.Model, LastAt: u.LastAt, Requests: u.Requests, InputTokens: u.InputTokens,
			OutputTokens: u.OutputTokens, Unpriced: u.Unpriced, Cost: svc.FormatUSD(u.CostMicro),
		}
		var a *repoAccess
		if u.Repo != "" {
			a = accessOf(ctx, cache, u.Repo)
			if a.code {
				r.RepoURL = a.repo.Link()
			}
		}
		switch u.Kind {
		case ascv1.AiActivityKind_AI_ACTIVITY_KIND_AGENT_RUN:
			if u.Subject != "" && a != nil && a.agents {
				r.SubjectURL = a.repo.Link() + "/asc/agents/runs/" + u.Subject
			}
		case ascv1.AiActivityKind_AI_ACTIVITY_KIND_REVIEW_SUMMARY:
			r.SubjectShort = ascagentui.ShortChange(u.Subject)
			if a != nil && a.pulls {
				links, err := ascagentui.ChangeLinks(ctx, a.repo, a.repo.Link(), [][]byte{ascagentui.ChangeBytes(u.Subject)}, true)
				if err == nil && len(links) == 1 {
					r.SubjectURL = links[0].URL
				}
			}
		}
		out = append(out, r)
	}
	return out
}

// loadUses fills the "Who used this key" table, best effort: an asc without the call, or a failure, leaves the
// table saying so and the rest of the page works.
func loadUses(ctx *context.Context, lvl *Level, s *svc.Service, p *Page, id string) {
	token := ctx.FormString(usesTokenParam)
	if !svc.ValidUsesToken(token) {
		token = ""
	}
	uses, err := s.KeyUses(ctx, lvl.Scope, id, token)
	if err != nil {
		log.Debug("asc byo: uses of a key in %s: %v", lvl.Scope, err)
		return
	}
	p.Uses = useRowsOf(ctx, uses.Rows)
	p.UsesLoaded = true
	p.UsesLater = token != ""
	if uses.Next != "" {
		p.UsesNextURL = lvl.Base + "/ai/keys/" + id + "?tab=" + keyTabActivity + "&" + usesTokenParam + "=" + uses.Next
	}
	p.UsesFirstURL = lvl.Base + "/ai/keys/" + id + "?tab=" + keyTabActivity
}
