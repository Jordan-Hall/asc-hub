// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"net/http"
	"net/url"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/templates"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimeFormatIsSavedAndDrawnOnEveryPage(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	save := func(form url.Values) string {
		ctx, resp := contexttest.MockContext(t, "POST user/settings/appearance/time_format")
		contexttest.LoadUser(t, ctx, 2)
		ctx.IsSigned = true
		ctx.Req.Form, ctx.Req.PostForm = form, form
		UpdateUserTimeFormat(ctx)
		require.Equal(t, http.StatusSeeOther, resp.Code)
		return ctx.Flash.ErrorMsg
	}
	page := func(userID int64) string {
		ctx, resp := contexttest.MockContext(t, "user/settings/appearance")
		ctx.Render = templates.HTMLRenderer()
		contexttest.LoadUser(t, ctx, userID)
		ctx.IsSigned = true
		Appearance(ctx)
		require.Equal(t, http.StatusOK, resp.Code)
		return resp.Body.String()
	}

	body := page(2)
	assert.NotContains(t, body, "data-clock=", "the default is not written")
	assert.NotContains(t, body, "data-date-order=")
	assert.Contains(t, body, `name="clock" value="24" checked`)
	assert.Contains(t, body, `name="date_order" value="dmy" checked`)

	assert.Empty(t, save(url.Values{"clock": {"12"}, "date_order": {"ymd"}}))
	body = page(2)
	assert.Contains(t, body, `data-clock="12"`)
	assert.Contains(t, body, `data-date-order="ymd"`)
	assert.Contains(t, body, `name="clock" value="12" checked`)
	assert.Contains(t, body, `name="date_order" value="ymd" checked`)
	assert.NotContains(t, page(1), "data-clock=", "another person's page keeps the default")

	assert.Equal(t, "settings.asc_time_format_bad", save(url.Values{"clock": {"25"}, "date_order": {"dmy"}}))
	clock, order := user_model.AscTimeFormat(t.Context(), 2)
	assert.Equal(t, [2]string{"12", "ymd"}, [2]string{clock, order}, "a refused form changes nothing")
}
