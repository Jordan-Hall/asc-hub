// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	gocontext "context"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascidentity"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

var accountForwardedKey = []byte("0123456789abcdef0123456789abcdef")

const (
	accountToken    = "internal-token-for-tests"
	accountExportID = "ex_abcdefghij0123456789"
)

// fakeAccountAsc is an asc serve with Repo.Version, Sessions and DataExport. A nil hook answers Unimplemented.
type fakeAccountAsc struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedSessionsServer
	ascv1.UnimplementedDataExportServer

	mu       sync.Mutex
	features []string
	md       []metadata.MD
	reqs     []any

	list     func(*ascv1.ListSessionsRequest) (*ascv1.ListSessionsResponse, error)
	revoke   func(*ascv1.RevokeSessionRequest) (*ascv1.RevokeSessionResponse, error)
	revAll   func(*ascv1.RevokeOtherSessionsRequest) (*ascv1.RevokeOtherSessionsResponse, error)
	exports  func() (*ascv1.ListExportsResponse, error)
	request  func() (*ascv1.RequestExportResponse, error)
	del      func() (*ascv1.DeleteExportResponse, error)
	download func(grpc.ServerStreamingServer[ascv1.ExportChunk]) error
}

func (f *fakeAccountAsc) note(ctx gocontext.Context, req any) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.mu.Lock()
	f.md = append(f.md, md)
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
}

func (f *fakeAccountAsc) Version(ctx gocontext.Context, _ *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	return &ascv1.VersionResponse{Features: f.features}, nil
}

func (f *fakeAccountAsc) ListSessions(ctx gocontext.Context, r *ascv1.ListSessionsRequest) (*ascv1.ListSessionsResponse, error) {
	f.note(ctx, r)
	if f.list == nil {
		return f.UnimplementedSessionsServer.ListSessions(ctx, r)
	}
	return f.list(r)
}

func (f *fakeAccountAsc) RevokeSession(ctx gocontext.Context, r *ascv1.RevokeSessionRequest) (*ascv1.RevokeSessionResponse, error) {
	f.note(ctx, r)
	if f.revoke == nil {
		return f.UnimplementedSessionsServer.RevokeSession(ctx, r)
	}
	return f.revoke(r)
}

func (f *fakeAccountAsc) RevokeOtherSessions(ctx gocontext.Context, r *ascv1.RevokeOtherSessionsRequest) (*ascv1.RevokeOtherSessionsResponse, error) {
	f.note(ctx, r)
	if f.revAll == nil {
		return f.UnimplementedSessionsServer.RevokeOtherSessions(ctx, r)
	}
	return f.revAll(r)
}

func (f *fakeAccountAsc) ListExports(ctx gocontext.Context, r *ascv1.ListExportsRequest) (*ascv1.ListExportsResponse, error) {
	f.note(ctx, r)
	if f.exports == nil {
		return f.UnimplementedDataExportServer.ListExports(ctx, r)
	}
	return f.exports()
}

func (f *fakeAccountAsc) RequestExport(ctx gocontext.Context, r *ascv1.RequestExportRequest) (*ascv1.RequestExportResponse, error) {
	f.note(ctx, r)
	if f.request == nil {
		return f.UnimplementedDataExportServer.RequestExport(ctx, r)
	}
	return f.request()
}

func (f *fakeAccountAsc) DeleteExport(ctx gocontext.Context, r *ascv1.DeleteExportRequest) (*ascv1.DeleteExportResponse, error) {
	f.note(ctx, r)
	if f.del == nil {
		return f.UnimplementedDataExportServer.DeleteExport(ctx, r)
	}
	return f.del()
}

func (f *fakeAccountAsc) Download(r *ascv1.DownloadExportRequest, s grpc.ServerStreamingServer[ascv1.ExportChunk]) error {
	f.note(s.Context(), r)
	if f.download == nil {
		return f.UnimplementedDataExportServer.Download(r, s)
	}
	return f.download(s)
}

func (f *fakeAccountAsc) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reqs)
}

func startAccountAsc(t *testing.T, features ...string) *fakeAccountAsc {
	t.Helper()
	f := &fakeAccountAsc{features: features}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	ascv1.RegisterSessionsServer(srv, f)
	ascv1.RegisterDataExportServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: accountToken, Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	t.Cleanup(asctrust.UseConn(c))
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, accountForwardedKey))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	return f
}

