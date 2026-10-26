// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascinbox computes the inbox of one user (SPEC 20.8, PLAN units C41b and C41c): the pull requests
// that need that user's attention, each with the section that says whose turn it is, and the user's own pin
// and snooze marks. It builds on the state model of C41a (models/issues/asc_inbox_state.go).
//
// Trust: the user is always the one the caller passes, which must be the authenticated doer. Every pull
// request is checked for read access to the pull requests of its base repository through the caller's
// authorization reducer, so a pull request in a repository the user lost access to (or that the access
// token is not allowed to see) is never listed. Marks are read only for that user.
package ascinbox

import (
	"context"
	"errors"
	"sort"
	"time"

	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/services/authz"
	"forgejo.org/services/automerge"
	pull_service "forgejo.org/services/pull"

	"xorm.io/builder"
)

const (
	// MaxPulls bounds the pull requests examined per call, the most recently updated first.
	MaxPulls = 200
	// MergedWindow is how long a merged pull request stays in the "merged" section.
	MergedWindow = 14 * 24 * time.Hour
)

// SectionOrder is the order sections are listed in.
var SectionOrder = []issues_model.AscInboxSection{
	issues_model.AscInboxNeedsReview,
	issues_model.AscInboxReturned,
	issues_model.AscInboxReady,
	issues_model.AscInboxBlocked,
	issues_model.AscInboxWaiting,
	issues_model.AscInboxDrafts,
	issues_model.AscInboxQueued,
	issues_model.AscInboxMerged,
}

// ErrNoReducer is returned when no authorization reducer is given: access then cannot be decided, so nothing
// is listed.
var ErrNoReducer = errors.New("ascinbox: no authorization reducer")

// Entry is one pull request in the inbox of one user.
type Entry struct {
	PullID       int64
	Title        string
	HTMLURL      string
	RepoFullName string
	Index        int64
	Section      issues_model.AscInboxSection
	Pinned       bool
	SnoozedUntil time.Time // zero when not snoozed
	Updated      time.Time
}

// Options selects what List returns.
type Options struct {
	// Sections, when not empty, keeps only entries in one of these sections.
	Sections []issues_model.AscInboxSection
	// IncludeSnoozed keeps entries whose snooze is still running; they are left out otherwise.
	IncludeSnoozed bool
}

// List returns the inbox of doer: the entries with their marks applied, filtered by opts and ordered by
// section (SectionOrder), then pinned first, then most recently updated first.
func List(ctx context.Context, doer *user_model.User, reducer authz.AuthorizationReducer, now time.Time, opts Options) ([]*Entry, error) {
	entries, err := Load(ctx, doer, reducer, now)
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	ids := make([]int64, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.PullID)
	}
	states, err := issues_model.ListAscInboxStates(ctx, doer.ID, ids)
	if err != nil {
		return nil, err
	}
	return Arrange(entries, states, now, opts), nil
}

