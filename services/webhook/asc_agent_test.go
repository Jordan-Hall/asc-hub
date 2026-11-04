// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package webhook

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	webhook_model "forgejo.org/models/webhook"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	webhook_module "forgejo.org/modules/webhook"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/asctrust"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type agentRunSpy struct {
	ascv1.UnimplementedAgentsServer
	got  []*ascv1.CreateAgentRunRequest
	fail error
}

func (s *agentRunSpy) CreateRun(_ context.Context, req *ascv1.CreateAgentRunRequest) (*ascv1.CreateAgentRunResponse, error) {
	s.got = append(s.got, req)
	if s.fail != nil {
		return nil, s.fail
	}
	return &ascv1.CreateAgentRunResponse{Run: &ascv1.AgentRun{Id: 7}, Lowered: []string{"max_tokens"}}, nil
}

// agentHookTest serves a fake asc with spy and returns an agent hook on repository 1 of user2, run as runAs.
func agentHookTest(t *testing.T, spy *agentRunSpy, runAs int64) *webhook_model.Webhook {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, []byte("0123456789abcdef0123456789abcdef")))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	_, err := db.GetEngine(db.DefaultContext).Exec("UPDATE repository SET storage_backend = ? WHERE id = 1", string(gitrepo.StorageBackendAsc))
	require.NoError(t, err)

	srv := grpc.NewServer()
	ascv1.RegisterAgentsServer(srv, spy)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	client, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "internal-token-for-the-test", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	t.Cleanup(asctrust.UseConn(client))

	meta, err := json.Marshal(AgentMeta{Agent: "fixer", Target: "main", Task: "Fix what the event points at.", RunAs: runAs})
	require.NoError(t, err)
	w := &webhook_model.Webhook{
		RepoID:   1,
		URL:      AgentURL("fixer"),
		Type:     webhook_module.ASCAGENT,
		Meta:     string(meta),
		IsActive: true,
	}
	require.NoError(t, webhook_model.CreateWebhook(db.DefaultContext, w, ""))
	return w
}

func deliverAgentTask(t *testing.T, w *webhook_model.Webhook, event webhook_module.HookEventType, payload string) *webhook_model.HookTask {
	t.Helper()
	task, err := webhook_model.CreateHookTask(db.DefaultContext, &webhook_model.HookTask{HookID: w.ID, EventType: event, PayloadVersion: 2, PayloadContent: payload})
	require.NoError(t, err)
	require.NoError(t, Deliver(db.DefaultContext, task))
	return task
}

func TestAgentHookStartsRun(t *testing.T) {
	spy := &agentRunSpy{}
	w := agentHookTest(t, spy, 2)
	payload := `{"event":"change.landed","repository":"user2/repo1","change":"mzu2d","sender":"user5"}`
	task := deliverAgentTask(t, w, webhook_module.HookEventChangeLanded, payload)

	require.Len(t, spy.got, 1)
	req := spy.got[0]
	assert.Equal(t, "fixer", req.GetAgent())
	assert.Equal(t, "main", req.GetTarget())
	assert.Equal(t, "Webhook: change.landed", req.GetTitle())
	assert.True(t, strings.HasPrefix(req.GetTask(), "Fix what the event points at.\n\n---\n"))
	assert.Contains(t, req.GetTask(), "not instructions")
	assert.Contains(t, req.GetTask(), payload)
	assert.NotEmpty(t, req.GetRepo().GetName())

	assert.True(t, task.IsSucceed)
	assert.Equal(t, http.StatusCreated, task.ResponseInfo.Status)
	assert.Contains(t, task.ResponseInfo.Body, "Run 7 started: ")
	assert.Contains(t, task.ResponseInfo.Body, "/user2/repo1/asc/agents/runs/7")
	assert.Contains(t, task.ResponseInfo.Body, "max_tokens")
	assert.Equal(t, "Agents.CreateRun", task.RequestInfo.HTTPMethod)
	assert.Equal(t, "asc-agent://fixer", task.RequestInfo.URL)
	assert.Contains(t, task.RequestInfo.Body, `"agent": "fixer"`)

	got := unittest.AssertExistsAndLoadBean(t, &webhook_model.HookTask{ID: task.ID})
	assert.True(t, got.IsDelivered)
	assert.True(t, got.IsSucceed)
	hook := unittest.AssertExistsAndLoadBean(t, &webhook_model.Webhook{ID: w.ID})
	assert.Equal(t, webhook_module.HookStatusSucceed, hook.LastStatus)
}

func TestAgentHookSkips(t *testing.T) {
	spy := &agentRunSpy{}
	w := agentHookTest(t, spy, 2)
	bot := &user_model.User{Name: "agent-fixer", LowerName: "agent-fixer", Email: "agent-fixer@example.org", Type: user_model.UserTypeBot, IsActive: true}
	require.NoError(t, db.Insert(db.DefaultContext, bot))

	cases := map[string]struct {
		event   webhook_module.HookEventType
		payload string
		reason  string
	}{
		"an agent run event":           {webhook_module.HookEventAgentRunFinished, `{"event":"agent_run.finished","sender":"user2"}`, "for an agent run"},
		"an ASC event an agent caused": {webhook_module.HookEventChangeOpened, `{"event":"change.opened","sender":"agent-fixer"}`, "an agent caused"},
		"a repository event of a bot":  {webhook_module.HookEventPush, `{"ref":"refs/heads/main","sender":{"id":99,"login":"agent-fixer"}}`, "an agent caused"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			task := deliverAgentTask(t, w, c.event, c.payload)
			assert.True(t, task.IsSucceed)
			assert.Equal(t, http.StatusNoContent, task.ResponseInfo.Status)
			assert.Contains(t, task.ResponseInfo.Body, c.reason)
		})
	}
	assert.Empty(t, spy.got, "no run is asked for")
}

