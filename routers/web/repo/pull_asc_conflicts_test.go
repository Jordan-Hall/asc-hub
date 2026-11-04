// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascbridge"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"
	pull_service "forgejo.org/services/pull"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	cfTestChangeset = "3333333333333333333333333333333333333333333333333333333333333333"
	cfTestMarked    = "4444444444444444444444444444444444444444444444444444444444444444"
	cfTestText      = "intro\n<<<<<<< ours\nAttempts: 3, base 200 ms\n||||||| base\nAttempts: 3\n=======\nAttempts: 5, base 100 ms\n<script>x</script>\n>>>>>>> theirs\n"
)

// cfOps answers a dry run without resolutions with conflict, and any other call with the stack up to date, so the
// handlers never move a head (the service tests cover that).
type cfOps struct {
	conflict *ascbridge.RestackStackResult
	reqs     []ascbridge.RestackStackRequest
}

func (o *cfOps) RestackStack(_ gocontext.Context, req ascbridge.RestackStackRequest) (*ascbridge.RestackStackResult, error) {
	o.reqs = append(o.reqs, req)
	if req.DryRun && len(req.Resolutions) == 0 {
		return o.conflict, nil
	}
	return &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: qrBottomChange, State: ascbridge.RestackUnchanged}, {Change: qrTopChange, State: ascbridge.RestackUnchanged}}}, nil
}

func (o *cfOps) RevertLanded(gocontext.Context, ascbridge.RevertLandedRequest) (*ascbridge.RevertLandedResult, error) {
	return nil, errors.New("not used")
}

func newConflictFixture(t *testing.T) (*qrFixture, *cfOps) {
	t.Helper()
	f := newQRFixture(t)
	ops := &cfOps{conflict: &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: qrBottomChange, State: ascbridge.RestackUnchanged},
		{Change: qrTopChange, State: ascbridge.RestackConflict, Conflicts: []string{"docs/retries.md", "logo.png"}, Files: []ascbridge.ConflictFile{
			{Path: "docs/retries.md", Kind: "content", ChangesetID: cfTestChangeset, MarkedID: cfTestMarked},
			{Path: "logo.png", Kind: "binary", ChangesetID: cfTestChangeset},
		}},
	}}}
	t.Cleanup(ascbridge.UseStackOps(ops))
	// The text of a file is read as the viewer (a signed claim), so the key that signs it is set.
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, testForwardedKey))
	setStorageBackend(t, 1, "asc")
	t.Cleanup(func() { setStorageBackend(t, 1, "") })
	t.Cleanup(pull_service.UseAscConflictReader(func(_ gocontext.Context, _, id string, _ int) ([]byte, bool, error) {
		if id != cfTestMarked {
			return nil, false, errors.New("unknown id")
		}
		return []byte(cfTestText), false, nil
	}))
	return f, ops
}

