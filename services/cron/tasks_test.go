// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package cron

import (
	"context"
	"sort"
	"strconv"
	"testing"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/taskout"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddTaskToScheduler(t *testing.T) {
	assert.Empty(t, scheduler.Jobs())
	defer scheduler.Clear()

	// no seconds
	err := addTaskToScheduler(&Task{
		Name: "task 1",
		config: &BaseConfig{
			Schedule: "5 4 * * *",
		},
	})
	require.NoError(t, err)
	jobs := scheduler.Jobs()
	assert.Len(t, jobs, 1)
	assert.Equal(t, "task 1", jobs[0].Tags()[0])
	assert.Equal(t, "5 4 * * *", jobs[0].Tags()[1])

	// with seconds
	err = addTaskToScheduler(&Task{
		Name: "task 2",
		config: &BaseConfig{
			Schedule: "30 5 4 * * *",
		},
	})
	require.NoError(t, err)
	jobs = scheduler.Jobs() // the item order is not guaranteed, so we need to sort it before "assert"
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].Tags()[0] < jobs[j].Tags()[0]
	})
	assert.Len(t, jobs, 2)
	assert.Equal(t, "task 2", jobs[1].Tags()[0])
	assert.Equal(t, "30 5 4 * * *", jobs[1].Tags()[1])
}

func TestScheduleHasSeconds(t *testing.T) {
	tests := []struct {
		schedule  string
		hasSecond bool
	}{
		{"* * * * * *", true},
		{"* * * * *", false},
		{"5 4 * * *", false},
		{"5 4 * * *", false},
		{"5,8 4 * * *", false},
		{"*   *   *  * * *", true},
		{"5,8 4   *  *   *", false},
	}

	for i, test := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			assert.Equal(t, test.hasSecond, scheduleHasSeconds(test.schedule))
		})
	}
}

func TestRunWithUserKeepsTheOutputOfTheRun(t *testing.T) {
	release := make(chan struct{})
	said := make(chan struct{})
	defer UseTestTask("test_output", func(ctx context.Context, _ *user_model.User, _ Config) error {
		taskout.Say(ctx, "first slice")
		close(said)
		<-release
		taskout.Say(ctx, "second slice")
		return nil
	})()
	task := LookupTask("test_output")
	require.NotNil(t, task)
	assert.Equal(t, int64(0), task.Output(0).Run, "no run yet")

	done := make(chan struct{})
	go func() {
		task.RunWithUser(&user_model.User{ID: 1, Name: "admin"}, nil)
		close(done)
	}()
	<-said
	out := task.Output(0)
	assert.True(t, out.Running)
	assert.Equal(t, "admin", out.Doer)
	assert.Equal(t, []string{"first slice"}, out.Lines)
	close(release)
	<-done
	out = task.Output(1)
	assert.False(t, out.Running)
	assert.False(t, out.Failed)
	assert.Equal(t, int64(1), out.Run)
	assert.Equal(t, []string{"second slice"}, out.Lines, "the lines after the first")
	assert.Equal(t, "finished", task.Status)

	// The schedule runs a task as (Cron): the run has no doer name.
	release = make(chan struct{})
	said = make(chan struct{})
	close(release)
	task.Run()
	<-said
	assert.Equal(t, int64(2), task.Output(0).Run)
	assert.Empty(t, task.Output(0).Doer)
}
