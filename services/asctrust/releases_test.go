// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeReleaseSink struct {
	mu   sync.Mutex
	got  []*ascv1.CreateReleaseRequest
	fail error
}

func (f *fakeReleaseSink) Create(_ context.Context, in *ascv1.CreateReleaseRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, in)
	return f.fail
}

func useReleaseFakes(t *testing.T, sink *fakeReleaseSink, files map[string]string) {
	t.Helper()
	t.Cleanup(test.MockVariableValue(&releaseSink, releaseMirror(sink)))
	t.Cleanup(test.MockVariableValue(&releaseAsync, func(f func()) { f() }))
	t.Cleanup(test.MockVariableValue(&releaseCall, func(ctx context.Context, _ *user_model.User, _ *repo_model.Repository) (context.Context, func(), error) {
		return ctx, func() {}, nil
	}))
	t.Cleanup(test.MockVariableValue(&releaseOpen, func(path string) (io.ReadCloser, error) {
		for uuid, content := range files {
			if strings.HasSuffix(path, uuid) {
				return io.NopCloser(strings.NewReader(content)), nil
			}
		}
		return nil, io.ErrUnexpectedEOF
	}))
}

// A release that is published on the hub is made in asc as the person, with the checksum of every asset.
func TestMirrorReleaseMakesTheRecordWithChecksums(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	useForwardedKey(t)
	repo := ascRepo(t)
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	sink := &fakeReleaseSink{}
	useReleaseFakes(t, sink, map[string]string{"uuid-a": "hello"})

	rel := &repo_model.Release{
		RepoID: repo.ID, Repo: repo, PublisherID: doer.ID, TagName: "v1.2.0", Title: "One two", Note: "### Added\n- retries", Target: "main", IsPrerelease: true,
		Attachments: []*repo_model.Attachment{
			{UUID: "uuid-a", Name: "asc-linux.tar.gz", Size: 5},
			{UUID: "uuid-b", Name: "elsewhere.zip", ExternalURL: "https://example.org/x.zip"},
			{UUID: "uuid-c", Name: "gone.bin", Size: 9},
		},
	}
	// the release is loaded with its attributes by the notifier; here they are given
	t.Cleanup(test.MockVariableValue(&releaseLoad, func(context.Context, *repo_model.Release) error { return nil }))
	MirrorRelease(t.Context(), doer, rel)

	require.Len(t, sink.got, 1)
	req := sink.got[0]
	assert.Equal(t, "user2/repo1", req.GetRepo().GetName())
	assert.Equal(t, "v1.2.0", req.GetTag())
	assert.Equal(t, ascv1.ReleaseChannel_RELEASE_CHANNEL_PRE_RELEASE, req.GetChannel())
	assert.Equal(t, "### Added\n- retries", req.GetNotes())
	require.Len(t, req.GetAssets(), 1, "an asset that lives elsewhere or cannot be read is not listed")
	assert.Equal(t, "asc-linux.tar.gz", req.GetAssets()[0].GetName())
	assert.Equal(t, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", req.GetAssets()[0].GetSha256(), "sha256 of hello")
	assert.EqualValues(t, 5, req.GetAssets()[0].GetSize())
	assert.Contains(t, req.GetAssets()[0].GetUrl(), "/releases/download/v1.2.0/asc-linux.tar.gz")
}

// Nothing is made for a draft, a bare tag, a bad tag or nobody; a release asc has already is not an error.
func TestMirrorReleaseSkipsAndNeverFails(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	useForwardedKey(t)
	repo := ascRepo(t)
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	sink := &fakeReleaseSink{}
	useReleaseFakes(t, sink, nil)
	t.Cleanup(test.MockVariableValue(&releaseLoad, func(context.Context, *repo_model.Release) error { return nil }))

	for _, rel := range []*repo_model.Release{
		{Repo: repo, TagName: "v1", IsDraft: true},
		{Repo: repo, TagName: "v1", IsTag: true},
		{Repo: repo, TagName: "bad tag"},
		nil,
	} {
		MirrorRelease(t.Context(), doer, rel)
	}
	MirrorRelease(t.Context(), nil, &repo_model.Release{Repo: repo, TagName: "v1"})
	assert.Empty(t, sink.got)

	sink.fail = status.Error(codes.AlreadyExists, "tag exists")
	MirrorRelease(t.Context(), doer, &repo_model.Release{Repo: repo, TagName: "v1"})
	assert.Len(t, sink.got, 1)
}

// The answer of asc is cleaned: a build address that is not https is dropped, a checksum that is not hex is dropped.
func TestReleaseRecordOfCleansTheAnswer(t *testing.T) {
	rec := releaseRecordOf(&ascv1.Release{
		Tag: "v1.0.0",
		Provenance: &ascv1.ReleaseProvenance{
			BuildRun: "4218", BuildUrl: "http://ci.example/run/4218", ChecksPassed: 14, ChecksTotal: 14, SignedBy: "maya", SigningKey: "7F3A", SignatureValid: true,
		},
		Assets: []*ascv1.ReleaseAsset{
			{Name: "a.tgz", Sha256: strings.Repeat("ab", 32)},
			{Name: "b.tgz", Sha256: "not hex"},
			{Name: "", Sha256: strings.Repeat("ab", 32)},
		},
	})
	require.NotNil(t, rec)
	assert.Empty(t, rec.BuildURL, "http is not shown as a link")
	assert.EqualValues(t, 14, rec.ChecksTotal)
	assert.True(t, rec.SignatureOK)
	assert.Equal(t, map[string]string{"a.tgz": strings.Repeat("ab", 32)}, rec.Assets)

	assert.Nil(t, releaseRecordOf(&ascv1.Release{Tag: "bad tag"}))
	assert.Nil(t, releaseRecordOf(nil))
	https := releaseRecordOf(&ascv1.Release{Tag: "v2", Provenance: &ascv1.ReleaseProvenance{BuildUrl: "https://ci.example/run/1"}})
	assert.Equal(t, "https://ci.example/run/1", https.BuildURL)
}