// assertAccountClaim checks that call i carried the hub's service credential and a claim for the account of user.
func (f *fakeAccountAsc) assertClaim(t *testing.T, i int, userID, login string) {
	t.Helper()
	f.mu.Lock()
	md := f.md[i]
	f.mu.Unlock()
	assert.Equal(t, []string{"Bearer " + accountToken}, md.Get("authorization"))
	user, role, err := ascidentity.Verify(accountForwardedKey, func(k string) string { return strings.Join(md.Get(k), ",") }, "user:"+login, time.Now())
	require.NoError(t, err, "the claim is signed for the account of the person")
	assert.Equal(t, userID+":"+login, user)
	assert.Equal(t, ascidentity.RoleWrite, role)
}

type accountResp struct {
	status int
	body   string
	loc    string
	header http.Header
	flash  string
}

func runAccount(t *testing.T, method, path string, user int64, form url.Values, params map[string]string, handler func(*context.Context)) accountResp {
	t.Helper()
	ctx, resp := contexttest.MockContext(t, method+" "+path)
	ctx.Render = templates.HTMLRenderer()
	if user != 0 {
		contexttest.LoadUser(t, ctx, user)
		ctx.IsSigned = true
	}
	ctx.Data["CsrfTokenHtml"] = template.HTML(`<input type="hidden" name="_csrf" value="csrf-token-for-tests">`)
	if form != nil {
		ctx.Req.Form = form
		ctx.Req.PostForm = form
	}
	for k, v := range params {
		ctx.SetParams(":"+k, v)
	}
	handler(ctx)
	r := accountResp{status: resp.Code, body: resp.Body.String(), loc: resp.Header().Get("Location"), header: resp.Header()}
	if ctx.Flash != nil {
		r.flash = ctx.Flash.ErrorMsg + ctx.Flash.SuccessMsg
	}
	return r
}

func opForm(extra ...string) url.Values {
	v := url.Values{"op": {uuid.Must(uuid.NewV7()).String()}}
	for i := 0; i+1 < len(extra); i += 2 {
		v.Set(extra[i], extra[i+1])
	}
	return v
}

func TestAscAccountPagesFailClosed(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startAccountAsc(t, asctrust.FeatureSessions, asctrust.FeatureDataExport)

	t.Run("asc off is a 404", func(t *testing.T) {
		t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, false))
		assert.Equal(t, http.StatusNotFound, runAccount(t, "GET", "/user/settings/asc/sessions", 2, nil, nil, AscSessions).status)
		assert.Equal(t, http.StatusNotFound, runAccount(t, "GET", "/user/settings/asc/export", 2, nil, nil, AscExport).status)
	})
	t.Run("nobody is a 403 and nothing is sent", func(t *testing.T) {
		for name, h := range map[string]func(*context.Context){
			"sessions": AscSessions, "revoke": AscSessionsRevoke, "revoke-all": AscSessionsRevokeAll,
			"export": AscExport, "request": AscExportRequest, "download": AscExportDownload, "delete": AscExportDelete,
		} {
			r := runAccount(t, "GET", "/user/settings/asc/x", 0, opForm(), map[string]string{"id": accountExportID}, h)
			assert.Equal(t, http.StatusForbidden, r.status, name)
		}
		assert.Zero(t, f.calls())
	})
	t.Run("an organisation account is a 403", func(t *testing.T) {
		r := runAccount(t, "GET", "/user/settings/asc/sessions", 3, nil, nil, AscSessions) // user 3 is an organisation
		assert.Equal(t, http.StatusForbidden, r.status)
		assert.Zero(t, f.calls())
	})
}

func TestAscSessionsPage(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startAccountAsc(t, asctrust.FeatureSessions)
	f.list = func(r *ascv1.ListSessionsRequest) (*ascv1.ListSessionsResponse, error) {
		return &ascv1.ListSessionsResponse{Sessions: []*ascv1.Session{
			{Id: "tok:7", Kind: ascv1.SessionKind_SESSION_KIND_TOKEN, Label: "<script>alert(1)</script>", Scope: "acme/* write", Created: 1700000000},
			{Id: "ssh:3", Kind: ascv1.SessionKind_SESSION_KIND_SSH_KEY, Label: "work laptop", Created: 1700000100},
		}}, nil
	}
	r := runAccount(t, "GET", "/user/settings/asc/sessions", 2, nil, nil, AscSessions)
	require.Equal(t, http.StatusOK, r.status, r.body)
	assert.Equal(t, "no-store", r.header.Get("Cache-Control"))
	assert.Contains(t, r.body, `data-testid="asc-sessions-list"`)
	assert.Contains(t, r.body, "work laptop")
	assert.NotContains(t, r.body, "<script>alert(1)</script>", "a label is escaped")
	assert.Contains(t, r.body, `name="id" value="tok:7"`)
	assert.Contains(t, r.body, `/revoke-all`)
	req := f.reqs[0].(*ascv1.ListSessionsRequest)
	assert.Equal(t, "user2", req.GetLogin(), "the login is the signed-in person's, from the session")
	f.assertClaim(t, 0, "2", "user2")
}

