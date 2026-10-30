// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/automerge"
	"forgejo.org/services/contexttest"
	pull_service "forgejo.org/services/pull"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	rmBottom = "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk"
	rmTop    = "llllllllllllllllllllllllllllllll"
)

type rmLander struct{ lands []int64 }

// Land stands in for pull_service.MergeAscStack, which is where a land refuses a person who may not merge
// (the queue hands it the permission of the doer).
func (l *rmLander) Land(_ context.Context, _ *user_model.User, perm access_model.Permission, pr *issues_model.PullRequest, _ pull_service.AscMergeOptions) (*pull_service.AscStackMergeResult, error) {
	if !perm.CanWrite(unit.TypePullRequests) {
		return nil, pull_service.ErrAscStackNotAllowed
	}
	l.lands = append(l.lands, pr.ID)
	return &pull_service.AscStackMergeResult{Merged: []*issues_model.PullRequest{pr}}, nil
}

func (*rmLander) Restack(context.Context, *user_model.User, *issues_model.PullRequest) error {
	return nil
}

func ascRequestMergeFixture(t *testing.T, backed bool) *rmLander {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	bottom := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	top := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 5})
	top.BaseBranch = "master"
	_, err := db.GetEngine(t.Context()).ID(top.ID).Cols("base_branch").Update(top)
	require.NoError(t, err)
	require.NoError(t, issues_model.SetPullRequestAscStack(t.Context(), bottom, rmBottom, 0))
	require.NoError(t, issues_model.SetPullRequestAscStack(t.Context(), top, rmTop, bottom.ID))

	l := &rmLander{}
	t.Cleanup(automerge.UseAscQueueLander(l))
	t.Cleanup(automerge.UseAscQueueKicker(func(int64, string) {}))
	prev := ascRequestMergeBacked
	ascRequestMergeBacked = func(*repo_model.Repository) (bool, error) { return backed, nil }
	t.Cleanup(func() { ascRequestMergeBacked = prev })
	// The queue and the stack service ask for themselves whether the repository is stored in asc (ascsync.BackedBy).
	isBacked := func(*repo_model.Repository) (bool, error) { return backed, nil }
	t.Cleanup(automerge.UseAscQueueBacked(isBacked))
	t.Cleanup(pull_service.UseAscStackBacked(isBacked))
	return l
}

func ascRequestMergeCall(t *testing.T, body string) (int, ascRequestMergeResponse) {
	t.Helper()
	ctx, resp := contexttest.MockPrivateContext(t, "POST /api/internal/asc/request-merge")
	ctx.Req.Method = http.MethodPost
	ctx.Req.Body = io.NopCloser(strings.NewReader(body))
	AscRequestMerge(ctx)
	var out ascRequestMergeResponse
	if resp.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	}
	return resp.Code, out
}

// ascRequestMergeCallAt posts as asc serve does: the repository is in the path and the body names the user and the
// prefix beside the change.
func ascRequestMergeCallAt(t *testing.T, owner, repo, body string) (int, ascRequestMergeResponse) {
	t.Helper()
	ctx, resp := contexttest.MockPrivateContext(t, "POST /api/internal/asc/request-merge/"+owner+"/"+repo)
	ctx.Req.Method = http.MethodPost
	ctx.Req.Body = io.NopCloser(strings.NewReader(body))
	rc := chi.RouteContext(ctx.Req.Context())
	require.NotNil(t, rc)
	rc.URLParams.Add("owner", owner)
	rc.URLParams.Add("repo", repo)
	AscRequestMerge(ctx)
	var out ascRequestMergeResponse
	if resp.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	}
	return resp.Code, out
}

func TestAscRequestMergeTakesTheRepositoryFromThePathAsAscServeSendsIt(t *testing.T) {
	l := ascRequestMergeFixture(t, true)
	body := `{"user_id":2,"user_name":"user2","change":"` + rmTop + `","now":true,"prefix":[{"change":"` + rmBottom + `","git":"0123456789012345678901234567890123456789"},{"change":"` + rmTop + `","git":"0123456789012345678901234567890123456789"}]}`
	code, out := ascRequestMergeCallAt(t, "user2", "repo1", body)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "landed", out.State)
	require.Len(t, l.lands, 1)

	// A body that names another repository than the path is not served.
	code, _ = ascRequestMergeCallAt(t, "user2", "repo1", `{"repo":"user2/repo2","user_id":2,"change":"`+rmTop+`"}`)
	assert.Equal(t, http.StatusBadRequest, code)
}

