// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"net/url"
	"regexp"
	"strings"

	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
)

// absPathPattern matches an absolute file path that stands alone in a message (after a space, a quote, a bracket
// or "="), not the path part of a URL.
var absPathPattern = regexp.MustCompile(`(^|[\s"'(\[=])((?:[A-Za-z]:\\|/)(?:[\w.\-~]+[/\\])+[\w.\-~]*)`)

// RedactMigrationMessage makes the reason of a failed migration fit to show to a person: credentials in URLs
// are removed, and so are the places inside the instance that an error of git or asc names (the repository
// root, file paths, the address of the asc server). The text stays otherwise as it is.
func RedactMigrationMessage(msg string) string {
	msg = util.SanitizeCredentialURLs(msg)
	if root := strings.TrimRight(setting.RepoRootPath, `/\`); len(root) > 1 {
		msg = strings.ReplaceAll(msg, root, "<repositories>")
	}
	if u, err := url.Parse(setting.Asc.ServerURL); err == nil && u.Host != "" {
		msg = strings.ReplaceAll(msg, u.Host, "<asc>")
	}
	return absPathPattern.ReplaceAllString(msg, "${1}<path>")
}
