// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package webhook

import (
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
)

// An ASC hub checks the receiver's certificate even when SKIP_TLS_VERIFY is set.
func TestSkipTLSVerify(t *testing.T) {
	defer test.MockVariableValue(&setting.Webhook.SkipTLSVerify, true)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	assert.False(t, skipTLSVerify())
	setting.Asc.Enabled = false
	assert.True(t, skipTLSVerify())
	setting.Webhook.SkipTLSVerify = false
	assert.False(t, skipTLSVerify())
}
