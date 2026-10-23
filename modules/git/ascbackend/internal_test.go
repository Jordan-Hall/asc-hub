// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend

import (
	"path/filepath"
	"strings"
	"testing"

	"forgejo.org/modules/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRepoName(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repos")
	ok := map[string]string{
		filepath.Join(root, "alice", "proj.git"):    "alice/proj",
		filepath.Join(root, "bob-1", "my.repo.git"): "bob-1/my.repo",
	}
	for path, want := range ok {
		got, err := RepoName(root, path)
		require.NoError(t, err, path)
		assert.Equal(t, want, got)
	}
	for _, path := range []string{
		filepath.Join(root, "alice", "proj.wiki.git"),
		filepath.Join(root, "alice", "proj"),
		filepath.Join(root, "alice.git"),
		filepath.Join(root, "alice", "sub", "proj.git"),
		filepath.Join(root, "..", "elsewhere", "proj.git"),
		filepath.Join(root, "al ice", "proj.git"),
		filepath.Join(root, "alice", "pr oj.git"),
		filepath.Join(root, "alice", "..git"),
		root,
		filepath.Join(filepath.Dir(root), "other", "alice", "proj.git"),
	} {
		_, err := RepoName(root, path)
		assert.Error(t, err, path)
	}
	_, err := RepoName("", filepath.Join(root, "a", "b.git"))
	assert.Error(t, err)
}

func TestSplitLines(t *testing.T) {
	for in, want := range map[string][]string{
		"":         nil,
		"\n":       {""},
		"a":        {"a"},
		"a\n":      {"a"},
		"a\nb":     {"a", "b"},
		"a\nb\n":   {"a", "b"},
		"a\n\nb\n": {"a", "", "b"},
		"a\r\nb\n": {"a\r", "b"},
	} {
		assert.Equal(t, want, splitLines([]byte(in)), "%q", in)
	}
}

func TestParseNumstat(t *testing.T) {
	stat, err := parseNumstat([]byte("3\t1\ta.txt\n-\t-\tbig.bin\n0\t7\tdir/x\n"))
	require.NoError(t, err)
	assert.Equal(t, git.DiffStat{NumFiles: 3, Additions: 3, Deletions: 8}, stat)
	stat, err = parseNumstat(nil)
	require.NoError(t, err)
	assert.Equal(t, git.DiffStat{}, stat)
	for _, bad := range []string{"3\t1\n", "x\t1\ta\n", "-5\t1\ta\n"} {
		_, err = parseNumstat([]byte(bad))
		assert.ErrorIs(t, err, ErrBadAnswer, bad)
	}
}

func TestMergeLanguageStats(t *testing.T) {
	got := mergeLanguageStats(map[string]int64{"Go": 5, "go": 2, "GO": 1, "Text": 4})
	assert.Equal(t, map[string]int64{"GO": 8, "Text": 4}, got)
}

func TestErrorsAreSanitized(t *testing.T) {
	err := callErr("ReadTree", "abc", status.Error(codes.Internal, "boom \x00\x1b[31m\n"+strings.Repeat("é", 500)))
	var ascErr *Error
	require.ErrorAs(t, err, &ascErr)
	assert.LessOrEqual(t, len(ascErr.Msg), maxMessageEcho+3)
	for _, r := range ascErr.Msg {
		assert.True(t, r >= 0x20 && r < 0x7f, "%q", r)
	}
	assert.True(t, git.IsErrNotExist(callErr("x", "abc", status.Error(codes.NotFound, "no"))))
	assert.NoError(t, callErr("x", "abc", nil))
	var repoErr *Error
	require.ErrorAs(t, callErr("x", "abc", status.Error(codes.NotFound, "no such repository")), &repoErr)
	assert.True(t, isMissing(status.Error(codes.NotFound, "changeset is missing")))
	assert.False(t, isMissing(status.Error(codes.NotFound, "no such repository")))
	assert.ErrorIs(t, &Error{Code: codes.OutOfRange}, ErrTooLarge)
	assert.NotErrorIs(t, &Error{Code: codes.Internal}, ErrTooLarge)
}

func TestSortKeys(t *testing.T) {
	for _, k := range []string{"refname", "-creatordate", "refname:lstrip=2", "committerdate"} {
		assert.True(t, isSortKey(k), k)
	}
	for _, k := range []string{"", "-", "1a", "a b", "a;b", strings.Repeat("a", 129)} {
		assert.False(t, isSortKey(k), k)
	}
}

func TestFactCacheMerges(t *testing.T) {
	c := newFactCache(2)
	a, b, d := git.MustIDFromString(strings.Repeat("a", 40)), git.MustIDFromString(strings.Repeat("b", 40)), git.MustIDFromString(strings.Repeat("d", 40))
	c.putTree(a, treeLoc{rev: "r", path: "p"})
	c.putTree(a, treeLoc{rev: "other", path: "q"})
	f, ok := c.get(a)
	require.True(t, ok)
	assert.Equal(t, treeLoc{rev: "r", path: "p"}, *f.loc, "the first location is kept")
	c.putBlob(a, 5) // a tree id is never retyped by a later claim
	f, _ = c.get(a)
	assert.Equal(t, git.ObjectTree, f.typ)
	c.putBlob(b, 7)
	c.putBlob(d, 9) // evicts the least recently used: a
	_, ok = c.get(a)
	assert.False(t, ok)
	f, ok = c.get(b)
	require.True(t, ok)
	assert.Equal(t, int64(7), f.size)
}
