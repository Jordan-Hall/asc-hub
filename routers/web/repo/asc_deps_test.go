// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	perm_model "forgejo.org/models/perm"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascidentity"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const depsOp = "0190b7f5-8d5e-7000-8000-000000000001"

func depsFinding(id uint64, sev ascv1.DepSeverity, state ascv1.DepFindingState) *ascv1.DepFinding {
	return &ascv1.DepFinding{
		Id: id, Ecosystem: "npm", Name: "left-pad", Version: "1.0.0", Direct: true, Path: []byte("package-lock.json"), State: state, FixedIn: "1.3.0",
		FirstSeen: 1_700_000_000, LastSeen: 1_700_000_000,
		Advisory: &ascv1.DepAdvisory{Id: "GHSA-aaaa", Summary: "Prototype pollution <script>alert(1)</script>", Severity: sev, Cvss: 7.5, Url: "https://github.com/advisories/GHSA-aaaa"},
	}
}

func depsGet(t *testing.T, query string, user int64, access perm_model.AccessMode) trustPage {
	t.Helper()
	return runTrust(t, trustReq{path: "/user2/repo1/asc/deps" + query, user: user, access: access}, AscDeps)
}

func depsPost(t *testing.T, path string, h func(*context.Context), user int64, access perm_model.AccessMode, form url.Values, params map[string]string) trustPage {
	t.Helper()
	return runTrust(t, trustReq{method: http.MethodPost, path: "/user2/repo1/asc/deps" + path, user: user, access: access, form: form, params: params}, h)
}

func TestAscDepsFindingsView(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureDeps)
	f.depFindings = func(r *ascv1.ListDepFindingsRequest) (*ascv1.ListDepFindingsResponse, error) {
		return &ascv1.ListDepFindingsResponse{
			Findings:      []*ascv1.DepFinding{depsFinding(1, ascv1.DepSeverity_DEP_SEVERITY_HIGH, ascv1.DepFindingState_DEP_FINDING_STATE_OPEN)},
			NextPageToken: "n_2", CountsByState: map[string]uint32{"open": 4, "dismissed": 2}, LastScan: 1_700_000_000, LastLookup: ascv1.DepLookupStatus_DEP_LOOKUP_STATUS_PARTIAL,
		}, nil
	}
	page := depsGet(t, "?severity=moderate&ecosystem=npm", 2, adminAccess)

	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `data-testid="asc-dep-finding"`)
	assert.Contains(t, page.body, "asc_trust.deps.severity.high")
	assert.Contains(t, page.body, `href="https://github.com/advisories/GHSA-aaaa" rel="noopener noreferrer nofollow"`)
	assert.NotContains(t, page.body, "<script>alert(1)</script>", "advisory text is escaped")
	assert.Contains(t, page.body, `data-testid="asc-deps-lookup"`, "a partial lookup is said, never drawn as clean")
	assert.Contains(t, page.body, "asc_trust.deps.last_lookup_partial")
	assert.Contains(t, page.body, `data-testid="asc-deps-tab-open"`)
	assert.Contains(t, page.body, `rel="next"`)
	assert.Contains(t, page.body, `for="asc-deps-severity"`, "fields have labels")
	assert.Contains(t, page.body, `data-testid="asc-deps-scan"`, "an administrator can scan")
	assert.Contains(t, page.body, `name="_csrf" value="csrf-token-for-tests"`)
	assert.NotContains(t, page.body, "Vulnerable dependencies", "no English literal")

	calls := f.callsOf("DepFindings")
	require.Len(t, calls, 1)
	req := calls[0].Req.(*ascv1.ListDepFindingsRequest)
	assert.Equal(t, "user2/repo1", req.GetRepo().GetName())
	assert.Equal(t, ascv1.DepSeverity_DEP_SEVERITY_MODERATE, req.GetMinSeverity())
	assert.Equal(t, []string{"npm"}, req.GetEcosystems())
	assertAs(t, calls[0], "2:user2", ascidentity.RoleAdmin, "user2/repo1")
}

func TestAscDepsHidesManagementFromNonAdministrators(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureDeps)
	f.depFindings = func(*ascv1.ListDepFindingsRequest) (*ascv1.ListDepFindingsResponse, error) {
		return &ascv1.ListDepFindingsResponse{Findings: []*ascv1.DepFinding{depsFinding(1, ascv1.DepSeverity_DEP_SEVERITY_LOW, ascv1.DepFindingState_DEP_FINDING_STATE_OPEN)}, LastScan: 1}, nil
	}
	f.depUpdates = func(*ascv1.ListDepUpdatesRequest) (*ascv1.ListDepUpdatesResponse, error) {
		return &ascv1.ListDepUpdatesResponse{}, nil
	}
	page := depsGet(t, "", 4, perm_model.AccessModeWrite)
	require.Equal(t, http.StatusOK, page.status)
	assert.NotContains(t, page.body, `data-testid="asc-deps-scan"`)
	assert.NotContains(t, page.body, "/dismiss")
	page = depsGet(t, "?view=updates", 4, perm_model.AccessModeWrite)
	assert.NotContains(t, page.body, `data-testid="asc-deps-propose-form"`)
}

