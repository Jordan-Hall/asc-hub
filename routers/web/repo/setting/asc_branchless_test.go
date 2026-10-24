// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"context"
	"net/http"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/services/ascsync"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
)

func TestAscBranchlessParse(t *testing.T) {
	assert.Equal(t, []string{"master", "release/*"}, parseAscSharedRefs("master\r\n\n  release/*  \n"))
	assert.Empty(t, parseAscSharedRefs(" \n"))
}

type countWriter struct{ n int }

func (w *countWriter) SetRepoSettings(context.Context, string, ascsync.BranchlessSettings) error {
	w.n++
	return nil
}

func TestAscBranchlessPostRefusals(t *testing.T) {
	unittest.PrepareTestEnv(t)
	w := &countWriter{}
	defer ascsync.UseSettingsWriter(w)()

	// [asc] disabled: the page does not exist.
	defer test.MockVariableValue(&setting.Asc.Enabled, false)()
	ctx, _ := contexttest.MockContext(t, "user2/repo1/settings")
	contexttest.LoadUser(t, ctx, 2)
	contexttest.LoadRepo(t, ctx, 1)
	AscBranchlessPost(ctx)
	assert.Equal(t, http.StatusNotFound, ctx.Resp.Status())

	// Enabled, but the repository is stored in git: still nothing.
	setting.Asc.Enabled = true
	ctx, _ = contexttest.MockContext(t, "user2/repo1/settings")
	contexttest.LoadUser(t, ctx, 2)
	contexttest.LoadRepo(t, ctx, 1)
	AscBranchlessPost(ctx)
	assert.Equal(t, http.StatusNotFound, ctx.Resp.Status())
	assert.Zero(t, w.n)
}
