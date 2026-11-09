// Copyright 2020 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package stats

import (
	"context"
	"fmt"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/graceful"
	"forgejo.org/modules/log"
	"forgejo.org/modules/process"
	"forgejo.org/modules/setting"
)

// DBIndexer implements Indexer interface to use database's like search
type DBIndexer struct{}

// Index repository status function
func (db *DBIndexer) Index(id int64) error {
	ctx, _, finished := process.GetManager().AddContext(graceful.GetManager().ShutdownContext(), fmt.Sprintf("Stats.DB Index Repo[%d]", id))
	defer finished()

	repo, err := repo_model.GetRepositoryByID(ctx, id)
	if err != nil {
		return err
	}
	if repo.IsEmpty {
		return nil
	}

	status, err := repo_model.GetIndexerStatus(ctx, repo, repo_model.RepoIndexerTypeStats)
	if err != nil {
		return err
	}

	gitRepo, err := gitrepo.OpenRepository(ctx, repo)
	if err != nil {
		if err.Error() == "no such file or directory" {
			return nil
		}
		return err
	}
	defer gitRepo.Close()

	// Get latest commit for default branch
	commitID, err := gitRepo.GetBranchCommitID(repo.DefaultBranch)
	if err != nil {
		if git.IsErrBranchNotExist(err) || git.IsErrNotExist(err) || setting.IsInTesting {
			log.Debug("Unable to get commit ID for default branch %s in %s ... skipping this repository", repo.DefaultBranch, repo.RepoPath())
			return nil
		}
		log.Error("Unable to get commit ID for default branch %s in %s. Error: %v", repo.DefaultBranch, repo.RepoPath(), err)
		return err
	}

	if err := indexChangesets(ctx, repo, gitRepo, commitID); err != nil {
		log.Error("Unable to count the changesets of %s at %s: %v", repo.FullName(), commitID, err)
		return err
	}

	// Do not recalculate stats if already calculated for this commit
	if status.CommitSha == commitID {
		return nil
	}

	// Calculate and save language statistics to database
	stats, err := gitRepo.GetLanguageStats(commitID)
	if err != nil {
		if !setting.IsInTesting {
			log.Error("Unable to get language stats for ID %s for default branch %s in %s. Error: %v", commitID, repo.DefaultBranch, repo.RepoPath(), err)
		}
		return err
	}
	err = repo_model.UpdateLanguageStats(ctx, repo, commitID, stats)
	if err != nil {
		log.Error("Unable to update language stats for ID %s for default branch %s in %s. Error: %v", commitID, repo.DefaultBranch, repo.RepoPath(), err)
		return err
	}

	log.Debug("DBIndexer completed language stats for ID %s for default branch %s in %s. stats count: %d", commitID, repo.DefaultBranch, repo.RepoPath(), len(stats))
	return nil
}

// Close dummy function
func (db *DBIndexer) Close() {
}

// indexChangesets stores how many changesets (commits) the head of the default branch reaches, the count of the
// repository rows. It counts only when the stored count is of another head.
func indexChangesets(ctx context.Context, repo *repo_model.Repository, gitRepo *git.Repository, commitID string) error {
	stored, err := repo_model.GetAscRepoChangesets(ctx, []int64{repo.ID})
	if err != nil {
		return err
	}
	if r := stored[repo.ID]; r != nil && r.CommitSha == commitID {
		return nil
	}
	commit, err := gitRepo.GetCommit(commitID)
	if err != nil {
		return err
	}
	n, err := commit.CommitsCount()
	if err != nil {
		return err
	}
	return repo_model.SetAscRepoChangesets(ctx, repo.ID, commitID, n)
}
