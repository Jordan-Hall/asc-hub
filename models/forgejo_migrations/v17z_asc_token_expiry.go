// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package forgejo_migrations

import (
	"strings"

	"code.forgejo.org/xorm/xorm"
)

func init() {
	registerMigration(&Migration{
		Description: "personal access tokens get an expiry and the scopes changes and runs; a device code remembers the scope the terminal asked for",
		Upgrade:     addAccessTokenExpiryAndAscScopes,
	})
}

func addAccessTokenExpiryAndAscScopes(x *xorm.Engine) error {
	type AccessToken struct {
		ID          int64  `xorm:"pk autoincr"`
		Scope       string `xorm:"TEXT"`
		ExpiresUnix int64  `xorm:"INDEX NOT NULL DEFAULT 0"`
	}
	type AscDeviceCode struct {
		ID    int64  `xorm:"pk autoincr"`
		Scope string `xorm:"VARCHAR(255) NOT NULL DEFAULT ''"`
	}
	if _, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true}, new(AccessToken), new(AscDeviceCode)); err != nil {
		return err
	}

	// A token that could use repositories keeps being able to use the asc work of its owner: it gets the scopes
	// changes and runs at the level of its repository scope. A token with "all" already has them. Run twice, this
	// changes nothing: a scope that already names them is left alone.
	var tokens []AccessToken
	if err := x.Table("access_token").Cols("id", "scope").Find(&tokens); err != nil {
		return err
	}
	for _, t := range tokens {
		scope := t.Scope
		if strings.Contains(scope, ":changes") || strings.Contains(scope, ":runs") || scope == "all" {
			continue
		}
		var add string
		switch {
		case strings.Contains(scope, "write:repository"):
			add = "write:changes,write:runs"
		case strings.Contains(scope, "read:repository"):
			add = "read:changes,read:runs"
		default:
			continue
		}
		if _, err := x.Table("access_token").ID(t.ID).Cols("scope").Update(map[string]any{"scope": scope + "," + add}); err != nil {
			return err
		}
	}
	return nil
}
