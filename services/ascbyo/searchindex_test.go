// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo

import (
	"math"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
)

func TestSearchIndexOf(t *testing.T) {
	si := SearchIndexOf(&ascv1.IndexStatusResponse{Indexes: []*ascv1.IndexStatus{
		{Index: "files", Coverage: 0.876, LagSeconds: 61},
		{Index: "chunks", Coverage: 0.876},
		{Index: "vectors", Coverage: 0.5, Version: "jina-embeddings-v2-base-code@ab12"},
		{Index: "files@vendor/lib", Coverage: 0.1},
		{Index: "vectors@vendor/lib", Coverage: 0.2, Version: "other"},
	}})
	assert.Equal(t, &SearchIndex{
		TextPercent: 87, TextKnown: true, MeaningOn: true, MeaningModel: "jina-embeddings-v2-base-code@ab12",
		MeaningPercent: 50, LagMinutes: 2,
	}, si, "a submodule's entries are left out and the lag is rounded up")

	si = SearchIndexOf(&ascv1.IndexStatusResponse{Indexes: []*ascv1.IndexStatus{
		{Index: "files", Coverage: 1}, {Index: "vectors", Coverage: 0},
	}})
	assert.True(t, si.TextKnown)
	assert.Equal(t, 100, si.TextPercent)
	assert.False(t, si.MeaningOn, "a vectors index without a model is off")
	assert.Zero(t, si.LagMinutes)

	si = SearchIndexOf(&ascv1.IndexStatusResponse{Indexes: []*ascv1.IndexStatus{
		{Index: "files", Coverage: float32(math.NaN())}, {Index: "vectors", Coverage: 2, Version: "m"},
	}})
	assert.False(t, si.TextKnown, "a coverage that is not between 0 and 1 is dropped")
	assert.False(t, si.MeaningOn)

	si = SearchIndexOf(&ascv1.IndexStatusResponse{Indexes: []*ascv1.IndexStatus{{Index: "files", Coverage: 0.9}, {Index: "vectors", Coverage: 0.42, Version: "m"}}})
	assert.Equal(t, 90, si.TextPercent, "0.9 as a float32 is 90%")
	assert.Equal(t, 42, si.MeaningPercent)

	assert.Equal(t, &SearchIndex{}, SearchIndexOf(nil))
}
