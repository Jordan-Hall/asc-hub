// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package packages

import (
	"fmt"
	"net/http"

	packages_model "forgejo.org/models/packages"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/structs"
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
	vis := make(map[int64]PackageVisibility, len(pds))
	for _, pd := range pds {
		vis[pd.Package.ID] = VisibilityOf(pd.Owner)
	}
	ctx.Data["PackageVisibility"] = vis
	return pds, total, nil
}

// PackageVisibility is who can install a package: Key is public, internal or private (asc_work.pk_vis_<key>), Owner
// names the owner for the hint of a private package.
type PackageVisibility struct {
	Key   string
	Owner string
}

// VisibilityOf is the visibility of the packages of an owner, as every registry enforces it
// (packages_service.DeterminePackageAccessMode): a package is as visible as its owner. A public owner on a hub that needs
// a sign-in to see anything is internal. The registries also serve indexes of all the packages of an owner of one type
// (Debian, RPM, Alpine, Arch, Helm, Conda, CRAN), which is why visibility is the owner's and not set per package.
func VisibilityOf(owner *user_model.User) PackageVisibility {
	v := PackageVisibility{Key: "public", Owner: owner.Name}
	switch {
	case owner.Visibility == structs.VisibleTypePrivate:
		v.Key = "private"
	case owner.Visibility == structs.VisibleTypeLimited || setting.Service.RequireSignInView:
		v.Key = "internal"
	}
	return v
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
