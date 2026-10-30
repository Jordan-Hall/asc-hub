// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"testing"

	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
)

// The script of the page asks for JSON and gets the answer in place; a form without the script asks for a page.
func TestAscWantsJSON(t *testing.T) {
	ctx, _ := contexttest.MockContext(t, "POST /user2/repo1/settings/asc/security/scan")
	assert.False(t, ascWantsJSON(ctx))
	ctx.Req.Header.Set("Accept", "text/html,application/xhtml+xml")
	assert.False(t, ascWantsJSON(ctx))
	ctx.Req.Header.Set("Accept", "application/json")
	assert.True(t, ascWantsJSON(ctx))
}
