// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"errors"
	"fmt"
	"strings"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	pull_service "forgejo.org/services/pull"

	"google.golang.org/grpc"
)

// A proposed policy change is an ordinary change under review (docs/BUILDOUT.md section 1): asc made it on top of
// trunk, and the hub opens the pull request that carries it, the same way a pushed `asc submit` does (AGit flow,
// one pull request per change, found by repository and change). A maintainer reviews and lands it like any other.

// ErrNotMirrored is returned when the change exists in asc but its commit is not in the git view of the
// repository yet, so no pull request can be opened for it. The change is not lost: it is under review in asc, and
// the page says so and gives its id.
var ErrNotMirrored = errors.New("asctrust: the change is not in the git view of the repository yet")

// PullSeams are the parts of OpenProposalPull that need a database, a git repository or a server. Tests replace
// them; production uses the defaults.
type PullSeams struct {
	// GitCommit returns the git commit (40 hex digits) of a changeset, or "" when asc has none.
	GitCommit func(ctx context.Context, conn grpc.ClientConnInterface, repoName string, changeset []byte) (string, error)
	// HasCommit reports whether the git view of the repository holds commit.
	HasCommit func(ctx context.Context, repo *repo_model.Repository, commit string) (bool, error)
	// FindOpen returns the open pull request of change, or nil.
	FindOpen func(ctx context.Context, repo *repo_model.Repository, change string) (*issues_model.PullRequest, error)
	// Create opens the pull request and links it to the change.
	Create func(ctx context.Context, repo *repo_model.Repository, doer *user_model.User, change, commit, title, body string) (*issues_model.PullRequest, error)
}

// DefaultPullSeams are the seams of production.
var DefaultPullSeams = PullSeams{
	GitCommit: gitCommitOf,
	HasCommit: hasCommit,
	FindOpen:  findOpen,
	Create:    createPull,
}

// OpenProposalPull returns the pull request of a proposed change, opening it when there is none. It fails with
// ErrNotMirrored when the git view lacks the commit.
func OpenProposalPull(ctx context.Context, conn grpc.ClientConnInterface, repo *repo_model.Repository, repoName string, doer *user_model.User, p *Proposal, title string, seams PullSeams) (*issues_model.PullRequest, error) {
	if doer == nil || p == nil || p.Change == "" {
		return nil, ErrBadInput
	}
	if !issues_model.IsAscChange(p.Change) {
		return nil, fmt.Errorf("%w: change id", ErrBadAnswer)
	}
	if pr, err := seams.FindOpen(ctx, repo, p.Change); err != nil {
		return nil, err
	} else if pr != nil {
		return pr, nil
	}
	commit, err := seams.GitCommit(ctx, conn, repoName, p.Changeset)
	if err != nil {
		return nil, err
	}
	if commit == "" {
		return nil, ErrNotMirrored
	}
	ok, err := seams.HasCommit(ctx, repo, commit)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotMirrored
	}
	title = firstLine(title)
	if title == "" {
		title = "policy: update " + policyPath
	}
	body := "This change proposes an update of `" + policyPath + "`. It is an ordinary change under review: a maintainer reads the diff and lands it, or asks for another version. Nothing changes on trunk until then."
	return seams.Create(ctx, repo, doer, p.Change, commit, title, body)
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return CleanText(s, 200)
}

func gitCommitOf(ctx context.Context, conn grpc.ClientConnInterface, repoName string, changeset []byte) (string, error) {
	if len(changeset) != 32 {
		return "", ErrBadInput
	}
	cctx, cancel := SystemContext(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewRepoClient(conn).GitMap(cctx, &ascv1.GitMapRequest{Repo: RepoRef(repoName), Asc: [][]byte{changeset}})
	if err != nil {
		return "", err
	}
	got := resp.GetAscToGit()
	if len(got) != 1 || (len(got[0]) != 0 && len(got[0]) != 20) {
		return "", fmt.Errorf("%w: git map", ErrBadAnswer)
	}
	if len(got[0]) == 0 {
		return "", nil
	}
	return hexPrefix(got[0], 40), nil
}

func hasCommit(ctx context.Context, repo *repo_model.Repository, commit string) (bool, error) {
	gitRepo, err := gitrepo.OpenRepository(ctx, repo)
	if err != nil {
		return false, err
	}
	defer gitRepo.Close()
	if _, err := gitRepo.GetCommit(commit); err != nil {
		return false, nil
	}
	return true, nil
}

func findOpen(ctx context.Context, repo *repo_model.Repository, change string) (*issues_model.PullRequest, error) {
	pr, err := issues_model.GetOpenPullRequestByAscChange(ctx, repo.ID, change)
	if err != nil {
		if issues_model.IsErrPullRequestNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return pr, nil
}

func createPull(ctx context.Context, repo *repo_model.Repository, doer *user_model.User, change, commit, title, body string) (*issues_model.PullRequest, error) {
	issue := &issues_model.Issue{
		RepoID:   repo.ID,
		Title:    title,
		PosterID: doer.ID,
		Poster:   doer,
		IsPull:   true,
		Content:  body,
	}
	pr := &issues_model.PullRequest{
		HeadRepoID:   repo.ID,
		BaseRepoID:   repo.ID,
		HeadBranch:   strings.ToLower(doer.Name) + "/asc-" + change,
		HeadCommitID: commit,
		BaseBranch:   repo.DefaultBranch,
		HeadRepo:     repo,
		BaseRepo:     repo,
		Type:         issues_model.PullRequestGitea,
		Flow:         issues_model.PullRequestFlowAGit,
	}
	if err := pull_service.NewPullRequest(ctx, repo, issue, nil, nil, pr, nil); err != nil {
		return nil, fmt.Errorf("asctrust: open the pull request: %w", err)
	}
	// The change is the bottom of its own stack: it sits on trunk.
	if err := issues_model.SetPullRequestAscStack(ctx, pr, change, 0); err != nil {
		return nil, fmt.Errorf("asctrust: link the pull request to its change: %w", err)
	}
	if err := pull_service.RecomputeAscStackBase(ctx, pr); err != nil {
		return nil, fmt.Errorf("asctrust: stack base: %w", err)
	}
	return pr, nil
}
