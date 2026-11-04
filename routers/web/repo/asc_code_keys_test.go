// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"strings"
	"testing"

	"forgejo.org/models/unittest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodeKeysGoToLine(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	keys := func(set func(d map[string]any)) string {
		return renderPart(t, "repo/code_keys", func(d map[string]any) {
			d["BranchNameSubURL"] = "branch/master"
			set(d)
		})
	}

	file := keys(func(d map[string]any) {
		d["IsViewFile"] = true
		d["IsRepresentableAsText"] = true
	})
	assert.Contains(t, file, `data-asc-key="l"`)
	assert.Contains(t, file, `<dialog class="rp-goto" data-goto-line`)
	assert.Contains(t, file, `id="rp-goto-n"`)
	assert.Contains(t, file, `data-asc-key="t"`)

	assert.Contains(t, keys(func(d map[string]any) { d["IsBlame"] = true }), `data-asc-key="l"`, "blame has lines too")

	image := keys(func(d map[string]any) { d["IsViewFile"] = true })
	assert.NotContains(t, image, `data-asc-key="l"`, "a file shown as an image has no lines")
	assert.NotContains(t, image, "data-goto-line")
}

func TestCodeLoadingSkeleton(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	body := renderPart(t, "repo/code_loading", func(map[string]any) {})
	assert.Contains(t, body, `data-loading-skel`)
	assert.Contains(t, body, "asc_rp.code_loading")
	assert.Equal(t, 12, strings.Count(body, "rp-codeload-line"), "a page of lines")
}
