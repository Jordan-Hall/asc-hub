// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func depFinding(id uint64, sev ascv1.DepSeverity) *ascv1.DepFinding {
	return &ascv1.DepFinding{
		Id: id, Ecosystem: "crates.io", Name: "time", Version: "0.1.0", Direct: true, Path: []byte("Cargo.lock"),
		Advisory: &ascv1.DepAdvisory{Id: "RUSTSEC-2020-0071", Aliases: []string{"CVE-2020-26235"}, Summary: "Potential segfault", Severity: sev, Cvss: 6.2, FixedVersions: []string{"0.2.23"}, Url: "https://rustsec.org/advisories/RUSTSEC-2020-0071.html"},
		State:    ascv1.DepFindingState_DEP_FINDING_STATE_OPEN, FixedIn: "0.2.23", FirstSeen: 1_700_000_000, LastSeen: 1_700_086_400,
	}
}

func TestLoadDepFindingsCleansWhatThePublicSourceSends(t *testing.T) {
	f, c := startFake(t)
	f.depFindings = func(r *ascv1.ListDepFindingsRequest) (*ascv1.ListDepFindingsResponse, error) {
		bad := depFinding(2, ascv1.DepSeverity_DEP_SEVERITY_UNSPECIFIED)
		bad.Advisory.Url = "javascript:alert(1)"
		bad.Advisory.Summary = "evil\x1b[31m‮text"
		cred := depFinding(3, ascv1.DepSeverity_DEP_SEVERITY_HIGH)
		cred.Advisory.Url = "https://user:pw@example.org/a"
		return &ascv1.ListDepFindingsResponse{
			Findings:      []*ascv1.DepFinding{depFinding(1, ascv1.DepSeverity_DEP_SEVERITY_CRITICAL), bad, cred},
			NextPageToken: "n_2", CountsByState: map[string]uint32{"open": 3, "dismissed": 1}, LastScan: 1_700_000_000,
			LastLookup: ascv1.DepLookupStatus_DEP_LOOKUP_STATUS_PARTIAL,
		}, nil
	}
	page, err := LoadDepFindings(userCtx(t, "tok"), c, "o/r", DepFindingsQuery{Severity: "moderate", Ecosystem: "crates.io", PageToken: "n_1"})
	require.NoError(t, err)

	req := f.last(t, "DepFindings").Req.(*ascv1.ListDepFindingsRequest)
	assert.Equal(t, []ascv1.DepFindingState{ascv1.DepFindingState_DEP_FINDING_STATE_OPEN}, req.GetStates(), "open by default")
	assert.Equal(t, ascv1.DepSeverity_DEP_SEVERITY_MODERATE, req.GetMinSeverity())
	assert.Equal(t, []string{"crates.io"}, req.GetEcosystems())
	assert.Equal(t, "n_1", req.GetPageToken())

	require.Len(t, page.Rows, 3)
	assert.Equal(t, "critical", page.Rows[0].SeverityKey)
	assert.Equal(t, "no", page.Rows[0].Tone)
	assert.Equal(t, "6.2", page.Rows[0].CVSS)
	assert.Equal(t, "https://rustsec.org/advisories/RUSTSEC-2020-0071.html", page.Rows[0].URL)
	assert.Equal(t, "Cargo.lock", page.Rows[0].Path)
	assert.Equal(t, "unknown", page.Rows[1].SeverityKey)
	assert.Empty(t, page.Rows[1].URL, "a link that is not https is not drawn")
	assert.Equal(t, "evil[31mtext", page.Rows[1].Summary, "control and bidirectional characters are removed")
	assert.Empty(t, page.Rows[2].URL, "a link with credentials is not drawn")
	assert.Equal(t, "n_2", page.Next)
	assert.EqualValues(t, 3, page.Counts["open"])
	assert.EqualValues(t, 0, page.Counts["fixed"])
	assert.Equal(t, "partial", page.LookupKey)
	assert.False(t, page.NeverScanned)
}

