// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ascteams copies the hub's organization teams to asc. A path rule (`[asc] path rules` of a repository) or
// a repository policy subject `team:<name>` admits the members asc holds for that team of the repository's
// organization; the hub is where teams are kept, so it sends each team's members whenever they change and checks
// every organization with asc repositories on a schedule, which also catches renamed and deleted accounts.
//
// The hub is the authority: a team asc holds that the organization no longer has is cleared, and a team's members
// are replaced by the hub's list. Team and member names are sent lower case, as asc compares them.
package ascteams

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"forgejo.org/models"
	"forgejo.org/models/db"
	"forgejo.org/models/organization"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/asctrust"
)

// Feature is the asc feature that carries teams.
const Feature = "pathrules.v1"

// Debounce is how long a change waits for more changes of the same organization before it is sent.
var Debounce = 2 * time.Second

// Result counts what one reconcile of an organization did.
type Result struct {
	Set     int // teams whose members were sent
	Cleared int // teams asc held that the organization no longer has
}

// ErrUnavailable is returned when asc cannot be reached or does not keep teams.
var ErrUnavailable = errors.New("ascteams: asc does not keep teams")

// Desired is the teams of org as asc should hold them: lower-case team name to the sorted lower-case logins.
func Desired(ctx context.Context, orgID int64) (map[string][]string, error) {
	teams, err := organization.FindOrgTeams(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]string, len(teams))
	byID := make(map[int64]string, len(teams))
	for _, t := range teams {
		out[t.LowerName] = []string{}
		byID[t.ID] = t.LowerName
	}
	type row struct {
		TeamID    int64
		LowerName string
	}
	var rows []row
	err = db.GetEngine(ctx).Table("team_user").
		Join("INNER", "`user`", "`user`.id = team_user.uid").
		Where("team_user.org_id = ?", orgID).
		Cols("team_user.team_id", "`user`.lower_name").
		Find(&rows)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if name, ok := byID[r.TeamID]; ok {
			out[name] = append(out[name], r.LowerName)
		}
	}
	for name, members := range out {
		slices.Sort(members)
		out[name] = slices.Compact(members)
	}
	return out, nil
}

// Reconcile makes asc hold the teams of the organization orgID: it sends each team whose members differ and clears
// the teams the organization no longer has. It returns ErrUnavailable when asc cannot take teams.
func Reconcile(ctx context.Context, orgID int64) (Result, error) {
	var res Result
	if !setting.Asc.Enabled {
		return res, ErrUnavailable
	}
	org, err := user_model.GetUserByID(ctx, orgID)
	if err != nil {
		return res, err
	}
	if !org.IsOrganization() {
		return res, fmt.Errorf("ascteams: %d is not an organization", orgID)
	}
	want, err := Desired(ctx, orgID)
	if err != nil {
		return res, err
	}
	conn, err := asctrust.Conn()
	if err != nil {
		return res, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if !asctrust.HasFeature(ctx, Feature) {
		return res, ErrUnavailable
	}
	sctx, cancel := asctrust.SystemContext(ctx, 2*time.Minute)
	defer cancel()
	have, err := asctrust.ListTeams(sctx, conn, org.LowerName)
	if err != nil {
		return res, fmt.Errorf("ascteams: reading the teams of %s: %w", org.LowerName, err)
	}
	held := make(map[string][]string, len(have))
	for _, t := range have {
		held[t.Name] = t.Members
	}
	names := make([]string, 0, len(want))
	for name := range want {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		members := want[name]
		if len(members) > asctrust.MaxTeamMembers {
			log.Warn("ascteams: team %s/%s has %d members; asc keeps %d, the rest are not sent", org.LowerName, name, len(members), asctrust.MaxTeamMembers)
			members = members[:asctrust.MaxTeamMembers]
		}
		cur, ok := held[name]
		if (ok && slices.Equal(cur, members)) || (!ok && len(members) == 0) {
			continue
		}
		if err := asctrust.SetTeamMembers(sctx, conn, org.LowerName, name, members, asctrust.NewOpID()); err != nil {
			return res, fmt.Errorf("ascteams: sending team %s/%s: %w", org.LowerName, name, err)
		}
		res.Set++
	}
	for _, t := range have {
		if _, ok := want[t.Name]; ok || len(t.Members) == 0 {
			continue
		}
		if err := asctrust.SetTeamMembers(sctx, conn, org.LowerName, t.Name, nil, asctrust.NewOpID()); err != nil {
			return res, fmt.Errorf("ascteams: clearing team %s/%s: %w", org.LowerName, t.Name, err)
		}
		res.Cleared++
	}
	return res, nil
}

// OrgsWithAscRepos lists the organizations that own a repository stored in asc.
func OrgsWithAscRepos(ctx context.Context) ([]int64, error) {
	var ids []int64
	err := db.GetEngine(ctx).Table("repository").
		Join("INNER", "`user`", "`user`.id = repository.owner_id").
		Where("repository.storage_backend = ?", "asc").
		And("`user`.type = ?", user_model.UserTypeOrganization).
		Distinct("repository.owner_id").
		Find(&ids)
	return ids, err
}

// ReconcileAll reconciles every organization with asc repositories. One organization failing does not stop the
// others; the first error is returned with the totals.
func ReconcileAll(ctx context.Context) (Result, int, error) {
	var total Result
	ids, err := OrgsWithAscRepos(ctx)
	if err != nil {
		return total, 0, err
	}
	var first error
	done := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			return total, done, ctx.Err()
		}
		res, err := Reconcile(ctx, id)
		if errors.Is(err, ErrUnavailable) {
			return total, done, err
		}
		if err != nil {
			log.Warn("ascteams: %v", err)
			if first == nil {
				first = err
			}
			continue
		}
		total.Set += res.Set
		total.Cleared += res.Cleared
		done++
	}
	return total, done, first
}

var (
	kickMu   sync.Mutex
	kickCtx  = context.Background()
	timers   = map[int64]*time.Timer{}
	inFlight sync.WaitGroup
)

// Kick schedules a reconcile of the organization orgID after Debounce; more changes in that time push it back, so
// adding ten members sends the team once. A failure is logged; the scheduled sync repairs it.
func Kick(orgID int64) {
	if !setting.Asc.Enabled || orgID <= 0 {
		return
	}
	kickMu.Lock()
	defer kickMu.Unlock()
	if t, ok := timers[orgID]; ok {
		t.Reset(Debounce)
		return
	}
	inFlight.Add(1)
	timers[orgID] = time.AfterFunc(Debounce, func() {
		defer inFlight.Done()
		kickMu.Lock()
		delete(timers, orgID)
		ctx := kickCtx
		kickMu.Unlock()
		if ctx.Err() != nil {
			return
		}
		if _, err := Reconcile(ctx, orgID); err != nil && !errors.Is(err, ErrUnavailable) {
			log.Warn("ascteams: %v", err)
		}
	})
}

// Wait blocks until every scheduled reconcile has run. It is for tests.
func Wait() { inFlight.Wait() }

// Init connects team changes to Kick when asc is on. ctx bounds the reconciles that Kick starts.
func Init(ctx context.Context) error {
	if !setting.Asc.Enabled {
		return nil
	}
	kickMu.Lock()
	kickCtx = ctx
	kickMu.Unlock()
	models.TeamsChanged = Kick
	return nil
}
