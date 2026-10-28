// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLicenseFileName(t *testing.T) {
	assert.Equal(t, "LICENSE", licenseFileName([]string{"README.md", "LICENSE"}))
	assert.Equal(t, "License.md", licenseFileName([]string{"Cargo.toml", "License.md"}))
	assert.Equal(t, "COPYING", licenseFileName([]string{"COPYING"}))
	assert.Empty(t, licenseFileName([]string{"README.md", "licenses-list.txt"}))
	assert.Empty(t, licenseFileName(nil))
}
