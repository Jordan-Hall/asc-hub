// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/context"
)

// ascWorkflowHidePrivate applies the rule of ascAgentsMaySeeRun to the agent steps of a workflow run: the log, the failure
// text and the agent's words of a step are shown only when the viewer may see the agent run behind it (its creator, or an
// administrator). It fails closed: a step whose agent run cannot be read, or that is past the number of runs looked at, is
// private. The failure text of the whole run is dropped when any agent step is private, because it repeats the step's.
func ascWorkflowHidePrivate(ctx *context.Context, callCtx gocontext.Context, env *ascAgentsEnv, api ascv1.AgentsClient, row *ascWorkflowRunRow) {
	checked := 0
	anyPrivate := false
	for i := range row.Steps {
		s := &row.Steps[i]
		if s.AgentRunID == 0 {
			continue
		}
		see := false
		if checked < ascWorkflowsMaxLogRuns {
			checked++
			if got, err := api.GetRun(callCtx, &ascv1.GetAgentRunRequest{Repo: env.ref(), Id: s.AgentRunID}); err == nil {
				if run := got.GetRun(); run != nil && run.GetRepo() == env.name && run.GetId() == s.AgentRunID {
					see = ascAgentsMaySeeRun(ctx, run.GetCreatedById())
				}
			}
		}
		if !see {
			s.Private = true
			s.Error = ""
			s.Log = nil
			s.LogHidden = 0
			anyPrivate = true
		}
	}
	if anyPrivate && row.Error != "" {
		row.Error = ""
		row.ErrorPrivate = true
	}
}
