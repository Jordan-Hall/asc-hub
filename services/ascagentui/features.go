// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"context"
	"errors"

	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
)

// FeaturesKey is the user setting that turns the agent surfaces on. Agents are optional: with the setting
// absent or "false" the agents and workflow actions of this unit answer 404 and are not linked from any page.
const FeaturesKey = "asc.agent_features"

// FeaturesEnabled reports whether the user chose to see agent features. It fails closed: an anonymous user, a
// missing setting and a database error all read as off.
func FeaturesEnabled(ctx context.Context, user *user_model.User) bool {
	if user == nil || user.ID <= 0 {
		return false
	}
	v, err := user_model.GetUserSetting(ctx, user.ID, FeaturesKey, "false")
	if err != nil {
		log.Warn("asc agent features: reading the setting of user %d failed: %v", user.ID, err)
		return false
	}
	return v == "true"
}

// SetFeatures stores the switch.
func SetFeatures(ctx context.Context, user *user_model.User, on bool) error {
	if user == nil || user.ID <= 0 {
		return errors.New("no signed-in user")
	}
	v := "false"
	if on {
		v = "true"
	}
	return user_model.SetUserSetting(ctx, user.ID, FeaturesKey, v)
}
