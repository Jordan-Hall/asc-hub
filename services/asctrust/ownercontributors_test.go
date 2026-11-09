// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/services/ascidentity"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// orgAscRepos makes the repositories of org3 asc ones: 3 (private), 5 (private) and 32 (public). Repository 1 is user2's.
func orgAscRepos(t *testing.T) (org *user_model.User, private, public, personal *repo_model.Repository) {
	t.Helper()
	_, err := db.GetEngine(t.Context()).Exec("UPDATE repository SET storage_backend = ? WHERE id IN (1, 3, 5, 32)", string(gitrepo.StorageBackendAsc))
	require.NoError(t, err)
	org = unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 3})
	private = unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 3})
	public = unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 32})
	personal = unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	return org, private, public, personal
}

func TestUserCallOrgReposNamesOnlyWhatThePersonMayRead(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	useForwardedKey(t)
	org, private, public, personal := orgAscRepos(t)
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	stranger := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})
	want := func(r *repo_model.Repository) string {
		n, err := RepoName(r)
		require.NoError(t, err)
		return n
	}

	ctx, release, names, err := UserCallOrgRepos(t.Context(), owner, org, []*repo_model.Repository{private, public, personal, nil})
	require.NoError(t, err)
	release()
	assert.Equal(t, []string{want(private), want(public)}, names, "a repository of another owner is never named")
	user, role, verr := claimIn(t, ctx, "org:"+org.LowerName)
	require.NoError(t, verr)
	assert.Equal(t, ascidentity.RoleRead, role, "a read of the organisation, whatever the person is in it")
	assert.Contains(t, user, owner.Name)

	// Someone outside the organisation reads its public repository only.
	_, release, names, err = UserCallOrgRepos(t.Context(), stranger, org, []*repo_model.Repository{private, public})
	require.NoError(t, err)
	release()
	assert.Equal(t, []string{want(public)}, names)

	// Nothing readable is no call at all.
	_, _, names, err = UserCallOrgRepos(t.Context(), stranger, org, []*repo_model.Repository{private})
	require.ErrorIs(t, err, ErrForbidden)
	assert.Empty(t, names)
	_, _, _, err = UserCallOrgRepos(t.Context(), nil, org, []*repo_model.Repository{public})
	require.ErrorIs(t, err, ErrNoDoer)
	_, _, _, err = UserCallOrgRepos(t.Context(), owner, owner, []*repo_model.Repository{personal})
	require.Error(t, err, "an account is not an organisation")
}

// insightsOnly answers ListOwnerContributors; the other methods are not used.
type insightsOnly struct {
	ascv1.InsightsClient
	got  *ascv1.ListOwnerContributorsRequest
	resp *ascv1.ListOwnerContributorsResponse
}

func (c *insightsOnly) ListOwnerContributors(_ context.Context, in *ascv1.ListOwnerContributorsRequest, _ ...grpc.CallOption) (*ascv1.ListOwnerContributorsResponse, error) {
	c.got = in
	return c.resp, nil
}

func TestOwnerLandedSumsByLogin(t *testing.T) {
	c := &insightsOnly{resp: &ascv1.ListOwnerContributorsResponse{Contributors: []*ascv1.OwnerContributor{
		{Name: "Alice", Landed: 4},
		{Name: "bob", Landed: 1},
	}}}
	got, err := OwnerLanded(t.Context(), c, "Acme", []string{"acme/a", "acme/b"})
	require.NoError(t, err)
	assert.Equal(t, map[string]int{"alice": 4, "bob": 1}, got)
	assert.Equal(t, "acme", c.got.GetOwner())
	assert.Equal(t, []string{"acme/a", "acme/b"}, c.got.GetRepos())
	assert.Equal(t, uint32(366), c.got.GetDays())
	assert.Equal(t, ascv1.AuthorKind_AUTHOR_KIND_PERSON, c.got.GetKind())
}
