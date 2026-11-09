// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"context"
	"strings"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascbyo"
)

// RepoStorage is what the Storage column of the repositories page says for one repository: the name of the storage
// target its objects go to (empty for the server default), Unknown when asc could not be asked, and Warning, the
// locale key of what is wrong when where the objects are and where they should be disagree.
type RepoStorage struct {
	Name    string
	Unknown bool
	Moving  bool
	Warning string
}

// repoStorageOf is the column for one placement.
func repoStorageOf(p ascbyo.Placement) RepoStorage {
	s := RepoStorage{Name: p.TargetName, Moving: p.Migrating}
	switch {
	case p.Gone():
		s.Warning = "asc_admin.storage_target_gone"
	case p.Off():
		s.Warning = "asc_admin.storage_target_off"
	case p.Split():
		s.Warning = "asc_admin.storage_split"
	}
	return s
}

// loadRepoStorage asks asc, in one call per hundred repositories, where each repository keeps its objects. Without asc
// configured every repository uses the server default; when asc cannot answer, the column says it is not known.
func loadRepoStorage(ctx context.Context, repos []*repo_model.Repository) map[int64]RepoStorage {
	out := make(map[int64]RepoStorage, len(repos))
	if len(repos) == 0 {
		return out
	}
	unknown := func(asked bool) map[int64]RepoStorage {
		for _, r := range repos {
			out[r.ID] = RepoStorage{Unknown: asked || setting.Asc.Enabled}
		}
		return out
	}
	svc, err := ascbyo.New()
	if err != nil {
		return unknown(false)
	}
	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.FullName()
	}
	got, err := svc.RepoPlacements(ctx, names)
	if err != nil {
		return unknown(true)
	}
	for _, r := range repos {
		// A repository asc has no record of has never been given a target: the server default.
		out[r.ID] = repoStorageOf(got[strings.ToLower(r.FullName())])
	}
	return out
}
