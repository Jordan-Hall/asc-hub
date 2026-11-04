// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package user

import (
	"encoding/json"
	"strings"
	"testing"

	packages_model "forgejo.org/models/packages"
	"forgejo.org/models/perm"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	arch_model "forgejo.org/modules/packages/arch"
	cargo_module "forgejo.org/modules/packages/cargo"
	maven_module "forgejo.org/modules/packages/maven"
	npm_module "forgejo.org/modules/packages/npm"
	nuget_module "forgejo.org/modules/packages/nuget"
	rubygems_module "forgejo.org/modules/packages/rubygems"
	"forgejo.org/modules/templates"
	"forgejo.org/modules/test"
	"forgejo.org/services/context"
	"forgejo.org/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackageDepGroups(t *testing.T) {
	type row struct{ key, names string }
	flat := func(groups []PackageDepGroup) []row {
		out := make([]row, 0, len(groups))
		for _, g := range groups {
			n := make([]string, 0, len(g.Deps))
			for _, d := range g.Deps {
				n = append(n, d.Name+"@"+d.Version)
			}
			out = append(out, row{strings.TrimPrefix(g.TitleKey, "asc_work.pkg_deps_"), strings.Join(n, " ")})
		}
		return out
	}
	assert.Nil(t, packageDepGroups(nil))
	assert.Nil(t, packageDepGroups(&packages_model.PackageDescriptor{}))

	npm := packageDepGroups(&packages_model.PackageDescriptor{Metadata: &npm_module.Metadata{
		Dependencies:         map[string]string{"zod": "^3", "axios": "^1.6"},
		PeerDependencies:     map[string]string{"react": ">=18"},
		BundleDependencies:   []string{"left-pad", " "},
		OptionalDependencies: map[string]string{},
	}})
	assert.Equal(t, []row{{"runtime", "axios@^1.6 zod@^3"}, {"peer", "react@>=18"}, {"bundled", "left-pad@"}}, flat(npm), "map names are sorted, empty groups and blank names left out")
	assert.Equal(t, 4, packageDepCount(npm))
	assert.False(t, npm[0].HasNote)

	cargo := packageDepGroups(&packages_model.PackageDescriptor{Metadata: &cargo_module.Metadata{Dependencies: []*cargo_module.Dependency{
		{Name: "serde", Req: "^1", Kind: "normal"},
		{Name: "tokio", Req: "^1", Kind: "normal", Optional: true},
		{Name: "proptest", Req: "^1", Kind: "dev"},
		{Name: "cc", Req: "^1", Kind: "build"},
		nil,
	}}})
	assert.Equal(t, []row{{"runtime", "serde@^1 tokio@^1"}, {"development", "proptest@^1"}, {"build", "cc@^1"}}, flat(cargo))
	assert.True(t, cargo[0].HasNote, "an optional dependency adds the note column")
	assert.True(t, cargo[0].Deps[1].Optional)
	assert.False(t, cargo[1].HasNote)

	nuget := packageDepGroups(&packages_model.PackageDescriptor{Metadata: &nuget_module.Metadata{Dependencies: map[string][]nuget_module.Dependency{
		"net8.0":         {{ID: "Serilog", Version: "3.1.1"}},
		"netstandard2.0": {{ID: "System.Memory", Version: "4.5.5"}},
	}}})
	require.Len(t, nuget, 1)
	assert.Equal(t, PackageDep{Name: "Serilog", Version: "3.1.1", Note: "net8.0"}, nuget[0].Deps[0])
	assert.Equal(t, "netstandard2.0", nuget[0].Deps[1].Note)
	assert.True(t, nuget[0].HasNote)

	gems := packageDepGroups(&packages_model.PackageDescriptor{Metadata: &rubygems_module.Metadata{
		RuntimeDependencies: []rubygems_module.Dependency{{Name: "rack", Version: []rubygems_module.VersionRequirement{{Restriction: ">=", Version: "2.0"}, {Restriction: "<", Version: "4"}}}},
	}})
	assert.Equal(t, []row{{"runtime", "rack@>= 2.0, < 4"}}, flat(gems))

	maven := packageDepGroups(&packages_model.PackageDescriptor{Metadata: &maven_module.Metadata{Dependencies: []*maven_module.Dependency{{GroupID: "org.example", ArtifactID: "core", Version: "1.2"}}}})
	assert.Equal(t, []row{{"runtime", "org.example:core@1.2"}}, flat(maven))

	arch := packageDepGroups(&packages_model.PackageDescriptor{Metadata: &arch_model.VersionMetadata{Depends: []string{"glibc"}, CheckDepends: []string{"python"}}})
	assert.Equal(t, []row{{"runtime", "glibc@"}, {"check", "python@"}}, flat(arch))
}

