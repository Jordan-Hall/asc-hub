// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import "code.forgejo.org/xorm/xorm"

func init() {
	registerMigration(&Migration{
		Description: "add secret.scopes, action_variable.scopes and action_variable.updater_id: which runs receive a secret or variable, and who last set a variable",
		Upgrade:     addAscRunScopes,
	})
}

type v19eSecret struct {
	ID     int64 `xorm:"pk autoincr"`
	Scopes int   `xorm:"NOT NULL DEFAULT 3"`
}

func (v19eSecret) TableName() string { return "secret" }

type v19eVariable struct {
	ID        int64 `xorm:"pk autoincr"`
	UpdaterID int64 `xorm:"NOT NULL DEFAULT 0"`
	Scopes    int   `xorm:"NOT NULL DEFAULT 3"`
}

func (v19eVariable) TableName() string { return "action_variable" }

// addAscRunScopes gives every secret and variable Checks and Releases (3): a run an agent started stops receiving
// them until someone ticks Agent runs.
func addAscRunScopes(x *xorm.Engine) error {
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(v19eSecret), new(v19eVariable))
	return err
}
