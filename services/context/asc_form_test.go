// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package context

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A url-encoded form is read on the first call: a list field such as ids[] is not lost because the body is not multipart.
func TestFormStringsReadsAURLEncodedBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/admin/notices/delete", strings.NewReader("ids%5B%5D=3&ids%5B%5D=5&dismiss=true"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	b := &Base{Req: req}
	assert.Equal(t, []string{"3", "5"}, b.FormStrings("ids[]"))
	assert.Nil(t, b.FormStrings("missing"))
}
