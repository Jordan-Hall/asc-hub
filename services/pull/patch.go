// Copyright 2019 The Gitea Authors.
// All rights reserved.
// SPDX-License-Identifier: MIT

package pull

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"forgejo.org/models"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/graceful"
	"forgejo.org/modules/log"
	"forgejo.org/modules/process"
	"forgejo.org/modules/repostore"

	"github.com/gobwas/glob"
)

// DownloadDiffOrPatch will write the patch for the pr to the writer
func DownloadDiffOrPatch(ctx context.Context, pr *issues_model.PullRequest, w io.Writer, patch, binary bool) error {
	if err := pr.LoadBaseRepo(ctx); err != nil {
		log.Error("Unable to load base repository ID %d for pr #%d [%d]", pr.BaseRepoID, pr.Index, pr.ID)
		return err
	}

	gitRepo, closer, err := gitrepo.RepositoryFromContextOrOpen(ctx, pr.BaseRepo)
	if err != nil {
		return fmt.Errorf("OpenRepository: %w", err)
	}
	defer closer.Close()

	if err := gitRepo.GetDiffOrPatch(pr.MergeBase, pr.GetGitRefName(), w, patch, binary); err != nil {
		log.Error("Unable to get patch file from %s to %s in %s Error: %v", pr.MergeBase, pr.HeadBranch, pr.BaseRepo.FullName(), err)
		return fmt.Errorf("Unable to get patch file from %s to %s in %s Error: %w", pr.MergeBase, pr.HeadBranch, pr.BaseRepo.FullName(), err)
	}
	return nil
}

// TestPatch will test whether a simple patch will apply
func TestPatch(pr *issues_model.PullRequest) error {
	ctx, _, finished := process.GetManager().AddContext(graceful.GetManager().HammerContext(), fmt.Sprintf("TestPatch: %s", pr))
	defer finished()

	_, err := testPatch(ctx, pr, false)
	return err
}

type testPatchContext struct {
	headRev        string
	headIsCommitID bool
	baseRev        string
	env            []string
	gitRepo        *git.Repository
	close          func()
}

// LoadHeadRevision loads the necessary information to access the head revision.
func (t *testPatchContext) LoadHeadRevision(ctx context.Context, pr *issues_model.PullRequest) error {
	// If AGit, then use HeadCommitID if set (AGit flow creates pull request),
	// otherwise use the pull request reference.
	if pr.Flow == issues_model.PullRequestFlowAGit {
		if len(pr.HeadCommitID) > 0 {
			t.headRev = pr.HeadCommitID
			t.headIsCommitID = true
			return nil
		}
		t.headRev = pr.GetGitRefName()
		return nil
	}

	// If it is within the same repository, simply return the branch name.
	if pr.BaseRepoID == pr.HeadRepoID {
		t.headRev = pr.GetGitHeadBranchRefName()
		return nil
	}

	// We are in Github flow, head and base repository are different.
	// Resolve the head branch to a commitID and return a Git alternate
	// environment for the head repository.
	gitRepo, err := gitrepo.OpenRepository(ctx, pr.HeadRepo)
	if err != nil {
		return err
	}
	defer gitRepo.Close()

	headCommitID, err := gitRepo.GetRefCommitID(pr.GetGitHeadBranchRefName())
	if err != nil {
		return err
	}

	t.headRev = headCommitID
	t.headIsCommitID = true
	t.env = append(os.Environ(), `GIT_ALTERNATE_OBJECT_DIRECTORIES=`+pr.HeadRepo.RepoPath()+"/objects")
	return nil
}

// getTestPatchCtx constructs a new testpatch context for the given pull request. It reads from the
// base repository, which does not contain a working tree, and reaches the objects of the head
// repository through git alternates.
func getTestPatchCtx(ctx context.Context, pr *issues_model.PullRequest) (*testPatchContext, error) {
	testPatchCtx := &testPatchContext{
		close: func() {},
	}

	if err := pr.LoadBaseRepo(ctx); err != nil {
		return testPatchCtx, fmt.Errorf("LoadBaseRepo: %w", err)
	}
	if err := pr.LoadHeadRepo(ctx); err != nil {
		return testPatchCtx, fmt.Errorf("LoadHeadRepo: %w", err)
	}

	if err := testPatchCtx.LoadHeadRevision(ctx, pr); err != nil {
		return testPatchCtx, fmt.Errorf("LoadHeadRevision: %w", err)
	}

	gitRepo, err := gitrepo.OpenRepository(ctx, pr.BaseRepo)
	if err != nil {
		return testPatchCtx, fmt.Errorf("OpenRepository: %w", err)
	}

	testPatchCtx.baseRev = git.BranchPrefix + pr.BaseBranch
	testPatchCtx.gitRepo = gitRepo
	testPatchCtx.close = func() {
		gitRepo.Close()
	}
	return testPatchCtx, nil
}

