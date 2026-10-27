// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
	"context"
	"strings"
	"testing"
	"time"

	issues_model "forgejo.org/models/issues"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscReviewedVersionOf(t *testing.T) {
	c1, c2, c3 := strings.Repeat("1", 40), strings.Repeat("2", 40), strings.Repeat("3", 40)
	commits := []string{c1, c2, c3}
	rv := func(typ issues_model.ReviewType, commit string, dismissed bool) *issues_model.Review {
		return &issues_model.Review{Type: typ, CommitID: commit, Dismissed: dismissed}
	}

	assert.Zero(t, reviewedVersionOf(nil, commits))
	assert.Zero(t, reviewedVersionOf([]*issues_model.Review{rv(issues_model.ReviewTypeApprove, c1, false)}, nil), "no versions known")

	// The newest version any published review was made on.
	assert.Equal(t, 2, reviewedVersionOf([]*issues_model.Review{
		rv(issues_model.ReviewTypeApprove, c1, false), rv(issues_model.ReviewTypeComment, c2, false),
	}, commits))
	assert.Equal(t, 3, reviewedVersionOf([]*issues_model.Review{
		rv(issues_model.ReviewTypeReject, c3, false), rv(issues_model.ReviewTypeApprove, c1, false),
	}, commits), "the order of the reviews does not matter")

	// Dismissed, pending and request reviews, a commit that is no version and a bad id count for nothing.
	assert.Equal(t, 1, reviewedVersionOf([]*issues_model.Review{
		rv(issues_model.ReviewTypeApprove, c1, false),
		rv(issues_model.ReviewTypeApprove, c3, true),
		rv(issues_model.ReviewTypeComment, c2, true),
		rv(issues_model.ReviewTypePending, c3, false),
		rv(issues_model.ReviewTypeRequest, c3, false),
		rv(issues_model.ReviewTypeApprove, strings.Repeat("9", 40), false),
		rv(issues_model.ReviewTypeApprove, "abc", false),
		nil,
	}, commits))

	// A commit two versions share is the later one; the case of the hex digits does not matter.
	shared := []string{strings.Repeat("a", 40), strings.Repeat("a", 40)}
	assert.Equal(t, 2, reviewedVersionOf([]*issues_model.Review{rv(issues_model.ReviewTypeApprove, strings.ToUpper(shared[0]), false)}, shared))
}

func TestAscReviewStateCarriesReviewedVersion(t *testing.T) {
	fx := newFixture(t)
	now := time.Unix(1700000000, 0)
	f := fx.facts()
	f.ReviewedVersion = 3
	_, st, err := BuildReviewState(f, now)
	require.NoError(t, err)
	assert.EqualValues(t, 3, st.ReviewedVersion)

	f.ReviewedVersion = -1
	_, st, err = BuildReviewState(f, now)
	require.NoError(t, err)
	assert.Zero(t, st.ReviewedVersion)

	f.ReviewedVersion = 1 << 40
	_, st, err = BuildReviewState(f, now)
	require.NoError(t, err)
	assert.EqualValues(t, maxVersions, st.ReviewedVersion)
}

func TestAscRegisterVersionCommits(t *testing.T) {
	prev := versionCommitSource()
	called := 0
	restore := RegisterVersionCommits(func(context.Context, *issues_model.PullRequest) ([]string, error) {
		called++
		return []string{"x"}, nil
	})
	src := versionCommitSource()
	require.NotNil(t, src)
	got, err := src(t.Context(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"x"}, got)
	assert.Equal(t, 1, called)

	restore()
	assert.Equal(t, prev == nil, versionCommitSource() == nil, "the previous source is back")
}
