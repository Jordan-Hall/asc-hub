// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"testing"
	"time"

	"forgejo.org/models/unittest"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Board RepoSettingsPolicy, file header: "Last changed by <name>, <time> ago, in change <id>".
func TestAscPolicyLastChange(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx, _ := contexttest.MockContext(t, "GET /user2/repo1/settings/asc/policy")
	contexttest.LoadRepo(t, ctx, 1)
	contexttest.LoadGitRepo(t, ctx)
	defer ctx.Repo.GitRepo.Close()
	repo := ctx.Repo.Repository

	t.Run("the newest change of the default branch that touched the file", func(t *testing.T) {
		c := ascPolicyLastChange(ctx, repo, "README.md")
		require.NotNil(t, c)
		head, err := ctx.Repo.GitRepo.GetBranchCommit(repo.DefaultBranch)
		require.NoError(t, err)
		want, err := head.GetCommitByPath("README.md")
		require.NoError(t, err)
		assert.Equal(t, want.Author.Name, c.Who)
		assert.True(t, want.Author.When.Equal(c.When))
		assert.Equal(t, want.ID.String()[:10], c.Short)
		assert.Equal(t, repo.Link()+"/commit/"+want.ID.String(), c.URL)
	})
	t.Run("a file that was never committed has no line", func(t *testing.T) {
		assert.Nil(t, ascPolicyLastChange(ctx, repo, ascPolicyFile))
	})
	t.Run("the header shows it", func(t *testing.T) {
		p := &ascPolicyPage{
			View:       &asctrust.PolicyView{Present: true, Usable: true, Source: "0123456789ab"},
			PolicyURL:  "/user2/repo1/settings/asc/policy",
			ToggleOps:  map[string]string{},
			LastChange: &ascPolicyChange{Who: "Ada <b>Lovelace</b>", When: time.Now().Add(-5 * 24 * time.Hour), Short: "65f1bf27bc", URL: "/user2/repo1/commit/65f1bf27bc3bf70f64657658635e66094edbcb4d"},
		}
		html := renderSettingsPage(t, t.TempDir(), "policy-last-change", tplAscPolicy, map[string]any{"AscPolicy": p})
		assert.Contains(t, html, `data-testid="asc-policy-last-change">Last changed by Ada &lt;b&gt;Lovelace&lt;/b&gt;, `)
		assert.Contains(t, html, `, in change <a href="/user2/repo1/commit/65f1bf27bc3bf70f64657658635e66094edbcb4d"><code>65f1bf27bc</code></a>`)
	})
}
