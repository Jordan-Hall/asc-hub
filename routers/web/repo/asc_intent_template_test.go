// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"strings"
	"testing"

	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
)

func TestIntentTemplateHasTheFourHeadings(t *testing.T) {
	ctx, _ := contexttest.MockContext(t, "GET /user2/repo1/compare/main...feature")
	got := ascIntentTemplate(ctx)
	headings := 0
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(l, "## ") {
			headings++
			assert.NotContains(t, l, "asc_rp.", "every heading is translated")
		}
	}
	assert.Equal(t, 4, headings)
}
