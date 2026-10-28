// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascsync"
)

// Versions dropdown, interdiff and "since my last review" (SPEC section 20.5 flow 2, PLAN unit C37).
//
// The versions of a stacked pull request are the versions of its asc change, and asc owns them
// (Changes.Versions). Forgejo shows "version N of M", a dropdown, and the diff between any two versions
// (Changes.InterDiff). "Changes since your last review" is the interdiff from the version whose git commit
// is the commit_id of the reviewer's last review to the latest version.
//
// Rules that hold for every caller:
//   - The change comes from the pull request's own pull_request_asc_stack row, never from a request, so a
//     caller who may read this pull request cannot ask for the versions of another change. The handler must
//     have checked read access to the pull request before calling.
//   - Version numbers are checked against the list asc returned for that change; a number outside it is
//     refused before InterDiff is called.
//   - Every asc answer is verified. A list that is not 1..N in order, or has an id of the wrong length, is
//     refused and nothing from it is shown. Sizes are bounded.
//   - Errors returned here never carry server messages.

const (
	// ascVersionsTimeout bounds the Versions pages of one request.
	ascVersionsTimeout = 30 * time.Second
	// ascInterDiffTimeout bounds one InterDiff stream.
	ascInterDiffTimeout = 2 * time.Minute
	// AscMaxVersions bounds the versions one change may list.
	AscMaxVersions = 10_000
	// ascMaxVersionPages bounds the pages read to collect them.
	ascMaxVersionPages = 100
	// AscMaxInterDiffBytes bounds the patch text of one interdiff; more is cut and flagged Truncated.
	AscMaxInterDiffBytes = 8 << 20
	// AscMaxInterDiffPaths bounds the paths an interdiff may be limited to.
	AscMaxInterDiffPaths = 1000
	// ascMaxPathBytes bounds one such path.
	ascMaxPathBytes = 4096
)

var (
	// ErrAscNotStacked means the pull request is not the pull request of an asc change.
	ErrAscNotStacked = errors.New("pull request is not a stacked change")
	// ErrAscBadVersion means a version number is not one of the change's versions.
	ErrAscBadVersion = errors.New("no such version")
	// ErrAscBadAnswer means asc's answer failed a check and was not used.
	ErrAscBadAnswer = errors.New("asc sent an answer that failed verification")
	// ErrAscUnavailable means asc could not answer.
	ErrAscUnavailable = errors.New("asc could not answer")
	// ErrAscBadPaths means the path filter of an interdiff is not acceptable.
	ErrAscBadPaths = errors.New("bad path filter")
)

// AscVersion is one verified version of a change.
type AscVersion struct {
	N         uint32
	GitCommit string // 40 hex digits: the commit the version had in the git view
	At        int64  // unix seconds
	Actor     string
}

// AscVersionList is the verified versions of a pull request's change, oldest first.
type AscVersionList struct {
	Versions []AscVersion
}

// Latest is the newest version number, 0 when the list is empty.
func (l *AscVersionList) Latest() uint32 {
	if l == nil || len(l.Versions) == 0 {
		return 0
	}
	return l.Versions[len(l.Versions)-1].N
}

// Newest returns the versions newest first, the order the versions panel lists them in.
func (l *AscVersionList) Newest() []AscVersion {
	if l == nil {
		return nil
	}
	out := make([]AscVersion, len(l.Versions))
	for i, v := range l.Versions {
		out[len(l.Versions)-1-i] = v
	}
	return out
}

// Has reports whether n is one of the versions.
func (l *AscVersionList) Has(n uint32) bool {
	return l != nil && n >= 1 && int(n) <= len(l.Versions)
}

// AscVersionStep is the interdiff from one version to the next.
type AscVersionStep struct {
	From, To uint32
}

// Steps returns the interdiffs between neighbouring versions, oldest first. A list of one version has none.
func (l *AscVersionList) Steps() []AscVersionStep {
	if l == nil || len(l.Versions) < 2 {
		return nil
	}
	steps := make([]AscVersionStep, 0, len(l.Versions)-1)
	for i := 1; i < len(l.Versions); i++ {
		steps = append(steps, AscVersionStep{From: l.Versions[i-1].N, To: l.Versions[i].N})
	}
	return steps
}

// ByGitCommit returns the version whose git commit is commit (hex, any case), or 0.
func (l *AscVersionList) ByGitCommit(commit string) uint32 {
	if l == nil || len(commit) != 40 {
		return 0
	}
	b, err := hex.DecodeString(commit)
	if err != nil {
		return 0
	}
	want := hex.EncodeToString(b)
	// Newest first: a commit that two versions share is the later one.
	for i := len(l.Versions) - 1; i >= 0; i-- {
		if l.Versions[i].GitCommit == want {
			return l.Versions[i].N
		}
	}
	return 0
}

// AscInterDiff is the patch between two versions.
type AscInterDiff struct {
	From, To  uint32
	Patch     []byte
	Paths     []string // the paths asc reported as changed
	Truncated bool
}

