// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"math"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCoverage(t *testing.T) {
	f := &fakeAsc{index: func(req *ascv1.IndexStatusRequest) (*ascv1.IndexStatusResponse, error) {
		switch req.GetScope().GetRepo().GetName() {
		case "o/r1":
			return &ascv1.IndexStatusResponse{
				Scope: &ascv1.ResolvedScope{Changeset: []byte{0xe9, 0x1b, 0x0d, 0x44}},
				Indexes: []*ascv1.IndexStatus{
					{Index: "files", Coverage: 0.992, LagSeconds: 12},
					{Index: "files@vendor/sub", Coverage: 0.5, LagSeconds: 40},
					{Index: "vectors", Coverage: 0.1},
				},
			}, nil
		case "o/r2":
			return &ascv1.IndexStatusResponse{Indexes: []*ascv1.IndexStatus{{Index: "files", Coverage: 1}}}, nil
		case "o/r3":
			return nil, status.Error(codes.Internal, "boom")
		case "o/r4":
			return &ascv1.IndexStatusResponse{Indexes: []*ascv1.IndexStatus{{Index: "files", Coverage: float32(math.NaN())}}}, nil
		case "o/r5":
			// No index of the mode is listed: the resolved scope stands in.
			return &ascv1.IndexStatusResponse{Scope: &ascv1.ResolvedScope{Coverage: 0.8}, Indexes: []*ascv1.IndexStatus{{Index: "chunks", Coverage: 1}}}, nil
		}
		return &ascv1.IndexStatusResponse{}, nil
	}}
	svc := startFake(t, f)

	one := svc.Coverage(sys(t), reposN(1), IndexFiles)
	assert.Equal(t, 1, one.Known)
	assert.Equal(t, 50, one.Percent, "the lowest entry of the mode, the submodule, rules")
	assert.False(t, one.Complete)
	assert.EqualValues(t, 40, one.LagSeconds)
	assert.Equal(t, "e91b0d4", one.Revision)

	vec := svc.Coverage(sys(t), reposN(1), IndexVectors)
	assert.Equal(t, 10, vec.Percent)

	full := svc.Coverage(sys(t), []Repo{repoN(2)}, IndexFiles)
	assert.True(t, full.Complete)
	assert.Equal(t, 100, full.Percent)

	mixed := svc.Coverage(sys(t), []Repo{repoN(2), repoN(3), repoN(4), repoN(5)}, IndexFiles)
	assert.Equal(t, 4, mixed.Asked)
	assert.Equal(t, 2, mixed.Known, "a failed call and an impossible coverage are unknown, not zero")
	assert.Equal(t, 90, mixed.Percent)
	assert.False(t, mixed.Complete)
	assert.Empty(t, mixed.Revision)

	big := svc.Coverage(sys(t), reposN(40), IndexFiles)
	assert.Equal(t, maxCoverageRepos, big.Asked)
	assert.True(t, big.Sampled)
	assert.False(t, big.Complete, "a sample is never the whole")

	none := svc.Coverage(sys(t), nil, IndexFiles)
	assert.Zero(t, none.Asked)
	assert.False(t, none.Complete)

	// No credential: the note is advice, so it is unknown rather than an error.
	unk := svc.Coverage(t.Context(), reposN(1), IndexFiles)
	assert.Zero(t, unk.Known)
}