func TestAscConflictsPage(t *testing.T) {
	f, ops := newConflictFixture(t)
	page := func(userID int64, query string) (*http.Request, int, string) {
		ctx, resp := qrRequest(t, func(ctx *context.Context) {
			ctx.Render = templates.HTMLRenderer()
			AscConflicts(ctx)
		}, userID, f.top, "user2/repo1/pulls/5/asc/conflicts"+query, nil)
		return ctx.Req, resp.Code, resp.Body.String()
	}

	_, code, body := page(5, "")
	require.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, `data-state="conflict"`)
	assert.Contains(t, body, "docs/retries.md")
	assert.Contains(t, body, "Attempts: 5, base 100 ms")
	assert.Contains(t, body, "&lt;script&gt;x&lt;/script&gt;", "the text of a file is never markup")
	assert.NotContains(t, body, "<script>x</script>")
	assert.Contains(t, body, "repo.pulls.asc_conflicts.kind.binary")
	assert.Contains(t, body, "repo.pulls.asc_conflicts.no_text")
	assert.NotContains(t, body, "asc-conflict-edit", "a reader may not resolve")
	assert.NotContains(t, body, "<textarea")
	for _, r := range ops.reqs {
		assert.True(t, r.DryRun, "the page writes nothing")
	}

	_, code, _ = page(5, "?edit=1")
	assert.Equal(t, http.StatusForbidden, code, "the editor needs the right to restack")

	_, code, body = page(2, "")
	require.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, `data-testid="asc-conflict-edit"`)
	assert.Contains(t, body, `href="/user2/repo1/pulls/5/asc/conflicts?edit=1"`)

	_, code, body = page(2, "?edit=1")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, 1, countOf(body, "<textarea"), "only the text file has an editor")
	assert.Contains(t, body, `name="path" value="docs/retries.md"`)
	assert.Contains(t, body, `action="/user2/repo1/pulls/5/asc/conflicts"`)
	assert.Contains(t, body, `data-testid="asc-conflicts-submit"`)
	assert.Contains(t, body, "repo.pulls.asc_conflicts.not_editable")

	// No conflict: no editor even when asked for.
	prev := ops.conflict
	ops.conflict = &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: qrBottomChange, State: ascbridge.RestackUnchanged}, {Change: qrTopChange, State: ascbridge.RestackUnchanged}}}
	_, code, body = page(2, "?edit=1")
	require.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, `data-testid="asc-conflicts-none"`)
	assert.NotContains(t, body, "<textarea")

	// Blocked by the change below: the page links its pull request.
	ops.conflict = &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: qrBottomChange, State: ascbridge.RestackConflict, Conflicts: []string{"a.go"}}, {Change: qrTopChange, State: ascbridge.RestackBlocked}}}
	_, code, body = page(2, "")
	require.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, `data-state="blocked"`)
	assert.Contains(t, body, "repo.pulls.asc_conflicts.lead_blocked")
	assert.NotContains(t, body, "repo.pulls.asc_conflicts.lead_blocked_below", "the pull request below is known")
	ops.conflict = prev
}

func TestAscResolveConflictsHandler(t *testing.T) {
	f, ops := newConflictFixture(t)
	post := func(userID int64, form url.Values) (*context.Context, int, string) {
		ctx, resp := qrRequest(t, func(ctx *context.Context) {
			ctx.Render = templates.HTMLRenderer()
			ctx.Req.PostForm = form
			ctx.Req.Form = form
			AscResolveConflicts(ctx)
		}, userID, f.top, "POST /user2/repo1/pulls/5/asc/conflicts", nil)
		return ctx, resp.Code, resp.Body.String()
	}

	for name, form := range map[string]url.Values{
		"nothing":             {},
		"a path without text": {"path": {"docs/retries.md"}},
		"an empty path":       {"path": {""}, "content": {"x"}},
		"the same path twice": {"path": {"a", "a"}, "content": {"x", "y"}},
		"too many files":      {"path": strings.Split(strings.Repeat("p,", pull_service.AscConflictMaxFiles+1), ","), "content": make([]string, pull_service.AscConflictMaxFiles+2)},
	} {
		_, code, _ := post(2, form)
		assert.Equal(t, http.StatusBadRequest, code, name)
	}

	ops.reqs = nil
	_, code, body := post(2, url.Values{"path": {"docs/retries.md"}, "content": {"intro\n<<<<<<< ours\nkept text\n"}})
	assert.Equal(t, http.StatusOK, code, "markers left: the editor again")
	assert.Contains(t, body, "repo.pulls.asc_conflicts.markers_left")
	assert.Contains(t, body, "kept text", "what was written is not lost")
	for _, r := range ops.reqs {
		assert.True(t, r.DryRun)
	}

	_, code, body = post(2, url.Values{"path": {"logo.png"}, "content": {"x"}})
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "repo.pulls.asc_conflicts.changed")

	ctx, code, _ := post(5, url.Values{"path": {"docs/retries.md"}, "content": {"done\n"}})
	assert.Equal(t, http.StatusSeeOther, code)
	assert.Equal(t, "repo.pulls.asc_op.err.not_allowed", ctx.Flash.ErrorMsg)

	ops.reqs = nil
	ctx, code, _ = post(2, url.Values{"path": {"docs/retries.md"}, "content": {"intro\r\nAttempts: 5, base 200 ms\r\n"}})
	assert.Equal(t, http.StatusSeeOther, code)
	assert.Equal(t, "repo.pulls.asc_conflicts.resolved", ctx.Flash.SuccessMsg)
	require.NotEmpty(t, ops.reqs)
	last := ops.reqs[len(ops.reqs)-1]
	assert.Equal(t, []ascbridge.Resolution{{ChangesetID: cfTestChangeset, Path: "docs/retries.md", Content: []byte("intro\nAttempts: 5, base 200 ms\n")}}, last.Resolutions)

	ops.conflict = &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: qrBottomChange, State: ascbridge.RestackUnchanged}, {Change: qrTopChange, State: ascbridge.RestackUnchanged}}}
	ctx, code, _ = post(2, url.Values{"path": {"docs/retries.md"}, "content": {"x"}})
	assert.Equal(t, http.StatusSeeOther, code)
	assert.Equal(t, "repo.pulls.asc_conflicts.none_left", ctx.Flash.InfoMsg)
}

