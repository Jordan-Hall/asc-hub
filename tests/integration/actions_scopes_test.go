// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	actions_model "forgejo.org/models/actions"
	repo_model "forgejo.org/models/repo"
	secret_model "forgejo.org/models/secret"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascscope"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Board RepoSettingsSecrets, "Scope chips": the dialogs tick which runs receive a secret, the rows show them, the
// update dialog starts from them and can change them without a new value, and the delete confirmation names them.
func TestActionsSecretsScopes(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo1 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1, OwnerID: user2.ID})
	sess := loginUser(t, user2.Name)
	link := "/" + repo1.FullName() + "/settings/actions/secrets"

	doc := NewHTMLParser(t, sess.MakeRequest(t, NewRequest(t, "GET", link), http.StatusOK).Body)
	doc.AssertElement(t, `#add-secret-modal input[type="hidden"][name="scopes_sent"]`, true)
	doc.AssertElement(t, `#secret-scope-checks[checked]`, true)
	doc.AssertElement(t, `#secret-scope-releases[checked]`, true)
	doc.AssertElement(t, `#secret-scope-agents`, true)
	doc.AssertElement(t, `#secret-scope-agents[checked]`, false)
	assert.Contains(t, doc.Find(`#add-secret-modal .asc-scope-boxes`).Text(), "Agents only receive it when Agent runs is ticked.")

	// None ticked is refused.
	resp := sess.MakeRequest(t, NewRequestWithURLValues(t, "POST", link, url.Values{"name": {"NONE"}, "data": {"v"}, "scopes_sent": {"1"}}), http.StatusBadRequest)
	assert.Contains(t, resp.Body.String(), "Tick at least one kind of run.")
	unittest.AssertNotExistsBean(t, &secret_model.Secret{RepoID: repo1.ID, Name: "NONE"})

	sess.MakeRequest(t, NewRequestWithURLValues(t, "POST", link, url.Values{
		"name": {"DEPLOY_TOKEN"}, "data": {"v1"}, "scopes_sent": {"1"}, "scopes": {"checks", "agents"},
	}), http.StatusOK)
	secret := unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{RepoID: repo1.ID, Name: "DEPLOY_TOKEN"})
	assert.Equal(t, ascscope.ScopeChecks|ascscope.ScopeAgents, secret.Scopes)

	// A client without the boxes gets the default.
	sess.MakeRequest(t, NewRequestWithValues(t, "POST", link, map[string]string{"name": "PLAIN", "data": "v"}), http.StatusOK)
	assert.Equal(t, ascscope.DefaultScope, unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{RepoID: repo1.ID, Name: "PLAIN"}).Scopes)

	doc = NewHTMLParser(t, sess.MakeRequest(t, NewRequest(t, "GET", link), http.StatusOK).Body)
	edit := doc.Find(fmt.Sprintf(`button[data-modal-form\.action$="/%d/edit"]`, secret.ID))
	require.Equal(t, 1, edit.Length())
	for word, want := range map[string]string{"checks": "true", "agents": "true", "releases": "false"} {
		got, _ := edit.Attr("data-modal-dialog-secret-scope-" + word)
		assert.Equal(t, want, got, word)
	}
	row := edit.Closest(".flex-item")
	chips := row.Find(`[data-testid="scope-chips"] li`)
	require.Equal(t, 2, chips.Length())
	assert.Equal(t, "Checks", chips.Eq(0).Text())
	assert.Equal(t, "Agent runs", chips.Eq(1).Text())
	confirm, _ := row.Find(`.link-action`).Attr("data-modal-confirm")
	assert.Equal(t, "Delete DEPLOY_TOKEN? Checks, Agent runs no longer receive it. This cannot be undone.", confirm)
	doc.AssertElement(t, `#dialog-secret-data[required]`, false)

	// The scopes change without the value.
	editLink := fmt.Sprintf("%s/%d/edit", link, secret.ID)
	sess.MakeRequest(t, NewRequestWithURLValues(t, "POST", editLink, url.Values{
		"name": {"DEPLOY_TOKEN"}, "data": {""}, "scopes_sent": {"1"}, "scopes": {"releases"},
	}), http.StatusOK)
	after := unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{ID: secret.ID})
	assert.Equal(t, ascscope.ScopeReleases, after.Scopes)
	assert.Equal(t, secret.Data, after.Data, "an empty value keeps the one stored")

	// The value changes and the scopes stay when the form has no boxes.
	sess.MakeRequest(t, NewRequestWithValues(t, "POST", editLink, map[string]string{"name": "DEPLOY_TOKEN", "data": "v2"}), http.StatusOK)
	after = unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{ID: secret.ID})
	assert.Equal(t, ascscope.ScopeReleases, after.Scopes)
	plain, err := after.GetDecryptedData()
	require.NoError(t, err)
	assert.Equal(t, "v2", plain)

	resp = sess.MakeRequest(t, NewRequestWithURLValues(t, "POST", editLink, url.Values{
		"name": {"DEPLOY_TOKEN"}, "data": {"v3"}, "scopes_sent": {"1"},
	}), http.StatusBadRequest)
	assert.Contains(t, resp.Body.String(), "Tick at least one kind of run.")
	assert.Equal(t, ascscope.ScopeReleases, unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{ID: secret.ID}).Scopes)
}

