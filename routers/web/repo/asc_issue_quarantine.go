// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	issues_model "forgejo.org/models/issues"
	"forgejo.org/modules/log"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// ascIssueQuarantineChip is the chip "Quarantined" of an issue row: the issue names a failure group of its repository
// that is quarantined, so it is probably the issue that explains why the test does not hold up landing.
type ascIssueQuarantineChip struct {
	Test string
	Link string
	Note string
}

// ascGroupIssueBody is the body of a new issue opened from a failure group: the address of the group, which links the
// issue to the group (asctrust.GroupRefs).
func ascGroupIssueBody(ctx *context.Context, owner, repo string, id uint64) string {
	return ctx.Locale.TrString("asc_trust.checks.issue_body", asctrust.GroupURL(owner, repo, id))
}

// prepareAscIssueQuarantine puts the "Quarantined" chips of the rows of an issue list of the repository into
// .AscIssueQuarantine (by issue id). Failure groups are CI data, so only a reader of the code and of the Actions sees
// them (trustweb.CheckReader); asc is asked only when an issue on the page names a group. It never fails the page.
func prepareAscIssueQuarantine(ctx *context.Context, list issues_model.IssueList) {
	if len(list) == 0 {
		return
	}
	repo, name, ok := trustweb.CheckReader(ctx)
	if !ok {
		return
	}
	refs := map[int64][]uint64{}
	want := map[uint64]bool{}
	for _, issue := range list {
		if issue.RepoID != repo.ID {
			continue
		}
		ids := asctrust.GroupRefs(issue.Content, repo.OwnerName, repo.Name)
		if len(ids) == 0 {
			continue
		}
		refs[issue.ID] = ids
		for _, id := range ids {
			want[id] = true
		}
	}
	if len(want) == 0 || !asctrust.HasFeature(ctx, asctrust.FeatureCiInsights) {
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		log.Warn("asc issue quarantine: no connection to asc serve: %v", err)
		return
	}
	callCtx, release, err := trustweb.ReadCall(ctx, repo)
	if err != nil {
		log.Warn("asc issue quarantine: credential: %v", err)
		return
	}
	defer release()
	groups, err := asctrust.LoadQuarantinedOf(callCtx, conn, name, want)
	if err != nil {
		log.Warn("asc issue quarantine: %v", err)
		return
	}
	chips := map[int64]ascIssueQuarantineChip{}
	for issueID, ids := range refs {
		for _, id := range ids {
			if g, ok := groups[id]; ok {
				chips[issueID] = ascIssueQuarantineChip{Test: firstNonEmptyString(g.TestName, g.Title), Link: asctrust.GroupURL(repo.OwnerName, repo.Name, id), Note: g.Note}
				break
			}
		}
	}
	if len(chips) > 0 {
		ctx.Data["AscIssueQuarantine"] = chips
	}
}
