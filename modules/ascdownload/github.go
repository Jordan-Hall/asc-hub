// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascdownload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// This file reads the latest release of a public GitHub repository, so that /download and the landing page can link
// to the files of that release on GitHub itself (nothing is copied to the hub). The release carries manifest.json
// (see docs/RELEASE.md of asc); it is fetched from the release, validated like any manifest, and its files are linked
// below https://github.com/<owner>/<repo>/releases/download/<tag>/.
//
// The answer is cached: a good answer is served for GitHubFreshFor and, when GitHub cannot be reached, for
// GitHubStaleFor more; a failure (no release, rate limit, network) is remembered for GitHubRetryAfter, so a page
// view never waits for GitHub twice in a row. The page that finds the answer stale gets the old one at once and the
// refresh runs in the background.

const (
	// GitHubFreshFor is how long a release answer is used without asking GitHub again.
	GitHubFreshFor = 10 * time.Minute
	// GitHubStaleFor is how long an old good answer is still shown when GitHub does not answer.
	GitHubStaleFor = 24 * time.Hour
	// GitHubRetryAfter is how long a failure is remembered before GitHub is asked again.
	GitHubRetryAfter = time.Minute

	maxReleaseJSONBytes = 4 << 20
	githubTimeout       = 10 * time.Second
)

// ErrNoGitHubRelease means the repository has no published (non draft, non pre-release) release yet.
var ErrNoGitHubRelease = fmt.Errorf("%w: the repository has no published release yet", ErrNoManifest)

