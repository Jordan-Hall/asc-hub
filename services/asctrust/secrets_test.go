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

func TestMaskPreviewNeverShowsMoreThanTwoCharacters(t *testing.T) {
	cases := map[string]string{
		"AK***":                      "AK***",
		"AKIAIOSFODNN7EXAMPLE":       "AK***", // a server that sent the whole value
		"":                           "***",
		"a":                          "a***",
		"\x00\u202eAKIA***":          "AK***",
		"ghp_abcdefghijklmnopqrstuv": "gh***",
		"\u00e9\u00e8\u00ea":         "\u00e9\u00e8***",
		"**secret":                   "***",
	}
	for in, want := range cases {
		assert.Equal(t, want, MaskPreview(in), "%q", in)
	}
}

func TestSecretHitsOfReadsPlacesAndRulesOnly(t *testing.T) {
	hits := SecretHitsOf([]string{
		"config/prod.env:3 aws-access-key",
		"src/a b/c.rs:120 custom.my-rule",
		"nocolon aws-access-key",
		"x.rs:abc aws-access-key",
		"x.rs:7 Bad Rule",
		"x.rs:7",
		"x.rs: aws-access-key",
		"\u202e.env:1 private-key-block",
	})
	require.Len(t, hits, 3)
	assert.Equal(t, SecretHit{Path: "config/prod.env", Line: 3, Rule: "aws-access-key"}, hits[0])
	assert.Equal(t, SecretHit{Path: "src/a b/c.rs", Line: 120, Rule: "custom.my-rule"}, hits[1])
	assert.Equal(t, ".env", hits[2].Path, "a bidirectional control is removed from the path")
}

func TestSecretHitsAreBounded(t *testing.T) {
	var paths []string
	for i := 0; i < 500; i++ {
		paths = append(paths, "f.rs:1 aws-access-key")
	}
	assert.Len(t, SecretHitsOf(paths), 50)
}

func detailedError(t *testing.T, code codes.Code, detail *ascv1.ErrorDetail) error {
	t.Helper()
	st, err := status.New(code, "rejected").WithDetails(detail)
	require.NoError(t, err)
	return st.Err()
}

func TestSecretBlockTextNamesTheRuleAndPlaceNeverTheValue(t *testing.T) {
	err := detailedError(t, codes.FailedPrecondition, &ascv1.ErrorDetail{
		Code:  "SECRET_DETECTED",
		Paths: []string{"config/prod.env:3 aws-access-key", "src/main.rs:41 private-key-block"},
	})
	text := SecretBlockText(err)
	assert.Contains(t, text, "Push refused")
	assert.Contains(t, text, "config/prod.env:3 matched rule aws-access-key")
	assert.Contains(t, text, "src/main.rs:41 matched rule private-key-block")
	assert.Contains(t, text, "allow_paths")
	assert.NotContains(t, text, "AKIA")

	// Not a secret refusal, or not a status at all.
	other := detailedError(t, codes.FailedPrecondition, &ascv1.ErrorDetail{Code: "POLICY_DENIED"})
	assert.Empty(t, SecretBlockText(other))
	assert.Empty(t, SecretBlockText(assert.AnError))
	assert.Empty(t, SecretBlockText(nil))

	// A refusal with no usable places still says what to do.
	bare := detailedError(t, codes.FailedPrecondition, &ascv1.ErrorDetail{Code: "SECRET_DETECTED", Paths: []string{"garbage"}})
	assert.Contains(t, SecretBlockText(bare), "audit log")
}

