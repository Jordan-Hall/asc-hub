// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"

	"forgejo.org/modules/git"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func paths(changes []git.PathChange) []string {
	res := make([]string, len(changes))
	for i, c := range changes {
		res[i] = c.Path
	}
	return res
}

func TestChangedPaths(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	changes, err := f.be.ChangedPaths(ctx, f.id("c1"), f.id("c3"), git.DiffOpts{})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a.txt", "a2.txt", "big.bin"}, paths(changes))

	// A zero base lists what the head commit itself changed.
	changes, err = f.be.ChangedPaths(ctx, git.Sha1ObjectFormat.EmptyObjectID(), f.id("c2"), git.DiffOpts{})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a.txt", "big.bin"}, paths(changes))

	// A root commit lists everything it holds.
	changes, err = f.be.ChangedPaths(ctx, git.Sha1ObjectFormat.EmptyObjectID(), f.id("c1"), git.DiffOpts{})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{".gitattributes", "a.txt", "dir/b.txt"}, paths(changes))

	// Against the merge base, the changes of the other side do not count.
	changes, err = f.be.ChangedPaths(ctx, f.id("c1"), f.id("c3"), git.DiffOpts{MergeBase: true})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a.txt", "a2.txt", "big.bin"}, paths(changes))

	changes, err = f.be.ChangedPaths(ctx, f.id("c3"), f.id("c3"), git.DiffOpts{})
	require.NoError(t, err)
	assert.Empty(t, paths(changes))

	_, err = f.be.ChangedPaths(ctx, f.id("c1"), git.MustIDFromString(strings.Repeat("7", 40)), git.DiffOpts{})
	assert.Error(t, err)
}

func TestUnifiedDiff(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	diff := func(base, head string, opt git.DiffOpts) string {
		var buf bytes.Buffer
		require.NoError(t, f.be.UnifiedDiff(ctx, f.id(base), f.id(head), opt, &buf))
		return buf.String()
	}

	text := diff("c1", "c2", git.DiffOpts{Mode: git.DiffModeText})
	assert.Contains(t, text, "diff --git a/a.txt b/a.txt")
	assert.Contains(t, text, "-alpha one")
	assert.Contains(t, text, "+alpha two")
	assert.Contains(t, text, "Binary files /dev/null and b/big.bin differ", "plain text mode leaves binary files out")

	binary := diff("c1", "c2", git.DiffOpts{Mode: git.DiffModeBinary})
	assert.Contains(t, binary, "GIT binary patch")

	merged := diff("c2", "c3", git.DiffOpts{Mode: git.DiffModeBinary, MergeBase: true})
	assert.Contains(t, merged, "rename to a2.txt")

	patch := diff("c1", "c3", git.DiffOpts{Mode: git.DiffModePatch})
	assert.Contains(t, patch, "Subject: [PATCH 1/2] second commit")
	assert.Contains(t, patch, "Subject: [PATCH 2/2] rename a")

	// Unrelated histories fall back to a direct comparison.
	assert.Contains(t, diff("c3", "orphan", git.DiffOpts{Mode: git.DiffModeText, MergeBase: true}), "z.txt")
	assert.Contains(t, diff("c3", "orphan", git.DiffOpts{Mode: git.DiffModeBinary, MergeBase: true}), "z.txt")
	assert.Contains(t, diff("c3", "orphan", git.DiffOpts{Mode: git.DiffModePatch}), "z.txt")

	err := f.be.UnifiedDiff(ctx, f.id("c1"), f.id("c2"), git.DiffOpts{Mode: git.DiffMode(99)}, io.Discard)
	assert.ErrorIs(t, err, util.ErrInvalidArgument)
}

func TestDiffStat(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	stat, err := f.be.DiffStat(ctx, f.id("c1"), f.id("c2"))
	require.NoError(t, err)
	assert.Equal(t, git.DiffStat{NumFiles: 2, Additions: 2, Deletions: 1}, stat) // a.txt: +alpha two +third -alpha one; big.bin is binary

	stat, err = f.be.DiffStat(ctx, f.id("c2"), f.id("c3"))
	require.NoError(t, err)
	assert.Equal(t, git.DiffStat{NumFiles: 1}, stat, "a pure rename changes no line")

	stat, err = f.be.DiffStat(ctx, f.id("c3"), f.id("c3"))
	require.NoError(t, err)
	assert.Equal(t, git.DiffStat{}, stat)
}

