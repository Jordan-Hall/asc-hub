// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"strconv"

	actions_model "forgejo.org/models/actions"
	issues_model "forgejo.org/models/issues"
	packages_model "forgejo.org/models/packages"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
	"forgejo.org/services/asctrust"
)

// The head of a package version says where it was published from (design board "Packages": "Published from
// acme/payments by a landed stack", "Signed with release key").
//
// Hub data first: the repository, run and commit are what the hub recorded when an Actions job published the version
// (packages_model.AscProvenance), and "a landed stack" is a merged pull request of an asc stack that landed as that
// commit. Only the signature is asc's: the release of the repository tagged with the version (or "v" and the version), cut
// from the commit the version was published from and whose signature asc checked as valid (asctrust.ListReleaseRecords);
// it is shown to people who can read the releases. Nothing is shown about a repository the person cannot read the code of.

// AscPackageProvenance is the provenance line of a package version.
type AscPackageProvenance struct {
	Repo       *repo_model.Repository
	Commit     string // short
	CommitLink string
	RunIndex   int64 // 0 when the run is gone or the person cannot see runs
	RunLink    string
	StackLink  string // the pull request of the landed stack change, or "" when the commit is not one
	Landed     bool
	Signed     bool
	SignedBy   string
	SigningKey string
	ReleaseTag string
	ReleaseURL string
}

func ascPackageProvenance(ctx gocontext.Context, doer *user_model.User, pv *packages_model.PackageVersion) *AscPackageProvenance {
	p, err := packages_model.GetAscProvenance(ctx, pv.ID)
	if err != nil {
		log.Error("packages: the provenance of version %d: %v", pv.ID, err)
	}
	if p == nil {
		return nil
	}
	repo, err := repo_model.GetRepositoryByID(ctx, p.RepoID)
	if err != nil {
		return nil // the repository was deleted
	}
	perm, err := access_model.GetUserRepoPermission(ctx, repo, doer)
	if err != nil || !perm.CanRead(unit.TypeCode) {
		return nil
	}
	out := &AscPackageProvenance{Repo: repo, Commit: util.TruncateRunes(p.Commit, 10), CommitLink: repo.CommitLink(p.Commit)}
	if perm.CanRead(unit.TypeActions) {
		if run, err := actions_model.GetRunByID(ctx, p.RunID); err == nil && run.RepoID == repo.ID {
			out.RunIndex = run.Index
			out.RunLink = repo.Link() + "/actions/runs/" + strconv.FormatInt(run.Index, 10)
		}
	}
	if landed, err := issues_model.ListLandedAscStackPulls(ctx, repo.ID, []string{p.Commit}); err == nil && len(landed) > 0 {
		out.Landed = true
		if perm.CanRead(unit.TypePullRequests) {
			if pr, err := issues_model.GetPullRequestByID(ctx, landed[len(landed)-1].PullID); err == nil {
				out.StackLink = repo.Link() + "/pulls/" + strconv.FormatInt(pr.Index, 10)
			}
		}
	}
	if setting.Asc.Enabled && perm.CanRead(unit.TypeReleases) {
		records := asctrust.ListReleaseRecords(ctx, repo)
		for _, tag := range []string{pv.Version, "v" + pv.Version} {
			rec := records[tag]
			if rec == nil || rec.Draft || !rec.SignatureOK || rec.SignedBy == "" {
				continue
			}
			// The signature vouches for the commit the release was cut from, as asc recorded it (a tag can be moved;
			// that record cannot): it says something about the version only when the version was published from that
			// very commit.
			commit, err := asctrust.ReleaseCommit(ctx, repo, rec)
			if err != nil {
				log.Warn("packages: the commit of release %s of %-v: %v", rec.Tag, repo, err)
			}
			if err != nil || commit == "" || commit != p.Commit {
				continue
			}
			out.Signed, out.SignedBy, out.SigningKey, out.ReleaseTag = true, rec.SignedBy, rec.SigningKey, rec.Tag
			out.ReleaseURL = repo.Link() + "/releases/tag/" + util.PathEscapeSegments(rec.Tag)
			break
		}
	}
	return out
}
