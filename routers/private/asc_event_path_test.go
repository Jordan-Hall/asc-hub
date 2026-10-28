// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	"io"
	"net/http"
	"strings"
	"testing"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscEventPathsBecomeThePagesOfTheHub(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	other := "/" + repo.FullName()
	pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	require.NoError(t, issues_model.SetPullRequestAscStack(ctx, pr, rmBottom, 0))
	require.NoError(t, pr.LoadIssue(ctx))
	pull := other + "/pulls/" + itoaForTest(pr.Issue.Index)

	for in, want := range map[string]string{
		"":                                              other,
		other:                                           other,
		other + "/":                                     other,
		"/USER2/REPO1/pulls/3":                          other + "/pulls/3", // the case of the repository is the repository's own
		other + "/pulls/3":                              other + "/pulls/3",
		other + "/pulls/3?tab=files":                    other + "/pulls/3?tab=files",
		other + "/pulls/3#issuecomment-9":               other + "/pulls/3#issuecomment-9",
		other + "/pulls/3?a=<script>":                   other + "/pulls/3",
		other + "/issues/4":                             other + "/issues/4",
		other + "/changes/" + rmBottom:                  pull,
		other + "/changes/" + rmTop:                     other + "/pulls", // a change with no pull request yet
		other + "/changes/nothex":                       other,
		other + "/agents":                               other + "/asc/agents",
		other + "/agents/runs/run_01HX-9":               other + "/asc/agents/runs/run_01HX-9",
		other + "/agents/runs/a b":                      other,
		other + "/agents/runs/":                         other + "/asc/agents",
		other + "/merge-queue":                          other + "/asc/queue",
		other + "/settings/policy":                      other + "/settings/asc/policy",
		other + "/asc/agents/runs/9":                    other + "/asc/agents/runs/9",
		other + "/asc/queue":                            other + "/asc/queue",
		other + "/settings/asc/audit":                   other + "/settings/asc/audit",
		other + "/settings/keys":                        other, // a page asc has no business linking to
		other + "/settings/collaboration":               other,
		"/user2/repo2/pulls/1":                          other, // another repository
		"/user2/repo1-other/pulls/1":                    other,
		"/user2":                                        other,
		other + "/pulls/../../admin":                    other,
		other + "//evil.example":                        other,
		other + "/pulls/3" + string(rune(92)) + "x":     other,
		"/-/admin/users":                                other,
		other + "/changes/" + strings.ToUpper(rmBottom): other,
	} {
		assert.Equal(t, want, ascCanonicalPath(ctx, repo, in), in)
	}

	// the sub-path of the site is not part of the stored path
	defer test.MockVariableValue(&setting.AppSubURL, "/hub")()
	assert.Equal(t, other+"/pulls/3", ascCanonicalPath(ctx, repo, "/hub"+other+"/pulls/3"))
	assert.Equal(t, other+"/asc/queue", ascCanonicalPath(ctx, repo, other+"/merge-queue"))
}

func itoaForTest(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestAscEventReviewsAndMentionsAndRetries(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	call := func(req ascEventRequest, id string) (int, ascEventResponse) {
		body, err := json.Marshal(req)
		require.NoError(t, err)
		ctx, resp := contexttest.MockPrivateContext(t, "POST /api/internal/asc/event")
		ctx.Req.Method = http.MethodPost
		ctx.Req.Body = io.NopCloser(strings.NewReader(string(body)))
		if id != "" {
			ctx.Req.Header.Set("X-Asc-Event-Id", id)
		}
		AscEvent(ctx)
		var out ascEventResponse
		if resp.Code == http.StatusOK {
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
		}
		return resp.Code, out
	}
	for _, event := range []string{"review_requested", "mentioned"} {
		status, out := call(ascEventRequest{Event: event, Repo: "user2/repo1", Users: []string{"user2"}, Title: "x", Path: "/user2/repo1/changes/" + rmBottom}, "")
		require.Equal(t, http.StatusOK, status, event)
		assert.Equal(t, 1, out.Notified, event)
	}

	// a retry with the same event id tells nobody twice
	req := ascEventRequest{Event: "checks_failed", Repo: "user2/repo1", Users: []string{"user2", "user4"}, Title: "red"}
	_, first := call(req, "evt-0001")
	_, again := call(req, "evt-0001")
	_, other := call(req, "evt-0002")
	assert.Equal(t, 2, first.Notified)
	assert.Zero(t, again.Notified)
	assert.Equal(t, 2, other.Notified)
	has, err := user_model.HasAscNotificationEvent(t.Context(), 2, "evt-0001")
	require.NoError(t, err)
	assert.True(t, has)

	// the stored link is the canonical one
	var path string
	eng, err := unittest.GetXORMEngine()
	require.NoError(t, err)
	ok, err := eng.Table("asc_notify_log").Cols("path").Where("event = ?", "mentioned").Get(&path)
	require.NoError(t, err)
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(path, "/user2/repo1/pulls"), path)

	// an event id that is not one is refused
	status, _ := call(req, "bad id with spaces")
	assert.Equal(t, http.StatusBadRequest, status)
	status, _ = call(req, strings.Repeat("a", 65))
	assert.Equal(t, http.StatusBadRequest, status)
}
