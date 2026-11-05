// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"strings"
	"testing"

	"forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/services/asctrust"

	"github.com/stretchr/testify/assert"
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
