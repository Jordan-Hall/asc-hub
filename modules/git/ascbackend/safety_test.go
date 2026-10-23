// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/git/ascbackend"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Everything the server says is checked. Each case corrupts one answer and expects an error that says
// the answer failed verification, never a partial or wrong result.
func TestBadAnswersAreRefused(t *testing.T) {
	flip := func(b []byte) []byte {
		out := append([]byte(nil), b...)
		out[len(out)-1] ^= 0xff
		return out
	}
	cases := map[string]struct {
		method string
		tamper func(resp any)
		run    func(fx *fixture) error
	}{
		"raw commit does not hash to its id": {
			"ReadChangeset",
			func(resp any) { r := resp.(*ascv1.ReadChangesetResponse); r.RawGitCommit = append(r.RawGitCommit, 'x') },
			func(fx *fixture) error { _, err := fx.asc.ReadCommit(fx.t.Context(), fx.id("c2")); return err },
		},
		"header names another commit": {
			"ReadChangeset",
			func(resp any) {
				r := resp.(*ascv1.ReadChangesetResponse)
				r.Header.Git = oidBytes(strings.Repeat("ab", 20))
			},
			func(fx *fixture) error { _, err := fx.asc.ReadCommit(fx.t.Context(), fx.id("c2")); return err },
		},
		"header disagrees with the raw commit": {
			"ReadChangeset",
			func(resp any) {
				r := resp.(*ascv1.ReadChangesetResponse)
				r.RootGit = oidBytes(strings.Repeat("cd", 20))
			},
			func(fx *fixture) error { _, err := fx.asc.ReadCommit(fx.t.Context(), fx.id("c2")); return err },
		},
		"parents disagree with the raw commit": {
			"ReadChangeset",
			func(resp any) { r := resp.(*ascv1.ReadChangesetResponse); r.Header.Parents = nil },
			func(fx *fixture) error { _, err := fx.asc.ReadCommit(fx.t.Context(), fx.id("c2")); return err },
		},
		"short object id": {
			"ReadChangeset",
			func(resp any) { r := resp.(*ascv1.ReadChangesetResponse); r.RootGit = r.RootGit[:19] },
			func(fx *fixture) error { _, err := fx.asc.ReadCommit(fx.t.Context(), fx.id("c2")); return err },
		},
		"entry name with a slash": {
			"ReadTree",
			func(resp any) { r := resp.(*ascv1.ReadTreeResponse); r.Entries[0].Name = []byte("a/b") },
			func(fx *fixture) error { _, err := fx.asc.ReadTree(fx.t.Context(), fx.id("c4")); return err },
		},
		"entry with an impossible mode": {
			"ReadTree",
			func(resp any) { r := resp.(*ascv1.ReadTreeResponse); r.Entries[0].Mode = 0o100666 },
			func(fx *fixture) error { _, err := fx.asc.ReadTree(fx.t.Context(), fx.id("c4")); return err },
		},
		"entry with a short id": {
			"ReadTree",
			func(resp any) { r := resp.(*ascv1.ReadTreeResponse); r.Entries[0].Git = r.Entries[0].Git[:4] },
			func(fx *fixture) error { _, err := fx.asc.ReadTree(fx.t.Context(), fx.id("c4")); return err },
		},
		"entry outside the directory": {
			"ReadTree",
			func(resp any) {
				for _, e := range resp.(*ascv1.ReadTreeResponse).Entries {
					if strings.HasPrefix(string(e.Path), "dir/") {
						e.Path = []byte("elsewhere/x")
					}
				}
			},
			func(fx *fixture) error {
				ctx := fx.t.Context()
				if _, err := fx.asc.ReadTree(ctx, fx.id("c4")); err != nil {
					return err
				}
				_, err := fx.asc.ReadTree(ctx, fx.id("treeDir"))
				return err
			},
		},
		"listing of a different tree": {
			"ReadTree",
			func(resp any) { r := resp.(*ascv1.ReadTreeResponse); r.TreeGit = flip(r.TreeGit) },
			func(fx *fixture) error {
				// The commit is read first, so the tree its listing must have is known.
				if _, err := fx.asc.ReadCommit(fx.t.Context(), fx.id("c4")); err != nil {
					return err
				}
				_, err := fx.asc.ReadTree(fx.t.Context(), fx.id("c4"))
				return err
			},
		},
		"blob content does not hash to its id": {
			"ReadFile",
			func(resp any) { r := resp.(*ascv1.ReadFileResponse); r.Data = flip(r.Data) },
			func(fx *fixture) error {
				rc, _, err := fx.asc.OpenBlob(fx.t.Context(), fx.id("main"), -1)
				if err != nil {
					return err
				}
				defer rc.Close()
				_, err = io.ReadAll(rc)
				return err
			},
		},
		"blob stream carries more than its size": {
			"ReadFile",
			func(resp any) { r := resp.(*ascv1.ReadFileResponse); r.Data = append(r.Data, 'x') },
			func(fx *fixture) error {
				rc, _, err := fx.asc.OpenBlob(fx.t.Context(), fx.id("main"), -1)
				if err != nil {
					return err
				}
				defer rc.Close()
				_, err = io.ReadAll(rc)
				return err
			},
		},
		"blob stream changes size": {
			"ReadFile",
			func(resp any) { r := resp.(*ascv1.ReadFileResponse); r.TotalSize++ },
			func(fx *fixture) error {
				rc, _, err := fx.asc.OpenBlob(fx.t.Context(), fx.id("big"), -1)
				if err != nil {
					return err
				}
				defer rc.Close()
				_, err = io.ReadAll(rc)
				return err
			},
		},
		"blob stream ends early": {
			"ReadFile",
			func(resp any) { r := resp.(*ascv1.ReadFileResponse); r.Data = r.Data[:len(r.Data)/2] },
			func(fx *fixture) error {
				rc, _, err := fx.asc.OpenBlob(fx.t.Context(), fx.id("big"), -1)
				if err != nil {
					return err
				}
				defer rc.Close()
				_, err = io.ReadAll(rc)
				return err
			},
		},
		"reference without a git id": {
			"GitMap",
			func(resp any) { r := resp.(*ascv1.GitMapResponse); r.AscToGit[0] = nil },
			func(fx *fixture) error {
				_, err := fx.asc.ListRefs(fx.t.Context(), git.RefQuery{Prefix: "refs/heads/"})
				return err
			},
		},
		"reference answers do not line up": {
			"GitMap",
			func(resp any) { r := resp.(*ascv1.GitMapResponse); r.AscToGit = r.AscToGit[:1] },
			func(fx *fixture) error {
				_, err := fx.asc.ListRefs(fx.t.Context(), git.RefQuery{Prefix: "refs/heads/"})
				return err
			},
		},
		"reference with a bad name": {
			"ListRefs",
			func(resp any) { r := resp.(*ascv1.ListRefsResponse); r.Refs[0].Name = "heads/x" },
			func(fx *fixture) error {
				_, err := fx.asc.ListRefs(fx.t.Context(), git.RefQuery{Prefix: "refs/heads/"})
				return err
			},
		},
		"blame runs leave a gap": {
			"Blame",
			func(resp any) { r := resp.(*ascv1.BlameResponse); r.Hunks[0].Start = 1 },
			func(fx *fixture) error {
				for _, err := range fx.asc.Blame(fx.t.Context(), fx.id("c4"), "main.go", git.BlameOpts{}) {
					return err
				}
				return nil
			},
		},
		"last commits answers for a name nobody asked": {
			"LastCommits",
			func(resp any) {
				r := resp.(*ascv1.LastCommitsResponse)
				for _, e := range r.ByName {
					r.ByName["intruder"] = e
					return
				}
			},
			func(fx *fixture) error {
				_, err := fx.asc.LastCommits(fx.t.Context(), fx.id("c4"), "", []string{"main.go"})
				return err
			},
		},
		"diff is truncated": {
			"Diff",
			func(resp any) { r := resp.(*ascv1.DiffResponse); r.Truncated = true },
			func(fx *fixture) error {
				return fx.asc.UnifiedDiff(fx.t.Context(), fx.id("c1"), fx.id("c4"), git.DiffOpts{}, io.Discard)
			},
		},
		"resolve answers for two revisions": {
			"Resolve",
			func(resp any) { r := resp.(*ascv1.ResolveResponse); r.Ids = append(r.Ids, r.Ids[0]) },
			func(fx *fixture) error {
				_, _, err := fx.asc.Resolve(fx.t.Context(), "master")
				return err
			},
		},
		"stat answers for no objects": {
			"Stat",
			func(resp any) { r := resp.(*ascv1.StatResponse); r.Exists = nil },
			func(fx *fixture) error {
				_, err := fx.asc.StatObjects(fx.t.Context(), []git.ObjectID{fx.id("treeC1")})
				return err
			},
		},
		"attribute answers do not line up": {
			"CheckAttr",
			func(resp any) { r := resp.(*ascv1.CheckAttrResponse); r.PerPath = nil },
			func(fx *fixture) error {
				_, err := fx.asc.Attributes(fx.t.Context(), fx.id("c4"), []string{"main.go"}, []string{"text"})
				return err
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			fx := newFixture(t, ascbackend.Options{})
			fx.fake.tamper = func(method string, resp any) {
				if method == c.method {
					c.tamper(resp)
				}
			}
			err := c.run(fx)
			require.Error(t, err)
			want := ascbackend.ErrBadAnswer
			if name == "diff is truncated" {
				want = ascbackend.ErrTooLarge
			}
			assert.ErrorIs(t, err, want, "%v", err)
		})
	}
}

