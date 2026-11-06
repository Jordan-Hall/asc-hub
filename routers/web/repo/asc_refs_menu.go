// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"

	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascsync"
	"forgejo.org/services/context"
)

// The Switch version menu of an asc repository (board Repo, "Popover: switch version"): one list of the shared refs,
// the stacks and the tags, each with its kind, searched as one ("Find a ref, stack or tag"). In a branchless
// repository the shared refs are the ones asc keeps (RepoConfig.shared_refs) and the menu says so.

// ascRefKind is the kind of a ref in the menu, and the label the person reads next to it.
type ascRefKind string

const (
	ascRefShared ascRefKind = "shared"
	ascRefStack  ascRefKind = "stack"
	ascRefTag    ascRefKind = "tag"
)

// ascRefItem is one row of the menu.
type ascRefItem struct {
	Name string     `json:"name"`
	Kind ascRefKind `json:"kind"`
}

// ascRefsMenu is the answer of the menu route.
type ascRefsMenu struct {
	Refs       []ascRefItem `json:"refs"`
	Branchless bool         `json:"branchless"`
}

// ascRefsMenuItems puts the three lists in the order of the board (shared refs, stacks, tags), each name once. In a
// branchless repository the shared refs are asc's; a branch that asc does not share and that is not a stack is left
// out, since a branchless repository has no other branches to show.
func ascRefsMenuItems(shared, stacks, tags, ascShared []string, branchless bool) []ascRefItem {
	if branchless {
		shared = ascShared
	}
	out := make([]ascRefItem, 0, len(shared)+len(stacks)+len(tags))
	seen := map[string]bool{}
	add := func(kind ascRefKind, names []string) {
		n := 0
		for _, name := range names {
			if name == "" || seen[name] || n >= ascCompareMaxRefs {
				continue
			}
			seen[name] = true
			n++
			out = append(out, ascRefItem{Name: name, Kind: kind})
		}
	}
	add(ascRefShared, shared)
	add(ascRefStack, stacks)
	add(ascRefTag, tags)
	return out
}

// AscRefsMenu answers GET /{owner}/{repo}/asc/refs with the menu's rows.
func AscRefsMenu(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if !setting.Asc.Enabled || repo == nil || repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		ctx.NotFound("AscRefsMenu", nil)
		return
	}
	shared, stacks, tags := ascRefLists(ctx, repo)
	var ascShared []string
	branchless := false
	if bs, err := ascsync.LoadBranchlessSettings(ctx, repo); err != nil {
		// without asc the menu still lists the hub's branches and tags
		log.Warn("asc refs menu: branchless settings of %s: %v", repo.FullName(), err)
	} else {
		branchless, ascShared = bs.Branchless, bs.SharedRefs
	}
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	ctx.JSON(http.StatusOK, ascRefsMenu{Refs: ascRefsMenuItems(shared, stacks, tags, ascShared, branchless), Branchless: branchless})
}
