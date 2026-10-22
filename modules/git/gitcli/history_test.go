// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli_test

import (
	"testing"
	"time"

	"forgejo.org/modules/git"
	"forgejo.org/modules/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func refNames(refs []git.Reference) []string {
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.Name
	}
	return names
}

func TestListRefs(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	refs, err := f.be.ListRefs(ctx, git.RefQuery{})
	require.NoError(t, err)
	assert.Equal(t, []string{"refs/heads/feature", "refs/heads/master", "refs/heads/unrelated", "refs/tags/v1"}, refNames(refs))
	assert.Equal(t, f.id("tag").String(), refs[3].Object.String())
	assert.Equal(t, "tag", refs[3].Type)
	assert.Equal(t, "commit", refs[1].Type)

	refs, err = f.be.ListRefs(ctx, git.RefQuery{Prefix: git.BranchPrefix})
	require.NoError(t, err)
	assert.Equal(t, []string{"refs/heads/feature", "refs/heads/master", "refs/heads/unrelated"}, refNames(refs))

	refs, err = f.be.ListRefs(ctx, git.RefQuery{Prefix: git.BranchPrefix, Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, []string{"refs/heads/feature", "refs/heads/master"}, refNames(refs))

	refs, err = f.be.ListRefs(ctx, git.RefQuery{Prefix: git.BranchPrefix, Sort: "-refname"})
	require.NoError(t, err)
	assert.Equal(t, []string{"refs/heads/unrelated", "refs/heads/master", "refs/heads/feature"}, refNames(refs))

	// Only the branches whose history holds c2.
	refs, err = f.be.ListRefs(ctx, git.RefQuery{Prefix: git.BranchPrefix, Contains: f.id("c2")})
	require.NoError(t, err)
	assert.Equal(t, []string{"refs/heads/master"}, refNames(refs))

	// A sort key is a key, never an option.
	for _, sort := range []string{"--exec=x", "-", "refname\nx", "a b", "x;y"} {
		_, err = f.be.ListRefs(ctx, git.RefQuery{Sort: sort})
		require.Error(t, err, "%q", sort)
		assert.ErrorIs(t, err, util.ErrInvalidArgument, "%q", sort)
	}
}

func TestHead(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	head, err := f.be.Head(ctx)
	require.NoError(t, err)
	assert.Equal(t, git.RefName("refs/heads/master"), head)

	// The branch need not exist.
	require.NoError(t, f.be.SetHead(ctx, git.RefNameFromBranch("feature")))
	head, err = f.be.Head(ctx)
	require.NoError(t, err)
	assert.Equal(t, git.RefName("refs/heads/feature"), head)
	require.NoError(t, f.be.SetHead(ctx, git.RefNameFromBranch("not-yet")))
	head, err = f.be.Head(ctx)
	require.NoError(t, err)
	assert.Equal(t, git.RefName("refs/heads/not-yet"), head)

	for _, bad := range []git.RefName{"", "master", "refs/tags/v1", "refs/heads/a..b", "refs/heads/-x y", "refs/heads/x\ny"} {
		err := f.be.SetHead(ctx, bad)
		require.Error(t, err, "%q", bad)
		assert.ErrorIs(t, err, util.ErrInvalidArgument, "%q", bad)
	}
	head, err = f.be.Head(ctx)
	require.NoError(t, err)
	assert.Equal(t, git.RefName("refs/heads/not-yet"), head)
}

func TestLogAndCount(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	collect := func(q git.LogQuery) []string {
		var ids []string
		for c, err := range f.be.Log(ctx, q) {
			require.NoError(t, err)
			ids = append(ids, c.ID.String())
		}
		return ids
	}
	ids := func(names ...string) []string {
		res := make([]string, len(names))
		for i, n := range names {
			res[i] = f.id(n).String()
		}
		return res
	}
	from := func(names ...string) []git.ObjectID {
		res := make([]git.ObjectID, len(names))
		for i, n := range names {
			res[i] = f.id(n)
		}
		return res
	}

	assert.Equal(t, ids("c3", "c2", "c1"), collect(git.LogQuery{From: from("c3")}))
	assert.Equal(t, ids("c3", "c2"), collect(git.LogQuery{From: from("c3"), Not: from("c1")}))
	assert.Equal(t, ids("c2"), collect(git.LogQuery{From: from("c3"), Skip: 1, Limit: 1}))
	assert.ElementsMatch(t, ids("c3", "c2", "c1", "orphan"), collect(git.LogQuery{All: true}))

	// Paths are literal.
	assert.Equal(t, ids("c2", "c1"), collect(git.LogQuery{From: from("c3"), Paths: []string{"big.bin", "dir"}}))
	assert.Empty(t, collect(git.LogQuery{From: from("c3"), Paths: []string{"*.bin"}}))
	assert.Empty(t, collect(git.LogQuery{From: from("c3"), Paths: []string{":(top)a.txt"}}))

	// Filters.
	assert.Equal(t, ids("c3", "c2", "c1"), collect(git.LogQuery{From: from("c3"), Author: "ann@example.com"}))
	assert.Empty(t, collect(git.LogQuery{From: from("c3"), Author: "nobody"}))
	assert.Equal(t, ids("c2"), collect(git.LogQuery{From: from("c3"), Grep: "second"}))
	assert.Empty(t, collect(git.LogQuery{From: from("c3"), Grep: "sec.nd"}), "the filter text is literal")
	since := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, ids("c3", "c2"), collect(git.LogQuery{From: from("c3"), Since: since}))
	assert.Equal(t, ids("c1"), collect(git.LogQuery{From: from("c3"), Until: since}))
	assert.Equal(t, ids("c3", "c2", "c1"), collect(git.LogQuery{From: from("c3"), NoMerges: true}))

	// Follow crosses the rename a.txt -> a2.txt.
	assert.Equal(t, ids("c3", "c2", "c1"), collect(git.LogQuery{From: from("c3"), Paths: []string{"a2.txt"}, Follow: true}))

	n, err := f.be.Count(ctx, git.LogQuery{From: from("c3")})
	require.NoError(t, err)
	assert.EqualValues(t, 3, n)
	n, err = f.be.Count(ctx, git.LogQuery{All: true})
	require.NoError(t, err)
	assert.EqualValues(t, 4, n)
	n, err = f.be.Count(ctx, git.LogQuery{From: from("c3"), Paths: []string{"dir"}})
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	n, err = f.be.Count(ctx, git.LogQuery{From: from("c3"), Not: from("c2")})
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	// Bad queries fail before git runs.
	for name, q := range map[string]git.LogQuery{
		"no start":        {},
		"nil start":       {From: []git.ObjectID{nil}},
		"negative skip":   {From: from("c3"), Skip: -1},
		"negative limit":  {From: from("c3"), Limit: -1},
		"follow no path":  {From: from("c3"), Follow: true},
		"follow two":      {From: from("c3"), Follow: true, Paths: []string{"a", "b"}},
		"follow magic":    {From: from("c3"), Follow: true, Paths: []string{":(top)a"}},
		"too many starts": {From: make([]git.ObjectID, 5000)},
	} {
		_, err := f.be.Count(ctx, q)
		assert.Error(t, err, name)
		for _, err := range f.be.Log(ctx, q) {
			assert.Error(t, err, name)
		}
	}
	// Follow is a log option only.
	_, err = f.be.Count(ctx, git.LogQuery{From: from("c3"), Paths: []string{"a2.txt"}, Follow: true})
	assert.Error(t, err)
}

