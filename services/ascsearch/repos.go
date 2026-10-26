// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"context"
	"errors"
	"slices"
	"strings"

	"forgejo.org/models/db"
	"forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/setting"
	"forgejo.org/services/authz"

	"xorm.io/builder"
)

// MaxRepos bounds the repositories of one search (the server's cross_search_max_repos for an instance).
const MaxRepos = 200

// MaxSemanticRepos bounds the repositories of a meaning search: each one costs a query embedding.
const MaxSemanticRepos = 16

// Repo is a repository the caller may read, in the names both sides use.
type Repo struct {
	ID int64
	// Name is the name asc knows the repository by, "owner/repo" in lower case.
	Name string
	// FullName is the name Forgejo shows, "Owner/Repo".
	FullName string
	// Branch is the default branch; Rev is "refs/heads/<branch>".
	Branch string
	Rev    string
	// Link is the repository page path ("/Owner/Repo", with the sub URL).
	Link    string
	Updated int64
}

// Selector says which repositories a search is about. Whatever it says, only repositories the caller may
// read and that live in asc are returned.
type Selector struct {
	// Repos are "owner/name" pairs. They take precedence over Owner.
	Repos []string
	// Owner restricts the search to the repositories of this user or organisation.
	Owner string
	// Max bounds the answer; zero means MaxRepos.
	Max int
}

// Resolved is the repositories a search covers.
type Resolved struct {
	Repos []Repo
	// Total counts the readable repositories before the cut at Max.
	Total int
	// Truncated says that Total is larger than len(Repos).
	Truncated bool
}

// ByName finds a repository by its asc name.
func (r *Resolved) ByName(name string) (Repo, bool) {
	for _, x := range r.Repos {
		if x.Name == name {
			return x, true
		}
	}
	return Repo{}, false
}

// Names lists the asc names.
func (r *Resolved) Names() []string {
	out := make([]string, len(r.Repos))
	for i, x := range r.Repos {
		out[i] = x.Name
	}
	return out
}

// ErrBadSelector is returned for a repository or owner name that is not a name.
var ErrBadSelector = errors.New("ascsearch: bad repository or owner name")

// NameOf derives the asc name of a Forgejo repository.
func NameOf(repo *repo_model.Repository) (string, error) {
	return ascbackend.RepoName(setting.RepoRootPath, repo_model.RepoPath(repo.OwnerName, repo.Name))
}

func repoOf(r *repo_model.Repository) (Repo, bool) {
	if r.GetStorageBackend() != string(gitrepo.StorageBackendAsc) || r.IsEmpty || r.DefaultBranch == "" {
		return Repo{}, false
	}
	name, err := NameOf(r)
	if err != nil {
		return Repo{}, false
	}
	return Repo{
		ID: r.ID, Name: name, FullName: r.FullName(), Branch: r.DefaultBranch,
		Rev: "refs/heads/" + r.DefaultBranch, Updated: int64(r.UpdatedUnix), Link: r.Link(),
	}, true
}

// canRead decides one repository the way the repository pages do: the permission of the doer, reduced by
// the access token when there is one.
func canRead(ctx context.Context, doer *user_model.User, reducer authz.AuthorizationReducer, repo *repo_model.Repository) bool {
	var p access_model.Permission
	var err error
	if reducer != nil {
		p, err = access_model.GetUserRepoPermissionWithReducer(ctx, repo, doer, reducer)
	} else {
		p, err = access_model.GetUserRepoPermission(ctx, repo, doer)
	}
	return err == nil && p.CanRead(unit.TypeCode)
}

// CanRead reports whether doer may read the code of the repository with the asc name, and returns it. A
// name that is not a name, a repository that does not exist and one the doer cannot read all answer the
// same: false. doer may be nil (anonymous) and reducer may be nil (no token limits).
func CanRead(ctx context.Context, doer *user_model.User, reducer authz.AuthorizationReducer, ascName string) (Repo, bool) {
	owner, name, ok := splitName(ascName)
	if !ok {
		return Repo{}, false
	}
	repo, err := repo_model.GetRepositoryByOwnerAndName(ctx, owner, name)
	if err != nil {
		return Repo{}, false
	}
	if !canRead(ctx, doer, reducer, repo) {
		return Repo{}, false
	}
	return repoOf(repo)
}

func splitName(s string) (owner, name string, ok bool) {
	owner, name, ok = strings.Cut(s, "/")
	if !ok || owner == "" || name == "" || len(s) > 255 || strings.Contains(name, "/") || !Plain(s, 255) {
		return "", "", false
	}
	return owner, name, true
}

// Resolve returns the repositories the search covers. It is the only place that asks Forgejo who may read
// what. The result is ordered by last update, newest first, and cut at sel.Max.
func Resolve(ctx context.Context, doer *user_model.User, reducer authz.AuthorizationReducer, sel Selector) (*Resolved, error) {
	limit := sel.Max
	if limit <= 0 || limit > MaxRepos {
		limit = MaxRepos
	}
	var found []Repo
	switch {
	case len(sel.Repos) > 0:
		if len(sel.Repos) > MaxRepos {
			return nil, ErrBadSelector
		}
		seen := map[string]bool{}
		for _, n := range sel.Repos {
			owner, name, ok := splitName(n)
			if !ok {
				return nil, ErrBadSelector
			}
			key := strings.ToLower(owner + "/" + name)
			if seen[key] {
				continue
			}
			seen[key] = true
			if r, ok := CanRead(ctx, doer, reducer, n); ok {
				found = append(found, r)
			}
		}
	default:
		var cond builder.Cond
		if sel.Owner != "" {
			if !Plain(sel.Owner, 255) || strings.Contains(sel.Owner, "/") {
				return nil, ErrBadSelector
			}
			owner, err := user_model.GetUserByName(ctx, sel.Owner)
			if err != nil {
				if user_model.IsErrUserNotExist(err) {
					return &Resolved{}, nil
				}
				return nil, err
			}
			cond = builder.And(builder.Eq{"owner_id": owner.ID}, repo_model.AccessibleRepositoryCondition(doer, unit.TypeCode))
		} else if doer != nil && doer.IsAdmin && reducer == nil {
			cond = builder.NewCond()
		} else {
			cond = repo_model.AccessibleRepositoryCondition(doer, unit.TypeCode)
		}
		ids, err := repo_model.SearchRepositoryIDsByCondition(ctx, cond)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return &Resolved{}, nil
		}
		repos := make(map[int64]*repo_model.Repository, len(ids))
		for chunk := range slices.Chunk(ids, 500) {
			if err := db.GetEngine(ctx).In("id", chunk).Find(&repos); err != nil {
				return nil, err
			}
		}
		for _, r := range repos {
			if reducer != nil {
				mode, err := reducer.ReduceRepoAccess(ctx, r, perm.AccessModeRead)
				if err != nil {
					return nil, err
				}
				if mode < perm.AccessModeRead {
					continue
				}
			}
			if x, ok := repoOf(r); ok {
				found = append(found, x)
			}
		}
	}
	slices.SortFunc(found, func(a, b Repo) int {
		switch {
		case a.Updated != b.Updated:
			if a.Updated > b.Updated {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	out := &Resolved{Repos: found, Total: len(found)}
	if len(found) > limit {
		out.Repos, out.Truncated = found[:limit], true
	}
	return out, nil
}
