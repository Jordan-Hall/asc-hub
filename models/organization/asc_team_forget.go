// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package organization

import (
	"context"
	"strings"

	"forgejo.org/models/db"
	"forgejo.org/modules/timeutil"
)

// AscTeamForget is an organization name whose teams asc may still hold although the name no longer belongs to that
// organization (it was renamed or deleted). asc keeps teams by name, so the hub clears them there and only then
// deletes the row; until then every scheduled team sync tries again.
type AscTeamForget struct {
	ID          int64              `xorm:"pk autoincr"`
	OrgName     string             `xorm:"VARCHAR(255) UNIQUE NOT NULL"`
	CreatedUnix timeutil.TimeStamp `xorm:"created"`
}

func init() {
	db.RegisterModel(new(AscTeamForget))
}

// TableName is the table of the model.
func (AscTeamForget) TableName() string { return "asc_team_forget" }

// AddAscTeamForget records that asc must forget the teams held under name. A name already recorded is kept once.
func AddAscTeamForget(ctx context.Context, name string) error {
	name = strings.ToLower(name)
	return db.WithTx(ctx, func(ctx context.Context) error {
		has, err := db.GetEngine(ctx).Exist(&AscTeamForget{OrgName: name})
		if err != nil || has {
			return err
		}
		_, err = db.GetEngine(ctx).Insert(&AscTeamForget{OrgName: name})
		return err
	})
}

// AscTeamForgets lists the names still to be cleared in asc, oldest first.
func AscTeamForgets(ctx context.Context) ([]string, error) {
	var names []string
	err := db.GetEngine(ctx).Table("asc_team_forget").OrderBy("id").Cols("org_name").Find(&names)
	return names, err
}

// DoneAscTeamForget removes name once asc holds no teams under it.
func DoneAscTeamForget(ctx context.Context, name string) error {
	_, err := db.GetEngine(ctx).Where("org_name = ?", strings.ToLower(name)).Delete(new(AscTeamForget))
	return err
}
