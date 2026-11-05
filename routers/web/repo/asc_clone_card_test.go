// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"html/template"
	"net/http"
	"testing"

	perm_model "forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/services/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHomeCloneCard(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	card := func(user int64, access perm_model.AccessMode, cookie bool) any {
		t.Helper()
		var got any
		runTrust(t, trustReq{path: "/user2/repo1", user: user, access: access}, func(ctx *context.Context) {
			if cookie {
				ctx.Req.AddCookie(&http.Cookie{Name: "asc_clone_hidden_1", Value: "1"})
			}
			prepareHomeCloneCard(ctx, ctx.Repo.Repository)
			got = ctx.Data["HomeCloneCard"]
		})
		return got
	}

	assert.Equal(t, true, card(2, perm_model.AccessModeWrite, false), "a writer with no change here")
	assert.Nil(t, card(1, perm_model.AccessModeWrite, false), "user1 has sent changes to repo1")
	assert.Nil(t, card(2, perm_model.AccessModeWrite, true), "hidden for this repository")
	assert.Nil(t, card(2, perm_model.AccessModeRead, false), "a reader cannot send a change")
	assert.Nil(t, card(0, perm_model.AccessModeRead, false), "a visitor")

	page := renderPart(t, "repo/home_cards", func(d map[string]any) {
		d["HomeCloneCard"] = true
		d["CsrfTokenHtml"] = template.HTML(`<input type="hidden" name="_csrf" value="t">`)
		d["CloneButtonOriginLink"] = &repo_model.CloneLink{HTTPS: "https://hub.example/user2/repo1.git"}
	})
	assert.Contains(t, page, `action="/user2/repo1/asc/clone-card/hide"`)
	assert.Contains(t, page, "asc_rp.clonecard_title")
	assert.Contains(t, page, "asc clone https://hub.example/user2/repo1.git")
	assert.Contains(t, page, `data-clipboard-text="asc clone https://hub.example/user2/repo1.git`+"\n"+`cd repo1`+"\n"+`asc create asc_rp.clonecard_branch -a -m &#34;asc_rp.clonecard_message&#34;`+"\n"+`asc submit"`)
	assert.Contains(t, page, `data-testid="clone-card-hide"`)
	assert.Contains(t, page, `data-testid="gitnote-off"`, "the bridge is off unless the hub says otherwise")

	withGit := renderPart(t, "repo/home_cards", func(d map[string]any) {
		d["HomeCloneCard"] = true
		d["AscGitAccess"] = true
		d["CloneButtonOriginLink"] = &repo_model.CloneLink{HTTPS: "https://hub.example/user2/repo1.git"}
	})
	assert.Contains(t, withGit, `<details class="asc-git-compat rp-gitnote">`, "the git note follows the asc commands in the card")
	assert.Contains(t, withGit, `data-clipboard-target="#rp-clonecard-git-url"`)
	assert.Contains(t, withGit, `id="rp-clonecard-git-url"`, "its copy target does not clash with the one in the clone sheet")
	assert.NotContains(t, withGit, `id="rp-git-clone-url"`)

	assert.NotContains(t, renderPart(t, "repo/home_cards", func(map[string]any) {}), "clone-card")

	hide := runTrust(t, trustReq{method: http.MethodPost, path: "/user2/repo1/asc/clone-card/hide", user: 2, access: perm_model.AccessModeRead}, AscCloneCardHide)
	assert.Equal(t, http.StatusSeeOther, hide.status)
	assert.Equal(t, "/user2/repo1", hide.loc)
	assert.Contains(t, hide.ctx.Resp.Header().Get("Set-Cookie"), "asc_clone_hidden_1=1")
}
