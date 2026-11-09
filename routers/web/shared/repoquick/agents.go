// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repoquick

import (
	gocontext "context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

const (
	// agentRunsBatch is the most repositories one Agents.CountRuns call names.
	agentRunsBatch = 50
	// agentRunsTTL is how long the counts of a set of repositories are kept, in seconds.
	agentRunsTTL     = 60
	agentRunsTimeout = 3 * time.Second
	// AgentRunsMax bounds the number a row shows.
	AgentRunsMax = 999
)

// LoadAgentRuns gives the rows of the list their agent activity chip (ctx.Data["AscRowAgentRuns"], keyed by repository
// id): how many agent runs were started in the repository in the last repo_model.AscAgentActivityDays days, the window
// of the Agent activity quick chip. Only asc repositories have runs. The chip links to the agent runs of the
// repository, which need a signed-in person with agent features on who can read the code, so only such a viewer gets
// chips and only for the rows whose code they can read. The counts come from one Agents.CountRuns call per 50 rows,
// kept for agentRunsTTL seconds per set of repositories; they do not depend on the viewer once the rows are chosen. A
// row without runs has no entry. A failure is logged and no row shows a chip.
func LoadAgentRuns(ctx *context.Context, repos repo_model.RepositoryList) {
	if !setting.Asc.Enabled || ctx.Doer == nil || len(repos) == 0 || !ascagentui.FeaturesEnabled(ctx, ctx.Doer) {
		return
	}
	ids := make([]int64, 0, len(repos))
	names := make([]string, 0, len(repos))
	seen := map[string]bool{}
	for _, r := range repos {
		if r.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
			continue
		}
		p, err := access_model.GetUserRepoPermission(ctx, r, ctx.Doer)
		if err != nil {
			log.Error("repository list: the permission of %s (request %s): %v", r.FullName(), ctx.RequestIDForLog(), err)
			return
		}
		if !p.CanRead(unit.TypeCode) {
			continue
		}
		name, err := ascbackend.RepoName(setting.RepoRootPath, repo_model.RepoPath(r.OwnerName, r.Name))
		if err != nil || seen[name] {
			continue
		}
		seen[name] = true
		ids = append(ids, r.ID)
		names = append(names, name)
	}
	if len(names) == 0 {
		return
	}
	counts, ok := agentRunCounts(ctx, names)
	if !ok {
		return
	}
	runs := make(map[int64]uint64, len(ids))
	for i, id := range ids {
		if counts[i] > 0 {
			runs[id] = min(counts[i], AgentRunsMax)
		}
	}
	ctx.Data["AscRowAgentRuns"] = runs
	ctx.Data["AscRowAgentDays"] = repo_model.AscAgentActivityDays
}

// agentRunCounts returns the count of each name, in order, from the cache or from asc. ok is false when asc could not
// be asked; a failure is not cached.
func agentRunCounts(ctx *context.Context, names []string) ([]uint64, bool) {
	sum := sha256.Sum256([]byte(strings.Join(names, "\n")))
	key := "AscRowAgentRuns_" + hex.EncodeToString(sum[:16])
	if ctx.Cache != nil {
		if v, ok := ctx.Cache.Get(key).(string); ok {
			if counts, ok := parseCounts(v, len(names)); ok {
				return counts, true
			}
		}
	}
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("repository list: the connection to asc serve: %v (request %s)", err, ctx.RequestIDForLog())
		return nil, false
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx.Req.Context()), agentRunsTimeout)
	defer cancel()
	since := repo_model.AgentActivitySince(Now())
	client := ascv1.NewAgentsClient(conn)
	counts := make([]uint64, 0, len(names))
	for start := 0; start < len(names); start += agentRunsBatch {
		batch := names[start:min(start+agentRunsBatch, len(names))]
		refs := make([]*ascv1.RepoRef, 0, len(batch))
		for _, n := range batch {
			refs = append(refs, &ascv1.RepoRef{Name: n})
		}
		resp, err := client.CountRuns(callCtx, &ascv1.CountAgentRunsRequest{Repos: refs, Since: int64(since)})
		if err != nil {
			if f := ascagentui.Classify(err); f == nil || !f.Disabled {
				log.Warn("repository list: CountRuns: %v (request %s)", err, ctx.RequestIDForLog())
			}
			return nil, false
		}
		if len(resp.GetPerRepo()) != len(batch) {
			log.Warn("repository list: CountRuns answered %d counts for %d repositories (request %s)", len(resp.GetPerRepo()), len(batch), ctx.RequestIDForLog())
			return nil, false
		}
		counts = append(counts, resp.GetPerRepo()...)
	}
	if ctx.Cache != nil {
		if err := ctx.Cache.Put(key, formatCounts(counts), agentRunsTTL); err != nil {
			log.Warn("repository list: caching the agent run counts: %v", err)
		}
	}
	return counts, true
}

func formatCounts(counts []uint64) string {
	parts := make([]string, len(counts))
	for i, n := range counts {
		parts[i] = strconv.FormatUint(n, 10)
	}
	return strings.Join(parts, ",")
}

func parseCounts(v string, want int) ([]uint64, bool) {
	parts := strings.Split(v, ",")
	if len(parts) != want {
		return nil, false
	}
	counts := make([]uint64, len(parts))
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return nil, false
		}
		counts[i] = n
	}
	return counts, true
}
