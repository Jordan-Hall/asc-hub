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

// The probe runs once in the time to live, whatever the number of pages; a failure shows as degraded and is repaired by the next probe.
func TestFooterStatusIsKeptForAWhile(t *testing.T) {
	calls, answer := 0, "degraded"
	t.Cleanup(test.MockVariableValue(&statusProbe, func(context.Context) string { calls++; return answer }))
	statusMu.Lock()
	statusAt = time.Time{}
	statusMu.Unlock()

	assert.Equal(t, "degraded", FooterStatus())
	answer = "ok"
	assert.Equal(t, "degraded", FooterStatus(), "the answer is kept")
	assert.Equal(t, 1, calls)

	statusMu.Lock()
	statusAt = time.Now().Add(-time.Minute)
	statusMu.Unlock()
	assert.Equal(t, "ok", FooterStatus())
	assert.Equal(t, 2, calls)
}
