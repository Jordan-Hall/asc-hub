// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
	"context"

	repo_model "forgejo.org/models/repo"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// Branchless repository settings (SPEC section 21.5.2).
//
// asc is the owner of the settings (repos.branchless, repos.cloud, repos.shared_refs) and the only place that
// changes them: `asc serve admin branchless enable <repo>` moves a repository to branchless mode, and the
// server validates the shared_refs it stores. Forgejo reads them through RepoConfig and shows them; it keeps
// no copy that could disagree with asc and offers no way to write them. The page is
// routers/web/repo/setting/asc_branchless.go.

// BranchlessSettings is the per-repository policy asc stores.
type BranchlessSettings struct {
	Branchless bool
	Cloud      bool
	SharedRefs []string
}

// LoadBranchlessSettings reads the settings from asc (RepoConfig).
func LoadBranchlessSettings(ctx context.Context, repo *repo_model.Repository) (BranchlessSettings, error) {
	r, err := open(repo)
	if err != nil {
		return BranchlessSettings{}, err
	}
	ctx, cancel := system(ctx, CallTimeout)
	defer cancel()
	resp, err := r.api.RepoConfig(ctx, &ascv1.RepoConfigRequest{Repo: &ascv1.RepoRef{Name: r.name}})
	if err != nil {
		return BranchlessSettings{}, callError("RepoConfig", err)
	}
	return BranchlessSettings{Branchless: resp.GetBranchless(), Cloud: resp.GetCloud(), SharedRefs: resp.GetSharedRefs()}, nil
}

// SetBranchless switches the ref policy of an asc repository between branchless and branches (RepoAdmin.set_branchless:
// repository administrator, audited by asc as repo.branchless). asc flips the policy only when no ref would be stranded
// (see its refusal text); the hub does not decide that. The call is made with the hub's own credential: the caller has
// decided that the person may do this (the creator of a new repository, an administrator of the repository).
func SetBranchless(ctx context.Context, repo *repo_model.Repository, on bool) error {
	r, err := open(repo)
	if err != nil {
		return err
	}
	ctx, cancel := system(ctx, CallTimeout)
	defer cancel()
	_, err = r.api.RepoAdmin(ctx, &ascv1.RepoAdminRequest{
		Repo: &ascv1.RepoRef{Name: r.name},
		Op:   &ascv1.RepoAdminRequest_SetBranchless{SetBranchless: &ascv1.SetBranchlessOp{Branchless: on}},
	})
	return callError("RepoAdmin.set_branchless", err)
}
