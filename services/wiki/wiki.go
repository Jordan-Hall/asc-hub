// Copyright 2015 The Gogs Authors. All rights reserved.
// Copyright 2019 The Gitea Authors. All rights reserved.
// Copyright 2024 The Forgejo Authors c/o Codeberg e.V.. All rights reserved.
// SPDX-License-Identifier: MIT

package wiki

import (
	"context"
	"fmt"
	"os"
	"strings"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	repo_module "forgejo.org/modules/repository"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/sync"
	asymkey_service "forgejo.org/services/asymkey"
	repo_service "forgejo.org/services/repository"
)

// TODO: use clustered lock (unique queue? or *abuse* cache)
var wikiWorkingPool = sync.NewExclusivePool()

const (
	DefaultRemote = "origin"
)

// InitWiki initializes a wiki for repository,
// it does nothing when repository already has wiki.
func InitWiki(ctx context.Context, repo *repo_model.Repository) error {
	if repo.HasWiki() {
		return nil
	}

	writer, err := repostore.Default()
	if err != nil {
		return err
	}
	if err := writer.Init(ctx, repostore.InitRequest{Repo: repo.WikiStoreRef(), DefaultBranch: repo.GetWikiBranchName()}); err != nil {
		return fmt.Errorf("InitRepository: %w", err)
	}
	return nil
}

// NormalizeWikiBranch renames a repository wiki's branch to `setting.Repository.DefaultBranch`
func NormalizeWikiBranch(ctx context.Context, repo *repo_model.Repository, to string) error {
	from := repo.GetWikiBranchName()

	if err := repo.MustNotBeArchived(); err != nil {
		return err
	}

	updateDB := func() error {
		repo.WikiBranch = to
		return repo_model.UpdateRepositoryCols(ctx, repo, "wiki_branch")
	}

	if !repo.HasWiki() {
		return updateDB()
	}

	if from == to {
		return nil
	}

	gitRepo, err := gitrepo.OpenWikiRepository(ctx, repo)
	if err != nil {
		return err
	}
	defer gitRepo.Close()

	if gitRepo.IsBranchExist(to) {
		return nil
	}

	if !gitRepo.IsBranchExist(from) {
		return nil
	}

	if err := gitRepo.RenameBranch(from, to); err != nil {
		return err
	}

	if err := gitrepo.SetDefaultBranch(ctx, repo, to); err != nil {
		return err
	}

	return updateDB()
}

// prepareGitPath try to find a suitable file path with file name by the given raw wiki name.
// return: existence, prepared file path with name, error
func prepareGitPath(gitRepo *git.Repository, branch string, wikiPath WebPath) (bool, string, error) {
	unescaped := string(wikiPath) + ".md"
	gitPath := WebPathToGitPath(wikiPath)

	// Look for both files
	filesInIndex, err := gitRepo.LsTree(branch, unescaped, gitPath)
	if err != nil {
		if strings.Contains(err.Error(), "Not a valid object name "+branch) {
			return false, gitPath, nil
		}
		log.Error("%v", err)
		return false, gitPath, err
	}

	foundEscaped := false
	for _, filename := range filesInIndex {
		switch filename {
		case unescaped:
			// if we find the unescaped file return it
			return true, unescaped, nil
		case gitPath:
			foundEscaped = true
		}
	}

	// If not return whether the escaped file exists, and the escaped filename to keep backwards compatibility.
	return foundEscaped, gitPath, nil
}

