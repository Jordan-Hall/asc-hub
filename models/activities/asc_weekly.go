// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package activities

import (
	"context"
	"time"

	"forgejo.org/models/db"
	"forgejo.org/modules/structs"
	"forgejo.org/modules/timeutil"

	"xorm.io/builder"
)

// AscActivityWeeks is how many weeks the activity line of the people and organization directories covers.
const AscActivityWeeks = 12

const ascWeek = 7 * 24 * time.Hour

// AscWeekly is the public activity of one account over the last AscActivityWeeks weeks.
type AscWeekly struct {
	// Weeks holds the actions of each week, oldest first; the last is the week that ends now.
	Weeks []uint32
	// Total is the sum of Weeks.
	Total int64
	// Changesets is how many pull requests were opened (each is a changeset to review), and Reviews how many reviews
	// were given (approve, comment, request changes). Only for people.
	Changesets int64
	Reviews    int64
	// Last is the time of the latest action, 0 when there is none in the window. Only for organizations.
	Last timeutil.TimeStamp
}

// publicRepos is the repositories anyone may see: public, of an owner with a public profile.
func publicRepos() *builder.Builder {
	return builder.Select("`repository`.id").From("`repository`").
		Join("INNER", "`user` `owner`", "`owner`.id = `repository`.owner_id").
		Where(builder.Eq{"`repository`.is_private": false, "`owner`.visibility": structs.VisibleTypePublic})
}

// ownActions is the actions counted once each (the copy in the feed of the actor), not private, in public repositories.
func ownActions(since, until int64) builder.Cond {
	return builder.Expr("`action`.user_id = `action`.act_user_id").
		And(builder.Eq{"`action`.is_private": false}).
		And(builder.Gte{"`action`.created_unix": since}, builder.Lt{"`action`.created_unix": until}).
		And(builder.In("`action`.repo_id", publicRepos()))
}

// orgActions is ownActions without the actions of people who keep their activity private: the activity of an
// organization is shown to anyone, so it must not tell what such a person did.
func orgActions(since, until int64) builder.Cond {
	return ownActions(since, until).And(builder.NotIn("`action`.act_user_id",
		builder.Select("`actor`.id").From("`user`", "actor").Where(builder.Eq{"`actor`.keep_activity_private": true})))
}

type ascKeyCount struct {
	K int64
	N int64
}

func (w *AscWeekly) add(week int, n int64) {
	w.Weeks[week] += uint32(n)
	w.Total += n
}

func newWeeklies(ids []int64) map[int64]*AscWeekly {
	out := make(map[int64]*AscWeekly, len(ids))
	for _, id := range ids {
		out[id] = &AscWeekly{Weeks: make([]uint32, AscActivityWeeks)}
	}
	return out
}

// AscUserActivity gives the public activity of each of the users at now: actions per week, and the pull requests
// opened and reviews given in the window. Only what anyone may see counts (public repositories of public owners); the
// caller decides whether the viewer may see the activity of a person at all (ActivityReadable).
func AscUserActivity(ctx context.Context, userIDs []int64, now time.Time) (map[int64]*AscWeekly, error) {
	out := newWeeklies(userIDs)
	if len(userIDs) == 0 {
		return out, nil
	}
	e := db.GetEngine(ctx)
	start := now.Add(-AscActivityWeeks * ascWeek)
	for week := range AscActivityWeeks {
		from := start.Add(time.Duration(week) * ascWeek).Unix()
		var rows []ascKeyCount
		if err := e.Table("action").Select("act_user_id AS k, COUNT(*) AS n").
			Where(ownActions(from, from+int64(ascWeek/time.Second)).And(builder.In("act_user_id", userIDs))).
			GroupBy("act_user_id").Find(&rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.K].add(week, r.N)
		}
	}
	since, until := start.Unix(), now.Unix()+1
	var pulls []ascKeyCount
	if err := e.Table("issue").Select("poster_id AS k, COUNT(*) AS n").
		Where(builder.Eq{"is_pull": true}.And(builder.In("poster_id", userIDs), builder.Gte{"created_unix": since},
			builder.Lt{"created_unix": until}, builder.In("repo_id", publicRepos()))).
		GroupBy("poster_id").Find(&pulls); err != nil {
		return nil, err
	}
	for _, r := range pulls {
		out[r.K].Changesets = r.N
	}
	var reviews []ascKeyCount
	if err := e.Table("review").Select("reviewer_id AS k, COUNT(*) AS n").
		Where(builder.In("type", 1, 2, 3).And(builder.In("reviewer_id", userIDs), builder.Gte{"created_unix": since},
			builder.Lt{"created_unix": until},
			builder.In("issue_id", builder.Select("id").From("issue").Where(builder.In("repo_id", publicRepos()))))).
		GroupBy("reviewer_id").Find(&reviews); err != nil {
		return nil, err
	}
	for _, r := range reviews {
		out[r.K].Reviews = r.N
	}
	return out, nil
}

// AscOrgActivity gives the public activity of each of the organizations at now: the actions per week in its public
// repositories (a private or limited organization has none anyone may see), and the time of the latest. The actions
// of people who keep their activity private are left out.
func AscOrgActivity(ctx context.Context, orgIDs []int64, now time.Time) (map[int64]*AscWeekly, error) {
	out := newWeeklies(orgIDs)
	if len(orgIDs) == 0 {
		return out, nil
	}
	e := db.GetEngine(ctx)
	start := now.Add(-AscActivityWeeks * ascWeek)
	for week := range AscActivityWeeks {
		from := start.Add(time.Duration(week) * ascWeek).Unix()
		var rows []ascKeyCount
		if err := e.Table("action").Join("INNER", "repository", "`repository`.id = `action`.repo_id").
			Select("`repository`.owner_id AS k, COUNT(*) AS n").
			Where(orgActions(from, from+int64(ascWeek/time.Second)).And(builder.In("`repository`.owner_id", orgIDs))).
			GroupBy("`repository`.owner_id").Find(&rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.K].add(week, r.N)
		}
	}
	var last []ascKeyCount
	if err := e.Table("action").Join("INNER", "repository", "`repository`.id = `action`.repo_id").
		Select("`repository`.owner_id AS k, MAX(`action`.created_unix) AS n").
		Where(orgActions(start.Unix(), now.Unix()+1).And(builder.In("`repository`.owner_id", orgIDs))).
		GroupBy("`repository`.owner_id").Find(&last); err != nil {
		return nil, err
	}
	for _, r := range last {
		out[r.K].Last = timeutil.TimeStamp(r.N)
	}
	return out, nil
}
