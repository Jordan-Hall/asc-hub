// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	actions_model "forgejo.org/models/actions"
	"forgejo.org/models/db"
	git_model "forgejo.org/models/git"
	issues_model "forgejo.org/models/issues"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/routers/web/shared/trustweb"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"

	actions_service "forgejo.org/services/actions"
)

// The checks pages (docs/BUILDOUT.md section 8, the Checks board).
//
//	GET  /{owner}/{repo}/asc/checks                       failure groups, the flaky tests
//	GET  /{owner}/{repo}/asc/checks/groups/{id}           one group: where it happened, the history strip
//	POST /{owner}/{repo}/asc/checks/groups/{id}/state     acknowledge, quarantine, resolve, reopen
//	POST /{owner}/{repo}/asc/checks/rerun                 re-run a run of this repository
//	GET  /{owner}/{repo}/pulls/{index}/asc/checks         the checks of one version of a change
//	POST /{owner}/{repo}/pulls/{index}/asc/checks/rerun   re-run the failed runs, or all runs, of that version
//
// Reading needs the right to read the code, as for the rest of the repository; the reads are made with the hub's
// own credential because Forgejo has decided before the handler runs. A change of state is made as the person, and
// asc decides: quarantine needs a maintainer and a policy that allows it. Re-running needs write access to the
// repository's Actions and uses Forgejo's own re-run, so it takes the same checks as the button of the Actions page.
//
// Results belong to the exact version being read. A required check without a result on this version is shown as
// "no result for this version", never as passing. The text of a failure comes from a runner: it is cut, stripped
// of control characters, and escaped by the template.
const (
	tplAscChecks     base.TplName = "repo/asc_checks"
	tplAscCheckGroup base.TplName = "repo/asc_check_group"
	tplAscPullChecks base.TplName = "repo/pulls/asc_checks"

	maxChecksForm = 16 << 10
)

// ascChecksPage is the template data under .AscChecks.
type ascChecksPage struct {
	Data        *asctrust.ChecksPage
	View        string
	Views       []ascChecksView
	State       string
	States      []ascChecksState
	Flaky       bool
	Job         string
	Test        string
	Window      uint32
	Windows     []uint32
	Jobs        []asctrust.JobStat
	FlakyRows   []asctrust.FlakyRow
	Trends      []asctrust.TrendRow
	NextURL     string
	Problem     string
	Unsupported bool
	ChecksURL   string
	CanWrite    bool
}

type ascChecksState struct {
	Key, Label string
	Active     bool
}

// ascChecksView is one tab of the CI failures page.
type ascChecksView struct {
	Key    string
	URL    string
	Active bool
}

// The views of the CI failures page: the failing jobs and failure groups, the flaky tests, the trend of the jobs.
const (
	ascChecksViewFailures = "failures"
	ascChecksViewFlaky    = "flaky"
	ascChecksViewTrends   = "trends"
)

// ascChecksWindows are the windows of a history, in days.
var ascChecksWindows = []uint32{7, 14, 30, 90, 180}

func ascChecksURL(repo *repo_model.Repository) string {
	return setting.AppSubURL + "/" + repo.OwnerName + "/" + repo.Name + "/asc/checks"
}

// ascChecksViewURL is the link of a tab; the window carries over from tab to tab.
func ascChecksViewURL(base, view string, window uint32, windowGiven bool) string {
	q := url.Values{}
	if view != ascChecksViewFailures {
		q.Set("view", view)
	}
	if windowGiven {
		q.Set("window", strconv.FormatUint(uint64(window), 10))
	}
	if len(q) == 0 {
		return base
	}
	return base + "?" + q.Encode()
}

