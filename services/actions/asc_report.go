// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	actions_model "forgejo.org/models/actions"
	issues_model "forgejo.org/models/issues"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/graceful"
	"forgejo.org/modules/log"
	"forgejo.org/modules/queue"
	"forgejo.org/modules/setting"
	webhook_module "forgejo.org/modules/webhook"
	"forgejo.org/services/ascsearch"
	"forgejo.org/services/asctrust"
)

// Every Actions run of an asc repository is reported to asc's Ci service (SPEC §12.9), so `asc ci status`, the
// change header and the Checks board read one record whichever forge ran the job. A run of a stacked pull request is
// recorded against its change; a run of the default branch against no change. Other runs (a push to a topic branch, a
// schedule, a manual dispatch) have no place there and are not sent.
const (
	// ascReportSource names the hub as the reporter of a run (`ReportRunRequest.source`).
	ascReportSource  = "asc-hub-actions"
	ascReportTimeout = 20 * time.Second
	// ascReportMaxJobs is the Ci service's limit on jobs in one run.
	ascReportMaxJobs = 200
	ascReportMaxText = 255
)

var ascReportQueue *queue.WorkerPoolQueue[int64]

// initAscReport starts the queue that reports runs to asc. A run whose jobs change many times while it waits is
// reported once, with what it looks like then.
func initAscReport() {
	ascReportQueue = queue.CreateUniqueQueue(graceful.GetManager().ShutdownContext(), "actions_asc_report", ascReportHandler)
	if ascReportQueue == nil {
		log.Fatal("Unable to create actions_asc_report queue")
	}
	go graceful.GetManager().RunWithCancel(ascReportQueue)
}

// reportToAsc queues the runs of jobs whose state changed.
func reportToAsc(jobs []*actions_model.ActionRunJob) {
	if ascReportQueue == nil || !setting.Asc.Enabled {
		return
	}
	seen := map[int64]bool{}
	for _, j := range jobs {
		if j == nil || seen[j.RunID] {
			continue
		}
		seen[j.RunID] = true
		if err := ascReportQueue.Push(j.RunID); err != nil && !errors.Is(err, queue.ErrAlreadyInQueue) {
			log.Warn("asc report of run %d: %v", j.RunID, err)
		}
	}
}

func ascReportHandler(ids ...int64) []int64 {
	ctx := graceful.GetManager().ShutdownContext()
	for _, id := range ids {
		if err := reportRun(ctx, id); err != nil {
			// The next change of the run reports it again; a run is never left wrong for long.
			log.Warn("asc report of run %d: %v", id, err)
		}
	}
	return nil
}

// reportSendFn sends one report; a seam for tests.
var reportSendFn = func(ctx context.Context, req *ascv1.ReportRunRequest) error {
	conn, err := asctrust.Conn()
	if err != nil {
		return err
	}
	cctx, cancel := asctrust.SystemContext(ctx, ascReportTimeout)
	defer cancel()
	_, err = ascv1.NewCiClient(conn).ReportRun(cctx, req)
	return err
}

func reportRun(ctx context.Context, id int64) error {
	run, err := actions_model.GetRunByID(ctx, id)
	if err != nil {
		return err
	}
	if err := run.LoadRepo(ctx); err != nil {
		return err
	}
	if sb, err := gitrepo.StorageBackendOf(run.Repo); err != nil || sb != gitrepo.StorageBackendAsc {
		return err
	}
	change, ok, err := ascChangeOfRun(ctx, run)
	if err != nil || !ok {
		return err
	}
	name, err := asctrust.RepoName(run.Repo)
	if err != nil {
		return err
	}
	jobs, err := actions_model.GetRunJobsByRunID(ctx, run.ID)
	if err != nil {
		return err
	}
	return reportSendFn(ctx, ascReportOf(run, name, change, jobs))
}

// ascChangeOfRun says what a run is recorded against: the change of a stacked pull request, or no change for the
// default branch. ok is false for a run that is neither.
func ascChangeOfRun(ctx context.Context, run *actions_model.ActionRun) ([]byte, bool, error) {
	if run.PullRequestID > 0 {
		link, err := issues_model.GetPullRequestAscStack(ctx, run.PullRequestID)
		if err != nil || link == nil {
			return nil, false, err
		}
		change, err := ascsearch.ChangeBytes(link.AscChange)
		if err != nil {
			return nil, false, nil
		}
		return change, true, nil
	}
	if run.Event == webhook_module.HookEventPush && run.Ref == "refs/heads/"+run.Repo.DefaultBranch {
		return nil, true, nil
	}
	return nil, false, nil
}

func ascReportOf(run *actions_model.ActionRun, repo string, change []byte, jobs []*actions_model.ActionRunJob) *ascv1.ReportRunRequest {
	req := &ascv1.ReportRunRequest{
		Repo:       asctrust.RepoRef(repo),
		Source:     ascReportSource,
		ExternalId: strconv.FormatInt(run.ID, 10),
		Name:       reportText(run.Title, run.WorkflowID),
		Change:     change,
		Status:     ascStatusOf(run.Status),
		Started:    int64(run.Started),
		Finished:   int64(run.Stopped),
		Url:        httpsLink(run.HTMLURL()),
	}
	names := map[string]int{}
	for i, j := range jobs {
		if len(req.Jobs) == ascReportMaxJobs {
			break
		}
		name := reportText(j.Name, "job")
		// Jobs are matched by name within a run, so two jobs with one name (a matrix that names none of its
		// values) are told apart by their place in the run.
		base := name
		for names[name] > 0 {
			names[base]++
			suffix := fmt.Sprintf(" (%d)", names[base])
			name = asctrust.CleanText(base, ascReportMaxText-len(suffix)) + suffix
		}
		names[name]++
		req.Jobs = append(req.Jobs, &ascv1.ReportJob{
			Name:     name,
			Status:   ascStatusOf(j.Status),
			Started:  int64(j.Started),
			Finished: int64(j.Stopped),
			Url:      httpsLink(fmt.Sprintf("%s/jobs/%d", run.HTMLURL(), i)),
		})
	}
	return req
}

func ascStatusOf(s actions_model.Status) ascv1.CiStatus {
	switch s {
	case actions_model.StatusSuccess:
		return ascv1.CiStatus_CI_STATUS_SUCCESS
	case actions_model.StatusFailure:
		return ascv1.CiStatus_CI_STATUS_FAILURE
	case actions_model.StatusCancelled:
		return ascv1.CiStatus_CI_STATUS_CANCELLED
	case actions_model.StatusSkipped:
		return ascv1.CiStatus_CI_STATUS_SKIPPED
	case actions_model.StatusRunning:
		return ascv1.CiStatus_CI_STATUS_RUNNING
	default:
		return ascv1.CiStatus_CI_STATUS_QUEUED
	}
}

// reportText is s as the Ci service takes text: no control characters, at most 255 bytes, never empty.
func reportText(s, fallback string) string {
	s = strings.TrimSpace(asctrust.CleanText(s, ascReportMaxText))
	if s == "" {
		return fallback
	}
	return s
}

// httpsLink is link when it is https, else empty: the Ci service keeps https links only.
func httpsLink(link string) string {
	if !strings.HasPrefix(link, "https://") || strings.ContainsAny(link, " \t\r\n") {
		return ""
	}
	return link
}
