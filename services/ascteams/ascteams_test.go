// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascteams

import (
	"context"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"forgejo.org/models"
	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"
	_ "forgejo.org/modules/testimport"
	"forgejo.org/services/asctrust"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestMain(m *testing.M) { unittest.MainTest(m) }

// fakeAsc keeps teams as asc's PathRules service does and answers policies from a map.
type fakeAsc struct {
	ascv1.UnimplementedRepoServer
	ascv1.UnimplementedPathRulesServer
	ascv1.UnimplementedPolicyServer
	ascv1.UnimplementedOrgSettingsServer

	mu       sync.Mutex
	agents   []*ascv1.OrgAgent
	teams    map[string]map[string][]string // org -> team -> members
	sets     []string                       // "org/team" of every SetTeamMembers
	ops      []string
	policies map[string]*ascv1.GetPolicyResponse
	reads    int
	fail     string // "org/team" whose SetTeamMembers fails
}

func (f *fakeAsc) Version(context.Context, *ascv1.VersionRequest) (*ascv1.VersionResponse, error) {
	return &ascv1.VersionResponse{Features: []string{Feature, asctrust.FeaturePolicy, asctrust.FeatureOrgSettings}}, nil
}

func (f *fakeAsc) ListAgents(context.Context, *ascv1.ListOrgAgentsRequest) (*ascv1.ListOrgAgentsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &ascv1.ListOrgAgentsResponse{Agents: f.agents}, nil
}

func (f *fakeAsc) ListTeams(_ context.Context, req *ascv1.ListTeamsRequest) (*ascv1.ListTeamsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := &ascv1.ListTeamsResponse{}
	for name, members := range f.teams[req.GetOrg()] {
		out.Teams = append(out.Teams, &ascv1.Team{Name: name, Members: members})
	}
	return out, nil
}

func (f *fakeAsc) SetTeamMembers(ctx context.Context, req *ascv1.SetTeamMembersRequest) (*ascv1.SetTeamMembersResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	if v := md.Get("x-asc-op-id"); len(v) > 0 {
		f.ops = append(f.ops, v[0])
	}
	if f.fail == req.GetOrg()+"/"+req.GetTeam() {
		return nil, status.Error(codes.Unavailable, "down")
	}
	f.sets = append(f.sets, req.GetOrg()+"/"+req.GetTeam())
	if f.teams[req.GetOrg()] == nil {
		f.teams[req.GetOrg()] = map[string][]string{}
	}
	if len(req.GetMembers()) == 0 {
		delete(f.teams[req.GetOrg()], req.GetTeam())
	} else {
		f.teams[req.GetOrg()][req.GetTeam()] = slices.Clone(req.GetMembers())
	}
	return &ascv1.SetTeamMembersResponse{}, nil
}

func (f *fakeAsc) GetSource(_ context.Context, req *ascv1.GetPolicySourceRequest) (*ascv1.GetPolicySourceResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	p, ok := f.policies[req.GetRepo().GetName()]
	if !ok {
		return &ascv1.GetPolicySourceResponse{}, nil
	}
	return &ascv1.GetPolicySourceResponse{Present: true, Parsed: p}, nil
}

func (f *fakeAsc) members(org, team string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.teams[org][team])
}

func (f *fakeAsc) setCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sets)
}

