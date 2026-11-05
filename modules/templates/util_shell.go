// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package templates

import "strings"

// ShellArg is one word of a command line a person copies into a POSIX shell: kept as it is when made only of letters, digits and
// ._+-/:@= (and not starting with -), else single quoted with each quote closed, escaped and reopened. A tag or a file name from a repository
// cannot then run anything when pasted.
func ShellArg(s string) string {
	safe := s != "" && s[0] != '-'
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._+-/:@=", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