var (
	repoRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$`)
	tagRe  = regexp.MustCompile(`^v?[0-9]{1,6}\.[0-9]{1,6}\.[0-9]{1,6}(-[0-9A-Za-z][0-9A-Za-z.-]{0,30})?$`)
)

// ValidRepo reports whether s is an owner/name pair of a GitHub repository.
func ValidRepo(s string) bool { return repoRe.MatchString(s) && !strings.Contains(s, "..") }

// GitHubRelease is a published release of the repository, ready for BuildOffers.
type GitHubRelease struct {
	Tag      string
	Manifest *Manifest
	// BaseURL is the address the files of the release are linked below; it ends in a slash.
	BaseURL string
	// PageURL is the release page on GitHub.
	PageURL string
	// AllReleasesURL lists every release.
	AllReleasesURL string
}

// GitHubSource asks GitHub for the latest release of one repository.
type GitHubSource struct {
	Repo string
	// APIURL and ReleaseHost are api.github.com and github.com; tests point them elsewhere.
	APIURL      string
	ReleaseHost string
	Client      *http.Client
	// Now is time.Now; tests replace it.
	Now func() time.Time

	fetchMu    sync.Mutex
	mu         sync.Mutex
	value      *GitHubRelease
	valueAt    time.Time
	err        error
	errAt      time.Time
	refreshing bool
}

// NewGitHubSource returns a source for repo ("owner/name") that talks to GitHub.
func NewGitHubSource(repo string, transport http.RoundTripper) *GitHubSource {
	return &GitHubSource{
		Repo:        repo,
		APIURL:      "https://api.github.com",
		ReleaseHost: "https://github.com",
		Client:      &http.Client{Timeout: githubTimeout, Transport: transport},
		Now:         time.Now,
	}
}

// Latest returns the latest release, from the cache when it is fresh. With an old answer in hand it returns that
// and refreshes in the background; with none it asks GitHub and waits (for at most githubTimeout).
func (s *GitHubSource) Latest() (*GitHubRelease, error) {
	s.mu.Lock()
	now := s.Now()
	if s.value != nil && now.Sub(s.valueAt) > GitHubStaleFor {
		s.value = nil
	}
	if v := s.value; v != nil {
		if now.Sub(s.valueAt) >= GitHubFreshFor && !s.refreshing && (s.err == nil || now.Sub(s.errAt) >= GitHubRetryAfter) {
			s.refreshing = true
			go s.refresh()
		}
		s.mu.Unlock()
		return v, nil
	}
	if s.err != nil && now.Sub(s.errAt) < GitHubRetryAfter {
		err := s.err
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Unlock()
	s.refresh()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.value != nil {
		return s.value, nil
	}
	return nil, s.err
}

// refresh asks GitHub and stores the answer; one ask at a time. A failure keeps the old good value.
func (s *GitHubSource) refresh() {
	s.fetchMu.Lock()
	defer s.fetchMu.Unlock()
	s.mu.Lock()
	if now := s.Now(); (s.value != nil && now.Sub(s.valueAt) < GitHubFreshFor) || (s.value == nil && s.err != nil && now.Sub(s.errAt) < GitHubRetryAfter) {
		s.refreshing = false
		s.mu.Unlock()
		return // someone else just asked
	}
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), githubTimeout)
	defer cancel()
	rel, err := s.fetch(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshing = false
	now := s.Now()
	if err != nil {
		s.err, s.errAt = err, now
		if errors.Is(err, ErrNoGitHubRelease) {
			s.value = nil // the release was withdrawn
		}
		return
	}
	s.value, s.valueAt, s.err = rel, now, nil
}

type ghRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	HTMLURL    string `json:"html_url"`
	Assets     []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

func (s *GitHubSource) get(ctx context.Context, rawURL, accept string, limit int64) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "asc-hub")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, 0, errors.New("GitHub is not reachable")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading the answer of GitHub: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, resp.StatusCode, errors.New("the answer of GitHub is too large")
	}
	return data, resp.StatusCode, nil
}

// fetch reads the latest release and its manifest.
func (s *GitHubSource) fetch(ctx context.Context) (*GitHubRelease, error) {
	if !ValidRepo(s.Repo) {
		return nil, errors.New("the release repository is not an owner/name pair")
	}
	data, status, err := s.get(ctx, s.APIURL+"/repos/"+s.Repo+"/releases/latest", "application/vnd.github+json", maxReleaseJSONBytes)
	if err != nil {
		return nil, err
	}
	switch {
	case status == http.StatusNotFound:
		return nil, ErrNoGitHubRelease
	case status == http.StatusForbidden || status == http.StatusTooManyRequests:
		return nil, errors.New("GitHub is limiting requests from this hub; the release is shown again later")
	case status != http.StatusOK:
		return nil, fmt.Errorf("GitHub answered %d", status)
	}
	var rel ghRelease
	if err := json.Unmarshal(data, &rel); err != nil {
		return nil, errors.New("the release answer of GitHub is not readable")
	}
	if rel.Draft || rel.Prerelease {
		return nil, ErrNoGitHubRelease
	}
	if !tagRe.MatchString(rel.TagName) {
		return nil, fmt.Errorf("the release tag %q is not a version tag", clip(rel.TagName))
	}
	assets := map[string]bool{}
	for _, a := range rel.Assets {
		assets[a.Name] = true
	}
	if !assets[ManifestName] {
		return nil, fmt.Errorf("the release %s has no %s", rel.TagName, ManifestName)
	}
	base := s.ReleaseHost + "/" + s.Repo + "/releases/download/" + url.PathEscape(rel.TagName) + "/"
	body, status, err := s.get(ctx, base+ManifestName, "application/json", maxManifestBytes)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered %d for %s", status, ManifestName)
	}
	m, err := Parse(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if strings.TrimPrefix(rel.TagName, "v") != m.Version {
		return nil, fmt.Errorf("the tag %s and the manifest version %s differ", rel.TagName, m.Version)
	}
	// only offer what the release really has: a listed file that is not attached would be a dead link
	for _, f := range m.Files {
		if !assets[f.Name] {
			return nil, fmt.Errorf("the manifest lists %s, which the release does not have", f.Name)
		}
		if f.Signature != "" && !assets[f.Signature] {
			return nil, fmt.Errorf("the manifest lists %s, which the release does not have", f.Signature)
		}
	}
	if (m.Checksums != "" && !assets[m.Checksums]) || (m.ChecksumsSignature != "" && !assets[m.ChecksumsSignature]) {
		return nil, errors.New("the checksum files named by the manifest are not attached to the release")
	}
	page := s.ReleaseHost + "/" + s.Repo + "/releases/tag/" + url.PathEscape(rel.TagName)
	if !m.hasValidNotes() {
		m.ReleaseNotesURL = page
	}
	return &GitHubRelease{
		Tag: rel.TagName, Manifest: m, BaseURL: base, PageURL: page,
		AllReleasesURL: s.ReleaseHost + "/" + s.Repo + "/releases",
	}, nil
}

func (m *Manifest) hasValidNotes() bool {
	return m.ReleaseNotesURL != "" && validHTTPSURL(m.ReleaseNotesURL)
}

var (
	sourcesMu sync.Mutex
	sources   = map[string]*GitHubSource{}
)

// SharedGitHubSource returns the one source of repo (with its cache) that every page of the hub uses.
func SharedGitHubSource(repo string, transport http.RoundTripper) *GitHubSource {
	sourcesMu.Lock()
	defer sourcesMu.Unlock()
	if s, ok := sources[repo]; ok {
		return s
	}
	s := NewGitHubSource(repo, transport)
	sources[repo] = s
	return s
}
