// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAscSystemStatus(t *testing.T) {
	assert.Equal(t, "ok", AscSystemStatus(), "without a provider the hub is well")
	SetAscStatus(func() string { return "degraded" })
	assert.Equal(t, "degraded", AscSystemStatus())
	SetAscStatus(func() string { return "anything else" })
	assert.Equal(t, "ok", AscSystemStatus())
	SetAscStatus(func() string { return "ok" })
}
