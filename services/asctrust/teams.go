// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"fmt"
	"regexp"
	"slices"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/google/uuid"
	"google.golang.org/grpc"
)

// The teams asc keeps per organization (PathRules.ListTeams and SetTeamMembers). A path rule or a policy subject
// `team:<name>` admits the members asc holds for that team, so the hub copies its own teams there
// (services/ascteams). asc lower-cases the organization and a person member; another principal is `<kind>:<name>`.

const (
	// MaxTeamMembers is the most members asc keeps for one team.
	MaxTeamMembers = 5000
	// MaxOrgTeams bounds the teams read for one organization.
	MaxOrgTeams = 2000
)

var ascTeamName = regexp.MustCompile(`^[A-Za-z0-9._@+\-/]{1,128}$`)

// AscTeam is one team as asc holds it: the name and the sorted members.
type AscTeam struct {
	Name    string
	Members []string
}

// ValidTeamName reports whether asc takes name as a team name: 1 to 128 of [A-Za-z0-9._@+-/].
func ValidTeamName(name string) bool { return ascTeamName.MatchString(name) }

func validTeamMember(m string) bool {
	return ascTeamName.MatchString(m) || pathRulePrincipal.MatchString(m)
}

// ListTeams reads the teams asc holds for org. An answer with a name asc itself would refuse is a bad answer.
func ListTeams(ctx context.Context, conn grpc.ClientConnInterface, org string) ([]AscTeam, error) {
	cctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	resp, err := ascv1.NewPathRulesClient(conn).ListTeams(cctx, &ascv1.ListTeamsRequest{Org: org})
	if err != nil {
		return nil, err
	}
	if len(resp.GetTeams()) > MaxOrgTeams {
		return nil, fmt.Errorf("%w: too many teams", ErrBadAnswer)
	}
	out := make([]AscTeam, 0, len(resp.GetTeams()))
	for _, t := range resp.GetTeams() {
		if !ascTeamName.MatchString(t.GetName()) || len(t.GetMembers()) > MaxTeamMembers {
			return nil, fmt.Errorf("%w: team", ErrBadAnswer)
		}
		for _, m := range t.GetMembers() {
			if !validTeamMember(m) {
				return nil, fmt.Errorf("%w: team member", ErrBadAnswer)
			}
		}
		members := slices.Clone(t.GetMembers())
		slices.Sort(members)
		out = append(out, AscTeam{Name: t.GetName(), Members: members})
	}
	return out, nil
}

// SetTeamMembers replaces the members asc holds for team of org; no members removes the team. op makes a retry
// of the same change land once.
func SetTeamMembers(ctx context.Context, conn grpc.ClientConnInterface, org, team string, members []string, op string) error {
	if !ascTeamName.MatchString(team) {
		return fmt.Errorf("%w: team name", ErrBadInput)
	}
	if len(members) > MaxTeamMembers {
		return fmt.Errorf("%w: a team has at most %d members", ErrBadInput, MaxTeamMembers)
	}
	for _, m := range members {
		if !validTeamMember(m) {
			return fmt.Errorf("%w: team member", ErrBadInput)
		}
	}
	opID, err := uuid.Parse(op)
	if err != nil {
		return fmt.Errorf("%w: operation id", ErrBadInput)
	}
	cctx, cancel := context.WithTimeout(ascclient.WithOpID(ctx, opID), CallTimeout)
	defer cancel()
	_, err = ascv1.NewPathRulesClient(conn).SetTeamMembers(cctx, &ascv1.SetTeamMembersRequest{Org: org, Team: team, Members: members})
	return err
}