var (
	ascChangesMu  sync.Mutex
	ascChangesCli ascv1.ChangesClient

	// ascChangesFor returns the Changes client. Tests replace it.
	ascChangesFor = func() (ascv1.ChangesClient, error) {
		ascChangesMu.Lock()
		defer ascChangesMu.Unlock()
		if ascChangesCli == nil {
			if setting.InternalToken == "" {
				return nil, errors.New("the internal token is not set")
			}
			c, err := ascclient.New(ascclient.Config{Target: setting.Asc.ServerURL, InternalToken: setting.InternalToken, CallTimeout: 30 * time.Second})
			if err != nil {
				return nil, fmt.Errorf("asc client: %w", err)
			}
			ascChangesCli = c.Changes()
		}
		return ascChangesCli, nil
	}

	// ascRepoNameOf returns the asc repository name of pr's base repository, or "" when it is not on asc.
	// Tests replace it.
	ascRepoNameOf = func(ctx context.Context, pr *issues_model.PullRequest) (string, error) {
		if !setting.Asc.Enabled {
			return "", nil
		}
		if err := pr.LoadBaseRepo(ctx); err != nil {
			return "", err
		}
		if sb, err := gitrepo.StorageBackendOf(pr.BaseRepo); err != nil || sb != gitrepo.StorageBackendAsc {
			return "", err
		}
		return ascbackend.RepoName(setting.RepoRootPath, pr.BaseRepo.RepoPath())
	}
)

// ascChangeOf returns the repository name and change id bytes of pr, or ErrAscNotStacked.
func ascChangeOf(ctx context.Context, pr *issues_model.PullRequest) (string, []byte, error) {
	link, err := issues_model.GetPullRequestAscStack(ctx, pr.ID)
	if err != nil {
		return "", nil, err
	}
	if link == nil || !issues_model.IsAscChange(link.AscChange) {
		return "", nil, ErrAscNotStacked
	}
	name, err := ascRepoNameOf(ctx, pr)
	if err != nil {
		return "", nil, err
	}
	if name == "" {
		return "", nil, ErrAscNotStacked
	}
	raw := make([]byte, 16)
	for i := range raw {
		raw[i] = (('z' - link.AscChange[2*i]) << 4) | ('z' - link.AscChange[2*i+1])
	}
	return name, raw, nil
}

// AscVersions returns the verified versions of the change of pr.
func AscVersions(ctx context.Context, pr *issues_model.PullRequest) (*AscVersionList, error) {
	name, change, err := ascChangeOf(ctx, pr)
	if err != nil {
		return nil, err
	}
	api, err := ascChangesFor()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAscUnavailable, err)
	}
	ctx, cancel := context.WithTimeout(ascclient.WithSystem(ctx), ascVersionsTimeout)
	defer cancel()
	var all []*ascv1.Version
	token := ""
	for page := 0; ; page++ {
		if page >= ascMaxVersionPages {
			return nil, fmt.Errorf("%w: too many pages", ErrAscBadAnswer)
		}
		resp, err := api.Versions(ctx, &ascv1.VersionsRequest{Repo: &ascv1.RepoRef{Name: name}, Change: change, PageToken: token})
		if err != nil {
			return nil, ErrAscUnavailable
		}
		all = append(all, resp.GetVersions()...)
		if len(all) > AscMaxVersions {
			return nil, fmt.Errorf("%w: too many versions", ErrAscBadAnswer)
		}
		token = resp.GetNextPageToken()
		if token == "" {
			break
		}
	}
	return verifyAscVersions(all)
}

func init() {
	// The review summary that goes back to asc names the newest version a review looked at (reviewed_version).
	ascsync.RegisterVersionCommits(func(ctx context.Context, pr *issues_model.PullRequest) ([]string, error) {
		list, err := AscVersions(ctx, pr)
		if err != nil {
			return nil, err
		}
		commits := make([]string, len(list.Versions))
		for i, v := range list.Versions {
			commits[i] = v.GitCommit
		}
		return commits, nil
	})
}

// verifyAscVersions checks that the versions are numbered 1..N in order and carry ids of the right length.
func verifyAscVersions(in []*ascv1.Version) (*AscVersionList, error) {
	out := &AscVersionList{Versions: make([]AscVersion, 0, len(in))}
	for i, v := range in {
		if v == nil || v.GetV() != uint32(i+1) {
			return nil, fmt.Errorf("%w: versions are not numbered 1..N", ErrAscBadAnswer)
		}
		if len(v.GetGit()) != 20 || len(v.GetCs()) != 32 {
			return nil, fmt.Errorf("%w: bad id length", ErrAscBadAnswer)
		}
		out.Versions = append(out.Versions, AscVersion{N: v.GetV(), GitCommit: hex.EncodeToString(v.GetGit()), At: v.GetAt(), Actor: v.GetActor()})
	}
	return out, nil
}

