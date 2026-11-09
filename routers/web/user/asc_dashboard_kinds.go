// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"strings"
	"time"
	"unicode/utf8"

	issues_model "forgejo.org/models/issues"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/translation"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/automerge"
)

// The kinds of the rows of "Needs you" past their section: the reply of a person on a change that came back, the
// place a ready change would take in the merge queue with the wait asc measures, and the restack asc made after the
// trunk moved. Each fact is read by a hook that tests replace; a fact that cannot be read leaves the row as it is.

const (
	// ascDashQuoteRunes is the longest reply quote a row shows; the pull request has the rest.
	ascDashQuoteRunes = 140
	// ascDashRestackWindow is how recent a restack must be to say "Trunk moved" on the row.
	ascDashRestackWindow = 24 * time.Hour
	// ascDashRestackMembers is the largest stack whose restack the dashboard reads, one ref log call per change.
	ascDashRestackMembers = 10
)

// AscDashQuote is the reply of a person on a change of the person that came back to them.
type AscDashQuote struct {
	Who  string
	Text string
}

// ascDashReplyLoad returns the newest reply of somebody else on the pull request of e: a review asking for changes
// with text, else a comment. Tests replace it.
var ascDashReplyLoad = ascDashReplyFromModels

func ascDashReplyFromModels(ctx gocontext.Context, e *AscInboxEntry) *AscDashQuote {
	pr, err := issues_model.GetPullRequestByID(ctx, e.PullID)
	if err != nil {
		return nil
	}
	if err := pr.LoadIssue(ctx); err != nil {
		return nil
	}
	poster := pr.Issue.PosterID
	reviews, err := issues_model.FindReviews(ctx, issues_model.FindReviewOptions{IssueID: pr.IssueID, Types: []issues_model.ReviewType{issues_model.ReviewTypeReject}})
	if err != nil {
		log.Warn("asc dashboard: reviews of pull request %d: %v", pr.ID, err)
		return nil
	}
	for i := len(reviews) - 1; i >= 0; i-- {
		r := reviews[i]
		if r.ReviewerID == poster || strings.TrimSpace(r.Content) == "" {
			continue
		}
		if err := r.LoadReviewer(ctx); err != nil || r.Reviewer == nil {
			continue
		}
		return ascDashQuoteOf(r.Reviewer, r.Content)
	}
	comments, err := issues_model.FindComments(ctx, &issues_model.FindCommentsOptions{IssueID: pr.IssueID, Type: issues_model.CommentTypeComment})
	if err != nil {
		log.Warn("asc dashboard: comments of pull request %d: %v", pr.ID, err)
		return nil
	}
	for i := len(comments) - 1; i >= 0; i-- {
		c := comments[i]
		if c.PosterID == poster || strings.TrimSpace(c.Content) == "" {
			continue
		}
		if err := c.LoadPoster(ctx); err != nil || c.Poster == nil {
			continue
		}
		return ascDashQuoteOf(c.Poster, c.Content)
	}
	return nil
}

// ascDashQuoteOf keeps the first line of a reply that has text, at most ascDashQuoteRunes letters.
func ascDashQuoteOf(who *user_model.User, content string) *AscDashQuote {
	text := ""
	for line := range strings.SplitSeq(content, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			text = t
			break
		}
	}
	if text == "" {
		return nil
	}
	if utf8.RuneCountInString(text) > ascDashQuoteRunes {
		text = string([]rune(text)[:ascDashQuoteRunes-1]) + "…"
	}
	return &AscDashQuote{Who: who.GetDisplayName(), Text: text}
}

// ascDashJoinLoad returns the place a ready change would take in the merge queue of its branch (1 lands next) and the
// median wait asc measures there, empty when unknown. Tests replace it.
var ascDashJoinLoad = ascDashJoinFromModels

func ascDashJoinFromModels(ctx gocontext.Context, doer *user_model.User, e *AscInboxEntry) (int, string) {
	pr, err := issues_model.GetPullRequestByID(ctx, e.PullID)
	if err != nil {
		return 0, ""
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return 0, ""
	}
	view, err := automerge.AscQueueViewOf(ctx, pr.BaseRepo, pr.BaseBranch, doer)
	if err != nil {
		log.Warn("asc dashboard: the queue of %s %s: %v", pr.BaseRepo.FullName(), pr.BaseBranch, err)
		return 0, ""
	}
	median := ""
	if setting.Asc.Enabled {
		if m, _, err := ascDashQueueTiming(ctx, pr.BaseRepo, pr.BaseBranch); err == nil {
			median = m
		}
	}
	return len(view.Active) + 1, median
}

// ascDashRestack is how many changes of a stack asc restacked in the last day, of how many.
type ascDashRestack struct{ Done, Total int }

