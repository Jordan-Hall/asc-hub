// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"context"
	"sort"

	"forgejo.org/models/db"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/timeutil"
)

// AscTrendingRepo is the weekly score of one public repository, kept by a cron task. Only repositories that were
// public when the score was made are stored; when the list is read it is filtered again by what the viewer may see.
type AscTrendingRepo struct {
	RepoID       int64              `xorm:"pk"`
	Score        int64              `xorm:"INDEX NOT NULL DEFAULT 0"`
	Stars        int64              `xorm:"NOT NULL DEFAULT 0"`
	Forks        int64              `xorm:"NOT NULL DEFAULT 0"`
	Activity     int64              `xorm:"NOT NULL DEFAULT 0"`
	ComputedUnix timeutil.TimeStamp `xorm:"NOT NULL DEFAULT 0"`
}

func init() {
	db.RegisterModel(new(AscTrendingRepo))
}

// TableName is the table of the model.
func (AscTrendingRepo) TableName() string { return "asc_trending_repo" }

const (
	ascTrendingKeep   = 50
	ascTrendingWindow = 7 * 86400
)

// RefreshAscTrending computes the score of the last seven days from stars (3 points each), forks (2) and activity of
// the repository (1 per action), and keeps the best fifty public repositories.
func RefreshAscTrending(ctx context.Context) error {
	now := timeutil.TimeStampNow()
	since := now.Add(-ascTrendingWindow)
	type counts struct{ Stars, Forks, Activity int64 }
	byRepo := map[int64]*counts{}
	get := func(id int64) *counts {
		if byRepo[id] == nil {
			byRepo[id] = &counts{}
		}
		return byRepo[id]
	}
	type row struct {
		RepoID int64
		N      int64
	}
	var rows []row
	e := db.GetEngine(ctx)
	if err := e.Table("star").Select("repo_id, count(*) AS n").Where("created_unix > ?", since).GroupBy("repo_id").Find(&rows); err != nil {
		return err
	}
	for _, r := range rows {
		get(r.RepoID).Stars = r.N
	}
	rows = nil
	if err := e.Table("repository").Select("fork_id AS repo_id, count(*) AS n").Where("fork_id > 0 AND created_unix > ?", since).GroupBy("fork_id").Find(&rows); err != nil {
		return err
	}
	for _, r := range rows {
		get(r.RepoID).Forks = r.N
	}
	rows = nil
	if err := e.Table("action").Select("repo_id, count(*) AS n").Where("created_unix > ? AND is_private = ?", since, false).GroupBy("repo_id").Find(&rows); err != nil {
		return err
	}
	for _, r := range rows {
		get(r.RepoID).Activity = r.N
	}

	// only public repositories of public owners are scored
	var scored []*AscTrendingRepo
	for id, c := range byRepo {
		repo, err := GetRepositoryByID(ctx, id)
		if err != nil || repo.IsPrivate || repo.IsEmpty {
			continue
		}
		if err := repo.LoadOwner(ctx); err != nil || repo.Owner.Visibility != 0 {
			continue
		}
		score := c.Stars*3 + c.Forks*2 + c.Activity
		if score > 0 {
			scored = append(scored, &AscTrendingRepo{RepoID: id, Score: score, Stars: c.Stars, Forks: c.Forks, Activity: c.Activity, ComputedUnix: now})
		}
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].Score != scored[j].Score {
			return scored[i].Score > scored[j].Score
		}
		return scored[i].RepoID < scored[j].RepoID
	})
	if len(scored) > ascTrendingKeep {
		scored = scored[:ascTrendingKeep]
	}
	return db.WithTx(ctx, func(ctx context.Context) error {
		if _, err := db.GetEngine(ctx).Where("1 = 1").Delete(new(AscTrendingRepo)); err != nil {
			return err
		}
		for _, s := range scored {
			if _, err := db.GetEngine(ctx).Insert(s); err != nil {
				return err
			}
		}
		return nil
	})
}

// ListAscTrending returns the best n stored repositories the viewer may read (a nil viewer is a visitor).
func ListAscTrending(ctx context.Context, viewer *user_model.User, n int) ([]*AscTrendingRepo, []*Repository, error) {
	var rows []*AscTrendingRepo
	err := db.GetEngine(ctx).Table("asc_trending_repo").Select("`asc_trending_repo`.*").
		Join("INNER", "repository", "`repository`.id = `asc_trending_repo`.repo_id").
		Where(AccessibleRepositoryCondition(viewer, unit.TypeInvalid)).
		OrderBy("`asc_trending_repo`.score DESC, `asc_trending_repo`.repo_id ASC").Limit(n).Find(&rows)
	if err != nil {
		return nil, nil, err
	}
	repos := make([]*Repository, 0, len(rows))
	kept := rows[:0]
	for _, r := range rows {
		repo, err := GetRepositoryByID(ctx, r.RepoID)
		if err != nil {
			continue
		}
		repos = append(repos, repo)
		kept = append(kept, r)
	}
	return kept, repos, nil
}
