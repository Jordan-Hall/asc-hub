// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"forgejo.org/modules/timeutil"

	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add expires_unix to action_runner_token: a registration token shown once on the web expires after an hour",
		Upgrade:     addRunnerTokenExpiry,
	})
}

func addRunnerTokenExpiry(x *xorm.Engine) error {
	type ActionRunnerToken struct {
		ID          int64
		ExpiresUnix timeutil.TimeStamp `xorm:"NOT NULL DEFAULT 0"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(ActionRunnerToken))
	return err
}
