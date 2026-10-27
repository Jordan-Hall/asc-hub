// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	gocontext "context"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	perm_model "forgejo.org/models/perm"
	"forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/base"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/translation"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/require"
)

// Page snapshots for the design review of the secret scanning page. Set ASC_REVIEW_SNAPSHOT_DIR to a directory and
// run this test: the page is rendered from fixtures through its real template and written as <name>.html. Without the
// variable it does nothing.
func snapshotDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("ASC_REVIEW_SNAPSHOT_DIR")
	if dir == "" {
		t.Skip("set ASC_REVIEW_SNAPSHOT_DIR to write page snapshots")
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))
	return dir
}

func renderSettingsPage(t *testing.T, dir, name string, tpl base.TplName, data map[string]any) string {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	if len(setting.Langs) == 0 || setting.Langs[0] != "en-US" {
		setting.Langs = []string{"en-US"}
		setting.Names = []string{"English"}
	}
	translation.InitLocales(gocontext.Background())
	ctx, resp := contexttest.MockContext(t, "GET /user2/repo1/settings/asc/security")
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 2)
	ctx.IsSigned = true
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Base.Locale = translation.NewLocale("en-US")
	if ctx.TemplateContext != nil {
		ctx.TemplateContext.Locale = ctx.Base.Locale
	}
	ctx.Data["RepoLink"] = ctx.Repo.RepoLink
	ctx.Data["CsrfTokenHtml"] = template.HTML(`<input type="hidden" name="_csrf" value="csrf-token-for-tests">`)
	ctx.Repo.Permission.AccessMode = perm_model.AccessModeAdmin
	ctx.Repo.Permission.UnitsMode = map[unit.Type]perm_model.AccessMode{
		unit.TypeCode: perm_model.AccessModeAdmin, unit.TypePullRequests: perm_model.AccessModeAdmin, unit.TypeActions: perm_model.AccessModeAdmin,
	}
	ctx.Data["Title"] = "Secret scanning"
	ctx.Data["PageIsSettingsAscSecurity"] = true
	for k, v := range data {
		ctx.Data[k] = v
	}
	ctx.HTML(http.StatusOK, tpl)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".html"), resp.Body.Bytes(), 0o644))
	return resp.Body.String()
}

func securityFixture(state string, rows []asctrust.FindingRow) *ascSecurityPage {
	p := &ascSecurityPage{
		AllowOp: "op-allow", Branch: "main", State: state,
		PolicyURL: "/user2/repo1/settings/asc/policy", SecurityURL: "/user2/repo1/settings/asc/security", AuditURL: "/user2/repo1/settings/asc/audit",
		Ops: map[uint64]string{},
		Data: &asctrust.SecretsPage{
			Rules: &asctrust.RulesView{
				BlockOnPush: true, AllowPaths: []string{"tests/fixtures/fake_*.json", "docs/examples/**", "vendor/**", "tests/golden/*.pem"},
				Rules: []asctrust.RuleRow{
					{ID: "aws.access_key", Name: "Cloud access key", Description: "A key id of the cloud provider", Severity: "High", Tone: "no", Builtin: true, Enabled: true, Detector: "regex"},
					{ID: "pem.private_key", Name: "Private key header", Severity: "High", Tone: "no", Builtin: true, Enabled: true, Detector: "regex"},
					{ID: "custom.acme_token", Name: "Acme internal service token", Severity: "High", Tone: "no", Enabled: true, Detector: "regex"},
					{ID: "entropy.generic", Name: "Generic high-entropy string", Severity: "Low", Tone: "ro", Builtin: true, Enabled: false, Detector: "entropy"},
				},
			},
			Findings: &asctrust.FindingsPage{Rows: rows},
		},
		Blocked: []asctrust.AuditRow{
			{Time: time.Now().Add(-9 * time.Minute), Actor: "Hannah Weiss", Target: "refs/heads/main", Details: []asctrust.KV{{Key: "rule", Value: "aws.access_key"}, {Key: "path", Value: "config/prod.env:3"}}},
			{Time: time.Now().Add(-48 * time.Hour), Actor: "Priya Raman", Target: "refs/heads/main", Details: []asctrust.KV{{Key: "rule", Value: "aws.access_key"}}},
		},
		Allowlist: "tests/fixtures/fake_*.json\ndocs/examples/**\nvendor/**\ntests/golden/*.pem",
		States: []ascStateOption{
			{Key: "open", Label: "Open", Count: 2, Active: state == "open"}, {Key: "revoked", Label: "Revoked", Count: 9, Active: state == "revoked"},
			{Key: "false_positive", Label: "False positive", Count: 3, Active: state == "false_positive"}, {Key: "accepted_risk", Label: "Accepted risk", Count: 1, Active: state == "accepted_risk"},
		},
	}
	for _, r := range rows {
		p.Ops[r.ID] = "op-" + r.Path
	}
	return p
}

