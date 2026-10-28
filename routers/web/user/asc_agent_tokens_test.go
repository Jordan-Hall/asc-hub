// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeAgentsAsc struct {
	ascv1.UnimplementedAgentsServer
	mu   sync.Mutex
	reqs []*ascv1.ListAgentRunsRequest
	auth []string
	by   func(*ascv1.ListAgentRunsRequest) (*ascv1.ListAgentRunsResponse, error)
}

func (f *fakeAgentsAsc) ListRuns(_ context.Context, req *ascv1.ListAgentRunsRequest) (*ascv1.ListAgentRunsResponse, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	by := f.by
	f.mu.Unlock()
	if by == nil {
		return nil, status.Error(codes.Unimplemented, "no")
	}
	return by(req)
}

func useFakeAgents(t *testing.T, f *fakeAgentsAsc) {
	t.Helper()
	srv := grpc.NewServer()
	ascv1.RegisterAgentsServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	ascDraftsConnMu.Lock()
	oldNew, oldConn := ascDraftsNewConn, ascDraftsConnVal
	ascDraftsConnVal = nil
	ascDraftsNewConn = func() (grpc.ClientConnInterface, error) {
		return ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "test-internal-token", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	}
	ascDraftsConnMu.Unlock()
	t.Cleanup(func() {
		ascDraftsConnMu.Lock()
		if c, ok := ascDraftsConnVal.(*ascclient.Client); ok {
			_ = c.Close()
		}
		ascDraftsNewConn, ascDraftsConnVal = oldNew, oldConn
		ascDraftsConnMu.Unlock()
	})
}

func getAscAgentsHome(t *testing.T, doer *user_model.User) (int, string) {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, "/-/asc/agents")
	ctx.Render = templates.HTMLRenderer()
	ctx.Doer = doer
	ctx.IsSigned = doer != nil
	AscAgentsHome(ctx)
	return resp.Code, resp.Body.String()
}

func TestAscAgentsHomeSwitchedOff(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	setAscBackend(t, 1, "asc")
	f := &fakeAgentsAsc{}
	useFakeAgents(t, f)

	code, body := getAscAgentsHome(t, user2)
	assert.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "Right now: off.")
	assert.Contains(t, body, "Turn agent features on")
	assert.NotContains(t, body, "Your recent runs")
	assert.NotContains(t, body, "Your repositories")
	assert.Empty(t, f.reqs, "with the switch off, asc is not asked about agents at all")

	t.Run("anonymous and disabled", func(t *testing.T) {
		code, _ := getAscAgentsHome(t, nil)
		assert.Equal(t, http.StatusNotFound, code)
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		code, _ = getAscAgentsHome(t, user2)
		assert.Equal(t, http.StatusNotFound, code)
	})
}

