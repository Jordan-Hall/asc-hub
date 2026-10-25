// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issues

import (
	"context"
	"errors"
	"fmt"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"
)

// The inbox state model (SPEC section 20.8, PLAN unit C41a). The inbox lists, for one user, the pull requests
// that need that user's attention, grouped in sections. Two things feed it:
//
//   - whose turn it is, derived (never stored) from the review requests, each reviewer's latest review and
//     the version after it: AscInboxFacts and AscInboxSectionFor;
//   - the user's own marks, stored per (user, pull request): pinned, and snoozed until a time.
//
// The marks are private to their user: every read and write is keyed by the user id the caller passes, which
// the API takes from the authenticated session, never from a request body.

// AscInboxSection names a section of the inbox. The words are the API's (C41b).
type AscInboxSection string

const (
	// AscInboxNone means the pull request is not in the user's inbox (closed, or the user is not involved).
	AscInboxNone AscInboxSection = ""
	// AscInboxNeedsReview is the reviewer's turn: a review is requested, or the reviewer asked for changes
	// and a newer version has been pushed since.
	AscInboxNeedsReview AscInboxSection = "needs_review"
	// AscInboxReturned is the author's turn: a reviewer asked for changes on the current version.
	AscInboxReturned AscInboxSection = "returned"
	// AscInboxReady is the author's pull request, approved and clear to merge.
	AscInboxReady AscInboxSection = "ready"
	// AscInboxBlocked is the author's pull request that cannot merge: a failing check or a conflict.
	AscInboxBlocked AscInboxSection = "blocked"
	// AscInboxWaiting is someone else's turn.
	AscInboxWaiting AscInboxSection = "waiting"
	// AscInboxDrafts is the author's draft.
	AscInboxDrafts AscInboxSection = "drafts"
	// AscInboxQueued is a pull request in the merge queue.
	AscInboxQueued AscInboxSection = "queued"
	// AscInboxMerged is a recently merged pull request.
	AscInboxMerged AscInboxSection = "merged"
)

// IsValid reports whether s is one of the sections a client can ask for.
func (s AscInboxSection) IsValid() bool {
	switch s {
	case AscInboxNeedsReview, AscInboxReturned, AscInboxReady, AscInboxBlocked, AscInboxWaiting, AscInboxDrafts, AscInboxQueued, AscInboxMerged:
		return true
	}
	return false
}

// AscInboxReviewer is one reviewer's standing on a pull request: the latest review row that counts (a
// dismissed review and a pending one do not), or a review request.
type AscInboxReviewer struct {
	UserID int64
	// Type is the type of the reviewer's latest counting row: ReviewTypeRequest, ReviewTypeApprove,
	// ReviewTypeComment or ReviewTypeReject.
	Type ReviewType
	// Official marks a review by an assigned approver (it counts towards approval and blocks).
	Official bool
	// CommitID is the head commit the review was made on; empty for a request.
	CommitID string
}

// AscInboxFacts holds what the turn is derived from.
type AscInboxFacts struct {
	// HeadCommitID is the pull request's current version (its head commit).
	HeadCommitID string
	AuthorID     int64
	Draft        bool
	Merged       bool
	Closed       bool
	Queued       bool
	// ChecksFailing and Conflicted block a merge; Approved is true when the approval rule is met.
	ChecksFailing bool
	Conflicted    bool
	Approved      bool
	Reviewers     []AscInboxReviewer
}

// reviewer returns the standing of user in f, or nil.
func (f *AscInboxFacts) reviewer(userID int64) *AscInboxReviewer {
	for i := range f.Reviewers {
		if f.Reviewers[i].UserID == userID {
			return &f.Reviewers[i]
		}
	}
	return nil
}

// rejectedAtHead reports whether a reviewer asked for changes on the current version.
func (f *AscInboxFacts) rejectedAtHead() bool {
	for _, r := range f.Reviewers {
		if r.Type == ReviewTypeReject && r.CommitID == f.HeadCommitID {
			return true
		}
	}
	return false
}

// AscInboxSectionFor derives the section of the pull request described by f in the inbox of userID. It is a
// pure function of its inputs. A user who is neither the author nor a reviewer has no section.
func AscInboxSectionFor(f *AscInboxFacts, userID int64) AscInboxSection {
	if f == nil || userID <= 0 || (f.Closed && !f.Merged) {
		return AscInboxNone
	}
	isAuthor := f.AuthorID == userID
	me := f.reviewer(userID)
	if !isAuthor && me == nil {
		return AscInboxNone
	}
	if f.Merged {
		return AscInboxMerged
	}
	if isAuthor {
		switch {
		case f.Draft:
			return AscInboxDrafts
		case f.Queued:
			return AscInboxQueued
		case f.rejectedAtHead():
			return AscInboxReturned
		case f.ChecksFailing || f.Conflicted:
			return AscInboxBlocked
		case f.Approved:
			return AscInboxReady
		}
		return AscInboxWaiting
	}
	// A reviewer. A draft is not yet anyone's to review.
	if f.Draft {
		return AscInboxWaiting
	}
	switch me.Type {
	case ReviewTypeRequest:
		return AscInboxNeedsReview
	case ReviewTypeReject:
		// The reviewer asked for changes: it is the reviewer's turn again once a newer version exists.
		if me.CommitID != f.HeadCommitID {
			return AscInboxNeedsReview
		}
	}
	return AscInboxWaiting
}

// AscInboxMaxSnooze is the furthest ahead a snooze can be set.
const AscInboxMaxSnooze = 365 * 24 * time.Hour

