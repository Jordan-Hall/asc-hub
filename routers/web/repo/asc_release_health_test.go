// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
)

const relDay = 24 * 3600

func TestRelMedianGap(t *testing.T) {
	assert.Zero(t, relMedianGap(nil))
	assert.Zero(t, relMedianGap([]relHealthRelease{{Created: 10}}))
	// gaps 10, 2, 4: the median is 4
	assert.EqualValues(t, 4, relMedianGap([]relHealthRelease{{Created: 30}, {Created: 20}, {Created: 18}, {Created: 14}}))
	// gaps 10, 2: the median is their mean
	assert.EqualValues(t, 6, relMedianGap([]relHealthRelease{{Created: 30}, {Created: 20}, {Created: 18}}))
	// a release created before the one listed after it counts as no gap
	assert.Zero(t, relMedianGap([]relHealthRelease{{Created: 5}, {Created: 9}}))
}

func TestRelCadenceText(t *testing.T) {
	ctx, _ := contexttest.MockContext(t, "/user2/repo1/releases")
	assert.Equal(t, "asc_rp.rel_cadence_weeks", relCadenceText(ctx, 17*relDay))
	assert.Equal(t, "asc_rp.rel_cadence_days", relCadenceText(ctx, 5*relDay))
	assert.Equal(t, "asc_rp.rel_cadence_under_day", relCadenceText(ctx, 3600))
}

func TestRelRollbacks(t *testing.T) {
	// c is older than b: the release of c rolled back; d before it is a fresh commit
	rels := []relHealthRelease{{Sha: "c"}, {Sha: "b"}, {Sha: "b"}, {Sha: "a"}}
	older := map[string]bool{"c>b": true, "b>b": true, "a>b": true}
	isAncestor := func(a, b string) bool { return older[a+">"+b] }
	n, counted := relRollbacks(rels, 6, isAncestor)
	assert.Equal(t, 1, n, "the same commit released again is not a rollback")
	assert.Equal(t, 4, counted)

	n, counted = relRollbacks(rels, 2, isAncestor)
	assert.Equal(t, 1, n)
	assert.Equal(t, 2, counted, "only the window is counted")

	n, counted = relRollbacks(nil, 6, isAncestor)
	assert.Zero(t, n)
	assert.Zero(t, counted)
}