// ascDashRestackLoad returns how many changes of the stack of e asc restacked in the last day (the newest move of their
// branch is a restack) and the size of the stack; zero when asc does not answer or the stack is not read. memo holds
// the answer for every change of a stack already read, so one dashboard reads each stack once. Tests replace it.
var ascDashRestackLoad = ascDashRestackFromAsc

func ascDashRestackFromAsc(ctx gocontext.Context, doer *user_model.User, e *AscInboxEntry, now time.Time, memo map[int64]ascDashRestack) ascDashRestack {
	if r, ok := memo[e.PullID]; ok {
		return r
	}
	if !setting.Asc.Enabled || !asctrust.HasFeature(ctx, asctrust.FeatureStacks) {
		return ascDashRestack{}
	}
	members := ascStackMembers(ctx, e.PullID)
	res := ascDashRestackOf(ctx, doer, e.PullID, members, now)
	for _, id := range members {
		memo[id] = res
	}
	memo[e.PullID] = res
	return res
}

func ascDashRestackOf(ctx gocontext.Context, doer *user_model.User, pullID int64, members []int64, now time.Time) ascDashRestack {
	if len(members) < 2 || len(members) > ascDashRestackMembers {
		return ascDashRestack{}
	}
	first, err := issues_model.GetPullRequestByID(ctx, pullID)
	if err != nil {
		return ascDashRestack{}
	}
	if err := first.LoadBaseRepo(ctx); err != nil {
		return ascDashRestack{}
	}
	repo := first.BaseRepo
	name, err := asctrust.RepoName(repo)
	if err != nil {
		return ascDashRestack{}
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return ascDashRestack{}
	}
	callCtx, release, err := asctrust.UserCall(ctx, doer, asctrust.CallRepoRead, repo)
	if err != nil {
		return ascDashRestack{}
	}
	defer release()
	who := asctrust.UndoWho{ID: doer.ID, Name: doer.Name}
	done := 0
	for _, id := range members {
		pr, err := issues_model.GetPullRequestByID(ctx, id)
		if err != nil || pr.HeadBranch == "" || pr.HeadRepoID != repo.ID || pr.BaseRepoID != repo.ID {
			continue
		}
		ref := "refs/heads/" + pr.HeadBranch
		page, err := asctrust.LoadOpLog(callCtx, conn, name, who, asctrust.OpLogQuery{Ref: ref, Limit: 1})
		if err != nil {
			log.Trace("asc dashboard: operations of %s: %v", ref, asctrust.Classify(err))
			return ascDashRestack{}
		}
		for _, r := range page.Rows {
			if r.Ref == ref && r.ViaKey == "restack" && now.Sub(r.At) <= ascDashRestackWindow {
				done++
			}
			break
		}
	}
	return ascDashRestack{Done: done, Total: len(members)}
}

// ascStackMembers returns the pull requests of the stack of pullID, from the bottom; nil when it is not stacked.
func ascStackMembers(ctx gocontext.Context, pullID int64) []int64 {
	row, err := issues_model.GetPullRequestAscStack(ctx, pullID)
	if err != nil || row == nil {
		return nil
	}
	root := row
	for i := 0; i < ascStackWalkLimit && root.StackParentID != 0; i++ {
		parent, err := issues_model.GetPullRequestAscStack(ctx, root.StackParentID)
		if err != nil || parent == nil {
			break
		}
		root = parent
	}
	var out []int64
	queue := []int64{root.PullID}
	seen := map[int64]bool{}
	for len(queue) > 0 && len(out) < ascStackWalkLimit {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
		kids, err := issues_model.GetAscStackChildren(ctx, id)
		if err != nil {
			continue
		}
		for _, k := range kids {
			queue = append(queue, k.PullID)
		}
	}
	return out
}

// ascDashKinds adds to row what its kind shows: the reply on a change that came back, the place in the queue of a
// change ready to land through it, and the restack after the trunk moved. queueable is whether row lands through the
// asc merge queue.
func ascDashKinds(ctx gocontext.Context, l translation.Locale, doer *user_model.User, e *AscInboxEntry, row *AscDashRow, queueable bool, now time.Time, memo map[int64]ascDashRestack) {
	switch e.Section {
	case issues_model.AscInboxReturned:
		row.Quote = ascDashReplyLoad(ctx, e)
	case issues_model.AscInboxReady:
		if queueable {
			if pos, median := ascDashJoinLoad(ctx, doer, e); pos > 0 {
				if median != "" {
					row.Note = l.TrString("user.asc_dash.join_queue_eta", pos, median)
				} else {
					row.Note = l.TrString("user.asc_dash.join_queue", pos)
				}
			}
		}
	}
	if r := ascDashRestackLoad(ctx, doer, e, now, memo); r.Done > 0 && r.Total > 0 {
		row.Chips = append(row.Chips, AscDashChip{"grey", l.TrString("user.asc_dash.trunk_moved")})
		if row.Note == "" {
			row.Note = l.TrString("user.asc_dash.restacked", r.Done, r.Total)
		}
	}
}
