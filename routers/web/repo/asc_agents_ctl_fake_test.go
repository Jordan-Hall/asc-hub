// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// The control and board calls of the Agents service on the fake asc serve (asc_agents_fake_test.go).

func (s fakeAgentsSvc) ListRequests(ctx gocontext.Context, r *ascv1.ListAgentRequestsRequest) (*ascv1.ListAgentRequestsResponse, error) {
	return typed[*ascv1.ListAgentRequestsResponse](s.h, ctx, "Agents.ListRequests", r)
}

func (s fakeAgentsSvc) DecideRequest(ctx gocontext.Context, r *ascv1.DecideAgentRequestRequest) (*ascv1.DecideAgentRequestResponse, error) {
	return typed[*ascv1.DecideAgentRequestResponse](s.h, ctx, "Agents.DecideRequest", r)
}

func (s fakeAgentsSvc) PauseRun(ctx gocontext.Context, r *ascv1.PauseAgentRunRequest) (*ascv1.PauseAgentRunResponse, error) {
	return typed[*ascv1.PauseAgentRunResponse](s.h, ctx, "Agents.PauseRun", r)
}

func (s fakeAgentsSvc) ListCheckpoints(ctx gocontext.Context, r *ascv1.ListAgentCheckpointsRequest) (*ascv1.ListAgentCheckpointsResponse, error) {
	return typed[*ascv1.ListAgentCheckpointsResponse](s.h, ctx, "Agents.ListCheckpoints", r)
}

func (s fakeAgentsSvc) RollbackRun(ctx gocontext.Context, r *ascv1.RollbackAgentRunRequest) (*ascv1.RollbackAgentRunResponse, error) {
	return typed[*ascv1.RollbackAgentRunResponse](s.h, ctx, "Agents.RollbackRun", r)
}

func (s fakeAgentsSvc) SetRunLimits(ctx gocontext.Context, r *ascv1.SetAgentRunLimitsRequest) (*ascv1.SetAgentRunLimitsResponse, error) {
	return typed[*ascv1.SetAgentRunLimitsResponse](s.h, ctx, "Agents.SetRunLimits", r)
}

func (s fakeAgentsSvc) ContinueRun(ctx gocontext.Context, r *ascv1.ContinueAgentRunRequest) (*ascv1.ContinueAgentRunResponse, error) {
	return typed[*ascv1.ContinueAgentRunResponse](s.h, ctx, "Agents.ContinueRun", r)
}

func (s fakeAgentsSvc) GetEstimate(ctx gocontext.Context, r *ascv1.GetAgentEstimateRequest) (*ascv1.GetAgentEstimateResponse, error) {
	return typed[*ascv1.GetAgentEstimateResponse](s.h, ctx, "Agents.GetEstimate", r)
}

func (s fakeAgentsSvc) RecentTasks(ctx gocontext.Context, r *ascv1.RecentAgentTasksRequest) (*ascv1.RecentAgentTasksResponse, error) {
	return typed[*ascv1.RecentAgentTasksResponse](s.h, ctx, "Agents.RecentTasks", r)
}

func (s fakeAgentsSvc) GetRunLog(ctx gocontext.Context, r *ascv1.GetAgentRunLogRequest) (*ascv1.GetAgentRunLogResponse, error) {
	return typed[*ascv1.GetAgentRunLogResponse](s.h, ctx, "Agents.GetRunLog", r)
}

func (s fakeAgentsSvc) ListAgents(ctx gocontext.Context, r *ascv1.ListRepoAgentsRequest) (*ascv1.ListRepoAgentsResponse, error) {
	return typed[*ascv1.ListRepoAgentsResponse](s.h, ctx, "Agents.ListAgents", r)
}

func (s fakeAgentsSvc) GetSpend(ctx gocontext.Context, r *ascv1.GetAgentSpendRequest) (*ascv1.GetAgentSpendResponse, error) {
	return typed[*ascv1.GetAgentSpendResponse](s.h, ctx, "Agents.GetSpend", r)
}

func (s fakeAgentsSvc) SetMonthlyBudget(ctx gocontext.Context, r *ascv1.SetMonthlyBudgetRequest) (*ascv1.SetMonthlyBudgetResponse, error) {
	return typed[*ascv1.SetMonthlyBudgetResponse](s.h, ctx, "Agents.SetMonthlyBudget", r)
}
