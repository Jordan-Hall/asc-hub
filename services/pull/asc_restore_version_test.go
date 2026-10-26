// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"errors"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeHeads is a repository whose pull head is a string.
type fakeHeads struct {
	head    string
	commits map[string]bool
	moves   []string
	headErr error
	moveErr error
}

func (f *fakeHeads) Head(context.Context, *issues_model.PullRequest) (string, error) {
	return f.head, f.headErr
}

func (f *fakeHeads) HasCommit(_ context.Context, _ *issues_model.PullRequest, id string) (bool, error) {
	return f.commits[id], nil
}

func (f *fakeHeads) Move(_ context.Context, pr *issues_model.PullRequest, id string) error {
	if f.moveErr != nil {
		return f.moveErr
	}
	f.moves = append(f.moves, id)
	f.head = id
	pr.HeadCommitID = id
	return nil
}

type followUpCall struct {
	doerID int64
	oldID  string
	newID  string
}

// setupAscRestore stubs asc and the repository: three versions a, b, c, the head at c, every commit present.
func setupAscRestore(t *testing.T) (*issues_model.PullRequest, *fakeChanges, *fakeHeads, *[]followUpCall) {
	t.Helper()
	f := &fakeChanges{versions: threeVersions()}
	pr := setupAscVersions(t, f)
	heads := &fakeHeads{head: gitHexN('c'), commits: map[string]bool{gitHexN('a'): true, gitHexN('b'): true, gitHexN('c'): true}}
	calls := &[]followUpCall{}
	prevH, prevF := ascHeads, ascRestoreFollowUp
	ascHeads = heads
	ascRestoreFollowUp = func(_ context.Context, _ *issues_model.PullRequest, doer *user_model.User, oldID, newID string) {
		*calls = append(*calls, followUpCall{doerID: doer.ID, oldID: oldID, newID: newID})
	}
	t.Cleanup(func() { ascHeads, ascRestoreFollowUp = prevH, prevF })
	return pr, f, heads, calls
}

func ascTestUser(t *testing.T, id int64) *user_model.User {
	t.Helper()
	return unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: id})
}

func TestAscRestoreVersion(t *testing.T) {
	pr, _, heads, calls := setupAscRestore(t)
	admin := ascTestUser(t, 1) // site admin

	res, err := AscRestoreVersion(t.Context(), pr, admin, 2)
	require.NoError(t, err)
	assert.Equal(t, &AscRestoreResult{Version: 2, OldCommit: gitHexN('c'), NewCommit: gitHexN('b')}, res)
	assert.Equal(t, []string{gitHexN('b')}, heads.moves)
	assert.Equal(t, gitHexN('b'), pr.HeadCommitID)
	assert.Equal(t, []followUpCall{{doerID: 1, oldID: gitHexN('c'), newID: gitHexN('b')}}, *calls)

	// The head is now version 2; restoring it again is refused, restoring the newest is allowed.
	_, err = AscRestoreVersion(t.Context(), pr, admin, 2)
	require.ErrorIs(t, err, ErrAscRestoreCurrent)
	res, err = AscRestoreVersion(t.Context(), pr, admin, 3)
	require.NoError(t, err)
	assert.Equal(t, gitHexN('b'), res.OldCommit)
	assert.Equal(t, []string{gitHexN('b'), gitHexN('c')}, heads.moves)
	assert.Len(t, *calls, 2)
}

func TestAscRestoreVersionRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		version uint32
		doer    int64
		prep    func(t *testing.T, pr *issues_model.PullRequest, heads *fakeHeads)
		want    error
	}{
		"version 0":              {version: 0, doer: 1, want: ErrAscBadVersion},
		"version past the last":  {version: 4, doer: 1, want: ErrAscBadVersion},
		"huge version":           {version: 4294967295, doer: 1, want: ErrAscBadVersion},
		"already at the version": {version: 3, doer: 1, want: ErrAscRestoreCurrent},
		"commit is gone": {version: 1, doer: 1, want: ErrAscRestoreGone, prep: func(_ *testing.T, _ *issues_model.PullRequest, heads *fakeHeads) {
			delete(heads.commits, gitHexN('a'))
		}},
		"not the poster and no rights": {version: 1, doer: 4, want: ErrAscRestoreForbidden},
		"merged": {version: 1, doer: 1, want: ErrAscRestoreNotOpen, prep: func(_ *testing.T, pr *issues_model.PullRequest, _ *fakeHeads) {
			pr.HasMerged = true
		}},
		"closed": {version: 1, doer: 1, want: ErrAscRestoreNotOpen, prep: func(t *testing.T, pr *issues_model.PullRequest, _ *fakeHeads) {
			_, err := db.GetEngine(t.Context()).Exec("UPDATE `issue` SET is_closed = ? WHERE id = ?", true, pr.IssueID)
			require.NoError(t, err)
		}},
		"archived repository": {version: 1, doer: 1, want: ErrAscRestoreNotOpen, prep: func(t *testing.T, pr *issues_model.PullRequest, _ *fakeHeads) {
			_, err := db.GetEngine(t.Context()).Exec("UPDATE `repository` SET is_archived = ? WHERE id = ?", true, pr.BaseRepoID)
			require.NoError(t, err)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			pr, _, heads, calls := setupAscRestore(t)
			if tc.prep != nil {
				tc.prep(t, pr, heads)
			}
			_, err := AscRestoreVersion(t.Context(), pr, ascTestUser(t, tc.doer), tc.version)
			require.ErrorIs(t, err, tc.want)
			assert.Empty(t, heads.moves)
			assert.Empty(t, *calls)
			assert.Equal(t, gitHexN('c'), heads.head)
		})
	}
}

func TestAscRestoreVersionNeedsADoer(t *testing.T) {
	pr, f, heads, calls := setupAscRestore(t)
	_, err := AscRestoreVersion(t.Context(), pr, nil, 1)
	require.ErrorIs(t, err, ErrAscRestoreForbidden)
	assert.Empty(t, heads.moves)
	assert.Empty(t, *calls)
	assert.Nil(t, f.lastChange, "asc is not asked before the permission is checked")
}

func TestAscRestoreVersionForbiddenBeforeAsc(t *testing.T) {
	pr, f, heads, _ := setupAscRestore(t)
	_, err := AscRestoreVersion(t.Context(), pr, ascTestUser(t, 4), 1)
	require.ErrorIs(t, err, ErrAscRestoreForbidden)
	assert.Nil(t, f.lastChange)
	assert.Empty(t, heads.moves)
}

func TestAscRestoreVersionPoster(t *testing.T) {
	pr, _, heads, calls := setupAscRestore(t)
	// user4 posted the pull request and can read it: allowed without any other right.
	_, err := db.GetEngine(t.Context()).Exec("UPDATE `issue` SET poster_id = ? WHERE id = ?", 4, pr.IssueID)
	require.NoError(t, err)
	res, err := AscRestoreVersion(t.Context(), pr, ascTestUser(t, 4), 1)
	require.NoError(t, err)
	assert.EqualValues(t, 1, res.Version)
	assert.Equal(t, []string{gitHexN('a')}, heads.moves)
	assert.Equal(t, []followUpCall{{doerID: 4, oldID: gitHexN('c'), newID: gitHexN('a')}}, *calls)
}

func TestAscRestoreVersionOwner(t *testing.T) {
	pr, _, heads, _ := setupAscRestore(t)
	// user2 owns the repository and did not post the pull request.
	_, err := AscRestoreVersion(t.Context(), pr, ascTestUser(t, 2), 1)
	require.NoError(t, err)
	assert.Equal(t, []string{gitHexN('a')}, heads.moves)
}

func TestAscRestoreVersionNotStacked(t *testing.T) {
	_, f, heads, calls := setupAscRestore(t)
	other := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	_, err := AscRestoreVersion(t.Context(), other, ascTestUser(t, 1), 1)
	require.ErrorIs(t, err, ErrAscNotStacked)
	assert.Empty(t, heads.moves)
	assert.Empty(t, *calls)
	assert.Nil(t, f.lastChange)
}

