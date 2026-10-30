// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitDir is a repository with a line of commits made for a test.
type gitDir struct {
	t    *testing.T
	dir  string
	repo *git.Repository
	ids  []string // oldest first; ids[0] is the trunk
}

func (g *gitDir) run(args ...string) string {
	g.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = g.dir
	cmd.Env = append(cmd.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	require.NoError(g.t, err, string(out))
	return strings.TrimSpace(string(out))
}

func commitChain(t *testing.T, n int) *gitDir {
	t.Helper()
	g := &gitDir{t: t, dir: t.TempDir()}
	g.run("init", "-q", "-b", "main")
	for i := range n {
		g.run("commit", "-q", "--allow-empty", "-m", "change "+string(rune('a'+i)))
		g.ids = append(g.ids, g.run("rev-parse", "HEAD"))
	}
	repo, err := git.OpenRepository(t.Context(), g.dir)
	require.NoError(t, err)
	t.Cleanup(func() { repo.Close() })
	g.repo = repo
	return g
}

// stackedPull makes an open pull request of the repository 1 that is a change of a stack, by poster.
func stackedPull(t *testing.T, index, poster int64, change string) *issues_model.PullRequest {
	t.Helper()
	ctx := t.Context()
	issue := &issues_model.Issue{RepoID: 1, Index: index, PosterID: poster, Title: fmt.Sprintf("change %d", index), IsPull: true}
	require.NoError(t, db.Insert(ctx, issue))
	pr := &issues_model.PullRequest{IssueID: issue.ID, Index: index, BaseRepoID: 1, HeadRepoID: 1, BaseBranch: "master", HeadBranch: fmt.Sprintf("topic-%d", index), Type: issues_model.PullRequestGitea, Flow: issues_model.PullRequestFlowAGit}
	require.NoError(t, db.Insert(ctx, pr))
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, pr, strings.Repeat(change, 32), 0))
	return pr
}

func parentOf(t *testing.T, pr *issues_model.PullRequest) int64 {
	t.Helper()
	row, err := issues_model.GetPullRequestAscStack(t.Context(), pr.ID)
	require.NoError(t, err)
	return row.StackParentID
}

func stubParentChange(t *testing.T, f func(context.Context, *repo_model.Repository, string) (string, error)) {
	t.Helper()
	prev := AscParentChange
	AscParentChange = f
	t.Cleanup(func() { AscParentChange = prev })
}

