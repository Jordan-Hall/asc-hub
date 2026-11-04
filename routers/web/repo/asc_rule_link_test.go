// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/url"
	"strconv"
	"testing"

	git_model "forgejo.org/models/git"
	perm_model "forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProtectedRefRefusalOpensTheRule(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	require.NoError(t, git_model.UpdateProtectBranch(t.Context(), repo, &git_model.ProtectedBranch{RepoID: 1, RuleName: "branch*"}, git_model.WhitelistOptions{}))
	tag := &git_model.ProtectedTag{RepoID: 1, NamePattern: "v*"}
	require.NoError(t, git_model.InsertProtectedTag(t.Context(), tag))

	as := func(mode perm_model.AccessMode) *context.Context {
		ctx, _ := contexttest.MockContext(t, "user2/repo1/branches")
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 1)
		ctx.Repo.Permission.AccessMode = mode
		return ctx
	}

	admin := as(perm_model.AccessModeAdmin)
	assert.Equal(t, "/user2/repo1/settings/branches/edit?"+url.Values{"rule_name": {"branch*"}}.Encode(), protectedBranchRuleLink(admin, "branch2"))
	assert.Empty(t, protectedBranchRuleLink(admin, "feature"), "no rule matches")
	assert.Equal(t, "/user2/repo1/settings/tags/"+strconv.FormatInt(tag.ID, 10), protectedTagRuleLink(admin, "v1.1"))
	assert.Empty(t, protectedTagRuleLink(admin, "nightly"))

	writer := as(perm_model.AccessModeWrite)
	assert.Empty(t, protectedBranchRuleLink(writer, "branch2"), "only a person who may change the rule gets the link")
	assert.Empty(t, protectedTagRuleLink(writer, "v1.1"))

	msg := ascWithRuleLink(admin, "Branch is protected.", `/user2/repo1/settings/branches/edit?rule_name=a"b`)
	assert.Equal(t, `Branch is protected. <a href="/user2/repo1/settings/branches/edit?rule_name=a&#34;b" data-testid="open-rule">asc_rp.open_rule</a>`, string(msg))
	assert.Equal(t, "Branch is protected.", string(ascWithRuleLink(admin, "Branch is protected.", "")))

	t.Run("delete refused", func(t *testing.T) {
		ctx, _ := contexttest.MockContext(t, "POST /user2/repo1/branches/delete?name=branch2")
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 1)
		ctx.Repo.Permission.AccessMode = perm_model.AccessModeAdmin
		DeleteBranchPost(ctx)
		assert.Contains(t, ctx.Flash.ErrorMsg, "repo.branch.protected_deletion_failed")
		assert.Contains(t, ctx.Flash.ErrorMsg, `data-testid="open-rule"`)
		assert.Contains(t, ctx.Flash.ErrorMsg, "rule_name=branch%2A")
	})
}

func TestRulesEmptyState(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	empty := renderPart(t, "repo/settings/rules_empty", func(map[string]any) {})
	assert.Contains(t, empty, `data-testid="rules-empty"`)
	assert.Contains(t, empty, "repo.settings.asc_rules.empty_title")
	assert.Contains(t, empty, `action="/user2/repo1/settings/branches/edit"`)
	assert.Contains(t, empty, `name="rule_name" value="master"`)
	assert.Contains(t, empty, `data-testid="rules-protect-default"`)

	bare := renderPart(t, "repo/settings/rules_empty", func(d map[string]any) {
		r := *d["Repository"].(*repo_model.Repository)
		r.IsEmpty = true
		d["Repository"] = &r
	})
	assert.Contains(t, bare, "repo.settings.asc_rules.empty_title")
	assert.NotContains(t, bare, "rules-protect-default", "an empty repository has no branch to protect yet")
}