func TestAscRestoreVersionRepositoryFailures(t *testing.T) {
	pr, _, heads, calls := setupAscRestore(t)
	admin := ascTestUser(t, 1)

	heads.headErr = errors.New("repository is gone")
	_, err := AscRestoreVersion(t.Context(), pr, admin, 1)
	require.ErrorContains(t, err, "repository is gone")
	assert.Empty(t, heads.moves)

	heads.headErr = nil
	heads.moveErr = errors.New("asc refused")
	_, err = AscRestoreVersion(t.Context(), pr, admin, 1)
	require.ErrorContains(t, err, "asc refused")
	assert.Empty(t, *calls, "nothing follows a move that failed")
	assert.Equal(t, gitHexN('c'), heads.head)
}

func TestAscRestoreVersionRefusesABadList(t *testing.T) {
	pr, f, heads, calls := setupAscRestore(t)
	// A list that is not numbered 1..N is refused, and nothing is restored from it.
	f.versions = []*ascv1.Version{ascTestVersion(2, gitHexN('a'))}
	_, err := AscRestoreVersion(t.Context(), pr, ascTestUser(t, 1), 1)
	require.ErrorIs(t, err, ErrAscBadAnswer)
	assert.Empty(t, heads.moves)
	assert.Empty(t, *calls)
}

func TestAscRestoreAllowed(t *testing.T) {
	for _, tc := range []struct {
		poster, adminOrOwner, writer, maintainerEdit, want bool
	}{
		{false, false, false, false, false},
		{false, false, true, false, false},
		{false, false, false, true, false},
		{false, false, true, true, true},
		{true, false, false, false, true},
		{false, true, false, false, true},
	} {
		assert.Equal(t, tc.want, ascRestoreAllowed(tc.poster, tc.adminOrOwner, tc.writer, tc.maintainerEdit), "%+v", tc)
	}
}

func TestAscMayRestoreVersion(t *testing.T) {
	pr, _, _, _ := setupAscRestore(t)
	ok, err := AscMayRestoreVersion(t.Context(), pr, nil)
	require.NoError(t, err)
	assert.False(t, ok)
	ok, err = AscMayRestoreVersion(t.Context(), nil, ascTestUser(t, 1))
	require.NoError(t, err)
	assert.False(t, ok)
	ok, err = AscMayRestoreVersion(t.Context(), pr, ascTestUser(t, 1))
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = AscMayRestoreVersion(t.Context(), pr, ascTestUser(t, 4))
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestAscPullIsOpen(t *testing.T) {
	pr, _, _, _ := setupAscRestore(t)
	open, err := AscPullIsOpen(t.Context(), pr)
	require.NoError(t, err)
	assert.True(t, open)
	open, err = AscPullIsOpen(t.Context(), nil)
	require.NoError(t, err)
	assert.False(t, open)
	pr.HasMerged = true
	open, err = AscPullIsOpen(t.Context(), pr)
	require.NoError(t, err)
	assert.False(t, open)
}

func TestAscHeadVersion(t *testing.T) {
	pr, _, heads, _ := setupAscRestore(t)
	versions, err := AscVersions(t.Context(), pr)
	require.NoError(t, err)

	v, err := AscHeadVersion(t.Context(), pr, versions)
	require.NoError(t, err)
	assert.EqualValues(t, 3, v)

	heads.head = gitHexN('a')
	v, err = AscHeadVersion(t.Context(), pr, versions)
	require.NoError(t, err)
	assert.EqualValues(t, 1, v)

	heads.head = gitHexN('f') // a head that is none of the versions
	v, err = AscHeadVersion(t.Context(), pr, versions)
	require.NoError(t, err)
	assert.EqualValues(t, 0, v)

	heads.headErr = errors.New("no repository")
	_, err = AscHeadVersion(t.Context(), pr, versions)
	require.Error(t, err)

	v, err = AscHeadVersion(t.Context(), pr, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 0, v)
}
