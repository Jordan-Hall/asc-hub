// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"context"
	"sync"
	"testing"

	"forgejo.org/modules/setting/config"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
)

// fakeDyn is a store of saved values; every change of it is a new revision.
type fakeDyn struct {
	mu   sync.Mutex
	vals map[string]string
	rev  int
}

func (f *fakeDyn) GetValue(_ context.Context, key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.vals[key]
	return v, ok
}

func (f *fakeDyn) GetRevision(context.Context) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rev
}

func (f *fakeDyn) InvalidateCache() {}

func (f *fakeDyn) set(key, val string) {
	f.mu.Lock()
	f.vals[key] = val
	f.rev++
	f.mu.Unlock()
}

func TestApplyRuntimeService(t *testing.T) {
	ctx := t.Context()
	dyn := &fakeDyn{vals: map[string]string{}, rev: 1}
	old := config.GetDynGetter()
	config.SetDynGetter(dyn)
	t.Cleanup(func() { config.SetDynGetter(old) })

	// the file says: registration open, button shown, no confirmation
	t.Cleanup(test.MockVariableValue(&Service.DisableRegistration, false))
	t.Cleanup(test.MockVariableValue(&Service.RegisterEmailConfirm, false))
	t.Cleanup(test.MockVariableValue(&Service.ShowRegistrationButton, true))
	t.Cleanup(test.MockVariableValue(&Service.EnableOpenIDSignUp, true))
	t.Cleanup(test.MockVariableValue(&Service.EnableOpenIDSignIn, true))
	t.Cleanup(test.MockVariableValue(&Service.AllowOnlyExternalRegistration, false))
	t.Cleanup(test.MockVariableValue(&MailService, &Mailer{}))
	runtimeServiceFile.once = sync.Once{}
	t.Cleanup(func() { runtimeServiceFile.once = sync.Once{} })

	ApplyRuntimeService(ctx)
	assert.False(t, Service.DisableRegistration, "no saved value: the file stands")
	assert.False(t, Service.RegisterEmailConfirm)
	assert.True(t, Service.ShowRegistrationButton)

	dyn.set("service.allow_registration", "false")
	ApplyRuntimeService(ctx)
	assert.True(t, Service.DisableRegistration, "a saved switch wins over the file")
	assert.False(t, Service.ShowRegistrationButton, "the sign-up button goes with the switch")
	assert.False(t, Service.EnableOpenIDSignUp)

	dyn.set("service.allow_registration", "true")
	ApplyRuntimeService(ctx)
	assert.False(t, Service.DisableRegistration)
	assert.True(t, Service.ShowRegistrationButton, "the button comes back as the file had it")
	assert.True(t, Service.EnableOpenIDSignUp)

	dyn.set("service.require_email_confirm", "true")
	ApplyRuntimeService(ctx)
	assert.True(t, Service.RegisterEmailConfirm)

	// without a mailer a confirmation could never be sent, so the switch has no effect
	MailService = nil
	ApplyRuntimeService(ctx)
	assert.False(t, Service.RegisterEmailConfirm)
}

func TestApplyRuntimeServiceFileClosedRegistrationOpenedByTheSwitch(t *testing.T) {
	ctx := t.Context()
	dyn := &fakeDyn{vals: map[string]string{}, rev: 100}
	old := config.GetDynGetter()
	config.SetDynGetter(dyn)
	t.Cleanup(func() { config.SetDynGetter(old) })

	// the file says: registration closed (so the button was never shown)
	t.Cleanup(test.MockVariableValue(&Service.DisableRegistration, true))
	t.Cleanup(test.MockVariableValue(&Service.RegisterEmailConfirm, false))
	t.Cleanup(test.MockVariableValue(&Service.ShowRegistrationButton, false))
	t.Cleanup(test.MockVariableValue(&Service.EnableOpenIDSignUp, false))
	t.Cleanup(test.MockVariableValue(&Service.EnableOpenIDSignIn, true))
	t.Cleanup(test.MockVariableValue(&Service.AllowOnlyExternalRegistration, false))
	runtimeServiceFile.once = sync.Once{}
	t.Cleanup(func() { runtimeServiceFile.once = sync.Once{} })

	ApplyRuntimeService(ctx)
	assert.True(t, Service.DisableRegistration)
	assert.False(t, Service.ShowRegistrationButton)

	dyn.set("service.allow_registration", "true")
	ApplyRuntimeService(ctx)
	assert.False(t, Service.DisableRegistration)
	assert.True(t, Service.ShowRegistrationButton, "opening registration shows the button the file never showed")
	assert.True(t, Service.EnableOpenIDSignUp)
}
