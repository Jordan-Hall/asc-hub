// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const boardPolicyText = `version = 1

[roles]
maintainer = ["user:priya", "user:marcus"]

[trunk]
require_approvals = 1
require_checks = ["unit", "lint"]
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

// boardPolicy is the policy of the Policy board.
func boardPolicy() *ascv1.GetPolicyResponse {
	return &ascv1.GetPolicyResponse{
		Present:       true,
		Maintainer:    true,
		Roles:         []*ascv1.PolicyRole{{Name: "maintainer", Members: []string{"user:priya", "user:marcus"}}},
		Trunk:         &ascv1.PolicyTrunk{Branch: "main", RequireApprovals: 1, RequireChecks: []string{"unit", "lint"}},
		Paths:         []*ascv1.PolicyPathRule{{Glob: "infra/**", Write: []string{"role:maintainer"}}},
		Agents:        &ascv1.PolicyAgents{DefaultScope: []string{"src/**", "tests/**"}, MaxChangesPerRun: 8},
		MergeStrategy: "rebase",
	}
}

func TestMeaningOfTheBoardPolicy(t *testing.T) {
	got := Meaning(boardPolicy(), boardPolicyText, nil, nil, true)
	joined := strings.Join(got, "\n")
	assert.Contains(t, joined, "Trunk (main) needs 1 approval and green unit and lint checks.")
	assert.Contains(t, joined, "Nobody can force-push trunk.")
	assert.Contains(t, joined, "Every change on trunk must be signed.")
	assert.Contains(t, joined, "Only maintainers can touch infra/.")
	assert.Contains(t, joined, "Agents can write src/ and tests/, up to 8 changes a run.")
	assert.Contains(t, joined, "rebased onto trunk")
	assert.Contains(t, joined, "A push that contains a secret is refused", "scanning blocks by default")
}

func TestMeaningPrefersWhatAscAnswered(t *testing.T) {
	land := &ascv1.PolicyLandRequirements{Approvals: 2, Checks: []string{"build"}, Signed: false, MergeStrategy: "fast-forward"}
	secrets := &ascv1.ListSecretRulesResponse{BlockOnPush: false, AllowPaths: []string{"tests/fixtures/**"}}
	got := strings.Join(Meaning(boardPolicy(), boardPolicyText, land, secrets, true), "\n")
	assert.Contains(t, got, "needs 2 approvals and green build check.")
	assert.NotContains(t, got, "must be signed", "the land requirements say it is not")
	assert.Contains(t, got, "already sit on top of trunk")
	assert.Contains(t, got, "is let through")
	assert.Contains(t, got, "skips tests/fixtures/.")
}

func TestMeaningWithoutAPolicyFile(t *testing.T) {
	got := Meaning(nil, "", nil, nil, false)
	require.Len(t, got, 1)
	assert.Contains(t, got[0], "no policy file")
}

func TestMeaningOfVariousRules(t *testing.T) {
	p := &ascv1.GetPolicyResponse{
		Present: true,
		Trunk:   &ascv1.PolicyTrunk{AllowForce: true},
		Paths: []*ascv1.PolicyPathRule{
			{Glob: "**", Write: []string{"*"}},
			{Glob: "secrets/**", Write: nil},
			{Glob: "ops/**", Write: []string{"role:sre", "user:alice", "agent:*", "deploy:*", "ci:build", "user:*", "host:x"}},
		},
		Agents: &ascv1.PolicyAgents{DeniedPaths: []string{".github/**"}, MaxPathsPerRun: 50},
	}
	text := "[agents]\nenabled = false\n[agents.budgets]\nmax_tokens = 1000\nmax_cost_micro_usd = 2_500_000\n[ci]\nallow_quarantine = true\n[secrets]\nenabled = false\n"
	got := strings.Join(Meaning(p, text, nil, nil, true), "\n")
	assert.Contains(t, got, "Trunk (trunk) needs no approvals and no checks.")
	assert.Contains(t, got, "can be force-pushed")
	assert.Contains(t, got, "Anyone who can write to the repository can touch everything.")
	assert.Contains(t, got, "Nobody can touch secrets/.")
	assert.Contains(t, got, "Only the sre role, alice, agents, deploy keys, CI build, people and the host can touch ops/.")
	assert.Contains(t, got, "A run can touch up to 50 paths.")
	assert.Contains(t, got, "Agents never write .github/.")
	assert.Contains(t, got, "Agents are switched off")
	assert.Contains(t, got, "up to 1000 tokens and $2.50.")
	assert.Contains(t, got, "quarantine a flaky test")
	assert.Contains(t, got, "Secret scanning is switched off.")
}

