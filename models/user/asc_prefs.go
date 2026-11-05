// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"context"

	"forgejo.org/modules/log"
)

// The row density a person chose with the header menu or on Settings, Appearance: comfy (the default) or compact.
// It is kept with the account, so it follows the person to every browser.

const (
	AscDensityComfy   = "comfy"
	AscDensityCompact = "compact"
)

// AscDensity is the density of the person: comfy unless they chose compact.
func AscDensity(ctx context.Context, userID int64) string {
	v, err := GetUserSetting(ctx, userID, SettingsKeyAscDensity)
	if err != nil {
		log.Error("asc density of user %d: %v", userID, err)
		return AscDensityComfy
	}
	if v == AscDensityCompact {
		return AscDensityCompact
	}
	return AscDensityComfy
}

// SetAscDensity saves the density of the person; a value other than comfy or compact is refused.
func SetAscDensity(ctx context.Context, userID int64, density string) error {
	if density != AscDensityComfy && density != AscDensityCompact {
		return ErrAscDensity
	}
	return SetUserSetting(ctx, userID, SettingsKeyAscDensity, density)
}

// ErrAscDensity is the error of a density that is not comfy or compact.
var ErrAscDensity = errAscDensity{}

type errAscDensity struct{}

func (errAscDensity) Error() string { return "density must be comfy or compact" }
