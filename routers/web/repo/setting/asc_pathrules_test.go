// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	gocontext "context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"forgejo.org/models/perm"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/asctrust"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// fakePathRules keeps the rules of one repository in memory, with asc's revision check.
type fakePathRules struct {
	ascv1.UnimplementedPathRulesServer
	mu       sync.Mutex
	rules    []*ascv1.PathRule
	revision uint64
	calls    int
	ops      []string
}

func (f *fakePathRules) ListPathRules(_ gocontext.Context, _ *ascv1.ListPathRulesRequest) (*ascv1.ListPathRulesResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return &ascv1.ListPathRulesResponse{Rules: f.rules, Revision: f.revision, Updated: 1_700_000_000, UpdatedBy: "user2"}, nil
}

func (f *fakePathRules) SetPathRules(ctx gocontext.Context, r *ascv1.SetPathRulesRequest) (*ascv1.SetPathRulesResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if r.GetExpectedRevision() != f.revision {
		return nil, status.Error(codes.FailedPrecondition, "the rules changed")
	}
	f.ops = append(f.ops, firstOf(md, "x-asc-op-id"))
	f.rules = r.GetRules()
	f.revision++
	return &ascv1.SetPathRulesResponse{Revision: f.revision}, nil
}

func (f *fakePathRules) CheckPathAccess(_ gocontext.Context, r *ascv1.CheckPathAccessRequest) (*ascv1.CheckPathAccessResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	out := &ascv1.CheckPathAccessResponse{Teams: []string{"backend"}}
	for _, p := range r.GetPaths() {
		a := &ascv1.PathAccess{Path: p, Level: r.GetRepoLevel()}
		if p == "secret" || strings.HasPrefix(p, "secret/") {
			a.Level, a.DecidedBy = "none", "secret"
		}
		out.Access = append(out.Access, a)
	}
	return out, nil
}

