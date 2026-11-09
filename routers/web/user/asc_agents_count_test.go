// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeCountAsc struct {
	fakeAgentsAsc
	mu     sync.Mutex
	counts []*ascv1.CountAgentRunsRequest
	answer func(*ascv1.CountAgentRunsRequest) (*ascv1.CountAgentRunsResponse, error)
}

func (f *fakeCountAsc) CountRuns(_ context.Context, req *ascv1.CountAgentRunsRequest) (*ascv1.CountAgentRunsResponse, error) {
	f.mu.Lock()
	f.counts = append(f.counts, req)
	answer := f.answer
	f.mu.Unlock()
	return answer(req)
}

func (f *fakeCountAsc) calls() []*ascv1.CountAgentRunsRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*ascv1.CountAgentRunsRequest(nil), f.counts...)
}

func getAscAgentsCount(t *testing.T, doer *user_model.User) (int, int) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "/-/asc/agents/count")
	ctx.Doer = doer
	ctx.IsSigned = doer != nil
	AscAgentsCount(ctx)
	if resp.Code != http.StatusOK {
		return resp.Code, -1
	}
	assert.Equal(t, "private, no-store", resp.Header().Get("Cache-Control"))
	var body struct {
		Count int `json:"count"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	return resp.Code, body.Count
}

// The pill of the header counts the person's active runs with one CountRuns call over their asc repositories.
func TestAscAgentsCountAsksOnceForThePersonsActiveRuns(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	setAscBackend(t, 1, "asc")
	f := &fakeCountAsc{answer: func(*ascv1.CountAgentRunsRequest) (*ascv1.CountAgentRunsResponse, error) {
		return &ascv1.CountAgentRunsResponse{Total: 4}, nil
	}}
	useFakeAgents(t, f)

	t.Run("Agent features off: nothing is asked", func(t *testing.T) {
		require.NoError(t, ascagentui.SetFeatures(context.Background(), user2, false))
		code, n := getAscAgentsCount(t, user2)
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, 0, n)
		assert.Empty(t, f.calls())
	})

	require.NoError(t, ascagentui.SetFeatures(context.Background(), user2, true))

	t.Run("One call for the person's own active runs", func(t *testing.T) {
		code, n := getAscAgentsCount(t, user2)
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, 4, n)
		calls := f.calls()
		require.Len(t, calls, 1)
		req := calls[0]
		assert.Equal(t, "user2", req.GetCreatedBy(), "the signed-in name, never a value of the request")
		want, _ := ascagentui.RunStateFilter("active")
		assert.Equal(t, want, req.GetStates())
		require.NotEmpty(t, req.GetRepos())
		assert.LessOrEqual(t, len(req.GetRepos()), 50)
		seen := map[string]bool{}
		for _, r := range req.GetRepos() {
			assert.False(t, seen[r.GetName()], "each repository once")
			seen[r.GetName()] = true
		}
	})

	t.Run("A large count is bounded", func(t *testing.T) {
		f.mu.Lock()
		f.answer = func(*ascv1.CountAgentRunsRequest) (*ascv1.CountAgentRunsResponse, error) {
			return &ascv1.CountAgentRunsResponse{Total: 1 << 40}, nil
		}
		f.mu.Unlock()
		_, n := getAscAgentsCount(t, user2)
		assert.Equal(t, ascAgentsCountMax, n)
	})

	t.Run("asc cannot be asked: zero, not an error", func(t *testing.T) {
		f.mu.Lock()
		f.answer = func(*ascv1.CountAgentRunsRequest) (*ascv1.CountAgentRunsResponse, error) {
			return nil, status.Error(codes.Unavailable, "down")
		}
		f.mu.Unlock()
		code, n := getAscAgentsCount(t, user2)
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, 0, n)
	})

	t.Run("A visitor, or asc off", func(t *testing.T) {
		code, _ := getAscAgentsCount(t, nil)
		assert.Equal(t, http.StatusNotFound, code)
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		code, _ = getAscAgentsCount(t, user2)
		assert.Equal(t, http.StatusNotFound, code)
	})
}
