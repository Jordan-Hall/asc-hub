// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"testing"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascidentity"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What the organisation requires, what is in force and what the organisation made stricter are said in sentences, and a name from
// the server is cleaned.
func TestBuildOrgPolicyView(t *testing.T) {
	v := BuildOrgPolicyView("acme", &ascv1.ResolveMergePolicyResponse{
		Org:       &ascv1.MergeDefaults{Set: true, RequireApprovals: 2, RequireChecks: []string{"build"}, RequireSigned: true, HumanApproval: true},
		Effective: &ascv1.MergeDefaults{Set: true, RequireApprovals: 2, RequireChecks: []string{"build", "lint\x00"}, RequireSigned: true, HumanApproval: true, Queue: true},
		Tightened: []string{"require_approvals", "unknown_field"},
	})
	assert.True(t, v.Set)
	assert.Equal(t, []string{"2 approvals", "green build check", "signed changes", "approval by a person", "no approving your own change"}, v.Required)
	assert.Contains(t, v.Effective, "landing through the merge queue")
	assert.Contains(t, v.Effective, "2 approvals")
	for _, s := range v.Effective {
		assert.NotContains(t, s, "\x00")
	}
	assert.Equal(t, []string{"the number of approvals"}, v.Tightened, "only the fields the page knows are named")

	none := BuildOrgPolicyView("acme", &ascv1.ResolveMergePolicyResponse{Org: &ascv1.MergeDefaults{}, Effective: &ascv1.MergeDefaults{RequireApprovals: 1}})
	assert.False(t, none.Set)
	assert.Empty(t, none.Required)
	assert.Equal(t, "acme", BuildOrgPolicyView("acme", nil).Org)
}

func TestMergeDefaultsOfTheRepositoryFile(t *testing.T) {
	assert.Nil(t, MergeDefaultsOf(nil))
	m := MergeDefaultsOf(&ascv1.GetPolicyResponse{Trunk: &ascv1.PolicyTrunk{RequireApprovals: 3, RequireChecks: []string{"ci"}, AllowForce: true}})
	assert.Equal(t, uint32(3), m.GetRequireApprovals())
	assert.Equal(t, []string{"ci"}, m.GetRequireChecks())
	assert.True(t, m.GetAllowForce())
}

// An administrator of a repository of an organisation may read the organisation's defaults, as a reader of that organisation; anyone
// else, and a repository of a person, may not.
func TestUserCallOrgPolicy(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	useForwardedKey(t)
	orgRepo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 3})
	require.NoError(t, orgRepo.LoadOwner(t.Context()))
	require.True(t, orgRepo.Owner.IsOrganization())
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	stranger := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})

	ctx, release, err := UserCallOrgPolicy(t.Context(), owner, orgRepo)
	require.NoError(t, err)
	release()
	_, role, verr := claimIn(t, ctx, "org:"+orgRepo.Owner.LowerName)
	require.NoError(t, verr)
	assert.Equal(t, ascidentity.RoleRead, role, "a read of the organisation, whatever the person is in the repository")

	_, _, err = UserCallOrgPolicy(t.Context(), stranger, orgRepo)
	assert.ErrorIs(t, err, ErrForbidden)
	_, _, err = UserCallOrgPolicy(t.Context(), nil, orgRepo)
	assert.ErrorIs(t, err, ErrNoDoer)
	personal := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	_, _, err = UserCallOrgPolicy(t.Context(), owner, personal)
	assert.ErrorIs(t, err, ErrForbidden)
}
