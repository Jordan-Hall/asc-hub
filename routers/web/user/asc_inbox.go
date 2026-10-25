// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"net/http"
	"sort"
	"time"

	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/automerge"

	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// The inbox page (SPEC 20.8, PLAN unit C41c): `GET /-/asc/inbox` lists, for the signed-in user, the pull
// requests that need their attention, grouped in sections (whose turn it is), with their own pin and snooze
// marks. It is the web twin of `GET /api/v1/user/asc/inbox` (C41b) and uses the same state model (C41a).
//
// Trust: the user is always the signed-in doer. No query value names a user or a section filter that
// widens what is shown. Every pull request is re-checked for read access to the pull requests of its
// repository, so one the user lost access to is never listed, and marks are read only for the doer.
const (
	tplAscInbox base.TplName = "user/asc_inbox"

	// ascInboxMaxPulls bounds the pull requests examined per page view.
	ascInboxMaxPulls = 200
	// ascInboxMergedWindow is how long a merged pull request stays in the "merged" section.
	ascInboxMergedWindow = 14 * 24 * time.Hour
)

// ascInboxSectionOrder is the order sections are shown in.
var ascInboxSectionOrder = []issues_model.AscInboxSection{
	issues_model.AscInboxNeedsReview,
	issues_model.AscInboxReturned,
	issues_model.AscInboxReady,
	issues_model.AscInboxBlocked,
	issues_model.AscInboxWaiting,
	issues_model.AscInboxDrafts,
	issues_model.AscInboxQueued,
	issues_model.AscInboxMerged,
}

// AscInboxEntry is one pull request in the inbox of one user.
type AscInboxEntry struct {
	PullID       int64
	Title        string
	Link         string // relative link of the pull request
	RepoFullName string
	Index        int64
	Section      issues_model.AscInboxSection
	Pinned       bool
	SnoozedUntil time.Time // zero when not snoozed
	Updated      time.Time
}

// AscInboxGroup is one section as shown.
type AscInboxGroup struct {
	Section issues_model.AscInboxSection
	Entries []*AscInboxEntry
}

// ascInboxLoad returns the entries of doer, each with its derived section. Tests replace it.
//
// TODO(C41b): C41b's API handler computes the same list (GET /api/v1/user/asc/inbox). When it exports a
// shared service function, call it here instead of this page-local loader.
var ascInboxLoad = ascInboxLoadFromModels

func ascInboxLoadFromModels(ctx gocontext.Context, doer *user_model.User, now time.Time) ([]*AscInboxEntry, error) {
	var issues []*issues_model.Issue
	cutoff := now.Add(-ascInboxMergedWindow).Unix()
	err := db.GetEngine(ctx).
		Where("is_pull = ? AND (poster_id = ? OR id IN (SELECT issue_id FROM review WHERE reviewer_id = ? AND dismissed = ?)) AND (is_closed = ? OR closed_unix > ?)",
			true, doer.ID, doer.ID, false, false, cutoff).
		OrderBy("updated_unix DESC").Limit(ascInboxMaxPulls).Find(&issues)
	if err != nil {
		return nil, err
	}
	var out []*AscInboxEntry
	for _, issue := range issues {
		e, err := ascInboxEntryFor(ctx, doer, issue)
		if err != nil {
			log.Error("asc inbox: pull request %d skipped: %v", issue.ID, err)
			continue
		}
		if e != nil {
			out = append(out, e)
		}
	}
	return out, nil
}

// ascInboxEntryFor derives the entry of one pull request, or nil when it is not in doer's inbox.
func ascInboxEntryFor(ctx gocontext.Context, doer *user_model.User, issue *issues_model.Issue) (*AscInboxEntry, error) {
	if err := issue.LoadRepo(ctx); err != nil {
		return nil, err
	}
	if err := issue.LoadPullRequest(ctx); err != nil {
		return nil, err
	}
	pr := issue.PullRequest
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return nil, err
	}
	if !ascInboxCanSee(ctx, doer, pr.BaseRepo) {
		return nil, nil
	}
	reviewers, err := issues_model.GetAscInboxReviewers(ctx, issue.ID)
	if err != nil {
		return nil, err
	}
	facts := &issues_model.AscInboxFacts{
		AuthorID:   issue.PosterID,
		Draft:      pr.IsWorkInProgress(ctx),
		Merged:     pr.HasMerged,
		Closed:     issue.IsClosed,
		Conflicted: pr.Status == issues_model.PullRequestStatusConflict,
		Reviewers:  reviewers,
	}
	if !facts.Merged && !facts.Closed {
		facts.HeadCommitID = ascInboxHead(ctx, pr)
		if pass, err := pull_service.IsPullCommitStatusPass(ctx, pr); err == nil {
			facts.ChecksFailing = !pass
		}
		facts.Approved = ascInboxApproved(ctx, pr)
		facts.Queued = ascInboxQueued(ctx, pr)
	}
	section := issues_model.AscInboxSectionFor(facts, doer.ID)
	if section == issues_model.AscInboxNone {
		return nil, nil
	}
	return &AscInboxEntry{
		PullID:       pr.ID,
		Title:        issue.Title,
		Link:         issue.Link(),
		RepoFullName: pr.BaseRepo.FullName(),
		Index:        pr.Index,
		Section:      section,
		Updated:      issue.UpdatedUnix.AsTime(),
	}, nil
}

