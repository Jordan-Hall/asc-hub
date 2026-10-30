// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every template is a definition the hub can show: a name that is a file stem, a trigger, and steps with ids and a known kind.
func TestAscWorkflowTemplatesAreShaped(t *testing.T) {
	require.NotEmpty(t, AscWorkflowTemplates())
	seen := map[string]bool{}
	for _, tpl := range AscWorkflowTemplates() {
		assert.Regexp(t, `^[a-z0-9-]+$`, tpl.Name)
		assert.False(t, seen[tpl.Name], tpl.Name)
		seen[tpl.Name] = true
		assert.NotEmpty(t, tpl.Title)
		assert.Contains(t, tpl.Text, "[[trigger]]", tpl.Name)
		assert.Contains(t, tpl.Text, "[[step]]", tpl.Name)
		assert.NotContains(t, tpl.Text, "\x00")
		got, ok := ascWorkflowTemplate(tpl.Name)
		require.True(t, ok)
		assert.Equal(t, tpl.Text, got.Text)
	}
	_, ok := ascWorkflowTemplate("../etc/passwd")
	assert.False(t, ok, "only the names of the list")
}
