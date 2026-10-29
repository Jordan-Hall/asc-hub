// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"errors"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type approvalCall struct {
	doer   int64
	report AscApprovalReport
}

type approvalFixture struct {
	pr    *issues_model.PullRequest
	doer  *user_model.User
	calls []approvalCall
	gate  func(doer *user_model.User) (bool, error)
}

type approvalCtxKey struct{}

// newApprovalFixture is a pull request of an asc stack whose change has three versions (commits a, b, c), a reporter that records
// what would be sent, and a gate that allows every person unless a test says otherwise.
func newApprovalFixture(t *testing.T) *approvalFixture {
	t.Helper()
	f := &approvalFixture{}
	f.pr = setupAscVersions(t, &fakeChanges{versions: threeVersions()})
	f.doer = unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	t.Cleanup(UseAscApprovalReporter(func(callCtx context.Context, doer *user_model.User, r AscApprovalReport) {
		assert.Equal(t, doer.ID, callCtx.Value(approvalCtxKey{}), "the call context is the one the gate made for this person")
		f.calls = append(f.calls, approvalCall{doer.ID, r})
	}))
	t.Cleanup(SetAscUserCall(func(ctx context.Context, doer *user_model.User, _ *repo_model.Repository) (context.Context, bool, error) {
		if f.gate != nil {
			if ok, err := f.gate(doer); !ok || err != nil {
				return ctx, false, err
			}
		}
		return context.WithValue(ctx, approvalCtxKey{}, doer.ID), true, nil
	}))
	return f
}

func approvalOn(t *testing.T, f *approvalFixture, typ issues_model.ReviewType, reviewer int64, commit string) *issues_model.Review {
	t.Helper()
	r := &issues_model.Review{IssueID: f.pr.IssueID, ReviewerID: reviewer, Type: typ, CommitID: commit}
	_, err := db.GetEngine(t.Context()).Insert(r)
	require.NoError(t, err)
	return r
}

// A review of a pull request of an asc stack is reported to asc as the reviewer, bound to the version of the commit that was
// reviewed: an approval that stands, and a request for changes. A comment, a stale approval and a dismissed one are not.
func TestAscApprovalIsReportedAsTheReviewerOnItsVersion(t *testing.T) {
	f := newApprovalFixture(t)

	ascReportApproval(t.Context(), f.doer, f.pr, &issues_model.Review{Type: issues_model.ReviewTypeApprove, CommitID: gitHexN('b')})
	require.Len(t, f.calls, 1)
	assert.Equal(t, f.doer.ID, f.calls[0].doer, "the call is made for the person who reviewed")
	assert.Equal(t, ascv1.ApprovalState_APPROVAL_STATE_APPROVED, f.calls[0].report.State)
	assert.Equal(t, uint32(2), f.calls[0].report.Version, "bound to the version whose commit was reviewed, not the newest")
	assert.Equal(t, "owner/repo", f.calls[0].report.Repo)
	assert.Len(t, f.calls[0].report.Change, 16)
	assert.Zero(t, f.calls[0].report.ReviewerID)

	ascReportApproval(t.Context(), f.doer, f.pr, &issues_model.Review{Type: issues_model.ReviewTypeReject, CommitID: gitHexN('a')})
	require.Len(t, f.calls, 2)
	assert.Equal(t, ascv1.ApprovalState_APPROVAL_STATE_CHANGES_REQUESTED, f.calls[1].report.State)
	assert.Equal(t, uint32(1), f.calls[1].report.Version)

	for _, r := range []*issues_model.Review{
		{Type: issues_model.ReviewTypeComment, CommitID: gitHexN('c')},
		{Type: issues_model.ReviewTypeApprove, CommitID: gitHexN('c'), Stale: true},
		{Type: issues_model.ReviewTypeApprove, CommitID: gitHexN('c'), Dismissed: true},
		nil,
	} {
		ascReportApproval(t.Context(), f.doer, f.pr, r)
	}
	ascReportApproval(t.Context(), nil, f.pr, &issues_model.Review{Type: issues_model.ReviewTypeApprove, CommitID: gitHexN('c')})
	assert.Len(t, f.calls, 2, "nothing else is reported")
}

// An approval is never claimed for a version it was not made on: a commit that is no verified version of the change is not reported.
func TestAscApprovalWithoutAVerifiedVersionIsNotReported(t *testing.T) {
	f := newApprovalFixture(t)
	for _, commit := range []string{"", "abc", gitHexN('d'), gitHexN('z')} {
		ascReportApproval(t.Context(), f.doer, f.pr, &issues_model.Review{Type: issues_model.ReviewTypeApprove, CommitID: commit})
	}
	assert.Empty(t, f.calls)
}

