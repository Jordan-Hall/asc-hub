// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	"forgejo.org/services/ascagentui"

	"github.com/stretchr/testify/assert"
)

// services/context cannot import ascagentui (a cycle), so it spells the key of the agent features switch itself.
func TestAgentFeaturesKeyIsTheOneTheContextReads(t *testing.T) {
	assert.Equal(t, "asc.agent_features", ascagentui.FeaturesKey)
}