func TestStatusCodesMapToErrors(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	ctx := t.Context()
	secret := "Bearer s3cr3t-token \x1b[31m" + strings.Repeat("x", 1000)

	for method, code := range map[string]codes.Code{
		"ReadChangeset": codes.PermissionDenied,
		"Resolve":       codes.Unauthenticated,
	} {
		fx.fake.fail[method] = status.Error(code, secret)
	}
	_, err := fx.asc.ReadCommit(ctx, fx.id("c1"))
	require.Error(t, err)
	assert.False(t, git.IsErrNotExist(err), "a refusal is not a missing object")
	var ascErr *ascbackend.Error
	require.ErrorAs(t, err, &ascErr)
	assert.Equal(t, codes.PermissionDenied, ascErr.Code)
	assert.NotContains(t, err.Error(), "\x1b")
	assert.Less(t, len(err.Error()), 400, "the server's message is cut short")
	_, _, err = fx.asc.Resolve(ctx, "master")
	require.ErrorAs(t, err, &ascErr)
	assert.Equal(t, codes.Unauthenticated, ascErr.Code)

	delete(fx.fake.fail, "ReadChangeset")
	fx.fake.fail["ReadChangeset"] = status.Error(codes.ResourceExhausted, "too big")
	_, err = fx.asc.ReadCommit(ctx, fx.id("c1"))
	assert.ErrorIs(t, err, ascbackend.ErrTooLarge)
	fx.fake.fail["ReadChangeset"] = status.Error(codes.Unimplemented, "no")
	_, err = fx.asc.ReadCommit(ctx, fx.id("c1"))
	assert.ErrorIs(t, err, ascbackend.ErrUnsupported)
	fx.fake.fail["ReadChangeset"] = status.Error(codes.Unavailable, "down")
	_, err = fx.asc.ReadCommit(ctx, fx.id("c1"))
	require.Error(t, err)
	assert.NotErrorIs(t, err, ascbackend.ErrUnsupported)
	assert.False(t, git.IsErrNotExist(err), "an outage must never read as a missing commit")
}

