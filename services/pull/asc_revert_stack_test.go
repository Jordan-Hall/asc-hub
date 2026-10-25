// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAscRevert struct {
	landings map[string]*AscLanding
	reverts  []AscRevertRequest
	conflict int // 1-based index of the revert that conflicts
	findErr  error
}

func (f *fakeAscRevert) FindLanding(_ context.Context, _, _, landed string) (*AscLanding, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.landings[landed], nil
}

func (f *fakeAscRevert) Revert(_ context.Context, req AscRevertRequest) (*AscRevertResult, error) {
	f.reverts = append(f.reverts, req)
	if f.conflict == len(f.reverts) {
		return &AscRevertResult{Conflict: true}, nil
	}
	return &AscRevertResult{Result: strings.Repeat(string(rune('0'+len(f.reverts))), 40)}, nil
}

func ascRevertFixture(t *testing.T) (bottom, top *issues_model.PullRequest, fake *fakeAscRevert) {
	t.Helper()
	bottom, top, _ = ascMergeFixture(t)
	ctx := t.Context()
	for _, p := range []struct {
		pr *issues_model.PullRequest
		id string
	}{{bottom, landedBottom}, {top, landedTop}} {
		p.pr.HasMerged, p.pr.MergedCommitID = true, p.id
		_, err := db.GetEngine(ctx).ID(p.pr.ID).Cols("has_merged", "merged_commit_id").Update(p.pr)
		require.NoError(t, err)
	}
	fake = &fakeAscRevert{landings: map[string]*AscLanding{
		landedBottom: {Found: true, LandedAs: bytes.Repeat([]byte{1}, 32), Seq: 7},
		landedTop:    {Found: true, LandedAs: bytes.Repeat([]byte{2}, 32), Seq: 7},
	}}
	t.Cleanup(UseAscRevertBackend(fake))
	return bottom, top, fake
}

func TestAscRevertStackNewestFirst(t *testing.T) {
	bottom, top, fake := ascRevertFixture(t)
	doer, perm := ascMergePerm(t, top, 2)

	res, err := RevertAscStack(t.Context(), doer, perm, top)
	require.NoError(t, err)
	require.NoError(t, res.Err)
	require.Len(t, fake.reverts, 2)
	assert.Equal(t, bytes.Repeat([]byte{2}, 32), fake.reverts[0].Changeset, "the top reverts first")
	assert.Equal(t, bytes.Repeat([]byte{1}, 32), fake.reverts[1].Changeset)
	assert.Equal(t, "refs/heads/master", fake.reverts[0].Ref)
	assert.Equal(t, doer.GetEmail(), fake.reverts[0].CommitterEmail)
	require.Len(t, res.Done, 2)
	assert.Equal(t, top.ID, res.Done[0].PR.ID)
	assert.Equal(t, bottom.ID, res.Done[1].PR.ID)
	assert.Nil(t, res.Stopped)

	// A partial revert starts at the chosen pull request.
	fake.reverts = nil
	_, err = RevertAscStack(t.Context(), doer, perm, bottom)
	require.NoError(t, err)
	assert.Len(t, fake.reverts, 1)
}

func TestAscRevertStackStopsAtConflict(t *testing.T) {
	_, top, fake := ascRevertFixture(t)
	doer, perm := ascMergePerm(t, top, 2)
	fake.conflict = 1
	res, err := RevertAscStack(t.Context(), doer, perm, top)
	require.NoError(t, err)
	require.Len(t, fake.reverts, 1, "nothing runs after a conflict")
	assert.Empty(t, res.Done)
	require.NotNil(t, res.Stopped)
	assert.True(t, res.Stopped.Conflict)
}

// Security: a user without merge rights cannot revert, and asc is not called.
func TestAscRevertStackRefusesUserWithoutWrite(t *testing.T) {
	_, top, fake := ascRevertFixture(t)
	doer, perm := ascMergePerm(t, top, 5)
	require.False(t, perm.CanWrite(unit.TypeCode))
	_, err := RevertAscStack(t.Context(), doer, perm, top)
	require.ErrorIs(t, err, ErrAscRevertNotAllowed)
	assert.Empty(t, fake.reverts)
	_, err = RevertAscStack(t.Context(), nil, perm, top)
	require.ErrorIs(t, err, ErrAscRevertNotAllowed)
}

func TestAscRevertStackRefusesUnmergedAndNoBackend(t *testing.T) {
	bottom, top, fake := ascRevertFixture(t)
	doer, perm := ascMergePerm(t, top, 2)
	bottom.HasMerged = false
	_, err := db.GetEngine(t.Context()).ID(bottom.ID).Cols("has_merged").Update(bottom)
	require.NoError(t, err)
	_, err = RevertAscStack(t.Context(), doer, perm, top)
	require.ErrorIs(t, err, ErrAscRevertNotLanded)
	assert.Empty(t, fake.reverts)

	t.Cleanup(UseAscRevertBackend(nil))
	_, err = RevertAscStack(t.Context(), doer, perm, top)
	require.ErrorIs(t, err, ErrAscRevertDisabled)
}

// Security: a landing answer that is missing, malformed or out of order is refused before any write.
func TestAscRevertStackVerifiesLandings(t *testing.T) {
	_, top, fake := ascRevertFixture(t)
	doer, perm := ascMergePerm(t, top, 2)

	fake.landings[landedBottom] = &AscLanding{Found: false}
	_, err := RevertAscStack(t.Context(), doer, perm, top)
	require.ErrorIs(t, err, ErrAscRevertBadAnswer)

	fake.landings[landedBottom] = &AscLanding{Found: true, LandedAs: []byte{1}, Seq: 7}
	_, err = RevertAscStack(t.Context(), doer, perm, top)
	require.ErrorIs(t, err, ErrAscRevertBadAnswer)

	fake.landings[landedBottom] = &AscLanding{Found: true, LandedAs: bytes.Repeat([]byte{1}, 32), Seq: 9}
	_, err = RevertAscStack(t.Context(), doer, perm, top)
	require.ErrorIs(t, err, ErrAscRevertBadAnswer, "the lower pull request cannot be newer")

	fake.landings[landedBottom] = &AscLanding{Found: true, LandedAs: bytes.Repeat([]byte{2}, 32), Seq: 7}
	_, err = RevertAscStack(t.Context(), doer, perm, top)
	require.ErrorIs(t, err, ErrAscRevertBadAnswer, "two pull requests cannot share a landing")

	fake.findErr = errors.New("down")
	_, err = RevertAscStack(t.Context(), doer, perm, top)
	require.Error(t, err)
	assert.Empty(t, fake.reverts)
}