func TestAscDepsNeverScannedAndUnsupported(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureDeps)
	f.depFindings = func(*ascv1.ListDepFindingsRequest) (*ascv1.ListDepFindingsResponse, error) {
		return &ascv1.ListDepFindingsResponse{}, nil
	}
	page := depsGet(t, "", 2, adminAccess)
	assert.Contains(t, page.body, `data-testid="asc-deps-never"`)
	assert.NotContains(t, page.body, `data-testid="asc-deps-empty"`, "an unscanned repository is not shown as clean")

	f2 := startFakeTrust(t)
	page = depsGet(t, "", 2, adminAccess)
	assert.Equal(t, http.StatusOK, page.status)
	assert.Contains(t, page.body, `data-testid="asc-deps-unsupported"`)
	assert.Empty(t, f2.callsOf("DepFindings"))
}

func TestAscDepsNeedsSignInAndCodeRead(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureDeps)
	assert.Equal(t, http.StatusForbidden, depsGet(t, "", 0, perm_model.AccessModeRead).status)
	assert.Equal(t, http.StatusNotFound, depsGet(t, "", 4, perm_model.AccessModeNone).status)
	assert.Equal(t, http.StatusNotFound, depsGet(t, "?view=other", 2, adminAccess).status)
	assert.Equal(t, http.StatusBadRequest, depsGet(t, "?state=bogus", 2, adminAccess).status)
	assert.Empty(t, f.callsOf("DepFindings"), "no finding is asked of asc")
	assert.Empty(t, f.callsOf("DepUpdates"))
}

func TestAscDepsUpdatesView(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureDeps)
	f.depUpdates = func(r *ascv1.ListDepUpdatesRequest) (*ascv1.ListDepUpdatesResponse, error) {
		return &ascv1.ListDepUpdatesResponse{
			Updates:  []*ascv1.DepUpdate{{Ecosystem: "npm", Name: "left-pad", Current: "1.0.0", Latest: "2.0.0", Target: "1.3.0", Kind: ascv1.DepUpdateKind_DEP_UPDATE_KIND_MINOR, Fixes: []string{"GHSA-aaaa"}, Unfixed: []string{"GHSA-bbbb"}}},
			Packages: 9, UnknownPackages: 3, LookupErrors: []string{"registry timed out"},
		}, nil
	}
	page := depsGet(t, "?view=updates&security=1", 2, adminAccess)
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.True(t, f.callsOf("DepUpdates")[0].Req.(*ascv1.ListDepUpdatesRequest).GetSecurityOnly())
	assert.Contains(t, page.body, `data-testid="asc-dep-update"`)
	assert.Contains(t, page.body, "asc_trust.deps.kind.minor")
	assert.Contains(t, page.body, "GHSA-bbbb", "advisories that still affect the target are listed")
	assert.Contains(t, page.body, `data-testid="asc-deps-unknown"`, "unknown packages are counted")
	assert.Contains(t, page.body, `data-testid="asc-deps-propose-form"`)
	assert.Contains(t, page.body, `value="preview"`)
	assert.Contains(t, page.body, `value="apply"`)
}

func TestAscDepsConfigView(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureDeps)
	f.depConfig = func(*ascv1.GetDepsConfigRequest) (*ascv1.GetDepsConfigResponse, error) {
		return &ascv1.GetDepsConfigResponse{
			Source: "file", Text: "schedule = \"0 6 * * 1\"\n", Supported: []string{"npm"}, Error: "unknown key",
			Config: &ascv1.DepsConfig{Schedule: "0 6 * * 1", SeverityThreshold: ascv1.DepSeverity_DEP_SEVERITY_HIGH, MaxKind: ascv1.DepUpdateKind_DEP_UPDATE_KIND_MINOR, MaxProposals: 3, Grouping: "none",
				Ignore: []*ascv1.DepIgnore{{Name: "left-*", Reason: "pinned"}}},
		}, nil
	}
	page := depsGet(t, "?view=config", 4, perm_model.AccessModeRead)
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `data-testid="asc-deps-config"`)
	assert.Contains(t, page.body, "0 6 * * 1")
	assert.Contains(t, page.body, `data-testid="asc-deps-config-error"`)
	assert.Contains(t, page.body, `data-testid="asc-deps-ignores"`)
	assert.Contains(t, page.body, "asc_trust.deps.severity.high")
}

