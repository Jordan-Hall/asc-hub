// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"fmt"

	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/translation"
	"forgejo.org/services/automerge"
)

const ascDashQueueMax = 5

// AscDashQueueRow is one change of the person that waits in a merge queue.
type AscDashQueueRow struct {
	Title    string
	Where    string // "owner/repo - main"
	Link     string
	State    string // queued, landing or blocked
	Position int    // 1 lands next
	Label    string // "2nd in the queue", "Landing now", "Waits for a new head"
}

// ascDashQueue lists the changes among entries that the person queued (or wrote) and that wait in a merge queue, with their
// place in the queue of their branch. Only changes of the inbox are looked at, so the person can read them all.
func ascDashQueue(ctx gocontext.Context, doer *user_model.User, entries []*AscInboxEntry, l translation.Locale) []AscDashQueueRow {
	ids := make([]int64, 0, len(entries))
	byPull := map[int64]*AscInboxEntry{}
	for _, e := range entries {
		ids = append(ids, e.PullID)
		byPull[e.PullID] = e
	}
	queued, err := automerge.AscQueueEntries(ctx, ids)
	if err != nil {
		log.Error("asc dashboard: merge queue of user %d: %v", doer.ID, err)
		return nil
	}
	var out []AscDashQueueRow
	views := map[string]*automerge.AscQueueView{}
	for _, id := range ids {
		q := queued[id]
		if q == nil || len(out) == ascDashQueueMax {
			continue
		}
		if q.State != automerge.AscQueueQueued && q.State != automerge.AscQueueLanding && q.State != automerge.AscQueueBlocked {
			continue
		}
		e := byPull[id]
		if q.DoerID != doer.ID && e.Author != doer.Name {
			continue
		}
		repo, err := repo_model.GetRepositoryByID(ctx, q.RepoID)
		if err != nil {
			continue
		}
		key := fmt.Sprintf("%d/%s", q.RepoID, q.Branch)
		view := views[key]
		if view == nil {
			if view, err = automerge.AscQueueViewOf(ctx, repo, q.Branch, doer); err != nil {
				continue
			}
			views[key] = view
		}
		row := AscDashQueueRow{Title: e.Title, Where: repo.FullName() + " - " + q.Branch, Link: e.Link, State: q.State}
		for _, r := range view.Active {
			if r.Entry != nil && r.Entry.PullID == id {
				row.Position = r.Position
			}
		}
		switch {
		case q.State == automerge.AscQueueLanding:
			row.Label = l.TrString("asc_work.dash_queue_landing")
		case q.State == automerge.AscQueueBlocked:
			row.Label = l.TrString("asc_work.dash_queue_blocked")
		case row.Position > 0:
			row.Label = l.TrString("asc_work.dash_queue_position", row.Position)
		default:
			row.Label = l.TrString("asc_work.dash_queue_waiting")
		}
		out = append(out, row)
	}
	return out
}
