// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"testing"

	"forgejo.org/modules/setting"

	"github.com/stretchr/testify/assert"
)

func TestRedactMigrationMessage(t *testing.T) {
	oldRoot, oldURL := setting.RepoRootPath, setting.Asc.ServerURL
	setting.RepoRootPath, setting.Asc.ServerURL = "/srv/forgejo/repositories", "http://asc.internal:7890"
	defer func() { setting.RepoRootPath, setting.Asc.ServerURL = oldRoot, oldURL }()

	for in, want := range map[string]string{
		"Clone: fatal: unable to access 'https://user:s3cret-token@github.com/o/r.git/'": "Clone: fatal: unable to access 'https://sanitized-credential@github.com/o/r.git/'",
		"object missing in /srv/forgejo/repositories/jordan/wthn.git/objects":            "object missing in <repositories>/jordan/wthn.git/objects",
		"open /tmp/fixrun/asc/objects/ab/cd: no such file":                               "open <path>: no such file",
		"importing the history into asc: dial tcp asc.internal:7890: refused":            "importing the history into asc: dial tcp <asc>: refused",
		"asc refused the imported history: path policy: .env is not allowed":             "asc refused the imported history: path policy: .env is not allowed",
		"fetch https://github.com/Jordan-Hall/wthn failed":                               "fetch https://github.com/Jordan-Hall/wthn failed",
		`C:\data\repos\x.git broke`:                                                      `<path> broke`,
	} {
		assert.Equal(t, want, RedactMigrationMessage(in), in)
	}
}