// pkgTabsEnv is a package of user2 with two versions; the newer one declares dependencies.
type pkgTabsEnv struct {
	owner *user_model.User
	pd    *packages_model.PackageDescriptor // 1.1.0
	plain *packages_model.PackageDescriptor // 1.0.0, without dependencies
}

func pkgTabsSetup(t *testing.T) *pkgTabsEnv {
	t.Helper()
	require.NoError(t, unittest.PrepareTestDatabase())
	owner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	p, err := packages_model.TryInsertPackage(t.Context(), &packages_model.Package{OwnerID: owner.ID, Type: packages_model.TypeNpm, Name: "orbit-ui", LowerName: "orbit-ui"})
	require.NoError(t, err)
	version := func(v string, meta *npm_module.Metadata) *packages_model.PackageDescriptor {
		raw, err := json.Marshal(meta)
		require.NoError(t, err)
		pv, err := packages_model.GetOrInsertVersion(t.Context(), &packages_model.PackageVersion{PackageID: p.ID, CreatorID: owner.ID, Version: v, LowerVersion: v, MetadataJSON: string(raw)})
		require.NoError(t, err)
		pd, err := packages_model.GetPackageDescriptor(t.Context(), pv)
		require.NoError(t, err)
		return pd
	}
	plain := version("1.0.0", &npm_module.Metadata{})
	pd := version("1.1.0", &npm_module.Metadata{
		Dependencies:            map[string]string{"zod": "^3", "axios": "^1.6"},
		DevelopmentDependencies: map[string]string{"vitest": ""},
	})
	return &pkgTabsEnv{owner: owner, pd: pd, plain: plain}
}

func (e *pkgTabsEnv) ctx(t *testing.T, path string, pd *packages_model.PackageDescriptor, mode perm.AccessMode, render bool) *context.Context {
	t.Helper()
	opt := contexttest.MockContextOption{}
	if render {
		opt.Render = templates.HTMLRenderer()
	}
	ctx, _ := contexttest.MockContext(t, path, opt)
	contexttest.LoadUser(t, ctx, 2)
	ctx.ContextUser = e.owner
	ctx.Package = &context.Package{Owner: e.owner, AccessMode: mode, Descriptor: pd}
	ctx.SetParams(":type", "npm")
	ctx.SetParams(":name", "orbit-ui")
	return ctx
}

func pkgTabsOf(t *testing.T, ctx *context.Context) map[string]PackageTab {
	t.Helper()
	tabs, ok := ctx.Data["PackageTabs"].([]PackageTab)
	require.True(t, ok, "the page has the tab bar")
	out := map[string]PackageTab{}
	keys := make([]string, 0, len(tabs))
	for _, tab := range tabs {
		out[tab.Key] = tab
		keys = append(keys, tab.Key)
	}
	out["_order"] = PackageTab{Key: strings.Join(keys, ",")}
	return out
}

func activeTab(tabs map[string]PackageTab) string {
	active := ""
	for k, tab := range tabs {
		if tab.Active {
			active += k
		}
	}
	return active
}

