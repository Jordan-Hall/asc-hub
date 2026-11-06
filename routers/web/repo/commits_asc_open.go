// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"fmt"
	"strconv"
	"slices"
	"strings"
	"time"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/cache"
	"forgejo.org/modules/git"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/automerge"
	"forgejo.org/services/context"
)

// The "Open on top of main" section of an asc repository's History (design board History): the open changes onto the
// shown branch, each with its state (a node shape and a chip, never colour alone), its size, who made it, its place in
// its stack and what it waits on.

const (
	// ascOpenMax bounds the open changes the section shows.
	ascOpenMax = 10
	// ascChangeIDShort is the length of a change id in the section.
	ascChangeIDShort = 8
)

// The states of an open change, in the order the section decides them.
const (
	ascOpenWriting  = "writing"  // its agent run is still going
	ascOpenQueued   = "queued"   // it waits in the merge queue
	ascOpenDraft    = "draft"    // its author marked it work in progress
	ascOpenChanges  = "changes"  // a reviewer asked for changes
	ascOpenApproved = "approved" // approved, and no review is still asked for
	ascOpenReview   = "review"   // it needs a review
)

// AscOpenChange is one row of the section.
type AscOpenChange struct {
	Title, Link string
	State       string
	// HasStat says the size was read; Additions and Deletions are the lines it adds and removes.
	HasStat              bool
	Additions, Deletions int
	Poster               *user_model.User
	// Change is the short id of the change (its head commit when asc has not named it).
	Change string
	// Stack is the name of the stack (the head branch of its bottom change); Position counts from 1 at the bottom.
	Stack          string
	Position, Size int
	// What it waits on: the agent run and how long it has run, the place in the merge queue, the reviewers.
	Run         uint64
	RunMinutes  int
	QueuePlace  int
	QueueAhead  int
	People      string
	UpdatedUnix timeutil.TimeStamp
}

// ascOpenStateOf decides the state of an open change.
func ascOpenStateOf(writing, queued, draft, changesRequested, approved bool) string {
	switch {
	case writing:
		return ascOpenWriting
	case queued:
		return ascOpenQueued
	case draft:
		return ascOpenDraft
	case changesRequested:
		return ascOpenChanges
	case approved:
		return ascOpenApproved
	}
	return ascOpenReview
}

// ascOpenStackPlaces gives each open pull request its stack: the head branch of the bottom change, its place counted
// from 1 at the bottom, and the number of changes in the stack. parent maps a pull request to the one below it (0 at
// the bottom); a parent that is not open is not part of the stack any more.
func ascOpenStackPlaces(prs []*issues_model.PullRequest, parent map[int64]int64) (name map[int64]string, place, size map[int64]int) {
	byID := make(map[int64]*issues_model.PullRequest, len(prs))
	for _, pr := range prs {
		byID[pr.ID] = pr
	}
	bottom := map[int64]int64{}
	name, place, size = map[int64]string{}, map[int64]int{}, map[int64]int{}
	for _, pr := range prs {
		id, depth := pr.ID, 1
		// the walk stops at the bottom, at a parent that is not open, and on a loop
		for steps := 0; steps < len(prs); steps++ {
			p, ok := byID[parent[id]]
			if !ok || p.ID == pr.ID {
				break
			}
			id = p.ID
			depth++
		}
		bottom[pr.ID], place[pr.ID] = id, depth
		name[pr.ID] = byID[id].HeadBranch
	}
	tallest := map[int64]int{}
	for _, pr := range prs {
		tallest[bottom[pr.ID]] = max(tallest[bottom[pr.ID]], place[pr.ID])
	}
	for _, pr := range prs {
		size[pr.ID] = tallest[bottom[pr.ID]]
	}
	return name, place, size
}

// ascOpenRunningRuns returns the agent runs of the repository that are still going, by id. A variable, so tests answer
// without asc.
var ascOpenRunningRuns = func(ctx gocontext.Context, name string) (map[uint64]*ascv1.AgentRun, error) {
	conn, err := ascRPCConn()
	if err != nil {
		return nil, err
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx), ascAgentsCallTimeout)
	defer cancel()
	resp, err := ascv1.NewAgentsClient(conn).ListRuns(callCtx, &ascv1.ListAgentRunsRequest{
		Repo: &ascv1.RepoRef{Name: name}, Limit: 100,
		States: []ascv1.AgentRunState{
			ascv1.AgentRunState_AGENT_RUN_STATE_QUEUED, ascv1.AgentRunState_AGENT_RUN_STATE_RUNNING,
			ascv1.AgentRunState_AGENT_RUN_STATE_WAITING_INPUT,
		},
	})
	if err != nil {
		return nil, err
	}
	out := make(map[uint64]*ascv1.AgentRun, len(resp.GetRuns()))
	for _, r := range resp.GetRuns() {
		out[r.GetId()] = r
	}
	return out, nil
}

