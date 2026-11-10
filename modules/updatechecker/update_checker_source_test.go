// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package updatechecker

import (
	"testing"

	"forgejo.org/modules/system"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// storedError runs the checker with the two endpoints and returns the error it stored for the dashboard.
func storedError(t *testing.T, httpEndpoint, domainEndpoint string) string {
	t.Helper()
	var got string
	state := system.NewMockStateStore(t)
	state.On("Set", mock.Anything, mock.MatchedBy(func(item system.StateItem) bool {
		cs, ok := item.(*CheckerState)
		require.True(t, ok)
		assert.Nil(t, cs.SupportedVersions)
		got = cs.Error
		return true
	})).Return(nil)
	defer test.MockVariableValue[system.StateStore](&system.AppState, state)()
	require.NoError(t, GiteaUpdateChecker(httpEndpoint, domainEndpoint))
	return got
}

func TestUpdateCheckerSource(t *testing.T) {
	t.Run("no source is an error the dashboard shows", func(t *testing.T) {
		assert.Equal(t, ErrNoSource.Error(), storedError(t, "", ""))
	})

	t.Run("an http address is refused", func(t *testing.T) {
		assert.Contains(t, storedError(t, "http://releases.example.org/version.json", ""), "must be an https address")
	})

	t.Run("an address with credentials is refused", func(t *testing.T) {
		assert.Contains(t, storedError(t, "https://u:p@releases.example.org/version.json", ""), "must be an https address")
	})

	t.Run("a DNS failure is stored, not dropped", func(t *testing.T) {
		defer test.MockVariableValue(&lookupTXT, func(string) ([]string, error) {
			return []string{"forgejo_versions=16.0.3"}, nil
		})()
		assert.Contains(t, storedError(t, "", "releases.example.org"), "no TXT record with a valid value")
	})
}