func rmBody(t *testing.T, change string, now bool, user int64) string {
	t.Helper()
	b, err := json.Marshal(ascRequestMergeRequest{Repo: "user2/repo1", Change: change, Now: now, UserID: user})
	require.NoError(t, err)
	return string(b)
}

func TestAscRequestMergeQueuesPrefix(t *testing.T) {
	l := ascRequestMergeFixture(t, true)
	code, out := ascRequestMergeCall(t, rmBody(t, rmTop, false, 2))
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "queued", out.State)
	assert.Equal(t, []string{rmBottom, rmTop}, out.Pending)
	assert.Empty(t, l.lands, "queueing lands nothing by itself")

	list, err := automerge.AscQueueList(t.Context(), 1, "master")
	require.NoError(t, err)
	require.Len(t, list, 2)
}

func TestAscRequestMergeNowLandsAtOnce(t *testing.T) {
	l := ascRequestMergeFixture(t, true)
	code, out := ascRequestMergeCall(t, rmBody(t, rmTop, true, 2))
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "landed", out.State)
	assert.Empty(t, out.Pending)
	require.Len(t, l.lands, 1)
}

func TestAscRequestMergeRefusals(t *testing.T) {
	l := ascRequestMergeFixture(t, true)
	for name, tc := range map[string]struct {
		body   string
		reason string
	}{
		"a user without the right to merge": {rmBody(t, rmTop, false, 5), "not allowed to merge this stack"},
		"a user without the right, now":     {rmBody(t, rmTop, true, 5), "not allowed to merge this stack"},
		"an unknown user":                   {rmBody(t, rmTop, false, 999999), "not allowed"},
		"an unknown change":                 {rmBody(t, "mmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmm", false, 2), "no such pull request"},
	} {
		code, out := ascRequestMergeCall(t, tc.body)
		require.Equal(t, http.StatusOK, code, name)
		assert.Equal(t, "refused", out.State, name)
		assert.Equal(t, tc.reason, out.Reason, name)
	}
	assert.Empty(t, l.lands, "no refused request lands")
	list, err := automerge.AscQueueList(t.Context(), 1, "master")
	require.NoError(t, err)
	assert.Empty(t, list, "no refused request queues")

	// An unknown repository looks like an unknown pull request.
	b, err := json.Marshal(ascRequestMergeRequest{Repo: "nobody/nothing", Change: rmTop, UserID: 2})
	require.NoError(t, err)
	code, out := ascRequestMergeCall(t, string(b))
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "refused", out.State)
	assert.Equal(t, "no such pull request", out.Reason)
}

func TestAscRequestMergeRefusedWithoutAsc(t *testing.T) {
	l := ascRequestMergeFixture(t, false)
	for _, now := range []bool{false, true} {
		code, out := ascRequestMergeCall(t, rmBody(t, rmTop, now, 2))
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "refused", out.State)
	}
	assert.Empty(t, l.lands)
}

func TestAscRequestMergeBadRequests(t *testing.T) {
	ascRequestMergeFixture(t, true)
	for name, body := range map[string]string{
		"empty":         ``,
		"not json":      `x`,
		"unknown field": `{"repo":"user2/repo1","change":"` + rmTop + `","user_id":2,"admin":true}`,
		"trailing":      rmBody(t, rmTop, false, 2) + `{}`,
		"bad change":    `{"repo":"user2/repo1","change":"../x","user_id":2}`,
		"no user":       `{"repo":"user2/repo1","change":"` + rmTop + `"}`,
		"bad repo":      `{"repo":"user2","change":"` + rmTop + `","user_id":2}`,
		"deep repo":     `{"repo":"a/b/c","change":"` + rmTop + `","user_id":2}`,
		"huge repo":     `{"repo":"` + strings.Repeat("a", 2000) + `/b","change":"` + rmTop + `","user_id":2}`,
	} {
		code, _ := ascRequestMergeCall(t, body)
		assert.Equal(t, http.StatusBadRequest, code, name)
	}
}

func TestAscRequestMergeDisabled(t *testing.T) {
	ascRequestMergeFixture(t, true)
	defer test.MockVariableValue(&setting.Asc.Enabled, false)()
	code, _ := ascRequestMergeCall(t, rmBody(t, rmTop, false, 2))
	assert.Equal(t, http.StatusNotFound, code)
}
