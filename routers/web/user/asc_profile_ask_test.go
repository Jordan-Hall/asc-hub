// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/templates"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// askCtx is a request of doer to the ask routes of person's profile.
func askCtx(t *testing.T, method, kind string, doer, person int64, form map[string]string) (*context.Context, func() (int, string, string)) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, method+" /-/ask/"+kind)
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, doer)
	ctx.IsSigned = true
	ctx.ContextUser = unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: person})
	ctx.SetParams("kind", kind)
	for k, v := range form {
		ctx.Req.Form.Set(k, v)
	}
	return ctx, func() (int, string, string) { return resp.Code, resp.Header().Get("Location"), resp.Body.String() }
}

func TestAscProfileAskPicker(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	// user1 posted the open pull requests 2, 3 and 11 of the public user2/repo1; user5 may read them
	ctx, res := askCtx(t, "GET", "review", 1, 5, nil)
	AscProfileAsk(ctx)
	code, _, body := res()
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, `data-asc-ask-form="review"`)
	assert.Contains(t, body, `value="2"`)
	assert.Contains(t, body, "user2/repo1#")
	assert.NotContains(t, body, `name="content"`)

	ctx, res = askCtx(t, "GET", "mention", 1, 5, nil)
	AscProfileAsk(ctx)
	_, _, body = res()
	assert.Contains(t, body, `name="content"`)

	// user5 posted no pull request: the empty state
	ctx, res = askCtx(t, "GET", "review", 5, 1, nil)
	AscProfileAsk(ctx)
	_, _, body = res()
	assert.Contains(t, body, "data-asc-ask-empty")

	// user4 blocked user1 (fixture), so user1 can ask them nothing
	ctx, res = askCtx(t, "GET", "mention", 1, 4, nil)
	AscProfileAsk(ctx)
	_, _, body = res()
	assert.Contains(t, body, "data-asc-ask-empty")
	ctx, res = askCtx(t, "POST", "review", 1, 4, map[string]string{"pull": "2"})
	AscProfileAskPost(ctx)
	code, _, _ = res()
	assert.Equal(t, http.StatusNotFound, code)

	// no ask of oneself, of an organisation or of an unknown kind
	for _, c := range []struct {
		kind         string
		doer, person int64
	}{{"review", 1, 1}, {"review", 1, 3}, {"poke", 1, 5}} {
		ctx, res = askCtx(t, "GET", c.kind, c.doer, c.person, nil)
		AscProfileAsk(ctx)
		code, _, _ = res()
		assert.Equal(t, http.StatusNotFound, code, c)
	}
}

func TestAscProfileAskReview(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	ctx, res := askCtx(t, "POST", "review", 1, 5, map[string]string{"pull": "2"})
	AscProfileAskPost(ctx)
	code, loc, _ := res()
	assert.Equal(t, http.StatusSeeOther, code)
	assert.Equal(t, "/user5", loc)
	review, err := issues_model.GetReviewByIssueIDAndUserID(db.DefaultContext, 2, 5)
	require.NoError(t, err)
	assert.Equal(t, issues_model.ReviewTypeRequest, review.Type)

	// only a pull request of the doer, that exists and that is a pull request
	for _, pull := range []string{"12", "1", "99999"} {
		ctx, res = askCtx(t, "POST", "review", 1, 5, map[string]string{"pull": pull})
		AscProfileAskPost(ctx)
		code, _, _ = res()
		assert.Equal(t, http.StatusNotFound, code, pull)
	}
	ctx, res = askCtx(t, "POST", "review", 1, 5, map[string]string{"pull": "x"})
	AscProfileAskPost(ctx)
	code, _, _ = res()
	assert.Equal(t, http.StatusBadRequest, code)
}

func TestAscProfileAskMention(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	post := func(content string) (int, string) {
		ctx, res := askCtx(t, "POST", "mention", 1, 5, map[string]string{"pull": "3", "content": content})
		AscProfileAskPost(ctx)
		code, loc, _ := res()
		return code, loc
	}
	last := func() string {
		c := unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{IssueID: 3, PosterID: 1, Type: issues_model.CommentTypeComment}, unittest.OrderBy("id DESC"))
		return c.Content
	}

	code, loc := post("  could you look at the parser?  ")
	assert.Equal(t, http.StatusSeeOther, code)
	assert.True(t, strings.HasPrefix(loc, "/user2/repo1/pulls/3#issuecomment-"), loc)
	assert.Equal(t, "@user5 could you look at the parser?", last())

	_, _ = post("thanks @User5, merged")
	assert.Equal(t, "thanks @User5, merged", last(), "a mention already there is kept as it is")

	code, loc = post("   ")
	assert.Equal(t, http.StatusSeeOther, code)
	assert.Equal(t, "/user5", loc, "an empty comment goes back with an error")
	code, _ = post(strings.Repeat("a", ascAskMentionMax+1))
	assert.Equal(t, http.StatusSeeOther, code)
	assert.Equal(t, "thanks @User5, merged", last())
}

func TestAscMentions(t *testing.T) {
	for text, want := range map[string]bool{
		"@user4":         true,
		"hi @User4.":     true,
		"@user4.dev":     false,
		"hi @user4, ok":  true,
		"@user40 there":  false,
		"mail a@user4":   false,
		"x @user4 @user": true,
		"(@user4)":       true,
	} {
		assert.Equal(t, want, ascMentions(text, "user4"), text)
	}
}
