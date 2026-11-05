// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"net/http"
	"net/url"
	"strconv"

	"forgejo.org/models/unit"
	"forgejo.org/modules/ascclient"
	"forgejo.org/modules/base"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
	"forgejo.org/services/ascsearch"
	"forgejo.org/services/context"
)

// The code intelligence panel of the search page (board Search): the right column that shows the symbol a
// result was picked for. `GET /{owner}/{repo}/asc/search/panel?name=<symbol>[&at=<path>&line=<n>][&ref=]`
// answers an HTML fragment with the definition (signature, documentation, Go to definition), the references
// and the callers in this repository, and the references in other repositories the viewer can read.
//
// Trust is that of the search page: the code-read check of this repository, the internal token for asc, and
// Forgejo deciding which other repositories are shown (ascsearch.CanRead for the viewer, never asc).

const (
	tplAscSearchPanel base.TplName = "repo/asc_search_panel"

	// ascPanelMaxFiles files are listed per section, ascPanelMaxLines lines per file.
	ascPanelMaxFiles = 20
	ascPanelMaxLines = 3
)

// ascPanelSym is a definition.
type ascPanelSym struct {
	Name, Kind, Signature, Doc, Container, Confidence, Path string
	Line                                                    uint32
	Link                                                    string
}

// ascPanelLine is one occurrence in a file.
type ascPanelLine struct {
	Line uint32
	Link string
}

// ascPanelFile is a file with occurrences.
type ascPanelFile struct {
	Repo  string // the repository as Forgejo shows it, for another repository
	Path  string
	Link  string
	Count int
	Lines []ascPanelLine
}

// ascPanelView is what the fragment draws.
type ascPanelView struct {
	Name      string
	Defs      []ascPanelSym
	Refs      []ascPanelFile
	RefCount  int
	Callers   []ascPanelFile
	Cross     []ascPanelFile
	Truncated bool
	Fallback  bool
	// SymbolsLink is the full definition and references page.
	SymbolsLink string
}

func ascPanelFail(ctx *context.Context, code int) {
	ctx.Data["AscPanelError"] = code
	ctx.HTML(code, tplAscSearchPanel)
}

