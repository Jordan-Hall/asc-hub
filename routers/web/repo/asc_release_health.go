// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"slices"
	"strconv"
	"time"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
	"forgejo.org/services/context"

	"xorm.io/builder"
)

// The Release health card of the releases rail (board Releases): the cadence (the median gap between published
// releases), the rollbacks among the last releases, and the releases per week over the last weeks. A rollback is a
// release cut from a commit that is older than the commit of the release before it: it ships code the release before
// had already moved past.

const (
	// relHealthReleases is how many published releases the cadence is read from.
	relHealthReleases = 25
	// relHealthRollbackWindow is how many of the newest releases are checked for a rollback.
	relHealthRollbackWindow = 6
	// relHealthWeeks is the span of the sparkline.
	relHealthWeeks = 12
)

// relHealth is what the card shows.
type relHealth struct {
	Cadence   string // the median gap, "2.4 w" or "5 d"
	Rollbacks int
	Window    int // how many releases the rollbacks were counted over
	Weeks     int
	Recent    int    // releases in the span of the sparkline
	Points    string // the sparkline, in a 100 by 24 box
}

// relHealthRelease is one published release, newest first.
type relHealthRelease struct {
	Sha     string
	Created int64
}

// relMedianGap is the median of the gaps between consecutive releases (newest first), in seconds; 0 below two releases.
func relMedianGap(rels []relHealthRelease) int64 {
	if len(rels) < 2 {
		return 0
	}
	gaps := make([]int64, 0, len(rels)-1)
	for i := 0; i+1 < len(rels); i++ {
		gaps = append(gaps, max(rels[i].Created-rels[i+1].Created, 0))
	}
	slices.Sort(gaps)
	n := len(gaps)
	if n%2 == 1 {
		return gaps[n/2]
	}
	return (gaps[n/2-1] + gaps[n/2]) / 2
}

// relCadenceText words a gap: weeks with one decimal from two weeks on, whole days below, "< 1 d" under a day.
func relCadenceText(ctx *context.Context, gap int64) string {
	const day = 24 * 3600
	switch {
	case gap >= 14*day:
		return ctx.Locale.TrString("asc_rp.rel_cadence_weeks", strconv.FormatFloat(float64(gap)/(7*day), 'f', 1, 64))
	case gap >= day:
		return ctx.Locale.TrString("asc_rp.rel_cadence_days", strconv.FormatInt(gap/day, 10))
	}
	return ctx.Locale.TrString("asc_rp.rel_cadence_under_day")
}

// relRollbacks counts, among the newest window releases, those whose commit is an ancestor of the commit of the
// release before it and not the same commit. isAncestor reports whether a is an ancestor of b.
func relRollbacks(rels []relHealthRelease, window int, isAncestor func(a, b string) bool) (rollbacks, counted int) {
	for i := 0; i < len(rels) && i < window; i++ {
		counted++
		if i+1 >= len(rels) {
			break
		}
		cur, prev := rels[i].Sha, rels[i+1].Sha
		if cur != prev && isAncestor(cur, prev) {
			rollbacks++
		}
	}
	return rollbacks, counted
}

// prepareReleaseHealth sets ctx.Data["ReleaseHealth"] when the repository has two published releases or more. It reads
// the code, so it is only for a viewer who may read it.
func prepareReleaseHealth(ctx *context.Context) {
	if ctx.Repo.GitRepo == nil || !ctx.Repo.CanRead(unit.TypeCode) {
		return
	}
	var rows []*repo_model.Release
	err := db.GetEngine(ctx).Where(builder.Eq{"repo_id": ctx.Repo.Repository.ID, "is_draft": false, "is_tag": false}).
		And(builder.Neq{"sha1": ""}).OrderBy("created_unix DESC, id DESC").Limit(relHealthReleases).Find(&rows)
	if err != nil {
		log.Warn("the release health of %s is left out: %v", ctx.Repo.Repository.FullName(), err)
		return
	}
	if len(rows) < 2 {
		return
	}
	rels := make([]relHealthRelease, 0, len(rows))
	times := make([]int64, 0, len(rows))
	for _, r := range rows {
		rels = append(rels, relHealthRelease{Sha: r.Sha1, Created: int64(r.CreatedUnix)})
		times = append(times, int64(r.CreatedUnix))
	}
	dir := ctx.Repo.GitRepo.Path
	rollbacks, counted := relRollbacks(rels, relHealthRollbackWindow, func(a, b string) bool {
		_, _, err := git.NewCommand(ctx, "merge-base", "--is-ancestor").AddDynamicArguments(a, b).RunStdString(&git.RunOpts{Dir: dir})
		return err == nil
	})
	counts := weeklyCounts(times, time.Now(), relHealthWeeks)
	recent := 0
	for _, c := range counts {
		recent += c
	}
	ctx.Data["ReleaseHealth"] = &relHealth{
		Cadence:   relCadenceText(ctx, relMedianGap(rels)),
		Rollbacks: rollbacks,
		Window:    counted,
		Weeks:     relHealthWeeks,
		Recent:    recent,
		Points:    sparkPoints(counts),
	}
}