func TestAscDepsScanIsForAdministrators(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureDeps)
	f.depScan = func(r *ascv1.ScanDepsRequest) (*ascv1.ScanDepsResponse, error) {
		return &ascv1.ScanDepsResponse{Packages: 12, Findings: []*ascv1.DepFinding{depsFinding(1, ascv1.DepSeverity_DEP_SEVERITY_LOW, ascv1.DepFindingState_DEP_FINDING_STATE_OPEN)}, Lookup: ascv1.DepLookupStatus_DEP_LOOKUP_STATUS_PARTIAL, UnknownPackages: 2}, nil
	}
	for name, tc := range map[string]struct {
		user   int64
		access perm_model.AccessMode
	}{"signed out": {0, perm_model.AccessModeAdmin}, "writer": {4, perm_model.AccessModeWrite}, "reader": {4, perm_model.AccessModeRead}} {
		assert.Equal(t, http.StatusForbidden, depsPost(t, "/scan", AscDepsScan, tc.user, tc.access, url.Values{}, nil).status, name)
	}
	assert.Empty(t, f.callsOf("DepScan"))

	page := depsPost(t, "/scan", AscDepsScan, 2, adminAccess, url.Values{"refresh": {"1"}}, nil)
	assert.Equal(t, http.StatusSeeOther, page.status)
	assert.Equal(t, "/user2/repo1/asc/deps", page.loc)
	calls := f.callsOf("DepScan")
	require.Len(t, calls, 1)
	assert.True(t, calls[0].Req.(*ascv1.ScanDepsRequest).GetRefresh())
	assertAs(t, calls[0], "2:user2", ascidentity.RoleAdmin, "user2/repo1")

	f.depScan = func(*ascv1.ScanDepsRequest) (*ascv1.ScanDepsResponse, error) {
		return nil, status.Error(codes.PermissionDenied, "not a maintainer, /srv/asc/x")
	}
	page = depsPost(t, "/scan", AscDepsScan, 2, adminAccess, url.Values{}, nil)
	assert.Equal(t, http.StatusForbidden, page.status, "asc decides again")
	assert.NotContains(t, page.body, "/srv/asc")
}

func TestAscDepsProposePreviewThenApply(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureDeps)
	f.depPropose = func(r *ascv1.ProposeDepUpdateRequest) (*ascv1.ProposeDepUpdateResponse, error) {
		return &ascv1.ProposeDepUpdateResponse{
			Proposals: []*ascv1.DepProposal{{Group: "all", Ref: "refs/heads/agent/deps/all", Change: make([]byte, 16), Updates: []*ascv1.DepUpdate{{Name: "left-pad", Current: "1.0.0", Target: "1.3.0", Kind: ascv1.DepUpdateKind_DEP_UPDATE_KIND_MINOR}}, Files: [][]byte{[]byte("package.json")}, Notes: []string{"run npm install"}}},
			Skipped:   []*ascv1.DepSkipped{{Ecosystem: "npm", Name: "x", Reason: "a major update"}},
		}, nil
	}
	page := depsPost(t, "/propose", AscDepsPropose, 2, adminAccess, url.Values{"mode": {"preview"}, "security": {"1"}, "max_kind": {"minor"}}, nil)
	require.Equal(t, http.StatusOK, page.status, page.body)
	call := f.callsOf("DepPropose")[0]
	assert.True(t, call.Req.(*ascv1.ProposeDepUpdateRequest).GetDryRun(), "a preview writes nothing")
	assert.Contains(t, page.body, `data-testid="asc-plan-proposal"`)
	assert.Contains(t, page.body, "refs/heads/agent/deps/all")
	assert.Contains(t, page.body, `data-testid="asc-plan-skipped"`)
	assert.Contains(t, page.body, `name="_csrf" value="csrf-token-for-tests"`)
	assert.Contains(t, page.body, `name="mode" value="apply"`)
	assert.Contains(t, page.body, `name="max_kind" value="minor"`, "the apply form repeats what was previewed")
	assert.Contains(t, page.body, `name="security" value="1"`)

	page = depsPost(t, "/propose", AscDepsPropose, 2, adminAccess, url.Values{"mode": {"apply"}, "op": {depsOp}, "max_kind": {"minor"}}, nil)
	assert.Equal(t, http.StatusSeeOther, page.status)
	assert.Equal(t, "/user2/repo1/asc/deps?view=updates", page.loc)
	apply := f.callsOf("DepPropose")[1]
	assert.False(t, apply.Req.(*ascv1.ProposeDepUpdateRequest).GetDryRun())
	assert.Equal(t, depsOp, apply.OpID, "the form's operation id makes a double submit one proposal")

	n := len(f.callsOf("DepPropose"))
	assert.Equal(t, http.StatusBadRequest, depsPost(t, "/propose", AscDepsPropose, 2, adminAccess, url.Values{"mode": {"other"}}, nil).status)
	assert.Equal(t, http.StatusSeeOther, depsPost(t, "/propose", AscDepsPropose, 2, adminAccess, url.Values{"mode": {"apply"}, "op": {"nope"}}, nil).status, "an apply without a valid operation id is refused locally")
	assert.Equal(t, http.StatusSeeOther, depsPost(t, "/propose", AscDepsPropose, 2, adminAccess, url.Values{"mode": {"preview"}, "max_kind": {"huge"}}, nil).status)
	assert.Len(t, f.callsOf("DepPropose"), n)
	assert.Equal(t, http.StatusForbidden, depsPost(t, "/propose", AscDepsPropose, 4, perm_model.AccessModeWrite, url.Values{"mode": {"apply"}, "op": {depsOp}}, nil).status)
	assert.Len(t, f.callsOf("DepPropose"), n)
}

