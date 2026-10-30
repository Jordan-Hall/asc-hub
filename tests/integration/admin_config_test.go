// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"net/http"
	"testing"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	"forgejo.org/tests"

	"github.com/stretchr/testify/assert"
)

func TestAdminConfig(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	session := loginUser(t, "user1")
	req := NewRequest(t, "GET", "/admin/config")
	resp := session.MakeRequest(t, req, http.StatusOK)
	assert.True(t, test.IsNormalPageCompleted(resp.Body.String()))
}

// The curated page and its switches: registration closes when the switch is saved, and the saved value is what the
// sign-up page reads.
func TestAdminConfigRuntimeSwitches(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Service.DisableRegistration, false)()
	defer test.MockVariableValue(&setting.Service.ShowRegistrationButton, true)()
	defer test.MockVariableValue(&setting.Service.EnableOpenIDSignUp, false)()
	setting.ApplyRuntimeService(t.Context())

	session := loginUser(t, "user1")
	body := session.MakeRequest(t, NewRequest(t, "GET", "/admin/config"), http.StatusOK).Body.String()
	for _, want := range []string{`data-config-dyn-key="service.allow_registration"`, `data-config-dyn-key="service.require_email_confirm"`, "server.domain", "storage.byo", "mail.from", `id="adm-all"`} {
		assert.Contains(t, body, want)
	}

	anon := emptyTestSession(t)
	const closed = "Registration is disabled"
	assert.NotContains(t, anon.MakeRequest(t, NewRequest(t, "GET", "/user/sign_up"), http.StatusOK).Body.String(), closed)

	save := func(key, value string, status int) {
		req := NewRequestWithValues(t, "POST", "/admin/config", map[string]string{"key": key, "value": value})
		session.MakeRequest(t, req, status)
	}
	save("service.allow_registration", "false", http.StatusOK)
	assert.True(t, setting.Service.DisableRegistration)
	assert.Contains(t, anon.MakeRequest(t, NewRequest(t, "GET", "/user/sign_up"), http.StatusOK).Body.String(), closed)

	save("service.allow_registration", "true", http.StatusOK)
	assert.False(t, setting.Service.DisableRegistration)
	assert.NotContains(t, anon.MakeRequest(t, NewRequest(t, "GET", "/user/sign_up"), http.StatusOK).Body.String(), closed)

	// with a mailer the confirmation can be switched on; without one it is refused, because it could never be sent
	defer test.MockVariableValue(&setting.Service.RegisterEmailConfirm, false)()
	save("service.require_email_confirm", "true", http.StatusOK)
	assert.True(t, setting.Service.RegisterEmailConfirm)
	save("service.require_email_confirm", "false", http.StatusOK)
	assert.False(t, setting.Service.RegisterEmailConfirm)
	defer test.MockVariableValue(&setting.MailService, nil)()
	save("service.require_email_confirm", "true", http.StatusBadRequest)
	assert.False(t, setting.Service.RegisterEmailConfirm)
}
