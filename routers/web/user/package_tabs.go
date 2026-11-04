// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"maps"
	"slices"
	"strings"

	packages_model "forgejo.org/models/packages"
	arch_model "forgejo.org/modules/packages/arch"
	cargo_module "forgejo.org/modules/packages/cargo"
	chef_module "forgejo.org/modules/packages/chef"
	composer_module "forgejo.org/modules/packages/composer"
	cran_module "forgejo.org/modules/packages/cran"
	debian_module "forgejo.org/modules/packages/debian"
	helm_module "forgejo.org/modules/packages/helm"
	maven_module "forgejo.org/modules/packages/maven"
	npm_module "forgejo.org/modules/packages/npm"
	nuget_module "forgejo.org/modules/packages/nuget"
	rubygems_module "forgejo.org/modules/packages/rubygems"
	"forgejo.org/services/context"
)

// The views of a package page. Install and Dependencies are views of one version, Versions lists all of them and
// Settings is the version's settings page.
const (
	packageTabInstall      = "install"
	packageTabVersions     = "versions"
	packageTabDependencies = "dependencies"
	packageTabSettings     = "settings"
)

// PackageTab is one tab of the tab bar of a package page.
type PackageTab struct {
	Key, LabelKey, Href string
	Active              bool
	// Count is shown in a pill next to the label; 0 shows none.
	Count int
}

// PackageDep is one dependency a package version declares. Version is the requirement as the format writes it,
// Note says more where the format has it (the target framework of a NuGet dependency, the repository of a chart).
type PackageDep struct {
	Name, Version, Note string
	// Optional is set for a cargo dependency that only a feature pulls in.
	Optional bool
}

// PackageDepGroup is a kind of dependency (runtime, development, ...) and the dependencies of that kind.
type PackageDepGroup struct {
	TitleKey string
	Deps     []PackageDep
	// HasNote is set when a dependency of the group has a note or is optional, which adds the column for them.
	HasNote bool
}

// packageDepGroups reads the dependencies of a package version from its metadata, the same for every format:
// groups in a fixed order, empty groups left out, names sorted where the format keeps them in a map.
func packageDepGroups(pd *packages_model.PackageDescriptor) []PackageDepGroup {
	if pd == nil || pd.Metadata == nil {
		return nil
	}
	var out []PackageDepGroup
	add := func(key string, deps []PackageDep) {
		if len(deps) == 0 {
			return
		}
		g := PackageDepGroup{TitleKey: "asc_work.pkg_deps_" + key, Deps: deps}
		for _, d := range deps {
			g.HasNote = g.HasNote || d.Note != "" || d.Optional
		}
		out = append(out, g)
	}
	names := func(list []string) []PackageDep {
		deps := make([]PackageDep, 0, len(list))
		for _, n := range list {
			if n = strings.TrimSpace(n); n != "" {
				deps = append(deps, PackageDep{Name: n})
			}
		}
		return deps
	}
	versions := func(m map[string]string) []PackageDep {
		deps := make([]PackageDep, 0, len(m))
		for _, n := range slices.Sorted(maps.Keys(m)) {
			deps = append(deps, PackageDep{Name: n, Version: m[n]})
		}
		return deps
	}

	switch m := pd.Metadata.(type) {
	case *arch_model.VersionMetadata:
		add("runtime", names(m.Depends))
		add("optional", names(m.OptDepends))
		add("build", names(m.MakeDepends))
		add("check", names(m.CheckDepends))
	case *cargo_module.Metadata:
		kinds := map[string][]PackageDep{}
		for _, d := range m.Dependencies {
			if d == nil {
				continue
			}
			kinds[d.Kind] = append(kinds[d.Kind], PackageDep{Name: d.Name, Version: d.Req, Optional: d.Optional})
		}
		add("runtime", append(kinds[""], kinds["normal"]...))
		add("development", kinds["dev"])
		add("build", kinds["build"])
	case *chef_module.Metadata:
		add("runtime", versions(m.Dependencies))
	case *composer_module.Metadata:
		add("runtime", versions(m.Require))
		add("development", versions(m.RequireDev))
	case *cran_module.Metadata:
		add("runtime", names(m.Depends))
		add("imports", names(m.Imports))
		add("linking_to", names(m.LinkingTo))
		add("suggests", names(m.Suggests))
	case *debian_module.Metadata:
		add("runtime", names(m.Dependencies))
	case *helm_module.Metadata:
		deps := make([]PackageDep, 0, len(m.Dependencies))
		for _, d := range m.Dependencies {
			if d != nil {
				deps = append(deps, PackageDep{Name: d.Name, Version: d.Version, Note: d.Repository})
			}
		}
		add("runtime", deps)
	case *maven_module.Metadata:
		deps := make([]PackageDep, 0, len(m.Dependencies))
		for _, d := range m.Dependencies {
			if d != nil {
				deps = append(deps, PackageDep{Name: d.GroupID + ":" + d.ArtifactID, Version: d.Version})
			}
		}
		add("runtime", deps)
	case *npm_module.Metadata:
		add("runtime", versions(m.Dependencies))
		add("development", versions(m.DevelopmentDependencies))
		add("peer", versions(m.PeerDependencies))
		add("optional", versions(m.OptionalDependencies))
		add("bundled", names(m.BundleDependencies))
	case *nuget_module.Metadata:
		var deps []PackageDep
		for _, fw := range slices.Sorted(maps.Keys(m.Dependencies)) {
			for _, d := range m.Dependencies[fw] {
				deps = append(deps, PackageDep{Name: d.ID, Version: d.Version, Note: fw})
			}
		}
		add("runtime", deps)
	case *rubygems_module.Metadata:
		gems := func(list []rubygems_module.Dependency) []PackageDep {
			deps := make([]PackageDep, 0, len(list))
			for _, d := range list {
				req := make([]string, 0, len(d.Version))
				for _, v := range d.Version {
					req = append(req, strings.TrimSpace(v.Restriction+" "+v.Version))
				}
				deps = append(deps, PackageDep{Name: d.Name, Version: strings.Join(req, ", ")})
			}
			return deps
		}
		add("runtime", gems(m.RuntimeDependencies))
		add("development", gems(m.DevelopmentDependencies))
	}
	return out
}

