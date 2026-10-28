// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package web

import (
	"forgejo.org/services/ascpresence"
	"forgejo.org/services/context"
)

// ascTouchPresence notes a request of a signed-in person for the green "online" dot of their profile (stored at most
// every five minutes, see services/ascpresence).
func ascTouchPresence(ctx *context.Context) {
	if ctx.IsSigned {
		ascpresence.Touch(ctx, ctx.Doer)
	}
}
