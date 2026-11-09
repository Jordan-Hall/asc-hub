// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package actions

import (
	gocontext "context"
	"time"

	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
)

// agentRunnersTimeout is how long the runners page waits for asc serve's list of agent runners.
var agentRunnersTimeout = 3 * time.Second

// loadAgentRunners puts asc serve's agent runners on the site administrator's runners page: AgentRunnersShown when
// asc is on and runs agents, AgentRunners the list, AgentRunnersUnread when asc did not answer. The Forgejo Actions
// runners above it are the hub's; these are the processes of asc serve that execute agent runs.
func loadAgentRunners(ctx *context.Context) {
	if !setting.Asc.Enabled || ctx.Doer == nil || !ctx.Doer.IsAdmin || !asctrust.HasFeature(ctx, asctrust.FeatureAgentRunners) {
		return
	}
	ctx.Data["AgentRunnersShown"] = true
	runners, err := agentRunners(ctx)
	if err != nil {
		log.Warn("agent runners: %v", err)
		ctx.Data["AgentRunnersUnread"] = true
		return
	}
	busy := 0
	for _, r := range runners {
		busy += len(r.Runs)
	}
	ctx.Data["AgentRunners"] = runners
	ctx.Data["AgentRunsLive"] = busy
}

func agentRunners(ctx *context.Context) ([]asctrust.AgentRunner, error) {
	conn, err := asctrust.Conn()
	if err != nil {
		return nil, err
	}
	callCtx, release, err := asctrust.UserCall(ctx, ctx.Doer, asctrust.CallInstance, nil)
	if err != nil {
		return nil, err
	}
	defer release()
	callCtx, cancel := gocontext.WithTimeout(callCtx, agentRunnersTimeout)
	defer cancel()
	return asctrust.LoadAgentRunners(callCtx, conn)
}
