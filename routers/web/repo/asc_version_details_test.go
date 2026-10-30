// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
)

func TestAscVersionChecksOf(t *testing.T) {
	job := func(name string, s ascv1.CiStatus) *ascv1.CiJob { return &ascv1.CiJob{Name: name, Status: s} }
	runs := []*ascv1.CiRun{ // newest first
		{Version: 2, Jobs: []*ascv1.CiJob{job("unit", ascv1.CiStatus_CI_STATUS_SUCCESS), job("lint", ascv1.CiStatus_CI_STATUS_RUNNING)}},
		{Version: 2, Jobs: []*ascv1.CiJob{job("unit", ascv1.CiStatus_CI_STATUS_FAILURE)}}, // older run of a job already shown
		{Version: 1, Jobs: []*ascv1.CiJob{job("unit", ascv1.CiStatus_CI_STATUS_FAILURE), job("docs", ascv1.CiStatus_CI_STATUS_SKIPPED)}},
		{Version: 0, Jobs: []*ascv1.CiJob{job("unit", ascv1.CiStatus_CI_STATUS_SUCCESS)}}, // no version: not shown
		{Version: 3, Name: "security", Status: ascv1.CiStatus_CI_STATUS_SUCCESS},          // a run without jobs is one check
	}
	got := ascVersionChecksOf(runs)
	assert.Equal(t, []AscVersionCheck{{"unit", "ok"}, {"lint", "warn"}}, got[2])
	assert.Equal(t, []AscVersionCheck{{"unit", "err"}}, got[1], "a skipped job says nothing")
	assert.Equal(t, []AscVersionCheck{{"security", "ok"}}, got[3])
	assert.Len(t, got, 3)
}
