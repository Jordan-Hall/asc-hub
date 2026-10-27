// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// A fake asc serve for the agents and workflows pages: the five services they call, on one listener. A test sets
// the handler of each method it expects; a method without a handler answers Unimplemented. Every call is recorded
// with the credential and the op id it carried.

type fakeCall struct {
	method string
	req    proto.Message
	auth   string
	opID   string
	md     metadata.MD
}

type fakeHub struct {
	mu       sync.Mutex
	calls    []fakeCall
	handlers map[string]func(proto.Message) (proto.Message, error)
	// watch answers Agents.WatchRun: the events to send, and whether to keep the stream open afterwards.
	watch func(*ascv1.WatchAgentRunRequest) (events []*ascv1.AgentRunEvent, hold bool)
}

func (h *fakeHub) on(method string, fn func(proto.Message) (proto.Message, error)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.handlers == nil {
		h.handlers = map[string]func(proto.Message) (proto.Message, error){}
	}
	h.handlers[method] = fn
}

// ok sets a handler that always answers resp.
func (h *fakeHub) ok(method string, resp proto.Message) {
	h.on(method, func(proto.Message) (proto.Message, error) { return resp, nil })
}

// fail sets a handler that always fails with err.
func (h *fakeHub) fail(method string, err error) {
	h.on(method, func(proto.Message) (proto.Message, error) { return nil, err })
}

// record notes a call and returns the handler of the method, if any.
func (h *fakeHub) record(ctx gocontext.Context, method string, req proto.Message) func(proto.Message) (proto.Message, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	rec := fakeCall{method: method, req: proto.Clone(req), md: md}
	if v := md.Get("authorization"); len(v) > 0 {
		rec.auth = v[0]
	}
	if v := md.Get("x-asc-op-id"); len(v) > 0 {
		rec.opID = v[0]
	}
	h.mu.Lock()
	h.calls = append(h.calls, rec)
	fn := h.handlers[method]
	h.mu.Unlock()
	return fn
}

func (h *fakeHub) call(ctx gocontext.Context, method string, req proto.Message) (proto.Message, error) {
	fn := h.record(ctx, method, req)
	if fn == nil {
		return nil, status.Error(codes.Unimplemented, method)
	}
	return fn(req)
}

func (h *fakeHub) callsOf(method string) []fakeCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []fakeCall
	for _, c := range h.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

func (h *fakeHub) total() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.calls)
}

func typed[T proto.Message](h *fakeHub, ctx gocontext.Context, method string, req proto.Message) (T, error) {
	var zero T
	m, err := h.call(ctx, method, req)
	if err != nil {
		return zero, err
	}
	out, ok := m.(T)
	if !ok {
		return zero, status.Error(codes.Internal, "fake: wrong answer type for "+method)
	}
	return out, nil
}

type fakeAgentsSvc struct {
	ascv1.UnimplementedAgentsServer
	h *fakeHub
}

func (s fakeAgentsSvc) CreateRun(ctx gocontext.Context, r *ascv1.CreateAgentRunRequest) (*ascv1.CreateAgentRunResponse, error) {
	return typed[*ascv1.CreateAgentRunResponse](s.h, ctx, "Agents.CreateRun", r)
}

func (s fakeAgentsSvc) ListRuns(ctx gocontext.Context, r *ascv1.ListAgentRunsRequest) (*ascv1.ListAgentRunsResponse, error) {
	return typed[*ascv1.ListAgentRunsResponse](s.h, ctx, "Agents.ListRuns", r)
}

func (s fakeAgentsSvc) GetRun(ctx gocontext.Context, r *ascv1.GetAgentRunRequest) (*ascv1.GetAgentRunResponse, error) {
	return typed[*ascv1.GetAgentRunResponse](s.h, ctx, "Agents.GetRun", r)
}

func (s fakeAgentsSvc) CancelRun(ctx gocontext.Context, r *ascv1.CancelAgentRunRequest) (*ascv1.CancelAgentRunResponse, error) {
	return typed[*ascv1.CancelAgentRunResponse](s.h, ctx, "Agents.CancelRun", r)
}

func (s fakeAgentsSvc) SendInput(ctx gocontext.Context, r *ascv1.SendAgentInputRequest) (*ascv1.SendAgentInputResponse, error) {
	return typed[*ascv1.SendAgentInputResponse](s.h, ctx, "Agents.SendInput", r)
}

