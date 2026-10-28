// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"context"
	"strconv"
)

const ascAgentsReadProfileKey = "asc.agents_read_profile"

// AscAgentsMayReadProfile reports whether agents may read the profile fields of the person (name, bio, location,
// website, pronouns, email). An agent is a run of the hub's own automation, a request with an Actions task token, and the
// agent access path of asc serve. The default is yes; the person switches it off on the Privacy page.
func AscAgentsMayReadProfile(ctx context.Context, userID int64) bool {
	v, err := GetUserSetting(ctx, userID, ascAgentsReadProfileKey, "true")
	return err != nil || v != "false"
}

// SetAscAgentsMayReadProfile stores the choice.
func SetAscAgentsMayReadProfile(ctx context.Context, userID int64, allow bool) error {
	return SetUserSetting(ctx, userID, ascAgentsReadProfileKey, strconv.FormatBool(allow))
}