// AscInboxState is one user's marks on one pull request. A row exists only while a mark is set.
type AscInboxState struct {
	ID           int64              `xorm:"pk autoincr"`
	UserID       int64              `xorm:"UNIQUE(user_pull) NOT NULL"`
	PullID       int64              `xorm:"UNIQUE(user_pull) INDEX NOT NULL"`
	Pinned       bool               `xorm:"NOT NULL DEFAULT false"`
	SnoozedUntil timeutil.TimeStamp `xorm:"NOT NULL DEFAULT 0"` // 0: not snoozed
	UpdatedUnix  timeutil.TimeStamp `xorm:"updated"`
}

// TableName gives the table its name.
func (*AscInboxState) TableName() string { return "asc_inbox_state" }

func init() {
	db.RegisterModel(new(AscInboxState))
}

// IsSnoozed reports whether the snooze is still running at now.
func (s *AscInboxState) IsSnoozed(now time.Time) bool {
	return s != nil && s.SnoozedUntil > 0 && int64(s.SnoozedUntil) > now.Unix()
}

// ErrAscInboxBadSnooze is returned for a snooze time beyond AscInboxMaxSnooze.
var ErrAscInboxBadSnooze = errors.New("snooze time is too far ahead")

// GetAscInboxState returns the marks of userID on pullID, or nil when none are set.
func GetAscInboxState(ctx context.Context, userID, pullID int64) (*AscInboxState, error) {
	s := new(AscInboxState)
	has, err := db.GetEngine(ctx).Where("user_id = ? AND pull_id = ?", userID, pullID).Get(s)
	if err != nil || !has {
		return nil, err
	}
	return s, nil
}

// ListAscInboxStates returns the marks of userID on the given pull requests, by pull id. Other users' marks
// are never returned.
func ListAscInboxStates(ctx context.Context, userID int64, pullIDs []int64) (map[int64]*AscInboxState, error) {
	out := make(map[int64]*AscInboxState, len(pullIDs))
	if userID <= 0 || len(pullIDs) == 0 {
		return out, nil
	}
	var rows []*AscInboxState
	if err := db.GetEngine(ctx).Where("user_id = ?", userID).In("pull_id", pullIDs).Find(&rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.PullID] = r
	}
	return out, nil
}

// SetAscInboxState sets the marks of userID on pullID. A nil argument leaves that mark unchanged. A snooze time
// at or before now clears the snooze; one beyond AscInboxMaxSnooze is refused. When no mark remains set the
// row is deleted. The caller has already checked that the user may see the pull request.
func SetAscInboxState(ctx context.Context, userID, pullID int64, pinned *bool, snoozedUntil *time.Time, now time.Time) (*AscInboxState, error) {
	if userID <= 0 || pullID <= 0 {
		return nil, fmt.Errorf("bad inbox state key")
	}
	if snoozedUntil != nil && snoozedUntil.After(now.Add(AscInboxMaxSnooze)) {
		return nil, ErrAscInboxBadSnooze
	}
	var result *AscInboxState
	err := db.WithTx(ctx, func(ctx context.Context) error {
		e := db.GetEngine(ctx)
		cur, err := GetAscInboxState(ctx, userID, pullID)
		if err != nil {
			return err
		}
		next := &AscInboxState{UserID: userID, PullID: pullID}
		if cur != nil {
			*next = *cur
		}
		if pinned != nil {
			next.Pinned = *pinned
		}
		if snoozedUntil != nil {
			if snoozedUntil.After(now) {
				next.SnoozedUntil = timeutil.TimeStamp(snoozedUntil.Unix())
			} else {
				next.SnoozedUntil = 0
			}
		}
		empty := !next.Pinned && next.SnoozedUntil == 0
		switch {
		case empty && cur != nil:
			_, err = e.ID(cur.ID).Delete(new(AscInboxState))
			return err
		case empty:
			return nil
		case cur == nil:
			_, err = e.Insert(next)
		default:
			_, err = e.ID(cur.ID).Cols("pinned", "snoozed_until").Update(next)
		}
		result = next
		return err
	})
	return result, err
}

// DeleteAscInboxStatesByPull drops every user's marks on a pull request (the pull request was deleted).
func DeleteAscInboxStatesByPull(ctx context.Context, pullID int64) error {
	_, err := db.GetEngine(ctx).Where("pull_id = ?", pullID).Delete(new(AscInboxState))
	return err
}

// GetAscInboxReviewers returns the standing of every reviewer of the issue: for each reviewer the latest review
// row that counts. A pending or dismissed review does not count; neither does a review by a team or an
// imported author (no user id).
func GetAscInboxReviewers(ctx context.Context, issueID int64) ([]AscInboxReviewer, error) {
	var rows []*Review
	if err := db.GetEngine(ctx).
		Where("issue_id = ? AND reviewer_id > 0 AND type <> ? AND dismissed = ?", issueID, ReviewTypePending, false).
		OrderBy("id ASC").Find(&rows); err != nil {
		return nil, err
	}
	latest := make(map[int64]int, len(rows))
	var out []AscInboxReviewer
	for _, r := range rows {
		rv := AscInboxReviewer{UserID: r.ReviewerID, Type: r.Type, Official: r.Official, CommitID: r.CommitID}
		if i, ok := latest[r.ReviewerID]; ok {
			out[i] = rv
			continue
		}
		latest[r.ReviewerID] = len(out)
		out = append(out, rv)
	}
	return out, nil
}
