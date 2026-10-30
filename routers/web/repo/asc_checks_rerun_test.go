// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	actions_model "forgejo.org/models/actions"

	"github.com/stretchr/testify/assert"
)

func TestAscLatestFailedRuns(t *testing.T) {
	run := func(id int64, workflow, ref string, status actions_model.Status) *actions_model.ActionRun {
		return &actions_model.ActionRun{ID: id, WorkflowID: workflow, Ref: ref, Status: status}
	}
	// newest first
	runs := []*actions_model.ActionRun{
		run(9, "ci.yml", "refs/heads/main", actions_model.StatusSuccess), // a later success answers the older failure
		run(8, "ci.yml", "refs/heads/main", actions_model.StatusFailure),
		run(7, "ci.yml", "refs/heads/topic", actions_model.StatusFailure),
		run(6, "lint.yml", "refs/heads/topic", actions_model.StatusFailure),
		run(5, "ci.yml", "refs/heads/topic", actions_model.StatusFailure), // older run of a workflow and ref already seen
		run(4, "docs.yml", "refs/heads/topic", actions_model.StatusSuccess),
	}
	got := ascLatestFailedRuns(runs, 10)
	ids := []int64{}
	for _, r := range got {
		ids = append(ids, r.ID)
	}
	assert.Equal(t, []int64{7, 6}, ids)
	assert.Len(t, ascLatestFailedRuns(runs, 1), 1, "the limit holds")
}