// updateWikiPage adds a new page or edits an existing page in repository wiki.
func updateWikiPage(ctx context.Context, doer *user_model.User, repo *repo_model.Repository, oldWikiName, newWikiName WebPath, content, message string, isNew bool) (err error) {
	err = repo.MustNotBeArchived()
	if err != nil {
		return err
	}

	if err = validateWebPath(newWikiName); err != nil {
		return err
	}
	wikiWorkingPool.CheckIn(fmt.Sprint(repo.ID))
	defer wikiWorkingPool.CheckOut(fmt.Sprint(repo.ID))

	if err = InitWiki(ctx, repo); err != nil {
		return fmt.Errorf("InitWiki: %w", err)
	}

	writer, err := repostore.Default()
	if err != nil {
		return err
	}
	gitRepo, err := gitrepo.OpenWikiRepository(ctx, repo)
	if err != nil {
		log.Error("Unable to open wiki repository: %s (%v)", repo.FullName(), err)
		return fmt.Errorf("failed to open wiki repository: %s %w", repo.FullName(), err)
	}
	defer gitRepo.Close()

	var parent git.ObjectID
	if gitRepo.IsBranchExist(repo.GetWikiBranchName()) {
		commit, err := gitRepo.GetBranchCommit(repo.GetWikiBranchName())
		if err != nil {
			return err
		}
		parent = commit.ID
	}

	isWikiExist, newWikiPath, err := prepareGitPath(gitRepo, repo.GetWikiBranchName(), newWikiName)
	if err != nil {
		return err
	}

	var ops []repostore.FileOp
	if isNew {
		if isWikiExist {
			return repo_model.ErrWikiAlreadyExist{
				Title: newWikiPath,
			}
		}
	} else {
		// avoid check existence again if wiki name is not changed since gitRepo.LsFiles(...) is not free.
		isOldWikiExist := true
		oldWikiPath := newWikiPath
		if oldWikiName != newWikiName {
			isOldWikiExist, oldWikiPath, err = prepareGitPath(gitRepo, repo.GetWikiBranchName(), oldWikiName)
			if err != nil {
				return err
			}
		}

		if isOldWikiExist {
			ops = append(ops, repostore.FileOp{Kind: repostore.OpDelete, Path: oldWikiPath})
		}
	}

	// FIXME: The wiki doesn't have lfs support at present - if this changes need to check attributes here
	ops = append(ops, repostore.FileOp{Kind: repostore.OpPut, Path: newWikiPath, Content: strings.NewReader(content)})

	if _, err := writer.CreateCommit(ctx, wikiCommitRequest(ctx, doer, repo, parent, ops, message)); err != nil {
		log.Error("Push failed: %v", err)
		if git.IsErrPushOutOfDate(err) || git.IsErrPushRejected(err) {
			return err
		}
		return fmt.Errorf("failed to push: %w", err)
	}

	return nil
}

// wikiCommitRequest describes a commit on the wiki branch by a user.
func wikiCommitRequest(_ context.Context, doer *user_model.User, repo *repo_model.Repository, parent git.ObjectID, ops []repostore.FileOp, message string) repostore.CommitRequest {
	return repostore.CommitRequest{
		Repo:   repo.WikiStoreRef(),
		Parent: parent,
		Ops:    ops,
		Meta: repostore.CommitMeta{
			Author:    doer.NewGitSig(),
			Committer: doer.NewGitSig(),
			Message:   message,
			Sign: func(ctx context.Context, _ repostore.SignInput) (*repostore.SignDecision, error) {
				// An error means the commit is not signed: the reasons not to sign are errors (asymkey_service.ErrWontSign).
				sign, signingKey, signer, _ := asymkey_service.SignWikiCommit(ctx, repo, doer)
				if !sign {
					return nil, nil
				}
				decision := &repostore.SignDecision{KeyID: signingKey}
				if trust := repo.GetTrustModel(); trust == repo_model.CommitterTrustModel || trust == repo_model.CollaboratorCommitterTrustModel {
					decision.Committer = signer
				}
				return decision, nil
			},
		},
		Update: &repostore.RefUpdate{Repo: repo.WikiStoreRef(), Ref: git.BranchPrefix + repo.GetWikiBranchName()},
		HookEnv: repo_module.FullPushingEnvironment(
			doer,
			doer,
			repo,
			repo.Name+".wiki",
			0,
		),
	}
}

// AddWikiPage adds a new wiki page with a given wikiPath.
func AddWikiPage(ctx context.Context, doer *user_model.User, repo *repo_model.Repository, wikiName WebPath, content, message string) error {
	return updateWikiPage(ctx, doer, repo, "", wikiName, content, message, true)
}

