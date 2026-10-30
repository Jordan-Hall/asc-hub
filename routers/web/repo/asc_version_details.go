// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	issues_model "forgejo.org/models/issues"
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

// prepareAscVersionDetails puts AscVersionDetails (version number to pull_service.AscVersionDetail) and
// AscVersionChecks (version number to chips) into ctx.Data.
func prepareAscVersionDetails(ctx *context.Context, pull *issues_model.PullRequest, versions *pull_service.AscVersionList) {
	if versions == nil || len(versions.Versions) == 0 || ctx.Repo == nil || ctx.Repo.GitRepo == nil {
		return
	}
	ctx.Data["AscVersionDetails"] = pull_service.AscVersionDetails(ctx, ctx.Repo.GitRepo, versions)

	link, err := issues_model.GetPullRequestAscStack(ctx, pull.ID)
	if err != nil || link == nil {
		return
	}
	change := ascChangeBytes(link.AscChange)
	name, nerr := asctrust.RepoName(ctx.Repo.Repository)
	if change == nil || nerr != nil {
		return
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return
	}
	cctx, cancel := asctrust.SystemContext(ctx.Req.Context(), asctrust.CallTimeout)
	defer cancel()
	resp, err := ascv1.NewCiClient(conn).ListRuns(cctx, &ascv1.ListRunsRequest{Repo: &ascv1.RepoRef{Name: name}, Change: change, Limit: ascVersionCheckRuns})
	if err != nil {
		log.Trace("asc version checks of pull request %d: %v", pull.ID, asctrust.Classify(err))
		return
	}
	ctx.Data["AscVersionChecks"] = ascVersionChecksOf(resp.GetRuns())
}