// AscChecks serves GET /{owner}/{repo}/asc/checks.
func AscChecks(ctx *context.Context) {
	repo, name, ok := trustweb.ReaderRepo(ctx)
	if !ok {
		return
	}
	ctx.Data["Title"] = ctx.Locale.TrString("asc_trust.checks.title")
	ctx.Data["PageIsAscChecks"] = true
	p := &ascChecksPage{
		ChecksURL: ascChecksURL(repo), View: ctx.FormString("view"), State: ctx.FormString("state"), Flaky: ctx.FormBool("flaky"),
		Job: ctx.FormString("job"), Test: ctx.FormString("test"), Windows: ascChecksWindows,
	}
	p.CanWrite = ctx.IsSigned && ctx.Repo.CanWrite(unit.TypeCode)
	ctx.Data["AscChecks"] = p

	if p.View == "" {
		p.View = ascChecksViewFailures
	}
	window, wok := asctrust.WindowDays(ctx.FormString("window"))
	_, windowGiven := ctx.Req.Form["window"]
	switch p.View {
	case ascChecksViewFailures, ascChecksViewFlaky, ascChecksViewTrends:
	default:
		wok = false
	}
	for _, v := range []string{ascChecksViewFailures, ascChecksViewFlaky, ascChecksViewTrends} {
		p.Views = append(p.Views, ascChecksView{Key: v, Active: v == p.View, URL: ascChecksViewURL(p.ChecksURL, v, window, windowGiven && wok)})
	}
	if !wok {
		p.Problem = ctx.Locale.TrString("asc_trust.checks.err_window")
		ctx.HTML(http.StatusBadRequest, tplAscChecks)
		return
	}
	p.Window = window
	if !asctrust.HasFeature(ctx, asctrust.FeatureCiInsights) {
		p.Unsupported = true
		ctx.HTML(http.StatusOK, tplAscChecks)
		return
	}
	for _, s := range append([]struct {
		Key   string
		Label string
	}{{"", "open"}}, ascGroupStateOptions()...) {
		p.States = append(p.States, ascChecksState{Key: s.Key, Label: s.Key, Active: s.Key == p.State})
	}
	conn, err := asctrust.Conn()
	if err != nil {
		log.Error("asc checks: no connection to asc serve: %v", err)
		p.Problem = ctx.Locale.TrString("asc_trust.err.unavailable")
		ctx.HTML(http.StatusBadGateway, tplAscChecks)
		return
	}
	callCtx, release, err := trustweb.ReadCall(ctx, repo)
	if err != nil {
		f := trustweb.FailureOf("checks credential", err)
		p.Problem = f.Localized(ctx)
		ctx.HTML(f.Status, tplAscChecks)
		return
	}
	defer release()
	fail := func(op string, err error) {
		f := trustweb.FailureOf(op, err)
		if f.Status == http.StatusNotFound {
			ctx.NotFound("asc checks", nil)
			return
		}
		p.Problem = f.Localized(ctx)
		ctx.HTML(f.Status, tplAscChecks)
	}
	switch p.View {
	case ascChecksViewFlaky:
		page, err := asctrust.LoadFlaky(callCtx, conn, name, window, ctx.FormString("page"))
		if err != nil {
			fail("checks flaky", err)
			return
		}
		p.FlakyRows = page.Rows
		if page.Next != "" {
			p.NextURL = p.ChecksURL + "?" + url.Values{"view": {ascChecksViewFlaky}, "window": {strconv.FormatUint(uint64(window), 10)}, "page": {page.Next}}.Encode()
		}
	case ascChecksViewTrends:
		trends, err := asctrust.LoadTrends(callCtx, conn, name, window)
		if err != nil {
			fail("checks trends", err)
			return
		}
		p.Trends = trends
	default:
		data, err := asctrust.LoadChecks(callCtx, conn, name, asctrust.ChecksQuery{
			State: p.State, FlakyOnly: p.Flaky, Job: p.Job, Test: p.Test, PageToken: ctx.FormString("page"), SkipFlaky: true,
		}, window)
		if err != nil {
			fail("checks load", err)
			return
		}
		p.Data = data
		p.Jobs = asctrust.JobStats(data.Groups.Rows)
		if data.Groups.Next != "" {
			q := url.Values{"page": {data.Groups.Next}}
			if p.State != "" {
				q.Set("state", p.State)
			}
			if p.Flaky {
				q.Set("flaky", "1")
			}
			if p.Job != "" {
				q.Set("job", p.Job)
			}
			if p.Test != "" {
				q.Set("test", p.Test)
			}
			p.NextURL = p.ChecksURL + "?" + q.Encode()
		}
	}
	ctx.HTML(http.StatusOK, tplAscChecks)
}

