// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/timeutil"
	"forgejo.org/tests"
	"forgejo.org/tests/forgery"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscRepoRenameRedirect(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	repo := forgery.CreateRepository(t, nil, nil)
	other := forgery.CreateRepository(t, repo.Owner, nil)
	session := loginUser(t, repo.Owner.Name)
	owner := repo.Owner.Name
	oldName := repo.Name
	settings := "/" + owner + "/" + oldName + "/settings"

	t.Run("the name field says how long the old address redirects", func(t *testing.T) {
		resp := session.MakeRequest(t, NewRequest(t, "GET", settings), http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)
		assert.Equal(t, "Changing the name redirects the old address for 90 days.", strings.TrimSpace(doc.Find("#repo-name-hint").Text()))
		assert.Equal(t, 0, doc.Find("#repo-name-err").Length())
	})

	t.Run("a taken name is said under the name field", func(t *testing.T) {
		req := NewRequestWithValues(t, "POST", settings, map[string]string{"action": "update", "repo_name": other.Name})
		resp := session.MakeRequest(t, req, http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)
		errMsg := doc.Find("#repo-name-err")
		assert.Equal(t, owner+"/"+other.Name+" already exists. Choose another name.", strings.TrimSpace(errMsg.Text()))
		assert.Equal(t, "alert", errMsg.AttrOr("role", ""))
		input := doc.Find("#repo_name")
		assert.Equal(t, other.Name, input.AttrOr("value", ""))
		assert.Equal(t, "true", input.AttrOr("aria-invalid", ""))
		assert.Equal(t, 1, doc.Find(".ds-field.is-err #repo_name").Length())
	})

	newName := oldName + "-renamed"
	t.Run("a rename saves and the old address redirects", func(t *testing.T) {
		req := NewRequestWithValues(t, "POST", settings, map[string]string{"action": "update", "repo_name": newName})
		resp := session.MakeRequest(t, req, http.StatusSeeOther)
		assert.Equal(t, "/"+owner+"/"+newName+"/settings", resp.Header().Get("Location"))
		resp = session.MakeRequest(t, NewRequest(t, "GET", resp.Header().Get("Location")), http.StatusOK)
		flash := NewHTMLParser(t, resp.Body).Find("#flash-message").Text()
		assert.Equal(t, "Settings saved. The old address redirects for 90 days.", strings.TrimSpace(flash))

		resp = session.MakeRequest(t, NewRequest(t, "GET", "/"+owner+"/"+oldName), http.StatusMovedPermanently)
		assert.Equal(t, "/"+owner+"/"+newName, resp.Header().Get("Location"))
	})

	t.Run("an expired redirect is gone", func(t *testing.T) {
		r := unittest.AssertExistsAndLoadBean(t, &repo_model.Redirect{OwnerID: repo.OwnerID, LowerName: strings.ToLower(oldName)})
		_, err := db.GetEngine(db.DefaultContext).ID(r.ID).Cols("expires_unix").Update(&repo_model.Redirect{ExpiresUnix: timeutil.TimeStampNow() - 1})
		require.NoError(t, err)
		session.MakeRequest(t, NewRequest(t, "GET", "/"+owner+"/"+oldName), http.StatusNotFound)
	})
}
