// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascdownload

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const ghManifest = `{"version":"1.4.2","files":[{"name":"asc-1.4.2-x86_64-unknown-linux-gnu.tar.gz","os":"linux","arch":"x86_64","kind":"archive","sha256":"` +
	"0000000000000000000000000000000000000000000000000000000000000000" + `","size":10}],"checksums":"SHA256SUMS"}`

func ghServer(t *testing.T, calls *atomic.Int32, status int, assets string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/Jordan-Hall/asc/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_, _ = w.Write([]byte(`{"tag_name":"v1.4.2","draft":false,"prerelease":false,"assets":[` + assets + `]}`))
	})
	mux.HandleFunc("/Jordan-Hall/asc/releases/download/v1.4.2/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(ghManifest))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestSource(srv *httptest.Server, clock *time.Time) *GitHubSource {
	s := NewGitHubSource("Jordan-Hall/asc", nil)
	s.APIURL, s.ReleaseHost = srv.URL, srv.URL
	s.Now = func() time.Time { return *clock }
	return s
}

const goodAssets = `{"name":"manifest.json"},{"name":"asc-1.4.2-x86_64-unknown-linux-gnu.tar.gz"},{"name":"SHA256SUMS"}`

func TestGitHubLatest(t *testing.T) {
	var calls atomic.Int32
	srv := ghServer(t, &calls, http.StatusOK, goodAssets)
	clock := time.Unix(1_800_000_000, 0)
	s := newTestSource(srv, &clock)

	rel, err := s.Latest()
	require.NoError(t, err)
	assert.Equal(t, "v1.4.2", rel.Tag)
	assert.Equal(t, srv.URL+"/Jordan-Hall/asc/releases/download/v1.4.2/", rel.BaseURL)
	assert.Equal(t, "1.4.2", rel.Manifest.Version)
	assert.Equal(t, srv.URL+"/Jordan-Hall/asc/releases/tag/v1.4.2", rel.Manifest.ReleaseNotesURL)

	// cached while fresh
	_, err = s.Latest()
	require.NoError(t, err)
	assert.Equal(t, int32(1), calls.Load())
}

func TestGitHubNoRelease(t *testing.T) {
	var calls atomic.Int32
	srv := ghServer(t, &calls, http.StatusNotFound, "")
	clock := time.Unix(1_800_000_000, 0)
	s := newTestSource(srv, &clock)

	_, err := s.Latest()
	require.ErrorIs(t, err, ErrNoGitHubRelease)
	assert.True(t, IsNoRelease(err))
	// the failure is remembered for a minute
	_, _ = s.Latest()
	assert.Equal(t, int32(1), calls.Load())
	clock = clock.Add(2 * time.Minute)
	_, _ = s.Latest()
	assert.Equal(t, int32(2), calls.Load())
}

func TestGitHubRefusesDeadLinks(t *testing.T) {
	var calls atomic.Int32
	// the archive named by the manifest is not attached to the release
	srv := ghServer(t, &calls, http.StatusOK, `{"name":"manifest.json"},{"name":"SHA256SUMS"}`)
	clock := time.Unix(1_800_000_000, 0)
	s := newTestSource(srv, &clock)
	_, err := s.Latest()
	require.Error(t, err)
	assert.False(t, IsNoRelease(err))
	assert.Contains(t, err.Error(), "does not have")
}

func TestGitHubKeepsOldReleaseWhenGitHubFails(t *testing.T) {
	var calls atomic.Int32
	status := http.StatusOK
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/Jordan-Hall/asc/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_, _ = w.Write([]byte(`{"tag_name":"v1.4.2","assets":[` + goodAssets + `]}`))
	})
	mux.HandleFunc("/Jordan-Hall/asc/releases/download/v1.4.2/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(ghManifest))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	clock := time.Unix(1_800_000_000, 0)
	s := newTestSource(srv, &clock)
	_, err := s.Latest()
	require.NoError(t, err)

	status = http.StatusForbidden
	clock = clock.Add(GitHubFreshFor + time.Minute)
	rel, err := s.Latest() // stale answer now, refresh in the background
	require.NoError(t, err)
	assert.Equal(t, "v1.4.2", rel.Tag)
	require.Eventually(t, func() bool { return calls.Load() >= 2 }, 3*time.Second, 10*time.Millisecond)
	rel, err = s.Latest()
	require.NoError(t, err)
	assert.Equal(t, "v1.4.2", rel.Tag)
}

func TestValidRepo(t *testing.T) {
	assert.True(t, ValidRepo("Jordan-Hall/asc"))
	for _, bad := range []string{"", "asc", "a/b/c", "a/..", "a b/c", "../x"} {
		assert.False(t, ValidRepo(bad), bad)
	}
}
