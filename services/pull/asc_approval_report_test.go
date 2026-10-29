// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"testing"

	issues_model "forgejo.org/models/issues"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type approvalCall struct {
	reviewer int64
	change   []byte
	state    ascv1.ApprovalState
}

// A review of a pull request of an asc stack is reported to asc as the reviewer: an approval that stands, and a request for
// changes. A comment, a stale approval and a dismissed one are not.
func TestAscApprovalIsReportedAsTheReviewer(t *testing.T) {
	_, top, _ := ascMergeFixture(t)
	doer, _ := ascMergePerm(t, top, 2)
	prevName := ascRepoNameOf
	ascRepoNameOf = func(context.Context, *issues_model.PullRequest) (string, error) { return "owner/repo", nil }
	t.Cleanup(func() { ascRepoNameOf = prevName })
	var calls []approvalCall
	t.Cleanup(UseAscApprovalReporter(func(_ context.Context, reviewer *user_model.User, _ string, change []byte, state ascv1.ApprovalState) {
		calls = append(calls, approvalCall{reviewer.ID, change, state})
	}))

	ascReportApproval(t.Context(), doer, top, &issues_model.Review{Type: issues_model.ReviewTypeApprove})
	require.Len(t, calls, 1)
	assert.Equal(t, doer.ID, calls[0].reviewer, "the call is made for the person who reviewed")
	assert.Equal(t, ascv1.ApprovalState_APPROVAL_STATE_APPROVED, calls[0].state)
	assert.Len(t, calls[0].change, 16)

	ascReportApproval(t.Context(), doer, top, &issues_model.Review{Type: issues_model.ReviewTypeReject})
	require.Len(t, calls, 2)
	assert.Equal(t, ascv1.ApprovalState_APPROVAL_STATE_CHANGES_REQUESTED, calls[1].state)

	for _, r := range []*issues_model.Review{
		{Type: issues_model.ReviewTypeComment},
		{Type: issues_model.ReviewTypeApprove, Stale: true},
		{Type: issues_model.ReviewTypeApprove, Dismissed: true},
		nil,
	} {
		ascReportApproval(t.Context(), doer, top, r)
	}
	ascReportApproval(t.Context(), nil, top, &issues_model.Review{Type: issues_model.ReviewTypeApprove})
	assert.Len(t, calls, 2, "nothing else is reported")
}