// testPatch checks whether the head of the pull request merges into the base, and records the outcome in the
// pull request: the merge base, the head commit, the status, the conflicted files and the protected files that
// changed. With wantDivergence the result also has the number of commits the head is ahead and behind.
func testPatch(ctx context.Context, pr *issues_model.PullRequest, wantDivergence bool) (*repostore.MergeResult, error) {
	writer, err := repostore.Default()
	if err != nil {
		return nil, err
	}
	req, err := newMergeRequest(ctx, pr)
	if err != nil {
		return nil, fmt.Errorf("newMergeRequest: %w", err)
	}
	req.DryRun = true
	req.WantDivergence = wantDivergence

	pb, err := git_model.GetFirstMatchProtectedBranchRule(ctx, pr.BaseRepoID, pr.BaseBranch)
	if err != nil {
		return nil, fmt.Errorf("checkPullFilesProtection: %v", err)
	}
	if pb != nil {
		for pattern := range strings.SplitSeq(pb.ProtectedFilePatterns, ";") {
			if pattern = strings.TrimSpace(pattern); pattern != "" {
				req.ProtectedGlobs = append(req.ProtectedGlobs, strings.ToLower(pattern))
			}
		}
	}

	result, err := writer.Merge(ctx, *req)
	if err != nil {
		return nil, fmt.Errorf("testPatch: %w", mapMergeError(err, pr))
	}

	pr.MergeBase = result.MergeBase.String()
	pr.HeadCommitID = result.Head.String()

	// If the head commit is equal to the merge base it roughly means that the
	// head commit is a parent of the base commit.
	if result.Status == repostore.DryRunAncestor {
		pr.Status = issues_model.PullRequestStatusAncestor
		return result, nil
	}

	// Resets the conflict status.
	pr.ConflictedFiles = nil
	switch result.Status {
	case repostore.DryRunConflict:
		pr.Status = issues_model.PullRequestStatusConflict
		pr.ConflictedFiles = result.Conflicts
		log.Trace("Found %d files conflicted: %v", len(pr.ConflictedFiles), pr.ConflictedFiles)
	case repostore.DryRunEmpty:
		log.Debug("PullRequest[%d]: Patch is empty - ignoring", pr.ID)
		pr.Status = issues_model.PullRequestStatusEmpty
	case repostore.DryRunMergeable:
		pr.ChangedProtectedFiles = result.ProtectedFiles
		if len(pr.ChangedProtectedFiles) > 0 {
			log.Trace("Found %d protected files changed", len(pr.ChangedProtectedFiles))
		}
		pr.Status = issues_model.PullRequestStatusMergeable
	default:
		return nil, fmt.Errorf("testPatch: unexpected status %d", result.Status)
	}
	return result, nil
}

// CheckFileProtection check file Protection
func CheckFileProtection(repo *git.Repository, oldCommitID, newCommitID string, patterns []glob.Glob, limit int, env []string) ([]string, error) {
	if len(patterns) == 0 {
		return nil, nil
	}
	affectedFiles, err := git.GetAffectedFiles(repo, oldCommitID, newCommitID, env)
	if err != nil {
		return nil, err
	}
	changedProtectedFiles := make([]string, 0, limit)
	for _, affectedFile := range affectedFiles {
		lpath := strings.ToLower(affectedFile)
		for _, pat := range patterns {
			if pat.Match(lpath) {
				changedProtectedFiles = append(changedProtectedFiles, lpath)
				break
			}
		}
		if len(changedProtectedFiles) >= limit {
			break
		}
	}
	if len(changedProtectedFiles) > 0 {
		err = models.ErrFilePathProtected{
			Path: changedProtectedFiles[0],
		}
	}
	return changedProtectedFiles, err
}

// CheckUnprotectedFiles check if the commit only touches unprotected files
func CheckUnprotectedFiles(repo *git.Repository, oldCommitID, newCommitID string, patterns []glob.Glob, env []string) (bool, error) {
	if len(patterns) == 0 {
		return false, nil
	}
	affectedFiles, err := git.GetAffectedFiles(repo, oldCommitID, newCommitID, env)
	if err != nil {
		return false, err
	}
	for _, affectedFile := range affectedFiles {
		lpath := strings.ToLower(affectedFile)
		unprotected := false
		for _, pat := range patterns {
			if pat.Match(lpath) {
				unprotected = true
				break
			}
		}
		if !unprotected {
			return false, nil
		}
	}
	return true, nil
}
