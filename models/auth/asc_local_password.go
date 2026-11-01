// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package auth

import (
	"context"
	"sync"
	"time"

	system_model "forgejo.org/models/system"
)

const localPasswordsAdminsOnlyKey = "asc.local_passwords_admins_only"

// LocalPasswordsAdminsOnly says whether only site admins may sign in with a password kept here; everyone else signs
// in through another source.
// Every signed-in request asks, so the answer is kept for a few seconds.
func LocalPasswordsAdminsOnly(ctx context.Context) bool {
	localPasswordsMu.Lock()
	defer localPasswordsMu.Unlock()
	if time.Since(localPasswordsAt) < 5*time.Second {
		return localPasswordsOn
	}
	_, all, err := system_model.GetAllSettings(ctx)
	if err != nil {
		return localPasswordsOn
	}
	localPasswordsOn, localPasswordsAt = all[localPasswordsAdminsOnlyKey] == "true", time.Now()
	return localPasswordsOn
}

var (
	localPasswordsMu sync.Mutex
	localPasswordsOn bool
	localPasswordsAt time.Time
)

// SetLocalPasswordsAdminsOnly stores the admins-only option of the Local passwords row.
func SetLocalPasswordsAdminsOnly(ctx context.Context, on bool) error {
	v := "false"
	if on {
		v = "true"
	}
	if err := system_model.SetSettings(ctx, map[string]string{localPasswordsAdminsOnlyKey: v}); err != nil {
		return err
	}
	localPasswordsMu.Lock()
	localPasswordsOn, localPasswordsAt = on, time.Now()
	localPasswordsMu.Unlock()
	return nil
}
