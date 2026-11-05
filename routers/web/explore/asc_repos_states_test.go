// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package explore

import (
	"bytes"
	stdctx "context"
	"errors"
	"net/http"
	"testing"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/svg"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/modules/translation"
	"forgejo.org/modules/web/middleware"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ascExploreRender renders path with handle and the real templates, as the user at userID (0 for a visitor).
func ascExploreRender(t *testing.T, handle func(*context.Context), path string, userID int64) (*context.Context, *goquery.Document) {
	t.Helper()
	defer test.MockVariableValue(&setting.Langs, []string{"en-US"})()
	defer test.MockVariableValue(&setting.Names, []string{"English"})()
	translation.InitLocales(t.Context())
	require.NoError(t, svg.Init())
	ctx, resp := contexttest.MockContext(t, path)
	ctx.Render = templates.HTMLRenderer()
	ctx.Locale = translation.NewLocale("en-US")
	ctx.TemplateContext.Locale = ctx.Locale
	ctx.Data.MergeFrom(middleware.CommonTemplateContextData())
	ctx.Data["Context"] = ctx
	ctx.Data["CurrentURL"] = setting.AppSubURL + path
	ctx.Data["Link"] = ctx.Link
	ctx.Data["PageData"] = ctx.PageData
	ctx.Data["SystemConfig"] = setting.Config()
	ctx.Data["AllLangs"] = translation.AllLangs()
	ctx.Data["RequestID"] = "7f3a91"
	ctx.Data["IsSigned"] = false
	if userID > 0 {
		contexttest.LoadUser(t, ctx, userID)
		ctx.IsSigned = true
		ctx.Data["IsSigned"] = true
		ctx.Data[middleware.ContextDataKeySignedUser] = ctx.Doer
		ctx.Data["SignedUserID"] = ctx.Doer.ID
	}
	handle(ctx)
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(resp.Body.Bytes()))
	require.NoError(t, err)
	return ctx, doc
}

func TestAscExploreLoadFailed(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&searchRepositories, func(_ stdctx.Context, _ *repo_model.SearchRepoOptions) (repo_model.RepositoryList, int64, error) {
		return nil, 0, errors.New("database is down")
	})()
	ctx, doc := ascExploreRender(t, Repos, "/explore/repos?q=ledger", 2)
	assert.Equal(t, http.StatusInternalServerError, ctx.Resp.WrittenStatus())
	card := doc.Find(`.ds-card[role="alert"] .ds-empty-err`)
	require.Equal(t, 1, card.Length(), "the list says it could not be loaded, in the page")
	assert.Contains(t, card.Find("h2").Text(), "We could not load this list")
	assert.Contains(t, card.Text(), "request id 7f3a91")
	assert.Equal(t, "?q=ledger", card.Find("a.ds-btn-primary").AttrOr("href", ""), "Try again loads the same list")
	assert.Equal(t, "/-/status", card.Find("a.ds-btn-secondary").AttrOr("href", ""))
	assert.Equal(t, "ledger", doc.Find("#repo-search-q").AttrOr("value", ""), "the search keeps what was typed")
}

func TestAscExploreEmptyHub(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&searchRepositories, func(_ stdctx.Context, _ *repo_model.SearchRepoOptions) (repo_model.RepositoryList, int64, error) {
		return nil, 0, nil
	})()
	ctx, doc := ascExploreRender(t, Repos, "/explore/repos", 2)
	assert.Equal(t, http.StatusOK, ctx.Resp.WrittenStatus())
	empty := doc.Find(".ds-card .ds-empty")
	require.Equal(t, 1, empty.Length())
	assert.Equal(t, "No repositories yet", empty.Find("h2").Text())
	assert.Equal(t, "/repo/create", empty.Find("a.ds-btn-primary").AttrOr("href", ""))
	assert.Equal(t, "Import a project", empty.Find("a.ds-btn-secondary").Text())

	_, visitor := ascExploreRender(t, Repos, "/explore/repos", 0)
	assert.Equal(t, "No repositories yet", visitor.Find(".ds-card .ds-empty h2").Text())
	assert.Zero(t, visitor.Find(".ds-card .ds-empty a").Length(), "a visitor is offered no create button")
}

func TestAscExploreUsersLoadFailed(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&searchUsers, func(_ stdctx.Context, _ *user_model.SearchUserOptions) ([]*user_model.User, int64, error) {
		return nil, 0, errors.New("database is down")
	})()
	ctx, doc := ascExploreRender(t, Users, "/explore/users?q=maya", 2)
	assert.Equal(t, http.StatusInternalServerError, ctx.Resp.WrittenStatus())
	card := doc.Find(`.ds-card[role="alert"] .ds-empty-err`)
	require.Equal(t, 1, card.Length())
	assert.Contains(t, card.Text(), "request id 7f3a91")
	assert.Equal(t, "?q=maya", card.Find("a.ds-btn-primary").AttrOr("href", ""))
}
