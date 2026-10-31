// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package container

import (
	"context"

	packages_model "forgejo.org/models/packages"
	user_model "forgejo.org/models/user"
)

// MaxPreviewRules bounds the rules a preview lists.
const MaxPreviewRules = 500

// RulePreview is what one enabled cleanup rule would remove if it ran now.
type RulePreview struct {
	Rule     *packages_model.PackageCleanupRule
	Owner    string
	Versions int // versions the rule would delete
	Packages int // packages that lose at least one version
	Err      string
}

// PreviewRules counts, for every enabled cleanup rule, what running it now would delete. Nothing is deleted.
func PreviewRules(ctx context.Context) ([]RulePreview, error) {
	var out []RulePreview
	err := packages_model.IterateEnabledCleanupRules(ctx, func(ctx context.Context, pcr *packages_model.PackageCleanupRule) error {
		if len(out) >= MaxPreviewRules {
			return nil
		}
		p := RulePreview{Rule: pcr}
		if owner, err := user_model.GetUserByID(ctx, pcr.OwnerID); err == nil {
			p.Owner = owner.Name
		}
		targets, err := GetCleanupTargets(ctx, pcr, true)
		if err != nil {
			p.Err = err.Error()
		} else {
			seen := map[int64]bool{}
			for _, t := range targets {
				seen[t.Package.ID] = true
			}
			p.Versions, p.Packages = len(targets), len(seen)
		}
		out = append(out, p)
		return nil
	})
	return out, err
}
