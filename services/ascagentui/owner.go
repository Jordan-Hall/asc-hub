// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascagentui

import (
	"context"
	"errors"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/timeutil"
)

// Who started a run, bound to the immutable id of a person.
//
// asc names the creator of a run by user name. A name is not an identity on the hub: a person can be renamed, and the old
// name can be taken by someone else. The text and the log of a run are private to its creator, so the hub keeps its own
// index from (repository, run) to the user id of the creator, and the visibility rule compares ids, never names.
//
//   - A run the hub starts (a task form, a continued run) records its creator at that moment.
//   - A run the hub meets first on a page (started by the CLI, a workflow or an agent) is recorded the first time it is seen,
//     from the person the creator name belongs to then. From then on a rename or a recycled name cannot move it.
//   - A name that belongs to nobody, or a record that cannot be written, gives no owner: the text stays hidden.
//
// The window of the second case is between the start of a run outside the hub and the first time the hub sees it; asc does
// not put the hub's user id on a run, so the hub cannot close it alone.

// AscRunOwner is one row of the index.
type AscRunOwner struct {
	ID          int64              `xorm:"pk autoincr"`
	RepoID      int64              `xorm:"UNIQUE(repo_run) NOT NULL"`
	RunID       uint64             `xorm:"UNIQUE(repo_run) NOT NULL"`
	UserID      int64              `xorm:"INDEX NOT NULL"`
	CreatedUnix timeutil.TimeStamp `xorm:"created NOT NULL"`
}

// TableName gives the table its name.
func (*AscRunOwner) TableName() string { return "asc_run_owner" }

func init() {
	db.RegisterModel(new(AscRunOwner))
}

// RecordRunOwner records the person who started a run through the hub. It never changes a record that exists.
func RecordRunOwner(ctx context.Context, repoID int64, runID uint64, userID int64) error {
	if repoID == 0 || runID == 0 || userID == 0 {
		return errors.New("ascagentui: a run owner needs a repository, a run and a user")
	}
	has, err := db.GetEngine(ctx).Where("repo_id = ? AND run_id = ?", repoID, runID).Exist(new(AscRunOwner))
	if err != nil || has {
		return err
	}
	_, err = db.GetEngine(ctx).Insert(&AscRunOwner{RepoID: repoID, RunID: runID, UserID: userID})
	if err != nil {
		// A concurrent insert of the same row wins; read it back to see that someone recorded it.
		if has, e := db.GetEngine(ctx).Where("repo_id = ? AND run_id = ?", repoID, runID).Exist(new(AscRunOwner)); e == nil && has {
			return nil
		}
	}
	return err
}

// RunOwnerID returns the user id of the person who started the run, or 0 when there is none to be sure of.
// createdBy is the name asc gives; it is used only when the hub has no record yet, and then only to make one.
func RunOwnerID(ctx context.Context, repoID int64, runID uint64, createdBy string) int64 {
	if repoID == 0 || runID == 0 {
		return 0
	}
	var row AscRunOwner
	has, err := db.GetEngine(ctx).Where("repo_id = ? AND run_id = ?", repoID, runID).Get(&row)
	if err != nil {
		return 0 // fail closed
	}
	if has {
		return row.UserID
	}
	name := SafeName(createdBy)
	if name == "" {
		return 0
	}
	u, err := user_model.GetUserByName(ctx, name)
	if err != nil || u == nil || u.ID <= 0 {
		return 0
	}
	if err := RecordRunOwner(ctx, repoID, runID, u.ID); err != nil {
		return 0
	}
	// Read back: if another request recorded a different owner first, that one stands.
	if has, err := db.GetEngine(ctx).Where("repo_id = ? AND run_id = ?", repoID, runID).Get(&row); err != nil || !has {
		return 0
	}
	return row.UserID
}
