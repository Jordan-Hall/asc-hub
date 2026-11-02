// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"context"
	"testing"

	actions_model "forgejo.org/models/actions"
	actions_module "forgejo.org/modules/actions"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const affectedWorkflow = `on: push
jobs:
  web:
    affected: [web, libs/ui]
    runs-on: docker
    steps:
      - run: echo web
  api:
    affected: services/*
    runs-on: docker
    steps:
      - run: echo api
  lint:
    runs-on: docker
    steps:
      - run: echo lint
`

func TestAscAffectedSplit(t *testing.T) {
	out, sels, err := splitAffected([]byte(affectedWorkflow))
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{"web": {"web", "libs/ui"}, "api": {"services/*"}}, sels)
	assert.NotContains(t, string(out), "affected")

	same, sels, err := splitAffected([]byte("on: push\njobs:\n  a:\n    runs-on: x\n"))
	require.NoError(t, err)
	assert.Nil(t, sels)
	assert.Equal(t, "on: push\njobs:\n  a:\n    runs-on: x\n", string(same))

	for _, bad := range []string{"affected: {a: b}", "affected: []", `affected: ["a,b"]`, `affected: "["`} {
		_, _, err := splitAffected([]byte("on: push\njobs:\n  a:\n    " + bad + "\n    runs-on: x\n"))
		assert.Error(t, err, bad)
	}
}

func TestAscAffectedMatches(t *testing.T) {
	set := &affectedSet{names: map[string]struct{}{"web": {}}, roots: []string{"apps/web", "services/billing"}}
	for sel, want := range map[string]bool{
		"web": true, "apps/web": true, "apps": true, "apps/web/src": true,
		"services/*": true, "api": false, "apps/we": false, "libs/ui": false,
	} {
		assert.Equal(t, want, set.matches(sel), sel)
	}
	assert.True(t, (&affectedSet{all: true}).matches("anything"))
}

func TestAscAffectedSkipsUnaffectedJobs(t *testing.T) {
	content, sels, err := splitAffected([]byte(affectedWorkflow))
	require.NoError(t, err)
	jobs, err := actions_module.JobParser(content)
	require.NoError(t, err)

	defer func(f func(context.Context, *actions_model.ActionRun) (*affectedSet, error)) { affectedFn = f }(affectedFn)
	affectedFn = func(context.Context, *actions_model.ActionRun) (*affectedSet, error) {
		return &affectedSet{names: map[string]struct{}{}, roots: []string{"services/billing"}}, nil
	}
	run := &actions_model.ActionRun{}
	skipUnaffected(t.Context(), run, sels, jobs)

	skipped := map[string]string{}
	for _, v := range jobs {
		id, j := v.Job()
		if s, ok := notAffectedSelectors(j); ok {
			skipped[id] = s
		}
	}
	assert.Equal(t, map[string]string{"web": "web, libs/ui"}, skipped)

	calculateWarnings(run, jobs)
	assert.Equal(t, []actions_model.PreExecutionWarning{actions_model.WarningCodeAscNotAffected}, run.PreExecutionWarningCodes)
	assert.Equal(t, [][]any{{"web", "web, libs/ui"}}, run.PreExecutionWarningDetails)
}

func TestAscAffectedFailsOpen(t *testing.T) {
	content, sels, _ := splitAffected([]byte(affectedWorkflow))
	jobs, err := actions_module.JobParser(content)
	require.NoError(t, err)
	defer func(f func(context.Context, *actions_model.ActionRun) (*affectedSet, error)) { affectedFn = f }(affectedFn)
	affectedFn = func(context.Context, *actions_model.ActionRun) (*affectedSet, error) {
		return nil, assert.AnError
	}
	skipUnaffected(t.Context(), &actions_model.ActionRun{}, sels, jobs)
	for _, v := range jobs {
		_, j := v.Job()
		_, ok := notAffectedSelectors(j)
		assert.False(t, ok)
	}
}
