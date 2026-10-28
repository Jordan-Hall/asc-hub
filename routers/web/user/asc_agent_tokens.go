// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"net/http"
	"strconv"
	"time"

	"forgejo.org/models/db"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/base"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/context"
)

// "Your agents" (PLAN unit H5): the person's own switch for agent features, their recent agent runs across the
// repositories they work in, and the way into the agent token page of each repository they administer.
//
//	GET  /-/asc/agents            the switch, the runs, the repositories
//	POST /-/asc/agents/features   turn the switch on or off (enabled=1 or enabled=0)
//
// Agents are optional: the switch is off until the person turns it on, and while it is off no agent page of any
// repository answers (404) and no link to one is drawn. The page works for a signed-in person only. The runs
// come from the repositories the person can already reach (the access-checked repository search), only the
// person's own runs are asked for (`created_by` is the signed-in name, never a value of the request), and
// every run is checked to belong to the repository it was asked about before it is shown. Tokens are not listed
// here: they are repository admin business and are managed on each repository's agent token page.
const (
	tplAscAgentsHome base.TplName = "user/asc_agents"

	// ascAgentsHomeMaxRepos bounds the repositories asked per page view; ascAgentsHomeRunsPer the runs per repository.
	ascAgentsHomeMaxRepos = 30
	ascAgentsHomeRunsPer  = 5
	ascAgentsHomeTimeout  = 20 * time.Second
)

// AscAgentsHomeRun is one of the person's runs as shown.
type AscAgentsHomeRun struct {
	Repo    *repo_model.Repository
	ID      uint64
	Title   string
	Agent   string
	State   ascagentui.State
	Created time.Time
	Link    string
}

// AscAgentsHomeRepo is a repository the person can start agents in or administer.
type AscAgentsHomeRepo struct {
	Repo      *repo_model.Repository
	IsAdmin   bool
	TokensURL string
	RunsURL   string
}

// AscAgentsHome serves `GET /-/asc/agents`.
func AscAgentsHome(ctx *context.Context) {
	if !setting.Asc.Enabled || ctx.Doer == nil {
		ctx.NotFound("AscAgentsHome", nil)
		return
	}
	ctx.Data["Title"] = "Your agents"
	ctx.Data["PageIsAscAgents"] = true
	on := ascagentui.FeaturesEnabled(ctx, ctx.Doer)
	ctx.Data["AscAgentFeatures"] = on
	if !on {
		ctx.HTML(http.StatusOK, tplAscAgentsHome)
		return
	}

	repos, err := ascAgentsHomeRepos(ctx)
	if err != nil {
		ctx.ServerError("SearchRepository", err)
		return
	}
	rows := make([]AscAgentsHomeRepo, 0, len(repos))
	for _, repo := range repos {
		perm, err := access_model.GetUserRepoPermission(ctx, repo, ctx.Doer)
		if err != nil {
			ctx.ServerError("GetUserRepoPermission", err)
			return
		}
		link := repo.Link()
		rows = append(rows, AscAgentsHomeRepo{
			Repo:      repo,
			IsAdmin:   perm.IsAdmin(),
			TokensURL: link + "/asc/agents/tokens",
			RunsURL:   link + "/asc/agents",
		})
	}
	ctx.Data["AscAgentsRepos"] = rows

	conn, err := ascDraftsConn()
	if err != nil {
		log.Error("asc agents: no connection to asc serve: %v", err)
		ctx.Data["AscAgentsError"] = ascagentui.Classify(err).Message
		ctx.HTML(http.StatusBadGateway, tplAscAgentsHome)
		return
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx.Req.Context()), ascAgentsHomeTimeout)
	defer cancel()
	api := ascv1.NewAgentsClient(conn)

	var runs []AscAgentsHomeRun
	failed := false
	for _, repo := range repos {
		name, err := ascbackend.RepoName(setting.RepoRootPath, repo_model.RepoPath(repo.OwnerName, repo.Name))
		if err != nil {
			continue
		}
		resp, err := api.ListRuns(callCtx, &ascv1.ListAgentRunsRequest{
			Repo:      &ascv1.RepoRef{Name: name},
			CreatedBy: ctx.Doer.Name, // always the signed-in person
			Limit:     ascAgentsHomeRunsPer,
		})
		if err != nil {
			if ctx.Req.Context().Err() != nil {
				return // the browser went away
			}
			// A repository where agents are off answers FAILED_PRECONDITION: that is not a fault of the page.
			if f := ascagentui.Classify(err); f != nil && !f.Disabled {
				log.Error("asc agents: ListRuns failed for repository %d: %s", repo.ID, f.Message)
				failed = true
			}
			continue
		}
		for _, run := range resp.GetRuns() {
			if run == nil || run.GetId() == 0 || run.GetRepo() != name {
				continue // not a run of this repository: never shown
			}
			title := ascagentui.CleanLine(run.GetTitle(), ascagentui.MaxTitleBytes)
			if title == "" {
				title = "Run " + strconv.FormatUint(run.GetId(), 10)
			}
			runs = append(runs, AscAgentsHomeRun{
				Repo:    repo,
				ID:      run.GetId(),
				Title:   title,
				Agent:   ascagentui.SafeName(run.GetAgent()),
				State:   ascagentui.RunState(run.GetState()),
				Created: ascagentui.Time(run.GetCreated()),
				Link:    repo.Link() + "/asc/agents/runs/" + strconv.FormatUint(run.GetId(), 10),
			})
		}
	}
	sortAscAgentsHomeRuns(runs)
	if len(runs) > 20 {
		runs = runs[:20]
	}
	ctx.Data["AscAgentsRuns"] = runs
	ctx.Data["AscAgentsAnyFailed"] = failed
	ctx.HTML(http.StatusOK, tplAscAgentsHome)
}

