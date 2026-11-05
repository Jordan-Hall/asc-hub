// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAscSignUpTerms checks the box "I agree to the Terms and the Privacy policy" of the sign up form (board
// Onboarding): with ASC on it is shown with links to the About page (or the operator's policies page), an account is
// only made when it is ticked, and the agreement is kept. With ASC off the form is as before.
func TestAscSignUpTerms(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Service.EnableCaptcha, false)()
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()

	signUp := func(t *testing.T, name string, agree bool, status int) *HTMLDoc {
		t.Helper()
		values := map[string]string{
			"user_name": name,
			"email":     name + "@example.com",
			"password":  "examplePassword!1",
		}
		if agree {
			values["agree"] = "on"
		}
		resp := MakeRequest(t, NewRequestWithValues(t, "POST", "/user/sign_up", values), status)
		return NewHTMLParser(t, resp.Body)
	}

	t.Run("Box", func(t *testing.T) {
		q := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user/sign_up"), http.StatusOK).Body)
		box := q.Find("form.ds-form input#agree")
		assert.Equal(t, 1, box.Length())
		_, required := box.Attr("required")
		assert.True(t, required)
		_, checked := box.Attr("checked")
		assert.False(t, checked, "the box starts unticked")
		label := q.Find(`label[for="agree"]`)
		assert.Equal(t, "I agree to the Terms and the Privacy policy", label.Text())
		assert.Equal(t, "/-/about#terms", label.Find("a").Eq(0).AttrOr("href", ""))
		assert.Equal(t, "/-/about#privacy", label.Find("a").Eq(1).AttrOr("href", ""))
	})

	t.Run("PoliciesURL", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.PoliciesURL, "https://example.org/policies")()
		q := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user/sign_up"), http.StatusOK).Body)
		links := q.Find(`label[for="agree"] a`)
		assert.Equal(t, "https://example.org/policies", links.Eq(0).AttrOr("href", ""))
		assert.Equal(t, "https://example.org/policies", links.Eq(1).AttrOr("href", ""))
	})

	t.Run("Refused", func(t *testing.T) {
		q := signUp(t, "termsRefused", false, http.StatusOK)
		assert.Contains(t, q.Find(".ds-auth-flash").Text(), "Tick the box to agree to the Terms and the Privacy policy")
		assert.Equal(t, "true", q.Find("input#agree").AttrOr("aria-invalid", ""))
		assert.Equal(t, "termsRefused", q.Find("input#user_name").AttrOr("value", ""), "the form keeps what was typed")
		unittest.AssertNotExistsBean(t, &user_model.User{Name: "termsRefused"})
	})

	t.Run("Agreed", func(t *testing.T) {
		signUp(t, "termsAgreed", true, http.StatusSeeOther)
		u := unittest.AssertExistsAndLoadBean(t, &user_model.User{Name: "termsAgreed"})
		row, err := user_model.GetAscTermsAcceptance(t.Context(), u.ID)
		require.NoError(t, err)
		require.NotNil(t, row)
		assert.Positive(t, row.AcceptedUnix)
	})

	t.Run("AscOff", func(t *testing.T) {
		defer test.MockVariableValue(&setting.Asc.Enabled, false)()
		q := NewHTMLParser(t, MakeRequest(t, NewRequest(t, "GET", "/user/sign_up"), http.StatusOK).Body)
		assert.Equal(t, 0, q.Find("input#agree").Length())
		signUp(t, "termsOff", false, http.StatusSeeOther)
		u := unittest.AssertExistsAndLoadBean(t, &user_model.User{Name: "termsOff"})
		row, _ := user_model.GetAscTermsAcceptance(t.Context(), u.ID)
		assert.Nil(t, row, "nothing is kept when the box is not shown")
	})
}
