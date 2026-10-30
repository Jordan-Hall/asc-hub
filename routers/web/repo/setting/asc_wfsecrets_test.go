// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	gocontext "context"
	"html/template"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"forgejo.org/models/db"
	"forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const planted = "S3cr3t-value-that-must-never-come-back-9f2c"

// fakeWorkflowSecrets keeps the secrets of the Workflows service in memory: the value of one is stored once and never returned.
type fakeWorkflowSecrets struct {
	ascv1.UnimplementedWorkflowsServer
	mu     sync.Mutex
	values map[string]string
	ops    []string
	auths  []string
}

func (f *fakeWorkflowSecrets) note(ctx gocontext.Context) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops = append(f.ops, firstOf(md, "x-asc-op-id"))
	f.auths = append(f.auths, firstOf(md, "authorization"))
}

func firstOf(md metadata.MD, k string) string {
	if v := md.Get(k); len(v) > 0 {
		return v[0]
	}
	return ""
}

func (f *fakeWorkflowSecrets) ListSecrets(ctx gocontext.Context, _ *ascv1.ListWorkflowSecretsRequest) (*ascv1.ListWorkflowSecretsResponse, error) {
	f.note(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*ascv1.WorkflowSecret
	for name, v := range f.values {
		out = append(out, &ascv1.WorkflowSecret{Name: name, Hint: v[max(0, len(v)-4):], Updated: 1_700_000_000, UpdatedBy: "user2"})
	}
	return &ascv1.ListWorkflowSecretsResponse{Secrets: out}, nil
}

func (f *fakeWorkflowSecrets) SetSecret(ctx gocontext.Context, r *ascv1.SetWorkflowSecretRequest) (*ascv1.SetWorkflowSecretResponse, error) {
	f.note(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values[r.GetName()] = r.GetValue()
	return &ascv1.SetWorkflowSecretResponse{Secret: &ascv1.WorkflowSecret{Name: r.GetName(), Hint: "9f2c"}}, nil
}

func (f *fakeWorkflowSecrets) DeleteSecret(ctx gocontext.Context, r *ascv1.DeleteWorkflowSecretRequest) (*ascv1.DeleteWorkflowSecretResponse, error) {
	f.note(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.values, r.GetName())
	return &ascv1.DeleteWorkflowSecretResponse{}, nil
}

func startWorkflowSecrets(t *testing.T) *fakeWorkflowSecrets {
	t.Helper()
	f := &fakeWorkflowSecrets{values: map[string]string{"registry": "old-value-1111"}}
	srv := grpc.NewServer()
	ascv1.RegisterWorkflowsServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "internal-token-for-tests", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	t.Cleanup(asctrust.UseConn(c))
	t.Cleanup(test.MockVariableValue(&setting.Asc.ForwardedKey, []byte("0123456789abcdef0123456789abcdef")))
	t.Cleanup(test.MockVariableValue(&setting.Asc.Enabled, true))
	return f
}

// secretsReq builds a request to the page as user doer (0: signed out) with the access the hub gives that person on repository 1.
func secretsReq(t *testing.T, method string, doer int64, access perm.AccessMode, form url.Values) (*context.Context, *httptest.ResponseRecorder) {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	_, err := db.GetEngine(db.DefaultContext).ID(1).Cols("storage_backend").Update(&repo_model.Repository{StorageBackend: "asc"})
	require.NoError(t, err)
	ctx, rec := contexttest.MockContext(t, method+" /user2/repo1/settings/asc/workflow-secrets")
	ctx.Render = templates.HTMLRenderer()
	if doer != 0 {
		contexttest.LoadUser(t, ctx, doer)
		ctx.IsSigned = true
	}
	contexttest.LoadRepo(t, ctx, 1)
	ctx.Repo.Permission.AccessMode = access
	ctx.Data["CsrfTokenHtml"] = template.HTML("")
	if form != nil {
		ctx.Req.Form, ctx.Req.PostForm = form, form
	}
	return ctx, rec
}

func flashesOf(ctx *context.Context) string {
	if ctx.Flash == nil {
		return ""
	}
	return ctx.Flash.ErrorMsg + "|" + ctx.Flash.SuccessMsg
}

func TestAscWorkflowSecretsPage(t *testing.T) {
	f := startWorkflowSecrets(t)

	t.Run("an administrator sees names and hints, never a value", func(t *testing.T) {
		ctx, rec := secretsReq(t, "GET", 2, perm.AccessModeAdmin, nil)
		AscWorkflowSecrets(ctx)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		body := rec.Body.String()
		assert.Contains(t, body, `data-testid="asc-workflow-secrets-rows"`)
		assert.Contains(t, body, "<code>registry</code>")
		assert.Contains(t, body, "1111", "the hint: the last characters of a long value")
		assert.NotContains(t, body, "old-value-1111", "the value is never shown")
		assert.Contains(t, body, `name="op"`)
	})

	t.Run("a person who does not administer the repository is refused before asc is asked", func(t *testing.T) {
		f.mu.Lock()
		before := len(f.auths)
		f.mu.Unlock()
		for _, tc := range []struct {
			name   string
			doer   int64
			access perm.AccessMode
		}{{"a reader", 4, perm.AccessModeRead}, {"a writer", 4, perm.AccessModeWrite}, {"signed out", 0, perm.AccessModeNone}} {
			ctx, rec := secretsReq(t, "GET", tc.doer, tc.access, nil)
			AscWorkflowSecrets(ctx)
			assert.Equal(t, http.StatusForbidden, rec.Code, tc.name)
			assert.NotContains(t, rec.Body.String(), "registry", tc.name)
			ctx, rec = secretsReq(t, "POST", tc.doer, tc.access, url.Values{"name": {"x"}, "value": {planted}, "op": {uuid.NewString()}})
			AscWorkflowSecretSet(ctx)
			assert.Equal(t, http.StatusForbidden, rec.Code, tc.name)
			ctx, rec = secretsReq(t, "POST", tc.doer, tc.access, url.Values{"name": {"registry"}, "op": {uuid.NewString()}})
			AscWorkflowSecretDelete(ctx)
			assert.Equal(t, http.StatusForbidden, rec.Code, tc.name)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		assert.Len(t, f.auths, before, "asc was not called")
		assert.Equal(t, "old-value-1111", f.values["registry"])
		assert.NotContains(t, f.values, "x")
	})

	t.Run("a secret is set once and its value is never rendered, flashed or kept", func(t *testing.T) {
		op := uuid.Must(uuid.NewV7()).String()
		ctx, rec := secretsReq(t, "POST", 2, perm.AccessModeAdmin, url.Values{"name": {"registry"}, "value": {planted}, "op": {op}})
		AscWorkflowSecretSet(ctx)
		assert.Equal(t, http.StatusSeeOther, rec.Code)
		assert.Contains(t, flashesOf(ctx), "asc_review.wfs_saved")
		assert.NotContains(t, flashesOf(ctx), planted)
		assert.NotContains(t, rec.Body.String(), planted)
		assert.NotContains(t, rec.Header().Get("Location"), planted)
		f.mu.Lock()
		assert.Equal(t, planted, f.values["registry"], "asc got the value")
		assert.Equal(t, op, f.ops[len(f.ops)-1], "with the operation id of the form")
		f.mu.Unlock()

		ctx, rec = secretsReq(t, "GET", 2, perm.AccessModeAdmin, nil)
		AscWorkflowSecrets(ctx)
		assert.NotContains(t, rec.Body.String(), planted)
	})

	t.Run("bad input is refused with a message that does not carry the value", func(t *testing.T) {
		for _, form := range []url.Values{
			{"name": {"bad name"}, "value": {planted}, "op": {uuid.NewString()}},
			{"name": {"ok"}, "value": {""}, "op": {uuid.NewString()}},
			{"name": {"ok"}, "value": {planted}, "op": {"nope"}},
		} {
			ctx, rec := secretsReq(t, "POST", 2, perm.AccessModeAdmin, form)
			AscWorkflowSecretSet(ctx)
			assert.Equal(t, http.StatusSeeOther, rec.Code)
			assert.NotEmpty(t, ctx.Flash.ErrorMsg)
			assert.NotContains(t, ctx.Flash.ErrorMsg, planted)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		assert.NotContains(t, f.values, "ok")
	})

	t.Run("a secret is deleted by name", func(t *testing.T) {
		ctx, rec := secretsReq(t, "POST", 2, perm.AccessModeAdmin, url.Values{"name": {"registry"}, "op": {uuid.Must(uuid.NewV7()).String()}})
		AscWorkflowSecretDelete(ctx)
		assert.Equal(t, http.StatusSeeOther, rec.Code)
		assert.Contains(t, flashesOf(ctx), "asc_review.wfs_deleted")
		f.mu.Lock()
		defer f.mu.Unlock()
		assert.NotContains(t, f.values, "registry")
	})
}
