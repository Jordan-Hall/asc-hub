// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/storage"
	notify_service "forgejo.org/services/notify"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The Releases service of asc serve keeps the record of a release: tag, notes written from the changes that landed, the provenance
// (the run that built it, its checks, the signature on the release commit) and the SHA-256 of every asset. The hub keeps the
// release itself (git tag, attachments, page); a release published on the hub is also made in asc, as the person who
// published it, so `asc release list` and `asc release show` agree with the hub. asc has no call to change a release, so
// the record is made once, when the release is first published; a later edit of the notes on the hub stays on the hub.
// The pages read the asc record for what only asc knows: where it was built, how the checks went, who signed it, and the
// checksum of each asset.

const (
	releaseCallTimeout = 10 * time.Second
	// releaseMaxAssets is the most assets asc takes for a release.
	releaseMaxAssets = 100
	// releaseMaxAssetBytes is the largest asset that is hashed for the record; a larger file is left out of it.
	releaseMaxAssetBytes = 2 << 30
	releaseListLimit     = 100
)

var releaseTagShape = regexp.MustCompile(`^[A-Za-z0-9._+/-]{1,128}$`)

// ReleaseAsset is an asset of a release as the pages show it.
type ReleaseAsset struct {
	Name   string
	SHA256 string
}

// ReleaseRecord is what asc knows about a release and the pages show.
type ReleaseRecord struct {
	Tag          string
	BuildRun     string
	BuildURL     string // https only, or ""
	BuiltFrom    string
	ChecksPassed uint32
	ChecksTotal  uint32
	SignedBy     string
	SigningKey   string
	SignatureOK  bool
	Assets       map[string]string // asset name -> sha256

	// What a release that exists only in asc (made with `asc release create`) needs to be listed on the hub.
	Title      string
	Notes      string
	Draft      bool
	Prerelease bool
	Created    int64
	Published  int64
	CreatedBy  string
	AssetList  []ReleaseAssetInfo
}

// ReleaseAssetInfo is an asset of a release in asc; URL is https where the file can be downloaded, or "".
type ReleaseAssetInfo struct {
	Name   string
	Size   uint64
	SHA256 string
	URL    string
}

// Time is when the release was published, or created when it is a draft.
func (r *ReleaseRecord) Time() int64 {
	if r.Published > 0 {
		return r.Published
	}
	return r.Created
}

// releaseRecordOf cleans the answer of asc.
func releaseRecordOf(r *ascv1.Release) *ReleaseRecord {
	if r == nil || !releaseTagShape.MatchString(r.GetTag()) {
		return nil
	}
	out := &ReleaseRecord{
		Tag: r.GetTag(), Assets: map[string]string{},
		Title: CleanText(r.GetTitle(), 200), Notes: CleanBlock(r.GetNotes(), 64<<10),
		Draft: r.GetDraft(), Prerelease: r.GetChannel() == ascv1.ReleaseChannel_RELEASE_CHANNEL_PRE_RELEASE,
		Created: r.GetCreated(), Published: r.GetPublished(), CreatedBy: CleanText(r.GetCreatedBy(), 80),
	}
	if p := r.GetProvenance(); p != nil {
		out.BuildRun = CleanText(p.GetBuildRun(), 80)
		if u, err := url.Parse(p.GetBuildUrl()); err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil {
			out.BuildURL = u.String()
		}
		out.BuiltFrom = CleanText(p.GetBuiltFrom(), 120)
		out.ChecksPassed, out.ChecksTotal = p.GetChecksPassed(), p.GetChecksTotal()
		out.SignedBy = CleanText(p.GetSignedBy(), 80)
		out.SigningKey = CleanText(p.GetSigningKey(), 120)
		out.SignatureOK = p.GetSignatureValid()
	}
	for i, a := range r.GetAssets() {
		if i >= releaseMaxAssets {
			break
		}
		if sum := strings.ToLower(a.GetSha256()); len(sum) == 64 && isHex(sum) && a.GetName() != "" {
			out.Assets[a.GetName()] = sum
			info := ReleaseAssetInfo{Name: CleanText(a.GetName(), 200), Size: a.GetSize(), SHA256: sum}
			if u, err := url.Parse(a.GetUrl()); err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil {
				info.URL = u.String()
			}
			if info.Name != "" && !strings.ContainsAny(info.Name, "/\\") {
				out.AssetList = append(out.AssetList, info)
			}
		}
	}
	return out
}