func TestLinkAscStackPushLinksTheParentsOfABatchInAnyOrder(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	stubFirstParent(t, ascTestFirst, nil)
	g := commitChain(t, 4) // ids[0] is the trunk; the stack is ids[1] <- ids[2] <- ids[3]
	bottom, middle, top := stackedPull(t, 101, 2, "k"), stackedPull(t, 102, 2, "l"), stackedPull(t, 103, 2, "m")
	heads := map[int64]string{top.ID: g.ids[3], bottom.ID: g.ids[1], middle.ID: g.ids[2]}

	n, err := LinkAscStackPush(ctx, g.repo, heads)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Zero(t, parentOf(t, bottom), "the bottom sits on the trunk")
	assert.Equal(t, bottom.ID, parentOf(t, middle))
	assert.Equal(t, middle.ID, parentOf(t, top))

	// a submit again changes nothing
	n, err = LinkAscStackPush(ctx, g.repo, heads)
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestLinkAscStackPushNeverReplacesAParent(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	stubFirstParent(t, ascTestFirst, nil)
	g := commitChain(t, 3)
	bottom, middle, top := stackedPull(t, 111, 2, "k"), stackedPull(t, 112, 2, "l"), stackedPull(t, 113, 2, "m")
	// asc recorded the top on the bottom; git would say the middle
	require.NoError(t, issues_model.SetAscStackParent(ctx, top.ID, bottom.ID))
	_, err := LinkAscStackPush(ctx, g.repo, map[int64]string{bottom.ID: g.ids[0], middle.ID: g.ids[1], top.ID: g.ids[2]})
	require.NoError(t, err)
	assert.Equal(t, bottom.ID, parentOf(t, top), "a recorded parent stays")
	assert.Equal(t, bottom.ID, parentOf(t, middle), "a missing one is filled")
}

func TestLinkAscStackPushOnlyLinksThePullRequestsOfThePush(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	stubFirstParent(t, ascTestFirst, nil)
	g := commitChain(t, 3)
	bottom, other := stackedPull(t, 121, 2, "k"), stackedPull(t, 122, 2, "l")
	// other's head is on bottom in git, but the push moved only bottom
	n, err := LinkAscStackPush(t.Context(), g.repo, map[int64]string{bottom.ID: g.ids[1]})
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Zero(t, parentOf(t, other))
	g.run("update-ref", other.GetGitRefName(), g.ids[2])
}

func TestAscGitParentOfAnotherPersonNeedsAscToConfirm(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	stubFirstParent(t, ascTestFirst, nil)
	g := commitChain(t, 3)
	// user 4 made a pull request whose head is the git parent of user 2's change
	theirs := stackedPull(t, 131, 4, "k")
	mine := stackedPull(t, 132, 2, "l")
	heads := map[int64]string{theirs.ID: g.ids[1], mine.ID: g.ids[2]}

	stubParentChange(t, nil)
	n, err := LinkAscStackPush(ctx, g.repo, heads)
	require.NoError(t, err)
	assert.Zero(t, n, "git alone does not make a stranger's pull request the parent")
	assert.Zero(t, parentOf(t, mine))

	stubParentChange(t, func(context.Context, *repo_model.Repository, string) (string, error) {
		return strings.Repeat("z", 32), nil
	})
	n, err = LinkAscStackPush(ctx, g.repo, heads)
	require.NoError(t, err)
	assert.Zero(t, n, "asc names another change as the parent")

	stubParentChange(t, func(context.Context, *repo_model.Repository, string) (string, error) {
		return strings.Repeat("k", 32), nil
	})
	n, err = LinkAscStackPush(ctx, g.repo, heads)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "asc confirms it")
	assert.Equal(t, theirs.ID, parentOf(t, mine))
}

func TestAscGitParentIsNotGuessedWhenTwoPullRequestsHaveTheHead(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	stubFirstParent(t, ascTestFirst, nil)
	g := commitChain(t, 3)
	a, b, child := stackedPull(t, 141, 2, "k"), stackedPull(t, 142, 2, "l"), stackedPull(t, 143, 2, "m")
	g.run("update-ref", a.GetGitRefName(), g.ids[1])
	g.run("update-ref", b.GetGitRefName(), g.ids[1])
	parent, err := AscGitParent(ctx, g.repo, child, g.ids[2], nil)
	require.NoError(t, err)
	assert.Nil(t, parent, "two open pull requests on the commit: ambiguous")

	// once there is only one, it is the parent
	g.run("update-ref", "-d", b.GetGitRefName())
	parent, err = AscGitParent(ctx, g.repo, child, g.ids[2], nil)
	require.NoError(t, err)
	require.NotNil(t, parent)
	assert.Equal(t, a.ID, parent.ID)
}

func TestAscGitParentLooksAtBoundedRefs(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	stubFirstParent(t, ascTestFirst, nil)
	g := commitChain(t, 2)
	child := stackedPull(t, 151, 2, "k")
	// many pull request refs on one commit: the look stops and answers nobody
	for i := range ascParentScan + 5 {
		g.run("update-ref", fmt.Sprintf("refs/pull/%d/head", 1000+i), g.ids[0])
	}
	parent, err := AscGitParent(t.Context(), g.repo, child, g.ids[1], nil)
	require.NoError(t, err)
	assert.Nil(t, parent)
}