func ascGroupStateOptions() []struct {
	Key   string
	Label string
} {
	var out []struct {
		Key   string
		Label string
	}
	for _, s := range asctrust.GroupStates {
		out = append(out, struct {
			Key   string
			Label string
		}{s.Key, s.Label})
	}
	return out
}

// ascGroupPage is the template data under .AscCheckGroup.
type ascGroupPage struct {
	Detail      *asctrust.GroupDetail
	History     *asctrust.History
	Occurrences []ascOccurrenceView
	Op          string
	Problem     string
	Unsupported bool
	ChecksURL   string
	GroupURL    string
	NextURL     string
	CanWrite    bool
	CanRerun    bool
	Window      uint32
	IssueQuery  string
}

type ascOccurrenceView struct {
	asctrust.OccurrenceRow
	RunURL string
	RunID  int64
}

// AscCheckGroup serves GET /{owner}/{repo}/asc/checks/groups/{id}.
func AscCheckGroup(ctx *context.Context) {
	repo, name, ok := trustweb.ReaderRepo(ctx)
	if !ok {
		return
	}
	id := ctx.ParamsInt64(":id")
	ctx.Data["Title"] = ctx.Locale.TrString("asc_trust.checks.group_title")
	ctx.Data["PageIsAscChecks"] = true
	p := &ascGroupPage{ChecksURL: ascChecksURL(repo), Op: asctrust.NewOpID()}
	p.GroupURL = p.ChecksURL + "/groups/" + strconv.FormatInt(id, 10)
	p.CanWrite = ctx.IsSigned && ctx.Repo.CanWrite(unit.TypeCode)
	p.CanRerun = ctx.IsSigned && ctx.Repo.CanWrite(unit.TypeActions)
	ctx.Data["AscCheckGroup"] = p
	if id <= 0 {
		ctx.NotFound("asc check group", nil)
		return
	}
	window, wok := asctrust.WindowDays(ctx.FormString("window"))
	if !wok {
		p.Problem = ctx.Locale.TrString("asc_trust.checks.err_window")
		ctx.HTML(http.StatusBadRequest, tplAscCheckGroup)
		return
	}
	p.Window = window
	if !asctrust.HasFeature(ctx, asctrust.FeatureCiInsights) {
		p.Unsupported = true
		ctx.HTML(http.StatusOK, tplAscCheckGroup)
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		p.Problem = ctx.Locale.TrString("asc_trust.err.unavailable")
		ctx.HTML(http.StatusBadGateway, tplAscCheckGroup)
		return
	}
	callCtx, release, err := trustweb.ReadCall(ctx, repo)
	if err != nil {
		f := trustweb.FailureOf("checks credential", err)
		p.Problem = f.Localized(ctx)
		ctx.HTML(f.Status, tplAscCheckGroup)
		return
	}
	defer release()
	detail, hist, err := asctrust.LoadGroup(callCtx, conn, name, uint64(id), ctx.FormString("page"), window)
	if err != nil {
		f := trustweb.FailureOf("checks group", err)
		if f.Status == http.StatusNotFound {
			ctx.NotFound("asc check group", nil)
			return
		}
		p.Problem = f.Localized(ctx)
		ctx.HTML(f.Status, tplAscCheckGroup)
		return
	}
	p.Detail, p.History = detail, hist
	p.IssueQuery = "title=" + url.QueryEscape(ctx.Locale.TrString("asc_trust.checks.issue_title", firstNonEmptyString(detail.Group.TestName, detail.Group.Title)))
	for _, o := range detail.Occurrences {
		view := ascOccurrenceView{OccurrenceRow: o}
		// A run id from asc is only a link when the run belongs to this repository.
		if o.RunID > 0 && o.RunID < 1<<62 {
			if run, err := actions_model.GetRunByID(ctx, int64(o.RunID)); err == nil && run.RepoID == repo.ID {
				view.RunURL = repo.Link() + "/actions/runs/" + strconv.FormatInt(run.Index, 10)
				view.RunID = run.ID
			}
		}
		p.Occurrences = append(p.Occurrences, view)
	}
	if detail.Next != "" {
		p.NextURL = p.GroupURL + "?" + url.Values{"page": {detail.Next}}.Encode()
	}
	ctx.HTML(http.StatusOK, tplAscCheckGroup)
}

