// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascmaintenance

import (
	"strings"
	"testing"

	"forgejo.org/models/unittest"

	_ "forgejo.org/modules/testimport"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) { unittest.MainTest(m) }

func TestValidate(t *testing.T) {
	require.NoError(t, State{}.Validate())
	require.NoError(t, State{On: true, Message: "Upgrading", Window: "22:00 to 23:00 UTC", Until: "2026-10-29T23:00:00Z", StatusURL: "https://status.example.org"}.Validate())
	for name, s := range map[string]State{
		"long message": {Message: strings.Repeat("a", 301)},
		"line break":   {Message: "a\nb"},
		"bad date":     {Until: "tomorrow"},
		"http status":  {StatusURL: "http://status.example.org"},
		"script":       {StatusURL: "javascript:alert(1)"},
		"login in url": {StatusURL: "https://a:b@status.example.org"},
	} {
		assert.Error(t, s.Validate(), name)
	}
}

func TestSaveAndLoad(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	Forget()
	assert.False(t, Load(t.Context()).On)
	require.NoError(t, Save(t.Context(), State{On: true, Message: "Upgrading to v17", Until: "2026-10-29T23:00:00Z"}))
	got := Load(t.Context())
	assert.True(t, got.On)
	assert.Equal(t, "Upgrading to v17", got.Message)
	_, ok := got.UntilTime()
	assert.True(t, ok)
	// a bad state is never stored
	require.Error(t, Save(t.Context(), State{On: true, StatusURL: "http://x"}))
	assert.True(t, Load(t.Context()).On)
	require.NoError(t, Save(t.Context(), State{}))
	assert.False(t, Load(t.Context()).On)
}

func TestUntilText(t *testing.T) {
	assert.Equal(t, "2026-10-29 23:00 UTC", State{Until: "2026-10-29T23:00:00Z"}.UntilText())
	assert.Equal(t, "2026-10-29 22:00 UTC", State{Until: "2026-10-30T00:00:00+02:00"}.UntilText())
	assert.Empty(t, State{}.UntilText())
}
