// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"fmt"
	"path"
	"strings"

	"forgejo.org/modules/base"
	"forgejo.org/modules/cache"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
	"forgejo.org/services/context"
)

// The "Not found at this version" state of an asc repository's code pages (design board Code): a path that is not in
// the version being looked at says where it is on the default branch: the change that added it, or the change that
// removed it, with Go to main and File history.

// AscNotFound is what the state shows.
type AscNotFound struct {
	// Name is the file or folder the person asked for.
	Name string
	// Version is the ref and short commit being looked at ("main@8d1c7e0").
	Version string
	// Main is the default branch; MainLink is the path on it, or its root when the path is not there.
	Main, MainLink string
	// OnMain says the path is on the default branch, and Change is the short id of the change that added it. When the
	// path is not on it, Change is the change that removed it, or empty when no version of the default branch had it.
	OnMain bool
	Change string
	// ChangeLink opens Change; HistoryLink is the path's history on the default branch, empty when it has none.
	ChangeLink, HistoryLink string
}

// ascPathChanges returns the newest and the oldest commit of the default branch's history of a path, as ids. Both are
// empty for a path the branch never had.
func ascPathChanges(gitRepo *git.Repository, branch, treePath string) (newest, oldest string, err error) {
	n, err := gitRepo.FileCommitsCount(branch, treePath)
	if err != nil || n == 0 {
		return "", "", err
	}
	first, err := gitRepo.CommitsByFileAndRange(git.CommitsByFileAndRangeOptions{Revision: branch, File: treePath, Page: 1})
	if err != nil || len(first) == 0 {
		return "", "", err
	}
	// the history is read a page of setting.Git.CommitsRangeSize at a time, newest first: the oldest is the last
	// commit of the last page
	pageSize := int64(setting.Git.CommitsRangeSize)
	last := first
	if pages := (n + pageSize - 1) / pageSize; pages > 1 {
		if last, err = gitRepo.CommitsByFileAndRange(git.CommitsByFileAndRangeOptions{Revision: branch, File: treePath, Page: int(pages)}); err != nil || len(last) == 0 {
			return "", "", err
		}
	}
	return first[0].ID.String(), last[len(last)-1].ID.String(), nil
}

// prepareAscNotFound puts the state into ctx.Data["AscNotFound"] when the shown path of an asc repository is not in
// the version being looked at.
func prepareAscNotFound(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if repo == nil || ctx.Repo.TreePath == "" || ctx.Repo.GitRepo == nil || ctx.Repo.Commit == nil ||
		repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		return
	}
	treePath, main := ctx.Repo.TreePath, repo.DefaultBranch
	nf := &AscNotFound{
		Name:     path.Base(treePath),
		Version:  ctx.Repo.RefName + "@" + base.ShortSha(ctx.Repo.CommitID),
		Main:     main,
		MainLink: ctx.Repo.RepoLink + "/src/branch/" + util.PathEscapeSegments(main),
	}
	if ctx.Repo.IsViewCommit {
		nf.Version = base.ShortSha(ctx.Repo.CommitID)
	}
	ctx.Data["AscNotFound"] = nf

	mainCommit, err := ctx.Repo.GitRepo.GetBranchCommit(main)
	if err != nil {
		log.Debug("not found at this version: the default branch of %s: %v", repo.FullName(), err)
		return
	}
	if _, err := mainCommit.GetTreeEntryByPath(treePath); err == nil {
		nf.OnMain = true
		nf.MainLink += "/" + util.PathEscapeSegments(treePath)
	} else if !git.IsErrNotExist(err) {
		log.Debug("not found at this version: %s on the default branch of %s: %v", treePath, repo.FullName(), err)
		return
	}
	// the history of one path on one head does not change: read once per head
	key := fmt.Sprintf("asc-path-changes:%d:%s:%s", repo.ID, mainCommit.ID.String(), treePath)
	joined, err := cache.GetString(key, func() (string, error) {
		newest, oldest, err := ascPathChanges(ctx.Repo.GitRepo, mainCommit.ID.String(), treePath)
		if err != nil {
			log.Debug("not found at this version: the history of %s in %s: %v", treePath, repo.FullName(), err)
		}
		return newest + " " + oldest, nil
	})
	if err != nil {
		return
	}
	newest, oldest, _ := strings.Cut(joined, " ")
	if newest == "" {
		return
	}
	change := newest // the change that removed it
	if nf.OnMain {
		change = oldest // the change that added it
	}
	nf.Change = base.ShortSha(change)
	nf.ChangeLink = ctx.Repo.RepoLink + "/commit/" + change
	nf.HistoryLink = ctx.Repo.RepoLink + "/commits/branch/" + util.PathEscapeSegments(main) + "/" + util.PathEscapeSegments(treePath)
}