// packageDepCount is how many dependencies the groups hold together.
func packageDepCount(groups []PackageDepGroup) int {
	n := 0
	for _, g := range groups {
		n += len(g.Deps)
	}
	return n
}

// preparePackageTabs fills the tab bar of a package page. withVersion is false on the list of versions, which
// belongs to no version: its Install, Dependencies and Settings tabs go through the latest version.
func preparePackageTabs(ctx *context.Context, pd *packages_model.PackageDescriptor, active string, withVersion bool, deps int) {
	base := pd.PackageWebLink()
	install, depsHref, settings := base, base+"?tab="+packageTabDependencies, base+"?tab="+packageTabSettings
	if withVersion {
		install = pd.VersionWebLink()
		depsHref = install + "?tab=" + packageTabDependencies
		settings = install + "/settings"
	}
	tabs := []PackageTab{
		{Key: packageTabInstall, LabelKey: "asc_work.pkg_tab_install", Href: install},
		{Key: packageTabVersions, LabelKey: "asc_work.pkg_tab_versions", Href: base + "/versions"},
		{Key: packageTabDependencies, LabelKey: "asc_work.pkg_tab_dependencies", Href: depsHref, Count: deps},
	}
	if canWrite, _ := ctx.Data["CanWritePackages"].(bool); canWrite {
		tabs = append(tabs, PackageTab{Key: packageTabSettings, LabelKey: "asc_work.pkg_tab_settings", Href: settings})
	}
	for i := range tabs {
		tabs[i].Active = tabs[i].Key == active
	}
	ctx.Data["PackageTabs"] = tabs
	ctx.Data["PackageTab"] = active
}

// packageViewTab is the view of a version the address asks for: Dependencies, or Install for anything else.
func packageViewTab(ctx *context.Context) string {
	if ctx.FormString("tab") == packageTabDependencies {
		return packageTabDependencies
	}
	return packageTabInstall
}

// latestVersionTabLink is where the address of a package without a version sends a tab: the latest version's
// Dependencies view or settings, or the version itself.
func latestVersionTabLink(ctx *context.Context, pd *packages_model.PackageDescriptor) string {
	switch ctx.FormString("tab") {
	case packageTabDependencies:
		return pd.VersionWebLink() + "?tab=" + packageTabDependencies
	case packageTabSettings:
		return pd.VersionWebLink() + "/settings"
	}
	return pd.VersionWebLink()
}
