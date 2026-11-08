// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"context"

	"forgejo.org/models/db"
	"forgejo.org/models/perm"
)

// The Maintain role (board RepoSettingsAccess) is a collaboration with write access and AscMaintain set. On top of
// write access it opens the General settings, except renaming and the visibility, and the branch and tag rules. It
// opens nothing else of the settings: the access page, the danger zone, webhooks, secrets and deploy keys stay with
// the admins. A collaboration whose mode is not write is never a Maintain one, whatever AscMaintain holds.

// IsAscMaintainer reports whether the user holds the Maintain role in the repository.
func IsAscMaintainer(ctx context.Context, repoID, userID int64) (bool, error) {
	if repoID <= 0 || userID <= 0 {
		return false, nil
	}
	return db.GetEngine(ctx).
		Where("repo_id = ? AND user_id = ? AND mode = ? AND asc_maintain = ?", repoID, userID, perm.AccessModeWrite, true).
		Exist(new(Collaboration))
}
