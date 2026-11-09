// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"context"

	"forgejo.org/models/db"
	"forgejo.org/models/perm"
	"forgejo.org/modules/timeutil"
)

// AscAccessRequestMessageMax is the longest note a person may send with a request for access.
const AscAccessRequestMessageMax = 500

// AscAccessRequest is a request of a person for more access to a repository, made from the 403 page. The admins of
// the repository see it on the Collaborators page, where they grant or dismiss it. A person has at most one request
// for a repository; asking again replaces the note.
type AscAccessRequest struct {
	ID     int64 `xorm:"pk autoincr"`
	RepoID int64 `xorm:"UNIQUE(s) INDEX NOT NULL"`
	UserID int64 `xorm:"UNIQUE(s) INDEX NOT NULL"`
	// Mode is the access asked for: write, or admin for a person who can write already.
	Mode        perm.AccessMode    `xorm:"NOT NULL DEFAULT 2"`
	Message     string             `xorm:"TEXT"`
	CreatedUnix timeutil.TimeStamp `xorm:"created"`
}

func init() {
	db.RegisterModel(new(AscAccessRequest))
}

// TableName is the table of the model.
func (AscAccessRequest) TableName() string { return "asc_access_request" }

// AscAccessRequestMode is the access a request of the viewer asks for: write, or admin for a person who can write
// already. It is AccessModeNone for an admin, who has nothing more to ask for.
func AscAccessRequestMode(p perm.AccessMode) perm.AccessMode {
	switch {
	case p >= perm.AccessModeAdmin:
		return perm.AccessModeNone
	case p >= perm.AccessModeWrite:
		return perm.AccessModeAdmin
	default:
		return perm.AccessModeWrite
	}
}

// AscRequestAccess records the request of the user for the access mode to the repository, or replaces the mode and
// the note of the request the user made before. The note is cut to AscAccessRequestMessageMax runes.
func AscRequestAccess(ctx context.Context, repoID, userID int64, mode perm.AccessMode, message string) error {
	if r := []rune(message); len(r) > AscAccessRequestMessageMax {
		message = string(r[:AscAccessRequestMessageMax])
	}
	return db.WithTx(ctx, func(ctx context.Context) error {
		e := db.GetEngine(ctx)
		old := &AscAccessRequest{}
		has, err := e.Where("repo_id = ? AND user_id = ?", repoID, userID).Get(old)
		if err != nil {
			return err
		}
		if has {
			old.Mode, old.Message = mode, message
			_, err = e.ID(old.ID).Cols("mode", "message").Update(old)
			return err
		}
		return db.Insert(ctx, &AscAccessRequest{RepoID: repoID, UserID: userID, Mode: mode, Message: message})
	})
}

// GetAscAccessRequest gives the request of the user for the repository, nil when there is none.
func GetAscAccessRequest(ctx context.Context, repoID, userID int64) (*AscAccessRequest, error) {
	r := &AscAccessRequest{}
	has, err := db.GetEngine(ctx).Where("repo_id = ? AND user_id = ?", repoID, userID).Get(r)
	if err != nil || !has {
		return nil, err
	}
	return r, nil
}

// GetAscAccessRequestByID gives the request with the id when it is for the repository, nil otherwise.
func GetAscAccessRequestByID(ctx context.Context, repoID, id int64) (*AscAccessRequest, error) {
	r := &AscAccessRequest{}
	has, err := db.GetEngine(ctx).Where("id = ? AND repo_id = ?", id, repoID).Get(r)
	if err != nil || !has {
		return nil, err
	}
	return r, nil
}

// ListAscAccessRequests gives the requests for the repository, oldest first.
func ListAscAccessRequests(ctx context.Context, repoID int64) ([]*AscAccessRequest, error) {
	var out []*AscAccessRequest
	return out, db.GetEngine(ctx).Where("repo_id = ?", repoID).OrderBy("created_unix ASC, id ASC").Find(&out)
}

// CountAscAccessRequests counts the requests for the repository.
func CountAscAccessRequests(ctx context.Context, repoID int64) (int64, error) {
	return db.GetEngine(ctx).Where("repo_id = ?", repoID).Count(new(AscAccessRequest))
}

// DeleteAscAccessRequest removes the request of the user for the repository; none is not an error.
func DeleteAscAccessRequest(ctx context.Context, repoID, userID int64) error {
	_, err := db.GetEngine(ctx).Where("repo_id = ? AND user_id = ?", repoID, userID).Delete(new(AscAccessRequest))
	return err
}