// EditWikiPage updates a wiki page identified by its wikiPath,
// optionally also changing wikiPath.
func EditWikiPage(ctx context.Context, doer *user_model.User, repo *repo_model.Repository, oldWikiName, newWikiName WebPath, content, message string) error {
	return updateWikiPage(ctx, doer, repo, oldWikiName, newWikiName, content, message, false)
}

// DeleteWikiPage deletes a wiki page identified by its path.
func DeleteWikiPage(ctx context.Context, doer *user_model.User, repo *repo_model.Repository, wikiName WebPath) (err error) {
	err = repo.MustNotBeArchived()
	if err != nil {
		return err
	}

	wikiWorkingPool.CheckIn(fmt.Sprint(repo.ID))
	defer wikiWorkingPool.CheckOut(fmt.Sprint(repo.ID))

	if err = InitWiki(ctx, repo); err != nil {
		return fmt.Errorf("InitWiki: %w", err)
	}

	writer, err := repostore.Default()
	if err != nil {
		return err
	}
	gitRepo, err := gitrepo.OpenWikiRepository(ctx, repo)
	if err != nil {
		log.Error("Unable to open wiki repository: %s (%v)", repo.FullName(), err)
		return fmt.Errorf("failed to open wiki repository: %s %w", repo.FullName(), err)
	}
	defer gitRepo.Close()

	if !gitRepo.IsBranchExist(repo.GetWikiBranchName()) {
		return git.ErrBranchNotExist{Name: repo.GetWikiBranchName()}
	}
	commit, err := gitRepo.GetBranchCommit(repo.GetWikiBranchName())
	if err != nil {
		return err
	}

	found, wikiPath, err := prepareGitPath(gitRepo, repo.GetWikiBranchName(), wikiName)
	if err != nil {
		return err
	}
	if !found {
		return os.ErrNotExist
	}

	// FIXME: The wiki doesn't have lfs support at present - if this changes need to check attributes here
	ops := []repostore.FileOp{{Kind: repostore.OpDelete, Path: wikiPath}}
	if _, err := writer.CreateCommit(ctx, wikiCommitRequest(ctx, doer, repo, commit.ID, ops, fmt.Sprintf("Delete page %q", wikiName))); err != nil {
		if git.IsErrPushOutOfDate(err) || git.IsErrPushRejected(err) {
			return err
		}
		return fmt.Errorf("Push: %w", err)
	}

	return nil
}

// DeleteWiki removes the actual and local copy of repository wiki.
func DeleteWiki(ctx context.Context, repo *repo_model.Repository) error {
	if err := repo_service.UpdateRepositoryUnits(ctx, repo, nil, []unit.Type{unit.TypeWiki}); err != nil {
		return err
	}

	repo_service.RemoveFilesWithNotice(ctx, "Delete repository wiki", repo.WikiStoreRef())
	return nil
}

type SearchContentsResult struct {
	*git.GrepResult
	Title string
}

func SearchWikiContents(ctx context.Context, repo *repo_model.Repository, keyword string) ([]SearchContentsResult, error) {
	gitRepo, err := gitrepo.OpenWikiRepository(ctx, repo)
	if err != nil {
		return nil, err
	}
	defer gitRepo.Close()

	grepRes, err := git.GrepSearch(ctx, gitRepo, keyword, git.GrepOptions{
		ContextLineNumber: 0,
		Mode:              git.FixedAnyGrepMode,
		RefName:           repo.GetWikiBranchName(),
		MaxResultLimit:    10,
		MatchesPerFile:    3,
	})
	if err != nil {
		return nil, err
	}

	res := make([]SearchContentsResult, 0, len(grepRes))
	for _, entry := range grepRes {
		wp, err := GitPathToWebPath(entry.Filename)
		if err != nil {
			return nil, err
		}
		_, title := WebPathToUserTitle(wp)

		res = append(res, SearchContentsResult{
			GrepResult: entry,
			Title:      title,
		})
	}

	return res, nil
}
