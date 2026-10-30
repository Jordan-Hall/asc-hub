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
	"forgejo.org/modules/json"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ascRunFinishedCall(t *testing.T, body string) (int, ascRunFinishedResponse) {
	t.Helper()
	ctx, resp := contexttest.MockPrivateContext(t, "POST /api/internal/asc/agent-run-finished")
	ctx.Req.Method = http.MethodPost
	ctx.Req.Body = io.NopCloser(strings.NewReader(body))
	AscRunFinished(ctx)
	var out ascRunFinishedResponse
	if resp.Code == http.StatusOK || resp.Code == http.StatusServiceUnavailable {
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	}
	return resp.Code, out
}

func fakeOpener(t *testing.T, res *ascagentui.RunFinishedResult, err error) *[]uint64 {
	t.Helper()
	var asked []uint64
	old := ascOpenFinishedRun
	t.Cleanup(func() { ascOpenFinishedRun = old })
	ascOpenFinishedRun = func(_ gocontext.Context, _ *repo_model.Repository, id uint64) (*ascagentui.RunFinishedResult, error) {
		asked = append(asked, id)
		return res, err
	}
	return &asked
}

const ascRunFinishedBody = `{"repo":"user2/repo1","run":7,"state":"succeeded","changes":2}`

func TestAscRunFinishedOpensARunThatSucceeded(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	t.Cleanup(test.MockVariableValue(&setting.Asc.AutoOpenRunPulls, true))

	asked := fakeOpener(t, &ascagentui.RunFinishedResult{Outcome: ascagentui.RunFinishedOpened, Opened: 2}, nil)
	status, out := ascRunFinishedCall(t, ascRunFinishedBody)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, 2, out.Opened)
	assert.Equal(t, []uint64{7}, *asked)
}

func TestAscRunFinishedAnswersLaterWhenAChangeIsNotThereYet(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	fakeOpener(t, &ascagentui.RunFinishedResult{Outcome: ascagentui.RunFinishedWaiting, Waiting: 1}, nil)
	status, out := ascRunFinishedCall(t, ascRunFinishedBody)
	assert.Equal(t, http.StatusServiceUnavailable, status, "asc's outbox delivers the event again")
	assert.Equal(t, ascagentui.RunFinishedWaiting, out.Outcome)
}

func TestAscRunFinishedErrorsAreRetriedAndSkipsAreNot(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))

	fakeOpener(t, nil, errors.New("asc is down"))
	status, _ := ascRunFinishedCall(t, ascRunFinishedBody)
	assert.Equal(t, http.StatusInternalServerError, status)

	fakeOpener(t, &ascagentui.RunFinishedResult{Outcome: ascagentui.RunFinishedSkipped, Reason: "x"}, nil)
	status, out := ascRunFinishedCall(t, ascRunFinishedBody)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, ascagentui.RunFinishedSkipped, out.Outcome)
}

func TestAscRunFinishedDoesNothingWhenItShouldNot(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	asked := fakeOpener(t, &ascagentui.RunFinishedResult{Outcome: ascagentui.RunFinishedOpened}, nil)

	for name, body := range map[string]string{
		"a run that failed":         `{"repo":"user2/repo1","run":7,"state":"failed","changes":2}`,
		"a run with no change":      `{"repo":"user2/repo1","run":7,"state":"succeeded","changes":0}`,
		"a repository that is gone": `{"repo":"user2/no_such_repo","run":7,"state":"succeeded","changes":2}`,
	} {
		status, out := ascRunFinishedCall(t, body)
		assert.Equal(t, http.StatusOK, status, name)
		assert.Equal(t, ascagentui.RunFinishedSkipped, out.Outcome, name)
	}
	assert.Empty(t, *asked)

	t.Cleanup(test.MockVariableValue(&setting.Asc.AutoOpenRunPulls, false))
	status, out := ascRunFinishedCall(t, ascRunFinishedBody)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, ascagentui.RunFinishedSkipped, out.Outcome, "the switch is off")
	assert.Empty(t, *asked)
}

func TestAscRunFinishedRefusesWhatItDoesNotKnow(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	asked := fakeOpener(t, &ascagentui.RunFinishedResult{Outcome: ascagentui.RunFinishedOpened}, nil)
	for name, body := range map[string]string{
		"an unknown field":     `{"repo":"user2/repo1","run":7,"state":"succeeded","changes":2,"extra":1}`,
		"no run":               `{"repo":"user2/repo1","state":"succeeded","changes":2}`,
		"a run too large":      `{"repo":"user2/repo1","run":18446744073709551615,"state":"succeeded","changes":2}`,
		"a repo without owner": `{"repo":"repo1","run":7,"state":"succeeded","changes":2}`,
		"a repo with a path":   `{"repo":"a/b/c","run":7,"state":"succeeded","changes":2}`,
		"no state":             `{"repo":"user2/repo1","run":7,"state":"","changes":2}`,
		"not json":             `nope`,
		"trailing data":        ascRunFinishedBody + ` {}`,
	} {
		status, _ := ascRunFinishedCall(t, body)
		assert.Equal(t, http.StatusBadRequest, status, name)
	}
	assert.Empty(t, *asked)

	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, false))
	status, _ := ascRunFinishedCall(t, ascRunFinishedBody)
	assert.Equal(t, http.StatusNotFound, status, "asc is off")
}
