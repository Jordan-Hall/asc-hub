// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"sort"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/models/unit"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/log"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	pull_service "forgejo.org/services/pull"
)

// The detail of a row of the versions list: the commit message, the short hash, the lines added and removed, a
// restack mark, the checks of that version and the approvals given on it. The viewer has been checked for read
// access to the pull request, so the calls use the hub's own credential, as the signatures do. Every part is advice:
// a failure leaves the row without it and never fails the page.

// AscVersionCheck is one check chip of a version.
type AscVersionCheck struct {
	Name string
	Tone string // ok, err, warn
}

const (
	// ascVersionCheckRuns is how many runs of the change one look reads, and ascVersionCheckChips how many chips one
	// version shows.
	ascVersionCheckRuns  = 100
	ascVersionCheckChips = 4
	// ascVersionHistoryRows is how many versions the history of the checks page lists.
	ascVersionHistoryRows = 8
)

// ascVersionChecksOf turns the runs of a change (newest first) into the chips of each version: the newest run of
// each job name of that version.
func ascVersionChecksOf(runs []*ascv1.CiRun) map[uint32][]AscVersionCheck {
	out := map[uint32][]AscVersionCheck{}
	seen := map[uint32]map[string]bool{}
	for _, run := range runs {
		v := run.GetVersion()
		if v == 0 {
			continue
		}
		if seen[v] == nil {
			seen[v] = map[string]bool{}
		}
		jobs := run.GetJobs()
		if len(jobs) == 0 {
			jobs = []*ascv1.CiJob{{Name: run.GetName(), Status: run.GetStatus()}}
		}
		for _, j := range jobs {
			name := asctrust.CleanText(j.GetName(), 40)
			if name == "" || seen[v][name] || len(out[v]) >= ascVersionCheckChips {
				continue
			}
			tone := ""
			switch j.GetStatus() {
			case ascv1.CiStatus_CI_STATUS_SUCCESS:
				tone = "ok"
			case ascv1.CiStatus_CI_STATUS_FAILURE:
				tone = "err"
			case ascv1.CiStatus_CI_STATUS_QUEUED, ascv1.CiStatus_CI_STATUS_RUNNING:
				tone = "warn"
			default:
				continue // cancelled and skipped say nothing about the version
			}
			seen[v][name] = true
			out[v] = append(out[v], AscVersionCheck{Name: name, Tone: tone})
		}
	}
	return out
}

// AscVersionHistoryRow is one line of the version history of the checks page: what the newest run of each job said
// on that version.
type AscVersionHistoryRow struct {
	Version uint32
	Failing int
	Passing int
	Running int
}

// ascVersionHistoryOf turns the runs of a change (newest first) into one row per version, the newest version first: the
// newest run of each job name of the version is counted, as the chips do. Cancelled and skipped jobs say nothing.
func ascVersionHistoryOf(runs []*ascv1.CiRun) []AscVersionHistoryRow {
	rows := map[uint32]*AscVersionHistoryRow{}
	seen := map[uint32]map[string]bool{}
	for _, run := range runs {
		v := run.GetVersion()
		if v == 0 {
			continue
		}
		if seen[v] == nil {
			seen[v] = map[string]bool{}
			rows[v] = &AscVersionHistoryRow{Version: v}
		}
		jobs := run.GetJobs()
		if len(jobs) == 0 {
			jobs = []*ascv1.CiJob{{Name: run.GetName(), Status: run.GetStatus()}}
		}
		for _, j := range jobs {
			name := j.GetName()
			if seen[v][name] {
				continue
			}
			switch j.GetStatus() {
			case ascv1.CiStatus_CI_STATUS_SUCCESS:
				rows[v].Passing++
			case ascv1.CiStatus_CI_STATUS_FAILURE:
				rows[v].Failing++
			case ascv1.CiStatus_CI_STATUS_QUEUED, ascv1.CiStatus_CI_STATUS_RUNNING:
				rows[v].Running++
			default:
				continue
			}
			seen[v][name] = true
		}
	}
	out := make([]AscVersionHistoryRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	if len(out) > ascVersionHistoryRows {
		out = out[:ascVersionHistoryRows]
	}
	return out
}

// ascVersionChecksAllowed says whether the check chips of the versions may be fetched and shown: a signed-in viewer who
// may read the Actions of the repository.
func ascVersionChecksAllowed(signedIn, canReadActions bool) bool { return signedIn && canReadActions }

// prepareAscVersionDetails puts AscVersionDetails (version number to pull_service.AscVersionDetail) and
// AscVersionChecks (version number to chips) into ctx.Data.
func prepareAscVersionDetails(ctx *context.Context, pull *issues_model.PullRequest, versions *pull_service.AscVersionList) {
	if versions == nil || len(versions.Versions) == 0 || ctx.Repo == nil || ctx.Repo.GitRepo == nil {
		return
	}
	ctx.Data["AscVersionDetails"] = pull_service.AscVersionDetails(ctx, ctx.Repo.GitRepo, versions)

	// The check chips are Actions data: whoever may not read the Actions of the repository sees none, and asc is not
	// asked.
	if !ascVersionChecksAllowed(ctx.IsSigned, ctx.Repo.CanRead(unit.TypeActions)) {
		return
	}
	runs := ascChangeRuns(ctx, pull)
	if runs == nil {
		return
	}
	ctx.Data["AscVersionChecks"] = ascVersionChecksOf(runs)
}

// ascChangeRuns reads the newest runs of the change of a stacked pull request, as the viewer (asc decides again who may
// see them). It returns nil when the pull request is not stacked or asc cannot answer. The caller has checked that the
// viewer may read the Actions of the repository.
func ascChangeRuns(ctx *context.Context, pull *issues_model.PullRequest) []*ascv1.CiRun {
	link, err := issues_model.GetPullRequestAscStack(ctx, pull.ID)
	if err != nil || link == nil {
		return nil
	}
	change := ascChangeBytes(link.AscChange)
	name, nerr := asctrust.RepoName(ctx.Repo.Repository)
	if change == nil || nerr != nil {
		return nil
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return nil
	}
	userCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallRepoRead, ctx.Repo.Repository)
	if err != nil {
		return nil
	}
	defer release()
	cctx, cancel := gocontext.WithTimeout(userCtx, asctrust.CallTimeout)
	defer cancel()
	resp, err := ascv1.NewCiClient(conn).ListRuns(cctx, &ascv1.ListRunsRequest{Repo: &ascv1.RepoRef{Name: name}, Change: change, Limit: ascVersionCheckRuns})
	if err != nil {
		log.Trace("asc runs of pull request %d: %v", pull.ID, asctrust.Classify(err))
		return nil
	}
	return resp.GetRuns()
}
