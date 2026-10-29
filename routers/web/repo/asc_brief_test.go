// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"fmt"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func briefLinks() (func(uint64) string, func(string, uint32) string) {
	return func(id uint64) string { return fmt.Sprintf("/o/r/asc/agents/runs/%d", id) },
		func(p string, l uint32) string {
			if l == 0 {
				return "/o/r/src/branch/x/" + p
			}
			return fmt.Sprintf("/o/r/src/branch/x/%s#L%d", p, l)
		}
}

func TestAscBriefOfTheWrittenBrief(t *testing.T) {
	run, file := briefLinks()
	resp := &ascv1.GetBriefResponse{
		Brief: &ascv1.ReviewBrief{What: "Retry with backoff", Why: "Provider outages", How: "cargo test", RiskNote: "Retries may double charge",
			WhereToLook: []*ascv1.WhereToLook{{Path: []byte("src/retry.rs"), Line: 24, Note: "the delay"}, {Path: []byte{0xff, 0xfe}, Note: "bad path"}}},
		Files: 3, Added: 74, Removed: 8,
		Risk:       &ascv1.ChangeRisk{Level: ascv1.RiskLevel_RISK_LEVEL_MEDIUM, Reasons: []string{"touches src/retry.rs"}},
		Provenance: &ascv1.ChangeProvenance{RunId: 214, Agent: "coder-2", Title: "Backoff", ToolCalls: 38, FilesRead: 12, Versions: 4, Started: 1000, Finished: 1000 + 11*60},
		Gate:       &ascv1.ReviewGateData{OpenThreads: 1, TotalThreads: 3, ApprovalsCurrent: 1, ApprovalsCarried: 1},
	}
	b, err := ascBriefOf(resp, run, file)
	require.NoError(t, err)
	assert.True(t, b.Written)
	assert.Equal(t, "Retry with backoff", b.What)
	require.Len(t, b.Where, 1, "a pointer with a bad path is dropped")
	assert.Equal(t, "/o/r/src/branch/x/src/retry.rs#L24", b.Where[0].Link)
	assert.Equal(t, "medium", b.RiskLevel)
	assert.Equal(t, []string{"touches src/retry.rs"}, b.RiskReasons)
	require.NotNil(t, b.Provenance)
	assert.Equal(t, "/o/r/asc/agents/runs/214", b.Provenance.RunLink)
	assert.EqualValues(t, 11, b.Provenance.Minutes)
	assert.EqualValues(t, 1, b.OpenThreads)
}

func TestAscBriefOfWithoutABrief(t *testing.T) {
	run, file := briefLinks()
	b, err := ascBriefOf(&ascv1.GetBriefResponse{Brief: &ascv1.ReviewBrief{}, Files: 1, Added: 2}, run, file)
	require.NoError(t, err)
	assert.False(t, b.Written, "empty fields are no brief")
	assert.Nil(t, b.Provenance, "run 0 is no run")
	assert.Empty(t, b.RiskLevel)
}

func TestAscBriefOfRefusesAnAbsurdAnswer(t *testing.T) {
	run, file := briefLinks()
	_, err := ascBriefOf(nil, run, file)
	assert.Error(t, err)
	_, err = ascBriefOf(&ascv1.GetBriefResponse{Added: 1 << 30}, run, file)
	assert.Error(t, err)
	stats := make([]*ascv1.ChangeFileStat, 501)
	_, err = ascBriefOf(&ascv1.GetBriefResponse{FileStats: stats}, run, file)
	assert.Error(t, err)
}

func TestAscBriefOfApprovals(t *testing.T) {
	run, file := briefLinks()
	b, err := ascBriefOf(&ascv1.GetBriefResponse{Approvals: []*ascv1.ApprovalInfo{
		{Reviewer: "priya", Version: 3, State: ascv1.ApprovalState_APPROVAL_STATE_APPROVED, Applies: true, Carried: true, UnchangedFiles: 4},
		{Reviewer: "diego", Version: 4, State: ascv1.ApprovalState_APPROVAL_STATE_CHANGES_REQUESTED, Applies: true},
		{Reviewer: "hannah", Version: 1, State: ascv1.ApprovalState_APPROVAL_STATE_DISMISSED},
	}}, run, file)
	require.NoError(t, err)
	require.Len(t, b.Approvals, 2, "a dismissed verdict is not shown")
	assert.True(t, b.Approvals[0].Carried)
	assert.EqualValues(t, 4, b.Approvals[0].UnchangedFiles)
	assert.True(t, b.Approvals[1].ChangesRequested)
}
