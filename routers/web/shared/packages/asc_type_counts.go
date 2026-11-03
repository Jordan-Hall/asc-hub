// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package packages

import (
	packages_model "forgejo.org/models/packages"
	"forgejo.org/services/context"
)

// SetTypeCounts gives a package list its type chips: the count of every type and of all of them, under the
// name filter of the list. It reports false when it wrote an error page.
func SetTypeCounts(ctx *context.Context, opts *packages_model.PackageSearchOptions) bool {
	counts, err := packages_model.CountPackagesByType(ctx, opts)
	if err != nil {
		ctx.ServerError("CountPackagesByType", err)
		return false
	}
	var all int64
	for _, c := range counts {
		all += c.Count
	}
	ctx.Data["PackageTypeCounts"] = counts
	ctx.Data["PackageCountAll"] = all
	return true
}
