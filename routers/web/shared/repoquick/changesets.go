// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repoquick

import (
	git_model "forgejo.org/models/git"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	stats_indexer "forgejo.org/modules/indexer/stats"
	"forgejo.org/modules/log"
	"forgejo.org/services/context"
)

// QueueChangesetCount asks the stats indexer to count the changesets of a repository. It is a variable for the tests.
var QueueChangesetCount = stats_indexer.UpdateRepoIndexer

// LoadChangesets gives the rows of the list their changeset count (ctx.Data["AscRowChangesets"], keyed by repository
// id): how many changesets the head of the default branch reaches, as the stats indexer stored it. Only rows whose code
// the viewer can read and that are not empty get a count. A count of an older head is still shown, and the row is
// queued to be counted again, as is a row that was never counted; such a row shows no count until the indexer ran. A
// failure is logged and no row shows a count.
func LoadChangesets(ctx *context.Context, repos repo_model.RepositoryList) {
	if len(repos) == 0 {
		return
	}
	heads := make(map[int64]string, len(repos))
	byID := make(map[int64]*repo_model.Repository, len(repos))
	for _, r := range repos {
		if r.IsEmpty || r.DefaultBranch == "" {
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
		heads[r.ID] = r.DefaultBranch
		byID[r.ID] = r
	}
	if len(heads) == 0 {
		return
	}
	commits, err := git_model.FindBranchesByRepoAndBranchName(ctx, heads)
	if err != nil {
		log.Error("repository list: the default branch heads (request %s): %v", ctx.RequestIDForLog(), err)
		return
	}
	ids := make([]int64, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	stored, err := repo_model.GetAscRepoChangesets(ctx, ids)
	if err != nil {
		log.Error("repository list: the changeset counts (request %s): %v", ctx.RequestIDForLog(), err)
		return
	}
	counts := make(map[int64]int64, len(stored))
	for id, r := range byID {
		s := stored[id]
		if s != nil && s.Changesets > 0 {
			counts[id] = s.Changesets
		}
		if head := commits[id]; head != "" && (s == nil || s.CommitSha != head) {
			if err := QueueChangesetCount(r); err != nil {
				log.Warn("repository list: queueing the changeset count of %s: %v", r.FullName(), err)
			}
		}
	}
	ctx.Data["AscRowChangesets"] = counts
}