// ListReleaseRecords returns the asc records of the releases of repo by tag. Without asc it returns nothing and no error:
// the page then shows what the hub knows. The caller has checked that the person may read the repository.
func ListReleaseRecords(ctx context.Context, repo *repo_model.Repository) map[string]*ReleaseRecord {
	name, err := RepoName(repo)
	if err != nil {
		return nil
	}
	conn, err := Conn()
	if err != nil {
		return nil
	}
	cctx, cancel := SystemContext(ctx, releaseCallTimeout)
	defer cancel()
	resp, err := ascv1.NewReleasesClient(conn).List(cctx, &ascv1.ListReleasesRequest{Repo: RepoRef(name), Limit: releaseListLimit})
	if err != nil {
		if status.Code(err) != codes.Unimplemented && status.Code(err) != codes.NotFound {
			log.Warn("asc releases: List for %s: %v", name, err)
		}
		return nil
	}
	out := make(map[string]*ReleaseRecord, len(resp.GetReleases()))
	for _, r := range resp.GetReleases() {
		if rec := releaseRecordOf(r); rec != nil {
			out[rec.Tag] = rec
		}
	}
	return out
}

// ReleaseDraft is the notes and the tag asc proposes for the next release.
type ReleaseDraft struct {
	LatestTag   string `json:"latest_tag"`
	ProposedTag string `json:"proposed_tag"`
	DraftTag    string `json:"draft_tag"`
	Changes     uint32 `json:"changes"`
	Notes       string `json:"notes"`
	Truncated   bool   `json:"truncated"`
}

// DraftRelease asks asc for the proposed tag and the notes written from the changes since the previous release (from empty:
// the newest published release). The caller has checked that the person may write releases.
func DraftRelease(ctx context.Context, repo *repo_model.Repository, from, to string) (*ReleaseDraft, error) {
	name, err := RepoName(repo)
	if err != nil {
		return nil, err
	}
	for _, s := range []string{from, to} {
		if len(s) > 255 || strings.ContainsAny(s, "\x00\r\n") {
			return nil, ErrBadInput
		}
	}
	conn, err := Conn()
	if err != nil {
		return nil, err
	}
	api := ascv1.NewReleasesClient(conn)
	cctx, cancel := SystemContext(ctx, releaseCallTimeout)
	defer cancel()
	next, err := api.Next(cctx, &ascv1.NextReleaseRequest{Repo: RepoRef(name)})
	if err != nil {
		return nil, fmt.Errorf("asctrust: Releases.Next: %w", err)
	}
	notes, err := api.DraftNotes(cctx, &ascv1.DraftReleaseNotesRequest{Repo: RepoRef(name), From: from, To: to})
	if err != nil {
		return nil, fmt.Errorf("asctrust: Releases.DraftNotes: %w", err)
	}
	d := &ReleaseDraft{
		LatestTag: CleanText(next.GetLatestTag(), 128), DraftTag: CleanText(next.GetDraftTag(), 128), Changes: next.GetChangesSince(),
		Notes: CleanBlock(notes.GetMarkdown(), MaxPolicyBytes), Truncated: notes.GetTruncated() || next.GetTruncated(),
	}
	if t := next.GetProposedTag(); releaseTagShape.MatchString(t) {
		d.ProposedTag = t
	}
	if d.Changes == 0 {
		d.Changes = notes.GetTotal()
	}
	return d, nil
}

// releaseCreateRequest builds the record of a published release of the hub. assets are already hashed.
func releaseCreateRequest(name string, rel *repo_model.Release, assets []*ascv1.ReleaseAsset) *ascv1.CreateReleaseRequest {
	req := &ascv1.CreateReleaseRequest{
		Repo: RepoRef(name), Tag: rel.TagName, Title: rel.Title, Notes: rel.Note, Target: rel.Target, Assets: assets,
		Channel: ascv1.ReleaseChannel_RELEASE_CHANNEL_STABLE,
	}
	if rel.IsPrerelease {
		req.Channel = ascv1.ReleaseChannel_RELEASE_CHANNEL_PRE_RELEASE
	}
	if len(req.Title) > 200 {
		req.Title = req.Title[:200]
	}
	if len(req.Notes) > 64<<10 {
		req.Notes = req.Notes[:64<<10]
	}
	return req
}

