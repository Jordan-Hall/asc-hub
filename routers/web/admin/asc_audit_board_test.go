// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	gocontext "context"
	"net/http"
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/templates"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// auditVerifyAnswer is what the fake asc answers to VerifyChain.
var auditVerifyAnswer = &ascv1.VerifyAuditChainResponse{Ok: true}

func (f *fakeInstanceAgents) VerifyChain(gocontext.Context, *ascv1.VerifyAuditChainRequest) (*ascv1.VerifyAuditChainResponse, error) {
	return auditVerifyAnswer, nil
}

func (f *fakeInstanceAgents) ListActions(gocontext.Context, *ascv1.ListAuditActionsRequest) (*ascv1.ListAuditActionsResponse, error) {
	return &ascv1.ListAuditActionsResponse{}, nil
}

// The test renderer has no locale: the texts are their keys.
func renderAdminAudit(t *testing.T, path string, serve func(*context.Context)) (int, *goquery.Document) {
	t.Helper()
	ctx, rec := contexttest.MockContext(t, path)
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 1)
	ctx.IsSigned = true
	serve(ctx)
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rec.Body.String()))
	require.NoError(t, err)
	return rec.Code, doc
}

// The notes of board AdminAudit: a row expands in place to the full record with its request id and the diff of
// what changed; the states are default, filtered, no results and chain verification failed (the export in progress
// is web_src/js/features/asc-audit.test.js).
func TestAscAdminAuditBoardNotes(t *testing.T) {
	f := startInstanceAgents(t, asctrust.FeatureAudit)
	f.events = []*ascv1.AuditEvent{{
		Seq: 7, Id: "0b9c7d1e-0000-4000-8000-000000000007", TimeMs: 1_700_000_000_000,
		Actor: &ascv1.PrincipalRef{Kind: "user", Name: "maya"}, Action: "settings.updated", Repo: "acme/web",
		Target: "merge_method", Outcome: ascv1.AuditOutcome_AUDIT_OUTCOME_SUCCESS, RequestId: "req-4f2a9c", RemoteAddr: "203.0.113.0/24",
		UserAgent: "asc/0.1.3", Hash: []byte{0xab, 0xcd, 0xef},
		Details: map[string]string{"setting": "merge_method", "old": "merge", "new": "squash", "via": "web"},
	}, {
		Seq: 8, Id: "0b9c7d1e-0000-4000-8000-000000000008", TimeMs: 1_700_000_001_000,
		Actor: &ascv1.PrincipalRef{Kind: "user", Name: "maya"}, Action: "settings.updated", Outcome: ascv1.AuditOutcome_AUDIT_OUTCOME_SUCCESS,
		Details: map[string]string{"setting": "webhook_secret", "old": "s1", "new": "s2"},
	}}

	t.Run("a row expands in place to the full record, its request id and the diff", func(t *testing.T) {
		code, doc := renderAdminAudit(t, "GET /admin/asc/audit", AscAudit)
		require.Equal(t, http.StatusOK, code)
		row := doc.Find(`li.adm-audit-li[data-seq="7"]`)
		require.Equal(t, 1, row.Length())
		record := row.Find(`details[data-testid="asc-audit-record"]`)
		require.Equal(t, 1, record.Length(), "the record is a disclosure inside the row")
		_, open := record.Attr("open")
		assert.False(t, open, "closed until asked")
		assert.Equal(t, "req-4f2a9c", strings.TrimSpace(record.Find(`[data-testid="asc-audit-request"]`).Text()))
		text := record.Text()
		for _, want := range []string{"0b9c7d1e-0000-4000-8000-000000000007", "203.0.113.0/24", "asc/0.1.3", "abcdef", "via", "web", "Success"} {
			assert.Contains(t, text, want)
		}
		diff := record.Find(`[data-testid="asc-audit-diff"] li`)
		require.Equal(t, 1, diff.Length())
		assert.Equal(t, "merge_method", diff.Find("code").Text())
		assert.Equal(t, "asc_trust.audit.rec_old merge", strings.TrimSpace(diff.Find("del").Text()))
		assert.Equal(t, "asc_trust.audit.rec_new squash", strings.TrimSpace(diff.Find("ins").Text()))
		assert.NotContains(t, record.Find("dl").Text(), "squash", "the diff is not repeated in the list")

		secret := doc.Find(`li.adm-audit-li[data-seq="8"] [data-testid="asc-audit-diff"]`)
		assert.Equal(t, "asc_trust.audit.rec_old hidden", strings.TrimSpace(secret.Find("del").Text()))
		assert.NotContains(t, secret.Text(), "s2")
		assert.Equal(t, "asc_trust.audit.rec_none", strings.TrimSpace(doc.Find(`li.adm-audit-li[data-seq="8"] [data-testid="asc-audit-request"]`).Text()))
	})

	t.Run("default and filtered", func(t *testing.T) {
		_, doc := renderAdminAudit(t, "GET /admin/asc/audit", AscAudit)
		_, open := doc.Find(`[data-testid="asc-audit-filters"] details`).Attr("open")
		assert.False(t, open)
		_, doc = renderAdminAudit(t, "GET /admin/asc/audit?actor=maya", AscAudit)
		_, open = doc.Find(`[data-testid="asc-audit-filters"] details`).Attr("open")
		assert.True(t, open, "a filtered page shows its filters")
	})

	t.Run("no results", func(t *testing.T) {
		saved := f.events
		f.events = nil
		defer func() { f.events = saved }()
		_, doc := renderAdminAudit(t, "GET /admin/asc/audit?actor=nobody", AscAudit)
		assert.Equal(t, 1, doc.Find(`[data-testid="asc-audit-empty"]`).Length())
		assert.Equal(t, 0, doc.Find(`[data-testid="asc-audit-table"]`).Length())
	})

	t.Run("chain verification failed", func(t *testing.T) {
		auditVerifyAnswer = &ascv1.VerifyAuditChainResponse{Ok: false, Checked: 6, FirstBadSeq: 7, Reason: "hash mismatch", FirstSeq: 1, HeadSeq: 8}
		defer func() { auditVerifyAnswer = &ascv1.VerifyAuditChainResponse{Ok: true} }()
		_, doc := renderAdminAudit(t, "POST /admin/asc/audit/verify", AscAuditVerify)
		banner := doc.Find(`.ds-banner-err[data-testid="asc-audit-chain"]`)
		require.Equal(t, 1, banner.Length())
		assert.Equal(t, "alert", banner.AttrOr("role", ""))
		assert.Contains(t, banner.Text(), "hash mismatch")
	})
}
