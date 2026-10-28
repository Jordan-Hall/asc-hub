// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"context"
	"errors"
	"fmt"

	"forgejo.org/models/db"
)

// AscMaxProfilePins is how many repositories a person can pin to the profile.
const AscMaxProfilePins = 6

// AscProfilePin is one repository a person pinned to their profile. Position orders the pins (0 first).
type AscProfilePin struct {
	ID       int64 `xorm:"pk autoincr"`
	UserID   int64 `xorm:"UNIQUE(user_repo) INDEX NOT NULL"`
	RepoID   int64 `xorm:"UNIQUE(user_repo) NOT NULL"`
	Position int   `xorm:"NOT NULL DEFAULT 0"`
}

func init() {
	db.RegisterModel(new(AscProfilePin))
}

// TableName is the table of the model.
func (AscProfilePin) TableName() string { return "asc_profile_pin" }

// ErrTooManyProfilePins is returned when more than AscMaxProfilePins repositories are given.
var ErrTooManyProfilePins = errors.New("too many pinned repositories")

// ListAscProfilePins returns the repository ids pinned by the person, in order. The caller still has to check what
// the viewer may see: a pin of a private repository must not show to everyone.
func ListAscProfilePins(ctx context.Context, userID int64) ([]int64, error) {
	var pins []*AscProfilePin
	if err := db.GetEngine(ctx).Where("user_id = ?", userID).OrderBy("position ASC, id ASC").Find(&pins); err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(pins))
	for _, p := range pins {
		ids = append(ids, p.RepoID)
	}
	return ids, nil
}

// SetAscProfilePins replaces the pins of the person by the given repository ids, in the given order. The caller
// checks that the person may pin each repository.
func SetAscProfilePins(ctx context.Context, userID int64, repoIDs []int64) error {
	if len(repoIDs) > AscMaxProfilePins {
		return ErrTooManyProfilePins
	}
	seen := map[int64]bool{}
	for _, id := range repoIDs {
		if id <= 0 || seen[id] {
			return fmt.Errorf("repository id %d is not valid or repeated", id)
		}
		seen[id] = true
	}
	return db.WithTx(ctx, func(ctx context.Context) error {
		if _, err := db.GetEngine(ctx).Where("user_id = ?", userID).Delete(new(AscProfilePin)); err != nil {
			return err
		}
		for i, id := range repoIDs {
			if _, err := db.GetEngine(ctx).Insert(&AscProfilePin{UserID: userID, RepoID: id, Position: i}); err != nil {
				return err
			}
		}
		return nil
	})
}