func TestAscDepsDismissAndReopen(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureDeps)
	f.depDismiss = func(r *ascv1.DismissDepFindingRequest) (*ascv1.DismissDepFindingResponse, error) {
		state := ascv1.DepFindingState_DEP_FINDING_STATE_DISMISSED
		if r.GetReason() == ascv1.DepDismissReason_DEP_DISMISS_REASON_UNSPECIFIED {
			state = ascv1.DepFindingState_DEP_FINDING_STATE_OPEN
		}
		return &ascv1.DismissDepFindingResponse{Finding: depsFinding(r.GetId(), ascv1.DepSeverity_DEP_SEVERITY_LOW, state)}, nil
	}
	params := map[string]string{"id": "5"}
	page := depsPost(t, "/findings/5/dismiss", AscDepsDismiss, 2, adminAccess, url.Values{"reason": {"not_used"}, "note": {"never called"}, "op": {depsOp}}, params)
	assert.Equal(t, http.StatusSeeOther, page.status)
	assert.Equal(t, "/user2/repo1/asc/deps", page.loc)
	call := f.callsOf("DepDismiss")[0]
	req := call.Req.(*ascv1.DismissDepFindingRequest)
	assert.EqualValues(t, 5, req.GetId())
	assert.Equal(t, ascv1.DepDismissReason_DEP_DISMISS_REASON_NOT_USED, req.GetReason())
	assert.Equal(t, "never called", req.GetNote())
	assert.Equal(t, depsOp, call.OpID)

	page = depsPost(t, "/findings/5/dismiss", AscDepsDismiss, 2, adminAccess, url.Values{"reason": {"reopen"}, "state": {"dismissed"}, "op": {depsOp}}, params)
	assert.Equal(t, "/user2/repo1/asc/deps?state=dismissed", page.loc, "the tab the person came from is kept")
	assert.Equal(t, ascv1.DepDismissReason_DEP_DISMISS_REASON_UNSPECIFIED, f.callsOf("DepDismiss")[1].Req.(*ascv1.DismissDepFindingRequest).GetReason())

	n := len(f.callsOf("DepDismiss"))
	page = depsPost(t, "/findings/5/dismiss", AscDepsDismiss, 2, adminAccess, url.Values{"reason": {"whatever"}, "state": {"//evil.example"}, "op": {depsOp}}, params)
	assert.Equal(t, http.StatusSeeOther, page.status)
	assert.Equal(t, "/user2/repo1/asc/deps", page.loc, "the redirect target is built from fixed values only")
	assert.Equal(t, http.StatusNotFound, depsPost(t, "/findings/x/dismiss", AscDepsDismiss, 2, adminAccess, url.Values{"reason": {"tolerable"}, "op": {depsOp}}, map[string]string{"id": "x"}).status)
	assert.Equal(t, http.StatusForbidden, depsPost(t, "/findings/5/dismiss", AscDepsDismiss, 4, perm_model.AccessModeWrite, url.Values{"reason": {"tolerable"}, "op": {depsOp}}, params).status)
	assert.Len(t, f.callsOf("DepDismiss"), n, "a refused request makes no call")
}

func TestAscDepsPageHasNoStrayEnglish(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureDeps)
	f.depFindings = func(*ascv1.ListDepFindingsRequest) (*ascv1.ListDepFindingsResponse, error) {
		return &ascv1.ListDepFindingsResponse{Findings: []*ascv1.DepFinding{depsFinding(1, ascv1.DepSeverity_DEP_SEVERITY_LOW, ascv1.DepFindingState_DEP_FINDING_STATE_OPEN)}, LastScan: 1}, nil
	}
	page := depsGet(t, "", 2, adminAccess)
	body := page.body[strings.Index(page.body, `data-testid="asc-deps"`):]
	for _, word := range []string{">Dismiss<", ">Scan now<", ">Filter<", "Lowest severity"} {
		assert.NotContains(t, body, word)
	}
}