func TestLoadDepFindingsRefusesBadInputAndBadAnswers(t *testing.T) {
	f, c := startFake(t)
	for name, q := range map[string]DepFindingsQuery{
		"state": {State: "bogus"}, "severity": {Severity: "severe"}, "ecosystem": {Ecosystem: "a b"}, "page": {PageToken: "no way!"},
	} {
		_, err := LoadDepFindings(userCtx(t, "tok"), c, "o/r", q)
		assert.ErrorIs(t, err, ErrBadInput, name)
	}
	assert.Zero(t, f.count("DepFindings"), "nothing is sent")

	for name, mutate := range map[string]func(*ascv1.DepFinding){
		"no id":       func(d *ascv1.DepFinding) { d.Id = 0 },
		"no advisory": func(d *ascv1.DepFinding) { d.Advisory = nil },
		"cvss":        func(d *ascv1.DepFinding) { d.Advisory.Cvss = 11 },
	} {
		d := depFinding(1, ascv1.DepSeverity_DEP_SEVERITY_LOW)
		mutate(d)
		f.depFindings = func(*ascv1.ListDepFindingsRequest) (*ascv1.ListDepFindingsResponse, error) {
			return &ascv1.ListDepFindingsResponse{Findings: []*ascv1.DepFinding{d}}, nil
		}
		_, err := LoadDepFindings(userCtx(t, "tok"), c, "o/r", DepFindingsQuery{})
		assert.ErrorIs(t, err, ErrBadAnswer, name)
	}

	f.depFindings = func(*ascv1.ListDepFindingsRequest) (*ascv1.ListDepFindingsResponse, error) {
		return &ascv1.ListDepFindingsResponse{}, nil
	}
	page, err := LoadDepFindings(userCtx(t, "tok"), c, "o/r", DepFindingsQuery{State: "dismissed"})
	require.NoError(t, err)
	assert.True(t, page.NeverScanned, "no scan time means it never ran, and the page says so")
}

func TestLoadDepUpdatesAndConfig(t *testing.T) {
	f, c := startFake(t)
	f.depUpdates = func(r *ascv1.ListDepUpdatesRequest) (*ascv1.ListDepUpdatesResponse, error) {
		return &ascv1.ListDepUpdatesResponse{
			Updates:  []*ascv1.DepUpdate{{Ecosystem: "npm", Name: "left-pad", Current: "1.0.0", Latest: "2.0.0", Target: "1.3.0", Kind: ascv1.DepUpdateKind_DEP_UPDATE_KIND_MINOR, Fixes: []string{"GHSA-1"}, Unfixed: []string{"GHSA-2"}}},
			Packages: 10, UnknownPackages: 2, LookupErrors: []string{"registry timed out"},
		}, nil
	}
	up, err := LoadDepUpdates(userCtx(t, "tok"), c, "o/r", true)
	require.NoError(t, err)
	assert.True(t, f.last(t, "DepUpdates").Req.(*ascv1.ListDepUpdatesRequest).GetSecurityOnly())
	require.Len(t, up.Rows, 1)
	assert.Equal(t, "minor", up.Rows[0].KindKey)
	assert.Equal(t, "info", up.Rows[0].KindTone)
	assert.Equal(t, []string{"GHSA-2"}, up.Rows[0].Unfixed)
	assert.EqualValues(t, 2, up.Unknown, "an unknown package is counted, never reported clean")

	f.depConfig = func(*ascv1.GetDepsConfigRequest) (*ascv1.GetDepsConfigResponse, error) {
		return &ascv1.GetDepsConfigResponse{
			Source: "file", Text: "schedule = \"0 6 * * 1\"\n", Supported: []string{"npm", "crates.io"},
			Config: &ascv1.DepsConfig{
				Schedule: "0 6 * * 1", SeverityThreshold: ascv1.DepSeverity_DEP_SEVERITY_HIGH, MaxKind: ascv1.DepUpdateKind_DEP_UPDATE_KIND_MINOR, MaxProposals: 5, Grouping: "ecosystem",
				Groups: []*ascv1.DepGroup{{Name: "lint", Patterns: []string{"eslint*"}, Kinds: []ascv1.DepUpdateKind{ascv1.DepUpdateKind_DEP_UPDATE_KIND_PATCH}}},
				Ignore: []*ascv1.DepIgnore{{Name: "left-*", Until: "2027-01-01", Reason: "pinned on purpose", Kinds: []ascv1.DepUpdateKind{ascv1.DepUpdateKind_DEP_UPDATE_KIND_MAJOR}}},
			},
		}, nil
	}
	cfg, err := LoadDepsConfig(userCtx(t, "tok"), c, "o/r")
	require.NoError(t, err)
	assert.Equal(t, "file", cfg.Source)
	assert.Equal(t, "high", cfg.SeverityKey)
	assert.Equal(t, "minor", cfg.MaxKindKey)
	assert.Equal(t, []string{"patch"}, cfg.Groups[0].Kinds)
	assert.Equal(t, []string{"major"}, cfg.Ignores[0].Kinds)
	assert.Equal(t, []string{"npm", "crates.io"}, cfg.Supported)

	f.depConfig = func(*ascv1.GetDepsConfigRequest) (*ascv1.GetDepsConfigResponse, error) {
		return &ascv1.GetDepsConfigResponse{Source: "remote"}, nil
	}
	_, err = LoadDepsConfig(userCtx(t, "tok"), c, "o/r")
	assert.ErrorIs(t, err, ErrBadAnswer)
}

