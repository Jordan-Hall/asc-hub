// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	gocontext "context"
	"sort"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/gitrepo"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
	"forgejo.org/services/context"
	"forgejo.org/services/gitdiff"
)

// The files of a change page by meaning (board Changeset): Behaviour, Tests, Docs, Mechanical and Generated, each with a
// count and its files; generated files are folded; and a change of many files shows the ones that matter most first, the
// rest behind "Show all". The meaning is asc's Classify answer for the change against its parent; without an answer the
// page is the plain diff.

const (
	// ascChangeLargeFiles is how many files make a change "large".
	ascChangeLargeFiles = 40
	// ascChangeShownFiles is how many of a large change's files are shown at first.
	ascChangeShownFiles = 20
	// ascChangeClassifyTimeout bounds the Classify call of the page.
	ascChangeClassifyTimeout = 4 * time.Second
	// ascChangeGroupFilesMax bounds the files one group lists.
	ascChangeGroupFilesMax = 200
)

// ascChangeGroupKeys are the groups in the order they are shown, which is also the order of "matters most".
var ascChangeGroupKeys = []string{"behaviour", "tests", "docs", "mechanical", "generated"}

// AscChangeGroupFile is one file of a group.
type AscChangeGroupFile struct {
	Name     string
	Link     string // the anchor of its diff, behind "?all=1" when the file is not shown at first
	Add, Del int
}

// AscChangeGroup is one group of files.
type AscChangeGroup struct {
	Key      string
	Count    int
	Add, Del int
	Folded   bool // generated files are folded in the diff, and the group starts closed
	Files    []AscChangeGroupFile
	More     int // files of the group that are not listed
}

// AscChangeFiles is the template data under .AscChangeFiles.
type AscChangeFiles struct {
	Groups []AscChangeGroup
	Total  int
	// Hidden is how many files the large-change state leaves out (0: all are shown); AllLink shows them.
	Hidden  int
	Shown   int
	AllLink string
}

// ascChangeGroupOf puts a change class into a group.
func ascChangeGroupOf(c ascv1.ChangeClass) string {
	switch c {
	case ascv1.ChangeClass_CHANGE_CLASS_TEST:
		return "tests"
	case ascv1.ChangeClass_CHANGE_CLASS_DOCS, ascv1.ChangeClass_CHANGE_CLASS_COMMENT_ONLY:
		return "docs"
	case ascv1.ChangeClass_CHANGE_CLASS_GENERATED, ascv1.ChangeClass_CHANGE_CLASS_LOCKFILE:
		return "generated"
	case ascv1.ChangeClass_CHANGE_CLASS_RENAME_ONLY, ascv1.ChangeClass_CHANGE_CLASS_MODE_ONLY, ascv1.ChangeClass_CHANGE_CLASS_WHITESPACE_ONLY:
		return "mechanical"
	}
	return "behaviour"
}

// ascChangeFilesOf groups the files of diff by the classes in classes (path to class name; a file asc did not name is
// Behaviour). It marks generated files as generated, so the diff folds them, and, for a large change, reorders diff.Files
// to the files that matter most and cuts it to ascChangeShownFiles unless all is true. selfLink is the address of the
// page without a query.
func ascChangeFilesOf(diff *gitdiff.Diff, classes map[string]ascv1.ChangeClass, all bool, selfLink string) *AscChangeFiles {
	if diff == nil || len(diff.Files) == 0 {
		return nil
	}
	groupOf := func(f *gitdiff.DiffFile) string {
		c, ok := classes[f.Name]
		if !ok {
			return "behaviour" // asc did not name it: the class that is never relaxed
		}
		return ascChangeGroupOf(c)
	}
	rank := map[string]int{}
	for i, k := range ascChangeGroupKeys {
		rank[k] = i
	}
	out := &AscChangeFiles{Total: len(diff.Files)}
	if len(diff.Files) > ascChangeLargeFiles && !all {
		files := append([]*gitdiff.DiffFile(nil), diff.Files...)
		sort.SliceStable(files, func(i, j int) bool {
			ri, rj := rank[groupOf(files[i])], rank[groupOf(files[j])]
			if ri != rj {
				return ri < rj
			}
			return files[i].Addition+files[i].Deletion > files[j].Addition+files[j].Deletion
		})
		shown := map[string]bool{}
		for _, f := range files[:ascChangeShownFiles] {
			shown[f.Name] = true
		}
		kept := files[:ascChangeShownFiles]
		diff.Files = kept
		out.Shown, out.Hidden, out.AllLink = len(kept), out.Total-len(kept), selfLink+"?all=1"
		return fill(out, groupFilesFrom(files, shown), groupOf, selfLink)
	}
	shown := map[string]bool{}
	for _, f := range diff.Files {
		shown[f.Name] = true
	}
	out.Shown = len(diff.Files)
	return fill(out, groupFilesFrom(diff.Files, shown), groupOf, selfLink)
}