func TestMeaningCleansTextFromTheServer(t *testing.T) {
	p := &ascv1.GetPolicyResponse{
		Present: true,
		Trunk:   &ascv1.PolicyTrunk{Branch: "ma\u202ein\x00", RequireChecks: []string{"u\u200bnit\n"}},
	}
	got := strings.Join(Meaning(p, "", nil, nil, true), "\n")
	assert.Contains(t, got, "Trunk (main) needs green unit check.")
	assert.NotContains(t, got, "\u202e")
	assert.NotContains(t, got, "\x00")
}

func TestBuildPolicyViewTheWhoCanWriteTable(t *testing.T) {
	v := BuildPolicyView(PolicyInput{Source: &ascv1.GetPolicySourceResponse{
		Present: true, Text: []byte(boardPolicyText), Source: make([]byte, 32), Parsed: boardPolicy(),
	}})
	require.True(t, v.Usable)
	assert.Equal(t, "000000000000", v.Source)
	assert.True(t, v.Maintainer)
	assert.Equal(t, []string{"Trunk", "infra/", "Other paths"}, v.Table.Columns)

	rows := map[string]WhoRow{}
	for _, r := range v.Table.Rows {
		rows[r.Who] = r
		assert.Len(t, r.Cells, len(v.Table.Columns), r.Who)
	}
	m := rows["maintainer"]
	assert.Equal(t, "priya, marcus", m.Detail)
	assert.Equal(t, "Via a reviewed change", m.Cells[0].Text)
	assert.Equal(t, "Yes", m.Cells[1].Text)
	assert.Equal(t, "Yes", m.Cells[2].Text)

	other := rows["Other people with write access"]
	assert.Equal(t, "Via a reviewed change", other.Cells[0].Text)
	assert.Equal(t, "No", other.Cells[1].Text, "infra/ is for maintainers")
	assert.Contains(t, other.Cells[1].Title, "infra/**", "the reason is in the title")
	assert.Equal(t, "Yes", other.Cells[2].Text)

	agents := rows["Agents with a run token"]
	assert.Equal(t, "Never directly", agents.Cells[0].Text, "an agent never pushes trunk")
	assert.Equal(t, "never", agents.Cells[0].Tone)
	assert.Equal(t, "No", agents.Cells[1].Text)
	assert.Equal(t, "No", agents.Cells[2].Text, "outside the agent scope")

	everyone := rows["Everyone else"]
	assert.Equal(t, "No", everyone.Cells[0].Text)
	assert.Equal(t, "Read only", everyone.Cells[1].Text)
}

func TestWhoTableAgentsInScopeAreBudgeted(t *testing.T) {
	p := boardPolicy()
	p.Paths = append(p.Paths, &ascv1.PolicyPathRule{Glob: "src/**", Write: []string{"*"}})
	v := BuildPolicyView(PolicyInput{Source: &ascv1.GetPolicySourceResponse{Present: true, Text: []byte(boardPolicyText), Parsed: p}})
	var agents WhoRow
	for _, r := range v.Table.Rows {
		if r.Who == "Agents with a run token" {
			agents = r
		}
	}
	require.Equal(t, v.Table.Columns, []string{"Trunk", "infra/", "src/", "Other paths"})
	assert.Equal(t, "Yes, budgeted", agents.Cells[2].Text)
}

func TestWhoTableShowsTheCallersOwnAnswer(t *testing.T) {
	parsed := boardPolicy()
	probes := ProbePaths(parsed)
	assert.Equal(t, []string{"infra/x", otherPathProbe}, probes)
	you := &ascv1.EvaluatePolicyResponse{Decisions: []*ascv1.PolicyDecision{
		{Path: []byte("infra/x"), Allowed: false, Reason: "limited to role:maintainer"},
		{Path: []byte(otherPathProbe), Allowed: true},
	}}
	v := BuildPolicyView(PolicyInput{
		Source: &ascv1.GetPolicySourceResponse{Present: true, Text: []byte(boardPolicyText), Parsed: parsed},
		You:    you, YouPaths: probes,
	})
	var row WhoRow
	for _, r := range v.Table.Rows {
		if r.Who == "You" {
			row = r
		}
	}
	require.Equal(t, "You", row.Who)
	assert.Equal(t, "Via a reviewed change", row.Cells[0].Text)
	assert.Equal(t, "No", row.Cells[1].Text)
	assert.Equal(t, "limited to role:maintainer", row.Cells[1].Title)
	assert.Equal(t, "Yes", row.Cells[2].Text)

	// An answer that does not fit the table is not drawn.
	v = BuildPolicyView(PolicyInput{
		Source: &ascv1.GetPolicySourceResponse{Present: true, Text: []byte(boardPolicyText), Parsed: parsed},
		You:    you, YouPaths: probes[:1],
	})
	for _, r := range v.Table.Rows {
		assert.NotEqual(t, "You", r.Who)
	}
}

