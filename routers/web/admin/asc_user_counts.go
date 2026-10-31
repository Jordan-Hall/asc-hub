// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/services/context"
)

// AdminUserCounts are the numbers on the filter chips of the people list: how many accounts each filter holds, whatever
// the search words or the chip in use (the board shows "Admins 6, 2FA off 212").
type AdminUserCounts struct {
	All, Admins, TwoFAOff, Inactive, Suspended, Restricted int64
}

// adminUserCounts counts the accounts of each chip. A count that cannot be read is left at 0, which the chip does not show.
func adminUserCounts(ctx *context.Context) *AdminUserCounts {
	count := func(f func(o *user_model.SearchUserOptions)) int64 {
		o := &user_model.SearchUserOptions{
			Actor:           ctx.Doer,
			Type:            user_model.UserTypeIndividual,
			ListOptions:     db.ListOptions{PageSize: 1, Page: 1},
			IncludeReserved: true, // as the list: reserved, bot and remote accounts count too
		}
		f(o)
		_, n, err := user_model.SearchUsers(ctx, o)
		if err != nil {
			log.Warn("admin users: count for a chip: %v", err)
			return 0
		}
		return n
	}
	return &AdminUserCounts{
		All:        count(func(*user_model.SearchUserOptions) {}),
		Admins:     count(func(o *user_model.SearchUserOptions) { o.IsAdmin = optional.Some(true) }),
		TwoFAOff:   count(func(o *user_model.SearchUserOptions) { o.IsTwoFactorEnabled = optional.Some(false) }),
		Inactive:   count(func(o *user_model.SearchUserOptions) { o.IsActive = optional.Some(false) }),
		Suspended:  count(func(o *user_model.SearchUserOptions) { o.IsProhibitLogin = optional.Some(true) }),
		Restricted: count(func(o *user_model.SearchUserOptions) { o.IsRestricted = optional.Some(true) }),
	}
}
