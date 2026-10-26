// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func refs(repo, path string, syms ...*ascv1.Symbol) *ascv1.SymbolReferences {
	return &ascv1.SymbolReferences{Repo: ref(repo), Path: []byte(path), References: syms}
}

func TestPanelCross(t *testing.T) {
	f := &fakeAsc{symbolPanel: func(req *ascv1.SymbolPanelRequest) (*ascv1.SymbolPanelResponse, error) {
		return &ascv1.SymbolPanelResponse{
			Scope: &ascv1.ResolvedScope{Coverage: 1, Generation: 4},
			Definitions: []*ascv1.SymbolHit{
				{Repo: ref("o/r1"), Symbol: sym("Retry", "a.go", 3, true, "precise"), Score: 1},
				{Repo: ref("o/r5"), Symbol: sym("Retry", "d.go", 3, true, "name_match"), Score: 0.5},
				{Repo: ref("hidden/x"), Symbol: sym("Retry", "e.go", 3, true, "name_match"), Score: 0.5},
			},
			References: []*ascv1.SymbolReferences{
				refs("o/r1", "b.go", sym("Retry", "b.go", 5, false, "precise"), sym("Retry", "b.go", 9, false, "precise")),
				refs("o/r2", "other.go", sym("Retry", "other.go", 1, false, "name_match")),
			},
			Callers: []*ascv1.SymbolReferences{refs("o/r1", "c.go", sym("Do", "c.go", 7, true, "import_match"))},
			CrossRepo: []*ascv1.SymbolReferences{
				refs("o/r5", "use.go", sym("Retry", "use.go", 2, false, "name_match")),
				refs("hidden/x", "leak.go", sym("Retry", "leak.go", 2, false, "name_match")),
				refs("o/r1", "self.go", sym("Retry", "self.go", 2, false, "name_match")),
			},
			Truncated: true,
		}, nil
	}}
	svc := startFake(t, f)
	allow := func(n string) bool { return n == "o/r5" || n == "o/r1" }

	p, err := svc.Panel(sys(t), PanelQuery{Repo: repoN(1), Name: "Retry", Path: "a.go", Line: 3, CrossRepo: true, Allow: allow})
	require.NoError(t, err)
	req := f.panel[0]
	assert.Equal(t, "o/r1", req.GetRepo().GetName())
	assert.Equal(t, "refs/heads/main", req.GetRev())
	assert.Equal(t, "Retry", req.GetName())
	assert.Equal(t, "a.go", string(req.GetPath()))
	assert.EqualValues(t, 3, req.GetLine())
	assert.True(t, req.GetCrossRepo())

	require.Len(t, p.Definitions, 2, "the hidden repository's definition is dropped")
	assert.Equal(t, "o/r1", p.Definitions[0].Repo)
	assert.Equal(t, "o/r5", p.Definitions[1].Repo)
	require.Len(t, p.References, 1, "a repository other than the one asked about is not a reference of it")
	assert.Equal(t, "b.go", p.References[0].Path)
	assert.Len(t, p.References[0].Symbols, 2)
	require.Len(t, p.Callers, 1)
	require.Len(t, p.CrossRepo, 1, "hidden repositories and the repository itself are dropped from the cross-repository list")
	assert.Equal(t, "o/r5", p.CrossRepo[0].Repo)
	assert.True(t, p.Truncated)
	assert.EqualValues(t, 4, p.Scope.Generation)
	assert.False(t, p.Fallback)
}

func TestPanelCrossRepoNeedsAllow(t *testing.T) {
	f := &fakeAsc{symbolPanel: func(req *ascv1.SymbolPanelRequest) (*ascv1.SymbolPanelResponse, error) {
		return &ascv1.SymbolPanelResponse{
			Definitions: []*ascv1.SymbolHit{{Repo: ref("o/r9"), Symbol: sym("Retry", "e.go", 3, true, "precise")}},
			CrossRepo:   []*ascv1.SymbolReferences{refs("o/r9", "x.go", sym("Retry", "x.go", 1, false, "name_match"))},
		}, nil
	}}
	svc := startFake(t, f)
	p, err := svc.Panel(sys(t), PanelQuery{Repo: repoN(1), Name: "Retry", CrossRepo: true})
	require.NoError(t, err)
	assert.False(t, f.panel[0].GetCrossRepo(), "without a way to decide, other repositories are not even asked for")
	assert.Empty(t, p.CrossRepo, "and nothing from them is shown if asc sends it anyway")
	assert.Empty(t, p.Definitions)

	p, err = svc.Panel(sys(t), PanelQuery{Repo: repoN(1), Name: "Retry", CrossRepo: false, Allow: func(string) bool { return true }})
	require.NoError(t, err)
	assert.False(t, f.panel[1].GetCrossRepo())
	assert.Empty(t, p.CrossRepo)
}