func firstNonEmptyString(items ...string) string {
	for _, s := range items {
		if s != "" {
			return s
		}
	}
	return ""
}

// AscCheckGroupState serves POST /{owner}/{repo}/asc/checks/groups/{id}/state with the fields state, note and op.
func AscCheckGroupState(ctx *context.Context) {
	repo, name, ok := trustweb.WriterRepo(ctx)
	if !ok {
		return
	}
	id := ctx.ParamsInt64(":id")
	back := ascChecksURL(repo) + "/groups/" + strconv.FormatInt(id, 10)
	if id <= 0 {
		ctx.NotFound("asc check group", nil)
		return
	}
	if !trustweb.ParseForm(ctx, maxChecksForm) {
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		ctx.Flash.Error(ctx.Locale.TrString("asc_trust.err.unavailable"))
		ctx.Redirect(back)
		return
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoWrite, repo)
	if err != nil {
		ctx.Flash.Error(trustweb.FailureOf("checks credential", err).Localized(ctx))
		ctx.Redirect(back)
		return
	}
	defer release()
	g, err := asctrust.SetGroupState(callCtx, conn, name, uint64(id), ctx.Req.PostFormValue("state"), trustweb.Lines(ctx.Req.PostFormValue("note")), ctx.Req.PostFormValue("op"))
	if err != nil {
		ctx.Flash.Error(trustweb.FailureOf("checks state", err).Localized(ctx))
		ctx.Redirect(back)
		return
	}
	ctx.Flash.Success(ctx.Locale.TrString("asc_trust.checks.state_done." + g.StateKey))
	if next := ctx.Req.PostFormValue("next"); next == "pull" {
		if index, err := strconv.ParseInt(ctx.Req.PostFormValue("index"), 10, 64); err == nil && index > 0 {
			ctx.Redirect(repo.Link() + "/pulls/" + strconv.FormatInt(index, 10) + "/asc/checks")
			return
		}
	}
	ctx.Redirect(back)
}

// ascRerunMessage turns the error of a re-run into a sentence. known is false for an error the person cannot act on.
func ascRerunMessage(ctx *context.Context, err error) (msg string, known bool) {
	switch {
	case errors.Is(err, actions_service.ErrRerunWorkflowInvalid):
		return ctx.Locale.TrString("asc_trust.checks.rerun_invalid"), true
	case errors.Is(err, actions_service.ErrRerunWorkflowStillRunning), errors.Is(err, actions_service.ErrRerunJobStillRunning):
		return ctx.Locale.TrString("asc_trust.checks.rerun_running"), true
	case errors.Is(err, actions_service.ErrRerunWorkflowDisabled):
		return ctx.Locale.TrString("asc_trust.checks.rerun_disabled"), true
	}
	return "", false
}

