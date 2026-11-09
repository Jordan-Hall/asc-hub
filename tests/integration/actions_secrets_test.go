// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	auth_model "forgejo.org/models/auth"
	repo_model "forgejo.org/models/repo"
	secret_model "forgejo.org/models/secret"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	api "forgejo.org/modules/structs"
	app_context "forgejo.org/services/context"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActionsSecretsCreateSecret(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo1 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1, OwnerID: user2.ID})
	org3 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 3, Type: user_model.UserTypeOrganization})

	sess := loginUser(t, user2.Name)

	testCases := []struct {
		name    string
		url     string
		ownerID int64
		repoID  int64
	}{
		{
			name:    "User",
			url:     "/user/settings/actions/secrets",
			ownerID: user2.ID,
			repoID:  0,
		},
		{
			name:    "Repository",
			url:     "/" + repo1.FullName() + "/settings/actions/secrets",
			ownerID: 0,
			repoID:  repo1.ID,
		},
		{
			name:    "Organization",
			url:     "/org/" + org3.Name + "/settings/actions/secrets",
			ownerID: org3.ID,
			repoID:  0,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			req := NewRequestWithValues(t, "POST", testCase.url, map[string]string{
				"name": "my_secret",
				"data": "   \r\n\tSecrët dåtä\\   \r\n",
			})
			sess.MakeRequest(t, req, http.StatusOK)

			flashCookie := sess.GetCookie(app_context.CookieNameFlash)
			assert.NotNil(t, flashCookie)
			assert.Equal(t, "success%3DThe%2Bsecret%2B%2522MY_SECRET%2522%2Bhas%2Bbeen%2Badded.", flashCookie.Value)

			secret := unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{OwnerID: testCase.ownerID, RepoID: testCase.repoID, Name: "MY_SECRET"})
			assert.Equal(t, "MY_SECRET", secret.Name)

			value, err := secret.GetDecryptedData()
			require.NoError(t, err)
			assert.Equal(t, "   \n\tSecrët dåtä\\   \n", value)
		})
	}
}

func TestActionsSecretsCreateSecretRejectsNameMatchingExistingSecret(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo1 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1, OwnerID: user2.ID})
	org3 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 3, Type: user_model.UserTypeOrganization})

	sess := loginUser(t, user2.Name)

	testCases := []struct {
		name    string
		url     string
		ownerID int64
		repoID  int64
	}{
		{
			name:    "User",
			url:     "/user/settings/actions/secrets",
			ownerID: user2.ID,
			repoID:  0,
		},
		{
			name:    "Repository",
			url:     "/" + repo1.FullName() + "/settings/actions/secrets",
			ownerID: 0,
			repoID:  repo1.ID,
		},
		{
			name:    "Organization",
			url:     "/org/" + org3.Name + "/settings/actions/secrets",
			ownerID: org3.ID,
			repoID:  0,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			req := NewRequestWithValues(t, "POST", testCase.url, map[string]string{
				"name": "my_secret",
				"data": "original value",
			})
			sess.MakeRequest(t, req, http.StatusOK)

			flashCookie := sess.GetCookie(app_context.CookieNameFlash)
			assert.NotNil(t, flashCookie)
			assert.Equal(t, "success%3DThe%2Bsecret%2B%2522MY_SECRET%2522%2Bhas%2Bbeen%2Badded.", flashCookie.Value)

			secret := unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{OwnerID: testCase.ownerID, RepoID: testCase.repoID, Name: "MY_SECRET"})
			assert.Equal(t, "MY_SECRET", secret.Name)

			value, err := secret.GetDecryptedData()
			require.NoError(t, err)
			assert.Equal(t, "original value", value)

			// Try to create a new secret with the name but another value.
			req = NewRequestWithValues(t, "POST", testCase.url, map[string]string{
				"name": "my_secret",
				"data": "changed value",
			})
			sess.MakeRequest(t, req, http.StatusBadRequest)

			// Verify that the original secret has not been changed.
			secret = unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{OwnerID: testCase.ownerID, RepoID: testCase.repoID, Name: "MY_SECRET"})
			value, err = secret.GetDecryptedData()
			require.NoError(t, err)

			assert.Equal(t, "MY_SECRET", secret.Name)
			assert.Equal(t, "original value", value)
		})
	}
}

