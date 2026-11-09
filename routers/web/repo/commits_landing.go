// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"strconv"
	"strings"
	"time"

	"forgejo.org/modules/git"
	"forgejo.org/services/context"
)

// The "Landing rate" card of the history (design board "History"): the changes landed on the viewed ref in each of
// the last twelve weeks, this week's count and how it compares with last week. It comes from the repository alone,
// with one git log, like the Activity card of the repository home.

// HistoryLandingBar is one week of the card's bar chart, in a 100 by 32 box.
type HistoryLandingBar struct {
	X, Y, W, H string
	Count      int
	Current    bool // this week
}

// HistoryLanding is the card.
type HistoryLanding struct {
	Weeks    int
	Total    int
	ThisWeek int
	Delta    int // this week less last week
	Fewer    int // -Delta when this week is behind
	Bars     []HistoryLandingBar
}

// historyLandingOf builds the card from the counts per week, oldest first; nil when nothing landed in the weeks.
func historyLandingOf(counts []int) *HistoryLanding {
	n := len(counts)
	if n < 2 {
		return nil
	}
	l := &HistoryLanding{Weeks: n, ThisWeek: counts[n-1], Delta: counts[n-1] - counts[n-2]}
	most := 0
	for _, c := range counts {
		l.Total += c
		most = max(most, c)
	}
	l.Fewer = max(-l.Delta, 0)
	if l.Total == 0 {
		return nil
	}
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }
	slot := 100 / float64(n)
	for i, c := range counts {
		h := 1.0 // an empty week shows as a hairline
		if c > 0 {
			h = max(float64(c)*31/float64(most), 2)
		}
		l.Bars = append(l.Bars, HistoryLandingBar{
			X: f(float64(i)*slot + slot*0.15), Y: f(32 - h), W: f(slot * 0.7), H: f(h),
			Count: c, Current: i == n-1,
		})
	}
	return l
}

// historyLandingTimes reads the commit times of `git log --format=%ct`; a line that does not parse is skipped.
func historyLandingTimes(out string) []int64 {
	var times []int64
	for line := range strings.SplitSeq(out, "\n") {
		if t, err := strconv.ParseInt(strings.TrimSpace(line), 10, 64); err == nil {
			times = append(times, t)
		}
	}
	return times
}

// prepareHistoryLanding sets HistoryLanding for the viewed ref; a failure leaves the card out.
func prepareHistoryLanding(ctx *context.Context) {
	if ctx.Repo.GitRepo == nil || ctx.Repo.Commit == nil {
		return
	}
	now := time.Now()
	since := now.Add(-time.Duration(homeActivityWeeks) * 7 * 24 * time.Hour)
	out, _, err := git.NewCommand(ctx, "log", "--format=%ct").AddOptionFormat("--since=%d", since.Unix()).AddDynamicArguments(ctx.Repo.Commit.ID.String()).
		RunStdString(&git.RunOpts{Dir: ctx.Repo.GitRepo.Path})
	if err != nil {
		return
	}
	if l := historyLandingOf(weeklyCounts(historyLandingTimes(out), now, homeActivityWeeks)); l != nil {
		ctx.Data["HistoryLanding"] = l
	}
}