func TestAscAskResolve(t *testing.T) {
	f, ops := newConflictFixture(t)
	var reqs []*ascv1.CreateAgentRunRequest
	var runErr error
	t.Cleanup(test.MockVariableValue(&ascSuggestionRun, func(_ gocontext.Context, doer *user_model.User, _ *repo_model.Repository, req *ascv1.CreateAgentRunRequest) error {
		assert.EqualValues(t, 2, doer.ID)
		reqs = append(reqs, req)
		return runErr
	}))
	ask := func(userID int64) (*context.Context, int) {
		ctx, resp := qrRequest(t, AscAskResolve, userID, f.top, "POST /user2/repo1/pulls/5/asc/ask_resolve", nil)
		return ctx, resp.Code
	}

	_, code := ask(2)
	assert.Equal(t, http.StatusNotFound, code, "no agent made the change")
	require.NoError(t, issues_model.SetPullRequestAscRun(t.Context(), f.top, 7, "coder-2", "user2"))
	_, code = ask(5)
	assert.Equal(t, http.StatusForbidden, code, "a person who may not restack")
	_, code = ask(0)
	assert.Equal(t, http.StatusForbidden, code)

	// logo.png and docs/retries.md: the run may write those two files only.
	ctx, code := ask(2)
	assert.Equal(t, http.StatusSeeOther, code)
	assert.Equal(t, "repo.pulls.asc_conflicts.asked", ctx.Flash.SuccessMsg)
	require.Len(t, reqs, 1)
	assert.Equal(t, "coder-2", reqs[0].GetAgent())
	assert.Equal(t, f.top.HeadBranch, reqs[0].GetTarget())
	assert.Equal(t, []string{"docs/retries.md", "logo.png"}, reqs[0].GetPaths())
	assert.EqualValues(t, 2, reqs[0].GetLimits().GetMaxPaths())
	assert.Equal(t, ascChangeBytes(qrTopChange), reqs[0].GetForChange())
	assert.Contains(t, reqs[0].GetTask(), `"master"`)

	ops.conflict.Items[1].Files[1].Path = "src/*.rs"
	ctx, _ = ask(2)
	assert.Equal(t, "repo.pulls.asc_conflicts.ask_path", ctx.Flash.ErrorMsg, "a wildcard would widen the scope")
	ops.conflict.Items[1].Files[1].Path = "logo.png"
	assert.Len(t, reqs, 1)

	runErr = errors.New("asc is down")
	ctx, _ = ask(2)
	assert.Equal(t, "repo.pulls.asc_conflicts.ask_failed", ctx.Flash.ErrorMsg)
	runErr = asctrust.ErrForbidden
	_, code = ask(2)
	assert.Equal(t, http.StatusForbidden, code)
	runErr = nil

	ops.conflict = &ascbridge.RestackStackResult{Items: []ascbridge.RestackItem{
		{Change: qrBottomChange, State: ascbridge.RestackUnchanged}, {Change: qrTopChange, State: ascbridge.RestackUnchanged}}}
	ctx, _ = ask(2)
	assert.Equal(t, "repo.pulls.asc_conflicts.none_left", ctx.Flash.InfoMsg)
	assert.Len(t, reqs, 3)
}

