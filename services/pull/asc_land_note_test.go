// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"testing"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/ascsync"

	"github.com/stretchr/testify/assert"
)

// A land of a stack tells the hub whose write it is before asc calls the hub's hooks, and the hook (PeekWriter) takes
// the person back for exactly the ref change the land announces.
func TestNoteAscLandAttributesTheLandToTheDoer(t *testing.T) {
	unittest.PrepareTestEnv(t)
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	req := AscLandRequest{Repo: repo.FullName(), Target: "refs/heads/main", Heads: []string{headBottom, headTop}, Doer: doer, PullRequestID: 7}

	tok := noteAscLand(t.Context(), req)
	id, pr, ok := ascsync.PeekWriter(repo, []ascsync.PushRef{{Name: "refs/heads/main", Old: "any", New: headTop}})
	assert.True(t, ok)
	assert.Equal(t, doer.ID, id)
	assert.EqualValues(t, 7, pr)

	// another ref, or another commit, is nobody's write
	_, _, ok = ascsync.PeekWriter(repo, []ascsync.PushRef{{Name: "refs/heads/other", New: headTop}})
	assert.False(t, ok)
	_, _, ok = ascsync.PeekWriter(repo, []ascsync.PushRef{{Name: "refs/heads/main", New: headBottom}})
	assert.False(t, ok)

	// a refused land is forgotten
	tok.Forget()
	_, _, ok = ascsync.PeekWriter(repo, []ascsync.PushRef{{Name: "refs/heads/main", New: headTop}})
	assert.False(t, ok)
}

func TestNoteAscLandWithoutADoerRecordsNothing(t *testing.T) {
	unittest.PrepareTestEnv(t)
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	noteAscLand(t.Context(), AscLandRequest{Repo: repo.FullName(), Target: "refs/heads/main", Heads: []string{headTop}})
	_, _, ok := ascsync.PeekWriter(repo, []ascsync.PushRef{{Name: "refs/heads/main", New: headTop}})
	assert.False(t, ok)
	noteAscLand(t.Context(), AscLandRequest{Repo: "nobody/nothing", Heads: []string{headTop}, Doer: &user_model.User{ID: 2}})
}
