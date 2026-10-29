// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/ascinbox"
	"forgejo.org/services/automerge"

	"xorm.io/builder"
)

// The stack rows of the repository home ("Stacks in flight") and of the Stacks list (boards Repo and Stacks).
//
// A stack is the chain of pull requests that asc links through pull_request_asc_stack. One row is drawn for each
// open pull request that has no open child: the chain from the bottom to that tip, with the changes that already
// landed below it counted as landed. A pull request without a stack link is a stack of one change.
//
// Trust: the rows are built only after the caller has checked that the viewer may read the pull request unit of the
// repository. They contain what the pull request list of Forgejo shows to that viewer (title, poster, branch name,
// sizes, check results) and nothing from other repositories: a chain is followed only inside the base repository.

// Status words of a stack, from the one that needs a person first to the one that needs nobody.
const (
	stackNeedsReview = "needs_review"
	stackFailing     = "failing"
	stackReady       = "ready"
	stackQueued      = "queued"
	stackAgent       = "agent"
	stackWaiting     = "waiting"
	stackDraft       = "draft"
)

// stackStatusRank orders the status words: lower first.
var stackStatusRank = map[string]int{
	stackNeedsReview: 0, stackFailing: 1, stackReady: 2, stackQueued: 3, stackAgent: 4, stackWaiting: 5, stackDraft: 6,
}

// stackStatusTone is the tone of the dot and chip of a status word.
var stackStatusTone = map[string]string{
	stackNeedsReview: "warn", stackFailing: "err", stackReady: "ok", stackQueued: "ok", stackAgent: "agent", stackWaiting: "info", stackDraft: "",
}

// stackLoadLimit bounds the open pull requests read for one page of rows.
const stackLoadLimit = 300

// ascStackLabel is the name a stack goes by in lists. A stacked pull request has a head branch like
// "<user>/asc-<change id>", which says nothing to a reader, so the label is the title of the change in words joined by
// dashes ("add-a-processing-fee"); any other head branch is shown as it is.
func ascStackLabel(headBranch, title string) string {
	_, topic, ok := strings.Cut(headBranch, "/")
	if !ok {
		topic = headBranch
	}
	rest, isAsc := strings.CutPrefix(topic, "asc-")
	if !isAsc || !issues_model.IsAscChange(rest) {
		return headBranch
	}
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, string(cur))
			cur = cur[:0]
		}
	}
	for _, r := range strings.ToLower(title) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			cur = append(cur, r)
		} else {
			flush()
		}
	}
	flush()
	label := ""
	for _, w := range words {
		if len(label)+len(w) > 40 {
			break
		}
		if label != "" {
			label += "-"
		}
		label += w
	}
	if label == "" {
		return rest[:min(12, len(rest))]
	}
	return label
}

// stackSegment is one change of a stack in the progress rail. State is landed, ok, warn, agent, err or draft.
type stackSegment struct {
	State string
	Link  string // review link of the change; "" for a change that landed (its pull request may be unreadable)
	Title string
}

// stackRow is one stack of the list.
type stackRow struct {
	Name     string
	Title    string // the intent of the top change
	Link     string // where the row opens: the change that needs the viewer, else the bottom open change
	Status   string
	Tone     string
	People   []*user_model.User
	Agent    bool // a change was posted by an agent (bot user)
	Mine     bool // the viewer posted a change
	NeedsMe  bool // the viewer is asked for a review of a change
	Changes  int  // landed and open changes of the stack
	Landed   int
	Open     int
	Segments []stackSegment

	Additions, Deletions int
	HasSize              bool

	ChecksPass, ChecksFail, ChecksPending, ChecksTotal int

	Updated timeutil.TimeStamp
}

// stackChange is what is known of one open pull request.
type stackChange struct {
	pr         *issues_model.PullRequest
	parent     int64 // pull id of the parent change, 0 for the bottom
	draft      bool
	failing    bool
	pending    bool
	approved   bool
	queued     bool
	needsMe    bool
	agent      bool
	add, del   int
	hasSize    bool
	pass, fail int
	waiting    int
	total      int
}