func TestBuildPolicyViewOfAnUnusablePolicy(t *testing.T) {
	v := BuildPolicyView(PolicyInput{Source: &ascv1.GetPolicySourceResponse{
		Present: true, Text: []byte("garbage ["), Parsed: &ascv1.GetPolicyResponse{Present: true, Error: "line 1: expected a key at position 3 (internal detail)"},
	}})
	assert.True(t, v.Present)
	assert.False(t, v.Usable)
	assert.Contains(t, v.Error, "cannot be used")
	assert.NotContains(t, v.Error, "internal detail", "the parser's text is not shown")
	assert.Empty(t, v.Table.Rows)
	assert.Equal(t, "garbage [", v.Text, "the editor still gets the text, so it can be repaired")
}

func TestBuildPolicyViewWithoutAFile(t *testing.T) {
	v := BuildPolicyView(PolicyInput{Source: &ascv1.GetPolicySourceResponse{Present: false}})
	assert.False(t, v.Present)
	assert.False(t, v.Usable)
	require.Len(t, v.Meaning, 1)
	assert.Empty(t, v.Table.Rows)
	assert.Nil(t, BuildPolicyView(PolicyInput{}).Meaning)
}

func TestProtections(t *testing.T) {
	ps := protections(boardPolicyText, nil, nil)
	require.Len(t, ps, 3)
	assert.Equal(t, "signed", ps[0].Key)
	assert.True(t, ps[0].On)
	assert.True(t, ps[1].On, "scanning blocks by default")
	assert.False(t, ps[2].On)

	ps = protections(boardPolicyText, &ascv1.PolicyLandRequirements{Signed: false}, &ascv1.ListSecretRulesResponse{BlockOnPush: false})
	assert.False(t, ps[0].On, "asc's answer wins over the text")
	assert.False(t, ps[1].On)
}

func TestApplyToggleEditsOnlyThatKey(t *testing.T) {
	out, err := ApplyToggle(boardPolicyText, "signed", false)
	require.NoError(t, err)
	v, ok := TomlBool(out, "signing", "require_signed")
	assert.True(t, ok)
	assert.False(t, v)
	assert.Equal(t, strings.ReplaceAll(boardPolicyText, "require_signed = true", "require_signed = false"), out)

	out, err = ApplyToggle(boardPolicyText, "quarantine", true)
	require.NoError(t, err)
	v, _ = TomlBool(out, "ci", "allow_quarantine")
	assert.True(t, v)

	_, err = ApplyToggle(boardPolicyText, "no-such-switch", true)
	assert.ErrorIs(t, err, ErrBadInput)
	_, err = ApplyToggle(boardPolicyText, "[evil]", true)
	assert.ErrorIs(t, err, ErrBadInput, "only the three known switches")
}

func TestStarterPolicy(t *testing.T) {
	s := StarterPolicy("Alice")
	assert.Contains(t, s, `maintainer = ["user:Alice"]`)
	assert.True(t, strings.HasPrefix(s, "version = 1\n"))
	assert.LessOrEqual(t, len(s), MaxPolicyBytes)
	// A name with characters that could break out of the string is cleaned.
	evil := StarterPolicy("a\"]\n[x]")
	assert.Contains(t, evil, `maintainer = ["user:ax"]`)
	assert.Contains(t, StarterPolicy("\u202e"), `["user:your-name"]`)
	// It reads back as the maintainer.
	list, ok := TomlStrings(s, "roles", "maintainer")
	assert.True(t, ok)
	assert.Equal(t, []string{"user:Alice"}, list)
	// Everything else is a comment, so the starter is small and valid.
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		assert.True(t, trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "version") || strings.HasPrefix(trimmed, "[roles]") || strings.HasPrefix(trimmed, "maintainer"), line)
	}
}

func TestDiagnosticsOfPutsErrorsFirstAndCleans(t *testing.T) {
	resp := &ascv1.ValidatePolicyResponse{Diagnostics: []*ascv1.PolicyDiagnostic{
		{Severity: ascv1.PolicySeverity_POLICY_SEVERITY_WARNING, Code: "no_checks", Message: "no\x00 checks", Line: 3},
		{Severity: ascv1.PolicySeverity_POLICY_SEVERITY_ERROR, Code: "unknown_key", Message: "unknown key\u202e x", Line: 7, Column: 2},
	}}
	got := DiagnosticsOf(resp)
	require.Len(t, got, 2)
	assert.True(t, got[0].Error)
	assert.Equal(t, "unknown key x", got[0].Message)
	assert.EqualValues(t, 7, got[0].Line)
	assert.Equal(t, "no checks", got[1].Message)
}
