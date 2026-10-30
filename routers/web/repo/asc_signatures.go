// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// The Verified badge of a version of a change (docs/BUILDOUT.md section 5). A signature is an SSH signature over
// the changeset, kept beside it; asc says whether a live key of the author made it (Signing.Verify). The badge is
// drawn next to each version in the versions dropdown of a stacked pull request.
//
// The viewer has already been checked for read access to the pull request, so the calls use the hub's own
// credential. The badge is advice about a version, so a failure of asc leaves the dropdown without badges and never
// fails the page. Nothing in the answer is trusted before it is checked: the status, the signer and the
// fingerprint are cleaned in services/asctrust and escaped by the template.

// prepareAscSignatures puts AscSignatures (version number to asctrust.Badge) into ctx.Data for the templates.
func prepareAscSignatures(ctx *context.Context, versions *pull_service.AscVersionList) {
	ctx.Data["AscSignatures"] = ascVersionBadges(ctx, versions)
}

// ascVersionBadges is the badge of each version of versions that has one, by version number. A version asc cannot
// say anything about, and every version when asc cannot be asked, is left out.
func ascVersionBadges(ctx *context.Context, versions *pull_service.AscVersionList) map[uint32]asctrust.Badge {
	byVersion := map[uint32]asctrust.Badge{}
	if !setting.Asc.Enabled || versions == nil || len(versions.Versions) == 0 || ctx.Repo.Repository == nil {
		return byVersion
	}
	name, err := asctrust.RepoName(ctx.Repo.Repository)
	if err != nil {
		return byVersion
	}
	if !asctrust.HasFeature(ctx, asctrust.FeatureSigning) {
		return byVersion
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return byVersion
	}
	sys := asctrust.System(ctx.Req.Context())
	commits := make([]string, 0, len(versions.Versions))
	for _, v := range versions.Versions {
		commits = append(commits, v.GitCommit)
	}
	cctx, cancel := asctrust.SystemContext(ctx.Req.Context(), asctrust.CallTimeout)
	defer cancel()
	badges, err := asctrust.VerifyCommits(cctx, conn, name, commits, asctrust.RequireSigned(sys, conn, name))
	if err != nil {
		log.Warn("asc signatures of %s: %v", ctx.Repo.Repository.FullName(), asctrust.Classify(err))
		return byVersion
	}
	for _, v := range versions.Versions {
		if b, ok := badges[v.GitCommit]; ok && b.Label != "" {
			byVersion[v.N] = b
		}
	}
	return byVersion
}