func TestScanProposeAndDismiss(t *testing.T) {
	f, c := startFake(t)
	f.depScan = func(r *ascv1.ScanDepsRequest) (*ascv1.ScanDepsResponse, error) {
		return &ascv1.ScanDepsResponse{
			Files:    []*ascv1.DepFileResult{{Path: []byte("Cargo.lock"), Format: "Cargo.lock", Ecosystem: "crates.io", Packages: 40, Direct: 5}, {Path: []byte("package-lock.json"), Error: "unsupported lockfileVersion"}},
			Packages: 40, Findings: []*ascv1.DepFinding{depFinding(1, ascv1.DepSeverity_DEP_SEVERITY_LOW)}, Lookup: ascv1.DepLookupStatus_DEP_LOOKUP_STATUS_FAILED, UnknownPackages: 40, LookupErrors: []string{"osv unreachable"}, Source: "osv",
		}, nil
	}
	scan, err := ScanDeps(userCtx(t, "tok"), c, "o/r", true)
	require.NoError(t, err)
	assert.True(t, f.last(t, "DepScan").Req.(*ascv1.ScanDepsRequest).GetRefresh())
	assert.Equal(t, "failed", scan.LookupKey, "a failed lookup is reported as failed and never as clean")
	assert.EqualValues(t, 40, scan.Unknown)
	assert.Equal(t, "unsupported lockfileVersion", scan.Files[1].Error)

	change := make([]byte, 16)
	f.depPropose = func(r *ascv1.ProposeDepUpdateRequest) (*ascv1.ProposeDepUpdateResponse, error) {
		return &ascv1.ProposeDepUpdateResponse{
			Proposals: []*ascv1.DepProposal{{Group: "all", Ref: "refs/heads/agent/deps/all", Change: change, Updates: []*ascv1.DepUpdate{{Name: "time", Current: "0.1.0", Target: "0.2.23"}}, Files: [][]byte{[]byte("Cargo.toml")}, Notes: []string{"run cargo update"}}},
			Skipped:   []*ascv1.DepSkipped{{Ecosystem: "npm", Name: "x", Reason: "major update"}},
		}, nil
	}
	const op = "0190b7f5-8d5e-7000-8000-000000000001"
	plan, err := ProposeDeps(userCtx(t, "tok"), c, "o/r", DepProposeQuery{SecurityOnly: true, MaxKind: "minor", Op: op})
	require.NoError(t, err)
	call := f.last(t, "DepPropose")
	assert.Equal(t, op, call.OpID, "an apply carries its operation id")
	assert.Equal(t, ascv1.DepUpdateKind_DEP_UPDATE_KIND_MINOR, call.Req.(*ascv1.ProposeDepUpdateRequest).GetMaxKind())
	assert.False(t, call.Req.(*ascv1.ProposeDepUpdateRequest).GetDryRun())
	require.Len(t, plan.Proposals, 1)
	assert.Equal(t, "refs/heads/agent/deps/all", plan.Proposals[0].Ref)
	assert.Equal(t, []string{"Cargo.toml"}, plan.Proposals[0].Files)
	assert.Equal(t, "major update", plan.Skipped[0].Reason)

	n := f.count("DepPropose")
	_, err = ProposeDeps(userCtx(t, "tok"), c, "o/r", DepProposeQuery{Op: "nope"})
	assert.ErrorIs(t, err, ErrBadInput, "an apply needs an operation id")
	_, err = ProposeDeps(userCtx(t, "tok"), c, "o/r", DepProposeQuery{DryRun: true, MaxKind: "huge"})
	assert.ErrorIs(t, err, ErrBadInput)
	assert.Equal(t, n, f.count("DepPropose"))
	_, err = ProposeDeps(userCtx(t, "tok"), c, "o/r", DepProposeQuery{DryRun: true})
	require.NoError(t, err, "a preview needs no operation id")
	assert.True(t, f.last(t, "DepPropose").Req.(*ascv1.ProposeDepUpdateRequest).GetDryRun())

	f.depPropose = func(*ascv1.ProposeDepUpdateRequest) (*ascv1.ProposeDepUpdateResponse, error) {
		return &ascv1.ProposeDepUpdateResponse{Proposals: []*ascv1.DepProposal{{Group: "x", Ref: "refs/heads/main"}}}, nil
	}
	_, err = ProposeDeps(userCtx(t, "tok"), c, "o/r", DepProposeQuery{Op: op})
	assert.ErrorIs(t, err, ErrBadAnswer, "a proposal must stay off trunk")

	f.depDismiss = func(r *ascv1.DismissDepFindingRequest) (*ascv1.DismissDepFindingResponse, error) {
		return &ascv1.DismissDepFindingResponse{Finding: depFinding(1, ascv1.DepSeverity_DEP_SEVERITY_LOW)}, nil
	}
	row, err := DismissDep(userCtx(t, "tok"), c, "o/r", 7, "not_used", "not reachable", op)
	require.Error(t, err, "the answer is for finding 1, not 7")
	f.depDismiss = func(r *ascv1.DismissDepFindingRequest) (*ascv1.DismissDepFindingResponse, error) {
		d := depFinding(r.GetId(), ascv1.DepSeverity_DEP_SEVERITY_LOW)
		d.State, d.DismissReason, d.DismissedBy = ascv1.DepFindingState_DEP_FINDING_STATE_DISMISSED, r.GetReason(), "alice"
		return &ascv1.DismissDepFindingResponse{Finding: d}, nil
	}
	row, err = DismissDep(userCtx(t, "tok"), c, "o/r", 1, "not_used", "not reachable", op)
	require.NoError(t, err)
	assert.Equal(t, "dismissed", row.StateKey)
	assert.Equal(t, "not_used", row.ReasonKey)
	assert.Equal(t, ascv1.DepDismissReason_DEP_DISMISS_REASON_NOT_USED, f.last(t, "DepDismiss").Req.(*ascv1.DismissDepFindingRequest).GetReason())

	_, err = DismissDep(userCtx(t, "tok"), c, "o/r", 1, "reopen", "", op)
	require.NoError(t, err)
	assert.Equal(t, ascv1.DepDismissReason_DEP_DISMISS_REASON_UNSPECIFIED, f.last(t, "DepDismiss").Req.(*ascv1.DismissDepFindingRequest).GetReason(), "reopen sends no reason")

	n = f.count("DepDismiss")
	for name, args := range map[string][4]string{
		"reason": {"1", "whatever", "", op}, "note": {"1", "tolerable", strings.Repeat("a", 2000), op}, "op": {"1", "tolerable", "", "x"}, "id": {"0", "tolerable", "", op},
	} {
		id := uint64(1)
		if args[0] == "0" {
			id = 0
		}
		_, err := DismissDep(userCtx(t, "tok"), c, "o/r", id, args[1], args[2], args[3])
		assert.ErrorIs(t, err, ErrBadInput, name)
	}
	assert.Equal(t, n, f.count("DepDismiss"))

	f.depDismiss = func(*ascv1.DismissDepFindingRequest) (*ascv1.DismissDepFindingResponse, error) {
		return nil, status.Error(codes.PermissionDenied, "not a maintainer")
	}
	_, err = DismissDep(userCtx(t, "tok"), c, "o/r", 1, "tolerable", "", op)
	assert.Equal(t, KindDenied, Classify(err).Kind)
}
