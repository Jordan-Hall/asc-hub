// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package asctrust

import (
	"context"
	"fmt"

	auth_model "forgejo.org/models/auth"
	"forgejo.org/models/db"
	"forgejo.org/modules/log"
)

// LegacyCallTokenPrefix is the name an earlier version of the hub gave to the one-request access tokens it made for
// its web pages. The hub makes none now (docs/asc/ARCHITECTURE.md, "Forwarded identity"); a row that a crash left
// behind would still be a valid token with a repository scope, so each start removes them.
const LegacyCallTokenPrefix = "asc-hub-call-"

// PurgeLegacyCallTokens deletes the access tokens named with LegacyCallTokenPrefix and the rows that limit them to
// repositories. It is safe to run on every start: with nothing left it changes nothing. A token that a person named
// with the prefix is removed too, and the log says how many were.
func PurgeLegacyCallTokens(ctx context.Context) error {
	var removed int64
	err := db.WithTx(ctx, func(ctx context.Context) error {
		e := db.GetEngine(ctx)
		var ids []int64
		if err := e.Table("access_token").Where("name LIKE ?", LegacyCallTokenPrefix+"%").Cols("id").Find(&ids); err != nil {
			return fmt.Errorf("find: %w", err)
		}
		if len(ids) == 0 {
			return nil
		}
		if _, err := e.In("token_id", ids).Delete(&auth_model.AccessTokenResourceRepo{}); err != nil {
			return fmt.Errorf("delete the repository rows: %w", err)
		}
		n, err := e.In("id", ids).Delete(&auth_model.AccessToken{})
		if err != nil {
			return fmt.Errorf("delete the tokens: %w", err)
		}
		removed = n
		return nil
	})
	if err != nil {
		return fmt.Errorf("asctrust: removing the call tokens of an earlier version: %w", err)
	}
	if removed > 0 {
		log.Warn("asc: removed %d access token(s) named %q* left by an earlier version of the hub", removed, LegacyCallTokenPrefix)
	}
	return nil
}
