// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add browser_session: the browsers a person is signed in on (hashed session ID, browser, address, last active), so one can be signed out from another",
		Upgrade:     addBrowserSession,
	})
}

func addBrowserSession(x *xorm.Engine) error {
	type BrowserSession struct {
		ID             int64  `xorm:"pk autoincr"`
		UID            int64  `xorm:"INDEX NOT NULL"`
		SIDHash        string `xorm:"VARCHAR(64) UNIQUE NOT NULL"`
		RememberKey    string `xorm:"VARCHAR(255) NOT NULL DEFAULT ''"`
		UserAgent      string `xorm:"VARCHAR(255) NOT NULL DEFAULT ''"`
		IP             string `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
		CreatedUnix    int64  `xorm:"NOT NULL DEFAULT 0"`
		LastActiveUnix int64  `xorm:"INDEX NOT NULL DEFAULT 0"`
		RevokedUnix    int64  `xorm:"NOT NULL DEFAULT 0"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(BrowserSession))
	return err
}
