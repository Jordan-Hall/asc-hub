// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"testing"
	"time"

	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
)

func resetStatus(at time.Time) {
	statusMu.Lock()
	statusReport.CheckedAt = at
	statusMu.Unlock()
}

// The probe runs once in the time to live, whatever the number of pages; a failure shows as degraded and is repaired by the next probe.
func TestFooterStatusIsKeptForAWhile(t *testing.T) {
	calls, answer := 0, StatusReport{}
	t.Cleanup(test.MockVariableValue(&statusProbe, func(context.Context) StatusReport { calls++; return answer }))
	resetStatus(time.Time{})

	assert.Equal(t, "degraded", FooterStatus())
	answer = StatusReport{Database: true}
	assert.Equal(t, "degraded", FooterStatus(), "the answer is kept")
	assert.Equal(t, 1, calls)

	resetStatus(time.Now().Add(-time.Minute))
	assert.Equal(t, "ok", FooterStatus())
	assert.Equal(t, 2, calls)
	assert.False(t, SystemStatus().CheckedAt.IsZero(), "a report always says when it was made")
}

// asc serve counts only when [asc] is enabled.
func TestStatusReportValue(t *testing.T) {
	for _, c := range []struct {
		r    StatusReport
		want string
	}{
		{StatusReport{Database: true}, "ok"},
		{StatusReport{Database: true, AscEnabled: true}, "degraded"},
		{StatusReport{Database: true, AscEnabled: true, Asc: true}, "ok"},
		{StatusReport{AscEnabled: true, Asc: true}, "degraded"},
		{StatusReport{}, "degraded"},
	} {
		assert.Equal(t, c.want, c.r.Value(), "%+v", c.r)
	}
}
