// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"os/exec"
	"strings"
	"testing"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// commitChain makes a repository with a line of commits and returns it with the ids, oldest first.
func commitChain(t *testing.T, n int) (*git.Repository, []string) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")
	var ids []string
	for i := range n {
		run("commit", "-q", "--allow-empty", "-m", "change "+string(rune('a'+i)))
		ids = append(ids, run("rev-parse", "HEAD"))
	}
	repo, err := git.OpenRepository(t.Context(), dir)
	require.NoError(t, err)
	t.Cleanup(func() { repo.Close() })
	return repo, ids
}

func TestLinkAscStackByGitLinksTheParentsOfAPush(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	stubFirstParent(t, ascTestFirst, nil)
	bridge, ids := commitChain(t, 4) // ids[0] is the trunk; the stack is ids[1] <- ids[2] <- ids[3]

	bottom := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	middle := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 3})
	top := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5})
	for i, pr := range []*issues_model.PullRequest{bottom, middle, top} {
		// what the hook leaves when asc did not know the parents: every link is 0
		require.NoError(t, issues_model.SetPullRequestAscStack(ctx, pr, strings.Repeat(string(rune('k'+i)), 32), 0))
	}
	heads := map[int64]string{bottom.ID: ids[1], middle.ID: ids[2], top.ID: ids[3]}

	// the batch lists the changes in any order: the top first
	linked, err := LinkAscStackByGit(ctx, bridge, heads, []*issues_model.PullRequest{top, bottom, middle})
	require.NoError(t, err)
	assert.Equal(t, 2, linked)
	parentOf := func(pr *issues_model.PullRequest) int64 {
		row, err := issues_model.GetPullRequestAscStack(ctx, pr.ID)
		require.NoError(t, err)
		return row.StackParentID
	}
	assert.Zero(t, parentOf(bottom), "the bottom's parent is the trunk")
	assert.Equal(t, bottom.ID, parentOf(middle))
	assert.Equal(t, middle.ID, parentOf(top))

	// a submit again changes nothing
	linked, err = LinkAscStackByGit(ctx, bridge, heads, []*issues_model.PullRequest{bottom, middle, top})
	require.NoError(t, err)
	assert.Zero(t, linked)

	// a new version of the middle change on the same parent keeps its link; a head nobody has is skipped
	heads[middle.ID] = ids[2]
	delete(heads, bottom.ID)
	_, err = LinkAscStackByGit(ctx, bridge, heads, []*issues_model.PullRequest{bottom, middle, top})
	require.NoError(t, err)
	assert.Equal(t, middle.ID, parentOf(top))
}