func TestPackagePageTabs(t *testing.T) {
	e := pkgTabsSetup(t)
	link := "/user2/-/packages/npm/orbit-ui"

	t.Run("install", func(t *testing.T) {
		ctx := e.ctx(t, link+"/1.1.0", e.pd, perm.AccessModeWrite, false)
		ViewPackageVersion(ctx)
		tabs := pkgTabsOf(t, ctx)
		assert.Equal(t, "install,versions,dependencies,settings", tabs["_order"].Key)
		assert.Equal(t, "install", activeTab(tabs))
		assert.Equal(t, link+"/1.1.0", tabs["install"].Href)
		assert.Equal(t, link+"/versions", tabs["versions"].Href)
		assert.Equal(t, link+"/1.1.0?tab=dependencies", tabs["dependencies"].Href)
		assert.Equal(t, link+"/1.1.0/settings", tabs["settings"].Href)
		assert.Equal(t, 3, tabs["dependencies"].Count)
		assert.Equal(t, "install", ctx.Data["PackageTab"])

		ctx = e.ctx(t, link+"/1.1.0?tab=nonsense", e.pd, perm.AccessModeRead, false)
		ViewPackageVersion(ctx)
		tabs = pkgTabsOf(t, ctx)
		assert.Equal(t, "install,versions,dependencies", tabs["_order"].Key, "a reader has no Settings tab")
		assert.Equal(t, "install", activeTab(tabs), "an unknown tab is Install")
	})

	t.Run("dependencies", func(t *testing.T) {
		ctx := e.ctx(t, link+"/1.1.0?tab=dependencies", e.pd, perm.AccessModeWrite, true)
		ViewPackageVersion(ctx)
		assert.Equal(t, "dependencies", activeTab(pkgTabsOf(t, ctx)))
		assert.Equal(t, "dependencies", ctx.Data["PackageTab"])

		out, err := ctx.RenderToHTML("package/tabs", ctx.Data)
		require.NoError(t, err)
		bar := string(out)
		assert.Contains(t, bar, `data-testid="pkg-tabs"`)
		assert.Contains(t, bar, `href="`+link+`/1.1.0?tab=dependencies" aria-current="page"`)
		assert.Equal(t, 1, strings.Count(bar, `aria-current="page"`))
		assert.Contains(t, bar, `<span class="ds-count">3</span>`)
		assert.Contains(t, bar, "asc_work.pkg_tab_settings")

		out, err = ctx.RenderToHTML("package/dependencies", ctx.Data)
		require.NoError(t, err)
		deps := string(out)
		assert.Equal(t, 2, strings.Count(deps, `data-testid="pkg-deps-group"`))
		assert.Contains(t, deps, "asc_work.pkg_deps_runtime")
		assert.Contains(t, deps, "asc_work.pkg_deps_development")
		assert.Less(t, strings.Index(deps, ">axios<"), strings.Index(deps, ">zod<"), "sorted by name")
		assert.Contains(t, deps, `<span class="ds-mono">^1.6</span>`)
		assert.Contains(t, deps, "asc_work.pkg_deps_any", "a dependency without a requirement says so")
		assert.NotContains(t, deps, "asc_work.pkg_deps_note", "no note column when no dependency has one")
		assert.NotContains(t, deps, "pkg-deps-empty")
	})

	t.Run("dependencies of a version without any", func(t *testing.T) {
		ctx := e.ctx(t, link+"/1.0.0?tab=dependencies", e.plain, perm.AccessModeRead, true)
		ViewPackageVersion(ctx)
		assert.Equal(t, 0, pkgTabsOf(t, ctx)["dependencies"].Count)
		out, err := ctx.RenderToHTML("package/dependencies", ctx.Data)
		require.NoError(t, err)
		assert.Contains(t, string(out), `data-testid="pkg-deps-empty"`)
		assert.Contains(t, string(out), "asc_work.pkg_deps_none")
	})

	t.Run("versions", func(t *testing.T) {
		ctx := e.ctx(t, link+"/versions", nil, perm.AccessModeWrite, false)
		ListPackageVersions(ctx)
		tabs := pkgTabsOf(t, ctx)
		assert.Equal(t, "versions", activeTab(tabs))
		assert.Equal(t, link, tabs["install"].Href, "the list belongs to no version: the tabs go through the latest")
		assert.Equal(t, link+"?tab=dependencies", tabs["dependencies"].Href)
		assert.Equal(t, link+"?tab=settings", tabs["settings"].Href)
		assert.Zero(t, tabs["dependencies"].Count)

		ctx = e.ctx(t, link+"/versions", nil, perm.AccessModeRead, false)
		ListPackageVersions(ctx)
		assert.NotContains(t, pkgTabsOf(t, ctx), "settings")
	})

	t.Run("settings", func(t *testing.T) {
		ctx := e.ctx(t, link+"/1.1.0/settings", e.pd, perm.AccessModeWrite, false)
		PackageSettings(ctx)
		tabs := pkgTabsOf(t, ctx)
		assert.Equal(t, "settings", activeTab(tabs))
		assert.Equal(t, 3, tabs["dependencies"].Count)
	})

	t.Run("the latest version keeps the tab", func(t *testing.T) {
		for query, want := range map[string]string{
			"":                   link + "/1.1.0",
			"?tab=dependencies":  link + "/1.1.0?tab=dependencies",
			"?tab=settings":      link + "/1.1.0/settings",
			"?tab=https://x.org": link + "/1.1.0",
		} {
			ctx := e.ctx(t, link+query, nil, perm.AccessModeWrite, false)
			RedirectToLastVersion(ctx)
			assert.Equal(t, want, test.RedirectURL(ctx.Resp), query)
		}
	})
}
