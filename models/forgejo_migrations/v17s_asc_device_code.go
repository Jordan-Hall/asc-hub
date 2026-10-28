// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_device_code: a device authorisation of the asc command line",
		Upgrade:     addAscDeviceCode,
	})
}

func addAscDeviceCode(x *xorm.Engine) error {
	type AscDeviceCode struct {
		ID             int64  `xorm:"pk autoincr"`
		DeviceCodeHash string `xorm:"VARCHAR(64) UNIQUE NOT NULL"`
		UserCode       string `xorm:"VARCHAR(16) UNIQUE NOT NULL"`
		DeviceName     string `xorm:"VARCHAR(128) NOT NULL DEFAULT ''"`
		Status         int    `xorm:"NOT NULL DEFAULT 0"`
		UserID         int64  `xorm:"NOT NULL DEFAULT 0"`
		ExpiresUnix    int64  `xorm:"INDEX NOT NULL"`
		LastPollUnix   int64  `xorm:"NOT NULL DEFAULT 0"`
		CreatedUnix    int64  `xorm:"created"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscDeviceCode))
	return err
}