// sortAscAgentsHomeRuns puts the newest first (an insertion sort: there are at most 150 rows).
func sortAscAgentsHomeRuns(runs []AscAgentsHomeRun) {
	for i := 1; i < len(runs); i++ {
		for j := i; j > 0 && runs[j].Created.After(runs[j-1].Created); j-- {
			runs[j], runs[j-1] = runs[j-1], runs[j]
		}
	}
}

// ascAgentsHomeRepos lists the asc repositories the person owns or collaborates on, newest activity first.
func ascAgentsHomeRepos(ctx *context.Context) ([]*repo_model.Repository, error) {
	var out []*repo_model.Repository
	seen := map[int64]bool{}
	for _, collaborate := range []optional.Option[bool]{optional.Some(false), optional.Some(true)} {
		repos, _, err := repo_model.SearchRepository(ctx, &repo_model.SearchRepoOptions{
			ListOptions: db.ListOptions{Page: 1, PageSize: ascAgentsHomeMaxRepos * 4},
			Actor:       ctx.Doer,
			OwnerID:     ctx.Doer.ID,
			Private:     true,
			Collaborate: collaborate,
			Archived:    optional.Some(false),
			OrderBy:     "updated_unix DESC",
		})
		if err != nil {
			return nil, err
		}
		for _, repo := range repos {
			if len(out) >= ascAgentsHomeMaxRepos {
				return out, nil
			}
			if seen[repo.ID] || repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
				continue
			}
			seen[repo.ID] = true
			out = append(out, repo)
		}
	}
	return out, nil
}

// AscAgentsFeaturesPost serves `POST /-/asc/agents/features`.
func AscAgentsFeaturesPost(ctx *context.Context) {
	if !setting.Asc.Enabled || ctx.Doer == nil {
		ctx.NotFound("AscAgentsFeaturesPost", nil)
		return
	}
	var on bool
	switch ctx.FormString("enabled") {
	case "1":
		on = true
	case "0":
	default:
		ctx.Error(http.StatusBadRequest)
		return
	}
	if err := ascagentui.SetFeatures(ctx, ctx.Doer, on); err != nil {
		ctx.ServerError("SetFeatures", err)
		return
	}
	if on {
		ctx.Flash.Success("Agent features are on. They are for you only.")
	} else {
		ctx.Flash.Success("Agent features are off.")
	}
	if ctx.FormString("return") == "appearance" {
		ctx.Redirect(setting.AppSubURL + "/user/settings/appearance")
		return
	}
	ctx.Redirect(setting.AppSubURL + "/-/asc/agents")
}
