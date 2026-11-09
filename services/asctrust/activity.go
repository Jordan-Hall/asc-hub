// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"fmt"
	"math"
	"strconv"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"google.golang.org/grpc"
)

// FeatureUsageActivity is what Version.features says when the server answers Usage.GetActivity: the changes and agent
// runs of the whole server in a window, for the site administrator.
const FeatureUsageActivity = "usage-activity.v1"

// Activity is what asc serve counted on the whole server today (UTC).
type Activity struct {
	Changes         uint32 // changes first published
	ChangesByAgents uint32 // of those, published by an agent or held by an agent run
	Runs            uint32 // agent runs created
	RunsActive      uint32 // queued, running or waiting for input
	RunsSucceeded   uint32
	RunsFailed      uint32 // failed, over budget or expired
	RunsCancelled   uint32
	Truncated       bool // asc stopped counting changes at its bound: the change counts are lower bounds
}

// RunsEnded is how many of the runs have ended, however they ended.
func (a Activity) RunsEnded() uint32 { return a.RunsSucceeded + a.RunsFailed + a.RunsCancelled }

// AgentShare is the share of the changes published by agents as a percentage text (31, 96.4), or empty when no
// change was published.
func (a Activity) AgentShare() string { return percent(a.ChangesByAgents, a.Changes) }

// FinishedShare is the share of the ended runs that succeeded as a percentage text, or empty when no run has ended.
func (a Activity) FinishedShare() string { return percent(a.RunsSucceeded, a.RunsEnded()) }

// percent is part of whole in percent with at most one decimal, empty when whole is 0.
func percent(part, whole uint32) string {
	if whole == 0 {
		return ""
	}
	p := math.Round(float64(part)*1000/float64(whole)) / 10
	return strconv.FormatFloat(p, 'f', -1, 64)
}

// LoadActivity asks asc serve for the activity of the whole server since the start of the UTC day. The call must be
// made as the site administrator. An answer whose counts do not add up is ErrBadAnswer.
func LoadActivity(ctx context.Context, conn grpc.ClientConnInterface) (Activity, error) {
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewUsageClient(conn).GetActivity(cctx, &ascv1.GetActivityRequest{})
	if err != nil {
		return Activity{}, err
	}
	a := Activity{
		Changes: resp.GetChanges(), ChangesByAgents: resp.GetChangesByAgents(),
		Runs: resp.GetRuns(), RunsActive: resp.GetRunsActive(), RunsSucceeded: resp.GetRunsSucceeded(),
		RunsFailed: resp.GetRunsFailed(), RunsCancelled: resp.GetRunsCancelled(), Truncated: resp.GetTruncated(),
	}
	sum := uint64(a.RunsActive) + uint64(a.RunsSucceeded) + uint64(a.RunsFailed) + uint64(a.RunsCancelled)
	if a.ChangesByAgents > a.Changes || sum != uint64(a.Runs) || resp.GetSince() > resp.GetUntil() {
		return Activity{}, fmt.Errorf("%w: activity", ErrBadAnswer)
	}
	return a, nil
}
