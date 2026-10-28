// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_push_subscription: the browsers a person allowed to show notifications (Web Push)",
		Upgrade:     addAscPushSubscription,
	})
}

func addAscPushSubscription(x *xorm.Engine) error {
	type AscPushSubscription struct {
		ID           int64  `xorm:"pk autoincr"`
		UserID       int64  `xorm:"INDEX NOT NULL"`
		EndpointHash string `xorm:"VARCHAR(64) UNIQUE NOT NULL"`
		Endpoint     string `xorm:"TEXT NOT NULL"`
		P256dh       string `xorm:"VARCHAR(128) NOT NULL"`
		Auth         string `xorm:"VARCHAR(32) NOT NULL"`
		CreatedUnix  int64  `xorm:"created"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscPushSubscription))
	return err
}
