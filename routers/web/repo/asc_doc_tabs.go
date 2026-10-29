// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"bytes"
	"html/template"
	"io"
	"strings"

	"forgejo.org/modules/charset"
	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
	"forgejo.org/modules/markup"
	"forgejo.org/services/context"
)

// The README / AGENTS.md / License tabs of the repository home (board Repo). The README is the card the home
// always had; this adds the other two documents of the root directory as panels the tabs switch to. They are read
// from the tree being viewed, so a viewer sees only what the code unit already shows them.

// docTabMaxBytes bounds one extra document; a larger file is left out of the tabs.
const docTabMaxBytes = 256 * 1024

// homeDocTab is one extra document of the home card.
type homeDocTab struct {
	ID      string
	Label   string
	File    string
	Content template.HTML
}

// licenseFileNames are the root files taken for the License tab, compared in lower case.
var licenseFileNames = map[string]bool{
	"license": true, "license.md": true, "license.txt": true, "licence": true, "licence.md": true, "licence.txt": true,
	"copying": true, "copying.md": true, "copying.txt": true, "unlicense": true,
}

// prepareHomeDocTabs sets HomeDocTabs for the root directory of the repository.
func prepareHomeDocTabs(ctx *context.Context, entries git.Entries) {
	if ctx.Repo.TreePath != "" || ctx.Data["ReadmeExist"] != true {
		return
	}
	var agents, license *git.TreeEntry
	for _, e := range entries {
		if e.IsDir() || e.IsSubmodule() {
			continue
		}
		name := strings.ToLower(e.Name())
		switch {
		case name == "agents.md" && agents == nil:
			agents = e
		case licenseFileNames[name] && license == nil:
			license = e
		}
	}
	var tabs []*homeDocTab
	if t := renderDocTab(ctx, agents, "agents", ctx.Locale.TrString("asc_rp.tab_agents_md"), true); t != nil {
		tabs = append(tabs, t)
	}
	if t := renderDocTab(ctx, license, "license", ctx.Locale.TrString("asc_rp.tab_license"), false); t != nil {
		tabs = append(tabs, t)
	}
	if len(tabs) > 0 {
		ctx.Data["HomeDocTabs"] = tabs
	}
}

// renderDocTab reads one root file and renders it as markup (when markdown is true and the name is markup) or as
// escaped plain text. A file that cannot be read, is too large, or is not text is left out.
func renderDocTab(ctx *context.Context, entry *git.TreeEntry, id, label string, markdown bool) *homeDocTab {
	if entry == nil || entry.IsLink() || entry.Size() > docTabMaxBytes {
		return nil
	}
	buf, dataRc, fInfo, err := getFileReader(ctx, ctx.Repo.Repository.ID, entry.Blob())
	if err != nil {
		log.Error("doc tab %s: %v", entry.Name(), err)
		return nil
	}
	defer dataRc.Close()
	if !fInfo.isTextFile || fInfo.isLFSFile {
		return nil
	}
	rd := charset.ToUTF8WithFallbackReader(io.MultiReader(bytes.NewReader(buf), dataRc), charset.ConvertOpts{})
	tab := &homeDocTab{ID: id, Label: label, File: entry.Name()}
	if markdown && markup.Type(entry.Name()) != "" {
		_, out, err := markupRender(ctx, &markup.RenderContext{
			Ctx:          ctx,
			RelativePath: entry.Name(),
			Links:        markup.Links{Base: ctx.Repo.RepoLink, BranchPath: ctx.Repo.BranchNameSubURL()},
			Metas:        ctx.Repo.Repository.ComposeDocumentMetas(ctx),
			GitRepo:      ctx.Repo.GitRepo,
		}, rd)
		if err != nil {
			log.Error("doc tab %s: render: %v", entry.Name(), err)
			return nil
		}
		tab.Content = out
		return tab
	}
	content, err := io.ReadAll(io.LimitReader(rd, docTabMaxBytes))
	if err != nil {
		return nil
	}
	tab.Content = template.HTML("<pre class=\"rp-doc-plain\">" + template.HTMLEscapeString(string(content)) + "</pre>")
	return tab
}
