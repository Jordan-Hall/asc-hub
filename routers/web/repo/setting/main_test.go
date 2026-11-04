// Copyright 2017 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"testing"

	"forgejo.org/models/unittest"
	"forgejo.org/modules/validation"
)

func TestMain(m *testing.M) {
	validation.AddBindingRules() // the forms of the settings pages use ValidUrl and GlobPattern, as in the server
	unittest.MainTest(m)
}