// Load returns the entries of doer, each with its derived section and without marks.
func Load(ctx context.Context, doer *user_model.User, reducer authz.AuthorizationReducer, now time.Time) ([]*Entry, error) {
	if reducer == nil {
		return nil, ErrNoReducer
	}
	if doer == nil || doer.ID <= 0 {
		return nil, nil
	}
	var issues []*issues_model.Issue
	cutoff := now.Add(-MergedWindow).Unix()
	err := db.GetEngine(ctx).
		Where(builder.Eq{"is_pull": true}).
		And(builder.Or(
			builder.Eq{"poster_id": doer.ID},
			builder.In("id", builder.Select("issue_id").From("review").Where(builder.Eq{"reviewer_id": doer.ID, "dismissed": false})),
		)).
		And(builder.Or(builder.Eq{"is_closed": false}, builder.Gt{"closed_unix": cutoff})).
		OrderBy("updated_unix DESC, id DESC").Limit(MaxPulls).Find(&issues)
	if err != nil {
		return nil, err
	}
	var out []*Entry
	for _, issue := range issues {
		e, err := entryFor(ctx, doer, reducer, issue)
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

// entryFor derives the entry of one pull request, or nil when it is not in the inbox of doer.
func entryFor(ctx context.Context, doer *user_model.User, reducer authz.AuthorizationReducer, issue *issues_model.Issue) (*Entry, error) {
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
	if !canRead(ctx, doer, reducer, pr.BaseRepo) {
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
		facts.HeadCommitID = headCommit(ctx, pr)
		if pass, err := pull_service.IsPullCommitStatusPass(ctx, pr); err == nil {
			facts.ChecksFailing = !pass
		}
		facts.Approved = approved(ctx, pr)
		facts.Queued = queued(ctx, pr)
	}
	section := issues_model.AscInboxSectionFor(facts, doer.ID)
	if section == issues_model.AscInboxNone {
		return nil, nil
	}
	return &Entry{
		PullID:       pr.ID,
		Title:        issue.Title,
		HTMLURL:      issue.HTMLURL(),
		RepoFullName: pr.BaseRepo.FullName(),
		Index:        pr.Index,
		Section:      section,
		Updated:      issue.UpdatedUnix.AsTime(),
	}, nil
}

// canRead reports whether doer may read the pull requests of repo, as far as reducer allows.
func canRead(ctx context.Context, doer *user_model.User, reducer authz.AuthorizationReducer, repo *repo_model.Repository) bool {
	if repo == nil || doer == nil {
		return false
	}
	perm, err := access_model.GetUserRepoPermissionWithReducer(ctx, repo, doer, reducer)
	return err == nil && perm.CanRead(unit.TypePullRequests)
}

func headCommit(ctx context.Context, pr *issues_model.PullRequest) string {
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

// approved reports whether pr meets its branch rule's approvals; with no rule it is not "approved" (nothing
// was asked for), so an unreviewed pull request stays in waiting.
func approved(ctx context.Context, pr *issues_model.PullRequest) bool {
	pb, err := git_model.GetFirstMatchProtectedBranchRule(ctx, pr.BaseRepoID, pr.BaseBranch)
	if err != nil || pb == nil || pb.RequiredApprovals <= 0 {
		return false
	}
	return pull_service.HasEnoughApprovals(ctx, pb, pr)
}

// queued reports whether pr sits in the merge queue (C39) and has not landed or been cancelled.
func queued(ctx context.Context, pr *issues_model.PullRequest) bool {
	n, err := db.GetEngine(ctx).
		Where(builder.Eq{"pull_id": pr.ID}).
		And(builder.In("state", automerge.AscQueueQueued, automerge.AscQueueLanding, automerge.AscQueueBlocked)).
		Count(new(automerge.AscStackQueueEntry))
	return err == nil && n > 0
}

// Arrange applies the marks in states (keyed by pull id, all of one user) to entries, drops what opts leaves
// out and orders the rest: by section, pinned first, most recently updated first, then by pull id so the
// order is the same on every call. It sets Pinned and SnoozedUntil on the entries it is given.
func Arrange(entries []*Entry, states map[int64]*issues_model.AscInboxState, now time.Time, opts Options) []*Entry {
	want := make(map[issues_model.AscInboxSection]bool, len(opts.Sections))
	for _, s := range opts.Sections {
		want[s] = true
	}
	rank := make(map[issues_model.AscInboxSection]int, len(SectionOrder))
	for i, s := range SectionOrder {
		rank[s] = i
	}
	out := make([]*Entry, 0, len(entries))
	for _, e := range entries {
		if _, known := rank[e.Section]; !known || (len(want) > 0 && !want[e.Section]) {
			continue
		}
		e.Pinned = false
		e.SnoozedUntil = time.Time{}
		if st := states[e.PullID]; st != nil {
			e.Pinned = st.Pinned
			if st.IsSnoozed(now) {
				if !opts.IncludeSnoozed {
					continue
				}
				e.SnoozedUntil = time.Unix(int64(st.SnoozedUntil), 0).UTC()
			}
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Section != b.Section {
			return rank[a.Section] < rank[b.Section]
		}
		if a.Pinned != b.Pinned {
			return a.Pinned
		}
		if !a.Updated.Equal(b.Updated) {
			return a.Updated.After(b.Updated)
		}
		return a.PullID > b.PullID
	})
	return out
}
