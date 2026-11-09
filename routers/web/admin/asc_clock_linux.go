// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import "golang.org/x/sys/unix"

// hostClockSynced asks the kernel whether the host clock is kept in time (NTP), false in known when it cannot say.
func hostClockSynced() (synced, known bool) {
	var tx unix.Timex
	state, err := unix.Adjtimex(&tx)
	if err != nil {
		return false, false
	}
	return state != unix.TIME_ERROR && tx.Status&unix.STA_UNSYNC == 0, true
}