// ascOpenReviews reads the latest review of each reviewer: whether one asked for changes, whether it is approved with
// no review still asked for, and the names the status line shows for each state.
func ascOpenReviews(reviews issues_model.ReviewList) (changes, approved bool, asked, approvers, rejecters []string) {
	for _, r := range reviews {
		if r.Reviewer == nil {
			continue
		}
		switch r.Type {
		case issues_model.ReviewTypeReject:
			rejecters = append(rejecters, r.Reviewer.GetDisplayName())
		case issues_model.ReviewTypeApprove:
			approvers = append(approvers, r.Reviewer.GetDisplayName())
		case issues_model.ReviewTypeRequest:
			asked = append(asked, r.Reviewer.GetDisplayName())
		}
	}
	return len(rejecters) > 0, len(approvers) > 0 && len(asked) == 0, asked, approvers, rejecters
}

// ascOpenPeople is the names the status line of a state shows.
func ascOpenPeople(state string, asked, approvers, rejecters []string) string {
	switch state {
	case ascOpenChanges:
		return strings.Join(rejecters, ", ")
	case ascOpenApproved:
		return strings.Join(approvers, ", ")
	case ascOpenReview:
		return strings.Join(asked, ", ")
	}
	return ""
}

// prepareAscOpenChanges puts the section into ctx.Data["AscOpenChanges"] on the first page of the History of a branch
// of an asc repository.
func prepareAscOpenChanges(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if !setting.Asc.Enabled || repo == nil || repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) ||
		!ctx.Repo.IsViewBranch || ctx.Repo.GitRepo == nil || ctx.FormInt("page") > 1 ||
		!repo.CanEnablePulls() || !ctx.Repo.CanRead(unit.TypePullRequests) {
		return
	}
	all, err := issues_model.GetUnmergedPullRequestsByBaseInfo(ctx, repo.ID, ctx.Repo.BranchName)
	if err != nil {
		log.Warn("the open changes of %s are left out of its history: %v", repo.FullName(), err)
		return
	}
	prs := make([]*issues_model.PullRequest, 0, min(len(all), ascOpenMax))
	parent := map[int64]int64{}
	stacks := map[int64]*issues_model.PullRequestAscStack{}
	for _, pr := range all {
		if len(prs) >= ascOpenMax {
			break
		}
		if err := pr.LoadIssue(ctx); err != nil {
			log.Warn("the open changes of %s: issue of pull %d: %v", repo.FullName(), pr.ID, err)
			continue
		}
		pr.Issue.Repo = repo
		if link, err := issues_model.GetPullRequestAscStack(ctx, pr.ID); err == nil && link != nil {
			parent[pr.ID], stacks[pr.ID] = link.StackParentID, link
		}
		prs = append(prs, pr)
	}
	if len(prs) == 0 {
		return
	}
	stackName, place, size := ascOpenStackPlaces(prs, parent)

	queue, err := automerge.AscQueuePositions(ctx, repo.ID, ctx.Repo.BranchName)
	if err != nil {
		log.Warn("the open changes of %s: merge queue: %v", repo.FullName(), err)
	}
	// an agent run is only named to a person who may read the runs
	var running map[uint64]*ascv1.AgentRun
	if ascAgentsCanReadRuns(ctx) {
		if name, err := ascGraphRepoName(repo); err == nil {
			if running, err = ascOpenRunningRuns(ctx.Req.Context(), name); err != nil {
				log.Debug("the open changes of %s: agent runs: %v", repo.FullName(), err)
			}
		}
	}

	out := make([]*AscOpenChange, 0, len(prs))
	for _, pr := range prs {
		c := &AscOpenChange{
			Title: pr.Issue.Title, Link: pr.Issue.Link(),
			Stack: stackName[pr.ID], Position: place[pr.ID], Size: size[pr.ID],
			QueuePlace: queue[pr.ID], QueueAhead: max(0, queue[pr.ID]-1), UpdatedUnix: pr.Issue.UpdatedUnix,
		}
		if err := pr.Issue.LoadPoster(ctx); err == nil {
			c.Poster = pr.Issue.Poster
		}
		if link := stacks[pr.ID]; link != nil && issues_model.IsAscChange(link.AscChange) {
			c.Change = link.AscChange[:min(len(link.AscChange), ascChangeIDShort)]
		}
		head, headErr := ctx.Repo.GitRepo.GetRefCommitID(pr.GetGitRefName())
		if c.Change == "" && headErr == nil {
			c.Change = head[:min(len(head), ascChangeIDShort)]
		}
		if headErr == nil && pr.MergeBase != "" {
			c.HasStat, c.Additions, c.Deletions = ascOpenStat(ctx.Repo.GitRepo, pr, head)
		}
		writing := false
		if run, err := issues_model.GetPullRequestAscRun(ctx, pr.ID); err == nil && run != nil {
			if r, ok := running[run.RunID]; ok {
				writing, c.Run = true, run.RunID
				if started := r.GetStarted(); started > 0 {
					c.RunMinutes = max(0, int(time.Since(time.Unix(started, 0)).Minutes()))
				}
			}
		}
		var asked, approvers, rejecters []string
		var changes, approved bool
		if reviews, err := issues_model.GetReviewsByIssueID(ctx, pr.IssueID); err == nil && reviews.LoadReviewers(ctx) == nil {
			changes, approved, asked, approvers, rejecters = ascOpenReviews(reviews)
		}
		c.State = ascOpenStateOf(writing, c.QueuePlace > 0, pr.IsWorkInProgress(ctx), changes, approved)
		c.People = ascOpenPeople(c.State, asked, approvers, rejecters)
		out = append(out, c)
	}
	ctx.Data["AscOpenChanges"] = out
}

