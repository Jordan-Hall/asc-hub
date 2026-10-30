// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package context

import (
	"context"
	"sync"
	"time"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
)

// The counts on the Agent runs and Dependencies tabs of an asc repository (board Repo: "Agent runs 3",
// "Dependencies 2"): runs that are not finished and open dependency findings.
//
// A count is a fact about a list, so it is fetched and shown only to a person who may open that list, by the same
// rule as the page of the list: the agent runs need a signed-in person who reads the code and the Actions of the
// repository and has agent features on (ascAgentsCanReadRuns in routers/web/repo); the dependency findings need a
// signed-in person who reads the code (trustweb.CodeRepo). The answer is the same for everyone who passes the gate, so
// it is kept per repository and kind for a short time, and only after the gate. A slow or failing asc leaves the count
// out and never delays the page by more than ascCountTimeout.

const (
	ascCountTTL     = 30 * time.Second
	ascCountTimeout = 400 * time.Millisecond
	ascCountMax     = 4096 // entries kept; the map is cleared when it grows past this
)

const (
	ascCountRuns = iota
	ascCountDeps
)

type ascCountKey struct {
	repo int64
	kind int
}

type ascCountEntry struct {
	at time.Time
	n  uint32
}

// ascCountGates says which counts a viewer may be given.
func ascCountGates(signedIn, canCode, canActions, agentFeatures bool) (runs, deps bool) {
	deps = signedIn && canCode
	runs = deps && canActions && agentFeatures
	return runs, deps
}

var (
	ascCountMu     sync.Mutex
	ascCountCache  = map[ascCountKey]ascCountEntry{}
	ascCountClient *ascclient.Client
)

func ascCountConn() (*ascclient.Client, error) {
	ascCountMu.Lock()
	defer ascCountMu.Unlock()
	if ascCountClient != nil {
		return ascCountClient, nil
	}
	c, err := ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken, CallTimeout: ascCountTimeout})
	if err != nil {
		return nil, err
	}
	ascCountClient = c
	return c, nil
}

// ascTabCounts returns the counts the viewer may see; a count the gate refuses is 0 and is never fetched.
func ascTabCounts(ctx *Context, repo *repo_model.Repository) (runs, findings uint32) {
	agentFeatures, _ := ctx.Data["AscAgentFeatures"].(bool)
	wantRuns, wantDeps := ascCountGates(ctx.IsSigned && ctx.Doer != nil, ctx.Repo.CanRead(unit.TypeCode), ctx.Repo.CanRead(unit.TypeActions), agentFeatures)
	if !wantRuns && !wantDeps {
		return 0, 0
	}
	name, err := ascbackend.RepoName(setting.RepoRootPath, repo_model.RepoPath(repo.OwnerName, repo.Name))
	if err != nil {
		return 0, 0
	}
	if wantRuns {
		runs = ascCountOf(ctx, repo, ascCountRuns, func(cctx context.Context, c *ascclient.Client) (uint32, error) {
			resp, err := ascv1.NewAgentsClient(c).ListRuns(cctx, &ascv1.ListAgentRunsRequest{
				Repo: &ascv1.RepoRef{Name: name}, Limit: 100,
				States: []ascv1.AgentRunState{ascv1.AgentRunState_AGENT_RUN_STATE_QUEUED, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING, ascv1.AgentRunState_AGENT_RUN_STATE_WAITING_INPUT},
			})
			return uint32(len(resp.GetRuns())), err
		})
	}
	if wantDeps {
		findings = ascCountOf(ctx, repo, ascCountDeps, func(cctx context.Context, c *ascclient.Client) (uint32, error) {
			resp, err := ascv1.NewDepsServiceClient(c).ListFindings(cctx, &ascv1.ListDepFindingsRequest{Repo: &ascv1.RepoRef{Name: name}, Limit: 1})
			return resp.GetCountsByState()["open"], err
		})
	}
	return runs, findings
}

// ascCountOf returns the cached count of one kind, or asks asc (as the host, after the gate) and keeps the answer.
func ascCountOf(ctx context.Context, repo *repo_model.Repository, kind int, ask func(context.Context, *ascclient.Client) (uint32, error)) uint32 {
	key := ascCountKey{repo.ID, kind}
	ascCountMu.Lock()
	if e, ok := ascCountCache[key]; ok && time.Since(e.at) < ascCountTTL {
		ascCountMu.Unlock()
		return e.n
	}
	ascCountMu.Unlock()
	c, err := ascCountConn()
	if err != nil {
		return 0
	}
	cctx, cancel := context.WithTimeout(ascclient.WithSystem(ctx), ascCountTimeout)
	defer cancel()
	n, err := ask(cctx, c)
	if err != nil {
		log.Debug("asc tab counts: kind %d of %s: %v", kind, repo.FullName(), err)
		return 0
	}
	ascCountMu.Lock()
	if len(ascCountCache) >= ascCountMax {
		clear(ascCountCache)
	}
	ascCountCache[key] = ascCountEntry{at: time.Now(), n: n}
	ascCountMu.Unlock()
	return n
}
