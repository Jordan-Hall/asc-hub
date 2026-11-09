// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"net"
	"net/http"
	"testing"

	"forgejo.org/models/db"
	issues_model "forgejo.org/models/issues"
	packages_model "forgejo.org/models/packages"
	"forgejo.org/models/perm"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

type fakeProvenanceReleases struct {
	ascv1.UnimplementedReleasesServer
	releases []*ascv1.Release
}

func (f *fakeProvenanceReleases) List(gocontext.Context, *ascv1.ListReleasesRequest) (*ascv1.ListReleasesResponse, error) {
	return &ascv1.ListReleasesResponse{Releases: f.releases}, nil
}

func useFakeProvenanceReleases(t *testing.T, releases ...*ascv1.Release) {
	t.Helper()
	srv := grpc.NewServer()
	ascv1.RegisterReleasesServer(srv, &fakeProvenanceReleases{releases: releases})
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
}

func signedRelease(tag string, draft, valid bool) *ascv1.Release {
	return &ascv1.Release{Tag: tag, Draft: draft, Provenance: &ascv1.ReleaseProvenance{SignedBy: "user2", SigningKey: "SHA256:k3y", SignatureValid: valid}}
}

const (
	prov225Commit = "1a8823cd1a9549fde083f992f6b9b87a7ab74fb3"
	prov225Tagged = "65f1bf27bc3bf70f64657658635e66094edbcb4d"
)

func TestAscPackageProvenance(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	user4 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})

	pv := &packages_model.PackageVersion{ID: 9401, Version: "1.2.0"}
	assert.Nil(t, ascPackageProvenance(ctx, user2, pv), "no provenance, no line")

	require.NoError(t, packages_model.SetAscProvenance(ctx, pv.ID, &packages_model.AscProvenance{RepoID: 1, RunID: 791, Commit: prov225Commit}))

	t.Run("a commit that is no landed stack, a run of another repository", func(t *testing.T) {
		p := ascPackageProvenance(ctx, user2, pv)
		require.NotNil(t, p)
		assert.Equal(t, "user2/repo1", p.Repo.FullName())
		assert.Equal(t, "1a8823cd1a", p.Commit)
		assert.Equal(t, "/user2/repo1/commit/"+prov225Commit, p.CommitLink)
		assert.Empty(t, p.RunLink, "run 791 is a run of repository 4")
		assert.False(t, p.Landed)
		assert.False(t, p.Signed)
	})

	t.Run("a run of the repository links to it", func(t *testing.T) {
		require.NoError(t, packages_model.SetAscProvenance(ctx, 9402, &packages_model.AscProvenance{RepoID: 4, RunID: 791, Commit: "c2d72f548424103f01ee1dc02889c1e2bff816b0"}))
		user5 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})
		p := ascPackageProvenance(ctx, user5, &packages_model.PackageVersion{ID: 9402, Version: "1"})
		require.NotNil(t, p)
		assert.Equal(t, int64(187), p.RunIndex)
		assert.Equal(t, "/user5/repo4/actions/runs/187", p.RunLink)
	})

	t.Run("the commit a stack landed as", func(t *testing.T) {
		pr := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
		require.NoError(t, issues_model.SetPullRequestAscStack(ctx, pr, "mmmmmmmmmmmmmmmmmmmmmmmmmmmmmmmm", 0))
		p := ascPackageProvenance(ctx, user2, pv)
		require.NotNil(t, p)
		assert.True(t, p.Landed)
		assert.Equal(t, "/user2/repo1/pulls/2", p.StackLink)
	})

	t.Run("signed by the release of the version at the same commit, never by a draft or a bad signature", func(t *testing.T) {
		setAscBackend(t, 1, "asc")
		defer setAscBackend(t, 1, "git")
		// Release v1.1 of repository 1 is at commit 65f1bf27bc (fixture); v1.2.0 has no release in the hub.
		useFakeProvenanceReleases(t, signedRelease("1.1", true, true), signedRelease("v1.1", false, true), signedRelease("v1.2.0", false, true))
		require.NoError(t, packages_model.SetAscProvenance(ctx, 9403, &packages_model.AscProvenance{RepoID: 1, Commit: prov225Tagged}))
		at := &packages_model.PackageVersion{ID: 9403, Version: "1.1"}
		p := ascPackageProvenance(ctx, user2, at)
		require.NotNil(t, p)
		assert.True(t, p.Signed)
		assert.Equal(t, "user2", p.SignedBy)
		assert.Equal(t, "SHA256:k3y", p.SigningKey)
		assert.Equal(t, "v1.1", p.ReleaseTag)
		assert.Equal(t, "/user2/repo1/releases/tag/v1.1", p.ReleaseURL)

		require.NoError(t, packages_model.SetAscProvenance(ctx, 9406, &packages_model.AscProvenance{RepoID: 1, Commit: prov225Commit}))
		p = ascPackageProvenance(ctx, user2, &packages_model.PackageVersion{ID: 9406, Version: "1.1"})
		require.NotNil(t, p)
		assert.False(t, p.Signed, "a version published from another commit than the release's is not vouched for")
		assert.False(t, ascPackageProvenance(ctx, user2, pv).Signed, "v1.2.0 has no release in the hub")

		useFakeProvenanceReleases(t, signedRelease("v1.1", false, false))
		assert.False(t, ascPackageProvenance(ctx, user2, at).Signed, "the signature is not valid")

		useFakeProvenanceReleases(t, signedRelease("v1.1", false, true))
		_, err := db.GetEngine(db.DefaultContext).Where("repo_id = ? AND type = ?", 1, unit.TypeReleases).Delete(&repo_model.RepoUnit{})
		require.NoError(t, err)
		p = ascPackageProvenance(ctx, user2, at)
		require.NotNil(t, p, "the code is still readable")
		assert.False(t, p.Signed, "the releases are not")
	})

	t.Run("nothing about a repository the person cannot read", func(t *testing.T) {
		require.NoError(t, packages_model.SetAscProvenance(ctx, 9404, &packages_model.AscProvenance{RepoID: 2, RunID: 1, Commit: prov225Commit}))
		private := &packages_model.PackageVersion{ID: 9404, Version: "1"}
		assert.Nil(t, ascPackageProvenance(ctx, user4, private))
		assert.Nil(t, ascPackageProvenance(ctx, nil, private))
		assert.NotNil(t, ascPackageProvenance(ctx, user2, private), "the owner reads it")

		require.NoError(t, packages_model.SetAscProvenance(ctx, 9405, &packages_model.AscProvenance{RepoID: 99999, Commit: prov225Commit}))
		assert.Nil(t, ascPackageProvenance(ctx, user2, &packages_model.PackageVersion{ID: 9405}), "a deleted repository")
	})
}

