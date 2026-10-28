// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"testing"

	repo_model "forgejo.org/models/repo"

	"github.com/stretchr/testify/assert"
)

// The home page of a repository must never answer with a redirect to itself. The only redirect is the one
// that clears the database's "empty" flag, and that flag is what it tests, so it happens once.
func TestHomeStateOf(t *testing.T) {
	broken := func() *repo_model.Repository {
		r := &repo_model.Repository{}
		r.MarkAsBrokenEmpty() // what the repository middleware does when the default branch commit cannot be read
		return r
	}
	for name, c := range map[string]struct {
		repo         *repo_model.Repository
		hasCommit    bool
		storageEmpty bool
		want         homeState
	}{
		"readable":                             {&repo_model.Repository{}, true, false, homeRender},
		"empty in the database and storage":    {&repo_model.Repository{IsEmpty: true}, false, true, homeEmpty},
		"flagged empty but holds commits":      {&repo_model.Repository{IsEmpty: true}, false, false, homeFixEmptyFlag},
		"default branch commit missing":        {broken(), false, false, homeUnavailable},
		"default branch commit missing, empty": {broken(), false, true, homeUnavailable},
		"marked broken yet readable":           {&repo_model.Repository{Status: repo_model.RepositoryBroken}, true, false, homeUnavailable},
		"not flagged, commit unreadable":       {&repo_model.Repository{}, false, false, homeUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, c.want, homeStateOf(c.repo, c.hasCommit, c.storageEmpty))
		})
	}

	// The loop of staging: the repository holds refs in asc (storage not empty) but the commit of the default branch
	// is missing, so the middleware marked it broken, and the database flag is already false. No state may redirect.
	r := broken()
	r.IsEmpty = false // the database value; the middleware's in-memory change is not saved
	r.Status = repo_model.RepositoryBroken
	assert.NotEqual(t, homeFixEmptyFlag, homeStateOf(r, false, false))
}