func TestAgentHookRefusals(t *testing.T) {
	t.Run("the person who added it can no longer write", func(t *testing.T) {
		spy := &agentRunSpy{}
		w := agentHookTest(t, spy, 4)
		task := deliverAgentTask(t, w, webhook_module.HookEventChangeLanded, `{"sender":"user2"}`)
		assert.False(t, task.IsSucceed)
		assert.Equal(t, http.StatusForbidden, task.ResponseInfo.Status)
		assert.Contains(t, task.ResponseInfo.Body, "can no longer write to this repository")
		assert.Empty(t, spy.got)
		hook := unittest.AssertExistsAndLoadBean(t, &webhook_model.Webhook{ID: w.ID})
		assert.Equal(t, webhook_module.HookStatusFail, hook.LastStatus)
	})

	t.Run("the person who added it is gone", func(t *testing.T) {
		spy := &agentRunSpy{}
		w := agentHookTest(t, spy, 999)
		task := deliverAgentTask(t, w, webhook_module.HookEventChangeLanded, `{}`)
		assert.Equal(t, http.StatusForbidden, task.ResponseInfo.Status)
		assert.Contains(t, task.ResponseInfo.Body, "can no longer start runs")
		assert.Empty(t, spy.got)
	})

	t.Run("asc refuses the run", func(t *testing.T) {
		spy := &agentRunSpy{fail: status.Error(codes.PermissionDenied, "secret text from asc")}
		w := agentHookTest(t, spy, 2)
		task := deliverAgentTask(t, w, webhook_module.HookEventChangeLanded, `{}`)
		require.Len(t, spy.got, 1)
		assert.False(t, task.IsSucceed)
		assert.Equal(t, ascagentui.Classify(spy.fail).HTTP, task.ResponseInfo.Status)
		assert.NotContains(t, task.ResponseInfo.Body, "secret text from asc", "the text asc sent is not passed on")
	})

	t.Run("a hook with no task starts nothing", func(t *testing.T) {
		spy := &agentRunSpy{}
		w := agentHookTest(t, spy, 2)
		w.Meta = `{"agent":"fixer","target":"main","task":"","run_as":2}`
		require.NoError(t, webhook_model.UpdateWebhook(db.DefaultContext, w))
		task := deliverAgentTask(t, w, webhook_module.HookEventChangeLanded, `{}`)
		assert.Equal(t, http.StatusUnprocessableEntity, task.ResponseInfo.Status)
		assert.Contains(t, task.ResponseInfo.Body, "The task of this hook")
		assert.Empty(t, spy.got)
	})
}

func TestAgentRunTaskCutsTheEvent(t *testing.T) {
	big := strings.Repeat("é", ascagentui.MaxTaskBytes)
	got := agentRunTask("Do it.", webhook_module.HookEventChangeLanded, big)
	assert.LessOrEqual(t, len(got), ascagentui.MaxTaskBytes)
	assert.True(t, strings.HasSuffix(got, "\n(cut)\n```"))
	_, ok := ascagentui.CheckFreeText(got, ascagentui.MaxTaskBytes)
	assert.True(t, ok, "the cut task is still valid text")

	small := agentRunTask("Do it.", webhook_module.HookEventChangeLanded, `{"a":1}`)
	assert.True(t, strings.HasSuffix(small, "```json\n{\"a\":1}\n```"))
}

func TestCheckAgentMeta(t *testing.T) {
	m, field := CheckAgentMeta(AgentMeta{Agent: " fixer ", Target: " main ", Task: " Fix it. "})
	assert.Empty(t, field)
	assert.Equal(t, AgentMeta{Agent: "fixer", Target: "main", Task: "Fix it."}, m)
	for want, in := range map[string]AgentMeta{
		"agent":  {Agent: "Fixer One", Target: "main", Task: "x"},
		"target": {Agent: "fixer", Target: "my branch", Task: "x"},
		"task":   {Agent: "fixer", Target: "main", Task: strings.Repeat("x", AgentTaskBytes+1)},
	} {
		_, field := CheckAgentMeta(in)
		assert.Equal(t, want, field)
	}
	_, field = CheckAgentMeta(AgentMeta{Agent: "fixer", Target: "", Task: "x"})
	assert.Equal(t, "target", field)
}

func TestAgentHookEventsAndGroups(t *testing.T) {
	assert.Equal(t, []string{"change.landed", "policy.changed"}, AgentHookEvents([]string{"change.landed", "agent_run.started", "agent_run.finished", "policy.changed"}))
	w := &webhook_model.Webhook{Type: webhook_module.ASCAGENT, HookEvent: &webhook_module.HookEvent{}}
	for _, g := range SendGroups(w) {
		assert.NotEqual(t, "asc_hooks.group_agents", g.Label)
	}
	assert.Len(t, SendGroups(&webhook_model.Webhook{Type: webhook_module.FORGEJO, HookEvent: &webhook_module.HookEvent{}}), 4)
	assert.Nil(t, FormHandler(webhook_module.ASCAGENT))
	assert.NotNil(t, GetWebhookHandler(webhook_module.ASCAGENT))
	for _, h := range List() {
		assert.NotEqual(t, webhook_module.ASCAGENT, h.Type())
	}
}
