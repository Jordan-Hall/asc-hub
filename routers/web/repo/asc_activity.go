// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"fmt"
	"sort"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// The activity timeline of a stacked change (board Review, "Activity"): the versions pushed, the comments, the verdicts
// of reviewers, the approvals asc carried over to a newer version and the restacks, newest first. The hub's own records
// (comments, reviews) are merged with what asc knows (its versions, its approval carrying and its ref log), so one list
// tells the story. Every part that cannot be read is left out; none of it blocks the page.

// Kinds of event, also locale keys (asc_review.activity_<kind>).
const (
	ascActVersion  = "version"
	ascActComment  = "comment"
	ascActApproved = "approved"
	ascActChanges  = "changes"
	ascActCarried  = "carried"
	ascActRestack  = "restack"
	ascActUndo     = "undo"
	ascActRevert   = "revert"
)

const (
	// ascActivityShown is how many events are open before "Show all".
	ascActivityShown = 6
	// ascActivityMax caps the list; a change with more history shows its newest events.
	ascActivityMax = 100
	// ascActivityOps is how many ref log rows are read.
	ascActivityOps = 50
)

// AscActivityEvent is one line of the timeline.
type AscActivityEvent struct {
	Kind    string
	At      timeutil.TimeStamp
	Actor   string
	Version uint32 // the version pushed, or the newest one an approval was carried to
	Link    string // where the event can be read: a comment anchor, "" when there is none
}

// AscActivity is the timeline: Events newest first, the first Shown of them open.
type AscActivity struct {
	Events []AscActivityEvent
	Total  int
	Shown  int
}

// Hidden is how many events wait behind "Show all".
func (a *AscActivity) Hidden() int { return a.Total - a.Shown }

// ascActivityInput is everything the timeline is made from, already read.
type ascActivityInput struct {
	Versions  []pull_service.AscVersion
	Comments  []AscActivityEvent // Kind comment
	Verdicts  []AscActivityEvent // Kind approved or changes
	Approvals []AscApproval
	Ops       []asctrust.OpRow
}

// ascActivityOf merges the sources into the timeline, newest first. Ties keep the order versions, operations, verdicts,
// carried approvals, comments, so a version pushed comes after the comment that explains it only when the clock says so.
func ascActivityOf(in ascActivityInput) *AscActivity {
	var ev []AscActivityEvent
	for _, v := range in.Versions {
		ev = append(ev, AscActivityEvent{Kind: ascActVersion, At: timeutil.TimeStamp(v.At), Actor: v.Actor, Version: v.N})
	}
	for _, o := range in.Ops {
		kind := ""
		switch o.ViaKey {
		case "restack":
			kind = ascActRestack
		case "undo":
			kind = ascActUndo
		case "revert":
			kind = ascActRevert
		}
		if kind != "" {
			ev = append(ev, AscActivityEvent{Kind: kind, At: timeutil.TimeStamp(o.At.Unix()), Actor: o.Actor})
		}
	}
	ev = append(ev, in.Verdicts...)
	// An approval that counts for a newer version although it was given on an earlier one happened when the next version
	// was pushed; asc keeps no clock for the carrying itself.
	var newest uint32
	at := map[uint32]int64{}
	for _, v := range in.Versions {
		at[v.N] = v.At
		newest = max(newest, v.N)
	}
	for _, a := range in.Approvals {
		if !a.Carried || a.ChangesRequested {
			continue
		}
		when, ok := at[a.Version+1]
		if !ok {
			continue
		}
		ev = append(ev, AscActivityEvent{Kind: ascActCarried, At: timeutil.TimeStamp(when), Actor: a.Reviewer, Version: newest})
	}
	ev = append(ev, in.Comments...)
	sort.SliceStable(ev, func(i, j int) bool { return ev[i].At > ev[j].At })
	if len(ev) > ascActivityMax {
		ev = ev[:ascActivityMax]
	}
	return &AscActivity{Events: ev, Total: len(ev), Shown: min(len(ev), ascActivityShown)}
}

// ascActivityLocal reads the comments and verdicts of a pull request from the hub's own records.
func ascActivityLocal(ctx *context.Context, pull *issues_model.PullRequest) (comments, verdicts []AscActivityEvent, err error) {
	list, err := issues_model.FindComments(ctx, &issues_model.FindCommentsOptions{IssueID: pull.IssueID, Type: issues_model.CommentTypeComment})
	if err != nil {
		return nil, nil, fmt.Errorf("comments: %w", err)
	}
	if err := list.LoadPosters(ctx); err != nil {
		return nil, nil, fmt.Errorf("posters: %w", err)
	}
	for _, c := range list {
		if c.Poster == nil {
			continue
		}
		comments = append(comments, AscActivityEvent{Kind: ascActComment, At: c.CreatedUnix, Actor: c.Poster.GetDisplayName(), Link: "#" + c.HashTag()})
	}
	reviews, err := issues_model.FindReviews(ctx, issues_model.FindReviewOptions{
		IssueID: pull.IssueID, Dismissed: optional.Some(false),
		Types: []issues_model.ReviewType{issues_model.ReviewTypeApprove, issues_model.ReviewTypeReject},
	})
	if err != nil {
		return nil, nil, fmt.Errorf("reviews: %w", err)
	}
	if err := reviews.LoadReviewers(ctx); err != nil {
		return nil, nil, fmt.Errorf("reviewers: %w", err)
	}
	for _, r := range reviews {
		if r.Reviewer == nil {
			continue
		}
		kind := ascActApproved
		if r.Type == issues_model.ReviewTypeReject {
			kind = ascActChanges
		}
		verdicts = append(verdicts, AscActivityEvent{Kind: kind, At: r.CreatedUnix, Actor: r.Reviewer.GetDisplayName()})
	}
	return comments, verdicts, nil
}

// prepareAscActivity puts the timeline of an open or merged stacked pull request into ctx.Data["AscActivity"]. It runs
// after the brief, whose approvals it reuses.
func prepareAscActivity(ctx *context.Context, pull *issues_model.PullRequest) {
	if pull == nil || !setting.Asc.Enabled || ctx.Data["AscStackPanel"] == nil || ctx.Repo == nil {
		return
	}
	in := ascActivityInput{}
	var err error
	if in.Comments, in.Verdicts, err = ascActivityLocal(ctx, pull); err != nil {
		log.Warn("asc activity of pull request %d: %v", pull.ID, err)
		return
	}
	if versions, err := ascVersionsOnce(ctx, pull); err == nil {
		in.Versions = versions.Versions
	}
	if b, ok := ctx.Data["AscBrief"].(*AscBrief); ok && b != nil {
		in.Approvals = b.Approvals
	}
	if pull.HeadBranch != "" && pull.HeadRepoID == pull.BaseRepoID {
		in.Ops = ascRefOps(ctx, ctx.Repo.Repository, "refs/heads/"+pull.HeadBranch, ascActivityOps)
	}
	if act := ascActivityOf(in); act.Total > 0 {
		ctx.Data["AscActivity"] = act
	}
}