// The API sets values only: a new secret gets the default scopes and a changed one keeps its scopes.
func TestActionsSecretsAPIKeepsScopes(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo1 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1, OwnerID: user2.ID})
	token := getUserToken(t, user2.Name, "write:repository")
	link := fmt.Sprintf("/api/v1/repos/%s/actions/secrets/API_SECRET", repo1.FullName())

	MakeRequest(t, NewRequestWithJSON(t, "PUT", link, map[string]string{"data": "v1"}).AddTokenAuth(token), http.StatusCreated)
	secret := unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{RepoID: repo1.ID, Name: "API_SECRET"})
	assert.Equal(t, ascscope.DefaultScope, secret.Scopes)

	require.NoError(t, secret_model.SetScopes(t.Context(), secret.ID, ascscope.ScopeAgents))
	MakeRequest(t, NewRequestWithJSON(t, "PUT", link, map[string]string{"data": "v2"}).AddTokenAuth(token), http.StatusNoContent)
	assert.Equal(t, ascscope.ScopeAgents, unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{ID: secret.ID}).Scopes)
}

// Board RepoSettingsSecrets, "Variables table with Available to and updated-by".
func TestActionsVariablesScopes(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo1 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1, OwnerID: user2.ID})
	sess := loginUser(t, user2.Name)
	link := "/" + repo1.FullName() + "/settings/actions/variables"

	doc := NewHTMLParser(t, sess.MakeRequest(t, NewRequest(t, "GET", link), http.StatusOK).Body)
	add := doc.Find(`h4 button.show-modal[data-modal="#edit-variable-modal"]`)
	for word, want := range map[string]string{"checks": "true", "agents": "false", "releases": "true"} {
		got, _ := add.Attr("data-modal-dialog-variable-scope-" + word)
		assert.Equal(t, want, got, "Add starts from the default: "+word)
	}
	doc.AssertElement(t, `#edit-variable-modal input[name="scopes_sent"]`, true)
	doc.AssertElement(t, `#dialog-variable-scope-agents`, true)

	resp := sess.MakeRequest(t, NewRequestWithURLValues(t, "POST", link+"/new", url.Values{"name": {"NONE"}, "data": {"v"}, "scopes_sent": {"1"}}), http.StatusBadRequest)
	assert.Contains(t, resp.Body.String(), "Tick at least one kind of run.")
	unittest.AssertNotExistsBean(t, &actions_model.ActionVariable{RepoID: repo1.ID, Name: "NONE"})

	sess.MakeRequest(t, NewRequestWithURLValues(t, "POST", link+"/new", url.Values{
		"name": {"REGION"}, "data": {"eu"}, "scopes_sent": {"1"}, "scopes": {"agents"},
	}), http.StatusOK)
	v := unittest.AssertExistsAndLoadBean(t, &actions_model.ActionVariable{RepoID: repo1.ID, Name: "REGION"})
	assert.Equal(t, ascscope.ScopeAgents, v.Scopes)
	assert.Equal(t, user2.ID, v.UpdaterID)

	doc = NewHTMLParser(t, sess.MakeRequest(t, NewRequest(t, "GET", link), http.StatusOK).Body)
	updated := doc.Find(`[data-testid="variable-updated"]`)
	require.Equal(t, 1, updated.Length())
	assert.Contains(t, updated.Text(), "Updated by "+user2.GetDisplayName()+",")
	assert.Equal(t, 1, updated.Find("relative-time").Length())
	chips := doc.Find(`[data-testid="scope-chips"] li`)
	require.Equal(t, 1, chips.Length())
	assert.Equal(t, "Agent runs", chips.Text())
	edit := doc.Find(fmt.Sprintf(`button[data-modal-form\.action$="/%d/edit"]`, v.ID))
	got, _ := edit.Attr("data-modal-dialog-variable-scope-agents")
	assert.Equal(t, "true", got)
	confirm, _ := doc.Find(`.link-action[data-modal-confirm]`).Attr("data-modal-confirm")
	assert.Equal(t, "Delete REGION? Agent runs no longer receive it. This cannot be undone.", confirm)

	// The admin edits it: the scopes change and the admin becomes who set it.
	admin := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	adminSess := loginUser(t, admin.Name)
	adminSess.MakeRequest(t, NewRequestWithURLValues(t, "POST", fmt.Sprintf("%s/%d/edit", link, v.ID), url.Values{
		"name": {"REGION"}, "data": {"us"}, "scopes_sent": {"1"}, "scopes": {"checks", "releases"},
	}), http.StatusOK)
	v = unittest.AssertExistsAndLoadBean(t, &actions_model.ActionVariable{ID: v.ID})
	assert.Equal(t, ascscope.DefaultScope, v.Scopes)
	assert.Equal(t, admin.ID, v.UpdaterID)
	assert.Equal(t, "us", v.Data)

	// The API keeps the scopes and records who set it.
	token := getUserToken(t, user2.Name, "write:repository")
	MakeRequest(t, NewRequestWithJSON(t, "PUT", fmt.Sprintf("/api/v1/repos/%s/actions/variables/REGION", repo1.FullName()), map[string]string{"value": "ap"}).
		AddTokenAuth(token), http.StatusNoContent)
	v = unittest.AssertExistsAndLoadBean(t, &actions_model.ActionVariable{ID: v.ID})
	assert.Equal(t, ascscope.DefaultScope, v.Scopes)
	assert.Equal(t, user2.ID, v.UpdaterID)
	assert.True(t, strings.EqualFold("ap", v.Data))
}
