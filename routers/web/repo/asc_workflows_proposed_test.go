// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascagentui"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestAscWorkflowRunShowsWhatTheAgentStepProposed(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	setStorageBackend(t, 1, "asc")
	hub := startFakeHub(t)

	// three changes of the agent run: one landed, one waiting for review, one never submitted
	c1, c2, c3 := changeID(10), changeID(40), changeID(70)
	merged := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
	open := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 2})
	require.NoError(t, issues_model.SetPullRequestAscStack(db.DefaultContext, merged, ascagentui.ChangeText(c1), 0))
	require.NoError(t, issues_model.SetPullRequestAscStack(db.DefaultContext, open, ascagentui.ChangeText(c2), merged.ID))

	hub.ok("Workflows.GetRun", &ascv1.GetWorkflowRunResponse{Run: sampleWorkflowRun(3, ascv1.WorkflowRunState_WORKFLOW_RUN_STATE_RUNNING)})
	hub.on("Agents.GetRun", func(m proto.Message) (proto.Message, error) {
		r := sampleRun(m.(*ascv1.GetAgentRunRequest).GetId(), ascv1.AgentRunState_AGENT_RUN_STATE_SUCCEEDED)
		r.Changes = [][]byte{c1, c2, c3, {1, 2, 3}}
		return &ascv1.GetAgentRunResponse{Run: r}, nil
	})
	page := func(user int64, features bool) agentsPage {
		return runAgents(t, agentsReq{path: testWorkflowsPath + "/runs/3", user: user, features: features, params: map[string]string{":id": "3"}}, AscWorkflowRun)
	}

	p := page(agentsOwner, true)
	require.Equal(t, http.StatusOK, p.status, p.body)
	assert.Contains(t, p.body, `data-testid="asc-wf-proposed"`)
	assert.Contains(t, p.body, "asc_review.wf_proposed")
	assert.Contains(t, p.body, ascagentui.ShortChange(ascagentui.ChangeText(c1)))
	assert.Contains(t, p.body, "asc_review.run_change_landed")
	assert.Contains(t, p.body, "asc_review.run_change_draft", "a change never submitted for review has no link")
	assert.Contains(t, p.body, `href="/user2/repo1/pulls/`+idStr(uint64(open.Index))+`"`)
	assert.Equal(t, 3, strings.Count(p.body, `class="rv-result-row"`), "the id of the wrong length is skipped")

	t.Run("without the agent switch nothing of the agent run is read", func(t *testing.T) {
		assert.NotContains(t, page(agentsOwner, false).body, "asc-wf-proposed")
	})
	t.Run("a step the viewer may not read shows nothing", func(t *testing.T) {
		hub.on("Agents.GetRun", func(m proto.Message) (proto.Message, error) {
			r := sampleRun(m.(*ascv1.GetAgentRunRequest).GetId(), ascv1.AgentRunState_AGENT_RUN_STATE_SUCCEEDED)
			r.Changes, r.CreatedById = [][]byte{c1}, 9999
			return &ascv1.GetAgentRunResponse{Run: r}, nil
		})
		p := page(agentsReader, true)
		require.Equal(t, http.StatusOK, p.status)
		assert.NotContains(t, p.body, "asc-wf-proposed")
	})
	t.Run("an agent run of another repository is not listed", func(t *testing.T) {
		hub.on("Agents.GetRun", func(m proto.Message) (proto.Message, error) {
			r := sampleRun(m.(*ascv1.GetAgentRunRequest).GetId(), ascv1.AgentRunState_AGENT_RUN_STATE_SUCCEEDED)
			r.Repo, r.Changes = "user3/secret", [][]byte{c1}
			return &ascv1.GetAgentRunResponse{Run: r}, nil
		})
		assert.NotContains(t, page(agentsOwner, true).body, "asc-wf-proposed")
	})
}
