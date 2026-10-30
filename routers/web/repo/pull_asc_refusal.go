// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"html"

	repostore_asc "forgejo.org/modules/repostore/asc"
)

// ascRefusalFlash is the flash text for a landing that asc refused because of the repository's policy (approvals,
// checks, the order of a stack): asc's own sentence, escaped, so a person reads why instead of a server error.
func ascRefusalFlash(err error) (string, bool) {
	sentence, ok := repostore_asc.PolicyRefusal(err)
	if !ok {
		return "", false
	}
	return html.EscapeString(sentence), true
}