func TestActionsSecretsEditSecret(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo1 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1, OwnerID: user2.ID})
	org3 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 3, Type: user_model.UserTypeOrganization})

	sess := loginUser(t, user2.Name)

	testCases := []struct {
		name    string
		url     string
		ownerID int64
		repoID  int64
	}{
		{
			name:    "User",
			url:     "/user/settings/actions/secrets",
			ownerID: user2.ID,
			repoID:  0,
		},
		{
			name:    "Repository",
			url:     "/" + repo1.FullName() + "/settings/actions/secrets",
			ownerID: 0,
			repoID:  repo1.ID,
		},
		{
			name:    "Organization",
			url:     "/org/" + org3.Name + "/settings/actions/secrets",
			ownerID: org3.ID,
			repoID:  0,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			req := NewRequestWithValues(t, "POST", testCase.url, map[string]string{
				"name": "TEST_SECRET",
				"data": "value",
			})
			sess.MakeRequest(t, req, http.StatusOK)

			flashCookie := sess.GetCookie(app_context.CookieNameFlash)
			assert.NotNil(t, flashCookie)
			assert.Equal(t, "success%3DThe%2Bsecret%2B%2522TEST_SECRET%2522%2Bhas%2Bbeen%2Badded.", flashCookie.Value)

			secret := unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{OwnerID: testCase.ownerID, RepoID: testCase.repoID, Name: "TEST_SECRET"})

			req = NewRequestWithValues(t, "POST", fmt.Sprintf("%s/%d/edit", testCase.url, secret.ID), map[string]string{
				"name": secret.Name,
				"data": "   \r\n\tSecrët dåtä\\   \r\n",
			})
			sess.MakeRequest(t, req, http.StatusOK)

			flashCookie = sess.GetCookie(app_context.CookieNameFlash)
			assert.NotNil(t, flashCookie)
			assert.Equal(t, "success%3DThe%2Bsecret%2B%2522TEST_SECRET%2522%2Bhas%2Bbeen%2Bupdated.", flashCookie.Value)

			updatedSecret := unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{ID: secret.ID})
			decryptedValue, err := updatedSecret.GetDecryptedData()
			require.NoError(t, err)

			assert.Equal(t, secret.Name, updatedSecret.Name)
			assert.Equal(t, "   \n\tSecrët dåtä\\   \n", decryptedValue)
		})
	}
}

// An edit replaces the value only: the name is locked, and an edit without a value is refused.
func TestActionsSecretsEditSecretKeepsItsName(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo1 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1, OwnerID: user2.ID})
	org3 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 3, Type: user_model.UserTypeOrganization})

	sess := loginUser(t, user2.Name)

	testCases := []struct {
		name    string
		url     string
		ownerID int64
		repoID  int64
	}{
		{
			name:    "User",
			url:     "/user/settings/actions/secrets",
			ownerID: user2.ID,
			repoID:  0,
		},
		{
			name:    "Repository",
			url:     "/" + repo1.FullName() + "/settings/actions/secrets",
			ownerID: 0,
			repoID:  repo1.ID,
		},
		{
			name:    "Organization",
			url:     "/org/" + org3.Name + "/settings/actions/secrets",
			ownerID: org3.ID,
			repoID:  0,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			req := NewRequestWithValues(t, "POST", testCase.url, map[string]string{
				"name": "TEST_SECRET",
				"data": "   \r\n\tSecrët dåtä\\   \r\n",
			})
			sess.MakeRequest(t, req, http.StatusOK)

			flashCookie := sess.GetCookie(app_context.CookieNameFlash)
			assert.NotNil(t, flashCookie)
			assert.Equal(t, "success%3DThe%2Bsecret%2B%2522TEST_SECRET%2522%2Bhas%2Bbeen%2Badded.", flashCookie.Value)

			secret := unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{OwnerID: testCase.ownerID, RepoID: testCase.repoID, Name: "TEST_SECRET"})

			req = NewRequestWithValues(t, "POST", fmt.Sprintf("%s/%d/edit", testCase.url, secret.ID), map[string]string{
				"name": "TEST_SECRET",
				"data": "",
			})
			sess.MakeRequest(t, req, http.StatusBadRequest)

			req = NewRequestWithValues(t, "POST", fmt.Sprintf("%s/%d/edit", testCase.url, secret.ID), map[string]string{
				"name": "changed_secret",
				"data": "new value",
			})
			sess.MakeRequest(t, req, http.StatusOK)

			flashCookie = sess.GetCookie(app_context.CookieNameFlash)
			assert.NotNil(t, flashCookie)
			assert.Equal(t, "success%3DThe%2Bsecret%2B%2522TEST_SECRET%2522%2Bhas%2Bbeen%2Bupdated.", flashCookie.Value)

			updatedSecret := unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{ID: secret.ID})
			decryptedValue, err := updatedSecret.GetDecryptedData()
			require.NoError(t, err)

			assert.Equal(t, "TEST_SECRET", updatedSecret.Name)
			assert.Equal(t, "new value", decryptedValue)
		})
	}
}