func startFake(t *testing.T) *fakeAsc {
	t.Helper()
	f := &fakeAsc{teams: map[string]map[string][]string{}, policies: map[string]*ascv1.GetPolicyResponse{}}
	srv := grpc.NewServer()
	ascv1.RegisterRepoServer(srv, f)
	ascv1.RegisterPathRulesServer(srv, f)
	ascv1.RegisterPolicyServer(srv, f)
	ascv1.RegisterOrgSettingsServer(srv, f)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(srv.Stop)
	c, err := ascclient.New(ascclient.Config{Target: l.Addr().String(), InternalToken: "internal-token-for-tests", Retry: ascclient.RetryConfig{MaxAttempts: 1}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	t.Cleanup(asctrust.UseConn(c))
	return f
}

func TestReconcileCopiesTheTeamsOfAnOrganization(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	f := startFake(t)
	f.teams["org3"] = map[string][]string{
		"gone":  {"user9"},          // a team the organization no longer has
		"team1": {"user2", "user4"}, // already right
	}
	ctx := context.Background()

	res, err := Reconcile(ctx, 3)
	require.NoError(t, err)
	assert.Equal(t, Result{Set: 4, Cleared: 1}, res)
	assert.Equal(t, []string{"user2"}, f.members("org3", "owners"))
	assert.Equal(t, []string{"user2", "user4"}, f.members("org3", "team1"))
	assert.Equal(t, []string{"user15"}, f.members("org3", "test_team"))
	assert.Empty(t, f.members("org3", "gone"))
	assert.Len(t, f.ops, 5, "every change carries an operation id")

	// Nothing changed: nothing is sent.
	res, err = Reconcile(ctx, 3)
	require.NoError(t, err)
	assert.Equal(t, Result{}, res)
	assert.Equal(t, 5, f.setCount())

	// A person is not an organization.
	_, err = Reconcile(ctx, 2)
	require.Error(t, err)

	// With asc off nothing is asked.
	setting.Asc.Enabled = false
	_, err = Reconcile(ctx, 3)
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestReconcileGoesOnPastAFailingTeam(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	f := startFake(t)
	f.teams["org3"] = map[string][]string{"gone": {"user9"}}
	f.fail = "org3/team1"
	ctx := context.Background()
	_, err := db.GetEngine(ctx).Exec("UPDATE team SET name = ?, lower_name = ? WHERE id = 7", strings.Repeat("a", 129), strings.Repeat("a", 129))
	require.NoError(t, err)

	res, err := Reconcile(ctx, 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "org3/team1")
	assert.Equal(t, Result{Set: 3, Cleared: 1}, res, "the other teams are sent and the gone team is cleared")
	assert.Empty(t, f.members("org3", "gone"))
	assert.Equal(t, []string{"user2"}, f.members("org3", "owners"))
	assert.Equal(t, "org3/gone", f.sets[0], "removals go first")
	for _, s := range f.sets {
		assert.NotContains(t, s, "aaaa", "a name asc cannot take is not sent")
	}
}

func TestARenamedOrDeletedAccountMovesItsTeams(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	defer test.MockVariableValue(&Debounce, 20*time.Millisecond)()
	f := startFake(t)
	ctx := context.Background()
	require.NoError(t, Init(ctx))
	f.teams["oldorg"] = map[string][]string{"team1": {"user2"}, "owners": {"user2"}}
	f.teams["org3"] = map[string][]string{"team1": {"olduser4"}}

	org := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 3})
	Renamed(ctx, org, "OldOrg")
	Wait()
	assert.Empty(t, f.teams["oldorg"], "the old name keeps no teams")
	assert.Equal(t, []string{"user2", "user4"}, f.members("org3", "team1"), "the organization is sent under its name")

	f.teams["org3"]["team1"] = []string{"olduser4", "user2"}
	person := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	Renamed(ctx, person, "olduser4")
	Wait()
	assert.Equal(t, []string{"user2", "user4"}, f.members("org3", "team1"), "a person's teams are sent again")

	f.teams["deleted"] = map[string][]string{"team1": {"user2"}}
	OrgDeleted(ctx, "Deleted")
	Wait()
	assert.Empty(t, f.teams["deleted"])
	pending, err := organization.AscTeamForgets(ctx)
	require.NoError(t, err)
	assert.Empty(t, pending, "a cleared name is no longer recorded")

	n, err := Forget(ctx, "nobody")
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestAFailedForgetIsTriedAgainBySync(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	f := startFake(t)
	ctx := context.Background()
	require.NoError(t, Init(ctx))
	f.teams["oldorg"] = map[string][]string{"team1": {"user2"}}
	f.teams["org3"] = map[string][]string{"stale": {"user9"}}
	f.fail = "oldorg/team1"

	OrgDeleted(ctx, "OldOrg")
	Wait()
	assert.Equal(t, []string{"user2"}, f.members("oldorg", "team1"))
	// org3 is a name that belongs to an organization again: its own teams are sent, the stale one cleared.
	require.NoError(t, organization.AddAscTeamForget(ctx, "org3"))
	pending, err := organization.AscTeamForgets(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"oldorg", "org3"}, pending)

	f.fail = ""
	n, err := ForgetPending(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Empty(t, f.members("oldorg", "team1"))
	assert.Empty(t, f.members("org3", "stale"))
	assert.Equal(t, []string{"user2", "user4"}, f.members("org3", "team1"))
	pending, err = organization.AscTeamForgets(ctx)
	require.NoError(t, err)
	assert.Empty(t, pending)

	// ReconcileAll drains the records first.
	f.teams["gone2"] = map[string][]string{"t": {"user2"}}
	require.NoError(t, organization.AddAscTeamForget(ctx, "gone2"))
	_, _, err = ReconcileAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, f.members("gone2", "t"))
}

func TestAgentsAreTeamMembers(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	defer test.MockVariableValue(&Debounce, 20*time.Millisecond)()
	f := startFake(t)
	ctx := context.Background()
	require.NoError(t, Init(ctx))
	f.agents = []*ascv1.OrgAgent{
		{Name: "docs-bot", DisplayName: "Docs bot", Paths: []string{"docs/**"}},
		{Name: "fixer", State: ascv1.OrgAgentState_ORG_AGENT_STATE_PAUSED},
	}
	team := unittest.AssertExistsAndLoadBean(t, &organization.Team{ID: 1}) // org3 owners

	_, err := AddAgent(ctx, "org3", team, "stranger")
	require.ErrorIs(t, err, ErrUnknownAgent)
	added, err := AddAgent(ctx, "org3", team, "docs-bot")
	require.NoError(t, err)
	assert.True(t, added)
	added, err = AddAgent(ctx, "org3", team, "docs-bot")
	require.NoError(t, err)
	assert.False(t, added, "already a member")
	Wait()
	assert.Equal(t, []string{"agent:docs-bot", "user2"}, f.members("org3", "owners"))

	ta, err := AgentsOf(ctx, "org3", team)
	require.NoError(t, err)
	require.Len(t, ta.Members, 1)
	assert.Equal(t, "Docs bot", ta.Members[0].Label())
	assert.Equal(t, []string{"docs/**"}, ta.Members[0].Paths)
	require.Len(t, ta.Others, 1)
	assert.True(t, ta.Others[0].Paused)
	assert.Empty(t, ta.Gone)

	// An agent asc no longer has is not sent, and its membership goes on the next reconcile.
	_, err = AddAgent(ctx, "org3", team, "fixer")
	require.NoError(t, err)
	Wait()
	assert.Equal(t, []string{"agent:docs-bot", "agent:fixer", "user2"}, f.members("org3", "owners"))
	f.mu.Lock()
	f.agents = f.agents[:1]
	f.mu.Unlock()
	ta, err = AgentsOf(ctx, "org3", team)
	require.NoError(t, err)
	assert.Equal(t, []string{"fixer"}, ta.Gone)
	_, err = Reconcile(ctx, 3)
	require.NoError(t, err)
	assert.Equal(t, []string{"agent:docs-bot", "user2"}, f.members("org3", "owners"))
	names, err := organization.TeamAgents(ctx, team.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"docs-bot"}, names)

	removed, err := RemoveAgent(ctx, team, "docs-bot")
	require.NoError(t, err)
	assert.True(t, removed)
	Wait()
	assert.Equal(t, []string{"user2"}, f.members("org3", "owners"))

	// A deleted team takes its agents with it.
	other := unittest.AssertExistsAndLoadBean(t, &organization.Team{ID: 7})
	_, err = AddAgent(ctx, "org3", other, "docs-bot")
	require.NoError(t, err)
	require.NoError(t, models.DeleteTeam(ctx, other))
	unittest.AssertNotExistsBean(t, &organization.AscTeamAgent{TeamID: 7})
}