// ascRerunRun re-runs one run: all of its jobs, or only the jobs that failed (and, as Forgejo does, the jobs that
// need them). It returns how many jobs were started.
func ascRerunRun(ctx *context.Context, run *actions_model.ActionRun, failedOnly bool) (int, error) {
	if !failedOnly {
		jobs, err := actions_service.RerunAllJobs(ctx, run)
		return len(jobs), err
	}
	all, err := actions_model.GetRunJobsByRunID(ctx, run.ID)
	if err != nil {
		return 0, err
	}
	started := 0
	for _, job := range all {
		if !job.Status.IsFailure() {
			continue
		}
		jobs, err := actions_service.RerunJob(ctx, job)
		if errors.Is(err, actions_service.ErrRerunJobStillRunning) {
			continue // it was started again as a job that needs another one
		}
		if err != nil {
			return started, err
		}
		started += len(jobs)
	}
	return started, nil
}

// AscChecksRerun serves POST /{owner}/{repo}/asc/checks/rerun with the fields run (the id of an Actions run of this
// repository) and mode (all or failed).
func AscChecksRerun(ctx *context.Context) {
	repo, _, ok := trustweb.WriterRepo(ctx)
	if !ok {
		return
	}
	if !ctx.Repo.CanWrite(unit.TypeActions) {
		ctx.Error(http.StatusForbidden)
		return
	}
	if !trustweb.ParseForm(ctx, maxChecksForm) {
		return
	}
	back := ascChecksURL(repo)
	runID, err := strconv.ParseInt(ctx.Req.PostFormValue("run"), 10, 64)
	if err != nil || runID <= 0 {
		ctx.NotFound("asc rerun", nil)
		return
	}
	run, err := actions_model.GetRunByID(ctx, runID)
	if err != nil || run.RepoID != repo.ID {
		ctx.NotFound("asc rerun", nil) // a run of another repository looks like a run that does not exist
		return
	}
	failedOnly := ctx.Req.PostFormValue("mode") == "failed"
	n, err := ascRerunRun(ctx, run, failedOnly)
	if msg, known := ascRerunMessage(ctx, err); known {
		ctx.Flash.Error(msg)
		ctx.Redirect(back)
		return
	} else if err != nil {
		log.Error("asc checks: re-run of run %d: %v", run.ID, err)
		ctx.ServerError("AscChecksRerun", err)
		return
	}
	if n == 0 {
		ctx.Flash.Info(ctx.Locale.TrString("asc_trust.checks.rerun_nothing"))
		ctx.Redirect(back)
		return
	}
	ctx.Redirect(repo.Link() + "/actions/runs/" + strconv.FormatInt(run.Index, 10))
}

// ------------------------------------------------------------------------------------------------ one version

// ascPullChecksPage is the template data under .AscPullChecks.
type ascPullChecksPage struct {
	Checks      *asctrust.PullChecks
	Problem     string
	CausesNote  string
	PullURL     string
	ChecksURL   string
	RerunURL    string
	NewIssueURL string
	CanRerun    bool
	CanWrite    bool
	Ops         map[uint64]string
	Index       int64
}

