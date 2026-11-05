// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"context"

	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"
)

// AscTermsAcceptance records that a person agreed to the Terms and the Privacy policy when they created their account
// (board Onboarding), and when.
type AscTermsAcceptance struct {
	ID           int64              `xorm:"pk autoincr"`
	UserID       int64              `xorm:"UNIQUE NOT NULL"`
	AcceptedUnix timeutil.TimeStamp `xorm:"NOT NULL"`
}

func init() {
	db.RegisterModel(new(AscTermsAcceptance))
}

// TableName is the table of the model.
func (AscTermsAcceptance) TableName() string { return "asc_terms_acceptance" }

// RecordAscTermsAcceptance stores that the person agreed now. A second agreement keeps the first time.
func RecordAscTermsAcceptance(ctx context.Context, userID int64) error {
	e := db.GetEngine(ctx)
	if has, err := e.Where("user_id = ?", userID).Exist(new(AscTermsAcceptance)); err != nil || has {
		return err
	}
	_, err := e.Insert(&AscTermsAcceptance{UserID: userID, AcceptedUnix: timeutil.TimeStampNow()})
	return err
}

// GetAscTermsAcceptance returns when the person agreed, or nil when there is no record (an account made before the
// box, by an administrator or by an external sign-in).
func GetAscTermsAcceptance(ctx context.Context, userID int64) (*AscTermsAcceptance, error) {
	row := new(AscTermsAcceptance)
	has, err := db.GetEngine(ctx).Where("user_id = ?", userID).Get(row)
	if err != nil || !has {
		return nil, err
	}
	return row, nil
}
