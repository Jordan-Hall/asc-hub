// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package packages

import (
	"fmt"
	"net/http"

	packages_model "forgejo.org/models/packages"
	"forgejo.org/modules/log"
	"forgejo.org/services/context"
)

// SearchLatestVersions finds the packages of a list; tests replace it to make a list fail.
var SearchLatestVersions = packages_model.SearchLatestVersions

// LoadList loads the packages of an owner or repository list, and gives the list its type chips: the count of every
// type and of all of them, under the name filter of the list.
func LoadList(ctx *context.Context, opts *packages_model.PackageSearchOptions) ([]*packages_model.PackageDescriptor, int64, error) {
	pvs, total, err := SearchLatestVersions(ctx, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("SearchLatestVersions: %w", err)
	}
	counts, err := packages_model.CountPackagesByType(ctx, opts)
	if err != nil {
		return nil, 0, fmt.Errorf("CountPackagesByType: %w", err)
	}
	pds, err := packages_model.GetPackageDescriptors(ctx, pvs)
	if err != nil {
		return nil, 0, fmt.Errorf("GetPackageDescriptors: %w", err)
	}
	var all int64
	for _, c := range counts {
		all += c.Count
	}
	ctx.Data["PackageTypeCounts"] = counts
	ctx.Data["PackageCountAll"] = all
	return pds, total, nil
}

// ListFailed puts a list that could not be loaded into its error state: the error goes to the log with the request id,
// and the page shows the request id, Try again and the status page in place of the list. It returns the status of the
// page.
func ListFailed(ctx *context.Context, err error) int {
	log.Error("package list %s (request %s): %v", ctx.Req.URL.Path, ctx.RequestIDForLog(), err)
	ctx.Data["PackagesLoadFailed"] = true
	ctx.Data["RetryLink"] = ctx.Req.URL.RequestURI()
	return http.StatusInternalServerError
}
