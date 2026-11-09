// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"strings"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"google.golang.org/grpc"
)

// FeatureAgentRunners is what Version.features says when the server answers Maintenance.ListAgentRunners: the server
// processes that execute agent runs, for the site administrator's runners page. asc says it only with agents on.
const FeatureAgentRunners = "agent-runners.v1"

// AgentRunnerRun is one run an agent runner is executing.
type AgentRunnerRun struct {
	ID      uint64
	Repo    string // owner/name
	Title   string
	Agent   string
	Started time.Time
}

// AgentRunner is one server process that executes agent runs.
type AgentRunner struct {
	Name     string
	Slots    uint32
	VCPU     uint32
	Version  string
	Started  time.Time
	LastSeen time.Time
	Online   bool
	Runs     []AgentRunnerRun
}

// Busy is whether the runner is executing a run.
func (r AgentRunner) Busy() bool { return len(r.Runs) > 0 }

// LoadAgentRunners lists the agent runners, online first. The call must be made as the site administrator. A runner
// without a name, or a run without an id or whose repository is not owner/name, is a bad answer.
func LoadAgentRunners(ctx context.Context, conn grpc.ClientConnInterface) ([]AgentRunner, error) {
	resp, err := ascv1.NewMaintenanceClient(conn).ListAgentRunners(ctx, &ascv1.ListAgentRunnersRequest{})
	if err != nil {
		return nil, err
	}
	out := make([]AgentRunner, 0, len(resp.GetRunners()))
	for _, r := range resp.GetRunners() {
		if r.GetName() == "" {
			return nil, ErrBadAnswer
		}
		runner := AgentRunner{
			Name: r.GetName(), Slots: r.GetSlots(), VCPU: r.GetVcpu(), Version: r.GetVersion(),
			Started: time.Unix(r.GetStarted(), 0), LastSeen: time.Unix(r.GetLastSeen(), 0), Online: r.GetOnline(),
		}
		for _, run := range r.GetRuns() {
			owner, name, ok := strings.Cut(run.GetRepo(), "/")
			if run.GetId() == 0 || !ok || owner == "" || name == "" || strings.Contains(name, "/") {
				return nil, ErrBadAnswer
			}
			runner.Runs = append(runner.Runs, AgentRunnerRun{
				ID: run.GetId(), Repo: run.GetRepo(), Title: run.GetTitle(), Agent: run.GetAgent(), Started: time.Unix(run.GetStarted(), 0),
			})
		}
		out = append(out, runner)
	}
	return out, nil
}
