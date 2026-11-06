// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"fmt"
	"strings"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unit"
	"forgejo.org/modules/cache"
	"forgejo.org/modules/log"
	"forgejo.org/services/context"
)

// The folder rail of the code pages marks each file that open stacks change (design board Code: a count with the
// title "Touched by open stacks", and "N files touched by open stacks" under the rail). The stacks are the open pull
// requests onto the shown branch; their changed paths are read once per head and cached.

const (
	// ascTreeMaxStacks bounds the open stacks read for one page, most recently updated first.
	ascTreeMaxStacks = 50
	// ascTreeMaxStackPaths bounds the paths kept for one stack.
	ascTreeMaxStackPaths = 5000
	// ascStackPathsFailed is cached for a head whose changed paths could not be read. A path never contains NUL.
	ascStackPathsFailed = "\x00"
)

// ascStackPathsLister returns the paths that one open pull request changes.
type ascStackPathsLister func(pr *issues_model.PullRequest) ([]string, error)

// ascTouchedPaths counts, per path, the stacks that change it. A stack that cannot be read is left out.
func ascTouchedPaths(prs []*issues_model.PullRequest, list ascStackPathsLister) map[string]int {
	touched := map[string]int{}
	for i, pr := range prs {
		if i >= ascTreeMaxStacks {
			break
		}
		paths, err := list(pr)
		if err != nil {
			log.Debug("the changed paths of pull %d are left out of the folder rail: %v", pr.ID, err)
			continue
		}
		seen := map[string]bool{}
		for _, p := range paths {
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			touched[p]++
		}
	}
	return touched
}

// markAscTreeTouched sets the stack count of each file in the rail and returns how many shown files have one.
func markAscTreeTouched(nodes []*AscTreeNode, touched map[string]int) int {
	n := 0
	for _, node := range nodes {
		if node.IsDir {
			n += markAscTreeTouched(node.Children, touched)
			continue
		}
		node.Stacks = touched[node.Path]
		if node.Stacks > 0 {
			n++
		}
	}
	return n
}

// prepareAscTreeTouched marks the rail's files with the open stacks onto the shown branch.
func prepareAscTreeTouched(ctx *context.Context, nodes []*AscTreeNode) {
	// the counts tell which files open pull requests change: only for a person who may read the pull requests
	if !ctx.Repo.IsViewBranch || ctx.Repo.GitRepo == nil || !ctx.Repo.Repository.CanEnablePulls() || !ctx.Repo.CanRead(unit.TypePullRequests) {
		return
	}
	prs, err := issues_model.GetUnmergedPullRequestsByBaseInfo(ctx, ctx.Repo.Repository.ID, ctx.Repo.BranchName)
	if err != nil {
		log.Warn("the open stacks of %s are left out of the folder rail: %v", ctx.Repo.Repository.FullName(), err)
		return
	}
	gitRepo := ctx.Repo.GitRepo
	touched := ascTouchedPaths(prs, func(pr *issues_model.PullRequest) ([]string, error) {
		if pr.MergeBase == "" {
			return nil, fmt.Errorf("no merge base")
		}
		head, err := gitRepo.GetRefCommitID(pr.GetGitRefName())
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf("asc-stack-paths:%d:%s:%s", pr.ID, pr.MergeBase, head)
		joined, err := cache.GetString(key, func() (string, error) {
			paths, err := gitRepo.GetFilesChangedBetween(pr.MergeBase, head)
			if err != nil {
				// remembered, so a head that cannot be compared is not compared again on every page
				log.Debug("the changed paths of pull %d: %v", pr.ID, err)
				return ascStackPathsFailed, nil
			}
			if len(paths) > ascTreeMaxStackPaths {
				paths = paths[:ascTreeMaxStackPaths]
			}
			return strings.Join(paths, "\n"), nil
		})
		if err != nil || joined == "" || joined == ascStackPathsFailed {
			return nil, err
		}
		return strings.Split(joined, "\n"), nil
	})
	if len(touched) == 0 {
		return
	}
	if n := markAscTreeTouched(nodes, touched); n > 0 {
		ctx.Data["AscTreeTouched"] = n
	}
}
