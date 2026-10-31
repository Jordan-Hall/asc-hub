// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package repo

import (
	"fmt"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/services/gitdiff"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAscChangeGroupOf(t *testing.T) {
	for c, want := range map[ascv1.ChangeClass]string{
		ascv1.ChangeClass_CHANGE_CLASS_LOGIC: "behaviour", ascv1.ChangeClass_CHANGE_CLASS_TEST: "tests",
		ascv1.ChangeClass_CHANGE_CLASS_DOCS: "docs", ascv1.ChangeClass_CHANGE_CLASS_COMMENT_ONLY: "docs",
		ascv1.ChangeClass_CHANGE_CLASS_GENERATED: "generated", ascv1.ChangeClass_CHANGE_CLASS_LOCKFILE: "generated",
		ascv1.ChangeClass_CHANGE_CLASS_RENAME_ONLY: "mechanical", ascv1.ChangeClass_CHANGE_CLASS_MODE_ONLY: "mechanical",
		ascv1.ChangeClass_CHANGE_CLASS_WHITESPACE_ONLY: "mechanical",
	} {
		assert.Equal(t, want, ascChangeGroupOf(c), c.String())
	}
}

func diffOf(names ...string) *gitdiff.Diff {
	d := &gitdiff.Diff{}
	for i, n := range names {
		d.Files = append(d.Files, &gitdiff.DiffFile{Name: n, NameHash: fmt.Sprintf("h%d", i), Addition: i + 1, Deletion: 1})
	}
	return d
}

func TestAscChangeFilesGroupsAndFoldsGenerated(t *testing.T) {
	d := diffOf("src/a.go", "src/a_test.go", "go.sum", "README.md", "docs/x.md", "src/new")
	classes := map[string]ascv1.ChangeClass{
		"src/a.go": ascv1.ChangeClass_CHANGE_CLASS_LOGIC, "src/a_test.go": ascv1.ChangeClass_CHANGE_CLASS_TEST,
		"go.sum": ascv1.ChangeClass_CHANGE_CLASS_LOCKFILE, "README.md": ascv1.ChangeClass_CHANGE_CLASS_DOCS, "docs/x.md": ascv1.ChangeClass_CHANGE_CLASS_DOCS,
	}
	out := ascChangeFilesOf(d, classes, false, "/o/r/commit/abc")
	require.NotNil(t, out)
	var keys []string
	for _, g := range out.Groups {
		keys = append(keys, fmt.Sprintf("%s:%d", g.Key, g.Count))
	}
	assert.Equal(t, []string{"behaviour:2", "tests:1", "docs:2", "generated:1"}, keys, "a file asc did not name is Behaviour; order is the order of the board")
	assert.True(t, out.Groups[3].Folded)
	assert.True(t, d.Files[2].IsGenerated, "the diff folds a generated file")
	assert.False(t, d.Files[0].IsGenerated)
	assert.Zero(t, out.Hidden)
	assert.Equal(t, "#diff-h0", out.Groups[0].Files[0].Link)
	assert.Nil(t, ascChangeFilesOf(&gitdiff.Diff{}, nil, false, ""))
}

func TestAscChangeFilesLargeChangeShowsWhatMattersMost(t *testing.T) {
	var names []string
	classes := map[string]ascv1.ChangeClass{}
	for i := 0; i < 50; i++ {
		n := fmt.Sprintf("f%02d", i)
		names = append(names, n)
		if i < 30 {
			classes[n] = ascv1.ChangeClass_CHANGE_CLASS_GENERATED
		} else {
			classes[n] = ascv1.ChangeClass_CHANGE_CLASS_LOGIC
		}
	}
	d := diffOf(names...)
	out := ascChangeFilesOf(d, classes, false, "/o/r/commit/abc")
	require.NotNil(t, out)
	assert.Len(t, d.Files, ascChangeShownFiles)
	assert.Equal(t, 50, out.Total)
	assert.Equal(t, 30, out.Hidden)
	assert.Equal(t, "/o/r/commit/abc?all=1", out.AllLink)
	for _, f := range d.Files {
		assert.Equal(t, ascv1.ChangeClass_CHANGE_CLASS_LOGIC, classes[f.Name], "the behaviour files come first")
	}
	assert.Equal(t, "f49", d.Files[0].Name, "within a group, the largest first")
	// a hidden file's link opens the full page at its place
	var gen AscChangeGroup
	for _, g := range out.Groups {
		if g.Key == "generated" {
			gen = g
		}
	}
	assert.Equal(t, 30, gen.Count)
	assert.Equal(t, "/o/r/commit/abc?all=1#diff-h29", gen.Files[0].Link)

	// all=1 shows everything
	d = diffOf(names...)
	out = ascChangeFilesOf(d, classes, true, "/o/r/commit/abc")
	assert.Len(t, d.Files, 50)
	assert.Zero(t, out.Hidden)
}