// The head of the version page carries the provenance line and the signed chip.
func TestPackageViewShowsProvenance(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	pkg, err := packages_model.TryInsertPackage(db.DefaultContext, &packages_model.Package{OwnerID: 2, Name: "lib", LowerName: "lib", Type: packages_model.TypeGeneric})
	require.NoError(t, err)
	pv, err := packages_model.GetOrInsertVersion(db.DefaultContext, &packages_model.PackageVersion{PackageID: pkg.ID, CreatorID: 2, Version: "1.1", LowerVersion: "1.1", MetadataJSON: "null"})
	require.NoError(t, err)

	view := func(t *testing.T) string {
		t.Helper()
		pd, err := packages_model.GetPackageDescriptor(db.DefaultContext, pv)
		require.NoError(t, err)
		ctx, resp := contexttest.MockContext(t, "/user2/-/packages/generic/lib/1.1")
		ctx.Render = templates.HTMLRenderer()
		ctx.Doer, ctx.IsSigned = user2, true
		ctx.ContextUser = user2
		ctx.Package = &context.Package{Owner: user2, AccessMode: perm.AccessModeOwner, Descriptor: pd}
		ViewPackageVersion(ctx)
		require.Equal(t, http.StatusOK, resp.Code)
		return resp.Body.String()
	}

	assert.NotContains(t, view(t), `data-testid="package-provenance"`, "a version uploaded by a person")

	require.NoError(t, packages_model.SetAscProvenance(db.DefaultContext, pv.ID, &packages_model.AscProvenance{RepoID: 1, RunID: 791, Commit: prov225Tagged}))
	setAscBackend(t, 1, "asc")
	defer setAscBackend(t, 1, "git")
	useFakeProvenanceReleases(t, signedRelease("v1.1", false, true))
	body := view(t)
	assert.Contains(t, body, `data-testid="package-provenance"`)
	assert.Contains(t, body, "packages.asc_published_from_commit")
	assert.Contains(t, body, `data-testid="package-signed"`)
	assert.Contains(t, body, `href="/user2/repo1/releases/tag/v1.1"`)
}