// changeState is the colour of the segment of an open change.
func changeState(c *stackChange) string {
	switch {
	case c.failing:
		return "err"
	case c.draft:
		return "draft"
	case c.queued, c.approved:
		return "ok"
	case c.agent:
		return "agent"
	}
	return "warn"
}

// stackStatusOf is the status word of a stack of open changes: the one that needs a person soonest.
func stackStatusOf(changes []*stackChange) string {
	best := ""
	set := func(s string) {
		if best == "" || stackStatusRank[s] < stackStatusRank[best] {
			best = s
		}
	}
	allDraft, allApproved, anyQueued, anyAgent := len(changes) > 0, len(changes) > 0, false, false
	for _, c := range changes {
		if c.needsMe && !c.draft {
			set(stackNeedsReview)
		}
		if c.failing {
			set(stackFailing)
		}
		allDraft = allDraft && c.draft
		allApproved = allApproved && (c.approved || c.queued)
		anyQueued = anyQueued || c.queued
		anyAgent = anyAgent || c.agent
	}
	switch {
	case best != "":
		return best
	case anyQueued:
		return stackQueued
	case allApproved:
		return stackReady
	case anyAgent:
		return stackAgent
	case allDraft:
		return stackDraft
	}
	return stackWaiting
}

// checksCounts turns the latest status of each context of a head commit into counts.
func checksCounts(statuses []*git_model.CommitStatus) (pass, fail, pending int) {
	for _, s := range statuses {
		switch {
		case s.State.IsSuccess() || s.State.IsSkipped():
			pass++
		case s.State.IsPending():
			pending++
		default:
			fail++
		}
	}
	return pass, fail, pending
}

// buildStackRows groups the open changes into rows. landedBelow maps a pull id to the number of landed changes below
// the bottom of the stack that pull id belongs to (found by the caller). The order is: status, then newest update.
func buildStackRows(changes []*stackChange, landedBelow map[int64]int, repoLink string, doer *user_model.User) []*stackRow {
	byID := map[int64]*stackChange{}
	hasOpenChild := map[int64]bool{}
	for _, c := range changes {
		byID[c.pr.ID] = c
	}
	for _, c := range changes {
		if _, ok := byID[c.parent]; ok && c.parent != 0 {
			hasOpenChild[c.parent] = true
		}
	}
	var rows []*stackRow
	for _, tip := range changes {
		if hasOpenChild[tip.pr.ID] {
			continue
		}
		// Walk down to the bottom, bounded so a corrupt cycle ends.
		chain := []*stackChange{tip}
		seen := map[int64]bool{tip.pr.ID: true}
		for cur := tip; len(chain) < 64; {
			p, ok := byID[cur.parent]
			if !ok || seen[p.pr.ID] {
				break
			}
			seen[p.pr.ID] = true
			chain = append([]*stackChange{p}, chain...)
			cur = p
		}
		bottom := chain[0]
		row := &stackRow{
			Name:   ascStackLabel(bottom.pr.HeadBranch, bottom.pr.Issue.Title),
			Title:  tip.pr.Issue.Title,
			Landed: landedBelow[bottom.pr.ID],
			Open:   len(chain),
			Status: stackStatusOf(chain),
		}
		row.Tone = stackStatusTone[row.Status]
		row.Changes = row.Landed + row.Open
		for range row.Landed {
			row.Segments = append(row.Segments, stackSegment{State: "landed"})
		}
		var link *stackChange
		people := map[int64]bool{}
		for _, c := range chain {
			row.Segments = append(row.Segments, stackSegment{
				State: changeState(c),
				Link:  fmt.Sprintf("%s/pulls/%d", repoLink, c.pr.Issue.Index),
				Title: c.pr.Issue.Title,
			})
			if link == nil || (c.needsMe && !link.needsMe) {
				link = c
			}
			if c.pr.Issue.Poster != nil && !people[c.pr.Issue.PosterID] {
				people[c.pr.Issue.PosterID] = true
				row.People = append(row.People, c.pr.Issue.Poster)
			}
			row.Agent = row.Agent || c.agent
			row.Mine = row.Mine || (doer != nil && c.pr.Issue.PosterID == doer.ID)
			row.NeedsMe = row.NeedsMe || c.needsMe
			if c.hasSize {
				row.HasSize = true
				row.Additions += c.add
				row.Deletions += c.del
			}
			row.ChecksPass += c.pass
			row.ChecksFail += c.fail
			row.ChecksPending += c.waiting
			row.ChecksTotal += c.total
			if c.pr.Issue.UpdatedUnix > row.Updated {
				row.Updated = c.pr.Issue.UpdatedUnix
			}
		}
		row.Link = fmt.Sprintf("%s/pulls/%d", repoLink, link.pr.Issue.Index)
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if ri, rj := stackStatusRank[rows[i].Status], stackStatusRank[rows[j].Status]; ri != rj {
			return ri < rj
		}
		return rows[i].Updated > rows[j].Updated
	})
	return rows
}

