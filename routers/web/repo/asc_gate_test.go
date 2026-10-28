// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"strings"
	"testing"

	perm_model "forgejo.org/models/perm"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascidentity"
	"forgejo.org/services/asctrust"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const gatePolicyText = `# .asc/policy.toml
version = 1

[trunk]
require_approvals = 2
require_checks = ["unit", "lint", "audit"]
allow_force = false

[paths."infra/**"]
write = ["role:maintainer"]

[signing]
require_signed = true

[agents]
default_scope = ["src/**", "tests/**"]

[agents.budgets]
max_changes_per_run = 8
`

func gatePolicySource(valid bool) *ascv1.GetPolicySourceResponse {
	src := make([]byte, 32)
	src[0] = 0xab
	parsed := &ascv1.GetPolicyResponse{
		Present:    true,
		Maintainer: true,
		Roles:      []*ascv1.PolicyRole{{Name: "maintainer", Members: []string{"user:priya", "user:marcus"}}},
		Trunk:      &ascv1.PolicyTrunk{Branch: "main", RequireApprovals: 2, RequireChecks: []string{"unit", "lint", "audit"}},
		Paths:      []*ascv1.PolicyPathRule{{Glob: "infra/**", Write: []string{"role:maintainer"}}},
		Agents:     &ascv1.PolicyAgents{DefaultScope: []string{"src/**", "tests/**"}, MaxChangesPerRun: 8},
	}
	if !valid {
		parsed = &ascv1.GetPolicyResponse{Present: true, Error: "line 9: role:releaser does not exist"}
	}
	return &ascv1.GetPolicySourceResponse{Present: true, Source: src, Text: []byte(gatePolicyText), Parsed: parsed}
}

func gateFake(t *testing.T) *fakeTrust {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeaturePolicy, asctrust.FeatureAudit, asctrust.FeatureCiInsights)
	f.policySource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		return gatePolicySource(true), nil
	}
	f.auditList = func(r *ascv1.ListAuditRequest) (*ascv1.ListAuditResponse, error) {
		return &ascv1.ListAuditResponse{Events: []*ascv1.AuditEvent{
			auditEvent(9, "repo.ref.update", ascv1.AuditOutcome_AUDIT_OUTCOME_DENIED),
			auditEvent(8, "policy.proposed", ascv1.AuditOutcome_AUDIT_OUTCOME_DENIED),
		}}, nil
	}
	f.listGroups = func(*ascv1.ListFailureGroupsRequest) (*ascv1.ListFailureGroupsResponse, error) {
		return &ascv1.ListFailureGroupsResponse{Groups: []*ascv1.FailureGroup{quarantinedGroup(4, "webhook::refund_timeout", "Flaky since the sandbox moved.")}}, nil
	}
	return f
}

func gatePage(t *testing.T, path string, user int64, access perm_model.AccessMode) trustPage {
	t.Helper()
	return runTrust(t, trustReq{path: "/user2/repo1/asc/gate" + path, user: user, access: access}, withLocale(AscGate))
}

func TestAscGateShowsTheRulesTheRefusalsAndTheQuarantine(t *testing.T) {
	f := gateFake(t)
	p := gatePage(t, "", 2, adminAccess)
	require.Equal(t, http.StatusOK, p.status, p.body)
	assert.Contains(t, p.body, `data-testid="asc-gate"`)
	assert.Contains(t, p.body, "Policy gate")
	assert.Contains(t, p.body, "Trunk (main) needs 2 approvals")
	assert.Contains(t, p.body, `data-testid="asc-gate-held-list"`, "an administrator sees what was refused")
	assert.Contains(t, p.body, "/user2/repo1/settings/asc/policy", "and can open the rules")
	assert.Contains(t, p.body, "do not hold up landing")
	assert.Contains(t, p.body, "max_changes_per_run = 8", "the file is shown")

	calls := f.callsOf("AuditList")
	require.Len(t, calls, 1)
	req := calls[0].Req.(*ascv1.ListAuditRequest)
	assert.Equal(t, ascv1.AuditOutcome_AUDIT_OUTCOME_DENIED, req.GetFilter().GetOutcome(), "only refusals")
	assert.NotZero(t, req.GetFilter().GetSinceMs(), "inside the window")
	assertAs(t, f.callsOf("PolicySource")[0], "2:user2", ascidentity.RoleAdmin, "user2/repo1")
}

func TestAscGateShowsNoRefusalListToAReader(t *testing.T) {
	f := gateFake(t)
	p := gatePage(t, "", 4, perm_model.AccessModeRead)
	require.Equal(t, http.StatusOK, p.status, p.body)
	assert.NotContains(t, p.body, `data-testid="asc-gate-held-list"`)
	assert.NotContains(t, p.body, `data-testid="asc-gate-empty"`, "an empty list would say nothing was refused, which a reader cannot know")
	assert.NotContains(t, p.body, "/settings/asc/policy", "a reader has no link to the settings")
	assert.Empty(t, f.callsOf("AuditList"), "the audit log is not read for a reader")
	assert.Contains(t, p.body, "Trunk (main) needs 2 approvals", "the rules are for every reader")
}

func TestAscGateSaysWhenNothingWasHeld(t *testing.T) {
	f := gateFake(t)
	f.auditList = func(*ascv1.ListAuditRequest) (*ascv1.ListAuditResponse, error) {
		return &ascv1.ListAuditResponse{}, nil
	}
	p := gatePage(t, "?days=30", 2, adminAccess)
	require.Equal(t, http.StatusOK, p.status, p.body)
	assert.Contains(t, p.body, `data-testid="asc-gate-empty"`)
	assert.Contains(t, p.body, "The gate held nothing")
	assert.Contains(t, p.body, `aria-current="page">30 days`)
}

func TestAscGateNamesAPolicyThatCannotBeUsed(t *testing.T) {
	f := gateFake(t)
	f.policySource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		return gatePolicySource(false), nil
	}
	p := gatePage(t, "", 2, adminAccess)
	require.Equal(t, http.StatusOK, p.status, p.body)
	assert.Contains(t, p.body, `data-testid="asc-gate-invalid"`)
	assert.Contains(t, p.body, "Cannot be used")
}

func TestAscGateAnswersWhenTheServerCannotBeTrusted(t *testing.T) {
	f := gateFake(t)
	f.policySource = func(*ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
		return nil, status.Error(codes.Unavailable, "secret-internal-detail")
	}
	p := gatePage(t, "", 2, adminAccess)
	assert.GreaterOrEqual(t, p.status, http.StatusBadRequest)
	assert.Contains(t, p.body, `data-testid="asc-gate-problem"`)
	assert.False(t, strings.Contains(p.body, "secret-internal-detail"), "the text of the failure is not shown")
}

func TestAscGateNeedsASignedInReaderAndTheFeature(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	startFakeTrust(t, asctrust.FeatureStacks)
	p := gatePage(t, "", 0, perm_model.AccessModeRead)
	assert.Equal(t, http.StatusForbidden, p.status)
	p = gatePage(t, "", 2, adminAccess)
	require.Equal(t, http.StatusOK, p.status, p.body)
	assert.Contains(t, p.body, `data-testid="asc-gate-unsupported"`)
}