func TestAscConflictBanner(t *testing.T) {
	pull, change := badgePull(t)
	own := ascChangeBytes(change)
	f := startFakeBadges(t)
	f.overlaps = func(*ascv1.OverlapsRequest) (*ascv1.OverlapsResponse, error) { return &ascv1.OverlapsResponse{}, nil }
	f.merge = func(*ascv1.MergeabilityRequest) (*ascv1.MergeabilityResponse, error) {
		return &ascv1.MergeabilityResponse{Results: []*ascv1.Mergeable{{
			Change: own, State: ascv1.Mergeable_STATE_CONFLICTS,
			Conflicts: []*ascv1.Conflict{
				{Path: []byte("docs/retries.md"), MarkedId: []byte(strings.Repeat("\x44", 32))},
				{Path: []byte("b.go")}, {Path: []byte("c.go")},
			},
		}}}, nil
	}
	var read []string
	t.Cleanup(test.MockVariableValue(&ascBannerReadFile, func(_ gocontext.Context, repo, id string, limit int) ([]byte, bool, error) {
		read = append(read, id)
		assert.Equal(t, "user2/repo1", repo)
		assert.Equal(t, ascBannerPreviewBytes, limit)
		return []byte(cfTestText), true, nil
	}))

	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, testForwardedKey))
	setStorageBackend(t, pull.BaseRepoID, "asc")
	t.Cleanup(func() { setStorageBackend(t, pull.BaseRepoID, "") })

	// Nobody signed in: the banner names the conflict, but no text is read, since asc applies its path rules to
	// a person.
	anon := runBadges(t, pull).Data["AscBadges"].(*AscBadges)
	require.NotNil(t, anon.Merge)
	assert.Empty(t, read)
	assert.Empty(t, anon.Merge.Preview)
	assert.Equal(t, pull.Issue.Link()+"/asc/conflicts", anon.Merge.Link)

	ctx, _ := contexttest.MockContext(t, "user2/repo1/pulls/2")
	contexttest.LoadUser(t, ctx, 5)
	ctx.IsSigned = true
	contexttest.LoadRepo(t, ctx, pull.BaseRepoID)
	prepareAscBadges(ctx, pull)
	b, ok := ctx.Data["AscBadges"].(*AscBadges)
	require.True(t, ok)
	m := b.Merge
	require.NotNil(t, m)
	assert.Equal(t, []string{cfTestMarked}, read)
	assert.Equal(t, pull.BaseBranch, m.Target)
	assert.Equal(t, 2, m.Others)
	assert.Equal(t, pull.Issue.Link()+"/asc/conflicts", m.Link)
	assert.False(t, m.CanResolve, "a reader may not resolve")
	assert.Equal(t, 2, m.PreviewLine)
	assert.Equal(t, []AscConflictRow{
		{Line: 3, Add: true, Text: "Attempts: 5, base 100 ms"},
		{Line: 4, Add: true, Text: "<script>x</script>"},
		{Line: 3, Text: "Attempts: 3, base 200 ms", Target: true},
	}, m.Preview)

	out := renderBadges(t, ctx)
	assert.Contains(t, out, `data-state="conflicts"`)
	assert.Contains(t, out, "repo.pulls.asc_conflicts.banner_more")
	assert.Contains(t, out, "repo.pulls.asc_conflicts.show_both")
	assert.Contains(t, out, "&lt;script&gt;x&lt;/script&gt;")
	assert.NotContains(t, out, "<script>x</script>")
	assert.NotContains(t, out, "asc-conflict-edit", "a viewer who may not resolve has no editor button")

	// A viewer who may resolve gets the editor button.
	m.CanResolve = true
	out = renderBadges(t, ctx)
	assert.Contains(t, out, `data-testid="asc-conflict-edit"`)
	assert.Contains(t, out, pull.Issue.Link()+"/asc/conflicts?edit=1")

	// A marked text that cannot be read leaves the preview out, not the banner.
	t.Cleanup(test.MockVariableValue(&ascBannerReadFile, func(gocontext.Context, string, string, int) ([]byte, bool, error) {
		return nil, false, errors.New("down")
	}))
	prepareAscBadges(ctx, pull)
	b = ctx.Data["AscBadges"].(*AscBadges)
	require.NotNil(t, b.Merge)
	assert.Empty(t, b.Merge.Preview)
	assert.Equal(t, pull.Issue.Link()+"/asc/conflicts", b.Merge.Link)
}
