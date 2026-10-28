// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"net/http"
	"strings"
	"time"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/automerge"
	"forgejo.org/services/context"

	"forgejo.org/models/unit"
)

// The estimates and the live updates of the merge queue page. The hub keeps the queue itself (services/automerge);
// asc's MergeQueue service keeps the same entries by change id with a timing: the stage, the progress, and how long
// until each starts and lands, from the median of the last landings. This file reads that timing after the same checks as
// the page (the pull requests of the repository are readable) and joins it to the hub's rows by change id. A row asc does not
// know has no estimate; nothing is guessed.

// ascQueueTiming is what the page shows beside one waiting row.
type ascQueueTiming struct {
	Change   string `json:"change"`
	State    string `json:"state"`
	Position uint32 `json:"position"`
	Stage    string `json:"stage"`
	Detail   string `json:"detail,omitempty"`
	Percent  uint32 `json:"percent"`
	StartsIn string `json:"startsIn,omitempty"`
	ETA      string `json:"eta,omitempty"`
}

// ascQueueSnapshot is the answer of the live endpoint and the data of the first render.
type ascQueueSnapshot struct {
	Paused  bool             `json:"paused"`
	Median  string           `json:"median,omitempty"`
	Entries []ascQueueTiming `json:"entries"`
	Resume  string           `json:"resume,omitempty"`
}

var ascQueueStages = map[string]bool{"waiting": true, "checking": true, "landing": true, "done": true}

var ascQueueStates = map[ascv1.QueueEntryState]string{
	ascv1.QueueEntryState_QUEUE_ENTRY_STATE_WAITING:      "waiting",
	ascv1.QueueEntryState_QUEUE_ENTRY_STATE_REVALIDATING: "checking",
	ascv1.QueueEntryState_QUEUE_ENTRY_STATE_READY:        "ready",
	ascv1.QueueEntryState_QUEUE_ENTRY_STATE_LANDING:      "landing",
	ascv1.QueueEntryState_QUEUE_ENTRY_STATE_LANDED:       "landed",
	ascv1.QueueEntryState_QUEUE_ENTRY_STATE_BLOCKED:      "blocked",
	ascv1.QueueEntryState_QUEUE_ENTRY_STATE_REMOVED:      "removed",
}

func ascQueueTimingOf(e *ascv1.QueueEntry) (ascQueueTiming, bool) {
	change := ascagentui.ChangeText(e.GetChange())
	state, ok := ascQueueStates[e.GetState()]
	if change == "" || !ok {
		return ascQueueTiming{}, false
	}
	t := e.GetTiming()
	out := ascQueueTiming{Change: change, State: state, Position: min(e.GetPosition(), 100000), Percent: min(t.GetProgressPercent(), 100)}
	if s := t.GetStage(); ascQueueStages[s] {
		out.Stage = s
	}
	out.Detail = ascagentui.CleanLine(t.GetDetail(), 200)
	if s := t.GetStartsInSeconds(); s > 0 {
		out.StartsIn = ascagentui.FormatDuration(uint64(s))
	}
	if s := t.GetEtaSeconds(); s > 0 {
		out.ETA = ascagentui.FormatDuration(uint64(s))
	}
	return out, true
}

func ascQueueSnapshotOf(r *ascv1.ListQueueResponse) ascQueueSnapshot {
	snap := ascQueueSnapshot{Paused: r.GetPaused(), Entries: []ascQueueTiming{}}
	if m := r.GetMedianLandSeconds(); m > 0 {
		snap.Median = ascagentui.FormatDuration(uint64(m))
	}
	for _, e := range r.GetEntries() {
		if t, ok := ascQueueTimingOf(e); ok && len(snap.Entries) < 200 {
			snap.Entries = append(snap.Entries, t)
		}
	}
	return snap
}

// ascQueueRead lists the live entries of branch as the internal credential (the caller has passed the page's checks).
func ascQueueRead(ctx gocontext.Context, name, branch string) (*ascQueueSnapshot, error) {
	conn, err := ascRPCConn()
	if err != nil {
		return nil, err
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx), ascAgentsCallTimeout)
	defer cancel()
	resp, err := ascv1.NewMergeQueueClient(conn).List(callCtx, &ascv1.ListQueueRequest{Repo: &ascv1.RepoRef{Name: name}, Target: branch, Limit: 200})
	if err != nil {
		return nil, err
	}
	snap := ascQueueSnapshotOf(resp)
	return &snap, nil
}

// ascQueueGate is the checks of the queue page, for the live endpoint.
func ascQueueGate(ctx *context.Context) (name, branch string, ok bool) {
	if !setting.Asc.Enabled || !ctx.Repo.CanRead(unit.TypePullRequests) {
		ctx.NotFound("asc queue", nil)
		return "", "", false
	}
	repo := ctx.Repo.Repository
	if backed, err := automerge.AscQueueBackedBy(repo); err != nil || !backed {
		ctx.NotFound("asc queue", nil)
		return "", "", false
	}
	branch = strings.TrimSpace(ctx.FormString("branch"))
	if branch == "" {
		branch = repo.DefaultBranch
	}
	if !ascValidBranch(branch) {
		ctx.NotFound("asc queue", nil)
		return "", "", false
	}
	name, err := ascGraphRepoName(repo)
	if err != nil {
		ctx.NotFound("asc queue", nil)
		return "", "", false
	}
	return name, branch, true
}

// ascQueueLiveWait is how long the live endpoint holds a request open for news. Tests shorten it.
var ascQueueLiveWait = 20 * time.Second

// AscQueueLive serves `GET /{owner}/{repo}/asc/queue/live?branch=`: the timing of the live entries, held open until
// asc's Watch stream has news or the wait is over. The page script polls it again at once.
func AscQueueLive(ctx *context.Context) {
	name, branch, ok := ascQueueGate(ctx)
	if !ok {
		return
	}
	ascAgentsNoStore(ctx)
	if ctx.Doer == nil {
		// The hub's queue page is for signed-in people; an anonymous visitor reads the page but gets no live data.
		ctx.JSON(http.StatusOK, ascQueueSnapshot{Entries: []ascQueueTiming{}})
		return
	}
	// Wait for one event first (or the timeout), then read the whole list, so a reply is always the full truth.
	if conn, err := ascRPCConn(); err == nil {
		wctx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx.Req.Context()), ascQueueLiveWait)
		stream, err := ascv1.NewMergeQueueClient(conn).Watch(wctx, &ascv1.WatchQueueRequest{Repo: &ascv1.RepoRef{Name: name}, Target: branch})
		if err == nil {
			_, _ = stream.Recv() // an event, an end or the timeout: all mean "read again"
		}
		cancel()
	}
	if ctx.Req.Context().Err() != nil {
		return
	}
	snap, err := ascQueueRead(ctx.Req.Context(), name, branch)
	if err != nil {
		log.Warn("asc queue: live list failed: %s", ascagentuiLogLine(ascagentui.Classify(err)))
		ctx.JSON(http.StatusBadGateway, map[string]string{"error": ascagentui.Classify(err).Message})
		return
	}
	ctx.JSON(http.StatusOK, snap)
}
