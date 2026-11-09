// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTheActivitySharesArePercentagesWithOneDecimal(t *testing.T) {
	assert.Equal(t, "30", Activity{Changes: 10, ChangesByAgents: 3}.AgentShare())
	assert.Equal(t, "33.3", Activity{Changes: 3, ChangesByAgents: 1}.AgentShare())
	assert.Equal(t, "", Activity{}.AgentShare(), "no change, no share")
	a := Activity{Runs: 1000, RunsActive: 10, RunsSucceeded: 954, RunsFailed: 30, RunsCancelled: 6}
	assert.EqualValues(t, 990, a.RunsEnded())
	assert.Equal(t, "96.4", a.FinishedShare(), "of the runs that ended")
	assert.Equal(t, "", Activity{Runs: 2, RunsActive: 2}.FinishedShare())
	assert.Equal(t, "100", Activity{Runs: 1, RunsSucceeded: 1}.FinishedShare())
}
