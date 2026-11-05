// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package install

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
)

// The install progress (board Install, state "Installing"): while the form is sent, the page asks
// `GET /-/install/status` how many of the five steps are done. SubmitInstall does the first three (database tables
// created, repository root checked, administrator account); the post-install page shows the last two (the merge queue
// starts with the hub, then the hub opens) while it waits for the hub to answer.

// ascInstallSteps is the number of steps the install shows.
const ascInstallSteps = 5

// ascInstallDone is how many steps of the running install are done.
var ascInstallDone atomic.Int32

// installStatus answers `GET /-/install/status` with {"done": n, "total": 5}. It is outside the session middleware: a
// session saved by this request at the same time as the install would undo the sign in of the new administrator.
func installStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]int32{"done": ascInstallDone.Load(), "total": ascInstallSteps})
}
