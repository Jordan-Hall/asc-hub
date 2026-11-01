// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"context"

	system_model "forgejo.org/models/system"
)

const localPasswordsAdminsOnlyKey = "asc.local_passwords_admins_only"

// LocalPasswordsAdminsOnly says whether only site admins may sign in with a password kept here; everyone else signs
// in through another source.
func LocalPasswordsAdminsOnly(ctx context.Context) bool {
	_, all, err := system_model.GetAllSettings(ctx)
	return err == nil && all[localPasswordsAdminsOnlyKey] == "true"
}

// SetLocalPasswordsAdminsOnly stores the admins-only option of the Local passwords row.
func SetLocalPasswordsAdminsOnly(ctx context.Context, on bool) error {
	v := "false"
	if on {
		v = "true"
	}
	return system_model.SetSettings(ctx, map[string]string{localPasswordsAdminsOnlyKey: v})
}
