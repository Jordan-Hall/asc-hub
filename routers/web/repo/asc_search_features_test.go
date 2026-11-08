// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
)

// TestAscSearchMeaningOff checks that a repository with meaning search switched off (board RepoSettingsGeneral)
// runs a semantic request as a text search.
func TestAscSearchMeaningOff(t *testing.T) {
	ctx, _ := contexttest.MockContext(t, "/user2/repo1/asc/search?mode=semantic&q=retry")
	q := ascSearchRawQuery(ctx)
	assert.Equal(t, ascSearchModeSemantic, q.mode)
	assert.False(t, q.noMeaning)

	ctx.Data["AscMeaningOff"] = true
	q = ascSearchRawQuery(ctx)
	assert.Equal(t, ascSearchModeLexical, q.mode)
	assert.True(t, q.noMeaning)

	sym, _ := contexttest.MockContext(t, "/user2/repo1/asc/search?mode=symbol&q=retry")
	sym.Data["AscMeaningOff"] = true
	assert.Equal(t, ascSearchModeSymbol, ascSearchRawQuery(sym).mode)
}
