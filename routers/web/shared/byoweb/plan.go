// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package byoweb

import (
	"context"

	quota_model "forgejo.org/models/quota"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	svc "forgejo.org/services/ascbyo"
)

// The plan line of the storage page (design board "RepoSettingsStorage"): a repository on the server's storage
// shows how much of its owner's plan is used, "38.2 GB of 100 GB on your plan", with "Use my own bucket". The plan
// is the hub's quota: the limit on repository data of the owner's quota groups. asc only says where the repository
// keeps its objects and how much it keeps; the hub records that as the repository's size, so the quota counts an
// asc repository like a git one.

// StoragePlan is the plan line.
type StoragePlan struct {
	Used, Limit int64
	Percent     int
}

// MeterClass is the colour of the plan's meter.
func (p *StoragePlan) MeterClass() string { return svc.MeterClass(p.Percent) }

// planOf is the plan line for owner, or nil when quotas are off, no rule covers repository data or there is no
// limit.
func planOf(ctx context.Context, ownerID int64) (*StoragePlan, error) {
	if !setting.Quota.Enabled {
		return nil, nil
	}
	groups, err := quota_model.GetGroupsForUser(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	used, err := quota_model.GetUsedForUser(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	a, ok := groups.AllowanceFor(*used, quota_model.LimitSubjectSizeGitAll)
	if !ok || a.Limit < 0 {
		return nil, nil
	}
	p := &StoragePlan{Used: a.Used, Limit: a.Limit, Percent: 100}
	if a.Limit > 0 {
		p.Percent = int(min(a.Used*100/a.Limit, 100))
	}
	return p, nil
}

// syncRepoSize records what asc counted as the size of repo: the bytes on the server's storage, large files apart.
// A repository on its own bucket keeps nothing on the server's storage once its objects have moved, so it counts
// as nothing; while objects are still in an earlier place the size is left as it is.
func syncRepoSize(ctx context.Context, repo *repo_model.Repository, u *svc.StorageUsage) {
	if repo == nil || u == nil || u.ComputedAt == 0 || u.HasFallback {
		return
	}
	var git, lfs int64
	if u.OnServer() {
		lfs = u.LargeFileBytes()
		git = u.TotalSize() - lfs
	}
	if repo.GitSize == git && repo.LFSSize == lfs {
		return
	}
	if err := repo_model.UpdateRepoSize(ctx, repo.ID, git, lfs); err != nil {
		log.Warn("asc byo: size of %s: %v", repo.FullName(), err)
		return
	}
	repo.GitSize, repo.LFSSize, repo.Size = git, lfs, git+lfs
}
