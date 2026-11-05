// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package eventsource

import (
	"context"

	activities_model "forgejo.org/models/activities"
	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/timeutil"
)

// ascBellCounts makes the live number of the header bell count the events asc reported as well (the popover lists
// both): it adds the unread events to the counts of the hub, and adds the people told about an event in [from, to)
// who had no notification of the hub in that time.
func ascBellCounts(ctx context.Context, counts []activities_model.UserIDCount, from, to timeutil.TimeStamp) []activities_model.UserIDCount {
	if !setting.Asc.Enabled {
		return counts
	}
	seen := make(map[int64]bool, len(counts))
	out := make([]activities_model.UserIDCount, 0, len(counts))
	for _, c := range counts {
		seen[c.UserID] = true
		out = append(out, activities_model.UserIDCount{UserID: c.UserID, Count: c.Count + ascUnread(ctx, c.UserID)})
	}
	told, err := user_model.UserIDsWithAscNotificationsBetween(ctx, from, to)
	if err != nil {
		log.Error("asc bell: people told between %d and %d: %v", from, to, err)
		return out
	}
	for _, uid := range told {
		if seen[uid] {
			continue
		}
		hub, err := db.Count[activities_model.Notification](ctx, activities_model.FindNotificationOptions{
			UserID: uid,
			Status: []activities_model.NotificationStatus{activities_model.NotificationStatusUnread},
		})
		if err != nil {
			log.Error("asc bell: unread of user %d: %v", uid, err)
			continue
		}
		out = append(out, activities_model.UserIDCount{UserID: uid, Count: hub + ascUnread(ctx, uid)})
	}
	return out
}

func ascUnread(ctx context.Context, uid int64) int64 {
	n, err := user_model.CountUnreadAscNotifications(ctx, uid)
	if err != nil {
		log.Error("asc bell: unread events of user %d: %v", uid, err)
		return 0
	}
	return n
}