// AscPullChecks serves GET /{owner}/{repo}/pulls/{index}/asc/checks.
func AscPullChecks(ctx *context.Context) {
	repo, name, ok := trustweb.ReaderRepo(ctx)
	if !ok {
		return
	}
	issue, ok := getPullInfo(ctx)
	if !ok {
		return
	}
	pull := issue.PullRequest
	ctx.Data["PageIsPullList"] = true
	ctx.Data["Title"] = "Checks"
	p := &ascPullChecksPage{
		PullURL: issue.Link(), ChecksURL: ascChecksURL(repo), Ops: map[uint64]string{}, Index: issue.Index,
		CanRerun: ctx.IsSigned && ctx.Repo.CanWrite(unit.TypeActions),
		CanWrite: ctx.IsSigned && ctx.Repo.CanWrite(unit.TypeCode),
	}
	p.RerunURL = p.PullURL + "/asc/checks/rerun"
	p.NewIssueURL = repo.Link() + "/issues/new"
	ctx.Data["AscPullChecks"] = p

	sha, headOK := ascPullHead(ctx, pull)
	if !headOK {
		p.Problem = ctx.Locale.TrString("asc_trust.checks.err_head_page")
		ctx.HTML(http.StatusOK, tplAscPullChecks)
		return
	}
	in := asctrust.PullChecksIn{Head: sha, Now: time.Now()}

	// The version of the change that the head is.
	if versions, err := pull_service.AscVersions(ctx, pull); err == nil {
		in.Version = versions.ByGitCommit(sha)
	}

	statuses, _, err := git_model.GetLatestCommitStatus(ctx, repo.ID, sha, db.ListOptionsAll)
	if err != nil {
		ctx.ServerError("GetLatestCommitStatus", err)
		return
	}
	for _, s := range statuses {
		in.Statuses = append(in.Statuses, asctrust.StatusIn{Context: s.Context, State: string(s.State), Description: s.Description, URL: s.TargetURL})
	}

	runs, err := db.Find[actions_model.ActionRun](ctx, actions_model.FindRunOptions{RepoID: repo.ID, CommitSHA: sha})
	if err != nil {
		ctx.ServerError("FindRuns", err)
		return
	}
	runIDs := map[uint64]bool{}
	var earliest time.Time
	for _, r := range runs {
		in.Runs = append(in.Runs, ascRunIn(repo, r))
		runIDs[uint64(r.ID)] = true
		if c := r.Created.AsTime(); earliest.IsZero() || c.Before(earliest) {
			earliest = c
		}
	}

	// The base branch, for "compared with main".
	if ctx.Repo.GitRepo != nil {
		if baseSHA, err := ctx.Repo.GitRepo.GetBranchCommitID(pull.BaseBranch); err == nil {
			if base, _, err := git_model.GetLatestCommitStatus(ctx, repo.ID, baseSHA, db.ListOptionsAll); err == nil {
				in.MainStatuses = []asctrust.StatusIn{}
				for _, s := range base {
					in.MainStatuses = append(in.MainStatuses, asctrust.StatusIn{Context: s.Context, State: string(s.State)})
				}
			}
		}
	}

	// What the policy requires, and the failure groups seen in these runs: advice, so a failure of either leaves
	// the page without it and says so.
	if asctrust.HasFeature(ctx, asctrust.FeaturePolicy) || asctrust.HasFeature(ctx, asctrust.FeatureCiInsights) {
		ascPullAdvice(ctx, repo, name, &in, p, runIDs, earliest)
	}
	p.Checks = asctrust.BuildPullChecks(in)
	p.Checks.Localize(ctx.Locale.TrString)
	for _, card := range p.Checks.Flaky {
		p.Ops[card.GroupID] = asctrust.NewOpID()
	}
	ctx.HTML(http.StatusOK, tplAscPullChecks)
}

// ascPullAdvice adds what the policy requires and the failure groups seen in the runs to in. The reads are made as
// the signed-in person. A failure of either leaves the page without it and says so.
func ascPullAdvice(ctx *context.Context, repo *repo_model.Repository, name string, in *asctrust.PullChecksIn, p *ascPullChecksPage, runIDs map[uint64]bool, earliest time.Time) {
	conn, err := asctrust.Conn()
	if err != nil {
		return
	}
	callCtx, release, err := trustweb.ReadCall(ctx, repo)
	if err != nil {
		log.Warn("asc checks: credential for %s: %v", repo.FullName(), err)
		p.CausesNote = ctx.Locale.TrString("asc_trust.checks.err_causes")
		return
	}
	defer release()
	if required, err := asctrust.RequiredChecks(callCtx, conn, name); err == nil {
		in.Required = required
	} else {
		log.Warn("asc checks: required checks of %s: %v", repo.FullName(), err)
	}
	if asctrust.HasFeature(ctx, asctrust.FeatureCiInsights) {
		causes, err := asctrust.PullCauses(callCtx, conn, name, runIDs, earliest)
		if err != nil {
			log.Warn("asc checks: failure groups of %s: %v", repo.FullName(), err)
			p.CausesNote = ctx.Locale.TrString("asc_trust.checks.err_causes")
			return
		}
		in.Causes, in.CausesKnown = causes, true
	}
}