func TestFindingsPageOf(t *testing.T) {
	change := make([]byte, 16)
	resp := &ascv1.ListSecretFindingsResponse{
		Findings: []*ascv1.SecretFinding{{
			Id: 41, RuleId: "aws-access-key", Severity: ascv1.SecretSeverity_SECRET_SEVERITY_CRITICAL,
			State: ascv1.SecretFindingState_SECRET_FINDING_STATE_OPEN, Path: []byte("config/prod.env"), Line: 3,
			Preview: "AKIAIOSFODNN7EXAMPLE", Change: change, FirstSeen: 1700000000, LastSeen: 1700000500,
			Note: "rotated\nsoon\u202e", Fingerprint: "deadbeef",
		}},
		NextPageToken: "tok_2",
		CountsByState: map[string]uint32{"open": 2, "false_positive": 5},
	}
	page, err := FindingsPageOf(resp)
	require.NoError(t, err)
	require.Len(t, page.Rows, 1)
	r := page.Rows[0]
	assert.Equal(t, "config/prod.env:3", r.Location)
	assert.Equal(t, "AK***", r.Preview, "at most two characters")
	assert.Equal(t, "Critical", r.Severity)
	assert.Equal(t, "no", r.Tone)
	assert.Equal(t, "open", r.StateKey)
	assert.Equal(t, "zzzzzzzzzzzz", r.Change)
	assert.Equal(t, "rotated soon", r.Note)
	assert.Equal(t, "tok_2", page.Next)
	require.Len(t, page.Counts, 4)
	assert.Equal(t, StateCount{Key: "open", Label: "Open", Count: 2}, page.Counts[0])
	assert.EqualValues(t, 5, page.Counts[2].Count)
	assert.NotContains(t, strings.Join([]string{r.Preview, r.Location, r.Note}, "|"), "FODNN7", "no row field holds the value")
}

func TestFindingsPageOfRefusesBadAnswers(t *testing.T) {
	bad := []*ascv1.SecretFinding{
		{Id: 0, RuleId: "aws-access-key"},
		{Id: 1, RuleId: "Bad Rule"},
		{Id: 1, RuleId: ""},
	}
	for _, f := range bad {
		_, err := FindingsPageOf(&ascv1.ListSecretFindingsResponse{Findings: []*ascv1.SecretFinding{f}})
		assert.ErrorIs(t, err, ErrBadAnswer)
	}
	_, err := FindingsPageOf(&ascv1.ListSecretFindingsResponse{NextPageToken: "no good"})
	assert.ErrorIs(t, err, ErrBadAnswer)
	many := make([]*ascv1.SecretFinding, maxFindingsPage+1)
	for i := range many {
		many[i] = &ascv1.SecretFinding{Id: uint64(i + 1), RuleId: "aws-access-key"}
	}
	_, err = FindingsPageOf(&ascv1.ListSecretFindingsResponse{Findings: many})
	assert.ErrorIs(t, err, ErrBadAnswer)
}

func TestSecretRulesOf(t *testing.T) {
	v, err := SecretRulesOf(&ascv1.ListSecretRulesResponse{
		Rules: []*ascv1.SecretRule{
			{Id: "aws-access-key", Name: "Cloud access key", Severity: ascv1.SecretSeverity_SECRET_SEVERITY_HIGH, Builtin: true, Enabled: true, Detector: "prefix"},
			{Id: "custom.internal-token", Name: "Internal token", Severity: ascv1.SecretSeverity_SECRET_SEVERITY_MEDIUM, Enabled: true, Detector: "regex"},
		},
		BlockOnPush: true, AllowPaths: []string{"tests/fixtures/**"},
	})
	require.NoError(t, err)
	assert.True(t, v.BlockOnPush)
	assert.Equal(t, []string{"tests/fixtures/**"}, v.AllowPaths)
	require.Len(t, v.Rules, 2)
	assert.Equal(t, "High", v.Rules[0].Severity)
	assert.False(t, v.Rules[1].Builtin)

	_, err = SecretRulesOf(&ascv1.ListSecretRulesResponse{Rules: []*ascv1.SecretRule{{Id: "Not A Rule"}}})
	assert.ErrorIs(t, err, ErrBadAnswer)
}

func TestSecretStateAndNoteChecks(t *testing.T) {
	for _, k := range []string{"open", "revoked", "false_positive", "accepted_risk"} {
		_, ok := SecretStateOf(k)
		assert.True(t, ok, k)
	}
	_, ok := SecretStateOf("deleted")
	assert.False(t, ok)

	n, err := CheckNote("  rotated at the issuer  ")
	require.NoError(t, err)
	assert.Equal(t, "rotated at the issuer", n)
	_, err = CheckNote(strings.Repeat("x", 1025))
	assert.ErrorIs(t, err, ErrBadInput)
	_, err = CheckNote("a\x07b")
	assert.ErrorIs(t, err, ErrBadInput)
	_, err = CheckNote("\xff\xfe")
	assert.ErrorIs(t, err, ErrBadInput)
	n, err = CheckNote("line one\nline two")
	require.NoError(t, err)
	assert.Contains(t, n, "\n", "a note may have lines")
}

