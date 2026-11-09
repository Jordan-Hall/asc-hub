// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/structs"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThePackageListShowsTheVisibilityOfEachPackage(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	req := NewRequestWithBody(t, "PUT", "/api/packages/user2/generic/nova-client/1.0.0/build.tar", bytes.NewReader([]byte("build"))).
		AddBasicAuth("user2")
	MakeRequest(t, req, http.StatusCreated)
	sess := loginUser(t, "user2")
	chip := func(t *testing.T) (string, string) {
		t.Helper()
		resp := sess.MakeRequest(t, NewRequest(t, "GET", "/user2/-/packages"), http.StatusOK)
		s := NewHTMLParser(t, resp.Body).Find(`[data-testid="package-visibility"]`)
		assert.Equal(t, 1, s.Length())
		return strings.TrimSpace(s.Text()), s.AttrOr("title", "")
	}
	setVisibility := func(t *testing.T, v structs.VisibleType) {
		t.Helper()
		_, err := db.GetEngine(db.DefaultContext).ID(2).Cols("visibility").Update(&user_model.User{Visibility: v})
		require.NoError(t, err)
	}

	t.Run("A public owner", func(t *testing.T) {
		text, title := chip(t)
		assert.Equal(t, "Public", text)
		assert.Equal(t, "Anyone can install it, like everything user2 shares.", title)
	})
	t.Run("A limited owner", func(t *testing.T) {
		setVisibility(t, structs.VisibleTypeLimited)
		text, _ := chip(t)
		assert.Equal(t, "Internal", text)
	})
	t.Run("A private owner", func(t *testing.T) {
		setVisibility(t, structs.VisibleTypePrivate)
		text, title := chip(t)
		assert.Equal(t, "Private", text)
		assert.Equal(t, "Only people with access to user2 can install it.", title)
	})
}
