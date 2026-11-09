// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package activities

import (
	"context"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
)

// GetUserHeatmapDataByUserRange is GetUserHeatmapDataByUser over [from, until) rather than the last year: the year
// switch of the contribution grid on a profile. The same rule decides what the viewer may count.
func GetUserHeatmapDataByUserRange(ctx context.Context, user, doer *user_model.User, from, until timeutil.TimeStamp) ([]*UserHeatmapData, error) {
	hdata := make([]*UserHeatmapData, 0)
	if !ActivityReadable(user, doer) || until <= from {
		return hdata, nil
	}
	groupBy := "created_unix / 900 * 900"
	if setting.Database.Type.IsMySQL() {
		groupBy = "created_unix DIV 900 * 900"
	}
	cond, err := activityQueryCondition(ctx, GetFeedsOptions{
		RequestedUser:   user,
		Actor:           doer,
		IncludePrivate:  true, // filtered by repository access in the condition
		OnlyPerformedBy: !user.IsOrganization(),
	})
	if err != nil {
		return nil, err
	}
	return hdata, db.GetEngine(ctx).
		Select(groupBy+" AS timestamp, count(user_id) as contributions").
		Table("action").
		Where(cond).
		And("created_unix >= ? AND created_unix < ?", from, until).
		GroupBy("timestamp").
		OrderBy("timestamp").
		Find(&hdata)
}
