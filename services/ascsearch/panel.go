// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsearch

import (
	"context"
	"errors"

	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	api "forgejo.org/modules/structs"
)

// PanelQuery asks for the symbol panel of the file view: the definition of a symbol, its references and
// callers in the repository and, on request, its references in other repositories.
type PanelQuery struct {
	Repo Repo
	// Rev is the revision to read, "refs/heads/<branch>" or a commit id; empty means the default branch.
	Rev string
	// Name is the symbol. Path and Line pick one definition when the name is ambiguous.
	Name string
	Path string
	Line uint32
	// CrossRepo asks for references in other repositories. Allow then decides, for each repository asc
	// names, whether the caller may read it; without Allow no other repository is ever shown.
	CrossRepo bool
	Allow     func(ascName string) bool
	// Others are the readable repositories to ask one by one when asc lacks the panel service; at most
	// MaxSemanticRepos of them are asked.
	Others []Repo
	Limit  int
}

// FileRefs groups symbol occurrences by file.
type FileRefs = api.AscFileRefs

// Panel is the answer.
type Panel = api.AscSymbolPanel

const (
	maxPanelFiles   = 100
	maxPanelSymbols = 50
)

func (q *PanelQuery) check() error {
	if q.Name == "" && (q.Path == "" || q.Line == 0) {
		return input("name, or path and line, is required")
	}
	if !Plain(q.Name, MaxName) {
		return input("bad name")
	}
	if _, ok := CheckPath(q.Path); !ok {
		return input("bad path")
	}
	if q.Line > maxLine {
		return input("bad line")
	}
	if q.Rev != "" && !Plain(q.Rev, 255) {
		return input("bad rev")
	}
	return nil
}

func (q *PanelQuery) limit() int {
	switch {
	case q.Limit <= 0:
		return DefaultLimit * 4
	case q.Limit > MaxLimit*5:
		return MaxLimit * 5
	}
	return q.Limit
}

// group turns symbol occurrences into per-file lists, keeping the order of first appearance.
func group(repo string, syms []Symbol) []FileRefs {
	var out []FileRefs
	idx := map[string]int{}
	for _, s := range syms {
		i, ok := idx[s.Path]
		if !ok {
			if len(out) >= maxPanelFiles {
				continue
			}
			i = len(out)
			idx[s.Path] = i
			out = append(out, FileRefs{Repo: repo, Path: s.Path, Symbols: []Symbol{}})
		}
		if len(out[i].Symbols) < maxPanelSymbols {
			out[i].Symbols = append(out[i].Symbols, s)
		}
	}
	return out
}

// groupAsc converts and groups the references asc sent, keeping only repositories keep accepts.
func groupAsc(refs []*ascv1.SymbolReferences, keep func(repo string) bool) []FileRefs {
	var out []FileRefs
	for _, r := range refs {
		name := r.GetRepo().GetName()
		if !keep(name) {
			continue
		}
		path, ok := Path(r.GetPath())
		if !ok {
			continue
		}
		fr := FileRefs{Repo: name, Path: path, Symbols: []Symbol{}}
		for _, s := range r.GetReferences() {
			if len(fr.Symbols) >= maxPanelSymbols {
				break
			}
			if sym, ok := SymbolOf(s); ok {
				fr.Symbols = append(fr.Symbols, sym)
			}
		}
		if len(fr.Symbols) > 0 && len(out) < maxPanelFiles {
			out = append(out, fr)
		}
	}
	return out
}