func TestMergeBaseAndAncestor(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	base, err := f.be.MergeBase(ctx, f.id("c3"), f.id("c1"))
	require.NoError(t, err)
	assert.Equal(t, f.id("c1").String(), base.String())

	_, err = f.be.MergeBase(ctx, f.id("c3"), f.id("orphan"))
	assert.ErrorIs(t, err, git.ErrNoMergebaseFound)

	for _, tc := range []struct {
		ancestor, desc string
		want           bool
	}{
		{"c1", "c3", true},
		{"c3", "c3", true},
		{"c3", "c1", false},
		{"orphan", "c3", false},
	} {
		got, err := f.be.IsAncestor(ctx, f.id(tc.ancestor), f.id(tc.desc))
		require.NoError(t, err)
		assert.Equal(t, tc.want, got, "%s -> %s", tc.ancestor, tc.desc)
	}

	// A missing object is an error, not "false".
	_, err = f.be.IsAncestor(ctx, git.MustIDFromString("5555555555555555555555555555555555555555"), f.id("c3"))
	assert.Error(t, err)
}

func TestLastCommits(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	last, err := f.be.LastCommits(ctx, f.id("c3"), "", []string{"a2.txt", "big.bin", "dir", ".gitattributes"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"a2.txt":         f.id("c3").String(),
		"big.bin":        f.id("c2").String(),
		"dir":            f.id("c1").String(),
		".gitattributes": f.id("c1").String(),
	}, idStrings(last))

	last, err = f.be.LastCommits(ctx, f.id("c3"), "dir", []string{"b.txt"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"b.txt": f.id("c1").String()}, idStrings(last))

	names := make([]string, 10001)
	_, err = f.be.LastCommits(ctx, f.id("c3"), "", names)
	assert.ErrorIs(t, err, util.ErrInvalidArgument)
}

func idStrings(m map[string]git.ObjectID) map[string]string {
	res := make(map[string]string, len(m))
	for k, v := range m {
		res[k] = v.String()
	}
	return res
}

func TestBlame(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	var parts []git.BlamePart
	for part, err := range f.be.Blame(ctx, f.id("c2"), "a.txt", git.BlameOpts{}) {
		require.NoError(t, err)
		parts = append(parts, part)
	}
	// Line 2 is unchanged since c1; lines 1 and 3 come from c2.
	require.Len(t, parts, 3)
	assert.Equal(t, f.id("c2").String(), parts[0].Sha)
	assert.Equal(t, []string{"alpha two"}, parts[0].Lines)
	assert.Equal(t, f.id("c1").String(), parts[1].Sha)
	assert.Equal(t, []string{"second line"}, parts[1].Lines)
	assert.Equal(t, f.id("c2").String(), parts[2].Sha)

	// Stopping early releases the blame process.
	for range f.be.Blame(ctx, f.id("c2"), "a.txt", git.BlameOpts{BypassIgnoreRevs: true}) {
		break
	}

	for _, err := range f.be.Blame(ctx, git.MustIDFromString("6666666666666666666666666666666666666666"), "a.txt", git.BlameOpts{}) {
		require.Error(t, err)
	}
}