func startPathRules(t *testing.T) *fakePathRules {
	t.Helper()
	f := &fakePathRules{rules: []*ascv1.PathRule{{Dir: "secret", Subject: "*", Level: "none"}}, revision: 1}
	srv := grpc.NewServer()
	ascv1.RegisterPathRulesServer(srv, f)
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

func TestAscPathRulesPage(t *testing.T) {
	f := startPathRules(t)

	t.Run("an administrator sees the rules, the revision and blank rows", func(t *testing.T) {
		ctx, rec := secretsReq(t, "GET", 2, perm.AccessModeAdmin, nil)
		AscPathRules(ctx)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		body := rec.Body.String()
		assert.Contains(t, body, `data-testid="asc-path-rules-rows"`)
		assert.Contains(t, body, `value="secret"`)
		assert.Equal(t, 4, strings.Count(body, `name="subject"`), "one stored rule and three blank rows")
		assert.Contains(t, body, `name="revision" value="1"`)
	})

	t.Run("a person who does not administer the repository is refused before asc is asked", func(t *testing.T) {
		f.mu.Lock()
		before := f.calls
		f.mu.Unlock()
		for _, access := range []perm.AccessMode{perm.AccessModeRead, perm.AccessModeWrite} {
			ctx, rec := secretsReq(t, "GET", 4, access, nil)
			AscPathRules(ctx)
			assert.Equal(t, http.StatusForbidden, rec.Code)
			assert.NotContains(t, rec.Body.String(), `value="secret"`)
			ctx, rec = secretsReq(t, "POST", 4, access, url.Values{"dir": {""}, "subject": {"*"}, "level": {"admin"}, "revision": {"1"}, "op": {uuid.NewString()}})
			AscPathRulesSave(ctx)
			assert.Equal(t, http.StatusForbidden, rec.Code)
			ctx, rec = secretsReq(t, "POST", 4, access, url.Values{"principal": {"user:user4"}, "repo_level": {"read"}, "paths": {"secret"}})
			AscPathRulesPreview(ctx)
			assert.Equal(t, http.StatusForbidden, rec.Code)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		assert.Equal(t, before, f.calls, "asc was not called")
	})

	t.Run("the rows are saved with the revision and op; empty rows are dropped", func(t *testing.T) {
		op := uuid.Must(uuid.NewV7()).String()
		ctx, rec := secretsReq(t, "POST", 2, perm.AccessModeAdmin, url.Values{
			"dir":      {"/secret/", "docs", ""},
			"subject":  {"*", "team:backend", ""},
			"level":    {"none", "build", "none"},
			"revision": {"1"}, "op": {op},
		})
		AscPathRulesSave(ctx)
		assert.Equal(t, http.StatusSeeOther, rec.Code)
		assert.Contains(t, flashesOf(ctx), "asc_review.pr_saved")
		f.mu.Lock()
		defer f.mu.Unlock()
		require.Len(t, f.rules, 2)
		assert.Equal(t, "secret", f.rules[0].GetDir(), "slashes around a folder go")
		assert.Equal(t, "build", f.rules[1].GetLevel())
		assert.Equal(t, uint64(2), f.revision)
		assert.Equal(t, op, f.ops[len(f.ops)-1])
	})

	t.Run("a stale revision is refused and nothing changes", func(t *testing.T) {
		ctx, rec := secretsReq(t, "POST", 2, perm.AccessModeAdmin, url.Values{"dir": {""}, "subject": {"*"}, "level": {"admin"}, "revision": {"1"}, "op": {uuid.NewString()}})
		AscPathRulesSave(ctx)
		assert.Equal(t, http.StatusSeeOther, rec.Code)
		assert.Contains(t, flashesOf(ctx), "asc_review.pr_stale")
		f.mu.Lock()
		defer f.mu.Unlock()
		assert.Len(t, f.rules, 2)
	})

	t.Run("a bad row is named and the rows stay on the page", func(t *testing.T) {
		f.mu.Lock()
		before := f.calls
		f.mu.Unlock()
		ctx, rec := secretsReq(t, "POST", 2, perm.AccessModeAdmin, url.Values{"dir": {"ok", "../up"}, "subject": {"*", "*"}, "level": {"read", "read"}, "revision": {"2"}, "op": {uuid.NewString()}})
		AscPathRulesSave(ctx)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.Contains(t, rec.Body.String(), "row 2")
		assert.Contains(t, rec.Body.String(), `value="../up"`)
		for _, form := range []url.Values{
			{"dir": {"a"}, "subject": {"*", "*"}, "level": {"read"}, "revision": {"2"}, "op": {uuid.NewString()}},
			{"dir": {"a"}, "subject": {"*"}, "level": {"read"}, "revision": {"x"}, "op": {uuid.NewString()}},
		} {
			ctx, rec = secretsReq(t, "POST", 2, perm.AccessModeAdmin, form)
			AscPathRulesSave(ctx)
			assert.Equal(t, http.StatusSeeOther, rec.Code)
			assert.Contains(t, flashesOf(ctx), "asc_review.pr_bad_form")
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		assert.Equal(t, before, f.calls, "asc was not called")
	})

	t.Run("preview shows the level per path and who decided", func(t *testing.T) {
		ctx, rec := secretsReq(t, "POST", 2, perm.AccessModeAdmin, url.Values{"principal": {"user:user4"}, "repo_level": {"write"}, "paths": {"secret/keys\r\ndocs\n\n"}})
		AscPathRulesPreview(ctx)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		body := rec.Body.String()
		assert.Contains(t, body, `data-testid="asc-path-rules-preview"`)
		assert.Contains(t, body, "<code>secret/keys</code>")
		assert.Contains(t, body, "asc_review.pr_level_none")
		assert.Contains(t, body, "<code>docs</code>")
	})

	t.Run("a bad viewer is refused", func(t *testing.T) {
		ctx, rec := secretsReq(t, "POST", 2, perm.AccessModeAdmin, url.Values{"principal": {"team:x"}, "repo_level": {"read"}, "paths": {"a"}})
		AscPathRulesPreview(ctx)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.NotContains(t, rec.Body.String(), `data-testid="asc-path-rules-preview"`)
	})
}
