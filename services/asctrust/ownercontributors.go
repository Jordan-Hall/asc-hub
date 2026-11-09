// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"fmt"
	"strings"

	"forgejo.org/models/organization"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascidentity"
)

// FeatureOwnerContributors is the Version.features string of Insights.ListOwnerContributors: the contributors of several
// repositories of one owner, summed by person, in one call.
const FeatureOwnerContributors = "insights-owner.v1"

// MaxOwnerRepos is the most repositories one ListOwnerContributors call may name.
const MaxOwnerRepos = 64

// UserCallOrgRepos is UserCall for a read over several repositories of an organisation. Of repos it keeps those of the
// organisation the person may read the code of right now (the decision CallRepoRead makes for one repository, made
// again here) and returns their asc names with a context carrying a read claim for the organisation. asc counts exactly
// the repositories it is told, so a repository is named only when the person may read it. ErrForbidden when the person
// may not see the organisation or may read none of the repositories: no claim is signed.
func UserCallOrgRepos(ctx context.Context, doer, org *user_model.User, repos []*repo_model.Repository) (context.Context, func(), []string, error) {
	noop := func() {}
	if !usable(ctx, doer) {
		return ctx, noop, nil, ErrNoDoer
	}
	if org == nil || !org.IsOrganization() {
		return ctx, noop, nil, fmt.Errorf("asctrust: an organisation call needs an organisation")
	}
	if !organization.HasOrgOrUserVisible(ctx, org, doer) {
		return ctx, noop, nil, ErrForbidden
	}
	names := make([]string, 0, len(repos))
	for _, repo := range repos {
		if len(names) == MaxOwnerRepos {
			break
		}
		if repo == nil || repo.OwnerID != org.ID || repo.IsBeingCreated() || repo.IsBroken() {
			continue
		}
		perm, err := access_model.GetUserRepoPermission(ctx, repo, doer)
		if err != nil {
			return ctx, noop, nil, fmt.Errorf("asctrust: the permission of the person: %w", err)
		}
		if !perm.CanRead(unit.TypeCode) {
			continue
		}
		name, err := RepoName(repo)
		if err != nil {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return ctx, noop, nil, ErrForbidden
	}
	callCtx, release, err := claim(ctx, doer, ascidentity.RoleRead, "org:"+strings.ToLower(org.Name))
	if err != nil {
		return ctx, noop, nil, err
	}
	return callCtx, release, names, nil
}

// OwnerLanded asks asc for the changes each person landed in the last 366 days over the named repositories of org, in one
// call, and returns them by lower-case login.
func OwnerLanded(ctx context.Context, client ascv1.InsightsClient, org string, names []string) (map[string]int, error) {
	resp, err := client.ListOwnerContributors(ctx, &ascv1.ListOwnerContributorsRequest{
		Owner: strings.ToLower(org),
		Repos: names,
		Days:  366,
		Kind:  ascv1.AuthorKind_AUTHOR_KIND_PERSON,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]int, len(resp.GetContributors()))
	for _, c := range resp.GetContributors() {
		out[strings.ToLower(c.GetName())] += int(c.GetLanded())
	}
	return out, nil
}
