// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package organization

import (
	"context"
	"slices"

	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"
)

// AscTeamAgent is an agent of the organization (one of its asc agent identities, by name) that is a member of a team.
// asc admits it through the team's path rules and policy subjects as `agent:<name>`, always within the agent's own
// path scope.
type AscTeamAgent struct {
	ID          int64              `xorm:"pk autoincr"`
	OrgID       int64              `xorm:"INDEX NOT NULL"`
	TeamID      int64              `xorm:"UNIQUE(team_agent) NOT NULL"`
	Agent       string             `xorm:"VARCHAR(40) UNIQUE(team_agent) NOT NULL"`
	CreatedUnix timeutil.TimeStamp `xorm:"created"`
}

func init() {
	db.RegisterModel(new(AscTeamAgent))
}

// TableName is the table of the model.
func (AscTeamAgent) TableName() string { return "asc_team_agent" }

// MaxTeamAgents is the most agents one team holds.
const MaxTeamAgents = 64

// TeamAgents lists the agents of the team, sorted.
func TeamAgents(ctx context.Context, teamID int64) ([]string, error) {
	var names []string
	err := db.GetEngine(ctx).Table("asc_team_agent").Where("team_id = ?", teamID).Cols("agent").Find(&names)
	slices.Sort(names)
	return names, err
}

// OrgTeamAgents maps each team of the organization that has agents to their sorted names.
func OrgTeamAgents(ctx context.Context, orgID int64) (map[int64][]string, error) {
	var rows []AscTeamAgent
	if err := db.GetEngine(ctx).Where("org_id = ?", orgID).Find(&rows); err != nil {
		return nil, err
	}
	out := map[int64][]string{}
	for _, r := range rows {
		out[r.TeamID] = append(out[r.TeamID], r.Agent)
	}
	for id := range out {
		slices.Sort(out[id])
	}
	return out, nil
}

// AddTeamAgent makes the agent a member of the team. It reports false when it already was one.
func AddTeamAgent(ctx context.Context, t *Team, agent string) (bool, error) {
	added := false
	err := db.WithTx(ctx, func(ctx context.Context) error {
		has, err := db.GetEngine(ctx).Exist(&AscTeamAgent{TeamID: t.ID, Agent: agent})
		if err != nil || has {
			return err
		}
		_, err = db.GetEngine(ctx).Insert(&AscTeamAgent{OrgID: t.OrgID, TeamID: t.ID, Agent: agent})
		added = err == nil
		return err
	})
	return added, err
}

// RemoveTeamAgent takes the agent out of the team. It reports whether it was a member.
func RemoveTeamAgent(ctx context.Context, teamID int64, agent string) (bool, error) {
	n, err := db.GetEngine(ctx).Where("team_id = ? AND agent = ?", teamID, agent).Delete(new(AscTeamAgent))
	return n > 0, err
}

// PruneTeamAgents removes from every team of the organization the agents not in keep (the organization's agents
// as asc lists them). It returns how many memberships were removed.
func PruneTeamAgents(ctx context.Context, orgID int64, keep []string) (int64, error) {
	sess := db.GetEngine(ctx).Where("org_id = ?", orgID)
	if len(keep) > 0 {
		sess = sess.NotIn("agent", keep)
	}
	return sess.Delete(new(AscTeamAgent))
}
