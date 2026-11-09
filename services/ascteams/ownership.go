// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascteams

import (
	"context"
	"sync"
	"time"

	"forgejo.org/models/db"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
)

// What the policies of an organization's asc repositories give one of its teams, for the Policy owner part of the
// team page. A policy is the repository's `.asc/policy.toml` as asc parsed it (Policy.GetSource); the hub reads it as
// itself and shows a repository only to a viewer who may read its code, the same rule as the policy file.

const (
	// MaxOwnershipRepos bounds the repositories whose policy is read for one page.
	MaxOwnershipRepos = 50
	ownershipTTL      = time.Minute
)

// RepoOwnership is what the policy of one repository gives the team.
type RepoOwnership struct {
	Repo  *repo_model.Repository
	Roles []string
	Globs []string
}

// Ownership is the Policy owner part of a team page.
type Ownership struct {
	Repos []RepoOwnership
	// Unread counts the repositories the viewer may read whose policy could not be read.
	Unread int
	// More is true when the organization has more asc repositories than were looked at.
	More bool
}

type cachedPolicy struct {
	at     time.Time
	parsed *ascv1.GetPolicyResponse // nil: no usable policy
	err    error
}

var (
	policyMu    sync.Mutex
	policyCache = map[string]cachedPolicy{}
	now         = time.Now
)

// readPolicy returns the parsed policy of the asc repository name, kept for a minute; nil when it has none or the
// file does not parse.
func readPolicy(ctx context.Context, name string) (*ascv1.GetPolicyResponse, error) {
	policyMu.Lock()
	if c, ok := policyCache[name]; ok && now().Sub(c.at) < ownershipTTL {
		policyMu.Unlock()
		return c.parsed, c.err
	}
	policyMu.Unlock()
	conn, err := asctrust.Conn()
	if err != nil {
		return nil, err
	}
	sctx, cancel := asctrust.SystemContext(ctx, asctrust.CallTimeout)
	defer cancel()
	src, err := ascv1.NewPolicyClient(conn).GetSource(sctx, &ascv1.GetPolicySourceRequest{Repo: asctrust.RepoRef(name)})
	var parsed *ascv1.GetPolicyResponse
	if err == nil && src.GetPresent() && src.GetParsed().GetError() == "" {
		parsed = src.GetParsed()
	}
	policyMu.Lock()
	if len(policyCache) > 4096 {
		clear(policyCache)
	}
	policyCache[name] = cachedPolicy{at: now(), parsed: parsed, err: err}
	policyMu.Unlock()
	return parsed, err
}

// ForgetPolicies empties the policy cache. It is for tests.
func ForgetPolicies() {
	policyMu.Lock()
	clear(policyCache)
	policyMu.Unlock()
}

// OwnershipOf reads what the policies of the organization orgID's asc repositories that viewer may read give the
// team named team (its lower-case name). Repositories whose policy gives the team nothing are left out.
func OwnershipOf(ctx context.Context, orgID int64, team string, viewer *user_model.User) (*Ownership, error) {
	out := &Ownership{}
	if !setting.Asc.Enabled || !asctrust.HasFeature(ctx, asctrust.FeaturePolicy) {
		return out, nil
	}
	repos := make([]*repo_model.Repository, 0, MaxOwnershipRepos+1)
	err := db.GetEngine(ctx).Where("owner_id = ? AND storage_backend = ?", orgID, "asc").
		OrderBy("lower_name").Limit(MaxOwnershipRepos + 1).Find(&repos)
	if err != nil {
		return nil, err
	}
	if len(repos) > MaxOwnershipRepos {
		out.More = true
		repos = repos[:MaxOwnershipRepos]
	}
	for _, repo := range repos {
		perm, err := access_model.GetUserRepoPermission(ctx, repo, viewer)
		if err != nil {
			return nil, err
		}
		if !perm.CanRead(unit.TypeCode) {
			continue
		}
		name, err := asctrust.RepoName(repo)
		if err != nil {
			out.Unread++
			continue
		}
		parsed, err := readPolicy(ctx, name)
		if err != nil {
			out.Unread++
			continue
		}
		if parsed == nil {
			continue
		}
		own := asctrust.RulesOf(parsed).OwnershipOf(team)
		if len(own.Roles) == 0 && len(own.Globs) == 0 {
			continue
		}
		out.Repos = append(out.Repos, RepoOwnership{Repo: repo, Roles: own.Roles, Globs: own.Globs})
	}
	return out, nil
}
