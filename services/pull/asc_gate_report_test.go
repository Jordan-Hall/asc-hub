// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"sync"
	"testing"

	"forgejo.org/models/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type gateReports struct {
	mu   sync.Mutex
	list []map[string]bool // rule -> held, one map per report
	of   []string          // the change of each report
}

func (g *gateReports) use(t *testing.T) {
	t.Helper()
	t.Cleanup(UseAscGateReporter(func(_ context.Context, _, change string, rules []AscGateRule) {
		g.mu.Lock()
		defer g.mu.Unlock()
		m := map[string]bool{}
		for _, r := range rules {
			m[r.Rule] = r.Held
		}
		g.list = append(g.list, m)
		g.of = append(g.of, change)
	}))
}

// The gate tells asc what it decided: every rule it looked at, and the one that held the change.
func TestAscGateReportsWhatItDecided(t *testing.T) {
	bottom, top, _ := ascMergeFixture(t)
	doer, perm := ascMergePerm(t, top, 2)
	var rep gateReports
	rep.use(t)

	checks, err := CheckAscStack(t.Context(), doer, perm, top)
	require.NoError(t, err)
	require.Len(t, checks, 2)
	for _, c := range checks {
		require.NoError(t, c.Err)
	}
	require.Len(t, rep.list, 2, "one report per change of the prefix")
	for _, m := range rep.list {
		assert.Len(t, m, 6, "a change that passes was measured against every rule")
		for rule, held := range m {
			assert.False(t, held, rule)
		}
	}
	assert.Len(t, rep.of[0], 32)

	// A closed change is held by the first rule, and no later rule is claimed to have passed.
	_, err = db.GetEngine(t.Context()).Exec("UPDATE `issue` SET is_closed=? WHERE id=?", true, bottom.IssueID)
	require.NoError(t, err)
	rep.list, rep.of = nil, nil
	checks, err = CheckAscStack(t.Context(), doer, perm, top)
	require.NoError(t, err)
	require.ErrorIs(t, checks[0].Err, ErrAscStackNotMergable)
	require.NotEmpty(t, rep.list)
	assert.Equal(t, map[string]bool{ascGateRuleOpen: true}, rep.list[0])
}

// A person who may not merge is held by that rule, and the report names no person and no text of the refusal.
func TestAscGateReportsAPersonWhoMayNotMerge(t *testing.T) {
	_, top, _ := ascMergeFixture(t)
	doer, perm := ascMergePerm(t, top, 5)
	var rep gateReports
	rep.use(t)

	_, err := MergeAscStack(t.Context(), doer, perm, top)
	require.ErrorIs(t, err, ErrAscStackNotAllowed)
	require.NotEmpty(t, rep.list)
	assert.True(t, rep.list[0][ascGateRuleMayMerge])
	assert.False(t, rep.list[0][ascGateRuleOpen])
}

func TestAscChangeRaw(t *testing.T) {
	assert.Nil(t, ascChangeRaw(""))
	assert.Nil(t, ascChangeRaw("kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk")) // 31
	assert.Nil(t, ascChangeRaw("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	assert.Equal(t, make([]byte, 0, 16)[:0], ascChangeRaw("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")[:0])
	raw := ascChangeRaw("zyzyzyzyzyzyzyzyzyzyzyzyzyzyzyzy")
	require.Len(t, raw, 16)
	assert.Equal(t, byte(0x01), raw[0])
}
