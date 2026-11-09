// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !linux

package admin

// hostClockSynced cannot ask the kernel outside Linux: the clock check then compares with asc serve only.
func hostClockSynced() (synced, known bool) { return false, false }