func TestContextAndClose(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := fx.asc.ReadCommit(ctx, fx.id("c1"))
	assert.ErrorIs(t, err, context.Canceled)
	_, err = fx.asc.ListRefs(ctx, git.RefQuery{})
	assert.ErrorIs(t, err, context.Canceled)

	require.NoError(t, fx.asc.Close())
	_, err = fx.asc.ReadCommit(t.Context(), fx.id("c1"))
	assert.ErrorIs(t, err, ascbackend.ErrClosed)
	_, _, err = fx.asc.OpenBlob(t.Context(), fx.id("main"), -1)
	assert.ErrorIs(t, err, ascbackend.ErrClosed)
}

func TestWrongRepositoryAndObjectFormat(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	other, err := ascbackend.New(fx.client, "owner/other", fx.repo, ascbackend.Options{Auth: func(ctx context.Context) (context.Context, error) { return ctx, nil }})
	require.NoError(t, err)
	_, err = other.ReadCommit(t.Context(), fx.id("c1"))
	var ascErr *ascbackend.Error
	require.ErrorAs(t, err, &ascErr)
	assert.Equal(t, codes.NotFound, ascErr.Code)
	assert.False(t, git.IsErrNotExist(err), "a repository that asc does not have is not a missing commit")

	sha256ID := git.Sha256ObjectFormat.EmptyObjectID()
	_, err = fx.asc.ReadCommit(t.Context(), sha256ID)
	require.Error(t, err)
	_, _, err = fx.asc.OpenBlob(t.Context(), sha256ID, -1)
	require.Error(t, err)
	assert.Equal(t, git.Sha1ObjectFormat, fx.asc.ObjectFormat())

	for _, name := range []string{"", "owner", "owner/", "/repo", "a/b/c", "../x", "owner/repo.wiki", strings.Repeat("a", 300) + "/b", "ow ner/x"} {
		_, err := ascbackend.New(fx.client, name, fx.repo, ascbackend.Options{})
		require.Error(t, err, name)
	}
	_, err = ascbackend.New(nil, "owner/repo", fx.repo, ascbackend.Options{})
	assert.ErrorIs(t, err, ascbackend.ErrNotConfigured)
}