func TestAscAgentsHomeRuns(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	setAscBackend(t, 1, "asc")
	require.NoError(t, ascagentui.SetFeatures(context.Background(), user2, true))

	f := &fakeAgentsAsc{by: func(req *ascv1.ListAgentRunsRequest) (*ascv1.ListAgentRunsResponse, error) {
		return &ascv1.ListAgentRunsResponse{Runs: []*ascv1.AgentRun{
			{Id: 4, Repo: req.GetRepo().GetName(), Title: "older <b>run</b>", Agent: "coder", State: ascv1.AgentRunState_AGENT_RUN_STATE_SUCCEEDED, Created: 100},
			{Id: 5, Repo: req.GetRepo().GetName(), Title: "newer run", Agent: "coder", State: ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING, Created: 200},
			{Id: 6, Repo: "user9/elsewhere", Title: "STRANGER-RUN", State: ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING, Created: 300},
			{Id: 0, Repo: req.GetRepo().GetName(), Title: "NO-ID"},
		}}, nil
	}}
	useFakeAgents(t, f)

	code, body := getAscAgentsHome(t, user2)
	require.Equal(t, http.StatusOK, code, body)
	assert.Contains(t, body, "Right now: on.")
	assert.Contains(t, body, "Turn agent features off")

	require.NotEmpty(t, f.reqs)
	for _, r := range f.reqs {
		assert.Equal(t, "user2", r.GetCreatedBy(), "only the person's own runs, by the session")
		assert.EqualValues(t, ascAgentsHomeRunsPer, r.GetLimit())
		assert.NotEmpty(t, r.GetRepo().GetName())
	}
	assert.Contains(t, body, `href="/user2/repo1/asc/agents/runs/5"`)
	assert.Contains(t, body, "older &lt;b&gt;run&lt;/b&gt;")
	assert.NotContains(t, body, "<b>run</b>")
	assert.Less(t, strings.Index(body, "newer run"), strings.Index(body, "older &lt;b&gt;"), "newest first")
	assert.NotContains(t, body, "STRANGER-RUN", "a run of another repository is not shown")
	assert.NotContains(t, body, "NO-ID")
	// The repository list links to the pages of the repository.
	assert.Contains(t, body, `href="/user2/repo1/asc/agents"`)
	assert.Contains(t, body, `href="/user2/repo1/asc/agents/tokens"`, "the owner administers it")
	assert.Contains(t, body, `href="/user2/repo1/asc/workflows"`)

	t.Run("a repository with agents off is not a fault", func(t *testing.T) {
		st, err := status.New(codes.FailedPrecondition, "off").WithDetails(&ascv1.ErrorDetail{Code: "AGENTS_DISABLED"})
		require.NoError(t, err)
		f.mu.Lock()
		f.by = func(*ascv1.ListAgentRunsRequest) (*ascv1.ListAgentRunsResponse, error) { return nil, st.Err() }
		f.mu.Unlock()
		code, body := getAscAgentsHome(t, user2)
		assert.Equal(t, http.StatusOK, code)
		assert.NotContains(t, body, "could not be read")
		assert.Contains(t, body, "You have not started a run")
	})

	t.Run("a broken asc is told, without its words", func(t *testing.T) {
		f.mu.Lock()
		f.by = func(*ascv1.ListAgentRunsRequest) (*ascv1.ListAgentRunsResponse, error) {
			return nil, status.Error(codes.Internal, "secret internal detail sk-planted")
		}
		f.mu.Unlock()
		code, body := getAscAgentsHome(t, user2)
		assert.Equal(t, http.StatusOK, code)
		assert.Contains(t, body, "Some repositories could not be read")
		assert.NotContains(t, body, "sk-planted")
	})

	t.Run("a repository that is not in asc is not asked", func(t *testing.T) {
		setAscBackend(t, 1, "git")
		defer setAscBackend(t, 1, "asc")
		f.mu.Lock()
		f.reqs = nil
		f.mu.Unlock()
		_, body := getAscAgentsHome(t, user2)
		assert.Empty(t, f.reqs)
		assert.NotContains(t, body, `href="/user2/repo1/asc/agents"`)
	})
}

func TestAscAgentsHomeCollaborator(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user4 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	setAscBackend(t, 4, "asc") // repository 4 belongs to user5; user4 collaborates on it with write access
	require.NoError(t, ascagentui.SetFeatures(context.Background(), user4, true))
	f := &fakeAgentsAsc{by: func(*ascv1.ListAgentRunsRequest) (*ascv1.ListAgentRunsResponse, error) {
		return &ascv1.ListAgentRunsResponse{}, nil
	}}
	useFakeAgents(t, f)

	code, body := getAscAgentsHome(t, user4)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, f.reqs)
	assert.Equal(t, "user4", f.reqs[0].GetCreatedBy())
	assert.Contains(t, body, `/asc/agents"`)
	assert.NotContains(t, body, "/asc/agents/tokens", "a collaborator with write access does not administer the repository")
}

func TestAscAgentsFeaturesPost(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	user4 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))

	post := func(doer *user_model.User, enabled string) (int, string) {
		ctx, resp := contexttest.MockContext(t, "POST /-/asc/agents/features")
		ctx.Doer = doer
		ctx.IsSigned = doer != nil
		ctx.Req.Form = url.Values{"enabled": {enabled}}
		AscAgentsFeaturesPost(ctx)
		return resp.Code, resp.Header().Get("Location")
	}

	assert.False(t, ascagentui.FeaturesEnabled(context.Background(), user2), "off by default")
	code, loc := post(user2, "1")
	assert.Equal(t, http.StatusSeeOther, code)
	assert.Equal(t, "/-/asc/agents", loc)
	assert.True(t, ascagentui.FeaturesEnabled(context.Background(), user2))
	assert.False(t, ascagentui.FeaturesEnabled(context.Background(), user4), "a switch is one person's")

	code, _ = post(user2, "0")
	assert.Equal(t, http.StatusSeeOther, code)
	assert.False(t, ascagentui.FeaturesEnabled(context.Background(), user2))

	for _, bad := range []string{"", "true", "yes", "2"} {
		code, _ := post(user2, bad)
		assert.Equal(t, http.StatusBadRequest, code, bad)
	}
	assert.False(t, ascagentui.FeaturesEnabled(context.Background(), user2))

	code, _ = post(nil, "1")
	assert.Equal(t, http.StatusNotFound, code, "signed out")
}
