// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repoquick

import (
	"testing"

	git_model "forgejo.org/models/git"
	api "forgejo.org/modules/structs"

	"github.com/stretchr/testify/assert"
)

// A row sums up the latest status of each check of the head of its default branch.
func TestTheRowCheckSumsUpTheChecks(t *testing.T) {
	of := func(states ...api.CommitStatusState) RowCheck {
		list := make([]*git_model.CommitStatus, 0, len(states))
		for _, s := range states {
			list = append(list, &git_model.CommitStatus{State: s})
		}
		return rowCheckOf(list)
	}
	assert.Equal(t, RowCheck{State: CheckNone}, of())
	assert.Equal(t, RowCheck{State: CheckPassing}, of(api.CommitStatusSuccess, api.CommitStatusSuccess))
	assert.Equal(t, RowCheck{State: CheckPassing}, of(api.CommitStatusSuccess, api.CommitStatusWarning), "a warning does not fail")
	assert.Equal(t, RowCheck{State: CheckRunning}, of(api.CommitStatusSuccess, api.CommitStatusPending))
	assert.Equal(t, RowCheck{State: CheckFailing, Failing: 2}, of(api.CommitStatusFailure, api.CommitStatusPending, api.CommitStatusError, api.CommitStatusSuccess), "a failure counts over a running check")
}
