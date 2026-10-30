// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"google.golang.org/grpc"
)

// The OrgSettings service of asc serve: what agents may do in an organisation, the merge requirements its repositories
// inherit, the agent identities of the organisation, and the switch and default budget of the whole server. Reading is for
// members and changing for owners; the hub signs a claim with the role of an owner (UserCallOrg) and asc decides again.
// Everything a form sends is checked here first, with the same limits asc applies, so a mistake is a sentence on the
// page and not a refusal from the server; everything that comes back is cleaned.

// FeatureOrgSettings is what Version.features says when the server has the OrgSettings service.
const FeatureOrgSettings = "orgsettings.v1"

// Limits of asc (crates/asc-serve/src/org).
const (
	MaxOrgAgents    = 100
	MaxAgentPaths   = 64
	MaxAgentRepos   = 64
	MaxMergeChecks  = 32
	MaxCheckBytes   = 100
	MaxPathBytes    = 256
	MaxDisplayBytes = 100
	MaxApprovals    = 20
)

var (
	agentNameShape = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	repoNameShape  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}/[A-Za-z0-9._-]{1,100}$`)

	// MergeStrategies are the values a strategy takes; "" leaves the choice to the repository.
	MergeStrategies = []string{"", "rebase", "rebase-disjoint", "fast-forward"}
)

// ------------------------------------------------------------------------------------------------ settings

// MergeDefaults are the merge requirements of an organisation.
type MergeDefaults struct {
	RequireApprovals uint32
	RequireChecks    []string
	AllowForce       bool
	RequireSigned    bool
	HumanApproval    bool
	AuthorMayApprove bool
	Queue            bool
	Revalidate       bool
	Strategy         string
	Set              bool
}

// ChecksText is the checks, one to a line, for a text area.
func (m MergeDefaults) ChecksText() string { return strings.Join(m.RequireChecks, "\n") }

func mergeOf(m *ascv1.MergeDefaults) MergeDefaults {
	out := MergeDefaults{
		RequireApprovals: m.GetRequireApprovals(), AllowForce: m.GetAllowForce(), RequireSigned: m.GetRequireSigned(),
		HumanApproval: m.GetHumanApproval(), AuthorMayApprove: m.GetAuthorMayApprove(), Queue: m.GetQueue(),
		Revalidate: m.GetRevalidate(), Strategy: m.GetStrategy(), Set: m.GetSet(),
	}
	for _, c := range m.GetRequireChecks() {
		if c = CleanText(c, MaxCheckBytes); c != "" {
			out.RequireChecks = append(out.RequireChecks, c)
		}
	}
	return out
}

func (m MergeDefaults) wire() *ascv1.MergeDefaults {
	return &ascv1.MergeDefaults{
		RequireApprovals: m.RequireApprovals, RequireChecks: m.RequireChecks, AllowForce: m.AllowForce, RequireSigned: m.RequireSigned,
		HumanApproval: m.HumanApproval, AuthorMayApprove: m.AuthorMayApprove, Queue: m.Queue, Revalidate: m.Revalidate, Strategy: m.Strategy,
	}
}

// OrgSettings is the settings of an organisation.
type OrgSettings struct {
	AllowAgents      bool
	NeedPerson       bool
	CeilingMicro     uint64
	Merge            MergeDefaults
	AgentCount       uint32
	Updated          time.Time
	UpdatedBy        string
	InstanceAllows   bool
	InstanceBudget   uint64
	EffectiveCeiling uint64
}

// CeilingField, InstanceBudgetText and EffectiveCeilingText write dollars for a form field and for the page.
func (s *OrgSettings) CeilingField() string         { return USDField(s.CeilingMicro) }
func (s *OrgSettings) InstanceBudgetText() string   { return optionalUSD(s.InstanceBudget) }
func (s *OrgSettings) EffectiveCeilingText() string { return optionalUSD(s.EffectiveCeiling) }

func optionalUSD(micro uint64) string {
	if micro == 0 {
		return ""
	}
	return FormatUSD(micro)
}

func orgSettingsOf(r *ascv1.GetOrgSettingsResponse) *OrgSettings {
	return &OrgSettings{
		AllowAgents: r.GetAgents().GetAllowAgents(), NeedPerson: r.GetAgents().GetAgentChangesetsNeedPerson(),
		CeilingMicro: r.GetAgents().GetMonthlySpendCeilingMicroUsd(), Merge: mergeOf(r.GetMerge()), AgentCount: r.GetAgentCount(),
		Updated: unixTime(r.GetUpdated()), UpdatedBy: CleanText(r.GetUpdatedBy(), MaxTextBytes), InstanceAllows: r.GetInstanceAllowsAgents(),
		InstanceBudget: r.GetInstanceDefaultBudgetMicroUsd(), EffectiveCeiling: r.GetEffectiveSpendCeilingMicroUsd(),
	}
}

func orgName(org string) (string, error) {
	if !ownerName.MatchString(org) {
		return "", fmt.Errorf("%w: organization", ErrBadInput)
	}
	return strings.ToLower(org), nil
}

// LoadOrgSettings asks for the settings of org.
func LoadOrgSettings(ctx context.Context, conn grpc.ClientConnInterface, org string) (*OrgSettings, error) {
	org, err := orgName(org)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewOrgSettingsClient(conn).GetOrgSettings(cctx, &ascv1.GetOrgSettingsRequest{Org: org})
	if err != nil {
		return nil, err
	}
	return orgSettingsOf(resp), nil
}

// AgentPolicy is the agents card of an organisation.
type AgentPolicy struct {
	AllowAgents  bool
	NeedPerson   bool
	CeilingMicro uint64
}

// AgentPolicyFromForm reads the fields allow_agents and need_person ("1" is on) and ceiling_usd.
func AgentPolicyFromForm(get func(string) string) (AgentPolicy, error) {
	c, err := ParseUSD(get("ceiling_usd"))
	if err != nil {
		return AgentPolicy{}, err
	}
	return AgentPolicy{AllowAgents: get("allow_agents") == "1", NeedPerson: get("need_person") == "1", CeilingMicro: c}, nil
}

// SetAgentPolicy sets the agents card of org.
func SetAgentPolicy(ctx context.Context, conn grpc.ClientConnInterface, org string, p AgentPolicy, op string) (*OrgSettings, error) {
	org, err := orgName(org)
	if err != nil {
		return nil, err
	}
	cctx, cancel, err := opCtx(ctx, op)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := ascv1.NewOrgSettingsClient(conn).SetAgentPolicy(cctx, &ascv1.SetOrgAgentPolicyRequest{
		Org: org, Agents: &ascv1.OrgAgentPolicy{AllowAgents: p.AllowAgents, AgentChangesetsNeedPerson: p.NeedPerson, MonthlySpendCeilingMicroUsd: p.CeilingMicro},
	})
	if err != nil {
		return nil, err
	}
	return orgSettingsOf(resp.GetSettings()), nil
}

// Lines splits a text area into trimmed, non-empty, de-duplicated lines. More than max lines, or a line longer than
// maxBytes or with a control character, is a mistake of the form.
func Lines(text string, max, maxBytes int) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || seen[l] {
			continue
		}
		if len(l) > maxBytes || CleanText(l, maxBytes) != l {
			return nil, fmt.Errorf("%w: line", ErrBadInput)
		}
		seen[l] = true
		out = append(out, l)
		if len(out) > max {
			return nil, fmt.Errorf("%w: too many lines", ErrBadInput)
		}
	}
	return out, nil
}

// MergeDefaultsFromForm reads the merge defaults of a form: require_approvals, require_checks (a line each), strategy and
// the switches allow_force, require_signed, human_approval, author_may_approve, queue and revalidate ("1" is on).
func MergeDefaultsFromForm(get func(string) string) (MergeDefaults, error) {
	n, err := ParseCount(get("require_approvals"), MaxApprovals)
	if err != nil {
		return MergeDefaults{}, err
	}
	checks, err := Lines(get("require_checks"), MaxMergeChecks, MaxCheckBytes)
	if err != nil {
		return MergeDefaults{}, err
	}
	strategy := get("strategy")
	ok := false
	for _, s := range MergeStrategies {
		ok = ok || s == strategy
	}
	if !ok {
		return MergeDefaults{}, fmt.Errorf("%w: strategy", ErrBadInput)
	}
	on := func(k string) bool { return get(k) == "1" }
	return MergeDefaults{
		RequireApprovals: uint32(n), RequireChecks: checks, AllowForce: on("allow_force"), RequireSigned: on("require_signed"),
		HumanApproval: on("human_approval"), AuthorMayApprove: on("author_may_approve"), Queue: on("queue"), Revalidate: on("revalidate"),
		Strategy: strategy,
	}, nil
}

// SetMergeDefaults replaces the merge defaults of org.
func SetMergeDefaults(ctx context.Context, conn grpc.ClientConnInterface, org string, m MergeDefaults, op string) (*OrgSettings, error) {
	org, err := orgName(org)
	if err != nil {
		return nil, err
	}
	cctx, cancel, err := opCtx(ctx, op)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := ascv1.NewOrgSettingsClient(conn).SetMergeDefaults(cctx, &ascv1.SetOrgMergeDefaultsRequest{Org: org, Merge: m.wire()})
	if err != nil {
		return nil, err
	}
	return orgSettingsOf(resp.GetSettings()), nil
}

// ------------------------------------------------------------------------------------------------ agents

// OrgAgent is an agent identity of an organisation.
type OrgAgent struct {
	Name        string
	DisplayName string
	RunBy       string
	Paths       []string
	Repos       []string
	Paused      bool
	Created     time.Time
	CreatedBy   string
	Updated     time.Time
}

// PathsText and ReposText are the lists, one to a line, for a text area.
func (a OrgAgent) PathsText() string { return strings.Join(a.Paths, "\n") }
func (a OrgAgent) ReposText() string { return strings.Join(a.Repos, "\n") }

// Label is the display name, or the name.
func (a OrgAgent) Label() string {
	if a.DisplayName != "" {
		return a.DisplayName
	}
	return a.Name
}

func agentOf(a *ascv1.OrgAgent) (OrgAgent, error) {
	if !agentNameShape.MatchString(a.GetName()) || len(a.GetPaths()) > MaxAgentPaths || len(a.GetRepos()) > MaxAgentRepos {
		return OrgAgent{}, fmt.Errorf("%w: agent", ErrBadAnswer)
	}
	out := OrgAgent{
		Name: a.GetName(), DisplayName: CleanText(a.GetDisplayName(), MaxDisplayBytes), RunBy: CleanText(a.GetRunBy(), 64),
		Paused: a.GetState() == ascv1.OrgAgentState_ORG_AGENT_STATE_PAUSED, Created: unixTime(a.GetCreated()),
		CreatedBy: CleanText(a.GetCreatedBy(), 64), Updated: unixTime(a.GetUpdated()),
	}
	for _, p := range a.GetPaths() {
		out.Paths = append(out.Paths, CleanText(p, MaxPathBytes))
	}
	for _, r := range a.GetRepos() {
		out.Repos = append(out.Repos, CleanText(r, MaxTextBytes))
	}
	return out, nil
}

// ListOrgAgents lists the agents of org.
func ListOrgAgents(ctx context.Context, conn grpc.ClientConnInterface, org string) ([]OrgAgent, error) {
	org, err := orgName(org)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewOrgSettingsClient(conn).ListAgents(cctx, &ascv1.ListOrgAgentsRequest{Org: org})
	if err != nil {
		return nil, err
	}
	if len(resp.GetAgents()) > MaxOrgAgents {
		return nil, fmt.Errorf("%w: agents", ErrBadAnswer)
	}
	out := make([]OrgAgent, 0, len(resp.GetAgents()))
	for _, a := range resp.GetAgents() {
		row, err := agentOf(a)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// AgentForm is what the form of an agent sends, checked.
type AgentForm struct {
	Name        string
	DisplayName string
	RunBy       string
	Paths       []string
	Repos       []string
}

func checkAgentLists(get func(string) string) (paths, repos []string, err error) {
	if paths, err = Lines(get("paths"), MaxAgentPaths, MaxPathBytes); err != nil {
		return nil, nil, err
	}
	if repos, err = Lines(get("repos"), MaxAgentRepos, MaxTextBytes); err != nil {
		return nil, nil, err
	}
	for _, r := range repos {
		if !repoNameShape.MatchString(r) {
			return nil, nil, fmt.Errorf("%w: repository", ErrBadInput)
		}
	}
	return paths, repos, nil
}

func checkAgentText(get func(string) string) (display, runBy string, err error) {
	display = strings.TrimSpace(get("display_name"))
	if len(display) > MaxDisplayBytes || CleanText(display, MaxDisplayBytes) != display {
		return "", "", fmt.Errorf("%w: display name", ErrBadInput)
	}
	runBy = strings.TrimSpace(get("run_by"))
	if runBy != "" && !ownerName.MatchString(runBy) {
		return "", "", fmt.Errorf("%w: person", ErrBadInput)
	}
	return display, runBy, nil
}

// NewAgentFromForm reads the fields name, display_name, run_by, paths and repos (a line each).
func NewAgentFromForm(get func(string) string) (AgentForm, error) {
	name := strings.TrimSpace(get("name"))
	if !agentNameShape.MatchString(name) {
		return AgentForm{}, fmt.Errorf("%w: agent name", ErrBadInput)
	}
	display, runBy, err := checkAgentText(get)
	if err != nil {
		return AgentForm{}, err
	}
	paths, repos, err := checkAgentLists(get)
	if err != nil {
		return AgentForm{}, err
	}
	return AgentForm{Name: name, DisplayName: display, RunBy: runBy, Paths: paths, Repos: repos}, nil
}

// CreateOrgAgent creates an agent in org.
func CreateOrgAgent(ctx context.Context, conn grpc.ClientConnInterface, org string, a AgentForm, op string) (*OrgAgent, error) {
	org, err := orgName(org)
	if err != nil {
		return nil, err
	}
	cctx, cancel, err := opCtx(ctx, op)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := ascv1.NewOrgSettingsClient(conn).CreateAgent(cctx, &ascv1.CreateOrgAgentRequest{
		Org: org, Agent: &ascv1.OrgAgent{Name: a.Name, DisplayName: a.DisplayName, RunBy: a.RunBy, Paths: a.Paths, Repos: a.Repos},
	})
	if err != nil {
		return nil, err
	}
	row, err := agentOf(resp.GetAgent())
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// UpdateOrgAgent changes the display name, the person, the lists and the state of an agent. state is "", "active" or
// "paused"; "" leaves it.
func UpdateOrgAgent(ctx context.Context, conn grpc.ClientConnInterface, org, name string, get func(string) string, state, op string) (*OrgAgent, error) {
	org, err := orgName(org)
	if err != nil {
		return nil, err
	}
	if !agentNameShape.MatchString(name) {
		return nil, fmt.Errorf("%w: agent name", ErrBadInput)
	}
	req := &ascv1.UpdateOrgAgentRequest{Org: org, Name: name}
	switch state {
	case "":
	case "active":
		s := ascv1.OrgAgentState_ORG_AGENT_STATE_ACTIVE
		req.State = &s
	case "paused":
		s := ascv1.OrgAgentState_ORG_AGENT_STATE_PAUSED
		req.State = &s
	default:
		return nil, fmt.Errorf("%w: state", ErrBadInput)
	}
	if get != nil {
		display, runBy, err := checkAgentText(get)
		if err != nil {
			return nil, err
		}
		paths, repos, err := checkAgentLists(get)
		if err != nil {
			return nil, err
		}
		req.DisplayName, req.RunBy = &display, &runBy
		req.ReplacePaths, req.Paths, req.ReplaceRepos, req.Repos = true, paths, true, repos
	}
	cctx, cancel, err := opCtx(ctx, op)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := ascv1.NewOrgSettingsClient(conn).UpdateAgent(cctx, req)
	if err != nil {
		return nil, err
	}
	row, err := agentOf(resp.GetAgent())
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteOrgAgent deletes an agent of org. The call is idempotent.
func DeleteOrgAgent(ctx context.Context, conn grpc.ClientConnInterface, org, name, op string) error {
	org, err := orgName(org)
	if err != nil {
		return err
	}
	if !agentNameShape.MatchString(name) {
		return fmt.Errorf("%w: agent name", ErrBadInput)
	}
	cctx, cancel, err := opCtx(ctx, op)
	if err != nil {
		return err
	}
	defer cancel()
	_, err = ascv1.NewOrgSettingsClient(conn).DeleteAgent(cctx, &ascv1.DeleteOrgAgentRequest{Org: org, Name: name})
	return err
}

// ------------------------------------------------------------------------------------------------ the server

// InstanceAgents is the switch and the default budget of the server.
type InstanceAgents struct {
	AllowAgents bool
	BudgetMicro uint64
}

// BudgetField writes the budget for a form field.
func (i InstanceAgents) BudgetField() string { return USDField(i.BudgetMicro) }

// LoadInstanceAgents asks for the agent switch of the server.
func LoadInstanceAgents(ctx context.Context, conn grpc.ClientConnInterface) (*InstanceAgents, error) {
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewOrgSettingsClient(conn).GetInstanceAgents(cctx, &ascv1.GetInstanceAgentsRequest{})
	if err != nil {
		return nil, err
	}
	return &InstanceAgents{AllowAgents: resp.GetAllowAgents(), BudgetMicro: resp.GetDefaultBudgetMicroUsd()}, nil
}

// InstanceAgentsFromForm reads allow_agents ("1" is on) and budget_usd.
func InstanceAgentsFromForm(get func(string) string) (InstanceAgents, error) {
	b, err := ParseUSD(get("budget_usd"))
	if err != nil {
		return InstanceAgents{}, err
	}
	return InstanceAgents{AllowAgents: get("allow_agents") == "1", BudgetMicro: b}, nil
}

// SetInstanceAgents sets the agent switch and the default budget of the server.
func SetInstanceAgents(ctx context.Context, conn grpc.ClientConnInterface, i InstanceAgents, op string) (*InstanceAgents, error) {
	cctx, cancel, err := opCtx(ctx, op)
	if err != nil {
		return nil, err
	}
	defer cancel()
	resp, err := ascv1.NewOrgSettingsClient(conn).SetInstanceAgents(cctx, &ascv1.SetInstanceAgentsRequest{AllowAgents: i.AllowAgents, DefaultBudgetMicroUsd: i.BudgetMicro})
	if err != nil {
		return nil, err
	}
	return &InstanceAgents{AllowAgents: resp.GetSettings().GetAllowAgents(), BudgetMicro: resp.GetSettings().GetDefaultBudgetMicroUsd()}, nil
}
