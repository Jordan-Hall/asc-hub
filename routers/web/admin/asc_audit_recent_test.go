// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDashboardShowsTheNewestAuditEntries(t *testing.T) {
	f := startInstanceAgents(t, asctrust.FeatureAudit)
	for i := 1; i <= 8; i++ {
		f.events = append(f.events, &ascv1.AuditEvent{
			Seq: uint64(i), Id: "00000000-0000-4000-8000-00000000000" + string(rune('0'+i)), TimeMs: 1_700_000_000_000 + int64(i)*1000,
			Actor: &ascv1.PrincipalRef{Kind: "user", Name: "maya"}, Action: "repo.renamed", Repo: "acme/<b>x</b>", Outcome: ascv1.AuditOutcome_AUDIT_OUTCOME_SUCCESS,
		})
	}
	ctx, _ := contexttest.MockContext(t, "admin")
	contexttest.LoadUser(t, ctx, 1)
	ctx.IsSigned = true
	rows := ascRecentAudit(ctx)
	require.Len(t, rows, ascAuditRecentRows, "at most five, whatever asc answers")
	require.NotEmpty(t, f.listed)
	assert.EqualValues(t, ascAuditRecentRows, f.listed[0].GetLimit())
	assert.Equal(t, ascv1.OwnerKind_OWNER_KIND_INSTANCE, f.listed[0].GetScope().GetKind(), "the audit log of the whole server")

	// not an administrator: nothing is asked
	n := len(f.listed)
	ctx2, _ := contexttest.MockContext(t, "admin")
	contexttest.LoadUser(t, ctx2, 2)
	ctx2.IsSigned = true
	assert.Empty(t, ascRecentAudit(ctx2))
	assert.Len(t, f.listed, n)
}

func TestDashboardLeavesTheCardOutWithoutTheAuditService(t *testing.T) {
	f := startInstanceAgents(t) // asc does not advertise audit.v1
	ctx, _ := contexttest.MockContext(t, "admin")
	contexttest.LoadUser(t, ctx, 1)
	ctx.IsSigned = true
	assert.Empty(t, ascRecentAudit(ctx))
	assert.Empty(t, f.listed)
}