func TestLoadSecretsAndUpdateAndScan(t *testing.T) {
	f, c := startFake(t)
	f.listRules = func(*ascv1.ListSecretRulesRequest) (*ascv1.ListSecretRulesResponse, error) {
		return &ascv1.ListSecretRulesResponse{
			Rules:       []*ascv1.SecretRule{{Id: "aws-access-key", Name: "Cloud key", Builtin: true, Enabled: true}},
			BlockOnPush: true,
		}, nil
	}
	f.findings = func(req *ascv1.ListSecretFindingsRequest) (*ascv1.ListSecretFindingsResponse, error) {
		return &ascv1.ListSecretFindingsResponse{
			Findings:      []*ascv1.SecretFinding{{Id: 9, RuleId: "aws-access-key", State: req.GetStates()[0], Path: []byte("a.env"), Line: 1, Preview: "AKIAxx"}},
			CountsByState: map[string]uint32{"open": 1},
		}, nil
	}
	f.updateFind = func(req *ascv1.UpdateSecretFindingRequest) (*ascv1.UpdateSecretFindingResponse, error) {
		return &ascv1.UpdateSecretFindingResponse{Finding: &ascv1.SecretFinding{Id: req.GetId(), RuleId: "aws-access-key", State: req.GetState(), Note: req.GetNote()}}, nil
	}
	f.scan = func(req *ascv1.ScanSecretsRequest) (*ascv1.ScanSecretsResponse, error) {
		return &ascv1.ScanSecretsResponse{Findings: []*ascv1.SecretFinding{{Id: 1}, {Id: 2}}, FilesScanned: 40, Truncated: true}, nil
	}
	ctx := userCtx(t, "tok-admin")

	page, err := LoadSecrets(ctx, c, "acme/payments", SecretsQuery{State: "false_positive", PageToken: "p_2"})
	require.NoError(t, err)
	assert.Equal(t, "AK***", page.Findings.Rows[0].Preview)
	req := f.last(t, "ListFindings").Req.(*ascv1.ListSecretFindingsRequest)
	assert.Equal(t, []ascv1.SecretFindingState{ascv1.SecretFindingState_SECRET_FINDING_STATE_FALSE_POSITIVE}, req.GetStates())
	assert.Equal(t, "p_2", req.GetPageToken())
	assert.Equal(t, "Bearer tok-admin", f.last(t, "ListFindings").Auth)

	_, err = LoadSecrets(ctx, c, "acme/payments", SecretsQuery{State: "bogus"})
	assert.ErrorIs(t, err, ErrBadInput)
	_, err = LoadSecrets(ctx, c, "acme/payments", SecretsQuery{PageToken: "no good"})
	assert.ErrorIs(t, err, ErrBadInput)

	op := NewOpID()
	row, err := UpdateFinding(ctx, c, "acme/payments", 9, "false_positive", "test fixture", op)
	require.NoError(t, err)
	assert.Equal(t, "false_positive", row.StateKey)
	call := f.last(t, "UpdateFinding")
	assert.Equal(t, op, call.OpID, "the op id of the form is the op id of the call")
	assert.Equal(t, "test fixture", call.Req.(*ascv1.UpdateSecretFindingRequest).GetNote())

	for _, bad := range []struct {
		id    uint64
		state string
		note  string
		op    string
	}{
		{0, "open", "", op}, {9, "deleted", "", op}, {9, "open", strings.Repeat("n", 2000), op}, {9, "open", "", "not-a-uuid"},
	} {
		_, err = UpdateFinding(ctx, c, "acme/payments", bad.id, bad.state, bad.note, bad.op)
		assert.ErrorIs(t, err, ErrBadInput, "%+v", bad)
	}
	assert.Equal(t, 1, f.count("UpdateFinding"), "a bad input never reaches the server")

	sum, err := ScanRev(ctx, c, "acme/payments", "refs/heads/main")
	require.NoError(t, err)
	assert.Equal(t, &ScanSummary{Findings: 2, FilesScanned: 40, Truncated: true}, sum)
	assert.Equal(t, "refs/heads/main", f.last(t, "Scan").Req.(*ascv1.ScanSecretsRequest).GetRev())
	_, err = ScanRev(ctx, c, "acme/payments", "refs/heads/a\nb")
	assert.ErrorIs(t, err, ErrBadInput)
}
