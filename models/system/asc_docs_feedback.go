// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package system

import (
	"context"

	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"
)

// AscDocsFeedback is one answer to "Was this page helpful?" on a documentation page. It holds no reader: only the
// page, the answer and when.
type AscDocsFeedback struct {
	ID          int64              `xorm:"pk autoincr"`
	Slug        string             `xorm:"VARCHAR(100) INDEX NOT NULL"`
	Helpful     bool               `xorm:"NOT NULL"`
	CreatedUnix timeutil.TimeStamp `xorm:"created INDEX"`
}

func init() {
	db.RegisterModel(new(AscDocsFeedback))
}

// AddAscDocsFeedback stores one answer for the page slug.
func AddAscDocsFeedback(ctx context.Context, slug string, helpful bool) error {
	_, err := db.GetEngine(ctx).Insert(&AscDocsFeedback{Slug: slug, Helpful: helpful})
	return err
}

// AscDocsFeedbackCount is how many readers of a page found it helpful and how many did not.
type AscDocsFeedbackCount struct {
	Yes int64
	No  int64
}

// CountAscDocsFeedback counts the answers for the page slug.
func CountAscDocsFeedback(ctx context.Context, slug string) (AscDocsFeedbackCount, error) {
	var c AscDocsFeedbackCount
	var err error
	if c.Yes, err = db.GetEngine(ctx).Where("slug = ? AND helpful = ?", slug, true).Count(new(AscDocsFeedback)); err != nil {
		return c, err
	}
	c.No, err = db.GetEngine(ctx).Where("slug = ? AND helpful = ?", slug, false).Count(new(AscDocsFeedback))
	return c, err
}