func TestAscSessionsPageSaysSoWhenTheServerHasNoSessions(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startAccountAsc(t)
	r := runAccount(t, "GET", "/user/settings/asc/sessions", 2, nil, nil, AscSessions)
	assert.Equal(t, http.StatusOK, r.status)
	assert.Contains(t, r.body, `data-testid="asc-sessions-unsupported"`)
	assert.Zero(t, f.calls())
}

func TestAscSessionsPageShowsAFailureAsOneSentence(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startAccountAsc(t, asctrust.FeatureSessions)
	f.list = func(*ascv1.ListSessionsRequest) (*ascv1.ListSessionsResponse, error) {
		return &ascv1.ListSessionsResponse{Sessions: []*ascv1.Session{{Id: "../x", Kind: ascv1.SessionKind_SESSION_KIND_TOKEN}}}, nil
	}
	r := runAccount(t, "GET", "/user/settings/asc/sessions", 2, nil, nil, AscSessions)
	assert.Equal(t, http.StatusBadGateway, r.status, "an answer that cannot be trusted shows nothing")
	assert.Contains(t, r.body, `data-testid="asc-sessions-problem"`)
	assert.NotContains(t, r.body, "../x")
}

func TestAscSessionsRevoke(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startAccountAsc(t, asctrust.FeatureSessions)
	f.revoke = func(*ascv1.RevokeSessionRequest) (*ascv1.RevokeSessionResponse, error) {
		return &ascv1.RevokeSessionResponse{}, nil
	}
	f.revAll = func(*ascv1.RevokeOtherSessionsRequest) (*ascv1.RevokeOtherSessionsResponse, error) {
		return &ascv1.RevokeOtherSessionsResponse{Revoked: 3}, nil
	}

	r := runAccount(t, "POST", "/user/settings/asc/sessions/revoke", 2, opForm("id", "tok:7"), nil, AscSessionsRevoke)
	assert.Equal(t, http.StatusSeeOther, r.status)
	assert.Equal(t, "/user/settings/asc/sessions", r.loc)
	req := f.reqs[0].(*ascv1.RevokeSessionRequest)
	assert.Equal(t, "tok:7", req.GetId())
	assert.Equal(t, "user2", req.GetLogin())
	f.assertClaim(t, 0, "2", "user2")

	r = runAccount(t, "POST", "/user/settings/asc/sessions/revoke-all", 2, opForm(), nil, AscSessionsRevokeAll)
	assert.Equal(t, http.StatusSeeOther, r.status)
	assert.Contains(t, r.flash, "sess_revoked_n", "the count goes into the message")
	assert.Equal(t, "user2", f.reqs[1].(*ascv1.RevokeOtherSessionsRequest).GetLogin())

	n := f.calls()
	r = runAccount(t, "POST", "/user/settings/asc/sessions/revoke", 2, opForm("id", "../../admin"), nil, AscSessionsRevoke)
	assert.Equal(t, http.StatusSeeOther, r.status)
	assert.NotEmpty(t, r.flash, "a bad id is an error message")
	assert.Equal(t, n, f.calls(), "a bad id is refused before anything is sent")
}