func TestPanelChecks(t *testing.T) {
	f := &fakeAsc{symbolPanel: func(*ascv1.SymbolPanelRequest) (*ascv1.SymbolPanelResponse, error) {
		return &ascv1.SymbolPanelResponse{}, nil
	}}
	svc := startFake(t, f)
	for name, q := range map[string]PanelQuery{
		"nothing":  {Repo: repoN(1)},
		"no line":  {Repo: repoN(1), Path: "a.go"},
		"control":  {Repo: repoN(1), Name: "a\n"},
		"path":     {Repo: repoN(1), Name: "a", Path: "../x"},
		"line":     {Repo: repoN(1), Name: "a", Line: 1 << 30},
		"bad rev":  {Repo: repoN(1), Name: "a", Rev: "a\nb"},
		"long rev": {Repo: repoN(1), Name: "a", Rev: string(make([]byte, 300))},
	} {
		_, err := svc.Panel(sys(t), q)
		require.ErrorIs(t, err, ErrInput, name)
	}
	assert.Empty(t, f.panel)

	// A position alone is a valid question for the panel service.
	_, err := svc.Panel(sys(t), PanelQuery{Repo: repoN(1), Path: "a.go", Line: 4})
	require.NoError(t, err)

	f.symbolPanel = func(*ascv1.SymbolPanelRequest) (*ascv1.SymbolPanelResponse, error) {
		return nil, status.Error(codes.NotFound, "no")
	}
	_, err = svc.Panel(sys(t), PanelQuery{Repo: repoN(1), Name: "a"})
	assert.Equal(t, KindNotFound, KindOf(err))
}

func TestPanelFallback(t *testing.T) {
	f := &fakeAsc{symbols: func(req *ascv1.SymbolsRequest) (*ascv1.SymbolsResponse, error) {
		name := req.GetScope().GetRepo().GetName()
		switch {
		case name == "o/r1" && req.GetOp() == ascv1.SymbolOp_SYMBOL_OP_DEF:
			return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{sym("Retry", "a.go", 3, true, "precise")}, Scope: &ascv1.ResolvedScope{Coverage: 1}}, nil
		case name == "o/r1" && req.GetOp() == ascv1.SymbolOp_SYMBOL_OP_REFS:
			return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{
				sym("Retry", "b.go", 5, false, "precise"), sym("Retry", "c.go", 1, false, "name_match"), sym("Retry", "b.go", 9, false, "precise"),
			}, Truncated: true}, nil
		case name == "o/r1" && req.GetOp() == ascv1.SymbolOp_SYMBOL_OP_CALLERS:
			return nil, status.Error(codes.Internal, "boom")
		case name == "o/r2":
			return &ascv1.SymbolsResponse{Symbols: []*ascv1.Symbol{sym("Retry", "x.go", 2, false, "name_match")}}, nil
		}
		return nil, status.Error(codes.PermissionDenied, "no")
	}}
	svc := startFake(t, f) // no panel service

	allow := func(n string) bool { return n != "o/r3" }
	p, err := svc.Panel(sys(t), PanelQuery{
		Repo: repoN(1), Name: "Retry", Path: "a.go", Line: 3, CrossRepo: true, Allow: allow,
		Others: []Repo{repoN(1), repoN(2), repoN(3), repoN(4)},
	})
	require.NoError(t, err)
	assert.True(t, p.Fallback)
	require.Len(t, p.Definitions, 1)
	require.Len(t, p.References, 2, "references are grouped by file in order of first appearance")
	assert.Equal(t, "b.go", p.References[0].Path)
	assert.Len(t, p.References[0].Symbols, 2)
	assert.Empty(t, p.Callers, "a failed callers call does not take the rest away")
	assert.True(t, p.Truncated)
	require.Len(t, p.CrossRepo, 1, "o/r1 is the repository itself, o/r3 is not allowed, o/r4 is refused")
	assert.Equal(t, "o/r2", p.CrossRepo[0].Repo)

	for _, r := range f.syms {
		if r.GetScope().GetRepo().GetName() == "o/r3" {
			t.Error("a repository the caller may not read must not even be asked")
		}
		if r.GetScope().GetRepo().GetName() == "o/r2" {
			assert.Empty(t, r.GetDefPath(), "a position in one repository means nothing in another")
		}
	}

	// Without a name the fallback cannot work.
	_, err = svc.Panel(sys(t), PanelQuery{Repo: repoN(1), Path: "a.go", Line: 3})
	assert.Equal(t, KindUnavailable, KindOf(err))

	// Without allow nothing outside the repository is asked.
	f.syms = nil
	_, err = svc.Panel(sys(t), PanelQuery{Repo: repoN(1), Name: "Retry", CrossRepo: true, Others: []Repo{repoN(2)}})
	require.NoError(t, err)
	for _, r := range f.syms {
		assert.Equal(t, "o/r1", r.GetScope().GetRepo().GetName())
	}
}
