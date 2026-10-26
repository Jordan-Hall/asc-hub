// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/svg"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/modules/translation"
	"forgejo.org/modules/web/middleware"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ascRenderContext builds a web context that renders full pages with the real templates, the English
// texts and the data the request middleware would add, signed in as userID when it is above zero.
func ascRenderContext(t *testing.T, path string, userID int64) (*context.Context, func() []byte) {
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
	ctx.Data["UnitWikiGlobalDisabled"] = unit.TypeWiki.UnitGlobalDisabled()
	ctx.Data["UnitIssuesGlobalDisabled"] = unit.TypeIssues.UnitGlobalDisabled()
	ctx.Data["UnitPullsGlobalDisabled"] = unit.TypePullRequests.UnitGlobalDisabled()
	ctx.Data["UnitProjectsGlobalDisabled"] = unit.TypeProjects.UnitGlobalDisabled()
	ctx.Data["UnitActionsGlobalDisabled"] = unit.TypeActions.UnitGlobalDisabled()
	ctx.Data["AllLangs"] = translation.AllLangs()
	ctx.Data["IsSigned"] = false
	if userID > 0 {
		contexttest.LoadUser(t, ctx, userID)
		ctx.IsSigned = true
		ctx.Data["IsSigned"] = true
		ctx.Data[middleware.ContextDataKeySignedUser] = ctx.Doer
		ctx.Data["SignedUserID"] = ctx.Doer.ID
		ctx.Data["IsAdmin"] = ctx.Doer.IsAdmin
	}
	return ctx, resp.Body.Bytes
}

// ascRenderWrite stores one rendered page for the design comparison when ASC_RENDER_DIR is set.
func ascRenderWrite(t *testing.T, name string, code int, body []byte) {
	t.Helper()
	assert.Equal(t, http.StatusOK, code, name)
	dir := os.Getenv("ASC_RENDER_DIR")
	if dir == "" {
		return
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".html"), body, 0o644))
}

// ascRenderSeed gives the fixture database the content of the design boards: titled pull requests that form a
// stack, fresh activity and the agent switch on for user 2.
func ascRenderSeed(t *testing.T) {
	t.Helper()
	e := db.GetEngine(t.Context())
	titles := map[int64]string{2: "Add retry policy type", 3: "Backoff with jitter", 11: "Wire retry policy into the client"}
	for id, name := range titles {
		_, err := e.Exec("UPDATE issue SET name = ?, poster_id = 2 WHERE id = ?", name, id)
		require.NoError(t, err)
	}
	_, err := e.Exec("UPDATE action SET created_unix = ?", time.Now().Add(-90*time.Minute).Unix())
	require.NoError(t, err)
	changes := []string{strings.Repeat("k", 31) + "m", strings.Repeat("k", 31) + "n", strings.Repeat("k", 31) + "p"}
	prev := int64(0)
	for i, id := range []int64{1, 2, 5} {
		pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: id})
		require.NoError(t, issues_model.SetPullRequestAscStack(t.Context(), pr, changes[i], prev))
		prev = id
	}
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	require.NoError(t, ascagentui.SetFeatures(t.Context(), user2, true))
}

