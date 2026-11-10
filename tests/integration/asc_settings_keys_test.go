// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	asymkey_model "forgejo.org/models/asymkey"
	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	app_context "forgejo.org/services/context"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keysFlash returns the flash message of the given kind ("success", "error") a redirect carried, or "".
func keysFlash(t *testing.T, resp interface{ Result() *http.Response }, kind string) string {
	t.Helper()
	for _, c := range resp.Result().Cookies() {
		if c.Name != app_context.CookieNameFlash {
			continue
		}
		v, err := url.ParseQuery(c.Value)
		require.NoError(t, err)
		if m := v.Get(kind); m != "" {
			return m
		}
		for k := range v {
			if inner, err := url.ParseQuery(k); err == nil && inner.Get(kind) != "" {
				return inner.Get(kind)
			}
		}
	}
	return ""
}

// The board notes of UserSettingsKeys: the empty list with the command that generates a key, an invalid key error, a
// key added toast, Last used on each key, and a delete confirmation that lists what stops working.
func TestAscSettingsKeysBoardNotes(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{Name: "user8"})
	unittest.AssertNotExistsBean(t, &asymkey_model.PublicKey{OwnerID: user.ID})
	session := loginUser(t, user.Name)

	t.Run("empty list shows the command that generates a key", func(t *testing.T) {
		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user/settings/keys"), http.StatusOK).Body)
		cmd := page.Find(`#keys-ssh [data-testid="empty-command"]`)
		require.Equal(t, 1, cmd.Length())
		assert.Equal(t, `ssh-keygen -t ed25519 -C "`+user.Email+`"`, strings.TrimSpace(cmd.Find("code").Text()))
		copyText, ok := cmd.Find("button.ds-copy").Attr("data-clipboard-text")
		assert.True(t, ok)
		assert.Equal(t, `ssh-keygen -t ed25519 -C "`+user.Email+`"`, copyText)
	})

	t.Run("an invalid key says why and adds nothing", func(t *testing.T) {
		resp := session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/keys", map[string]string{
			"title": "laptop", "content": "ssh-ed25519 not-a-key", "type": "ssh",
		}), http.StatusSeeOther)
		assert.Equal(t, "/user/settings/keys", resp.Header().Get("Location"))
		assert.Contains(t, keysFlash(t, resp, "error"), "Cannot verify your SSH key")
		unittest.AssertNotExistsBean(t, &asymkey_model.PublicKey{OwnerID: user.ID})
	})

	pub, err := os.ReadFile("tests/integration/ssh-signing-key.pub")
	if os.IsNotExist(err) {
		pub, err = os.ReadFile("ssh-signing-key.pub")
	}
	require.NoError(t, err)

	t.Run("adding a key says so, and the key has no activity yet", func(t *testing.T) {
		resp := session.MakeRequest(t, NewRequestWithValues(t, "POST", "/user/settings/keys", map[string]string{
			"title": "laptop", "content": strings.TrimSpace(string(pub)), "type": "ssh",
		}), http.StatusSeeOther)
		assert.Equal(t, `The SSH key "laptop" has been added.`, keysFlash(t, resp, "success"))
		unittest.AssertExistsAndLoadBean(t, &asymkey_model.PublicKey{OwnerID: user.ID, Name: "laptop"})

		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user/settings/keys"), http.StatusOK).Body)
		assert.Equal(t, 0, page.Find(`[data-testid="empty-command"]`).Length())
		assert.Contains(t, page.Find("#keys-ssh .ds-li-meta").Text(), "No recent activity")
	})

	t.Run("Last used shows when the key was last used", func(t *testing.T) {
		key := unittest.AssertExistsAndLoadBean(t, &asymkey_model.PublicKey{OwnerID: user.ID, Name: "laptop"})
		used := key.CreatedUnix + 3*86400
		_, err := db.GetEngine(db.DefaultContext).ID(key.ID).Cols("updated_unix").NoAutoTime().Update(&asymkey_model.PublicKey{UpdatedUnix: used})
		require.NoError(t, err)

		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user/settings/keys"), http.StatusOK).Body)
		meta := page.Find("#keys-ssh .ds-li-meta").Text()
		assert.Contains(t, meta, "Last used on")
		assert.Contains(t, meta, time.Unix(int64(used), 0).Format("2006"))
		assert.NotContains(t, meta, "No recent activity")
	})

	t.Run("Delete asks to confirm and lists what stops working", func(t *testing.T) {
		page := NewHTMLParser(t, session.MakeRequest(t, NewRequest(t, "GET", "/user/settings/keys"), http.StatusOK).Body)
		modal := page.Find("#delete-ssh.g-modal-confirm")
		require.Equal(t, 1, modal.Length())
		assert.Equal(t, 2, modal.Find(".wk-stops li").Length())
		btn := page.Find(`#keys-ssh .delete-button[data-modal-id="delete-ssh"]`)
		require.Equal(t, 1, btn.Length())
		del, _ := btn.Attr("data-url")
		assert.Equal(t, "/user/settings/keys/delete?type=ssh", del)
		id, _ := btn.Attr("data-id")

		// confirming posts the id and the key is gone
		session.MakeRequest(t, NewRequestWithValues(t, "POST", del, map[string]string{"id": id}), http.StatusOK)
		unittest.AssertNotExistsBean(t, &asymkey_model.PublicKey{OwnerID: user.ID})
	})
}
