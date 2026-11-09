// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascagentdir is the directory of agents the hub draws on its people, profile and organization pages. An
// agent is a name that asc runs under, in the repositories of one owner: `@acme/review` is the agent `review` in the
// repositories of acme. The directory is read from the runs (asc `Agents.SummarizeAgents`) of the asc repositories
// the viewer can read, so it never names an agent of a repository the viewer may not see.
package ascagentdir

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"forgejo.org/models/db"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unit"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/cache"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/setting"
	"forgejo.org/services/ascagentui"
	"forgejo.org/services/asctrust"

	"google.golang.org/protobuf/proto"
)

const (
	// MaxRepos is the most repositories one directory is read from: the newest activity first.
	MaxRepos = 50
	// searchRepos is how many repositories are looked at to find MaxRepos asc repositories.
	searchRepos = 200
	ttl         = 60 // seconds
	timeout     = 3 * time.Second
	// MaxAgents is the most agents asked of asc for one directory.
	MaxAgents = 100
	// maxPeople is the most people an agent names.
	maxPeople = 5
)

// ErrUnavailable is returned when asc could not be asked.
var ErrUnavailable = errors.New("asc serve could not be asked")

// Agent is one agent of one owner.
type Agent struct {
	Name    string
	Owner   *user_model.User
	Runs    uint64
	Active  uint64
	LastRun time.Time
	// Repos are where it ran, most runs first.
	Repos  []*repo_model.Repository
	People []string
}

// Handle is how the agent is written: @owner/name.
func (a *Agent) Handle() string { return "@" + a.Owner.Name + "/" + a.Name }

// RunsLink is the agent runs page of the repository where it ran most, filtered to the agent.
func (a *Agent) RunsLink() string {
	return a.Repos[0].Link() + "/asc/agents?agent=" + url.QueryEscape(a.Name)
}

// Enabled reports whether the viewer gets agents at all: asc on, signed in and agent features on.
func Enabled(ctx context.Context, doer *user_model.User) bool {
	return setting.Asc.Enabled && doer != nil && ascagentui.FeaturesEnabled(ctx, doer)
}

// Repos returns the asc repositories whose code the doer can read, the newest activity first, at most MaxRepos.
// ownerID 0 is any owner; else the repositories of that owner and, when collaborations is set, also those the
// owner can reach.
func Repos(ctx context.Context, doer *user_model.User, ownerID int64, collaborations bool) ([]*repo_model.Repository, error) {
	opts := &repo_model.SearchRepoOptions{
		ListOptions: db.ListOptions{Page: 1, PageSize: searchRepos},
		Actor:       doer,
		OwnerID:     ownerID,
		Private:     true,
		Archived:    optional.Some(false),
		OrderBy:     "updated_unix DESC",
	}
	if ownerID != 0 && !collaborations {
		opts.Collaborate = optional.Some(false)
	}
	found, _, err := repo_model.SearchRepository(ctx, opts)
	if err != nil {
		return nil, err
	}
	out := make([]*repo_model.Repository, 0, MaxRepos)
	for _, r := range found {
		if len(out) >= MaxRepos {
			break
		}
		if r.IsEmpty || r.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
			continue
		}
		if err := r.LoadOwner(ctx); err != nil {
			return nil, err
		}
		p, err := access_model.GetUserRepoPermission(ctx, r, doer)
		if err != nil {
			return nil, err
		}
		if p.CanRead(unit.TypeCode) {
			out = append(out, r)
		}
	}
	return out, nil
}