func ascRenderEntries(t *testing.T) {
	t.Helper()
	now := time.Now()
	es := []*AscInboxEntry{
		{PullID: 8, Title: "Wire retry policy into the client", Link: "/user2/repo1/pulls/1", RepoFullName: "acme/payments", Index: 3, Section: issues_model.AscInboxNeedsReview, Updated: now.Add(-12 * time.Minute), Author: "coder-2"},
		{PullID: 9001, Title: "Pagination cursor format", Link: "/user2/repo1/pulls/2", RepoFullName: "acme/web-app", Index: 88, Section: issues_model.AscInboxReturned, Updated: now.Add(-41 * time.Minute), Author: "user2"},
		{PullID: 9002, Title: "Currency tables", Link: "/user2/repo1/pulls/3", RepoFullName: "acme/payments", Index: 91, Section: issues_model.AscInboxBlocked, Updated: now.Add(-time.Hour), Author: "user2", ChecksFailing: true},
		{PullID: 9003, Title: "Rate limit headers", Link: "/user2/repo1/pulls/4", RepoFullName: "acme/infra", Index: 12, Section: issues_model.AscInboxReady, Updated: now.Add(-2 * time.Hour), Author: "user2", Approved: true},
		{PullID: 9004, Title: "Docs and metrics", Link: "/user2/repo1/pulls/5", RepoFullName: "acme/payments", Index: 93, Section: issues_model.AscInboxWaiting, Updated: now.Add(-3 * time.Hour), Author: "user2"},
		{PullID: 9005, Title: "Add jitter to the retry delay", Link: "/user2/repo1/pulls/6", RepoFullName: "acme/payments", Index: 80, Section: issues_model.AscInboxMerged, Updated: now.Add(-5 * 24 * time.Hour), Author: "user2"},
	}
	oldLoad, oldInfo, oldAgents := ascInboxLoad, ascInboxInfoLoad, ascDashAgentsLoad
	ascInboxLoad = func(gocontext.Context, *user_model.User, time.Time) ([]*AscInboxEntry, error) { return es, nil }
	ascInboxInfoLoad = func(_ gocontext.Context, e *AscInboxEntry) *AscInboxInfo {
		switch e.PullID {
		case 8:
			return &AscInboxInfo{StackIndex: 3, StackTotal: 4, HasDiff: true, Files: 3, Additions: 84, Deletions: 12, ChecksPass: 42}
		case 9001:
			return &AscInboxInfo{HasDiff: true, Files: 2, Additions: 31, Deletions: 7, ChecksPass: 12}
		case 9002:
			return &AscInboxInfo{HasDiff: true, Files: 5, Additions: 120, Deletions: 40, ChecksFail: 2, ChecksPass: 3}
		case 9003:
			return &AscInboxInfo{HasDiff: true, Files: 1, Additions: 18, Deletions: 2, ChecksPass: 9, Approvals: 2}
		}
		return &AscInboxInfo{}
	}
	ascDashAgentsLoad = func(*context.Context, *user_model.User) ([]AscDashAgent, int, error) {
		return []AscDashAgent{
			{Title: "docs and metrics", Agent: "coder-2", Repo: "acme/payments", Link: "/user2/repo1/asc/agents/runs/214", State: ascagentui.State{Slug: "running", Label: "Running", Tone: "run", Active: true}, Percent: 58, Meter: "58% of time: 6m of 20m"},
			{Title: "currency tables", Agent: "coder-1", Repo: "acme/payments", Link: "/user2/repo1/asc/agents/runs/213", State: ascagentui.State{Slug: "waiting", Label: "Needs you", Tone: "wait", Active: true}, Percent: 31, Meter: "31% of time: 4m of 30m"},
			{Title: "first pass on web-app #88", Agent: "reviewer-1", Repo: "acme/web-app", Link: "/user2/repo1/asc/agents/runs/212", State: ascagentui.State{Slug: "running", Label: "Running", Tone: "run", Active: true}, Percent: 82, Meter: "82% of time: 2m of 10m"},
		}, 1, nil
	}
	t.Cleanup(func() { ascInboxLoad, ascInboxInfoLoad, ascDashAgentsLoad = oldLoad, oldInfo, oldAgents })
}

func TestAscRenderUserPages(t *testing.T) {
	if os.Getenv("ASC_RENDER_DIR") == "" {
		t.Skip("set ASC_RENDER_DIR to write the rendered pages")
	}
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	ascRenderSeed(t)
	ascRenderEntries(t)

	ctx, body := ascRenderContext(t, "/", 2)
	Dashboard(ctx)
	ascRenderWrite(t, "dashboard", ctx.Resp.Status(), body())

	oldComments := ascInboxCommentsLoad
	ascInboxCommentsLoad = func(gocontext.Context, int64) []AscInboxComment {
		return []AscInboxComment{{Poster: ctx.Doer, Body: "Every outbound call now goes through the retry policy. I kept the 30 s ceiling from the spec. Refund calls use the same policy, flagged for you.", Created: time.Now().Add(-12 * time.Minute)}}
	}
	defer func() { ascInboxCommentsLoad = oldComments }()
	oldAgents := ascInboxAgentsLoad
	ascInboxAgentsLoad = func(c *context.Context, u *user_model.User) ([]AscDashAgent, int, error) {
		return ascDashAgentsLoad(c, u)
	}
	defer func() { ascInboxAgentsLoad = oldAgents }()
	ctx, body = ascRenderContext(t, "/-/asc/inbox", 2)
	AscInbox(ctx)
	ascRenderWrite(t, "inbox", ctx.Resp.Status(), body())
}
