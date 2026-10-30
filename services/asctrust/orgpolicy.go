// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"errors"
	"fmt"
	"strings"

	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/ascidentity"

	"google.golang.org/grpc"
)

// FeatureOrgSettings is the Version.features string of the organisation settings service.
const FeatureOrgSettings = "orgsettings.v1"

// UserCallOrgPolicy is UserCall for reading the merge defaults of the organisation that owns repo: the person administers the
// repository (the page that shows them is the repository's policy page), and asc is told they may read the organisation.
func UserCallOrgPolicy(ctx context.Context, doer *user_model.User, repo *repo_model.Repository) (context.Context, func(), error) {
	noop := func() {}
	if !usable(ctx, doer) {
		return ctx, noop, ErrNoDoer
	}
	if repo == nil {
		return ctx, noop, errors.New("asctrust: an organisation policy call needs a repository")
	}
	if err := repo.LoadOwner(ctx); err != nil {
		return ctx, noop, fmt.Errorf("asctrust: the owner of the repository: %w", err)
	}
	if !repo.Owner.IsOrganization() {
		return ctx, noop, ErrForbidden
	}
	p, err := access_model.GetUserRepoPermission(ctx, repo, doer)
	if err != nil {
		return ctx, noop, fmt.Errorf("asctrust: the permission of the person: %w", err)
	}
	if !p.IsAdmin() && !doer.IsAdmin {
		return ctx, noop, ErrForbidden
	}
	return claim(ctx, doer, ascidentity.RoleRead, "org:"+strings.ToLower(repo.Owner.Name))
}

// OrgPolicyView is what the organisation of a repository requires of it, in sentences.
type OrgPolicyView struct {
	Org       string
	Set       bool     // the organisation has set merge defaults
	Required  []string // what the organisation requires
	Effective []string // what the repository must meet after its own file is added to that
	Tightened []string // what the organisation made stricter than the repository's own file
}

// OrgPolicyFor asks asc for the merge defaults of the organisation that owns repo, resolved with what the repository's own policy
// says (repoValues from its parsed file; nil when it has none). callCtx is the context from UserCallOrgPolicy.
func OrgPolicyFor(callCtx context.Context, conn grpc.ClientConnInterface, org, repo string, repoValues *ascv1.MergeDefaults) (*OrgPolicyView, error) {
	resp, err := ascv1.NewOrgSettingsClient(conn).ResolveMergePolicy(callCtx, &ascv1.ResolveMergePolicyRequest{Org: org, Repo: repo, RepoValues: repoValues})
	if err != nil {
		return nil, err
	}
	return BuildOrgPolicyView(org, resp), nil
}

// BuildOrgPolicyView turns the answer of ResolveMergePolicy into sentences. Every name that came from the server is cleaned.
func BuildOrgPolicyView(org string, r *ascv1.ResolveMergePolicyResponse) *OrgPolicyView {
	v := &OrgPolicyView{Org: CleanText(org, 64)}
	if r == nil {
		return v
	}
	v.Set = r.GetOrg().GetSet()
	if v.Set {
		v.Required = mergeDefaultsSentences(r.GetOrg())
	}
	v.Effective = mergeDefaultsSentences(r.GetEffective())
	names := map[string]string{
		"require_approvals": "the number of approvals", "require_checks": "the required checks", "allow_force": "force-pushing trunk",
		"require_signed": "signed changes", "human_approval": "approval by a person", "author_may_approve": "the author approving their own change",
		"queue": "landing through the merge queue", "revalidate": "revalidating before a land", "strategy": "the merge strategy",
	}
	for _, f := range r.GetTightened() {
		if n, ok := names[f]; ok {
			v.Tightened = append(v.Tightened, n)
		}
	}
	return v
}

func mergeDefaultsSentences(m *ascv1.MergeDefaults) []string {
	if m == nil {
		return nil
	}
	var out []string
	switch n := int(m.GetRequireApprovals()); {
	case n == 1:
		out = append(out, "1 approval")
	case n > 1:
		out = append(out, fmt.Sprintf("%d approvals", n))
	}
	if c := cleanAll(m.GetRequireChecks()); len(c) > 0 {
		out = append(out, "green "+joinWords(c)+" "+noun(len(c), "check", "checks"))
	}
	if m.GetRequireSigned() {
		out = append(out, "signed changes")
	}
	if m.GetHumanApproval() {
		out = append(out, "approval by a person")
	}
	if m.GetQueue() {
		out = append(out, "landing through the merge queue")
	}
	if m.GetRevalidate() {
		out = append(out, "revalidating before a land")
	}
	if !m.GetAuthorMayApprove() {
		out = append(out, "no approving your own change")
	}
	if m.GetAllowForce() {
		out = append(out, "force-pushing trunk allowed")
	}
	if s := CleanText(m.GetStrategy(), 32); s != "" {
		out = append(out, "merge strategy "+s)
	}
	return out
}

// MergeDefaultsOf reads the merge requirements a repository's own policy states, for the resolve call.
func MergeDefaultsOf(parsed *ascv1.GetPolicyResponse) *ascv1.MergeDefaults {
	if parsed == nil {
		return nil
	}
	t := parsed.GetTrunk()
	return &ascv1.MergeDefaults{RequireApprovals: t.GetRequireApprovals(), RequireChecks: t.GetRequireChecks(), AllowForce: t.GetAllowForce()}
}
