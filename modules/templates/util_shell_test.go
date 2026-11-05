// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShellArg(t *testing.T) {
	assert.Equal(t, "v2.18.0", ShellArg("v2.18.0"))
	assert.Equal(t, "nova_linux-x86_64.tar.gz", ShellArg("nova_linux-x86_64.tar.gz"))
	assert.Equal(t, "''", ShellArg(""))
	assert.Equal(t, "'-rf'", ShellArg("-rf"), "not read as a flag")
	assert.Equal(t, "'a;rm -rf ~'", ShellArg("a;rm -rf ~"))
	assert.Equal(t, `'$(id)'`, ShellArg("$(id)"))
	assert.Equal(t, `'it'\''s'`, ShellArg("it's"))
	assert.Equal(t, "'a\nb'", ShellArg("a\nb"), "a newline stays inside the quotes")
	assert.Equal(t, "'`x`'", ShellArg("`x`"))
}