func (s fakeAgentsSvc) WatchRun(r *ascv1.WatchAgentRunRequest, stream grpc.ServerStreamingServer[ascv1.AgentRunEvent]) error {
	s.h.record(stream.Context(), "Agents.WatchRun", r)
	if s.h.watch == nil {
		return nil
	}
	events, hold := s.h.watch(r)
	for _, ev := range events {
		if ev.GetSeq() > r.GetAfterSeq() || ev.GetSeq() == 0 {
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
	if hold {
		<-stream.Context().Done()
		return stream.Context().Err()
	}
	return nil
}

type fakeTokensSvc struct {
	ascv1.UnimplementedAgentTokensServer
	h *fakeHub
}

func (s fakeTokensSvc) CreateToken(ctx gocontext.Context, r *ascv1.CreateAgentTokenRequest) (*ascv1.CreateAgentTokenResponse, error) {
	return typed[*ascv1.CreateAgentTokenResponse](s.h, ctx, "AgentTokens.CreateToken", r)
}

func (s fakeTokensSvc) ListTokens(ctx gocontext.Context, r *ascv1.ListAgentTokensRequest) (*ascv1.ListAgentTokensResponse, error) {
	return typed[*ascv1.ListAgentTokensResponse](s.h, ctx, "AgentTokens.ListTokens", r)
}

func (s fakeTokensSvc) RevokeToken(ctx gocontext.Context, r *ascv1.RevokeAgentTokenRequest) (*ascv1.RevokeAgentTokenResponse, error) {
	return typed[*ascv1.RevokeAgentTokenResponse](s.h, ctx, "AgentTokens.RevokeToken", r)
}

func (s fakeTokensSvc) SetBudget(ctx gocontext.Context, r *ascv1.SetAgentTokenBudgetRequest) (*ascv1.SetAgentTokenBudgetResponse, error) {
	return typed[*ascv1.SetAgentTokenBudgetResponse](s.h, ctx, "AgentTokens.SetBudget", r)
}

type fakeAiSvc struct {
	ascv1.UnimplementedAiProvidersServer
	h *fakeHub
}

func (s fakeAiSvc) ListKeys(ctx gocontext.Context, r *ascv1.ListAiKeysRequest) (*ascv1.ListAiKeysResponse, error) {
	return typed[*ascv1.ListAiKeysResponse](s.h, ctx, "AiProviders.ListKeys", r)
}

func (s fakeAiSvc) ListModels(ctx gocontext.Context, r *ascv1.ListAiModelsRequest) (*ascv1.ListAiModelsResponse, error) {
	return typed[*ascv1.ListAiModelsResponse](s.h, ctx, "AiProviders.ListModels", r)
}

type fakePolicySvc struct {
	ascv1.UnimplementedPolicyServer
	h *fakeHub
}

func (s fakePolicySvc) GetSource(ctx gocontext.Context, r *ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
	return typed[*ascv1.GetPolicySourceResponse](s.h, ctx, "Policy.GetSource", r)
}

type fakeWorkflowsSvc struct {
	ascv1.UnimplementedWorkflowsServer
	h *fakeHub
}

func (s fakeWorkflowsSvc) ListDefinitions(ctx gocontext.Context, r *ascv1.ListWorkflowDefinitionsRequest) (*ascv1.ListWorkflowDefinitionsResponse, error) {
	return typed[*ascv1.ListWorkflowDefinitionsResponse](s.h, ctx, "Workflows.ListDefinitions", r)
}

func (s fakeWorkflowsSvc) GetDefinition(ctx gocontext.Context, r *ascv1.GetWorkflowDefinitionRequest) (*ascv1.GetWorkflowDefinitionResponse, error) {
	return typed[*ascv1.GetWorkflowDefinitionResponse](s.h, ctx, "Workflows.GetDefinition", r)
}

func (s fakeWorkflowsSvc) SetEnabled(ctx gocontext.Context, r *ascv1.SetWorkflowEnabledRequest) (*ascv1.SetWorkflowEnabledResponse, error) {
	return typed[*ascv1.SetWorkflowEnabledResponse](s.h, ctx, "Workflows.SetEnabled", r)
}

func (s fakeWorkflowsSvc) Trigger(ctx gocontext.Context, r *ascv1.TriggerWorkflowRequest) (*ascv1.TriggerWorkflowResponse, error) {
	return typed[*ascv1.TriggerWorkflowResponse](s.h, ctx, "Workflows.Trigger", r)
}

func (s fakeWorkflowsSvc) ListRuns(ctx gocontext.Context, r *ascv1.ListWorkflowRunsRequest) (*ascv1.ListWorkflowRunsResponse, error) {
	return typed[*ascv1.ListWorkflowRunsResponse](s.h, ctx, "Workflows.ListRuns", r)
}

func (s fakeWorkflowsSvc) GetRun(ctx gocontext.Context, r *ascv1.GetWorkflowRunRequest) (*ascv1.GetWorkflowRunResponse, error) {
	return typed[*ascv1.GetWorkflowRunResponse](s.h, ctx, "Workflows.GetRun", r)
}

func (s fakeWorkflowsSvc) CancelRun(ctx gocontext.Context, r *ascv1.CancelWorkflowRunRequest) (*ascv1.CancelWorkflowRunResponse, error) {
	return typed[*ascv1.CancelWorkflowRunResponse](s.h, ctx, "Workflows.CancelRun", r)
}

func (s fakeWorkflowsSvc) DecideApproval(ctx gocontext.Context, r *ascv1.DecideWorkflowApprovalRequest) (*ascv1.DecideWorkflowApprovalResponse, error) {
	return typed[*ascv1.DecideWorkflowApprovalResponse](s.h, ctx, "Workflows.DecideApproval", r)
}

// startFakeHub serves the fake on a loopback port and points the page code at it.
func startFakeHub(t *testing.T) *fakeHub {
	t.Helper()
	h := &fakeHub{}
	srv := grpc.NewServer()
	ascv1.RegisterAgentsServer(srv, fakeAgentsSvc{h: h})
	ascv1.RegisterAgentTokensServer(srv, fakeTokensSvc{h: h})
	ascv1.RegisterAiProvidersServer(srv, fakeAiSvc{h: h})
	ascv1.RegisterPolicyServer(srv, fakePolicySvc{h: h})
	ascv1.RegisterWorkflowsServer(srv, fakeWorkflowsSvc{h: h})
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	useAsc(t, l.Addr().String())
	// Short waits, so a poll of a finished or idle run does not slow the tests down.
	t.Cleanup(test.MockVariableValue(&ascAgentsPollWait, 300*time.Millisecond))
	t.Cleanup(test.MockVariableValue(&ascAgentsDrainWait, 30*time.Millisecond))
	t.Cleanup(test.MockVariableValue(&ascAgentsBacklogWait, 400*time.Millisecond))
	t.Cleanup(test.MockVariableValue(&ascAgentsBacklogDrain, 60*time.Millisecond))
	return h
}

// agentsPage runs a handler for repository 1 (user2/repo1) as the given user (0 is nobody) with the form values.
type agentsPage struct {
	status int
	body   string
	loc    string
	ctx    *context.Context
	resp   *httptest.ResponseRecorder
}

type agentsReq struct {
	method   string
	path     string
	user     int64
	features bool
	form     url.Values
	params   map[string]string
	// notAsc leaves the repository on git storage.
}

func runAgents(t *testing.T, r agentsReq, handler func(*context.Context)) agentsPage {
	t.Helper()
	if r.method == "" {
		r.method = "GET"
	}
	ctx, resp := contexttest.MockContext(t, r.method+" "+r.path)
	ctx.Render = templates.HTMLRenderer()
	if r.user != 0 {
		contexttest.LoadUser(t, ctx, r.user)
		ctx.IsSigned = true
		u := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: r.user})
		on := "false"
		if r.features {
			on = "true"
		}
		require.NoError(t, user_model.SetUserSetting(db.DefaultContext, u.ID, "asc.agent_features", on))
	}
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Data["RepoLink"] = ctx.Repo.RepoLink
	if r.form != nil {
		ctx.Req.Form = r.form
	}
	for k, v := range r.params {
		ctx.SetParams(k, v)
	}
	handler(ctx)
	return agentsPage{status: resp.Code, body: resp.Body.String(), loc: resp.Header().Get("Location"), ctx: ctx, resp: resp}
}

// Users of the fixtures: user2 owns repo1 (admin), user4 only reads it (a public repository).
const (
	agentsOwner  = int64(2)
	agentsReader = int64(4)
)

var _ = http.StatusOK