func TestReconcileAllCoversOrganizationsWithAscRepositories(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	f := startFake(t)
	ctx := context.Background()

	ids, err := OrgsWithAscRepos(ctx)
	require.NoError(t, err)
	assert.Empty(t, ids, "no fixture repository is stored in asc")

	_, err = db.GetEngine(ctx).Exec("UPDATE repository SET storage_backend = 'asc' WHERE id = 32")
	require.NoError(t, err)
	ids, err = OrgsWithAscRepos(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int64{3}, ids)

	total, done, err := ReconcileAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, done)
	assert.Equal(t, 5, total.Set)
	assert.Equal(t, []string{"user2", "user4"}, f.members("org3", "team1"))
}

func TestATeamChangeIsSentAfterItCommits(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	defer test.MockVariableValue(&Debounce, 20*time.Millisecond)()
	defer test.MockVariableValue(&models.TeamsChanged, models.TeamsChanged)()
	f := startFake(t)
	ctx := context.Background()
	require.NoError(t, Init(ctx))

	team := unittest.AssertExistsAndLoadBean(t, &organization.Team{ID: 7})
	require.NoError(t, models.AddTeamMember(ctx, team, 5))
	require.NoError(t, models.AddTeamMember(ctx, team, 4))
	Wait()
	assert.Equal(t, []string{"user15", "user4", "user5"}, f.members("org3", "test_team"))
	sent := f.setCount()

	require.NoError(t, models.RemoveTeamMember(ctx, team, 5))
	Wait()
	assert.Equal(t, []string{"user15", "user4"}, f.members("org3", "test_team"))

	// A team that fails to commit sends nothing.
	before := f.setCount()
	require.Error(t, models.NewTeam(ctx, &organization.Team{OrgID: 3, Name: "team1"}))
	Wait()
	assert.Equal(t, before, f.setCount())

	// A renamed team: the old name is cleared, the new one sent.
	team.Name = "renamed"
	team.LowerName = "renamed"
	require.NoError(t, models.UpdateTeam(ctx, team, false, false))
	Wait()
	assert.Empty(t, f.members("org3", "test_team"))
	assert.Equal(t, []string{"user15", "user4"}, f.members("org3", "renamed"))

	require.NoError(t, models.DeleteTeam(ctx, team))
	Wait()
	assert.Empty(t, f.members("org3", "renamed"))
	assert.Greater(t, f.setCount(), sent)
}