// The central gate decides who may give a verdict that asc counts: a person it refuses, a gate that fails and a hub without the gate
// report nothing.
func TestAscApprovalIsSkippedWhenTheGateRefuses(t *testing.T) {
	f := newApprovalFixture(t)
	review := &issues_model.Review{Type: issues_model.ReviewTypeApprove, CommitID: gitHexN('c')}

	f.gate = func(*user_model.User) (bool, error) { return false, nil }
	ascReportApproval(t.Context(), f.doer, f.pr, review)
	f.gate = func(*user_model.User) (bool, error) { return false, errors.New("boom") }
	ascReportApproval(t.Context(), f.doer, f.pr, review)
	assert.Empty(t, f.calls)

	f.gate = nil
	restore := SetAscUserCall(nil)
	ascReportApproval(t.Context(), f.doer, f.pr, review)
	restore()
	assert.Empty(t, f.calls, "no gate, no report")

	ascReportApproval(t.Context(), f.doer, f.pr, review)
	assert.Len(t, f.calls, 1)
}

// A review that is dismissed is reported to asc as DISMISSED, naming the reviewer by the hub's id; the person who dismissed is the
// caller. Another standing verdict of the reviewer keeps it from being reported.
func TestAscDismissalIsReportedForTheReviewer(t *testing.T) {
	f := newApprovalFixture(t)
	maintainer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	review := approvalOn(t, f, issues_model.ReviewTypeApprove, f.doer.ID, gitHexN('b'))

	// Still standing: nothing is dismissed in the hub yet, so asc is not told.
	ascReportDismissal(t.Context(), maintainer, f.pr, review)
	assert.Empty(t, f.calls)

	require.NoError(t, issues_model.DismissReview(t.Context(), review, true))
	ascReportDismissal(t.Context(), maintainer, f.pr, review)
	require.Len(t, f.calls, 1)
	assert.Equal(t, maintainer.ID, f.calls[0].doer, "the caller is the person who dismissed")
	assert.Equal(t, ascv1.ApprovalState_APPROVAL_STATE_DISMISSED, f.calls[0].report.State)
	assert.Equal(t, f.doer.ID, f.calls[0].report.ReviewerID)
	assert.Equal(t, uint32(2), f.calls[0].report.Version, "the version of the review")

	// A commit that is no version still dismisses (it only takes a verdict away), on the newest version.
	odd := approvalOn(t, f, issues_model.ReviewTypeReject, f.doer.ID+1, gitHexN('f'))
	require.NoError(t, issues_model.DismissReview(t.Context(), odd, true))
	ascReportDismissal(t.Context(), maintainer, f.pr, odd)
	require.Len(t, f.calls, 2)
	assert.Equal(t, uint32(3), f.calls[1].report.Version)

	// The same reviewer has a newer standing approval: that one is not taken away.
	approvalOn(t, f, issues_model.ReviewTypeApprove, f.doer.ID, gitHexN('c'))
	ascReportDismissal(t.Context(), maintainer, f.pr, review)
	assert.Len(t, f.calls, 2)

	// A comment is not a verdict.
	ascReportDismissal(t.Context(), maintainer, f.pr, &issues_model.Review{Type: issues_model.ReviewTypeComment, ReviewerID: 9, IssueID: f.pr.IssueID})
	assert.Len(t, f.calls, 2)

	// The gate refuses the person who dismisses.
	f.gate = func(*user_model.User) (bool, error) { return false, nil }
	ascReportDismissal(t.Context(), maintainer, f.pr, odd)
	assert.Len(t, f.calls, 2)
}

// Dismissing on new commits (DismissApprovalReviews) tells asc once per reviewer; dismissing by hand tells asc, and undoing it does
// not: the hub does not speak for a reviewer who did not review again.
func TestAscDismissalFlows(t *testing.T) {
	f := newApprovalFixture(t)
	maintainer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	require.NoError(t, f.pr.LoadIssue(t.Context()))
	require.NoError(t, f.pr.Issue.LoadRepo(t.Context()))

	a := approvalOn(t, f, issues_model.ReviewTypeApprove, f.doer.ID, gitHexN('a'))
	b := approvalOn(t, f, issues_model.ReviewTypeApprove, 4, gitHexN('c'))
	require.NoError(t, DismissApprovalReviews(t.Context(), maintainer, f.pr))
	got := map[int64]uint32{}
	for _, c := range f.calls {
		assert.Equal(t, ascv1.ApprovalState_APPROVAL_STATE_DISMISSED, c.report.State)
		assert.Equal(t, int64(1), c.doer, "the caller is the person whose push dismissed them")
		_, twice := got[c.report.ReviewerID]
		assert.False(t, twice, "once per reviewer")
		got[c.report.ReviewerID] = c.report.Version
	}
	assert.Equal(t, uint32(1), got[f.doer.ID])
	assert.Equal(t, uint32(3), got[4])

	// By hand: undo (not reported), then dismiss again (reported).
	f.calls = nil
	_, err := DismissReview(t.Context(), a.ID, f.pr.Issue.RepoID, "", maintainer, false, false)
	require.NoError(t, err)
	assert.Empty(t, f.calls, "un-dismissing is not reported")
	_, err = DismissReview(t.Context(), a.ID, f.pr.Issue.RepoID, "outdated", maintainer, true, false)
	require.NoError(t, err)
	require.Len(t, f.calls, 1)
	assert.Equal(t, f.doer.ID, f.calls[0].report.ReviewerID)
	assert.Equal(t, uint32(1), f.calls[0].report.Version)
	_ = b
}
