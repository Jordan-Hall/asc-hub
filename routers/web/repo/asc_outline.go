// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	gocontext "context"
	"net/url"
	"time"

	"forgejo.org/models/unit"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
	"forgejo.org/services/context"
)

// The outline panel and the "Used in" panel of the file view (board Code). Both are read through CodeIntel.Symbols
// with the same trust rules as the symbols page (asc_symbols.go): the scope is a plain revision Forgejo resolved
// itself (the branch or the commit being viewed), the call is made only for a viewer who may read the code of an asc
// repository, and every text from asc is cut and checked by ascSymbolsBuild before it is drawn. A failure leaves the
// panels out; it never blocks the file.

const (
	// outlineTimeout bounds all calls for one file view; the file is already on its way to the browser.
	outlineTimeout = 3 * time.Second
	// outlineShown is how many symbols the panel lists; the symbols page has the rest.
	outlineShown = 40
	// usedInSymbols is how many declared symbols are asked for their references.
	usedInSymbols = 3
)

// usedInRow is a symbol of this file with the number of references to it in other files.
type usedInRow struct {
	Name  string
	Count int
	Link  string // the symbols page with the definition and the references
}

// prepareAscOutline adds the outline and the "Used in" rows of the viewed file.
func prepareAscOutline(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if !setting.Asc.Enabled || repo == nil || repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) ||
		!ctx.Repo.CanRead(unit.TypeCode) || ctx.Repo.TreePath == "" {
		return
	}
	path, ok := ascSymbolsPath(ctx.Repo.TreePath)
	if !ok || path == "" {
		return
	}
	q := ascSymbolsQuery{op: "outline", path: path}
	switch {
	case ctx.Repo.IsViewCommit && ascSymbolsSHA.MatchString(ctx.Repo.CommitID):
		q.rev, q.label = ctx.Repo.CommitID, ctx.Repo.CommitID[:10]
		q.base = ctx.Repo.RepoLink + "/src/commit/" + ctx.Repo.CommitID
	case ctx.Repo.IsViewBranch && ascSymbolsBranchOK(ctx.Repo.BranchName):
		q.rev, q.label = "refs/heads/"+ctx.Repo.BranchName, ctx.Repo.BranchName
		q.base = ctx.Repo.RepoLink + "/src/branch/" + util.PathEscapeSegments(ctx.Repo.BranchName)
	default:
		return
	}
	name, err := ascGraphRepoName(repo)
	if err != nil {
		return
	}
	conn, err := ascRPCConn()
	if err != nil {
		return
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx.Req.Context()), outlineTimeout)
	defer cancel()
	api := ascv1.NewCodeIntelClient(conn)
	scope := func() *ascv1.Scope {
		return &ascv1.Scope{Repo: &ascv1.RepoRef{Name: name, IdMode: ascv1.IdMode_ID_MODE_GIT}, At: &ascv1.Scope_Rev{Rev: q.rev}}
	}
	resp, err := api.Symbols(callCtx, &ascv1.SymbolsRequest{
		Op: ascv1.SymbolOp_SYMBOL_OP_OUTLINE, Path: []byte(q.path), Scope: scope(), Budget: &ascv1.Budget{MaxResults: ascSymbolsMaxSymbols},
	})
	if err != nil {
		log.Debug("outline of %s %s: %v", repo.FullName(), q.path, err)
		return
	}
	rows, truncated, err := ascSymbolsBuild(resp, q)
	if err != nil || len(rows) == 0 {
		return
	}
	if len(rows) > outlineShown {
		rows, truncated = rows[:outlineShown], true
	}
	ctx.Data["AscOutline"] = rows
	ctx.Data["AscOutlineMore"] = truncated

	// "Used in": the references to the first symbols the file declares, counted outside the file itself.
	var used []usedInRow
	seen := map[string]bool{}
	for _, r := range rows {
		if len(used) >= usedInSymbols {
			break
		}
		if !r.IsDef || r.Name == "" || seen[r.Name] || r.Container != "" {
			continue
		}
		seen[r.Name] = true
		refs, err := api.Symbols(callCtx, &ascv1.SymbolsRequest{
			Op: ascv1.SymbolOp_SYMBOL_OP_REFS, Name: r.Name, Scope: scope(), Budget: &ascv1.Budget{MaxResults: ascSymbolsMaxSymbols},
		})
		if err != nil {
			break
		}
		refRows, _, err := ascSymbolsBuild(refs, q)
		if err != nil {
			break
		}
		n := 0
		for _, rr := range refRows {
			if rr.Path != q.path {
				n++
			}
		}
		used = append(used, usedInRow{
			Name: r.Name, Count: n,
			Link: ctx.Repo.RepoLink + "/asc/symbols?op=def&name=" + url.QueryEscape(r.Name),
		})
	}
	if len(used) > 0 {
		ctx.Data["AscUsedIn"] = used
	}
}
