// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package moderation

import (
	"context"
	"errors"

	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"
)

// Hidden is reported content an administrator hid (board AdminModeration, Hide): it stays in place for its owner and
// the administrators but is out of public view until the hide is undone. Saved keeps what hiding changed (a visibility,
// a title or a text), so undoing it puts back exactly that.
type Hidden struct {
	ID          int64               `xorm:"pk autoincr"`
	ContentType ReportedContentType `xorm:"UNIQUE(s) NOT NULL"`
	ContentID   int64               `xorm:"UNIQUE(s) NOT NULL"`
	Saved       string              `xorm:"LONGTEXT"`
	ModeratorID int64               `xorm:"NOT NULL"`
	CreatedUnix timeutil.TimeStamp  `xorm:"created NOT NULL"`
}

// TableName is the table of hidden content.
func (Hidden) TableName() string { return "asc_moderation_hidden" }

func init() {
	db.RegisterModel(new(Hidden))
}

// ErrHidden is returned when the owner tries to change what a hide changed (the text of a hidden comment or issue, the
// visibility of a hidden repository or profile) while the hide stands.
var ErrHidden = errors.New("moderation: the content is hidden by a moderator")

// CheckNotHidden answers ErrHidden when the content is hidden and the doer is not an administrator.
func CheckNotHidden(ctx context.Context, doerIsAdmin bool, contentType ReportedContentType, contentID int64) error {
	if doerIsAdmin {
		return nil
	}
	hidden, err := IsHidden(ctx, contentType, contentID)
	if err != nil {
		return err
	}
	if hidden {
		return ErrHidden
	}
	return nil
}

// GetHidden returns the hide of the content, or nil when it is not hidden.
func GetHidden(ctx context.Context, contentType ReportedContentType, contentID int64) (*Hidden, error) {
	h := new(Hidden)
	has, err := db.GetEngine(ctx).Where("content_type = ? AND content_id = ?", contentType, contentID).Get(h)
	if err != nil || !has {
		return nil, err
	}
	return h, nil
}

// IsHidden tells whether the content is hidden.
func IsHidden(ctx context.Context, contentType ReportedContentType, contentID int64) (bool, error) {
	return db.GetEngine(ctx).Where("content_type = ? AND content_id = ?", contentType, contentID).Exist(new(Hidden))
}

// InsertHidden keeps a hide.
func InsertHidden(ctx context.Context, h *Hidden) error {
	h.ID = 0
	_, err := db.GetEngine(ctx).Insert(h)
	return err
}

// DeleteHidden forgets the hide of the content.
func DeleteHidden(ctx context.Context, contentType ReportedContentType, contentID int64) error {
	_, err := db.GetEngine(ctx).Where("content_type = ? AND content_id = ?", contentType, contentID).Delete(new(Hidden))
	return err
}
