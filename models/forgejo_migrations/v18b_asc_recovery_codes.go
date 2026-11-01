// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add two_factor_recovery_code: ten recovery codes per person, each spent once, kept as salted hashes",
		Upgrade:     addTwoFactorRecoveryCodes,
	})
}

func addTwoFactorRecoveryCodes(x *xorm.Engine) error {
	type TwoFactorRecoveryCode struct {
		ID          int64  `xorm:"pk autoincr"`
		UID         int64  `xorm:"INDEX NOT NULL"`
		Salt        string `xorm:"VARCHAR(64) NOT NULL"`
		Hash        string `xorm:"VARCHAR(128) NOT NULL"`
		UsedUnix    int64  `xorm:"NOT NULL DEFAULT 0"`
		CreatedUnix int64  `xorm:"NOT NULL DEFAULT 0"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(TwoFactorRecoveryCode))
	return err
}
