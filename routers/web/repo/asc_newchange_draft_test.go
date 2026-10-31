// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscNewChangeDraftIDAndPut(t *testing.T) {
	a := ascNewChangeDraftIDOf(1, "main...topic")
	assert.True(t, ascNewChangeDraftID.MatchString(a), a)
	assert.Equal(t, a, ascNewChangeDraftIDOf(1, "main...topic"))
	assert.NotEqual(t, a, ascNewChangeDraftIDOf(2, "main...topic"), "another repository")
	assert.NotEqual(t, a, ascNewChangeDraftIDOf(1, "main...other"), "another comparison")

	d := ascNewChangeDrafts{}
	for i := 0; i < ascNewChangeDraftMax+3; i++ {
		d.put(ascNewChangeDraftIDOf(1, strings.Repeat("x", i+1)), ascNewChangeDraft{Title: "t", At: int64(100 + i)})
	}
	assert.Len(t, d, ascNewChangeDraftMax)
	assert.NotContains(t, d, ascNewChangeDraftIDOf(1, "x"), "the oldest goes first")
	assert.Contains(t, d, ascNewChangeDraftIDOf(1, strings.Repeat("x", ascNewChangeDraftMax+3)))
}

func TestParseAscNewChangeDraftsKeepsOnlyWhatIsWithinTheLimits(t *testing.T) {
	good := ascNewChangeDraftIDOf(1, "a...b")
	raw, err := json.Marshal(ascNewChangeDrafts{
		good:                                   {Title: "ok", Body: "text", At: 5},
		"not-an-id":                            {Title: "x"},
		ascNewChangeDraftIDOf(1, "long title"): {Title: strings.Repeat("t", ascNewChangeDraftTitle+1)},
		ascNewChangeDraftIDOf(1, "long body"):  {Body: strings.Repeat("b", ascNewChangeDraftBody+1)},
	})
	require.NoError(t, err)
	got := parseAscNewChangeDrafts(string(raw))
	assert.Len(t, got, 1)
	assert.Equal(t, "ok", got[good].Title)

	assert.Empty(t, parseAscNewChangeDrafts(""))
	assert.Empty(t, parseAscNewChangeDrafts("{broken"))
	assert.Empty(t, parseAscNewChangeDrafts(strings.Repeat("a", ascNewChangeDraftValue+1)), "a value over the cap is not read")
}

type ascDraftEnv struct {
	ctx  *context.Context
	resp *httptest.ResponseRecorder
}

func ascDraftCtx(t *testing.T, backend string) (*ascDraftEnv, string) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "/user2/repo1/asc/newchange_draft")
	contexttest.LoadUser(t, ctx, 2)
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Repo.Repository.StorageBackend = backend
	return &ascDraftEnv{ctx, resp}, ascNewChangeDraftIDOf(ctx.Repo.Repository.ID, "master...branch2")
}

func TestAscNewChangeDraftSaveRestoreAndClear(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	c, id := ascDraftCtx(t, "asc")
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()

	post := func(form url.Values) (int, map[string]any) {
		if form.Get("id") == "" {
			form.Set("id", id)
		}
		r, _ := ascDraftCtx(t, "asc")
		r.ctx.Req.Method = http.MethodPost
		r.ctx.Req.PostForm, r.ctx.Req.Form = form, form
		AscNewChangeDraftSave(r.ctx)
		var out map[string]any
		_ = json.Unmarshal(r.resp.Body.Bytes(), &out)
		return r.resp.Code, out
	}
	stored := func() ascNewChangeDrafts {
		raw, err := user_model.GetUserSetting(c.ctx, 2, ascNewChangeDraftKey)
		require.NoError(t, err)
		return parseAscNewChangeDrafts(raw)
	}

	code, out := post(url.Values{"title": {"Add retries"}, "content": {"## What\r\nretries"}})
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, true, out["saved"])
	require.Contains(t, stored(), id)
	assert.Equal(t, "## What\nretries", stored()[id].Body, "line ends are normalised")

	// the form comes back with the draft, unless the address brought its own text
	prepareAscNewChangeDraft(c.ctx, "master...branch2")
	st, ok := c.ctx.Data["AscNewChangeDraft"].(*AscNewChangeDraftState)
	require.True(t, ok)
	assert.True(t, st.Restored)
	assert.Equal(t, id, st.ID)
	assert.Equal(t, "Add retries", c.ctx.Data["TitleQuery"])
	assert.Equal(t, "## What\nretries", c.ctx.Data["BodyQuery"])
	c.ctx.Data["TitleQuery"], c.ctx.Data["BodyQuery"] = "From the address", ""
	prepareAscNewChangeDraft(c.ctx, "master...branch2")
	assert.Equal(t, "From the address", c.ctx.Data["TitleQuery"], "text from the address wins")

	// a draft over the limits is refused and the old one stays
	code, _ = post(url.Values{"title": {"x"}, "content": {strings.Repeat("b", ascNewChangeDraftBody+1)}})
	assert.Equal(t, http.StatusRequestEntityTooLarge, code)
	code, _ = post(url.Values{"title": {strings.Repeat("t", ascNewChangeDraftTitle+1)}})
	assert.Equal(t, http.StatusRequestEntityTooLarge, code)
	code, _ = post(url.Values{"title": {"two\nlines"}})
	assert.Equal(t, http.StatusRequestEntityTooLarge, code)
	assert.Equal(t, "Add retries", stored()[id].Title)

	// an id that is not one is refused
	code, _ = post(url.Values{"id": {"../../x"}, "title": {"t"}})
	assert.Equal(t, http.StatusBadRequest, code)

	// empty text keeps no draft, and so does a discard
	code, out = post(url.Values{"title": {"  "}, "content": {""}})
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, false, out["saved"])
	assert.NotContains(t, stored(), id)
	post(url.Values{"title": {"again"}})
	require.Contains(t, stored(), id)
	post(url.Values{"discard": {"1"}, "title": {"ignored"}})
	assert.NotContains(t, stored(), id)

	// opening the change removes the draft
	post(url.Values{"title": {"again"}})
	require.Contains(t, stored(), id)
	clearAscNewChangeDraft(c.ctx, "master...branch2")
	assert.NotContains(t, stored(), id)
}

func TestAscNewChangeDraftOnlyForAscRepositories(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	c, id := ascDraftCtx(t, "")
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	c.ctx.Req.Method = http.MethodPost
	c.ctx.Req.PostForm = url.Values{"id": {id}, "title": {"t"}}
	c.ctx.Req.Form = c.ctx.Req.PostForm
	AscNewChangeDraftSave(c.ctx)
	assert.Equal(t, http.StatusNotFound, c.resp.Code)
	raw, err := user_model.GetUserSetting(c.ctx, 2, ascNewChangeDraftKey)
	require.NoError(t, err)
	assert.Empty(t, raw)

	prepareAscNewChangeDraft(c.ctx, "master...branch2")
	assert.Nil(t, c.ctx.Data["AscNewChangeDraft"])
}