// releaseAssetsOf hashes the attachments of a release. An asset that is stored elsewhere, too large or unreadable is left out
// of the record (asc wants the sha256 of what it lists).
func releaseAssetsOf(rel *repo_model.Release, open func(path string) (io.ReadCloser, error)) []*ascv1.ReleaseAsset {
	var out []*ascv1.ReleaseAsset
	for _, a := range rel.Attachments {
		if len(out) >= releaseMaxAssets {
			break
		}
		if a.ExternalURL != "" || a.Size > releaseMaxAssetBytes || a.Name == "" || strings.ContainsAny(a.Name, "/\\") {
			continue
		}
		f, err := open(a.RelativePath())
		if err != nil {
			log.Warn("asc releases: attachment %s is not readable: %v", a.UUID, err)
			continue
		}
		h := sha256.New()
		n, err := io.Copy(h, io.LimitReader(f, releaseMaxAssetBytes+1))
		_ = f.Close()
		if err != nil || n > releaseMaxAssetBytes {
			continue
		}
		out = append(out, &ascv1.ReleaseAsset{
			Name: a.Name, Size: uint64(n), Sha256: hex.EncodeToString(h.Sum(nil)),
			Url: releaseAssetURL(rel, a.Name),
		})
	}
	return out
}

func releaseAssetURL(rel *repo_model.Release, name string) string {
	if rel.Repo == nil {
		return ""
	}
	return setting.AppURL + rel.Repo.FullName() + "/releases/download/" + url.PathEscape(rel.TagName) + "/" + url.PathEscape(name)
}

// releaseMirror is the part of the Releases client the mirror uses; tests replace it.
type releaseMirror interface {
	Create(ctx context.Context, in *ascv1.CreateReleaseRequest) error
}

type releaseMirrorGRPC struct{}

func (releaseMirrorGRPC) Create(ctx context.Context, in *ascv1.CreateReleaseRequest) error {
	conn, err := Conn()
	if err != nil {
		return err
	}
	_, err = ascv1.NewReleasesClient(conn).Create(ctx, in)
	return err
}

var (
	releaseSink releaseMirror = releaseMirrorGRPC{}
	// releaseCall gives the context of a call made for doer as a writer; tests replace it.
	releaseCall = func(ctx context.Context, doer *user_model.User, repo *repo_model.Repository) (context.Context, func(), error) {
		return UserCall(ctx, doer, CallRepoWrite, repo)
	}
	// releaseLoad loads the repository and the attachments of a release; tests replace it.
	releaseLoad = func(ctx context.Context, rel *repo_model.Release) error { return rel.LoadAttributes(ctx) }
	// releaseOpen opens an attachment for hashing; tests replace it.
	releaseOpen = func(path string) (io.ReadCloser, error) { return storage.Attachments.Open(path) }
	// releaseAsync runs the mirror off the request path; tests run it in line.
	releaseAsync = func(f func()) { go f() }
)

// MirrorRelease makes the asc record of a release that was published on the hub. It never fails the publishing.
func MirrorRelease(ctx context.Context, doer *user_model.User, rel *repo_model.Release) {
	if rel == nil || rel.IsDraft || rel.IsTag || !releaseTagShape.MatchString(rel.TagName) || doer == nil {
		return
	}
	if err := releaseLoad(ctx, rel); err != nil || rel.Repo == nil {
		return
	}
	name, err := RepoName(rel.Repo)
	if err != nil {
		return // not an asc repository
	}
	ctx = context.WithoutCancel(ctx)
	releaseAsync(func() {
		callCtx, release, err := releaseCall(ctx, doer, rel.Repo)
		if err != nil {
			if !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrNoDoer) {
				log.Warn("asc releases: no call for user %d: %v", doer.ID, err)
			}
			return
		}
		defer release()
		req := releaseCreateRequest(name, rel, releaseAssetsOf(rel, releaseOpen))
		cctx, cancel := context.WithTimeout(callCtx, releaseCallTimeout)
		defer cancel()
		if err := releaseSink.Create(cctx, req); err != nil {
			// a release asc has already (made with `asc release`, or published here before) is not made again
			if status.Code(err) != codes.AlreadyExists {
				log.Warn("asc releases: Create %s on %s: %v", rel.TagName, name, err)
			}
		}
	})
}

type releaseNotifier struct {
	notify_service.NullNotifier
}

var _ notify_service.Notifier = (*releaseNotifier)(nil)

func init() {
	notify_service.RegisterNotifier(&releaseNotifier{})
}

func (*releaseNotifier) NewRelease(ctx context.Context, rel *repo_model.Release) {
	if rel == nil || !setting.Asc.Enabled {
		return
	}
	publisher, err := user_model.GetUserByID(ctx, rel.PublisherID)
	if err != nil {
		return
	}
	MirrorRelease(ctx, publisher, rel)
}

// A draft that is published is an update of the release on the hub; asc learns of it the same way.
func (*releaseNotifier) UpdateRelease(ctx context.Context, doer *user_model.User, rel *repo_model.Release) {
	if rel == nil || !setting.Asc.Enabled {
		return
	}
	MirrorRelease(ctx, doer, rel)
}
