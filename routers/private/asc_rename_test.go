// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/private"
	ascwriter "forgejo.org/modules/repostore/asc"
	repo_service "forgejo.org/services/repository"
	user_service "forgejo.org/services/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// renameRecorder is the RepoAdmin of an asc serve that records the renames it is asked for.
type renameRecorder struct {
	ascv1.RepoClient
	mu      sync.Mutex
	renames [][2]string
	fail    error
}

func (r *renameRecorder) RepoAdmin(_ context.Context, in *ascv1.RepoAdminRequest, _ ...grpc.CallOption) (*ascv1.RepoAdminResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return nil, r.fail
	}
	if to, ok := in.GetOp().(*ascv1.RepoAdminRequest_RenameTo); ok {
		r.renames = append(r.renames, [2]string{in.GetRepo().GetName(), to.RenameTo})
	}
	return &ascv1.RepoAdminResponse{}, nil
}

// moveBack puts a directory that a test renamed back, so the repositories of the other tests of the package stay.
func moveBack(t *testing.T, from, to string) {
	t.Helper()
	t.Cleanup(func() { _ = os.Rename(from, to) })
}

func useRenameRecorder(t *testing.T) *renameRecorder {
	t.Helper()
	rec := &renameRecorder{}
	ascwriter.Configure(rec, ascwriter.Options{})
	t.Cleanup(ascwriter.Reset)
	return rec
}

// A name that was given up by a rename or a transfer, and taken again by a repository on git storage, must not be
// answered for by the registry entry of the repository that left.
func TestAscAuthzRefusesANameTakenOverAfterAnOwnerRename(t *testing.T) {
	ascAuthzSetup(t)
	rec := useRenameRecorder(t)
	moveBack(t, user_model.UserPath("org3-renamed"), user_model.UserPath("org3"))

	// org3/repo3 is stored in asc and public; user2 owns org3.
	_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE repository SET is_private = ? WHERE id = 3", false)
	require.NoError(t, err)
	owner2 := ascNewToken(t, 2, auth_model.AccessTokenScopeAll)
	before := ascAuthzDo(t, private.AscAuthzRequest{Token: owner2, Owner: "org3", Repo: "repo3", Mode: "write"})
	require.True(t, before.Allowed)
	assert.EqualValues(t, 3, before.RepoID)
	assert.EqualValues(t, 3, before.OwnerID)

	// Rename the organisation: its asc repositories move in the registry before the rename is done.
	org := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 3})
	require.NoError(t, user_service.RenameUser(db.DefaultContext, org, "org3-renamed"))
	rec.mu.Lock()
	renames := rec.renames
	rec.mu.Unlock()
	assert.Contains(t, renames, [2]string{"org3/repo3", "org3-renamed/repo3"})

	// Another person takes the old name and makes a repository of it on git storage.
	taker := &user_model.User{Name: "org3", LowerName: "org3", Email: "taker@example.com", Passwd: "x", IsActive: true, Type: user_model.UserTypeIndividual}
	require.NoError(t, db.Insert(db.DefaultContext, taker))
	require.NoError(t, db.Insert(db.DefaultContext, &repo_model.Repository{
		OwnerID: taker.ID, OwnerName: "org3", Name: "repo3", LowerName: "repo3", StorageBackend: string(gitrepo.StorageBackendGit), Status: repo_model.RepositoryReady,
	}))
	takerToken := ascNewToken(t, taker.ID, auth_model.AccessTokenScopeAll)
	for _, mode := range []string{"read", "write"} {
		resp := ascAuthzDo(t, private.AscAuthzRequest{Token: takerToken, Owner: "org3", Repo: "repo3", Mode: mode})
		assert.False(t, resp.Allowed, mode+": the new owner of a git repository is not authorised by the entry of the old one")
		assert.Zero(t, resp.RepoID)
	}
	// It looks the same as a name that was never used.
	gone := ascAuthzDo(t, private.AscAuthzRequest{Token: takerToken, Owner: "org3", Repo: "never-existed", Mode: "read"})
	taken := ascAuthzDo(t, private.AscAuthzRequest{Token: takerToken, Owner: "org3", Repo: "repo3", Mode: "read"})
	assert.Equal(t, gone, taken)

	// The repository that moved is reached by its new name, with the same id.
	after := ascAuthzDo(t, private.AscAuthzRequest{Token: owner2, Owner: "org3-renamed", Repo: "repo3", Mode: "write"})
	require.True(t, after.Allowed)
	assert.Equal(t, before.RepoID, after.RepoID, "the id is stable across the rename")
}

func TestAscAuthzRefusesAGitRepository(t *testing.T) {
	ascAuthzSetup(t)
	_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE repository SET storage_backend = ? WHERE id = 1", string(gitrepo.StorageBackendGit))
	require.NoError(t, err)
	tok := ascNewToken(t, 2, auth_model.AccessTokenScopeAll)
	assert.False(t, ascAuthzDo(t, private.AscAuthzRequest{Token: tok, Owner: "user2", Repo: "repo1", Mode: "read"}).Allowed)
	assert.False(t, ascAuthzDo(t, private.AscAuthzRequest{Owner: "user2", Repo: "repo1", Mode: "read"}).Allowed, "also for an anonymous read")
}

func TestRenameOfAnAscRepositoryMovesTheRegistryOrIsBlocked(t *testing.T) {
	ascAuthzSetup(t)
	rec := useRenameRecorder(t)
	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	moveBack(t, repo_model.RepoPath("user2", "renamed-repo"), repo_model.RepoPath("user2", "repo1"))

	require.NoError(t, repo_service.ChangeRepositoryName(db.DefaultContext, user, repo, "renamed-repo"))
	rec.mu.Lock()
	assert.Equal(t, [][2]string{{"user2/repo1", "user2/renamed-repo"}}, rec.renames)
	rec.fail = errors.New("asc is down")
	rec.mu.Unlock()

	// When asc cannot be told, the rename does not happen: the name stays and no directory moves.
	repo = unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	repo.Name = "renamed-repo"
	err := repo_service.ChangeRepositoryName(db.DefaultContext, user, repo, "second-name")
	require.ErrorContains(t, err, "asc")
	assert.Equal(t, "renamed-repo", repo.Name)
	rec.mu.Lock()
	assert.Len(t, rec.renames, 1)
	rec.mu.Unlock()
}