// ascInboxCanSee reports whether doer may read the pull requests of repo.
func ascInboxCanSee(ctx gocontext.Context, doer *user_model.User, repo *repo_model.Repository) bool {
	if repo == nil || doer == nil {
		return false
	}
	perm, err := access_model.GetUserRepoPermission(ctx, repo, doer)
	return err == nil && perm.CanRead(unit.TypePullRequests)
}

func ascInboxHead(ctx gocontext.Context, pr *issues_model.PullRequest) string {
	if pr.HeadCommitID != "" {
		return pr.HeadCommitID
	}
	g, closer, err := gitrepo.RepositoryFromContextOrOpen(ctx, pr.BaseRepo)
	if err != nil {
		return ""
	}
	defer closer.Close()
	id, err := g.GetRefCommitID(pr.GetGitRefName())
	if err != nil {
		return ""
	}
	return id
}

// ascInboxApproved reports whether pr meets its branch rule's approvals; with no rule it is not "approved"
// (nothing was asked for), so an unreviewed pull request stays in waiting.
func ascInboxApproved(ctx gocontext.Context, pr *issues_model.PullRequest) bool {
	pb, err := git_model.GetFirstMatchProtectedBranchRule(ctx, pr.BaseRepoID, pr.BaseBranch)
	if err != nil || pb == nil || pb.RequiredApprovals <= 0 {
		return false
	}
	return pull_service.HasEnoughApprovals(ctx, pb, pr)
}

// ascInboxQueued reports whether pr sits in the merge queue (C39) and has not landed or been cancelled.
func ascInboxQueued(ctx gocontext.Context, pr *issues_model.PullRequest) bool {
	n, err := db.GetEngine(ctx).Where("pull_id = ? AND state IN (?, ?, ?)", pr.ID,
		automerge.AscQueueQueued, automerge.AscQueueLanding, automerge.AscQueueBlocked).Count(new(automerge.AscStackQueueEntry))
	return err == nil && n > 0
}

// ascInboxGroup applies doer's marks to entries and groups them. A snoozed entry is left out and counted;
// pinned entries come first in their section, then the most recently updated.
func ascInboxGroup(entries []*AscInboxEntry, states map[int64]*issues_model.AscInboxState, now time.Time) ([]AscInboxGroup, int) {
	by := map[issues_model.AscInboxSection][]*AscInboxEntry{}
	snoozed := 0
	for _, e := range entries {
		if st := states[e.PullID]; st != nil {
			e.Pinned = st.Pinned
			if st.IsSnoozed(now) {
				snoozed++
				continue
			}
		}
		by[e.Section] = append(by[e.Section], e)
	}
	var out []AscInboxGroup
	for _, s := range ascInboxSectionOrder {
		es := by[s]
		if len(es) == 0 {
			continue
		}
		sort.SliceStable(es, func(i, j int) bool {
			if es[i].Pinned != es[j].Pinned {
				return es[i].Pinned
			}
			return es[i].Updated.After(es[j].Updated)
		})
		out = append(out, AscInboxGroup{Section: s, Entries: es})
	}
	return out, snoozed
}

// AscInbox serves `GET /-/asc/inbox`.
func AscInbox(ctx *context.Context) {
	if !setting.Asc.Enabled || ctx.Doer == nil {
		ctx.NotFound("AscInbox", nil)
		return
	}
	ctx.Data["Title"] = ctx.Tr("user.asc_inbox")
	ctx.Data["PageIsAscInbox"] = true

	now := time.Now()
	entries, err := ascInboxLoad(ctx, ctx.Doer, now)
	if err != nil {
		ctx.ServerError("ascInboxLoad", err)
		return
	}
	ids := make([]int64, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.PullID)
	}
	states, err := issues_model.ListAscInboxStates(ctx, ctx.Doer.ID, ids)
	if err != nil {
		ctx.ServerError("ListAscInboxStates", err)
		return
	}
	groups, snoozed := ascInboxGroup(entries, states, now)
	ctx.Data["AscInboxGroups"] = groups
	ctx.Data["AscInboxSnoozed"] = snoozed
	ctx.HTML(http.StatusOK, tplAscInbox)
}
