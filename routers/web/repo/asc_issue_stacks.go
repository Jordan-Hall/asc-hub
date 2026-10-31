// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	issues_model "forgejo.org/models/issues"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// prepareAscIssueStacks puts the stacks that name the issue into the page: the rail "Linked stacks" and, for a closed issue
// that a landed stack fixed, the banner "Closed by a stack". It never fails the page.
func prepareAscIssueStacks(ctx *context.Context, issue *issues_model.Issue) {
	if !setting.Asc.Enabled {
		return
	}
	stacks, err := pull_service.AscStacksOfIssue(ctx, issue)
	if err != nil {
		log.Warn("asc issue stacks of issue %d: %v", issue.ID, err)
		return
	}
	if len(stacks) == 0 {
		return
	}
	ctx.Data["AscIssueStacks"] = stacks
	if closing := pull_service.AscClosingStackLanded(ctx, issue, stacks); closing != nil {
		ctx.Data["AscClosedByStack"] = closing
	}
}

// prepareAscIssueChips puts the "Fixed by stack" chips of the rows of an issue list into .AscIssueChips (by issue id). It never
// fails the page.
func prepareAscIssueChips(ctx *context.Context, list issues_model.IssueList) {
	if !setting.Asc.Enabled || len(list) == 0 {
		return
	}
	chips, err := pull_service.AscIssueStackChips(ctx, list)
	if err != nil {
		log.Warn("asc issue stack chips: %v", err)
		return
	}
	ctx.Data["AscIssueChips"] = chips
}
