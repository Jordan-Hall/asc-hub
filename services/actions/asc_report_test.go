// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"strings"
	"testing"

	actions_model "forgejo.org/models/actions"
	repo_model "forgejo.org/models/repo"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	webhook_module "forgejo.org/modules/webhook"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscReportOfRun(t *testing.T) {
	run := &actions_model.ActionRun{
		ID: 42, Title: "ci\x07 on main", WorkflowID: "ci.yml", Status: actions_model.StatusRunning, Started: 100,
		Repo: &repo_model.Repository{OwnerName: "acme", Name: "app"},
	}
	jobs := []*actions_model.ActionRunJob{
		{Name: "build", Status: actions_model.StatusSuccess, Started: 100, Stopped: 160},
		{Name: "build", Status: actions_model.StatusWaiting},
		{Name: "", Status: actions_model.StatusBlocked},
		{Name: "build (2)"},
	}
	req := ascReportOf(run, "acme.app", []byte{1, 2}, jobs)
	assert.Equal(t, "acme.app", req.GetRepo().GetName())
	assert.Equal(t, ascReportSource, req.Source)
	assert.Equal(t, "42", req.ExternalId)
	assert.Equal(t, "ci on main", req.Name)
	assert.Equal(t, []byte{1, 2}, req.Change)
	assert.Equal(t, ascv1.CiStatus_CI_STATUS_RUNNING, req.Status)
	assert.EqualValues(t, 100, req.Started)

	require.Len(t, req.Jobs, 4)
	assert.Equal(t, "build (2) (2)", req.Jobs[3].Name)
	assert.Equal(t, "build", req.Jobs[0].Name)
	assert.Equal(t, ascv1.CiStatus_CI_STATUS_SUCCESS, req.Jobs[0].Status)
	assert.EqualValues(t, 160, req.Jobs[0].Finished)
	assert.Equal(t, "build (2)", req.Jobs[1].Name)
	assert.Equal(t, ascv1.CiStatus_CI_STATUS_QUEUED, req.Jobs[1].Status)
	assert.Equal(t, "job", req.Jobs[2].Name)
	assert.Equal(t, ascv1.CiStatus_CI_STATUS_QUEUED, req.Jobs[2].Status)
}

func TestAscReportBounds(t *testing.T) {
	run := &actions_model.ActionRun{ID: 1, WorkflowID: "ci.yml", Repo: &repo_model.Repository{}}
	jobs := make([]*actions_model.ActionRunJob, ascReportMaxJobs+5)
	for i := range jobs {
		jobs[i] = &actions_model.ActionRunJob{Name: strings.Repeat("j", 300)}
	}
	req := ascReportOf(run, "r", nil, jobs)
	assert.Equal(t, "ci.yml", req.Name)
	assert.Len(t, req.Jobs, ascReportMaxJobs)
	seen := map[string]bool{}
	for _, j := range req.Jobs {
		assert.LessOrEqual(t, len(j.Name), ascReportMaxText)
		assert.False(t, seen[j.Name], "duplicate %q", j.Name)
		seen[j.Name] = true
	}

	assert.Equal(t, "https://hub.example.org/a/b/actions/runs/1", httpsLink("https://hub.example.org/a/b/actions/runs/1"))
	assert.Empty(t, httpsLink("http://localhost:3000/a/b/actions/runs/1"))
	assert.Empty(t, httpsLink("https://a b"))
}

func TestAscReportOnlyDefaultBranchPushes(t *testing.T) {
	repo := &repo_model.Repository{DefaultBranch: "main"}
	for ref, want := range map[string]bool{"refs/heads/main": true, "refs/heads/topic": false, "refs/tags/v1": false} {
		run := &actions_model.ActionRun{Repo: repo, Event: webhook_module.HookEventPush, Ref: ref}
		change, ok, err := ascChangeOfRun(t.Context(), run)
		require.NoError(t, err)
		assert.Equal(t, want, ok, ref)
		assert.Nil(t, change)
	}
	run := &actions_model.ActionRun{Repo: repo, Event: webhook_module.HookEventSchedule, Ref: "refs/heads/main"}
	_, ok, err := ascChangeOfRun(t.Context(), run)
	require.NoError(t, err)
	assert.False(t, ok)
}
