// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package templates

import "sync/atomic"

// The status line of the footer ("All systems operational") says what the hub knows about itself: whether its database
// and, with asc enabled, asc serve answer. The check lives in the services (they own the connections); this package only
// holds the function the start-up code gives it, so a page never waits for a probe it started itself.
var ascStatusFn atomic.Pointer[func() string]

// SetAscStatus sets the function that answers "ok" or "degraded".
func SetAscStatus(f func() string) { ascStatusFn.Store(&f) }

// AscSystemStatus is "ok" unless the function set with SetAscStatus says otherwise.
func AscSystemStatus() string {
	if f := ascStatusFn.Load(); f != nil {
		if s := (*f)(); s == "degraded" {
			return s
		}
	}
	return "ok"
}
