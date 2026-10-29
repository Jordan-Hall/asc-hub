// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"sort"

	"forgejo.org/models/unit"
	"forgejo.org/modules/log"
	"forgejo.org/services/context"
)

// The Stacks list (board Stacks): the open stacks of an asc repository as rows with a status chip, a progress rail,
// the filters Open, Mine, Agents and Needs me, and the sort Needs attention or Newest. The legacy pull request list
// stays for the Closed state and for a keyword search.

const (
	stackSortAttention = "attention"
	stackSortNewest    = "newest"
)

// sortStackRows orders rows: "newest" by the last update, anything else by what needs a person first.
func sortStackRows(rows []*stackRow, by string) {
	if by == stackSortNewest {
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Updated > rows[j].Updated })
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if ri, rj := stackStatusRank[rows[i].Status], stackStatusRank[rows[j].Status]; ri != rj {
			return ri < rj
		}
		return rows[i].Updated > rows[j].Updated
	})
}

// stackYourTurn is the first row that waits for the viewer: where the card "Your turn" jumps to.
func stackYourTurn(rows []*stackRow) *stackRow {
	for _, r := range rows {
		if r.NeedsMe {
			return r
		}
	}
	return nil
}

// prepareAscStackList adds the stack rows to the list page of pull requests of an asc repository. It does nothing when
// the viewer cannot read pull requests; the unit check is the handler's, repeated here so the data never exists for a
// viewer without it.
func prepareAscStackList(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if !ctx.Repo.CanRead(unit.TypePullRequests) || repo.GetStorageBackend() != "asc" {
		return
	}
	if ctx.FormString("state") == "closed" || ctx.FormTrim("q") != "" {
		return
	}
	changes, landed, err := loadStackChanges(ctx, repo, ctx.Repo.GitRepo, ctx.Doer)
	if err != nil {
		log.Error("stacks: list of %s: %v", repo.FullName(), err)
		ctx.Data["StackListFailed"] = true
		ctx.Data["StackRequestID"] = requestID(ctx.Req)
		return
	}
	rows := buildStackRows(changes, landed, repo.Link(), ctx.Doer)
	sortBy := stackSortAttention
	if ctx.FormString("sort") == stackSortNewest {
		sortBy = stackSortNewest
	}
	sortStackRows(rows, sortBy)
	ctx.Data["StackList"] = true
	ctx.Data["Title"] = ctx.Locale.TrString("asc_rp.tab_stacks")
	ctx.Data["StackRows"] = rows
	ctx.Data["StackCounts"] = stackFilterCounts(rows)
	ctx.Data["StackSort"] = sortBy
	ctx.Data["StackYourTurn"] = stackYourTurn(rows)
}