// ascOpenStat returns the lines a pull request adds and removes, read once per head.
func ascOpenStat(gitRepo *git.Repository, pr *issues_model.PullRequest, head string) (ok bool, additions, deletions int) {
	key := fmt.Sprintf("asc-open-stat:%d:%s:%s", pr.ID, pr.MergeBase, head)
	v, err := cache.GetString(key, func() (string, error) {
		_, add, del, err := gitRepo.GetShortStat(pr.MergeBase, head, false)
		if err != nil {
			// remembered, so a head that cannot be compared is not compared again on every page
			log.Debug("the size of pull %d: %v", pr.ID, err)
			return ascStackPathsFailed, nil
		}
		return strconv.Itoa(add) + " " + strconv.Itoa(del), nil
	})
	if err != nil || v == ascStackPathsFailed {
		return false, 0, 0
	}
	a, d, _ := strings.Cut(v, " ")
	additions, errA := strconv.Atoi(a)
	deletions, errD := strconv.Atoi(d)
	return errA == nil && errD == nil, additions, deletions
}

// The bracket of a landed stack: the rows of the History whose changes landed as one stack are joined by a thick line
// beside them (board History). The first row of a bracket is "start", the last "end" and any between "mid".
const (
	ascBracketStart = "start"
	ascBracketMid   = "mid"
	ascBracketEnd   = "end"
)

// ascLandedBrackets marks the shown commits (newest first) that landed as one stack. Changes of one stack are the
// ones linked by their parents; a stack is marked only when it has two rows or more and they are next to each other.
func ascLandedBrackets(order []string, landed []*issues_model.LandedAscStackPull) map[string]string {
	row := make(map[string]int, len(order))
	for i, c := range order {
		row[c] = i
	}
	byPull := map[int64]*issues_model.LandedAscStackPull{}
	for _, l := range landed {
		if _, shown := row[l.MergedCommitID]; shown {
			byPull[l.PullID] = l
		}
	}
	// the bottom of each landed change among the shown ones
	bottomOf := func(l *issues_model.LandedAscStackPull) int64 {
		id := l.PullID
		for steps := 0; steps < len(byPull); steps++ {
			p, ok := byPull[byPull[id].StackParentID]
			if !ok || p.PullID == l.PullID {
				break
			}
			id = p.PullID
		}
		return id
	}
	groups := map[int64][]int{}
	for _, l := range byPull {
		b := bottomOf(l)
		groups[b] = append(groups[b], row[l.MergedCommitID])
	}
	out := map[string]string{}
	for _, rows := range groups {
		slices.Sort(rows)
		rows = slices.Compact(rows)
		if len(rows) < 2 || rows[len(rows)-1]-rows[0] != len(rows)-1 {
			continue
		}
		for k, r := range rows {
			switch k {
			case 0:
				out[order[r]] = ascBracketStart
			case len(rows) - 1:
				out[order[r]] = ascBracketEnd
			default:
				out[order[r]] = ascBracketMid
			}
		}
	}
	return out
}

// prepareAscLandedBrackets puts the brackets of the shown commits into ctx.Data["AscLandedBrackets"].
func prepareAscLandedBrackets(ctx *context.Context, commits []*git.Commit) {
	repo := ctx.Repo.Repository
	if !setting.Asc.Enabled || repo == nil || repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) ||
		len(commits) < 2 || !ctx.Repo.CanRead(unit.TypePullRequests) {
		return
	}
	order := make([]string, 0, len(commits))
	for _, c := range commits {
		order = append(order, c.ID.String())
	}
	landed, err := issues_model.ListLandedAscStackPulls(ctx, repo.ID, order)
	if err != nil {
		log.Warn("the landed stacks of %s are left out of its history: %v", repo.FullName(), err)
		return
	}
	if brackets := ascLandedBrackets(order, landed); len(brackets) > 0 {
		ctx.Data["AscLandedBrackets"] = brackets
	}
}
