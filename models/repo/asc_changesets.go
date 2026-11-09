// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"context"

	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"
)

// AscRepoChangesets is the number of changesets (commits) reachable from the head of the default branch of a
// repository, as the stats indexer last counted it. CommitSha is the head it was counted at, so a reader can tell a
// stale count from a current one.
type AscRepoChangesets struct {
	RepoID      int64              `xorm:"pk"`
	CommitSha   string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	Changesets  int64              `xorm:"NOT NULL DEFAULT 0"`
	UpdatedUnix timeutil.TimeStamp `xorm:"updated"`
}

func init() {
	db.RegisterModel(new(AscRepoChangesets))
}

// TableName is the table of the model.
func (AscRepoChangesets) TableName() string { return "asc_repo_changesets" }

// GetAscRepoChangesets returns the stored counts of the repositories, keyed by repository id. A repository that was
// never counted has no entry.
func GetAscRepoChangesets(ctx context.Context, repoIDs []int64) (map[int64]*AscRepoChangesets, error) {
	out := make(map[int64]*AscRepoChangesets, len(repoIDs))
	if len(repoIDs) == 0 {
		return out, nil
	}
	var rows []*AscRepoChangesets
	if err := db.GetEngine(ctx).In("repo_id", repoIDs).Find(&rows); err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.RepoID] = r
	}
	return out, nil
}

// SetAscRepoChangesets stores the count of a repository at a head, replacing the count it had.
func SetAscRepoChangesets(ctx context.Context, repoID int64, commitSha string, changesets int64) error {
	return db.WithTx(ctx, func(ctx context.Context) error {
		e := db.GetEngine(ctx)
		row := &AscRepoChangesets{RepoID: repoID, CommitSha: commitSha, Changesets: changesets}
		n, err := e.ID(repoID).Cols("commit_sha", "changesets", "updated_unix").Update(row)
		if err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		_, err = e.Insert(row)
		return err
	})
}
