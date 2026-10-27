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
	"forgejo.org/services/asctrust"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func auditEvent(seq uint64, action string, outcome ascv1.AuditOutcome) *ascv1.AuditEvent {
	return &ascv1.AuditEvent{
		Seq: seq, Id: "0190b7f5-8d5e-7000-8000-00000000000" + string(rune('0'+seq%10)), TimeMs: 1_700_000_000_000,
		Actor: &ascv1.PrincipalRef{Kind: "user", Name: "user2"}, Action: action, Repo: "user2/repo1", Target: "refs/heads/main", Outcome: outcome,
		Details: map[string]string{"token": "ghp_secret", "branch": "main"},
	}
}

// adminAccess is the access of a repository administrator.
const adminAccess = perm_model.AccessModeAdmin

func TestAscAuditIsForRepositoryAdministratorsOnly(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureAudit)
	for name, tc := range map[string]struct {
		user   int64
		access perm_model.AccessMode
		want   int
	}{
		"signed out": {0, perm_model.AccessModeRead, http.StatusForbidden},
		"reader":     {4, perm_model.AccessModeRead, http.StatusForbidden},
		"writer":     {4, perm_model.AccessModeWrite, http.StatusForbidden},
	} {
		page := runTrust(t, trustReq{path: "/user2/repo1/settings/asc/audit", user: tc.user, access: tc.access}, AscAudit)
		assert.Equal(t, tc.want, page.status, name)
		export := runTrust(t, trustReq{path: "/user2/repo1/settings/asc/audit/export?format=jsonl", user: tc.user, access: tc.access}, AscAuditExport)
		assert.Equal(t, tc.want, export.status, name+" export")
		verify := runTrust(t, trustReq{method: http.MethodPost, path: "/user2/repo1/settings/asc/audit/verify", user: tc.user, access: tc.access}, AscAuditVerify)
		assert.Equal(t, tc.want, verify.status, name+" verify")
	}
	assert.Empty(t, f.calls, "nothing is asked of asc for a person who may not read the log")
}

func TestAscAuditListsEventsWithFiltersAndPaging(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureAudit)
	f.auditList = func(r *ascv1.ListAuditRequest) (*ascv1.ListAuditResponse, error) {
		return &ascv1.ListAuditResponse{Events: []*ascv1.AuditEvent{
			auditEvent(9, "repo.ref.update", ascv1.AuditOutcome_AUDIT_OUTCOME_SUCCESS),
			auditEvent(8, "policy.proposed", ascv1.AuditOutcome_AUDIT_OUTCOME_DENIED),
		}, NextPageToken: "8"}, nil
	}
	page := runTrust(t, trustReq{path: "/user2/repo1/settings/asc/audit?actor=user2&action=repo.ref.*&outcome=denied&target=refs/heads&limit=25", user: 2, access: adminAccess}, AscAudit)

	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `data-testid="asc-audit-table"`)
	assert.Contains(t, page.body, "repo.ref.update")
	assert.Contains(t, page.body, "asc_trust.audit.outcome_ok")
	assert.Contains(t, page.body, "asc_trust.audit.outcome_no")
	assert.NotContains(t, page.body, "ghp_secret", "a secret-looking detail is hidden")
	assert.Contains(t, page.body, `rel="next"`, "paging")
	assert.Contains(t, page.body, `for="asc-audit-actor"`, "fields have labels")
	assert.NotContains(t, page.body, "Audit events", "no English literal")

	calls := f.callsOf("AuditList")
	require.Len(t, calls, 1)
	req := calls[0].Req.(*ascv1.ListAuditRequest)
	assert.Equal(t, ascv1.OwnerKind_OWNER_KIND_REPO, req.GetScope().GetKind())
	assert.Equal(t, "user2/repo1", req.GetScope().GetName(), "the scope comes from the route")
	assert.Equal(t, []string{"user2"}, req.GetFilter().GetActors())
	assert.Equal(t, []string{"repo.ref.*"}, req.GetFilter().GetActions())
	assert.Equal(t, ascv1.AuditOutcome_AUDIT_OUTCOME_DENIED, req.GetFilter().GetOutcome())
	assert.Equal(t, "refs/heads", req.GetFilter().GetTarget())
	assert.EqualValues(t, 25, req.GetLimit())
	assert.Equal(t, "Bearer per-request-token", calls[0].Auth, "read as the person")
	assert.Contains(t, page.ctx.Resp.Header().Get("Cache-Control"), "no-store")

	// The next page carries the marker and keeps the filters.
	next := runTrust(t, trustReq{path: "/user2/repo1/settings/asc/audit?actor=user2&page=8", user: 2, access: adminAccess}, AscAudit)
	assert.Equal(t, http.StatusOK, next.status)
	assert.Equal(t, "8", f.callsOf("AuditList")[1].Req.(*ascv1.ListAuditRequest).GetPageToken())
}

func TestAscAuditRefusesBadFiltersAndShowsFixedErrors(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureAudit)
	bad := runTrust(t, trustReq{path: "/user2/repo1/settings/asc/audit?outcome=bogus&limit=7", user: 2, access: adminAccess}, AscAudit)
	assert.NotEqual(t, http.StatusInternalServerError, bad.status)
	assert.Empty(t, f.callsOf("AuditList"), "a filter that failed the local check is not sent")

	f.auditList = func(*ascv1.ListAuditRequest) (*ascv1.ListAuditResponse, error) {
		return nil, status.Error(codes.PermissionDenied, "internal detail /srv/asc")
	}
	page := runTrust(t, trustReq{path: "/user2/repo1/settings/asc/audit", user: 2, access: adminAccess}, AscAudit)
	assert.Equal(t, http.StatusForbidden, page.status)
	assert.Contains(t, page.body, "asc_trust.err.denied")
	assert.False(t, strings.Contains(page.body, "/srv/asc"))
}

func TestAscAuditVerifyChain(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureAudit)
	f.auditChain = func(*ascv1.VerifyAuditChainRequest) (*ascv1.VerifyAuditChainResponse, error) {
		return &ascv1.VerifyAuditChainResponse{Ok: true, Checked: 41}, nil
	}
	page := runTrust(t, trustReq{method: http.MethodPost, path: "/user2/repo1/settings/asc/audit/verify", user: 2, access: adminAccess}, AscAuditVerify)
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `data-testid="asc-audit-chain"`)
	assert.Contains(t, page.body, `name="_csrf"`, "the verify form carries the CSRF token")
	assert.Equal(t, "user2/repo1", f.callsOf("VerifyChain")[0].Req.(*ascv1.VerifyAuditChainRequest).GetScope().GetName())
}