// loadStackChanges reads the open pull requests of repo and what the rows need of each. doer may be nil (a guest has
// no "mine" and no "needs me"). Failing lookups of one change leave that fact out; they never hide the change.
func loadStackChanges(ctx context.Context, repo *repo_model.Repository, gitRepo *git.Repository, doer *user_model.User) ([]*stackChange, map[int64]int, error) {
	var issues []*issues_model.Issue
	err := db.GetEngine(ctx).Where(builder.Eq{"repo_id": repo.ID, "is_pull": true, "is_closed": false}).
		OrderBy("updated_unix DESC, id DESC").Limit(stackLoadLimit).Find(&issues)
	if err != nil {
		return nil, nil, err
	}
	if len(issues) == 0 {
		return nil, map[int64]int{}, nil
	}
	if err := issues_model.IssueList(issues).LoadPosters(ctx); err != nil {
		return nil, nil, err
	}
	var pulls []*issues_model.PullRequest
	ids := make([]int64, 0, len(issues))
	for _, is := range issues {
		ids = append(ids, is.ID)
	}
	if err := db.GetEngine(ctx).In("issue_id", ids).Find(&pulls); err != nil {
		return nil, nil, err
	}
	byIssue := map[int64]*issues_model.Issue{}
	for _, is := range issues {
		byIssue[is.ID] = is
	}
	pullIDs := make([]int64, 0, len(pulls))
	for _, pr := range pulls {
		pr.Issue = byIssue[pr.IssueID]
		pr.BaseRepo = repo
		pullIDs = append(pullIDs, pr.ID)
	}
	stackRows := map[int64]*issues_model.PullRequestAscStack{}
	var links []*issues_model.PullRequestAscStack
	if err := db.GetEngine(ctx).Where("repo_id = ?", repo.ID).In("pull_id", pullIDs).Find(&links); err != nil {
		return nil, nil, err
	}
	for _, l := range links {
		stackRows[l.PullID] = l
	}
	queue, err := automerge.AscQueueEntries(ctx, pullIDs)
	if err != nil {
		log.Error("stack rows: queue entries of %s: %v", repo.FullName(), err)
		queue = map[int64]*automerge.AscStackQueueEntry{}
	}
	changes := make([]*stackChange, 0, len(pulls))
	heads := map[int64]string{}
	var shas []string
	for _, pr := range pulls {
		c := &stackChange{pr: pr, draft: pr.IsWorkInProgress(ctx)}
		if l := stackRows[pr.ID]; l != nil {
			c.parent = l.StackParentID
		}
		if e := queue[pr.ID]; e != nil {
			switch e.State {
			case automerge.AscQueueQueued, automerge.AscQueueLanding, automerge.AscQueueBlocked:
				c.queued = true
			}
		}
		if pr.Issue.Poster != nil && pr.Issue.Poster.IsBot() {
			c.agent = true
		}
		if sha := ascinbox.HeadCommit(ctx, pr); sha != "" {
			heads[pr.ID] = sha
			shas = append(shas, sha)
		}
		if gitRepo != nil {
			base := pr.MergeBase
			if base == "" {
				base = pr.BaseBranch
			}
			if sha := heads[pr.ID]; sha != "" {
				if _, add, del, err := gitRepo.GetShortStat(base, sha, base == pr.BaseBranch); err == nil {
					c.add, c.del, c.hasSize = add, del, true
				}
			}
		}
		if doer != nil && !c.draft {
			c.needsMe = needsReviewOf(ctx, pr, doer, heads[pr.ID])
		}
		changes = append(changes, c)
	}
	statuses, err := git_model.GetLatestCommitStatusForRepoCommitIDs(ctx, repo.ID, shas)
	if err != nil {
		log.Error("stack rows: check results of %s: %v", repo.FullName(), err)
		statuses = map[string][]*git_model.CommitStatus{}
	}
	for _, c := range changes {
		c.pass, c.fail, c.waiting = checksCounts(statuses[heads[c.pr.ID]])
		c.total = c.pass + c.fail + c.waiting
		c.failing = c.fail > 0
		c.pending = c.waiting > 0
		c.approved = ascinbox.Approved(ctx, c.pr)
	}

	// Landed changes below the bottom of each stack: follow the parent links through merged pull requests.
	open := map[int64]bool{}
	for _, c := range changes {
		open[c.pr.ID] = true
	}
	landed := map[int64]int{}
	for _, c := range changes {
		if c.parent == 0 || open[c.parent] {
			continue
		}
		n := 0
		for next := c.parent; next != 0 && n < 64; {
			p, err := issues_model.GetPullRequestByID(ctx, next)
			if err != nil || p.BaseRepoID != repo.ID || !p.HasMerged {
				break
			}
			n++
			l, err := issues_model.GetPullRequestAscStack(ctx, p.ID)
			if err != nil || l == nil {
				break
			}
			next = l.StackParentID
		}
		landed[c.pr.ID] = n
	}
	return changes, landed, nil
}

