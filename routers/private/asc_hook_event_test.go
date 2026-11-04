// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package private

import (
	gocontext "context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	webhook_module "forgejo.org/modules/webhook"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ascHookCall struct {
	repo, id, event, state, before, after, summary, sender string
	run, changes                                           uint64
}

func fakeAscHooks(t *testing.T, err error) *[]ascHookCall {
	t.Helper()
	var calls []ascHookCall
	oldRun, oldPolicy := ascHookRun, ascHookPolicy
	t.Cleanup(func() { ascHookRun, ascHookPolicy = oldRun, oldPolicy })
	ascHookRun = func(_ gocontext.Context, repo *repo_model.Repository, id string, event webhook_module.HookEventType, run uint64, state string, changes uint64) error {
		calls = append(calls, ascHookCall{repo: repo.FullName(), id: id, event: string(event), run: run, state: state, changes: changes})
		return err
	}
	ascHookPolicy = func(_ gocontext.Context, repo *repo_model.Repository, id, before, after, summary, sender string) error {
		calls = append(calls, ascHookCall{repo: repo.FullName(), id: id, event: "policy.changed", before: before, after: after, summary: summary, sender: sender})
		return err
	}
	return &calls
}

func ascHookEventCall(t *testing.T, id, body string) int {
	t.Helper()
	ctx, resp := contexttest.MockPrivateContext(t, "POST /api/internal/asc/hook-event")
	ctx.Req.Method = http.MethodPost
	ctx.Req.Body = io.NopCloser(strings.NewReader(body))
	if id != "" {
		ctx.Req.Header.Set("X-Asc-Event-Id", id)
	}
	AscHookEvent(ctx)
	return resp.Code
}

func TestAscHookEventQueuesRunsAndPolicyChanges(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	calls := fakeAscHooks(t, nil)

	assert.Equal(t, http.StatusOK, ascHookEventCall(t, "op-1", `{"event":"agent_run.started","repo":"user2/repo1","run":7}`))
	assert.Equal(t, http.StatusOK, ascHookEventCall(t, "op-2", `{"event":"agent_run.finished","repo":"user2/repo1","run":7,"state":"failed"}`))
	assert.Equal(t, http.StatusOK, ascHookEventCall(t, "op-3",
		`{"event":"policy.changed","repo":"user2/repo1","before":"ab12","after":"cd34","summary":"The rules of the repository changed.","sender":"user2"}`))
	assert.Equal(t, []ascHookCall{
		{repo: "user2/repo1", id: "op-1", event: "agent_run.started", run: 7},
		{repo: "user2/repo1", id: "op-2", event: "agent_run.finished", run: 7, state: "failed"},
		{repo: "user2/repo1", id: "op-3", event: "policy.changed", before: "ab12", after: "cd34", summary: "The rules of the repository changed.", sender: "user2"},
	}, *calls)

	// a repository that is gone is not an error: asc's outbox drops the event
	assert.Equal(t, http.StatusOK, ascHookEventCall(t, "op-4", `{"event":"agent_run.started","repo":"user2/gone","run":7}`))
	assert.Len(t, *calls, 3)
}

func TestAscHookEventRefusesWhatIsNotAnEvent(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	calls := fakeAscHooks(t, nil)

	for _, c := range []struct{ id, body string }{
		{"", `{"event":"agent_run.started","repo":"user2/repo1","run":7}`},
		{"op 1", `{"event":"agent_run.started","repo":"user2/repo1","run":7}`},
		{"op-1", `{"event":"change.landed","repo":"user2/repo1","run":7}`},
		{"op-1", `{"event":"push","repo":"user2/repo1"}`},
		{"op-1", `{"event":"agent_run.started","repo":"user2/repo1"}`},
		{"op-1", `{"event":"agent_run.started","repo":"user2/repo1","run":7,"state":"failed"}`},
		{"op-1", `{"event":"agent_run.finished","repo":"user2/repo1","run":7}`},
		{"op-1", `{"event":"agent_run.finished","repo":"user2/repo1","run":7,"state":"Failed!"}`},
		{"op-1", `{"event":"agent_run.started","repo":"user2","run":7}`},
		{"op-1", `{"event":"agent_run.started","repo":"user2/repo1/x","run":7}`},
		{"op-1", `{"event":"agent_run.started","repo":"user2/repo1","run":7,"extra":1}`},
		{"op-1", `{"event":"agent_run.started","repo":"user2/repo1","run":7,"sender":"a b"}`},
		{"op-1", `{"event":"policy.changed","repo":"user2/repo1","before":"ab","after":"cd"}`},
		{"op-1", `{"event":"policy.changed","repo":"user2/repo1","before":"AB","after":"cd","summary":"x"}`},
		{"op-1", `{"event":"policy.changed","repo":"user2/repo1","before":"ab","after":"cd","summary":"<b>x</b>"}`},
		{"op-1", `{"event":"policy.changed","repo":"user2/repo1","before":"ab","after":"cd","summary":"x","run":1}`},
	} {
		assert.Equal(t, http.StatusBadRequest, ascHookEventCall(t, c.id, c.body), c.body)
	}
	assert.Empty(t, *calls)
}

func TestAscHookEventErrorsAreRetried(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	fakeAscHooks(t, errors.New("database is down"))
	assert.Equal(t, http.StatusInternalServerError, ascHookEventCall(t, "op-1", `{"event":"agent_run.started","repo":"user2/repo1","run":7}`))

	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, false))
	assert.Equal(t, http.StatusNotFound, ascHookEventCall(t, "op-1", `{"event":"agent_run.started","repo":"user2/repo1","run":7}`))
}