func TestOwnershipShowsWhatPoliciesGiveATeam(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	defer test.MockVariableValue(&setting.Asc.Enabled, true)()
	f := startFake(t)
	ForgetPolicies()
	t.Cleanup(ForgetPolicies)
	ctx := context.Background()
	_, err := db.GetEngine(ctx).Exec("UPDATE repository SET storage_backend = 'asc' WHERE id IN (3, 5, 32)")
	require.NoError(t, err)
	f.policies["org3/repo21"] = &ascv1.GetPolicyResponse{
		Roles: []*ascv1.PolicyRole{{Name: "maintainer", Members: []string{"user:user2", "team:Team1"}}},
		Paths: []*ascv1.PolicyPathRule{
			{Glob: "payments/**", Write: []string{"team:team1"}},
			{Glob: "docs/**", Write: []string{"*"}},
			{Glob: "infra/**", Write: []string{"role:maintainer"}},
		},
	}
	f.policies["org3/repo3"] = &ascv1.GetPolicyResponse{
		Paths: []*ascv1.PolicyPathRule{{Glob: "src/**", Write: []string{"team:team1"}}},
	}
	f.policies["org3/repo5"] = &ascv1.GetPolicyResponse{
		Paths: []*ascv1.PolicyPathRule{{Glob: "src/**", Write: []string{"team:owners"}}},
	}

	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	own, err := OwnershipOf(ctx, 3, "team1", owner)
	require.NoError(t, err)
	require.Len(t, own.Repos, 2, "repo5 names another team")
	assert.Equal(t, "repo21", own.Repos[0].Repo.Name)
	assert.Equal(t, []string{"maintainer"}, own.Repos[0].Roles)
	assert.Equal(t, []string{"payments/**", "infra/**"}, own.Repos[0].Globs, "`*` is not the team")
	assert.Equal(t, "repo3", own.Repos[1].Repo.Name)
	assert.Equal(t, []string{"src/**"}, own.Repos[1].Globs)
	assert.False(t, own.More)
	reads := f.reads

	// A viewer outside the organization sees the public repository only; the policies come from the cache.
	outsider := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})
	own, err = OwnershipOf(ctx, 3, "team1", outsider)
	require.NoError(t, err)
	require.Len(t, own.Repos, 1)
	assert.Equal(t, "repo21", own.Repos[0].Repo.Name)
	assert.Equal(t, reads, f.reads)

	// asc off: nothing.
	setting.Asc.Enabled = false
	own, err = OwnershipOf(ctx, 3, "team1", owner)
	require.NoError(t, err)
	assert.Empty(t, own.Repos)
}
