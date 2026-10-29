// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	admin_model "forgejo.org/models/admin"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/structs"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A repository whose default branch points at a commit that the storage does not hold (an import that never
// reached the storage, a lost object) used to answer 303 to its own address forever. It must render the
// "repository unavailable" page instead, with no Location header.
func TestRepoHomeMissingDefaultBranchCommit(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	require.False(t, repo.IsEmpty)
	// A loose ref wins over the packed one: master now names a commit that is in no object store.
	ref := filepath.Join(repo.RepoPath(), "refs", "heads", repo.DefaultBranch)
	require.NoError(t, os.MkdirAll(filepath.Dir(ref), 0o755))
	require.NoError(t, os.WriteFile(ref, []byte("247ec20668c5ff36a399ce9c64e3cd853b25cedf\n"), 0o644))

	link := "/" + repo.OwnerName + "/" + repo.Name
	for _, session := range []*TestSession{loginUser(t, "user2"), emptyTestSession(t)} {
		// Repeat: a redirect would show on any of the requests, and the page must not change the repository
		// into something that redirects on the next one.
		for range 3 {
			resp := session.MakeRequest(t, NewRequest(t, "GET", link), NoExpectedStatus)
			assert.Equal(t, http.StatusConflict, resp.Code)
			assert.Empty(t, resp.Header().Get("Location"), "the page must not redirect")
			assert.Contains(t, resp.Body.String(), `data-test-id="repo-unavailable"`)
		}
	}

	// The owner is offered the way out; a visitor is not.
	owner := loginUser(t, "user2")
	body := owner.MakeRequest(t, NewRequest(t, "GET", link), http.StatusConflict).Body.String()
	assert.Contains(t, body, `id="repo_unavailable_reason"`)
	assert.Contains(t, body, `id="delete-repo-modal"`)
	anon := emptyTestSession(t).MakeRequest(t, NewRequest(t, "GET", link), http.StatusConflict).Body.String()
	assert.NotContains(t, anon, `id="delete-repo-modal"`)
}

// The reason of a failed migration may hold remote credentials and internal paths: the owners and the user who
// started the migration read it redacted, a visitor reads only the generic text.
func TestRepoUnavailableShowsTheMigrationReasonOnlyToOwners(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: repo.OwnerID})
	ref := filepath.Join(repo.RepoPath(), "refs", "heads", repo.DefaultBranch)
	require.NoError(t, os.WriteFile(ref, []byte("247ec20668c5ff36a399ce9c64e3cd853b25cedf\n"), 0o644))
	require.NoError(t, admin_model.CreateTask(t.Context(), &admin_model.Task{
		DoerID:         owner.ID,
		OwnerID:        owner.ID,
		RepoID:         repo.ID,
		Type:           structs.TaskTypeMigrateRepo,
		Status:         structs.TaskStatusFailed,
		PayloadContent: `{"clone_addr":"https://github.com/o/r"}`,
		Message:        "Clone: fatal: unable to access 'https://user:s3cret-token@github.com/o/r.git/' while writing /srv/internal/repos/x.git/objects",
	}))

	link := "/" + repo.OwnerName + "/" + repo.Name
	body := loginUser(t, owner.Name).MakeRequest(t, NewRequest(t, "GET", link), http.StatusConflict).Body.String()
	assert.Contains(t, body, "fatal: unable to access")
	assert.Contains(t, body, "sanitized-credential@github.com")
	assert.NotContains(t, body, "s3cret-token")
	assert.NotContains(t, body, "/srv/internal")

	for _, session := range []*TestSession{loginUser(t, "user4"), emptyTestSession(t)} {
		body := session.MakeRequest(t, NewRequest(t, "GET", link), http.StatusConflict).Body.String()
		assert.Contains(t, body, `id="repo_unavailable_reason"`)
		assert.NotContains(t, body, "fatal: unable to access")
		assert.NotContains(t, body, "s3cret-token")
		assert.NotContains(t, body, "sanitized-credential")
		assert.Contains(t, body, "cannot be read from the repository storage")
	}
}

// A repository that the database marks empty although it holds commits is corrected with one redirect, then
// served. The redirect must happen once, not on every request.
func TestRepoHomeEmptyFlagRedirectsOnce(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	repo.IsEmpty = true
	require.NoError(t, repo_model.UpdateRepositoryCols(t.Context(), repo, "is_empty"))

	link := "/" + repo.OwnerName + "/" + repo.Name
	session := loginUser(t, "user2")
	resp := session.MakeRequest(t, NewRequest(t, "GET", link), http.StatusSeeOther)
	assert.Equal(t, link, resp.Header().Get("Location"))
	session.MakeRequest(t, NewRequest(t, "GET", link), http.StatusOK)
	session.MakeRequest(t, NewRequest(t, "GET", link), http.StatusOK)
}