func TestArchive(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	var buf bytes.Buffer
	require.NoError(t, f.be.Archive(ctx, f.id("c3"), git.TARGZ, "proj/", &buf))
	gz, err := gzip.NewReader(&buf)
	require.NoError(t, err)
	tr := tar.NewReader(gz)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, hdr.Name)
	}
	assert.Contains(t, names, "proj/a2.txt")
	assert.Contains(t, names, "proj/dir/b.txt")

	buf.Reset()
	require.NoError(t, f.be.Archive(ctx, f.id("c1"), git.ZIP, "", &buf))
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	var zipped []string
	for _, zf := range zr.File {
		zipped = append(zipped, zf.Name)
	}
	assert.Contains(t, zipped, "a.txt")
	assert.NotContains(t, zipped, "a2.txt")

	assert.Error(t, f.be.Archive(ctx, f.id("c1"), git.ArchiveType(0), "", io.Discard))
	assert.Error(t, f.be.Archive(ctx, git.MustIDFromString(strings.Repeat("8", 40)), git.ZIP, "", io.Discard))
}

func TestGrep(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	oldInclude, oldExclude := setting.Indexer.IncludePatterns, setting.Indexer.ExcludePatterns
	setting.Indexer.IncludePatterns, setting.Indexer.ExcludePatterns = nil, nil
	t.Cleanup(func() { setting.Indexer.IncludePatterns, setting.Indexer.ExcludePatterns = oldInclude, oldExclude })

	res, err := f.be.Grep(ctx, f.id("c3"), git.GrepQuery{Search: "ALPHA"})
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "a2.txt", res[0].Filename)
	assert.Equal(t, []int{1}, res[0].LineNumbers)
	assert.Equal(t, []string{"alpha two"}, res[0].LineCodes)
	assert.Equal(t, [][3]int{{0, 0, 5}}, res[0].HighlightedRanges)

	// Any of the words.
	res, err = f.be.Grep(ctx, f.id("c3"), git.GrepQuery{Search: "bravo third", Options: git.GrepOptions{Mode: git.FixedAnyGrepMode}})
	require.NoError(t, err)
	assert.Len(t, res, 2)

	// A regular expression.
	res, err = f.be.Grep(ctx, f.id("c3"), git.GrepQuery{Search: `^third\s+line$`, Options: git.GrepOptions{Mode: git.RegExpGrepMode}})
	require.NoError(t, err)
	require.Len(t, res, 1)

	// A single file.
	res, err = f.be.Grep(ctx, f.id("c3"), git.GrepQuery{Search: "line", Options: git.GrepOptions{Filename: "a2.txt"}})
	require.NoError(t, err)
	require.Len(t, res, 1)

	// The search text is never an option; nothing matches.
	res, err = f.be.Grep(ctx, f.id("c3"), git.GrepQuery{Search: "--open-files-in-pager=touch /tmp/x"})
	require.NoError(t, err)
	assert.Empty(t, res)

	res, err = f.be.Grep(ctx, f.id("c3"), git.GrepQuery{Search: "no-such-content"})
	require.NoError(t, err)
	assert.Empty(t, res)

	// The result cap stops the search.
	res, err = f.be.Grep(ctx, f.id("c3"), git.GrepQuery{Search: "e", Options: git.GrepOptions{MaxResultLimit: 1}})
	require.NoError(t, err)
	assert.Len(t, res, 1)
}

func TestAttributesAndLanguageStats(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	attrs, err := f.be.Attributes(ctx, f.id("c3"), []string{"a2.txt", "big.bin"}, []string{"linguist-language"})
	require.NoError(t, err)
	assert.Equal(t, map[string]map[string]string{
		"a2.txt":  {"linguist-language": "Text"},
		"big.bin": {"linguist-language": "unspecified"},
	}, attrs)

	_, err = f.be.Attributes(ctx, f.id("c3"), make([]string, 5000), []string{"linguist-language"})
	assert.ErrorIs(t, err, util.ErrInvalidArgument)

	stats, err := f.be.LanguageStats(ctx, f.id("c3"))
	require.NoError(t, err)
	assert.NotNil(t, stats)
}
