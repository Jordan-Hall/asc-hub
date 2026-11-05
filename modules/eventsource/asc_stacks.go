// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package eventsource

import (
	"context"
	"maps"
	"slices"

	issues_model "forgejo.org/models/issues"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/json"
	"forgejo.org/modules/log"
	"forgejo.org/modules/timeutil"
)

// ascStacksMaxRepos bounds the repositories one asc-stacks tick looks at.
const ascStacksMaxRepos = 50

// connectedUIDs returns the users with an open event stream.
func (m *Manager) connectedUIDs() []int64 {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	uids := make([]int64, 0, len(m.messengers))
	for uid := range m.messengers {
		uids = append(uids, uid)
	}
	return uids
}

// sendAscStacks tells each connected user which of the repositories they can read had a pull request updated in
// (from, to], so an open Stacks list reloads its rows (board Stacks: the status chip pulses once on change). Only
// repository IDs are sent and only to readers of the pull requests of that repository.
func (m *Manager) sendAscStacks(ctx context.Context, from, to timeutil.TimeStamp) {
	repoIDs, err := issues_model.PullRepoIDsUpdatedBetween(ctx, from, to, ascStacksMaxRepos)
	if err != nil {
		log.Error("asc-stacks: updated pull requests: %v", err)
		return
	}
	if len(repoIDs) == 0 {
		return
	}
	for uid, ids := range ascStackReaders(ctx, m.connectedUIDs(), repoIDs) {
		data, err := json.Marshal(ids)
		if err != nil {
			continue
		}
		m.SendMessage(uid, &Event{Name: "asc-stacks", Data: string(data)})
	}
}

// ascStackReaders maps each user to the repositories, of repoIDs, whose pull requests that user can read. A user
// who can read none is left out.
func ascStackReaders(ctx context.Context, uids, repoIDs []int64) map[int64][]int64 {
	out := map[int64][]int64{}
	if len(uids) == 0 || len(repoIDs) == 0 {
		return out
	}
	repos, err := repo_model.GetRepositoriesMapByIDs(ctx, repoIDs)
	if err != nil {
		log.Error("asc-stacks: repositories: %v", err)
		return out
	}
	users, err := user_model.GetUsersByIDs(ctx, uids)
	if err != nil {
		log.Error("asc-stacks: users: %v", err)
		return out
	}
	ids := slices.Sorted(maps.Keys(repos))
	for _, u := range users {
		if !u.IsActive || u.ProhibitLogin {
			continue
		}
		for _, id := range ids {
			perm, err := access_model.GetUserRepoPermission(ctx, repos[id], u)
			if err != nil {
				log.Error("asc-stacks: permission of %d on %d: %v", u.ID, id, err)
				continue
			}
			if perm.CanRead(unit.TypePullRequests) {
				out[u.ID] = append(out[u.ID], id)
			}
		}
	}
	return out
}
