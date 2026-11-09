// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascteams

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"forgejo.org/models/organization"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
)

// ErrUnknownAgent is returned when the agent to add is not one of the organization's agents in asc.
var ErrUnknownAgent = errors.New("ascteams: not an agent of the organization")

// ErrTooManyAgents is returned when the team already has organization.MaxTeamAgents agents.
var ErrTooManyAgents = errors.New("ascteams: the team has the most agents it may hold")

// TeamAgents is what the team page shows of a team's agents: the members with the scope asc gives each, the
// organization's other agents (to add), and the members asc no longer lists.
type TeamAgents struct {
	Members []asctrust.OrgAgent
	Others  []asctrust.OrgAgent
	Gone    []string
}

// Shown reports whether the page has anything to show or offer.
func (t *TeamAgents) Shown() bool {
	return len(t.Members) > 0 || len(t.Others) > 0 || len(t.Gone) > 0
}

func orgAgents(ctx context.Context, orgName string) ([]asctrust.OrgAgent, error) {
	if !setting.Asc.Enabled {
		return nil, ErrUnavailable
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if !asctrust.HasFeature(ctx, asctrust.FeatureOrgSettings) || !asctrust.HasFeature(ctx, Feature) {
		return nil, ErrUnavailable
	}
	sctx, cancel := asctrust.SystemContext(ctx, 10*time.Second)
	defer cancel()
	return asctrust.ListOrgAgents(sctx, conn, orgName)
}

// AgentsOf reads the agents of team t of the organization orgName. With asc off or without organization agents it
// returns ErrUnavailable.
func AgentsOf(ctx context.Context, orgName string, t *organization.Team) (*TeamAgents, error) {
	names, err := organization.TeamAgents(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	agents, err := orgAgents(ctx, orgName)
	if err != nil {
		return nil, err
	}
	out := &TeamAgents{}
	for _, a := range agents {
		if slices.Contains(names, a.Name) {
			out.Members = append(out.Members, a)
		} else {
			out.Others = append(out.Others, a)
		}
	}
	for _, n := range names {
		if !slices.ContainsFunc(agents, func(a asctrust.OrgAgent) bool { return a.Name == n }) {
			out.Gone = append(out.Gone, n)
		}
	}
	return out, nil
}

// AddAgent makes the organization's agent a member of team t and sends the team to asc. It reports false when the
// agent already was a member.
func AddAgent(ctx context.Context, orgName string, t *organization.Team, agent string) (bool, error) {
	agents, err := orgAgents(ctx, orgName)
	if err != nil {
		return false, err
	}
	if !slices.ContainsFunc(agents, func(a asctrust.OrgAgent) bool { return a.Name == agent }) {
		return false, ErrUnknownAgent
	}
	names, err := organization.TeamAgents(ctx, t.ID)
	if err != nil {
		return false, err
	}
	if len(names) >= organization.MaxTeamAgents && !slices.Contains(names, agent) {
		return false, ErrTooManyAgents
	}
	added, err := organization.AddTeamAgent(ctx, t, agent)
	if err != nil {
		return false, err
	}
	if added {
		Kick(t.OrgID)
	}
	return added, nil
}

// RemoveAgent takes the agent out of team t and sends the team to asc. It needs no answer from asc: removing never
// gives more.
func RemoveAgent(ctx context.Context, t *organization.Team, agent string) (bool, error) {
	removed, err := organization.RemoveTeamAgent(ctx, t.ID, agent)
	if err != nil {
		return false, err
	}
	if removed {
		Kick(t.OrgID)
	}
	return removed, nil
}
