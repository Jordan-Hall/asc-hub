// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package byoweb

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const usedChange = "kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkz"

func useRows(t *testing.T, body string) *goquery.Selection {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	require.NoError(t, err)
	return doc.Find(`[data-testid="asc-key-uses"] tr[data-testid="asc-key-use"]`)
}

// The Activity tab lists who used the key and for what, with links the viewer may follow, and pages through them.
func TestAIKeyUses(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := setup(t)
	lvl := userLevel(t)
	k := f.AddKey(lvl.Scope.Owner(), "Team", ascv1.AiProvider_AI_PROVIDER_ANTHROPIC, ascv1.AiKeyState_AI_KEY_STATE_ACTIVE)
	id := k.GetId()

	r := run(t, AIKey, lvl, http.MethodGet, url.Values{"tab": {"activity"}}, id)
	require.Equal(t, http.StatusOK, r.resp.Code, r.body())
	assert.Contains(t, r.body(), `data-testid="asc-key-uses-empty"`, "a key no one used says so")

	// The change has a pull request in user2/repo1 (pull 1).
	_, err := db.GetEngine(t.Context()).Insert(&issues_model.PullRequestAscStack{PullID: 1, RepoID: 1, AscChange: usedChange})
	require.NoError(t, err)
	f.Uses = map[string][]*ascv1.AiActivity{id: {
		{Kind: ascv1.AiActivityKind_AI_ACTIVITY_KIND_AGENT_RUN, Subject: "42", Actor: "carol", Repo: "user2/repo1", Agent: "fixer",
			Model: "claude-x", FirstAt: 1_790_000_000, LastAt: 1_790_000_600, Requests: 3, InputTokens: 300, OutputTokens: 120, CostMicroUsd: 4_500_000},
		{Kind: ascv1.AiActivityKind_AI_ACTIVITY_KIND_REVIEW_SUMMARY, Subject: usedChange, Actor: "dave", Repo: "user2/repo1", Requests: 1},
		{Kind: ascv1.AiActivityKind_AI_ACTIVITY_KIND_REVIEW_SUMMARY, Subject: usedChange, Actor: "dave", Repo: "user15/big_test_private_1", Requests: 1},
		{Kind: ascv1.AiActivityKind_AI_ACTIVITY_KIND_AGENT_RUN, Subject: "7", Repo: "ghost/none", Requests: 1},
		{Kind: ascv1.AiActivityKind_AI_ACTIVITY_KIND_KEY_TEST, Subject: "2026-11-09", Requests: 1, UnpricedRequests: 1},
	}}
	r = run(t, AIKey, lvl, http.MethodGet, url.Values{"tab": {"activity"}}, id)
	require.Equal(t, http.StatusOK, r.resp.Code, r.body())
	rows := useRows(t, r.body())
	require.Equal(t, 5, rows.Length())

	runRow := rows.Eq(0)
	assert.Contains(t, runRow.Text(), tr(t, "ascbyo.ai.uses.kind_run"))
	assert.Equal(t, "/user2/repo1/asc/agents/runs/42", runRow.Find(`a[title="42"]`).AttrOr("href", ""))
	assert.Equal(t, "/user2/repo1", runRow.Find(`a[href="/user2/repo1"]`).AttrOr("href", ""))
	assert.Contains(t, runRow.Text(), "carol")
	assert.Contains(t, runRow.Text(), "fixer")
	assert.Contains(t, runRow.Text(), "claude-x")
	assert.Contains(t, runRow.Text(), "$4.50")

	summary := rows.Eq(1)
	assert.Contains(t, summary.Text(), tr(t, "ascbyo.ai.uses.kind_summary"))
	assert.Regexp(t, `^/user2/repo1/pulls/[0-9]+$`, summary.Find(`a[title="`+usedChange+`"]`).AttrOr("href", ""))

	// A repository the viewer cannot read, or one that does not exist, gives no link.
	for _, i := range []int{2, 3} {
		assert.Zero(t, rows.Eq(i).Find("a").Length(), "row %d", i)
	}
	assert.Contains(t, rows.Eq(2).Text(), "user15/big_test_private_1", "the name is still shown")

	test := rows.Eq(4)
	assert.Contains(t, test.Text(), tr(t, "ascbyo.ai.uses.kind_test"))
	assert.Contains(t, test.Text(), tr(t, "ascbyo.ai.uses.nobody"))
	assert.Contains(t, test.Text(), tr(t, "ascbyo.ai.uses.unpriced", 1))

	// With the agents switched off in the repository, the run is not linked.
	require.NoError(t, repo_model.SetAscFeature(t.Context(), 1, repo_model.AscFeatureAgents, false))
	r = run(t, AIKey, lvl, http.MethodGet, url.Values{"tab": {"activity"}}, id)
	assert.Zero(t, useRows(t, r.body()).Eq(0).Find(`a[title="42"]`).Length())
	require.NoError(t, repo_model.SetAscFeature(t.Context(), 1, repo_model.AscFeatureAgents, true))

	// Paging: the next page opens the activity tab at the older uses, and offers the newest again.
	var many []*ascv1.AiActivity
	for i := range 60 {
		many = append(many, &ascv1.AiActivity{Kind: ascv1.AiActivityKind_AI_ACTIVITY_KIND_AGENT_RUN, Subject: strconv.Itoa(i + 1), Requests: 1})
	}
	f.Uses[id] = many
	r = run(t, AIKey, lvl, http.MethodGet, url.Values{"tab": {"activity"}}, id)
	assert.Equal(t, 50, useRows(t, r.body()).Length())
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(r.body()))
	require.NoError(t, err)
	next := doc.Find(`[data-testid="asc-key-uses-next"]`).AttrOr("href", "")
	assert.Equal(t, "/user/settings/asc/ai/keys/"+id+"?tab=activity&uses=o50", next)
	assert.Zero(t, doc.Find(`[data-testid="asc-key-uses-first"]`).Length())

	r = run(t, AIKey, lvl, http.MethodGet, url.Values{"tab": {"activity"}, "uses": {"o50"}}, id)
	assert.Equal(t, 10, useRows(t, r.body()).Length())
	doc, err = goquery.NewDocumentFromReader(strings.NewReader(r.body()))
	require.NoError(t, err)
	assert.Equal(t, "/user/settings/asc/ai/keys/"+id+"?tab=activity", doc.Find(`[data-testid="asc-key-uses-first"]`).AttrOr("href", ""))
	assert.Zero(t, doc.Find(`[data-testid="asc-key-uses-next"]`).Length())
	assert.Contains(t, r.body(), `id="asc-key-tab-activity" aria-controls="asc-key-activity" aria-selected="true"`)

	// A bad page token reads the first page.
	r = run(t, AIKey, lvl, http.MethodGet, url.Values{"tab": {"activity"}, "uses": {"o50&x"}}, id)
	assert.Equal(t, 50, useRows(t, r.body()).Length())

	// An asc without the call leaves the table saying so, and the rest of the page works.
	f.SetFail("/asc.v1.AiProviders/ListActivity", status.Error(codes.Unimplemented, "no ListActivity"))
	defer f.SetFail("/asc.v1.AiProviders/ListActivity", nil)
	r = run(t, AIKey, lvl, http.MethodGet, url.Values{"tab": {"activity"}}, id)
	require.Equal(t, http.StatusOK, r.resp.Code)
	assert.Contains(t, r.body(), `data-testid="asc-key-uses-none"`)
	assert.Contains(t, r.body(), `data-testid="asc-key-usage"`)
}
