// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascbyo

import (
	"context"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
)

// KeyActivityActions are the audit actions asc writes about an AI key, each with the key's id as its target.
var KeyActivityActions = []string{
	"ai.key.created",
	"ai.key.updated",
	"ai.key.replaced",
	"ai.key.tested",
	"ai.key.shared",
	"ai.key.unshared",
	"ai.key.caps.updated",
	"ai.key.cap.warning",
	"ai.key.deleted",
}

// MaxKeyActivity is how many events the Activity section of a key shows, newest first.
const MaxKeyActivity = 25

// KeyActivity returns the newest audit events about one key of the scope. The key is checked to be the scope's
// first, so a key id of another owner reads nothing. The answer is raw: the caller cleans it (asctrust.AuditPageOf).
func (s *Service) KeyActivity(ctx context.Context, sc Scope, keyID string) (*ascv1.ListAuditResponse, error) {
	if !sc.Valid() || !ValidKeyID(keyID) {
		return nil, failure(FailNotFound)
	}
	if s.api.Audit == nil {
		return nil, failure(FailUnimplemented)
	}
	if _, err := s.GetKey(ctx, sc, keyID); err != nil {
		return nil, err
	}
	cctx, cancel := s.call(ctx, sc, "KeyActivity", "", false)
	defer cancel()
	resp, err := s.api.Audit.List(cctx, &ascv1.ListAuditRequest{
		Scope:  sc.Owner(),
		Filter: &ascv1.AuditFilter{Target: keyID, Actions: KeyActivityActions},
		Limit:  MaxKeyActivity,
	})
	if err != nil {
		return nil, done("ai key activity", sc, err)
	}
	if len(resp.GetEvents()) > MaxKeyActivity {
		resp.Events = resp.Events[:MaxKeyActivity]
	}
	return resp, nil
}
