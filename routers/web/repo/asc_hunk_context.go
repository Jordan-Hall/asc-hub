// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"strings"
	"time"

	issues_model "forgejo.org/models/issues"
	"forgejo.org/modules/ascclient"
	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/services/asctrust"
	"forgejo.org/services/context"
	"forgejo.org/services/gitdiff"
)

// The hunk header of a stacked change names the definitions around the hunk, outermost first ("UserList > @for (...) >
// @if (...)"), as asc's code intelligence knows them, also for a language git has no rule for (TSRX components and
// their scopes) (Review.HunkContext). Git's own header stays as it is; this is added beside it. Advice only.

const (
	ascHunkContextMaxFiles = 40
	ascHunkContextMaxLines = 200
	ascHunkContextBudget   = 4 * time.Second
	ascHunkContextMaxText  = 160
)

// ascHunkContextText is the chain of definitions as one line, or "".
func ascHunkContextText(chain []*ascv1.HunkContextEntry) string {
	var parts []string
	for _, e := range chain {
		if n := asctrust.CleanText(e.GetName(), 80); n != "" {
			parts = append(parts, n)
		}
	}
	text := strings.Join(parts, " > ")
	if len(text) > ascHunkContextMaxText {
		text = text[:ascHunkContextMaxText-3] + "..."
	}
	return text
}

// ascHunkStarts are the lines (of the newer side) the hunks of the file start at, without repeats, at most the limit.
func ascHunkStarts(f *gitdiff.DiffFile) []uint32 {
	var out []uint32
	seen := map[uint32]bool{}
	for _, s := range f.Sections {
		for _, l := range s.Lines {
			if l.Type == gitdiff.DiffLineSection && l.SectionInfo != nil && l.SectionInfo.RightIdx > 0 && len(out) < ascHunkContextMaxLines {
				n := uint32(l.SectionInfo.RightIdx)
				if !seen[n] {
					seen[n] = true
					out = append(out, n)
				}
			}
		}
	}
	return out
}

// prepareAscHunkContext writes the chain of definitions into the hunk headers of diff. version 0 is the newest version. It never
// fails the page: a file asc has no grammar for keeps git's header.
func prepareAscHunkContext(ctx *context.Context, pull *issues_model.PullRequest, diff *gitdiff.Diff, version uint32) {
	if diff == nil || len(diff.Files) == 0 || pull == nil {
		return
	}
	link, err := issues_model.GetPullRequestAscStack(ctx, pull.ID)
	if err != nil || link == nil || pull.BaseRepo == nil && pull.LoadBaseRepo(ctx) != nil {
		return
	}
	if pull.BaseRepo.GetStorageBackend() != string(gitrepo.StorageBackendAsc) {
		return
	}
	name, err := ascGraphRepoName(pull.BaseRepo)
	change := ascChangeBytes(link.AscChange)
	if err != nil || change == nil {
		return
	}
	conn, err := ascRPCConn()
	if err != nil {
		return
	}
	budget, cancel := gocontext.WithTimeout(ascclient.WithSystem(ctx), ascHunkContextBudget)
	defer cancel()
	client := ascv1.NewReviewClient(conn)
	for i, f := range diff.Files {
		if i >= ascHunkContextMaxFiles || budget.Err() != nil {
			return
		}
		starts := ascHunkStarts(f)
		if len(starts) == 0 || f.IsDeleted || f.IsBin {
			continue
		}
		resp, err := client.HunkContext(budget, &ascv1.HunkContextRequest{Repo: &ascv1.RepoRef{Name: name}, Change: change, Version: version, Path: []byte(f.Name), Lines: starts})
		if err != nil {
			log.Warn("asc hunk context for %s: %v", f.Name, err)
			continue
		}
		byLine := map[uint32]string{}
		for _, l := range resp.GetLines() {
			if text := ascHunkContextText(l.GetChain()); text != "" {
				byLine[l.GetLine()] = text
			}
		}
		for _, s := range f.Sections {
			for _, l := range s.Lines {
				if l.Type == gitdiff.DiffLineSection && l.SectionInfo != nil {
					l.SectionInfo.AscContext = byLine[uint32(l.SectionInfo.RightIdx)]
				}
			}
		}
	}
}