// Panel builds the symbol panel.
func (s *Service) Panel(ctx context.Context, q PanelQuery) (*Panel, error) {
	if err := q.check(); err != nil {
		return nil, err
	}
	if q.Rev == "" {
		q.Rev = q.Repo.Rev
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	crossRepo := q.CrossRepo && q.Allow != nil

	resp, err := s.Cross.SymbolPanel(ctx, &ascv1.SymbolPanelRequest{
		Repo: &ascv1.RepoRef{Name: q.Repo.Name, IdMode: ascv1.IdMode_ID_MODE_GIT}, Rev: q.Rev, Name: q.Name,
		Path: []byte(q.Path), Line: q.Line, CrossRepo: crossRepo,
		Budget: &ascv1.Budget{MaxResults: uint32(q.limit())},
	})
	if err != nil {
		if unimplemented(err) && q.Name != "" {
			return s.panelFallback(ctx, q, crossRepo)
		}
		return nil, err
	}

	same := func(name string) bool { return name == q.Repo.Name }
	p := &Panel{
		Scope: ScopeOf(resp.GetScope()), Truncated: resp.GetTruncated(),
		Definitions: []SymbolHit{}, References: groupAsc(resp.GetReferences(), same),
		Callers: groupAsc(resp.GetCallers(), same), CrossRepo: []FileRefs{},
	}
	for _, d := range resp.GetDefinitions() {
		// Definitions are in the repository asked about; a definition elsewhere is a cross-repository one
		// and shown only when the caller may read that repository.
		name := d.GetRepo().GetName()
		if !same(name) && !(crossRepo && q.Allow(name)) {
			continue
		}
		if sym, ok := SymbolOf(d.GetSymbol()); ok {
			p.Definitions = append(p.Definitions, SymbolHit{Repo: name, Rev: Clean([]byte(d.GetRev()), 255), Symbol: sym, Score: finite(d.GetScore())})
		}
	}
	if crossRepo {
		p.CrossRepo = groupAsc(resp.GetCrossRepo(), func(name string) bool { return name != q.Repo.Name && q.Allow(name) })
	}
	return p, nil
}

// panelFallback builds the panel from CodeIntel.Symbols calls, for an asc that lacks the panel service.
func (s *Service) panelFallback(ctx context.Context, q PanelQuery, crossRepo bool) (*Panel, error) {
	scope := func(r Repo, rev string) *ascv1.Scope {
		return &ascv1.Scope{Repo: &ascv1.RepoRef{Name: r.Name, IdMode: ascv1.IdMode_ID_MODE_GIT}, At: &ascv1.Scope_Rev{Rev: rev}}
	}
	call := func(r Repo, rev string, op ascv1.SymbolOp) (*ascv1.SymbolsResponse, error) {
		req := &ascv1.SymbolsRequest{
			Scope: scope(r, rev), Op: op, Name: q.Name, Budget: &ascv1.Budget{MaxResults: uint32(q.limit())},
		}
		if op == ascv1.SymbolOp_SYMBOL_OP_DEF || op == ascv1.SymbolOp_SYMBOL_OP_REFS {
			req.DefPath, req.DefLine = []byte(q.Path), q.Line
		}
		return s.Intel.Symbols(ctx, req)
	}
	collect := func(resp *ascv1.SymbolsResponse) []Symbol {
		var out []Symbol
		for _, x := range resp.GetSymbols() {
			if sym, ok := SymbolOf(x); ok {
				out = append(out, sym)
			}
		}
		return out
	}

	p := &Panel{Fallback: true, Definitions: []SymbolHit{}, References: []FileRefs{}, Callers: []FileRefs{}, CrossRepo: []FileRefs{}}
	def, err := call(q.Repo, q.Rev, ascv1.SymbolOp_SYMBOL_OP_DEF)
	if err != nil {
		return nil, err
	}
	p.Scope, p.Truncated = ScopeOf(def.GetScope()), def.GetTruncated()
	for _, sym := range collect(def) {
		p.Definitions = append(p.Definitions, SymbolHit{Repo: q.Repo.Name, Rev: q.Rev, Symbol: sym, Score: 1})
	}
	// The references and callers add to the panel; their failure does not take the definition away.
	if refs, err := call(q.Repo, q.Rev, ascv1.SymbolOp_SYMBOL_OP_REFS); err == nil {
		p.References = group(q.Repo.Name, collect(refs))
		p.Truncated = p.Truncated || refs.GetTruncated()
	} else if errors.Is(err, ascclient.ErrNoCredential) {
		return nil, err
	}
	if callers, err := call(q.Repo, q.Rev, ascv1.SymbolOp_SYMBOL_OP_CALLERS); err == nil {
		p.Callers = group(q.Repo.Name, collect(callers))
		p.Truncated = p.Truncated || callers.GetTruncated()
	} else if errors.Is(err, ascclient.ErrNoCredential) {
		return nil, err
	}
	if crossRepo {
		others := make([]Repo, 0, MaxSemanticRepos)
		for _, o := range q.Others {
			if o.Name != q.Repo.Name && len(others) < MaxSemanticRepos {
				others = append(others, o)
			}
		}
		type one struct {
			syms []Symbol
			cut  bool
		}
		out := make([]one, len(others))
		parallel(len(others), fanOut, func(i int) {
			if !q.Allow(others[i].Name) {
				return
			}
			// Another repository is asked by name only: a position in this one means nothing there.
			resp, err := s.Intel.Symbols(ctx, &ascv1.SymbolsRequest{
				Scope: scope(others[i], others[i].Rev), Op: ascv1.SymbolOp_SYMBOL_OP_REFS, Name: q.Name,
				Budget: &ascv1.Budget{MaxResults: uint32(q.limit())},
			})
			if err == nil {
				out[i] = one{collect(resp), resp.GetTruncated()}
			}
		})
		for i, o := range out {
			p.CrossRepo = append(p.CrossRepo, group(others[i].Name, o.syms)...)
			p.Truncated = p.Truncated || o.cut
		}
		if len(p.CrossRepo) > maxPanelFiles {
			p.CrossRepo, p.Truncated = p.CrossRepo[:maxPanelFiles], true
		}
	}
	return p, nil
}
