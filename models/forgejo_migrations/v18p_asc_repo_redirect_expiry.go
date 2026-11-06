// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"time"

	"forgejo.org/modules/timeutil"

	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add repo_redirect.expires_unix: the old address of a renamed repository redirects for 90 days",
		Upgrade:     addRepoRedirectExpiry,
	})
}

type v18pRepoRedirect struct {
	ID          int64              `xorm:"pk autoincr"`
	ExpiresUnix timeutil.TimeStamp `xorm:"INDEX NOT NULL DEFAULT 0"`
}

func (v18pRepoRedirect) TableName() string { return "repo_redirect" }

func addRepoRedirectExpiry(x *xorm.Engine) error {
	if _, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(v18pRepoRedirect)); err != nil {
		return err
	}
	// the redirects made before keep redirecting for 90 days from the upgrade
	expires := timeutil.TimeStampNow().AddDuration(90 * 24 * time.Hour)
	_, err := x.Exec("UPDATE `repo_redirect` SET `expires_unix` = ? WHERE `expires_unix` = 0", expires)
	return err
}
