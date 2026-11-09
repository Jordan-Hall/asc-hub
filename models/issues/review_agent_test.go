// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issues_test

import (
	"testing"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An agent comments on a pull request and asks for changes; only a person approves.
func TestSubmitReviewRefusesAnAgentApproval(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 2})
	require.NoError(t, issue.LoadRepo(t.Context()))
	agent := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	agent.Type = user_model.UserTypeBot

	_, _, err := issues_model.SubmitReview(t.Context(), agent, issue, issues_model.ReviewTypeApprove, "looks good", "", false, nil)
	require.ErrorIs(t, err, issues_model.ErrAgentMayNotApprove)
	unittest.AssertNotExistsBean(t, &issues_model.Review{IssueID: issue.ID, ReviewerID: agent.ID, Type: issues_model.ReviewTypeApprove})

	review, _, err := issues_model.SubmitReview(t.Context(), agent, issue, issues_model.ReviewTypeComment, "one question", "", false, nil)
	require.NoError(t, err)
	assert.Equal(t, issues_model.ReviewTypeComment, review.Type)
	review, _, err = issues_model.SubmitReview(t.Context(), agent, issue, issues_model.ReviewTypeReject, "the test fails", "", false, nil)
	require.NoError(t, err)
	assert.Equal(t, issues_model.ReviewTypeReject, review.Type)

	person := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	review, _, err = issues_model.SubmitReview(t.Context(), person, issue, issues_model.ReviewTypeApprove, "", "", false, nil)
	require.NoError(t, err)
	assert.Equal(t, issues_model.ReviewTypeApprove, review.Type)
}