func ascRunIn(repo *repo_model.Repository, r *actions_model.ActionRun) asctrust.RunIn {
	in := asctrust.RunIn{
		ID: r.ID, Title: r.Title, URL: repo.Link() + "/actions/runs/" + strconv.FormatInt(r.Index, 10),
		Created: r.Created.AsTime(),
	}
	if r.Started > 0 {
		in.Started = r.Started.AsTime()
	}
	if r.Stopped > 0 {
		in.Stopped = r.Stopped.AsTime()
	}
	switch {
	case r.Status.IsSuccess():
		in.State = "success"
	case r.Status.IsFailure():
		in.State = "failure"
	case r.Status.IsRunning():
		in.State = "running"
	case r.Status.IsWaiting():
		in.State = "waiting"
	case r.Status.IsBlocked():
		in.State = "blocked"
	case r.Status.IsCancelled():
		in.State = "cancelled"
	case r.Status.IsSkipped():
		in.State = "skipped"
	}
	return in
}

// ascPullHead returns the commit the head of a pull request is at.
func ascPullHead(ctx *context.Context, pull *issues_model.PullRequest) (string, bool) {
	if ctx.Repo.GitRepo != nil {
		if sha, err := ctx.Repo.GitRepo.GetRefCommitID(pull.GetGitRefName()); err == nil && sha != "" {
			return sha, true
		}
	}
	if pull.HeadCommitID != "" {
		return pull.HeadCommitID, true
	}
	return "", false
}

// AscPullChecksRerun serves POST /{owner}/{repo}/pulls/{index}/asc/checks/rerun with the field mode (all or failed).
// The runs are the ones of the head commit of the pull request, found here: the request names no run.
func AscPullChecksRerun(ctx *context.Context) {
	repo, _, ok := trustweb.WriterRepo(ctx)
	if !ok {
		return
	}
	if !ctx.Repo.CanWrite(unit.TypeActions) {
		ctx.Error(http.StatusForbidden)
		return
	}
	issue, ok := getPullInfo(ctx)
	if !ok {
		return
	}
	if !trustweb.ParseForm(ctx, maxChecksForm) {
		return
	}
	back := issue.Link() + "/asc/checks"
	sha, headOK := ascPullHead(ctx, issue.PullRequest)
	if !headOK {
		ctx.Flash.Error(ctx.Locale.TrString("asc_trust.checks.err_head"))
		ctx.Redirect(back)
		return
	}
	runs, err := db.Find[actions_model.ActionRun](ctx, actions_model.FindRunOptions{RepoID: repo.ID, CommitSHA: sha})
	if err != nil {
		ctx.ServerError("FindRuns", err)
		return
	}
	failedOnly := ctx.Req.PostFormValue("mode") == "failed"
	started := 0
	var firstErr error
	for _, run := range runs {
		if failedOnly && !run.Status.IsFailure() {
			continue
		}
		n, err := ascRerunRun(ctx, run, failedOnly)
		started += n
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		if msg, known := ascRerunMessage(ctx, firstErr); known {
			ctx.Flash.Error(msg)
		} else {
			log.Error("asc checks: re-run for pull request %d: %v", issue.PullRequest.ID, firstErr)
			ctx.Flash.Error(ctx.Locale.TrString("asc_trust.checks.rerun_some_failed"))
		}
	}
	if started > 0 {
		ctx.Flash.Success(ctx.Locale.TrString("asc_trust.checks.rerun_started", started))
	} else if firstErr == nil {
		ctx.Flash.Info(ctx.Locale.TrString("asc_trust.checks.rerun_nothing"))
	}
	ctx.Redirect(back)
}
