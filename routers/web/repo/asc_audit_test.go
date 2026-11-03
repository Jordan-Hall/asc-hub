// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"io"
	"net/http"
	"strings"
	"testing"

	perm_model "forgejo.org/models/perm"
	"forgejo.org/models/unittest"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascidentity"
	"forgejo.org/services/asctrust"

	"github.com/42wim/sshsig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeebo/blake3"
	"golang.org/x/crypto/ssh"
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
	assertAs(t, calls[0], "2:user2", ascidentity.RoleAdmin, "user2/repo1")
	assert.Contains(t, page.ctx.Resp.Header().Get("Cache-Control"), "no-store")

	// The owner of the repository read the log: the hub decided "administrator" and said so in the claim. The hub
	// made no access token for it.

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

func TestAscAuditVerifyChainBrokenOffersTheEvent(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureAudit)
	f.auditChain = func(*ascv1.VerifyAuditChainRequest) (*ascv1.VerifyAuditChainResponse, error) {
		return &ascv1.VerifyAuditChainResponse{Ok: false, Checked: 12, FirstBadSeq: 9, Reason: "hash mismatch"}, nil
	}
	page := runTrust(t, trustReq{method: http.MethodPost, path: "/user2/repo1/settings/asc/audit/verify", user: 2, access: adminAccess}, AscAuditVerify)
	require.Equal(t, http.StatusOK, page.status, page.body)
	assert.Contains(t, page.body, `data-testid="asc-audit-show-event"`)
	assert.Contains(t, page.body, "?seq=9")
}

func TestAscAuditExportIsSignedWhenTheServerSignsIt(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startFakeTrust(t, asctrust.FeatureAudit)
	row := []byte("{\"seq\":1}\n")
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	block, err := ssh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)
	sig, err := sshsig.Sign(pem.EncodeToMemory(block), bytes.NewReader(row), asctrust.AuditExportNamespace)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(priv)
	require.NoError(t, err)
	key := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	last := &ascv1.ExportAuditResponse{Data: row, Last: true, Digest: blake3Of(row), Events: 1}
	f.auditExport = func(*ascv1.ExportAuditRequest) []*ascv1.ExportAuditResponse {
		return []*ascv1.ExportAuditResponse{last}
	}
	export := func() trustPage {
		return runTrust(t, trustReq{path: "/user2/repo1/settings/asc/audit/export?format=jsonl", user: 2, access: adminAccess}, AscAuditExport)
	}

	// Without a server key the file itself downloads.
	plain := export()
	require.Equal(t, http.StatusOK, plain.status, plain.body)
	assert.Equal(t, string(row), plain.body)
	assert.Contains(t, plain.ctx.Resp.Header().Get("Content-Type"), "application/x-ndjson")
	assert.Regexp(t, `^attachment; filename="asc-audit-user2-repo1-\d{8}\.jsonl";`, plain.ctx.Resp.Header().Get("Content-Disposition"))

	// With one, the download is the archive that checks itself.
	last.Signature, last.SignerKey = string(sig), key
	signed := export()
	require.Equal(t, http.StatusOK, signed.status, signed.body)
	assert.Equal(t, "application/zip", signed.ctx.Resp.Header().Get("Content-Type"))
	assert.Regexp(t, `^attachment; filename="asc-audit-user2-repo1-\d{8}\.jsonl\.zip";`, signed.ctx.Resp.Header().Get("Content-Disposition"))
	zr, err := zip.NewReader(strings.NewReader(signed.body), int64(len(signed.body)))
	require.NoError(t, err)
	files := map[string]string{}
	for _, zf := range zr.File {
		rc, err := zf.Open()
		require.NoError(t, err)
		b, err := io.ReadAll(rc)
		require.NoError(t, err)
		_ = rc.Close()
		files[zf.Name[strings.LastIndex(zf.Name, ".jsonl")+1:]] = string(b)
	}
	assert.Equal(t, string(row), files["jsonl"])
	require.NoError(t, sshsig.Verify(strings.NewReader(files["jsonl"]), []byte(files["jsonl.sig"]), []byte(key), asctrust.AuditExportNamespace))
	assert.Contains(t, files["allowed_signers"], key)
	assert.Contains(t, files["VERIFY.txt"], "ssh-keygen -Y verify")

	// A signature that does not match the file is an error page, not a download.
	last.Data, last.Digest = []byte("{\"seq\":2}\n"), blake3Of([]byte("{\"seq\":2}\n"))
	bad := export()
	assert.NotEqual(t, http.StatusOK, bad.status)
	assert.NotContains(t, bad.ctx.Resp.Header().Get("Content-Disposition"), "attachment")
	assert.Len(t, f.callsOf("Export"), 3)
}

func blake3Of(b []byte) []byte {
	sum := blake3.Sum256(b)
	return sum[:]
}