// The default credential is the internal token: reads made through the real ascclient carry it, and a
// server that wants another token refuses the read.
func TestDefaultCredentialIsTheInternalToken(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	fx.fake.token = "internal-secret"
	_, addr := fx.fake.serve()

	for token, wantOK := range map[string]bool{"internal-secret": true, "wrong-token": false} {
		client, err := ascclient.New(ascclient.Config{Target: addr, InternalToken: token})
		require.NoError(t, err)
		t.Cleanup(func() { _ = client.Close() })
		be, err := ascbackend.New(client.Repo(), fakeRepoName, fx.repo, ascbackend.Options{})
		require.NoError(t, err)
		_, err = be.ReadCommit(t.Context(), fx.id("c2"))
		if wantOK {
			require.NoError(t, err)
			continue
		}
		require.Error(t, err)
		assert.False(t, git.IsErrNotExist(err))
		assert.NotContains(t, err.Error(), token)
	}

	// No internal token at all: the client refuses before anything is sent.
	client, err := ascclient.New(ascclient.Config{Target: addr})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	be, err := ascbackend.New(client.Repo(), fakeRepoName, fx.repo, ascbackend.Options{})
	require.NoError(t, err)
	_, err = be.ReadCommit(t.Context(), fx.id("c2"))
	assert.ErrorIs(t, err, ascclient.ErrNoInternalToken)
}

func TestAuthFailureStopsTheCall(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	be := fx.newAsc(t, ascbackend.Options{Auth: func(ctx context.Context) (context.Context, error) {
		return nil, assert.AnError
	}})
	_, err := be.ReadCommit(t.Context(), fx.id("c1"))
	assert.ErrorIs(t, err, assert.AnError)
	assert.Zero(t, fx.fake.callCount("ReadChangeset"))
}

// A repository opened by name with no client configured never reads from anywhere else.
func TestRegisteredBackendFailsClosedWithoutAClient(t *testing.T) {
	ascbackend.Reset()
	t.Cleanup(ascbackend.Reset)
	fx := newFixture(t, ascbackend.Options{})
	_, err := git.OpenRepositoryWithBackend(t.Context(), fx.dir, ascbackend.Name)
	assert.ErrorIs(t, err, ascbackend.ErrNotConfigured)
}
