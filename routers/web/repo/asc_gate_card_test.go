// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	api "forgejo.org/modules/structs"

	"github.com/stretchr/testify/assert"
)

func TestAscGateRules(t *testing.T) {
	land := &ascv1.PolicyLandRequirements{Approvals: 2, Checks: []string{"unit", "lint", "integration"}}
	statuses := map[string]api.CommitStatusState{"unit": api.CommitStatusSuccess, "lint": api.CommitStatusSuccess, "integration": api.CommitStatusPending}
	rules := ascGateRules(land, 1, statuses, true, 2)
	byKind := map[string]AscGateRule{}
	for _, r := range rules {
		byKind[r.Kind] = r
	}
	assert.True(t, byKind["approvals"].Held)
	assert.EqualValues(t, 1, byKind["approvals"].Have)
	assert.True(t, byKind["checks"].Held)
	assert.True(t, byKind["checks"].Pending, "held only because a check is still running")
	assert.Equal(t, []string{"integration"}, byKind["checks"].Names)
	assert.True(t, byKind["order"].Held)
	assert.False(t, byKind["threads"].Held, "open threads are advice")
	assert.Equal(t, 3, ascGateToClear(rules))

	statuses["integration"] = api.CommitStatusFailure
	assert.False(t, ascGateRules(land, 2, statuses, false, 0)[1].Pending, "a failed check is not pending")

	statuses["integration"] = api.CommitStatusSuccess
	clear := ascGateRules(land, 2, statuses, false, 0)
	assert.Zero(t, ascGateToClear(clear))
	assert.Len(t, clear, 3, "approvals, checks and order; no threads rule without open threads")

	assert.Len(t, ascGateRules(nil, 0, nil, false, 0), 1, "without a policy only the order is known")
}

func TestAscGateReport(t *testing.T) {
	rules := ascGateRules(&ascv1.PolicyLandRequirements{Approvals: 1}, 0, nil, false, 4)
	rep := ascGateReport(rules)
	assert.Len(t, rep, 2, "the threads advice is not reported")
	assert.Equal(t, "Approvals", rep[0].GetRule())
	assert.True(t, rep[0].GetHeld())
	assert.Equal(t, "Stack order", rep[1].GetRule())
	assert.False(t, rep[1].GetHeld())
}
