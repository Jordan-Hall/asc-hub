// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"strings"
	"testing"

	asymkey_model "forgejo.org/models/asymkey"
	"forgejo.org/models/db"
	"forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func accessRows() []*repo_model.Collaborator {
	person := &user_model.User{ID: 4, Name: "user4", LowerName: "user4", Type: user_model.UserTypeIndividual}
	agent := &user_model.User{ID: 90, Name: "builder", LowerName: "builder", Type: user_model.UserTypeBot}
	return []*repo_model.Collaborator{
		{User: person, Collaboration: &repo_model.Collaboration{Mode: perm.AccessModeWrite}},
		{User: agent, Collaboration: &repo_model.Collaboration{Mode: perm.AccessModeWrite}},
	}
}

func TestAccessAgentRows(t *testing.T) {
	render := func(scope *asctrust.AgentScope) string {
		data := map[string]any{"Collaborators": accessRows(), "PageIsSettingsCollaboration": true}
		if scope != nil {
			data["AgentScope"] = scope
		}
		return renderSettingsPage(t, t.TempDir(), "access", tplCollaboration, data)
	}

	t.Run("an agent row has the chip, the violet mark and what the policy lets it write", func(t *testing.T) {
		page := render(&asctrust.AgentScope{Paths: "src/ and tests/"})
		assert.Contains(t, page, `aria-label="builder" class="rp-agent-mark"`)
		assert.NotContains(t, page, `aria-label="user4" class="rp-agent-mark"`, "a person has no mark")
		assert.Contains(t, page, `<span class="ds-chip ds-chip-agent">Agent</span>`)
		assert.Contains(t, page, `<div class="flex-item-body rp-agent-scope">Limited by policy to src/ and tests/</div>`)
		assert.Equal(t, 1, strings.Count(page, "rp-agent-scope"), "only the agent row says it")
	})

	t.Run("agents switched off by the policy", func(t *testing.T) {
		page := render(&asctrust.AgentScope{Off: true})
		assert.Contains(t, page, `<div class="flex-item-body rp-agent-scope">Switched off by policy</div>`)
	})

	t.Run("no policy answer: the row says nothing more", func(t *testing.T) {
		page := render(nil)
		assert.Contains(t, page, `class="rp-agent-mark"`)
		assert.NotContains(t, page, "rp-agent-scope")
	})
}

// Board RepoSettingsAccess: the deploy keys sit on the access page, each with its title, fingerprint, read-only or
// read-write, who added it and when it was last used, and Add deploy key opens a dialog.
func TestAccessDeployKeys(t *testing.T) {
	now := timeutil.TimeStampNow()
	keys := []*asymkey_model.DeployKey{
		{ID: 1, Name: "ci <deploy>", Fingerprint: "SHA256:abc", Mode: perm.AccessModeRead, AddedByID: 4, CreatedUnix: now - 3600, UpdatedUnix: now - 3600},
		{ID: 2, Name: "mirror", Fingerprint: "SHA256:def", Mode: perm.AccessModeWrite, CreatedUnix: now - 7200, UpdatedUnix: now - 60, HasUsed: true, HasRecentActivity: true},
	}
	adders := map[int64]*user_model.User{4: {ID: 4, Name: "user4", LowerName: "user4"}}
	render := func(extra map[string]any) string {
		data := map[string]any{"Collaborators": accessRows(), "PageIsSettingsCollaboration": true, "Deploykeys": keys, "DeployKeyAdders": adders}
		for k, v := range extra {
			data[k] = v
		}
		return renderSettingsPage(t, t.TempDir(), "access-keys", tplCollaboration, data)
	}

	page := render(nil)
	assert.Equal(t, 2, strings.Count(page, `data-testid="asc-deploy-key"`))
	assert.Contains(t, page, "ci &lt;deploy&gt;")
	assert.Contains(t, page, "<code>SHA256:abc</code>")
	assert.Contains(t, page, `<span class="ds-chip">Read-only</span>`)
	assert.Contains(t, page, `<span class="ds-chip ds-chip-warn">Read-write</span>`)
	assert.Contains(t, page, `data-testid="asc-deploy-key-adder">Added by <a href="/user4">user4</a> on `)
	assert.Equal(t, 1, strings.Count(page, "asc-deploy-key-adder"), "a key added before it was recorded names no one")
	assert.Contains(t, page, "No recent activity")
	assert.Contains(t, page, "Last used ")
	assert.Contains(t, page, `data-url="/user2/repo1/settings/keys/delete?back=access"`)
	assert.Contains(t, page, `<dialog id="dk-add-dlg"`)
	assert.Contains(t, page, `action="/user2/repo1/settings/keys?back=access"`)
	assert.Contains(t, page, `action="/user2/repo1/settings/collaboration"`, "the people form posts to its own route whatever the page URL")
	assert.NotContains(t, page, "data-dk-add-reopen")

	t.Run("on /settings/keys the actions come back to the keys page", func(t *testing.T) {
		page := render(map[string]any{"PageIsSettingsCollaboration": false, "PageIsSettingsKeys": true})
		assert.Contains(t, page, `action="/user2/repo1/settings/keys"`)
		assert.Contains(t, page, `data-url="/user2/repo1/settings/keys/delete"`)
	})
	t.Run("a refused add opens the dialog again", func(t *testing.T) {
		page := render(map[string]any{"HasError": true, "Err_Title": true, "title": "ci"})
		assert.Contains(t, page, "data-dk-add-reopen")
		assert.Contains(t, page, `name="title" value="ci" required aria-invalid="true"`)
	})
	t.Run("no keys", func(t *testing.T) {
		page := render(map[string]any{"Deploykeys": []*asymkey_model.DeployKey{}})
		assert.Contains(t, page, `data-testid="asc-deploy-keys-empty">There are no deploy keys yet.`)
	})
	t.Run("ssh switched off: no dialog", func(t *testing.T) {
		page := render(map[string]any{"DisableSSH": true})
		assert.NotContains(t, page, "dk-add-dlg")
		assert.NotContains(t, page, "data-dk-add-open")
	})
}

// Both routes render the access page, with the deploy keys and the person who added each.
func TestAccessDeployKeysRoutes(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	const content = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHnbqnHNKh/Td/1O6t9Q8qdJmitCAApnvHImHV8TkptX ci@example.org"
	_, err := asymkey_model.AddDeployKeyBy(db.DefaultContext, 1, 2, "ci", content, true)
	require.NoError(t, err)

	for path, run := range map[string]func(ctx *context.Context){"collaboration": Collaboration, "keys": DeployKeys} {
		ctx, resp := contexttest.MockContext(t, "GET /user2/repo1/settings/"+path)
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 1)
		run(ctx)
		assert.Equal(t, 200, resp.Code, path)
		assert.NotNil(t, ctx.Data["Collaborators"], path)
		keys := ctx.Data["Deploykeys"].([]*asymkey_model.DeployKey)
		require.Len(t, keys, 1, path)
		adders := ctx.Data["DeployKeyAdders"].(map[int64]*user_model.User)
		assert.Equal(t, "user2", adders[keys[0].AddedByID].Name, path)
	}
}
