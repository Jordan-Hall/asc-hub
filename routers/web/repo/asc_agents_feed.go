// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"errors"
	"io"
	"time"

	repo_model "forgejo.org/models/repo"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascagentui"
)

// The live feed of an agent run. asc streams events (Agents.WatchRun, resumable by `after_seq`); the browser polls
// the hub (`GET .../runs/{id}/events?after=N`), the way the Actions log view does, so a proxy that buffers streams
// cannot stall the page. One poll holds the request open until the first new event (or until
// ascAgentsPollWait), gives the burst that follows a short moment to arrive, and answers with what it has.
// The answer says `done` when asc ended the stream, which it does once the run is finished and every event was
// sent.

var (
	// ascAgentsPollWait is how long a poll of an active run waits for news. Tests shorten it.
	ascAgentsPollWait = 8 * time.Second
	// ascAgentsDrainWait is the grace after the first event of a poll, so the rest of a burst rides along.
	ascAgentsDrainWait = 150 * time.Millisecond
	// ascAgentsBacklogWait is the same for the first render of the page.
	ascAgentsBacklogWait  = 1500 * time.Millisecond
	ascAgentsBacklogDrain = 300 * time.Millisecond
)

const (
	ascAgentsPollMaxEvents    = 500
	ascAgentsPollMaxBytes     = 512 << 10
	ascAgentsBacklogMaxEvents = 2000
	ascAgentsBacklogMaxBytes  = 2 << 20
	// ascAgentsShownEvents is how many of the newest events the page itself draws; the script appends the rest.
	ascAgentsShownEvents = 200
)

var errAscAgentsBadAnswer = ascagentui.ErrBadAnswer

// ascAgentEvent is one event as drawn (page and feed). Every text has been cleaned; none is markup.
type ascAgentEvent struct {
	Seq       uint64 `json:"seq"`
	Clock     string `json:"clock"` // hh:mm:ss UTC
	ISO       string `json:"iso"`
	Kind      string `json:"kind"` // message, tool_call, ...
	Word      string `json:"word"`
	Tone      string `json:"tone"`
	Quiet     bool   `json:"quiet"` // the agent's own words and bookkeeping: hidden unless "show everything"
	Text      string `json:"text,omitempty"`
	Tool      string `json:"tool,omitempty"`
	Change    string `json:"change,omitempty"`    // short change id
	ChangeURL string `json:"changeUrl,omitempty"` // the review page, when the change has one
}

// ascAgentFeedBatch is what one collection read.
type ascAgentFeedBatch struct {
	events []*ascv1.AgentRunEvent
	ended  bool // asc closed the stream: the run is finished and everything was sent
}

// ascAgentsCollect reads events of a run after `after`. It stops at the first of: the stream ends, `maxEvents` or
// `maxBytes` is reached, `wait` passes, or `drain` passes after the first event. Events are checked: they must
// belong to the run and their sequence numbers must rise above `after`; anything else is a bad answer and nothing
// of the batch is used.
func ascAgentsCollect(callCtx gocontext.Context, api ascv1.AgentsClient, ref *ascv1.RepoRef, id, after uint64, wait, drain time.Duration, maxEvents, maxBytes int) (*ascAgentFeedBatch, error) {
	streamCtx, cancel := gocontext.WithTimeout(callCtx, wait)
	defer cancel()
	stream, err := api.WatchRun(streamCtx, &ascv1.WatchAgentRunRequest{Repo: ref, Id: id, AfterSeq: after})
	if err != nil {
		return nil, err
	}
	out := &ascAgentFeedBatch{}
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	last, size := after, 0
	for len(out.events) < maxEvents && size < maxBytes {
		ev, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			out.ended = true
			break
		}
		if err != nil {
			// Our own deadline (or the drain cancel) is the normal end of a wait; the caller's cancel is not.
			if streamCtx.Err() != nil && callCtx.Err() == nil {
				break
			}
			return nil, err
		}
		if ev == nil || ev.GetRunId() != id || ev.GetSeq() <= last {
			return nil, errAscAgentsBadAnswer
		}
		last = ev.GetSeq()
		size += len(ev.GetText()) + len(ev.GetTool()) + 64
		out.events = append(out.events, ev)
		if timer == nil {
			timer = time.AfterFunc(drain, cancel)
		}
	}
	return out, nil
}

// ascAgentEventRows turns events into rows. changeURLs maps the text form of a change id to its review page.
func ascAgentEventRows(events []*ascv1.AgentRunEvent, changeURLs map[string]string) []ascAgentEvent {
	rows := make([]ascAgentEvent, 0, len(events))
	for _, ev := range events {
		word, tone := ascagentui.EventKind(ev.GetKind())
		row := ascAgentEvent{
			Seq:   ev.GetSeq(),
			Kind:  ascAgentKindSlug(ev.GetKind()),
			Word:  word,
			Tone:  tone,
			Text:  ascagentui.CleanText(ev.GetText(), ascagentui.MaxEventText),
			Tool:  ascagentui.CleanLine(ev.GetTool(), 64),
			Quiet: ev.GetKind() == ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_MESSAGE || ev.GetKind() == ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_USAGE || ev.GetKind() == ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_STATE,
		}
		if t := ascagentui.TimeMs(ev.GetTimeMs()); !t.IsZero() {
			row.Clock = t.Format("15:04:05")
			row.ISO = t.Format(time.RFC3339)
		}
		switch ev.GetKind() {
		case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_STATE:
			if row.Text == "" {
				row.Text = ascagentui.RunState(ev.GetState()).Label
			}
		case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_USAGE:
			if row.Text == "" {
				row.Text = ascAgentUsageLine(ev.GetUsage())
			}
		case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_CHANGE:
			if text := ascagentui.ChangeText(ev.GetChange()); text != "" {
				row.Change = ascagentui.ShortChange(text)
				row.ChangeURL = changeURLs[text]
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func ascAgentUsageLine(u *ascv1.AgentBudgetUsage) string {
	if u == nil {
		return ""
	}
	return ascagentui.FormatCount(u.GetTokens()) + " tokens, " + ascagentui.FormatUSD(u.GetCostMicroUsd()) + ", " + ascagentui.FormatCount(uint64(u.GetChanges())) + " changes"
}

func ascAgentKindSlug(k ascv1.AgentRunEventKind) string {
	switch k {
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_STATE:
		return "state"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_MESSAGE:
		return "message"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_INPUT:
		return "input"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_TOOL_CALL:
		return "tool_call"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_TOOL_RESULT:
		return "tool_result"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_CHANGE:
		return "change"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_USAGE:
		return "usage"
	case ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_ERROR:
		return "error"
	}
	return "event"
}

// ascAgentEventChangeURLs resolves the changes named by events to review pages.
func ascAgentEventChangeURLs(ctx gocontext.Context, repo *repo_model.Repository, repoLink string, events []*ascv1.AgentRunEvent, canReadPulls bool) map[string]string {
	var raw [][]byte
	for _, ev := range events {
		if ev.GetKind() == ascv1.AgentRunEventKind_AGENT_RUN_EVENT_KIND_CHANGE && len(ev.GetChange()) == 16 {
			raw = append(raw, ev.GetChange())
		}
	}
	if len(raw) == 0 {
		return nil
	}
	links, err := ascagentui.ChangeLinks(ctx, repo, repoLink, raw, canReadPulls)
	if err != nil {
		return nil // a link is a convenience; the event is still drawn
	}
	urls := make(map[string]string, len(links))
	for _, l := range links {
		if l.URL != "" {
			urls[l.Text] = l.URL
		}
	}
	return urls
}