func TestAscStackPrefixDerivesTheParentWithoutWriting(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	g := commitChain(t, 3)
	bottom, top := stackedPull(t, 161, 2, "k"), stackedPull(t, 162, 2, "l")
	g.run("update-ref", bottom.GetGitRefName(), g.ids[1])
	g.run("update-ref", top.GetGitRefName(), g.ids[2])
	prevOpener := ascBridgeOpener
	ascBridgeOpener = func(ctx context.Context, _ *repo_model.Repository) (*git.Repository, error) {
		return git.OpenRepository(ctx, g.dir)
	}
	defer func() { ascBridgeOpener = prevOpener }()

	rows := func() int64 {
		n, err := db.GetEngine(ctx).Count(new(issues_model.PullRequestAscStack))
		require.NoError(t, err)
		return n
	}
	before := rows()
	items, err := AscStackPrefix(ctx, top)
	require.NoError(t, err)
	require.Len(t, items, 2, "git says the top sits on the bottom")
	assert.Equal(t, bottom.ID, items[0].PR.ID)
	assert.True(t, items[1].Derived)
	assert.Zero(t, parentOf(t, top), "a page reads: nothing was stored")
	assert.Equal(t, before, rows())

	panel, err := BuildAscStackPanel(ctx, top)
	require.NoError(t, err)
	require.NotNil(t, panel)
	assert.Len(t, panel.Entries, 2, "the page shows the derived parent")
	assert.Zero(t, parentOf(t, top), "and writes nothing")

	// stored only after the merge was allowed to the doer
	require.NoError(t, PersistAscStackLinks(ctx, items))
	assert.Equal(t, bottom.ID, parentOf(t, top))
}

func TestMarkLandedAscStackMerged(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	landed := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2}) // base master
	open := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5})   // base branch2
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, landed, strings.Repeat("k", 32), 0))
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, open, strings.Repeat("l", 32), landed.ID))

	gitIn := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo.RepoPath()
		cmd.Env = append(cmd.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	// the head of the first is a commit of the trunk; the head of the second is a commit no branch has
	gitIn("update-ref", landed.GetGitRefName(), gitIn("rev-parse", "master"))
	orphan := gitIn("commit-tree", "-m", "not landed", gitIn("rev-parse", "master^{tree}"))
	gitIn("update-ref", open.GetGitRefName(), orphan)

	n, err := MarkLandedAscStackMerged(ctx, repo)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	got := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: landed.ID})
	assert.True(t, got.HasMerged)
	assert.Equal(t, gitIn("rev-parse", "master"), got.MergedCommitID)
	assert.False(t, unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: open.ID}).HasMerged)

	// nothing is left to mark the second time
	n, err = MarkLandedAscStackMerged(ctx, repo)
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestAscVersionDetails(t *testing.T) {
	g := commitChain(t, 1)
	commitFile := func(name, body, msg string) string {
		require.NoError(t, os.WriteFile(filepath.Join(g.dir, name), []byte(body), 0o644))
		g.run("add", name)
		g.run("commit", "-q", "-m", msg)
		return g.run("rev-parse", "HEAD")
	}
	v1 := commitFile("a.txt", "one\ntwo\n", "Add a\n\nbody")
	// the same patch on a new base: the trunk moved by one commit, the change was replayed on it
	g.run("checkout", "-q", "-b", "moved", g.ids[0])
	moved := commitFile("other.txt", "x\n", "Trunk moves")
	require.NotEqual(t, g.ids[0], moved)
	v2 := commitFile("a.txt", "one\ntwo\n", "Add a\n\nbody")
	// a real change on top of that
	v3 := commitFile("a.txt", "one\ntwo\nthree\n", "Add a, with three")

	list := &AscVersionList{Versions: []AscVersion{{N: 1, GitCommit: v1}, {N: 2, GitCommit: v2}, {N: 3, GitCommit: v3}}}
	d := AscVersionDetails(t.Context(), g.repo, list)
	require.Len(t, d, 3)
	assert.Equal(t, "Add a", d[1].Subject)
	assert.Equal(t, v1[:10], d[1].Short)
	assert.Equal(t, 2, d[1].Add)
	assert.Zero(t, d[1].Del)
	assert.False(t, d[1].Restack)
	assert.True(t, d[2].Restack, "same patch, new base")
	assert.False(t, d[3].Restack)
	assert.Equal(t, "Add a, with three", d[3].Subject)
}
