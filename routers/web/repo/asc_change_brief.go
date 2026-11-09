// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"fmt"

	issues_model "forgejo.org/models/issues"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/modules/util"
	"forgejo.org/services/context"
)

// The "Where to look" and "Risk" lines of the change page (board Changeset). Both are the brief of the pull request
// the change landed through: the pointers its author wrote and the risk class asc gave the change (Review.GetBrief,
// the same answer the review page shows). The hub links each pointer to the file at the landed commit, so a pointer
// opens the code as it landed, not as the branch is now.

// changeBrief is what the change page shows of the brief.
type changeBrief struct {
	Where       []AscBriefWhere
	RiskLevel   string // "low", "medium", "high" or ""
	RiskNote    string // the author's "what could go wrong"
	RiskReasons []string
}

// changeBriefOf keeps the parts of a brief the change page shows, with each pointer linked to the file at the commit.
// It is nil when the brief has neither pointers nor a risk.
func changeBriefOf(resp *ascv1.GetBriefResponse, repoLink, commitID string) (*changeBrief, error) {
	brief, err := ascBriefOf(resp,
		func(uint64) string { return "" },
		func(path string, line uint32) string {
			l := fmt.Sprintf("%s/src/commit/%s/%s", repoLink, util.PathEscapeSegments(commitID), util.PathEscapeSegments(path))
			if line > 0 {
				l += fmt.Sprintf("#L%d", line)
			}
			return l
		})
	if err != nil {
		return nil, err
	}
	out := &changeBrief{Where: brief.Where, RiskLevel: brief.RiskLevel, RiskNote: brief.RiskNote, RiskReasons: brief.RiskReasons}
	if len(out.Where) == 0 && out.RiskLevel == "" && out.RiskNote == "" {
		return nil, nil
	}
	return out, nil
}

// prepareAscChangeBrief puts the "Where to look" and "Risk" lines of the landed pull request pr into
// ctx.Data["ChangeBrief"]. The caller has checked that the viewer may read pull requests. Without asc, without a brief
// or on any asc problem it sets nothing.
func prepareAscChangeBrief(ctx *context.Context, pr *issues_model.PullRequest, commitID string) {
	resp, _, _, _, ok := fetchAscBrief(ctx, pr)
	if !ok {
		return
	}
	brief, err := changeBriefOf(resp, ctx.Repo.RepoLink, commitID)
	if err != nil {
		log.Error("change page: brief of %s#%d: %v", ctx.Repo.Repository.FullName(), pr.Index, err)
		return
	}
	if brief != nil {
		ctx.Data["ChangeBrief"] = brief
	}
}
