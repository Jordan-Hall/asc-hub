// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"strconv"
	"strings"
	"time"

	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/translation"
	"forgejo.org/services/ascnotify"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/gitdiff"
	issue_service "forgejo.org/services/issue"
)

// Reviewers of the open-change form (board NewChange): Review.SuggestReviewers for the files of the comparison, before
// the change exists. Each row says why (the policy names them as a writer, the share of the threads they opened on these
// files, how fast they usually review), when they are away and whether they are an agent. Add marks a person to be asked
// when the change opens; asking is the hub's own review request, with its own checks.

const (
	ascNewChangeReviewersMax = 5
	// ascNewChangeReviewerField is the form field of the people to ask (user ids).
	ascNewChangeReviewerField = "asc_reviewer"
	ascNewChangeReviewPaths   = 1000
)

// AscNewChangeReviewer is one suggested reviewer of the open-change form.
type AscNewChangeReviewer struct {
	User          *user_model.User // nil when the hub cannot ask them (an agent without an account here)
	Handle        string
	IsAgent       bool
	NeverApproves bool
	Owns          string
	Knows         string
	KnowsPercent  uint32
	ReviewSecs    uint64
	Away          time.Time // the last day they are out; zero when they are not
}

// ReviewIn is how long a review of theirs usually takes, in words, or "" when asc does not know.
func (r AscNewChangeReviewer) ReviewIn(l translation.Locale) string {
	switch d := time.Duration(r.ReviewSecs) * time.Second; {
	case r.ReviewSecs == 0:
		return ""
	case d < time.Hour:
		return l.TrString("asc_rp.rev_minutes", max(1, int(d/time.Minute)))
	case d < 48*time.Hour:
		return l.TrString("asc_rp.rev_hours", int((d+30*time.Minute)/time.Hour))
	default:
		return l.TrString("asc_rp.rev_days", int((d+12*time.Hour)/(24*time.Hour)))
	}
}

// ascNewChangePaths are the files of the comparison, with the old name of a renamed one, at most ascNewChangeReviewPaths.
func ascNewChangePaths(diff *gitdiff.Diff) [][]byte {
	if diff == nil {
		return nil
	}
	var out [][]byte
	add := func(name string) {
		if name != "" && len(out) < ascNewChangeReviewPaths {
			out = append(out, []byte(name))
		}
	}
	for _, f := range diff.Files {
		add(f.Name)
		if f.OldName != f.Name {
			add(f.OldName)
		}
	}
	return out
}

// ascNewChangeReviewersOf checks the answer of asc and keeps, in its order, the people the hub offers and the agents (which
// are only listed unless they have an account the hub offers). away gives the last day a person is out.
func ascNewChangeReviewersOf(resp *ascv1.SuggestReviewersResponse, offered []*user_model.User, away func(*user_model.User) time.Time) []AscNewChangeReviewer {
	byName := make(map[string]*user_model.User, len(offered))
	for _, u := range offered {
		if u != nil {
			byName[strings.ToLower(u.Name)] = u
		}
	}
	var out []AscNewChangeReviewer
	for _, s := range resp.GetReviewers() {
		handle := asctrust.CleanText(s.GetHandle(), 100)
		u := byName[strings.ToLower(handle)]
		if handle == "" || (u == nil && !s.GetIsAgent()) {
			continue
		}
		row := AscNewChangeReviewer{
			User:          u,
			Handle:        handle,
			IsAgent:       s.GetIsAgent(),
			NeverApproves: !s.GetCanApprove(),
			Owns:          asctrust.CleanText(s.GetOwns(), 200),
			ReviewSecs:    s.GetMedianReviewSecs(),
		}
		if k := asctrust.CleanText(string(s.GetKnows()), 200); k != "" {
			row.Knows, row.KnowsPercent = k, min(s.GetKnowsPercent(), 100)
		}
		if u != nil {
			row.Away = away(u)
		}
		out = append(out, row)
		if len(out) >= ascNewChangeReviewersMax {
			break
		}
	}
	return out
}

// ascAwayUntil is the last day the person said they are out, or zero.
func ascAwayUntil(ctx gocontext.Context, u *user_model.User) time.Time {
	day, ok := ascnotify.Load(ctx, u.ID).AwayUntil(time.Now())
	if !ok {
		return time.Time{}
	}
	return day
}

// prepareAscNewChangeReviewers puts the suggestions into ctx.Data["AscNewChangeReviewers"]. It never fails the page.
func prepareAscNewChangeReviewers(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if !setting.Asc.Enabled || !ctx.IsSigned || repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		return
	}
	diff, _ := ctx.Data["Diff"].(*gitdiff.Diff)
	paths := ascNewChangePaths(diff)
	name, err := ascGraphRepoName(repo)
	if err != nil || len(paths) == 0 {
		return
	}
	offered, err := repo_model.GetReviewers(ctx, repo, ctx.Doer.ID, ctx.Doer.ID)
	if err != nil {
		log.Warn("asc new change reviewers: %v", err)
		return
	}
	conn, err := ascRPCConn()
	if err != nil {
		return
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx), ascBriefTimeout)
	defer cancel()
	resp, err := ascv1.NewReviewClient(conn).SuggestReviewers(callCtx, &ascv1.SuggestReviewersRequest{
		Repo: &ascv1.RepoRef{Name: name}, Paths: paths, Author: ctx.Doer.Name, Limit: ascNewChangeReviewersMax + 5,
	})
	if err != nil {
		log.Warn("asc new change reviewers: %v", err)
		return
	}
	if list := ascNewChangeReviewersOf(resp, offered, func(u *user_model.User) time.Time { return ascAwayUntil(ctx, u) }); len(list) > 0 {
		ctx.Data["AscNewChangeReviewers"] = list
	}
}

// ascRequestNewChangeReviewers asks the people marked on the open-change form to review the change that was just opened,
// with the hub's own checks. The change is open either way: a person who cannot be asked is named in a warning.
func ascRequestNewChangeReviewers(ctx *context.Context, issue *issues_model.Issue) {
	seen := map[int64]bool{}
	var refused []string
	for _, raw := range ctx.Req.PostForm[ascNewChangeReviewerField] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 || seen[id] || len(seen) >= ascNewChangeReviewersMax {
			continue
		}
		seen[id] = true
		reviewer, err := user_model.GetUserByID(ctx, id)
		if err != nil {
			continue
		}
		if err := issue_service.IsValidReviewRequest(ctx, reviewer, ctx.Doer, true, issue); err != nil {
			refused = append(refused, reviewer.Name)
			continue
		}
		if _, err := issue_service.ReviewRequest(ctx, issue, ctx.Doer, reviewer, true); err != nil {
			log.Warn("asc new change: asking %s to review: %v", reviewer.Name, err)
			refused = append(refused, reviewer.Name)
		}
	}
	if len(refused) > 0 {
		ctx.Flash.Warning(ctx.Tr("asc_rp.rev_not_asked", strings.Join(refused, ", ")))
	}
}
