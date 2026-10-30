// Copyright 2018 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"net/http"
	"net/url"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/cache"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/web"
	"forgejo.org/services/contexttest"
	"forgejo.org/services/forms"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChangePassword(t *testing.T) {
	oldPassword := "password"
	setting.MinPasswordLength = 6
	pcALL := []string{"lower", "upper", "digit", "spec"}
	pcLUN := []string{"lower", "upper", "digit"}
	pcLU := []string{"lower", "upper"}

	for _, req := range []struct {
		OldPassword        string
		NewPassword        string
		Retype             string
		Message            string
		PasswordComplexity []string
	}{
		{
			OldPassword:        oldPassword,
			NewPassword:        "Qwerty123456-",
			Retype:             "Qwerty123456-",
			Message:            "",
			PasswordComplexity: pcALL,
		},
		{
			OldPassword:        oldPassword,
			NewPassword:        "12345",
			Retype:             "12345",
			Message:            "auth.password_too_short",
			PasswordComplexity: pcALL,
		},
		{
			OldPassword:        "12334",
			NewPassword:        "123456",
			Retype:             "123456",
			Message:            "settings.password_incorrect",
			PasswordComplexity: pcALL,
		},
		{
			OldPassword:        oldPassword,
			NewPassword:        "123456",
			Retype:             "12345",
			Message:            "form.password_not_match",
			PasswordComplexity: pcALL,
		},
		{
			OldPassword:        oldPassword,
			NewPassword:        "Qwerty",
			Retype:             "Qwerty",
			Message:            "form.password_complexity",
			PasswordComplexity: pcALL,
		},
		{
			OldPassword:        oldPassword,
			NewPassword:        "Qwerty",
			Retype:             "Qwerty",
			Message:            "form.password_complexity",
			PasswordComplexity: pcLUN,
		},
		{
			OldPassword:        oldPassword,
			NewPassword:        "QWERTY",
			Retype:             "QWERTY",
			Message:            "form.password_complexity",
			PasswordComplexity: pcLU,
		},
	} {
		t.Run(req.OldPassword+"__"+req.NewPassword, func(t *testing.T) {
			unittest.PrepareTestEnv(t)
			setting.PasswordComplexity = req.PasswordComplexity
			ctx, _ := contexttest.MockContext(t, "user/settings/security")
			contexttest.LoadUser(t, ctx, 2)
			contexttest.LoadRepo(t, ctx, 1)

			web.SetForm(ctx, &forms.ChangePasswordForm{
				OldPassword: req.OldPassword,
				Password:    req.NewPassword,
				Retype:      req.Retype,
			})
			AccountPost(ctx)

			assert.Contains(t, ctx.Flash.ErrorMsg, req.Message)
			assert.Equal(t, http.StatusSeeOther, ctx.Resp.Status())
		})
	}
}

func TestDeleteAccountAsksForTheUsernameFirst(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx, resp := contexttest.MockContext(t, "POST user/settings/account/delete")
	ctx.Render = templates.HTMLRenderer()
	require.NoError(t, cache.Init())
	ctx.Cache = cache.GetCache()
	contexttest.LoadUser(t, ctx, 2)
	ctx.Req.Form = url.Values{"username_confirm": {"somebody-else"}, "password": {"password"}}
	ctx.Req.PostForm = ctx.Req.Form

	DeleteAccount(ctx)

	assert.Equal(t, "settings.delete_username_mismatch", ctx.Flash.ErrorMsg, "a wrong name stops before the password is looked at")
	assert.Equal(t, true, ctx.Data["Err_UsernameConfirm"])
	assert.Equal(t, http.StatusOK, resp.Code)
	unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
}
