// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package git_test

// Link the default backend into the test binary of package git: its internal
// tests open repositories with git.OpenRepository, which binds the "gitcli"
// backend. The package cannot import gitcli itself (gitcli imports git), but an
// external test package can.
import _ "forgejo.org/modules/git/gitcli"
