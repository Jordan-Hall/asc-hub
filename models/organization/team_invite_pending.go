// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package organization

import (
	"context"
	"strings"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"

	"xorm.io/builder"
)

// MaxPendingInvites bounds the invitations one person's page lists.
const MaxPendingInvites = 50

// GetPendingInvitesForUser lists the invitations to a team that wait for the person: those made for their account, and those
// made for an email address of theirs that they have confirmed (an address they have not confirmed proves nothing). Newest
// first. An invitation to a team of an organization the person already belongs to through that team is not listed.
func GetPendingInvitesForUser(ctx context.Context, u *user_model.User) ([]*TeamInvite, error) {
	emails, err := user_model.GetEmailAddresses(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	var confirmed []string
	for _, e := range emails {
		if e.IsActivated {
			confirmed = append(confirmed, strings.ToLower(e.Email))
		}
	}
	var cond builder.Cond = builder.Eq{"invited_id": u.ID}
	if len(confirmed) > 0 {
		cond = builder.Or(cond, builder.And(builder.IsNull{"invited_id"}, builder.In("LOWER(email)", confirmed)))
	}
	invites := make([]*TeamInvite, 0, 8)
	if err := db.GetEngine(ctx).Where(cond).Desc("id").Limit(MaxPendingInvites).Find(&invites); err != nil {
		return nil, err
	}
	out := invites[:0]
	for _, inv := range invites {
		member, err := db.GetEngine(ctx).Table("team_user").Where("team_id = ? AND uid = ?", inv.TeamID, u.ID).Exist()
		if err != nil {
			return nil, err
		}
		if !member {
			out = append(out, inv)
		}
	}
	return out, nil
}

// DeclineInviteForUser removes an invitation that waits for the person, found by its token. It answers ErrTeamInviteNotFound
// for an invitation that is not theirs, so a token cannot be used to probe for invitations of others.
func DeclineInviteForUser(ctx context.Context, u *user_model.User, token string) error {
	mine, err := GetPendingInvitesForUser(ctx, u)
	if err != nil {
		return err
	}
	for _, inv := range mine {
		if inv.Token == token {
			return RemoveInviteByID(ctx, inv.ID, inv.TeamID)
		}
	}
	return ErrTeamInviteNotFound{Token: token}
}