// Summarize returns the agents that ran in the repositories (at most MaxRepos, each read by the caller), most runs
// first, then by handle. createdBy keeps the runs a person started or created. The answer of asc is kept ttl
// seconds per set of repositories and person.
func Summarize(ctx context.Context, repos []*repo_model.Repository, createdBy string) ([]*Agent, error) {
	if len(repos) > MaxRepos {
		repos = repos[:MaxRepos]
	}
	names := make([]string, 0, len(repos))
	named := make([]*repo_model.Repository, 0, len(repos))
	seen := map[string]bool{}
	for _, r := range repos {
		name, err := ascbackend.RepoName(setting.RepoRootPath, repo_model.RepoPath(r.OwnerName, r.Name))
		if err != nil || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
		named = append(named, r)
	}
	if len(names) == 0 {
		return nil, nil
	}
	resp, err := summarize(ctx, names, createdBy)
	if err != nil {
		return nil, err
	}
	return fold(named, resp), nil
}

func summarize(ctx context.Context, names []string, createdBy string) (*ascv1.SummarizeAgentsResponse, error) {
	sum := sha256.Sum256([]byte(createdBy + "\n" + strings.Join(names, "\n")))
	key := "AscAgentDir_" + hex.EncodeToString(sum[:16])
	c := cache.GetCache()
	if c != nil {
		if v, ok := c.Get(key).(string); ok {
			resp := &ascv1.SummarizeAgentsResponse{}
			if proto.Unmarshal([]byte(v), resp) == nil {
				return resp, nil
			}
		}
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return nil, errors.Join(ErrUnavailable, err)
	}
	callCtx, cancel := context.WithTimeout(ascclient.WithSystem(ctx), timeout)
	defer cancel()
	refs := make([]*ascv1.RepoRef, 0, len(names))
	for _, n := range names {
		refs = append(refs, &ascv1.RepoRef{Name: n})
	}
	resp, err := ascv1.NewAgentsClient(conn).SummarizeAgents(callCtx, &ascv1.SummarizeAgentsRequest{
		Repos:     refs,
		CreatedBy: createdBy,
		Limit:     MaxAgents,
	})
	if err != nil {
		return nil, errors.Join(ErrUnavailable, err)
	}
	if c != nil {
		if b, err := proto.Marshal(resp); err == nil {
			_ = c.Put(key, string(b), ttl)
		}
	}
	return resp, nil
}

// fold splits each agent of asc by the owner of the repositories where it ran.
func fold(repos []*repo_model.Repository, resp *ascv1.SummarizeAgentsResponse) []*Agent {
	type key struct {
		owner int64
		name  string
	}
	type place struct {
		repo *repo_model.Repository
		runs uint64
	}
	agents := map[key]*Agent{}
	places := map[key][]place{}
	for _, s := range resp.GetAgents() {
		name := s.GetAgent()
		if !ascagentui.ValidAgentName(name) {
			continue
		}
		for _, p := range s.GetPlaces() {
			if int(p.GetRepo()) >= len(repos) {
				continue
			}
			r := repos[p.GetRepo()]
			if r.Owner == nil {
				continue
			}
			k := key{r.OwnerID, name}
			a := agents[k]
			if a == nil {
				a = &Agent{Name: name, Owner: r.Owner}
				agents[k] = a
			}
			a.Runs += p.GetRuns()
			a.Active += p.GetActive()
			if t := time.Unix(p.GetLastRun(), 0); t.After(a.LastRun) {
				a.LastRun = t
			}
			for _, who := range p.GetPeople() {
				if len(a.People) < maxPeople && !slices.Contains(a.People, who) {
					a.People = append(a.People, who)
				}
			}
			places[k] = append(places[k], place{r, p.GetRuns()})
		}
	}
	out := make([]*Agent, 0, len(agents))
	for k, a := range agents {
		ps := places[k]
		sort.SliceStable(ps, func(i, j int) bool { return ps[i].runs > ps[j].runs })
		for _, p := range ps {
			a.Repos = append(a.Repos, p.repo)
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Runs != out[j].Runs {
			return out[i].Runs > out[j].Runs
		}
		return out[i].Handle() < out[j].Handle()
	})
	return out
}

// OfOwner keeps the agents of one owner.
func OfOwner(agents []*Agent, ownerID int64) []*Agent {
	var out []*Agent
	for _, a := range agents {
		if a.Owner.ID == ownerID {
			out = append(out, a)
		}
	}
	return out
}
