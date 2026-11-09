// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	gocontext "context"
	"fmt"
	"net/url"
	"strings"

	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/translation"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/automerge"
)

const ascDashQueueMax = 5

// AscDashQueueCard is the merge queue card of the dashboard: the person's changes that wait, whether a queue they wait
// in is landing now, and the median wait of those queues as asc measures it.
type AscDashQueueCard struct {
	Rows    []AscDashQueueRow
	Running bool   // a queue of the rows holds its branch: a change is landing now
	Median  string // "9 min", the median time to land of the first queue asc answers for; empty when unknown
}

// ascDashQueueTiming is what asc knows of the queue of one branch: the median time to land and the estimate of each
// change (by change id). Tests replace it.
var ascDashQueueTiming = ascDashQueueTimingFromAsc

func ascDashQueueTimingFromAsc(ctx gocontext.Context, repo *repo_model.Repository, branch string) (median string, eta map[string]string, err error) {
	name, err := ascbackend.RepoName(setting.RepoRootPath, repo_model.RepoPath(repo.OwnerName, repo.Name))
	if err != nil {
		return "", nil, err
	}
	conn, err := ascDraftsConn()
	if err != nil {
		return "", nil, err
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx), ascDashAgentTimeout)
	defer cancel()
	target := branch
	if !strings.HasPrefix(target, "refs/") {
		target = "refs/heads/" + branch
	}
	resp, err := ascv1.NewMergeQueueClient(conn).List(callCtx, &ascv1.ListQueueRequest{Repo: &ascv1.RepoRef{Name: name}, Target: target, Limit: 200})
	if err != nil {
		return "", nil, err
	}
	if m := resp.GetMedianLandSeconds(); m > 0 {
		median = ascagentui.FormatDuration(uint64(m))
	}
	eta = map[string]string{}
	for _, e := range resp.GetEntries() {
		if c := ascagentui.ChangeText(e.GetChange()); c != "" && e.GetTiming().GetEtaSeconds() > 0 {
			eta[c] = ascagentui.FormatDuration(uint64(e.GetTiming().GetEtaSeconds()))
		}
	}
	return median, eta, nil
}

// ascDashChecksText is "2 of 3 checks" for the checks of a head that are not all done or not all passing, empty when
// the head has no checks or all of them pass.
func ascDashChecksText(l translation.Locale, info *AscInboxInfo) string {
	if info == nil {
		return ""
	}
	total := info.ChecksPass + info.ChecksFail + info.ChecksPending
	if total == 0 || info.ChecksPass == total {
		return ""
	}
	return l.TrString("asc_work.dash_queue_checks", info.ChecksPass, total)
}

// AscDashQueueRow is one change of the person that waits in a merge queue.
type AscDashQueueRow struct {
	Title    string
	Where    string // "owner/repo - main"
	Link     string
	State    string // queued, landing or blocked
	Position int    // 1 lands next
	Label    string // "2nd in the queue", "Landing now", "Waits for a new head"
	Checks   string // "2 of 3 checks" while the checks of its head run or fail, else empty
	ETA      string // "lands in about 4 min" when asc has an estimate, else empty
	Open     string // the merge queue page of its branch
}

// ascDashQueue lists the changes among entries that the person queued (or wrote) and that wait in a merge queue, with their
// place in the queue of their branch, the checks of their head and the estimate asc gives. Only changes of the inbox are
// looked at, so the person can read them all. asc not answering leaves the estimates and the median out.
func ascDashQueue(ctx gocontext.Context, doer *user_model.User, entries []*AscInboxEntry, l translation.Locale) AscDashQueueCard {
	ids := make([]int64, 0, len(entries))
	byPull := map[int64]*AscInboxEntry{}
	for _, e := range entries {
		ids = append(ids, e.PullID)
		byPull[e.PullID] = e
	}
	queued, err := automerge.AscQueueEntries(ctx, ids)
	if err != nil {
		log.Error("asc dashboard: merge queue of user %d: %v", doer.ID, err)
		return AscDashQueueCard{}
	}
	var card AscDashQueueCard
	out := []AscDashQueueRow{}
	views := map[string]*automerge.AscQueueView{}
	type timing struct {
		median string
		eta    map[string]string
	}
	timings := map[string]*timing{}
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
			card.Running = card.Running || view.Landing
		}
		tm := timings[key]
		if tm == nil {
			tm = &timing{}
			if setting.Asc.Enabled {
				if med, eta, err := ascDashQueueTiming(ctx, repo, q.Branch); err == nil {
					tm.median, tm.eta = med, eta
				} else {
					log.Debug("asc dashboard: no queue timing of %s %s: %v", repo.FullName(), q.Branch, err)
				}
			}
			timings[key] = tm
			if card.Median == "" {
				card.Median = tm.median
			}
		}
		row := AscDashQueueRow{
			Title: e.Title, Where: repo.FullName() + " - " + q.Branch, Link: e.Link, State: q.State,
			Open:   repo.Link() + "/asc/queue?branch=" + url.QueryEscape(q.Branch),
			Checks: ascDashChecksText(l, ascInboxInfoLoad(ctx, e)),
		}
		if t := tm.eta[q.AscChange]; t != "" && q.State != automerge.AscQueueBlocked {
			row.ETA = l.TrString("asc_work.dash_queue_eta", t)
		}
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
	card.Rows = out
	return card
}