type groupedFile struct {
	f     *gitdiff.DiffFile
	shown bool
}

func groupFilesFrom(files []*gitdiff.DiffFile, shown map[string]bool) []groupedFile {
	out := make([]groupedFile, len(files))
	for i, f := range files {
		out[i] = groupedFile{f: f, shown: shown[f.Name]}
	}
	return out
}

func fill(out *AscChangeFiles, files []groupedFile, groupOf func(*gitdiff.DiffFile) string, selfLink string) *AscChangeFiles {
	byKey := map[string]*AscChangeGroup{}
	for _, k := range ascChangeGroupKeys {
		byKey[k] = &AscChangeGroup{Key: k, Folded: k == "generated"}
	}
	for _, gf := range files {
		g := byKey[groupOf(gf.f)]
		g.Count++
		g.Add += gf.f.Addition
		g.Del += gf.f.Deletion
		if g.Key == "generated" {
			gf.f.IsGenerated = true // the diff folds it
		}
		if len(g.Files) >= ascChangeGroupFilesMax {
			g.More++
			continue
		}
		link := "#diff-" + gf.f.NameHash
		if !gf.shown {
			link = selfLink + "?all=1" + link
		}
		g.Files = append(g.Files, AscChangeGroupFile{Name: gf.f.Name, Link: link, Add: gf.f.Addition, Del: gf.f.Deletion})
	}
	for _, k := range ascChangeGroupKeys {
		if g := byKey[k]; g.Count > 0 {
			out.Groups = append(out.Groups, *g)
		}
	}
	return out
}

// prepareAscChangeFiles puts .AscChangeFiles and the fold of generated files into the commit page. It never fails the page.
func prepareAscChangeFiles(ctx *context.Context, commit *git.Commit, diff *gitdiff.Diff) {
	if !setting.Asc.Enabled || diff == nil || len(diff.Files) == 0 || commit.ParentCount() == 0 {
		return
	}
	if sb, err := gitrepo.StorageBackendOf(ctx.Repo.Repository); err != nil || sb != gitrepo.StorageBackendAsc {
		return
	}
	parent, err := commit.ParentID(0)
	if err != nil {
		return
	}
	callCtx, cancel := gocontext.WithTimeout(ctx, ascChangeClassifyTimeout)
	defer cancel()
	cl, err := ascbackend.ClassifyRepository(callCtx, ctx.Repo.Repository.RepoPath(), parent.String(), commit.ID.String(), false)
	if err != nil {
		log.Warn("asc change files: Classify: %v", err)
		return
	}
	classes := make(map[string]ascv1.ChangeClass, len(cl.Files))
	for _, f := range cl.Files {
		classes[f.Path] = f.Class
	}
	ctx.Data["AscChangeFiles"] = ascChangeFilesOf(diff, classes, ctx.FormBool("all"), ctx.Repo.RepoLink+"/commit/"+commit.ID.String())
}
