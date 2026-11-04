// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"net/http"
	"regexp"
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runHealth(t *testing.T, path, id string, h func(*context.Context)) (int, string, http.Header) {
	t.Helper()
	ctx, rec := contexttest.MockContext(t, "GET "+path)
	ctx.Render = templates.HTMLRenderer()
	contexttest.LoadUser(t, ctx, 1)
	ctx.IsSigned = true
	if id != "" {
		ctx.SetParams("id", id)
	}
	h(ctx)
	return rec.Code, rec.Body.String(), rec.Header()
}

func TestAscHealthSteps(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Actions.Enabled, true)()

	ids := []string{}
	for _, c := range healthChecks() {
		ids = append(ids, c.ID)
	}
	assert.Equal(t, []string{"database", "collation", "cache", "runners", "notices"}, ids)

	t.Run("the page draws every step that applies, ready to run again", func(t *testing.T) {
		code, body, _ := runHealth(t, "/admin/self_check", "", SelfCheck)
		require.Equal(t, http.StatusOK, code)
		assert.Contains(t, body, `data-health-steps="/admin/self_check/checks/"`)
		assert.Contains(t, body, `data-health-run`)
		assert.Contains(t, body, `data-tally="asc_admin.check_tally"`)
		got := regexp.MustCompile(`data-check="([a-z]+)" data-state="([a-z]+)"`).FindAllStringSubmatch(body, -1)
		steps := map[string]string{}
		for _, m := range got {
			steps[m[1]] = m[2]
		}
		assert.Equal(t, "ok", steps["database"])
		assert.NotContains(t, steps, "collation", "SQLite has no collation to check")
		assert.Contains(t, steps, "runners")
		assert.Contains(t, steps, "notices")
		assert.Regexp(t, `data-health-tally>asc_admin.check_tally(_norun)?<`, body)
	})

	t.Run("one step runs alone", func(t *testing.T) {
		code, body, h := runHealth(t, "/admin/self_check/checks/database", "database", SelfCheckStep)
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "no-store", h.Get("Cache-Control"))
		assert.Regexp(t, `^\s*<li class="ds-li ds-li-wrap adm-check" data-check="database" data-state="ok">`, body)
		assert.Contains(t, body, "asc_admin.check_passed")
		assert.NotContains(t, body, "<html", "the step is the row only")
	})

	t.Run("a step that does not apply answers no content", func(t *testing.T) {
		code, body, _ := runHealth(t, "/admin/self_check/checks/collation", "collation", SelfCheckStep)
		assert.Equal(t, http.StatusNoContent, code)
		assert.Empty(t, body)
	})

	t.Run("a step of a switched off feature is not found", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Actions.Enabled, false)()
		code, _, _ := runHealth(t, "/admin/self_check/checks/runners", "runners", SelfCheckStep)
		assert.Equal(t, http.StatusNotFound, code)
		code, _, _ = runHealth(t, "/admin/self_check/checks/x", "<x>", SelfCheckStep)
		assert.Equal(t, http.StatusNotFound, code)
	})
}

func TestAscHealthNoRun(t *testing.T) {
	ctx, _ := contexttest.MockContext(t, "GET /admin/self_check")
	r, ok := runHealthCheck(ctx, healthCheck{"probe", func(*context.Context) (checkRow, bool) {
		return checkRow{State: checkNoRun, Name: "Probe", Text: "the count failed"}, true
	}})
	require.True(t, ok)
	assert.Equal(t, "probe", r.ID)
	assert.Equal(t, checkNoRun, r.State)
	assert.EqualValues(t, "asc_admin.check_norun", r.Label)
	r, _ = runHealthCheck(ctx, healthCheck{"odd", func(*context.Context) (checkRow, bool) { return checkRow{State: "weird"}, true }})
	assert.Equal(t, checkErr, r.State, "an unknown state is a failure")
	assert.Equal(t, "asc_admin.check_tally_norun", checkTally(ctx, 1, 2, 3, 4))
	assert.Equal(t, "asc_admin.check_tally", checkTally(ctx, 1, 2, 3, 0))
}
