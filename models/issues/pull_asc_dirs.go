// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package issues

import (
	"context"

	"forgejo.org/models/db"
)

// The directories a pull request changes, kept per head commit so "Knows best" on a profile reads the repository
// once per push rather than once per page view. The hub works them out from its own repository (the files changed
// between the merge base and the head of the pull request).

// PullRequestAscDirs is the count of changed files per directory of a pull request at a head commit.
type PullRequestAscDirs struct {
	ID     int64          `xorm:"pk autoincr"`
	PullID int64          `xorm:"UNIQUE NOT NULL"`
	Head   string         `xorm:"VARCHAR(64) NOT NULL"`
	Dirs   map[string]int `xorm:"JSON TEXT"`
}

// TableName gives the table its name.
func (*PullRequestAscDirs) TableName() string { return "pull_request_asc_dirs" }

func init() {
	db.RegisterModel(new(PullRequestAscDirs))
}

// GetPullRequestAscDirs returns the directories kept for the pull requests with the given ids, by pull request id.
func GetPullRequestAscDirs(ctx context.Context, pullIDs []int64) (map[int64]*PullRequestAscDirs, error) {
	out := make(map[int64]*PullRequestAscDirs, len(pullIDs))
	if len(pullIDs) == 0 {
		return out, nil
	}
	rows := make([]*PullRequestAscDirs, 0, len(pullIDs))
	if err := db.GetEngine(ctx).In("pull_id", pullIDs).Find(&rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.PullID] = r
	}
	return out, nil
}

// SetPullRequestAscDirs keeps the directories of the pull request at head, replacing what was kept for an older head.
func SetPullRequestAscDirs(ctx context.Context, pullID int64, head string, dirs map[string]int) error {
	if dirs == nil {
		dirs = map[string]int{}
	}
	return db.WithTx(ctx, func(ctx context.Context) error {
		e := db.GetEngine(ctx)
		n, err := e.Where("pull_id = ?", pullID).Cols("head", "dirs").Update(&PullRequestAscDirs{Head: head, Dirs: dirs})
		if err != nil || n > 0 {
			return err
		}
		_, err = e.Insert(&PullRequestAscDirs{PullID: pullID, Head: head, Dirs: dirs})
		return err
	})
}
