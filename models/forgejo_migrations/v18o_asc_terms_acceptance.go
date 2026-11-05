// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"forgejo.org/modules/timeutil"

	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "add asc_terms_acceptance: when a person agreed to the Terms and the Privacy policy",
		Upgrade:     addAscTermsAcceptance,
	})
}

func addAscTermsAcceptance(x *xorm.Engine) error {
	type AscTermsAcceptance struct {
		ID           int64              `xorm:"pk autoincr"`
		UserID       int64              `xorm:"UNIQUE NOT NULL"`
		AcceptedUnix timeutil.TimeStamp `xorm:"NOT NULL"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AscTermsAcceptance))
	return err
}
