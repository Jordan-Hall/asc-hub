// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package context

import (
	"context"
	"sync"
	"time"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
)

// The counts on the Agent runs and Dependencies tabs of an asc repository (board Repo: "Agent runs 3",
// "Dependencies 2"): runs that are not finished and open dependency findings. They are the same for everyone who may
// read the code, so one answer is kept per repository for a short time; a slow or failing asc leaves the count out
// and never delays the page by more than countTimeout. The template draws a count only inside the tab it belongs to,
// which already needs the code unit, so nothing here is shown to a person who may not read the repository.

const (
	ascCountTTL     = 30 * time.Second
	ascCountTimeout = 400 * time.Millisecond
	ascCountMax     = 4096 // entries kept; the map is cleared when it grows past this
)

type ascCountEntry struct {
	at       time.Time
	runs     uint32
	findings uint32
}

var (
	ascCountMu     sync.Mutex
	ascCountCache  = map[int64]ascCountEntry{}
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

// ascTabCounts returns the number of unfinished agent runs and open dependency findings of an asc repository.
func ascTabCounts(ctx context.Context, repo *repo_model.Repository) (runs, findings uint32) {
	ascCountMu.Lock()
	if e, ok := ascCountCache[repo.ID]; ok && time.Since(e.at) < ascCountTTL {
		ascCountMu.Unlock()
		return e.runs, e.findings
	}
	ascCountMu.Unlock()

	name, err := ascbackend.RepoName(setting.RepoRootPath, repo_model.RepoPath(repo.OwnerName, repo.Name))
	if err != nil {
		return 0, 0
	}
	c, err := ascCountConn()
	if err != nil {
		return 0, 0
	}
	cctx, cancel := context.WithTimeout(ascclient.WithSystem(ctx), ascCountTimeout)
	defer cancel()
	ref := &ascv1.RepoRef{Name: name}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		resp, err := ascv1.NewAgentsClient(c).ListRuns(cctx, &ascv1.ListAgentRunsRequest{
			Repo: ref, Limit: 100,
			States: []ascv1.AgentRunState{ascv1.AgentRunState_AGENT_RUN_STATE_QUEUED, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING, ascv1.AgentRunState_AGENT_RUN_STATE_WAITING_INPUT},
		})
		if err != nil {
			log.Debug("asc tab counts: agent runs of %s: %v", repo.FullName(), err)
			return
		}
		runs = uint32(len(resp.GetRuns()))
	}()
	go func() {
		defer wg.Done()
		resp, err := ascv1.NewDepsServiceClient(c).ListFindings(cctx, &ascv1.ListDepFindingsRequest{Repo: ref, Limit: 1})
		if err != nil {
			log.Debug("asc tab counts: findings of %s: %v", repo.FullName(), err)
			return
		}
		findings = resp.GetCountsByState()["open"]
	}()
	wg.Wait()

	ascCountMu.Lock()
	if len(ascCountCache) >= ascCountMax {
		clear(ascCountCache)
	}
	ascCountCache[repo.ID] = ascCountEntry{at: time.Now(), runs: runs, findings: findings}
	ascCountMu.Unlock()
	return runs, findings
}