func TestAscExportPageRequestAndDelete(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startAccountAsc(t, asctrust.FeatureDataExport)
	job := func(state ascv1.ExportState) *ascv1.ExportJob {
		return &ascv1.ExportJob{Id: accountExportID, State: state, Requested: time.Now().Unix() - 60, Expires: time.Now().Add(24 * time.Hour).Unix(), SizeBytes: 2048, Rows: 12}
	}
	f.exports = func() (*ascv1.ListExportsResponse, error) {
		return &ascv1.ListExportsResponse{Jobs: []*ascv1.ExportJob{job(ascv1.ExportState_EXPORT_STATE_READY)}}, nil
	}
	r := runAccount(t, "GET", "/user/settings/asc/export", 2, nil, nil, AscExport)
	require.Equal(t, http.StatusOK, r.status, r.body)
	assert.Contains(t, r.body, "/user/settings/asc/export/"+accountExportID, "a ready archive has a download link")
	assert.Contains(t, r.body, `data-testid="asc-export-request"`, "no export is running, so a new one can be asked for")
	f.assertClaim(t, 0, "2", "user2")

	f.exports = func() (*ascv1.ListExportsResponse, error) {
		return &ascv1.ListExportsResponse{Jobs: []*ascv1.ExportJob{job(ascv1.ExportState_EXPORT_STATE_RUNNING)}}, nil
	}
	r = runAccount(t, "GET", "/user/settings/asc/export", 2, nil, nil, AscExport)
	assert.Contains(t, r.body, `data-testid="asc-export-busy"`)
	assert.NotContains(t, r.body, `data-testid="asc-export-request"`, "a second export is not offered while one runs")
	assert.NotContains(t, r.body, "/user/settings/asc/export/"+accountExportID+`"`, "a running export has no download")

	f.request = func() (*ascv1.RequestExportResponse, error) {
		return &ascv1.RequestExportResponse{Job: job(ascv1.ExportState_EXPORT_STATE_PENDING)}, nil
	}
	form := opForm()
	r = runAccount(t, "POST", "/user/settings/asc/export", 2, form, nil, AscExportRequest)
	assert.Equal(t, http.StatusSeeOther, r.status)
	assert.Equal(t, "/user/settings/asc/export", r.loc)

	f.del = func() (*ascv1.DeleteExportResponse, error) { return &ascv1.DeleteExportResponse{}, nil }
	r = runAccount(t, "POST", "/user/settings/asc/export/x/delete", 2, opForm(), map[string]string{"id": accountExportID}, AscExportDelete)
	assert.Equal(t, http.StatusSeeOther, r.status)
	n := f.calls()
	r = runAccount(t, "POST", "/user/settings/asc/export/x/delete", 2, opForm(), map[string]string{"id": "ex_../../x"}, AscExportDelete)
	assert.Equal(t, http.StatusSeeOther, r.status)
	assert.NotEmpty(t, r.flash)
	assert.Equal(t, n, f.calls())
}

func TestAscExportDownload(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startAccountAsc(t, asctrust.FeatureDataExport)
	f.download = func(s grpc.ServerStreamingServer[ascv1.ExportChunk]) error {
		if err := s.Send(&ascv1.ExportChunk{Offset: 0, Data: []byte(`{"a":1}` + "\n")}); err != nil {
			return err
		}
		return s.Send(&ascv1.ExportChunk{Offset: 8, Data: []byte(`{"b":2}` + "\n"), Last: true})
	}
	r := runAccount(t, "GET", "/user/settings/asc/export/"+accountExportID, 2, nil, map[string]string{"id": accountExportID}, AscExportDownload)
	require.Equal(t, http.StatusOK, r.status)
	assert.Equal(t, `{"a":1}`+"\n"+`{"b":2}`+"\n", r.body)
	assert.Equal(t, "application/x-ndjson; charset=utf-8", r.header.Get("Content-Type"))
	assert.Contains(t, r.header.Get("Content-Disposition"), "attachment")
	assert.Equal(t, "no-store", r.header.Get("Cache-Control"))
	assert.Equal(t, "nosniff", r.header.Get("X-Content-Type-Options"))
	assert.Equal(t, "16", r.header.Get("Content-Length"))
	f.assertClaim(t, 0, "2", "user2")
}

func TestAscExportDownloadNeverServesAShortFile(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	f := startAccountAsc(t, asctrust.FeatureDataExport)
	f.download = func(s grpc.ServerStreamingServer[ascv1.ExportChunk]) error {
		return s.Send(&ascv1.ExportChunk{Offset: 0, Data: []byte("half an archive")}) // no last chunk
	}
	r := runAccount(t, "GET", "/user/settings/asc/export/"+accountExportID, 2, nil, map[string]string{"id": accountExportID}, AscExportDownload)
	assert.Equal(t, http.StatusBadGateway, r.status)
	assert.NotContains(t, r.body, "half an archive")
	assert.Empty(t, r.header.Get("Content-Disposition"))

	r = runAccount(t, "GET", "/user/settings/asc/export/x", 2, nil, map[string]string{"id": "../../etc/passwd"}, AscExportDownload)
	assert.Equal(t, http.StatusBadRequest, r.status, "a bad id is refused before a call")
}
