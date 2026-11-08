// Copyright 2017 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo_test

import (
	"testing"
	"time"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/timeutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRedirect(t *testing.T) {
	// redirect to a completely new name
	require.NoError(t, unittest.PrepareTestDatabase())

	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	require.NoError(t, repo_model.NewRedirect(db.DefaultContext, repo.OwnerID, repo.ID, repo.Name, "newreponame"))

	unittest.AssertExistsAndLoadBean(t, &repo_model.Redirect{
		OwnerID:        repo.OwnerID,
		LowerName:      repo.LowerName,
		RedirectRepoID: repo.ID,
	})
	unittest.AssertExistsAndLoadBean(t, &repo_model.Redirect{
		OwnerID:        repo.OwnerID,
		LowerName:      "oldrepo1",
		RedirectRepoID: repo.ID,
	})
}

func TestNewRedirect2(t *testing.T) {
	// redirect to previously used name
	require.NoError(t, unittest.PrepareTestDatabase())

	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	require.NoError(t, repo_model.NewRedirect(db.DefaultContext, repo.OwnerID, repo.ID, repo.Name, "oldrepo1"))

	unittest.AssertExistsAndLoadBean(t, &repo_model.Redirect{
		OwnerID:        repo.OwnerID,
		LowerName:      repo.LowerName,
		RedirectRepoID: repo.ID,
	})
	unittest.AssertNotExistsBean(t, &repo_model.Redirect{
		OwnerID:        repo.OwnerID,
		LowerName:      "oldrepo1",
		RedirectRepoID: repo.ID,
	})
}

func TestNewRedirect3(t *testing.T) {
	// redirect for a previously-unredirected repo
	require.NoError(t, unittest.PrepareTestDatabase())

	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 2})
	require.NoError(t, repo_model.NewRedirect(db.DefaultContext, repo.OwnerID, repo.ID, repo.Name, "newreponame"))

	unittest.AssertExistsAndLoadBean(t, &repo_model.Redirect{
		OwnerID:        repo.OwnerID,
		LowerName:      repo.LowerName,
		RedirectRepoID: repo.ID,
	})
}

func TestRedirectExpiry(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	require.NoError(t, repo_model.NewRedirect(db.DefaultContext, repo.OwnerID, repo.ID, repo.Name, "expiryrepo"))

	// a new redirect keeps redirecting for RedirectKeepDays
	r := unittest.AssertExistsAndLoadBean(t, &repo_model.Redirect{OwnerID: repo.OwnerID, LowerName: repo.LowerName})
	want := timeutil.TimeStampNow().AddDuration(repo_model.RedirectKeepDays * 24 * time.Hour)
	assert.InDelta(t, int64(want), int64(r.ExpiresUnix), 60)
	id, err := repo_model.GetRedirect(db.DefaultContext, repo.OwnerID, repo.Name)
	require.NoError(t, err)
	assert.Equal(t, repo.ID, id)

	// an expired redirect is gone, and its row with it
	_, err = db.GetEngine(db.DefaultContext).ID(r.ID).Cols("expires_unix").Update(&repo_model.Redirect{ExpiresUnix: timeutil.TimeStampNow() - 1})
	require.NoError(t, err)
	_, err = repo_model.GetRedirect(db.DefaultContext, repo.OwnerID, repo.Name)
	assert.True(t, repo_model.IsErrRedirectNotExist(err))
	unittest.AssertNotExistsBean(t, &repo_model.Redirect{ID: r.ID})

	// a redirect made before redirects expired (0) never stops
	legacy := &repo_model.Redirect{OwnerID: repo.OwnerID, LowerName: "legacyname", RedirectRepoID: repo.ID}
	require.NoError(t, db.Insert(db.DefaultContext, legacy))
	id, err = repo_model.GetRedirect(db.DefaultContext, repo.OwnerID, "legacyname")
	require.NoError(t, err)
	assert.Equal(t, repo.ID, id)
}