// needsReviewOf reports whether doer is asked for a review of pr that is still open: the turn is the reviewer's.
func needsReviewOf(ctx context.Context, pr *issues_model.PullRequest, doer *user_model.User, head string) bool {
	reviewers, err := issues_model.GetAscInboxReviewers(ctx, pr.IssueID)
	if err != nil {
		return false
	}
	facts := &issues_model.AscInboxFacts{HeadCommitID: head, AuthorID: pr.Issue.PosterID, Reviewers: reviewers}
	return issues_model.AscInboxSectionFor(facts, doer.ID) == issues_model.AscInboxNeedsReview
}

// stackCounts are the numbers on the filter buttons.
type stackCounts struct {
	Open, Mine, Agents, NeedsMe int
}

// stackFilterCounts counts rows for the Open, Mine, Agents and Needs me filters.
func stackFilterCounts(rows []*stackRow) stackCounts {
	c := stackCounts{Open: len(rows)}
	for _, r := range rows {
		if r.Mine {
			c.Mine++
		}
		if r.Agent {
			c.Agents++
		}
		if r.NeedsMe {
			c.NeedsMe++
		}
	}
	return c
}

// requestID is the id the proxy or client sent for the request, "-" when there is none. It is shown on an error
// state so a person can quote it; it is capped and printable, so it cannot carry markup (templates escape it anyway).
func requestID(req *http.Request) string {
	for _, key := range setting.Log.RequestIDHeaders {
		if v := req.Header.Get(key); v != "" {
			if len(v) > 40 {
				v = v[:40]
			}
			return strings.Map(func(r rune) rune {
				if r < 0x20 || r > 0x7e {
					return -1
				}
				return r
			}, v)
		}
	}
	return "-"
}