// AscSearchPanel serves `GET /{owner}/{repo}/asc/search/panel`.
func AscSearchPanel(ctx *context.Context) {
	repo := ctx.Repo.Repository
	if !setting.Asc.Enabled || repo == nil || repo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) || !ctx.Repo.CanRead(unit.TypeCode) {
		ctx.NotFound("AscSearchPanel", nil)
		return
	}
	q := ascSearchRawQuery(ctx)
	q.mode, q.scope, q.rawPath, q.rawLang = ascSearchModeSymbol, ascSearchScopeVersion, "", ""
	q.text = ctx.FormString("name")
	at, okAt := ascSymbolsPath(ctx.FormString("at"))
	line := ctx.FormInt("line")
	if q.text == "" || !okAt || line < 0 || line > 1<<24 {
		ascPanelFail(ctx, http.StatusBadRequest)
		return
	}
	if code, ok := ascSearchParse(ctx, &q); !ok {
		ascPanelFail(ctx, code)
		return
	}
	name, err := ascsearch.NameOf(repo)
	if err != nil {
		ctx.NotFound("AscSearchPanel", nil)
		return
	}
	svc, err := ascsearch.Default()
	if err != nil {
		log.Error("asc search panel: no connection to asc serve: %v", err)
		ascPanelFail(ctx, http.StatusBadGateway)
		return
	}
	callCtx, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx.Req.Context()), ascSearchTimeout)
	defer cancel()

	// Other repositories: only those the viewer can read, as Forgejo decides.
	readable := map[string]ascsearch.Repo{}
	allow := func(ascName string) bool {
		r, ok := ascsearch.CanRead(ctx, ctx.Doer, nil, ascName)
		if ok {
			readable[ascName] = r
		}
		return ok
	}
	others, err := ascsearch.Resolve(ctx, ctx.Doer, nil, ascsearch.Selector{Max: ascsearch.MaxSemanticRepos + 1})
	if err != nil {
		ctx.ServerError("ascsearch.Resolve", err)
		return
	}
	self := ascsearch.Repo{ID: repo.ID, Name: name, FullName: repo.FullName(), Branch: repo.DefaultBranch, Rev: q.rev, Link: ctx.Repo.RepoLink}
	panel, err := svc.Panel(callCtx, ascsearch.PanelQuery{
		Repo: self, Rev: q.rev, Name: q.text, Path: at, Line: uint32(line),
		CrossRepo: true, Allow: allow, Others: others.Repos,
	})
	if err != nil {
		ascPanelMapError(ctx, err)
		return
	}

	v := url.Values{"op": {"def"}, "name": {q.text}}
	if q.rawRef != "" {
		v.Set("ref", q.rawRef)
	}
	view := ascPanelView{
		Name: q.text, Truncated: panel.Truncated, Fallback: panel.Fallback,
		SymbolsLink: ctx.Repo.RepoLink + "/asc/symbols?" + v.Encode(),
	}
	link := func(base, path string, line uint32) string {
		l := base + "/" + util.PathEscapeSegments(path)
		if line > 0 {
			l += "#L" + strconv.FormatUint(uint64(line), 10)
		}
		return l
	}
	for _, d := range panel.Definitions {
		if d.Repo != name {
			continue // a definition in another repository is listed with the other repositories
		}
		s := d.Symbol
		view.Defs = append(view.Defs, ascPanelSym{
			Name: s.Name, Kind: s.Kind, Signature: s.Signature, Doc: s.Doc, Container: s.Container,
			Confidence: s.Confidence, Path: s.Path, Line: s.Line, Link: link(q.base, s.Path, s.Line),
		})
	}
	files := func(refs []ascsearch.FileRefs, cross bool) []ascPanelFile {
		var out []ascPanelFile
		for _, fr := range refs {
			base, repoName := q.base, ""
			if cross {
				r, ok := readable[fr.Repo]
				if !ok {
					continue
				}
				base, repoName = r.Link+"/src/branch/"+util.PathEscapeSegments(r.Branch), r.FullName
			}
			if len(out) >= ascPanelMaxFiles {
				view.Truncated = true
				break
			}
			f := ascPanelFile{Repo: repoName, Path: fr.Path, Link: link(base, fr.Path, 0), Count: len(fr.Symbols)}
			for _, s := range fr.Symbols {
				if len(f.Lines) < ascPanelMaxLines {
					f.Lines = append(f.Lines, ascPanelLine{Line: s.Line, Link: link(base, fr.Path, s.Line)})
				}
			}
			out = append(out, f)
		}
		return out
	}
	view.Refs = files(panel.References, false)
	for _, fr := range panel.References {
		view.RefCount += len(fr.Symbols)
	}
	view.Callers = files(panel.Callers, false)
	view.Cross = files(panel.CrossRepo, true)
	ctx.Data["AscPanel"] = view
	ctx.HTML(http.StatusOK, tplAscSearchPanel)
}

// ascPanelMapError answers a failed panel call with its status only.
func ascPanelMapError(ctx *context.Context, err error) {
	switch ascsearch.KindOf(err) {
	case ascsearch.KindBadRequest, ascsearch.KindTooBroad:
		ascPanelFail(ctx, http.StatusBadRequest)
	case ascsearch.KindNotFound:
		ascPanelFail(ctx, http.StatusNotFound)
	case ascsearch.KindLimited:
		ascPanelFail(ctx, http.StatusTooManyRequests)
	case ascsearch.KindTimeout:
		ascPanelFail(ctx, http.StatusGatewayTimeout)
	case ascsearch.KindUnavailable:
		ascPanelFail(ctx, http.StatusServiceUnavailable)
	default:
		log.Error("asc search panel: %v", err)
		ascPanelFail(ctx, http.StatusBadGateway)
	}
}