// A name sent with an edit, even one the rules refuse, leaves the locked name as it is.
func TestActionsSecretsEditSecretIgnoresAnInvalidName(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo1 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1, OwnerID: user2.ID})
	org3 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 3, Type: user_model.UserTypeOrganization})

	sess := loginUser(t, user2.Name)

	testCases := []struct {
		name    string
		url     string
		ownerID int64
		repoID  int64
	}{
		{
			name:    "User",
			url:     "/user/settings/actions/secrets",
			ownerID: user2.ID,
			repoID:  0,
		},
		{
			name:    "Repository",
			url:     "/" + repo1.FullName() + "/settings/actions/secrets",
			ownerID: 0,
			repoID:  repo1.ID,
		},
		{
			name:    "Organization",
			url:     "/org/" + org3.Name + "/settings/actions/secrets",
			ownerID: org3.ID,
			repoID:  0,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			req := NewRequestWithValues(t, "POST", testCase.url, map[string]string{
				"name": "TEST_SECRET",
				"data": "value",
			})
			sess.MakeRequest(t, req, http.StatusOK)

			flashCookie := sess.GetCookie(app_context.CookieNameFlash)
			assert.NotNil(t, flashCookie)
			assert.Equal(t, "success%3DThe%2Bsecret%2B%2522TEST_SECRET%2522%2Bhas%2Bbeen%2Badded.", flashCookie.Value)

			secret := unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{OwnerID: testCase.ownerID, RepoID: testCase.repoID, Name: "TEST_SECRET"})

			req = NewRequestWithValues(t, "POST", fmt.Sprintf("%s/%d/edit", testCase.url, secret.ID), map[string]string{
				"name": "FORGEJO_IS_INVALID",
				"data": "other value",
			})
			sess.MakeRequest(t, req, http.StatusOK)
			assert.Equal(t, "TEST_SECRET", unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{ID: secret.ID}).Name)
		})
	}
}

func TestActionsSecretsRemoveSecret(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo1 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1, OwnerID: user2.ID})
	org3 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 3, Type: user_model.UserTypeOrganization})

	sess := loginUser(t, user2.Name)

	testCases := []struct {
		name    string
		url     string
		ownerID int64
		repoID  int64
	}{
		{
			name:    "User",
			url:     "/user/settings/actions/secrets",
			ownerID: user2.ID,
			repoID:  0,
		},
		{
			name:    "Repository",
			url:     "/" + repo1.FullName() + "/settings/actions/secrets",
			ownerID: 0,
			repoID:  repo1.ID,
		},
		{
			name:    "Organization",
			url:     "/org/" + org3.Name + "/settings/actions/secrets",
			ownerID: org3.ID,
			repoID:  0,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			req := NewRequestWithValues(t, "POST", testCase.url, map[string]string{
				"name": "TEST_SECRET",
				"data": "value",
			})
			sess.MakeRequest(t, req, http.StatusOK)

			flashCookie := sess.GetCookie(app_context.CookieNameFlash)
			assert.NotNil(t, flashCookie)
			assert.Equal(t, "success%3DThe%2Bsecret%2B%2522TEST_SECRET%2522%2Bhas%2Bbeen%2Badded.", flashCookie.Value)

			secret := unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{OwnerID: testCase.ownerID, RepoID: testCase.repoID, Name: "TEST_SECRET"})

			req = NewRequest(t, "POST", fmt.Sprintf("%s/%d/delete", testCase.url, secret.ID))
			sess.MakeRequest(t, req, http.StatusOK)

			flashCookie = sess.GetCookie(app_context.CookieNameFlash)
			assert.NotNil(t, flashCookie)
			assert.Equal(t, "success%3DThe%2Bsecret%2Bhas%2Bbeen%2Bremoved.", flashCookie.Value)

			unittest.AssertNotExistsBean(t, secret)
		})
	}
}

