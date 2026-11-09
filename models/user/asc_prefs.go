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

// How a person reads times: a 24-hour or 12-hour clock, and the order of day, month and year. The defaults are the
// boards' "27 Oct 14:05" and "3 Feb 2026". The script that draws times (web_src/js/features/local-time.js) reads them
// from <html data-clock data-date-order>.

const (
	AscClock24 = "24"
	AscClock12 = "12"

	AscDateDMY = "dmy"
	AscDateMDY = "mdy"
	AscDateYMD = "ymd"
)

// AscTimeFormat is the clock and date order of the person, with the defaults for what they never chose.
func AscTimeFormat(ctx context.Context, userID int64) (clock, order string) {
	clock, order = AscClock24, AscDateDMY
	all, err := GetSettings(ctx, userID, []string{SettingsKeyAscClock, SettingsKeyAscDateOrder})
	if err != nil {
		log.Error("asc time format of user %d: %v", userID, err)
		return clock, order
	}
	if s := all[SettingsKeyAscClock]; s != nil && validAscClock(s.SettingValue) {
		clock = s.SettingValue
	}
	if s := all[SettingsKeyAscDateOrder]; s != nil && validAscDateOrder(s.SettingValue) {
		order = s.SettingValue
	}
	return clock, order
}

// SetAscTimeFormat saves the clock and date order of the person; a value it does not know is refused and nothing is saved.
func SetAscTimeFormat(ctx context.Context, userID int64, clock, order string) error {
	if !validAscClock(clock) || !validAscDateOrder(order) {
		return ErrAscTimeFormat
	}
	if err := SetUserSetting(ctx, userID, SettingsKeyAscClock, clock); err != nil {
		return err
	}
	return SetUserSetting(ctx, userID, SettingsKeyAscDateOrder, order)
}

func validAscClock(v string) bool { return v == AscClock24 || v == AscClock12 }

func validAscDateOrder(v string) bool { return v == AscDateDMY || v == AscDateMDY || v == AscDateYMD }

// ErrAscTimeFormat is the error of a clock or date order it does not know.
var ErrAscTimeFormat = errAscTimeFormat{}

type errAscTimeFormat struct{}

func (errAscTimeFormat) Error() string {
	return "clock must be 24 or 12 and date order dmy, mdy or ymd"
}
