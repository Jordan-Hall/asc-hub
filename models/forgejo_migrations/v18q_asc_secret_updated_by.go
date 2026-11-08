// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"forgejo.org/modules/timeutil"

	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add secret.updated_unix and secret.updater_id: the secrets list says who last set each value and when",
		Upgrade:     addSecretUpdatedBy,
	})
}

type v18qSecret struct {
	ID          int64              `xorm:"pk autoincr"`
	UpdatedUnix timeutil.TimeStamp `xorm:"NOT NULL DEFAULT 0"`
	UpdaterID   int64              `xorm:"NOT NULL DEFAULT 0"`
}

func (v18qSecret) TableName() string { return "secret" }

func addSecretUpdatedBy(x *xorm.Engine) error {
	if _, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(v18qSecret)); err != nil {
		return err
	}
	// a secret set before the upgrade was last set when it was made, by someone not recorded
	_, err := x.Exec("UPDATE `secret` SET `updated_unix` = `created_unix` WHERE `updated_unix` = 0")
	return err
}