func TestAscReviewSnapshotSecurity(t *testing.T) {
	dir := snapshotDir(t)
	now := time.Now()
	rows := []asctrust.FindingRow{
		{ID: 1, Rule: "aws.access_key", Severity: "High", Tone: "no", StateKey: "open", State: "Open", Path: "config/prod.env", Line: 3, Location: "config/prod.env:3", Preview: "AK***", LastSeen: now.Add(-9 * time.Minute)},
		{ID: 2, Rule: "stripe.token", Severity: "High", Tone: "no", StateKey: "open", State: "Open", Path: "tests/fixtures/stripe_sample.json", Line: 12, Location: "tests/fixtures/stripe_sample.json:12", Preview: "sk***", Change: "c1ab94fe02aa", LastSeen: now.Add(-72 * time.Hour)},
	}
	p := securityFixture("open", rows)
	p.NextURL = "/user2/repo1/settings/asc/security?state=open&page=n2"
	renderSettingsPage(t, dir, "security", tplAscSecurity, map[string]any{"AscSecurity": p})

	closed := []asctrust.FindingRow{
		{ID: 3, Rule: "bearer.token", Severity: "Medium", Tone: "wn", StateKey: "false_positive", State: "False positive", Path: "docs/examples/curl.md", Line: 41, Location: "docs/examples/curl.md:41", Preview: "ey***", ResolvedBy: "docs-1", Note: "False positive: example value", LastSeen: now.Add(-6 * 24 * time.Hour)},
		{ID: 4, Rule: "pem.private_key", Severity: "High", Tone: "no", StateKey: "revoked", State: "Revoked", Path: "deploy/old/rotate.sh", Line: 7, Location: "deploy/old/rotate.sh:7", Preview: "--***", ResolvedBy: "Priya Raman", Note: "Revoked and rotated", LastSeen: now.Add(-21 * 24 * time.Hour)},
		{ID: 5, Rule: "entropy.generic", Severity: "Low", Tone: "ro", StateKey: "accepted_risk", State: "Accepted risk", Path: "src/legacy/seed.rs", Line: 88, Location: "src/legacy/seed.rs:88", Preview: "x9***", ResolvedBy: "Maya Okafor", Note: "Public test key", LastSeen: now.Add(-40 * 24 * time.Hour)},
	}
	renderSettingsPage(t, dir, "security-closed", tplAscSecurity, map[string]any{"AscSecurity": securityFixture("revoked", closed)})

	renderSettingsPage(t, dir, "security-empty", tplAscSecurity, map[string]any{"AscSecurity": securityFixture("revoked", nil)})

	off := securityFixture("open", nil)
	off.Data = nil
	off.Unsupported = true
	renderSettingsPage(t, dir, "security-off", tplAscSecurity, map[string]any{"AscSecurity": off})

	bad := securityFixture("open", nil)
	bad.Data = nil
	bad.Problem = "The ASC server is not reachable right now."
	renderSettingsPage(t, dir, "security-error", tplAscSecurity, map[string]any{"AscSecurity": bad})
}
