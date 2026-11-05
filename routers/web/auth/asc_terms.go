// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/base"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
	"forgejo.org/services/forms"
)

// The box "I agree to the Terms and the Privacy policy" of the sign up form (board Onboarding). With ASC on, an
// account is only made when the box is ticked, and the time it was ticked is kept (asc_terms_acceptance). The links
// go to the operator's policies page when [asc] POLICIES_URL is set, else to the sections of the About page.

// ascTermsData puts the box and its links on a page that shows the sign up form.
func ascTermsData(ctx *context.Context) {
	if !setting.Asc.Enabled {
		return
	}
	terms, privacy := setting.AppSubURL+"/-/about#terms", setting.AppSubURL+"/-/about#privacy"
	if setting.Asc.PoliciesURL != "" {
		terms, privacy = setting.Asc.PoliciesURL, setting.Asc.PoliciesURL
	}
	ctx.Data["AscTerms"] = true
	ctx.Data["AscTermsURL"] = terms
	ctx.Data["AscPrivacyURL"] = privacy
}

// ascTermsRefused answers the form again with an error when ASC is on and the box was not ticked.
func ascTermsRefused(ctx *context.Context, tpl base.TplName, form *forms.RegisterForm) bool {
	if !setting.Asc.Enabled || form.Agree {
		return false
	}
	ctx.Data["Err_Agree"] = true
	ctx.RenderWithErr(ctx.Tr("asc_auth.signup_agree_required"), tpl, form)
	return true
}

// ascTermsRecord keeps when the new account agreed. The account exists by then, so a failure is logged and does not
// undo the sign up.
func ascTermsRecord(ctx *context.Context, u *user_model.User) {
	if !setting.Asc.Enabled {
		return
	}
	if err := user_model.RecordAscTermsAcceptance(ctx, u.ID); err != nil {
		log.Error("asc terms: record the agreement of user %d: %v", u.ID, err)
	}
}