// AscInterDiffOf returns the patch from version from to version to of the change of pr, limited to paths when
// given. Both numbers must be versions of the change and from must differ from to.
func AscInterDiffOf(ctx context.Context, pr *issues_model.PullRequest, from, to uint32, paths []string) (*AscInterDiff, error) {
	if err := checkAscPaths(paths); err != nil {
		return nil, err
	}
	versions, err := AscVersions(ctx, pr)
	if err != nil {
		return nil, err
	}
	if !versions.Has(from) || !versions.Has(to) || from == to {
		return nil, ErrAscBadVersion
	}
	name, change, err := ascChangeOf(ctx, pr)
	if err != nil {
		return nil, err
	}
	api, err := ascChangesFor()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrAscUnavailable, err)
	}
	ctx, cancel := context.WithTimeout(ascclient.WithSystem(ctx), ascInterDiffTimeout)
	defer cancel()
	st, err := api.InterDiff(ctx, &ascv1.InterDiffRequest{
		Repo: &ascv1.RepoRef{Name: name}, Change: change, FromV: from, ToV: to,
		Format: ascv1.DiffFormat_DIFF_FORMAT_PATCH, Paths: paths,
	})
	if err != nil {
		return nil, ErrAscUnavailable
	}
	res := &AscInterDiff{From: from, To: to}
	var buf bytes.Buffer
	seen := map[string]struct{}{}
	for {
		msg, err := st.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, ErrAscUnavailable
		}
		for _, c := range msg.GetChanges() {
			p := c.GetPath()
			if len(p) == 0 || len(p) > ascMaxPathBytes || bytes.IndexByte(p, 0) >= 0 {
				return nil, fmt.Errorf("%w: changed path is invalid", ErrAscBadAnswer)
			}
			if _, dup := seen[string(p)]; !dup && len(seen) < AscMaxInterDiffPaths*10 {
				seen[string(p)] = struct{}{}
				res.Paths = append(res.Paths, string(p))
			}
		}
		if msg.GetTruncated() {
			res.Truncated = true
		}
		text := msg.GetText()
		if room := AscMaxInterDiffBytes - buf.Len(); len(text) > room {
			buf.Write(text[:room])
			res.Truncated = true
			break
		}
		buf.Write(text)
	}
	res.Patch = buf.Bytes()
	return res, nil
}

func checkAscPaths(paths []string) error {
	if len(paths) > AscMaxInterDiffPaths {
		return ErrAscBadPaths
	}
	for _, p := range paths {
		if p == "" || len(p) > ascMaxPathBytes || bytes.IndexByte([]byte(p), 0) >= 0 {
			return ErrAscBadPaths
		}
	}
	return nil
}

// AscSinceLastReview returns the version the reviewer last reviewed (0 when none can be identified) and the
// versions of the change. The reviewer's last published review (approve, comment or reject) names a commit; it
// is "since" a version only when that commit is the git commit of one of the versions.
func AscSinceLastReview(ctx context.Context, pr *issues_model.PullRequest, reviewerID int64) (uint32, *AscVersionList, error) {
	versions, err := AscVersions(ctx, pr)
	if err != nil {
		return 0, nil, err
	}
	seen, err := AscSeenVersion(ctx, pr, reviewerID, versions)
	if err != nil {
		return 0, nil, err
	}
	return seen, versions, nil
}

// AscSeenVersion is AscSinceLastReview for a caller that already holds the verified versions of pr: it returns
// the version the reviewer last reviewed, or 0.
func AscSeenVersion(ctx context.Context, pr *issues_model.PullRequest, reviewerID int64, versions *AscVersionList) (uint32, error) {
	if reviewerID == 0 {
		return 0, nil
	}
	if err := pr.LoadIssue(ctx); err != nil {
		return 0, err
	}
	review, err := issues_model.GetReviewByIssueIDUserIDAndTypes(ctx, pr.Issue.ID, reviewerID,
		[]issues_model.ReviewType{issues_model.ReviewTypeApprove, issues_model.ReviewTypeComment, issues_model.ReviewTypeReject})
	if err != nil {
		if issues_model.IsErrReviewNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	return versions.ByGitCommit(review.CommitID), nil
}

// AscChangesSinceLastReview is the interdiff from the version the reviewer last reviewed to the latest. It
// returns nil (and no error) when there is nothing to show: no earlier review, a review of a commit that is
// not a version, or a review of the latest version.
func AscChangesSinceLastReview(ctx context.Context, pr *issues_model.PullRequest, reviewerID int64, paths []string) (*AscInterDiff, error) {
	seen, versions, err := AscSinceLastReview(ctx, pr, reviewerID)
	if err != nil {
		return nil, err
	}
	if seen == 0 || seen >= versions.Latest() {
		return nil, nil
	}
	return AscInterDiffOf(ctx, pr, seen, versions.Latest(), paths)
}
