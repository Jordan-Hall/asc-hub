// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package activities

import (
	"context"
	"sort"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/timeutil"
)

// AscContributorDays is how far back the contributor stack of the repository lists looks.
const AscContributorDays = 90

type ascContributorCount struct {
	RepoID    int64
	ActUserID int64
	N         int64
}

// AscTopContributors gives, for each of the repositories, the users that acted there since then, most actions first
// (the lower id first on a tie). An action is counted once: the
// hub keeps a copy of an action in the feed of each watcher, and the copy in the feed of the actor is the one counted.
// Agents act as their bot accounts, so they are in the stack like people. Deleted users are left out. A repository
// without actions in the window is not in the map.
func AscTopContributors(ctx context.Context, repoIDs []int64, since timeutil.TimeStamp) (map[int64][]*user_model.User, error) {
	out := make(map[int64][]*user_model.User, len(repoIDs))
	if len(repoIDs) == 0 {
		return out, nil
	}
	var counts []ascContributorCount
	if err := db.GetEngine(ctx).Table("action").
		Select("repo_id, act_user_id, COUNT(*) AS n").
		In("repo_id", repoIDs).
		Where("user_id = act_user_id").
		And("created_unix >= ?", since).
		GroupBy("repo_id, act_user_id").
		Find(&counts); err != nil {
		return nil, err
	}
	userIDs := make([]int64, 0, len(counts))
	seen := make(map[int64]bool, len(counts))
	for _, c := range counts {
		if !seen[c.ActUserID] {
			seen[c.ActUserID] = true
			userIDs = append(userIDs, c.ActUserID)
		}
	}
	users, err := user_model.GetUsersByIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*user_model.User, len(users))
	for _, u := range users {
		byID[u.ID] = u
	}
	perRepo := make(map[int64][]ascContributorCount, len(repoIDs))
	for _, c := range counts {
		if byID[c.ActUserID] != nil {
			perRepo[c.RepoID] = append(perRepo[c.RepoID], c)
		}
	}
	for repoID, cs := range perRepo {
		sort.Slice(cs, func(i, j int) bool {
			if cs[i].N != cs[j].N {
				return cs[i].N > cs[j].N
			}
			return cs[i].ActUserID < cs[j].ActUserID
		})
		list := make([]*user_model.User, 0, len(cs))
		for _, c := range cs {
			list = append(list, byID[c.ActUserID])
		}
		out[repoID] = list
	}
	return out, nil
}