// The page with no secrets shows the empty state with its Add button; with one, the delete confirmation names it and
// the update dialog locks the name.
func TestActionsSecretsPage(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo1 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1, OwnerID: user2.ID})
	sess := loginUser(t, user2.Name)
	url := "/" + repo1.FullName() + "/settings/actions/secrets"

	doc := NewHTMLParser(t, sess.MakeRequest(t, NewRequest(t, "GET", url), http.StatusOK).Body)
	doc.AssertElement(t, `[data-testid="secrets-empty"] button.show-modal[data-modal="#add-secret-modal"]`, true)
	assert.Contains(t, doc.Find(`[data-testid="secrets-redacted"]`).Text(), "its log shows *** in place of the value", "the page says a log never shows a value")

	sess.MakeRequest(t, NewRequestWithValues(t, "POST", url, map[string]string{"name": "DEPLOY_TOKEN", "data": "v"}), http.StatusOK)
	doc = NewHTMLParser(t, sess.MakeRequest(t, NewRequest(t, "GET", url), http.StatusOK).Body)
	doc.AssertElement(t, `[data-testid="secrets-empty"]`, false)
	confirm, ok := doc.Find(`.link-action[data-modal-confirm]`).Attr("data-modal-confirm")
	assert.True(t, ok)
	assert.Contains(t, confirm, "DEPLOY_TOKEN")
	doc.AssertElement(t, `#dialog-secret-name[readonly]`, true)
	doc.AssertElement(t, `#dialog-secret-data[required]`, false)
}

// Board RepoSettingsSecrets: each row says who last set the value and when.
func TestActionsSecretsPageShowsWhoUpdated(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo1 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1, OwnerID: user2.ID})
	sess := loginUser(t, user2.Name)
	url := "/" + repo1.FullName() + "/settings/actions/secrets"

	sess.MakeRequest(t, NewRequestWithValues(t, "POST", url, map[string]string{"name": "DEPLOY_TOKEN", "data": "v"}), http.StatusOK)
	secret := unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{RepoID: repo1.ID, Name: "DEPLOY_TOKEN"})
	assert.Equal(t, user2.ID, secret.UpdaterID)
	assert.NotZero(t, secret.UpdatedUnix)

	// A secret set before updaters were recorded shows the time alone.
	_, err := secret_model.InsertEncryptedSecret(t.Context(), 0, repo1.ID, "OLD_TOKEN", "v")
	require.NoError(t, err)

	doc := NewHTMLParser(t, sess.MakeRequest(t, NewRequest(t, "GET", url), http.StatusOK).Body)
	rows := doc.Find(`[data-testid="secret-updated"]`)
	require.Equal(t, 2, rows.Length())
	texts := []string{rows.Eq(0).Text(), rows.Eq(1).Text()}
	var by, alone string
	for _, s := range texts {
		if strings.Contains(s, "Updated by") {
			by = s
		} else {
			alone = s
		}
	}
	assert.Contains(t, by, "Updated by "+user2.GetDisplayName()+",")
	assert.Contains(t, alone, "Updated")
	assert.NotContains(t, alone, "by")
	assert.Equal(t, 2, rows.Find("relative-time").Length())
}

func TestActionsSecretsAPIRecordsWhoUpdated(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo1 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1, OwnerID: user2.ID})
	token := getUserToken(t, user2.Name, auth_model.AccessTokenScopeWriteRepository)

	url := fmt.Sprintf("/api/v1/repos/%s/actions/secrets/API_TOKEN", repo1.FullName())
	MakeRequest(t, NewRequestWithJSON(t, "PUT", url, api.CreateOrUpdateSecretOption{Data: "one"}).AddTokenAuth(token), http.StatusCreated)
	secret := unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{RepoID: repo1.ID, Name: "API_TOKEN"})
	assert.Equal(t, user2.ID, secret.UpdaterID)
	first := secret.UpdatedUnix
	assert.NotZero(t, first)

	MakeRequest(t, NewRequestWithJSON(t, "PUT", url, api.CreateOrUpdateSecretOption{Data: "two"}).AddTokenAuth(token), http.StatusNoContent)
	secret = unittest.AssertExistsAndLoadBean(t, &secret_model.Secret{RepoID: repo1.ID, Name: "API_TOKEN"})
	assert.Equal(t, user2.ID, secret.UpdaterID)
	assert.GreaterOrEqual(t, secret.UpdatedUnix, first)
}
