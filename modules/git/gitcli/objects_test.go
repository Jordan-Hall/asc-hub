// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli_test

import (
	"io"
	"strings"
	"testing"

	"forgejo.org/modules/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolve(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	for rev, want := range map[string]struct {
		id  string
		typ git.ObjectType
	}{
		"master":                 {f.id("c3").String(), git.ObjectCommit},
		"refs/heads/master":      {f.id("c3").String(), git.ObjectCommit},
		"master~1":               {f.id("c2").String(), git.ObjectCommit},
		f.id("c2").String()[:10]: {f.id("c2").String(), git.ObjectCommit},
		"v1":                     {f.id("tag").String(), git.ObjectTag},
		"master:big.bin":         {f.id("big").String(), git.ObjectBlob},
	} {
		id, typ, err := f.be.Resolve(ctx, rev)
		require.NoError(t, err, rev)
		assert.Equal(t, want.id, id.String(), rev)
		assert.Equal(t, want.typ, typ, rev)
	}

	// Unknown revisions, and anything that could smuggle a second request into the batch stream, are "not found".
	for _, rev := range []string{"", "no-such-ref", "master\nv1", "master\x00", strings.Repeat("0", 40)} {
		_, _, err := f.be.Resolve(ctx, rev)
		require.Error(t, err, "%q", rev)
		assert.True(t, git.IsErrNotExist(err), "%q: %v", rev, err)
	}

	// The batch process is still in step after those failures.
	id, _, err := f.be.Resolve(ctx, "master")
	require.NoError(t, err)
	assert.Equal(t, f.id("c3").String(), id.String())
}

func TestReadCommit(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	c, err := f.be.ReadCommit(ctx, f.id("c1"))
	require.NoError(t, err)
	assert.Equal(t, f.id("c1").String(), c.ID.String())
	assert.Empty(t, c.Parents)
	assert.Equal(t, "Ann Author", c.Author.Name)
	assert.Equal(t, "ann@example.com", c.Author.Email)
	assert.Equal(t, "Cy Committer", c.Committer.Name)
	assert.Equal(t, int64(1767261600), c.Author.When.Unix()) // 2026-01-01T10:00:00Z
	assert.Equal(t, "first commit\n\nbody of the first\n", c.Message)
	assert.Nil(t, c.Signature)
	assert.NotNil(t, c.Tree)

	c3, err := f.be.ReadCommit(ctx, f.id("c3"))
	require.NoError(t, err)
	require.Len(t, c3.Parents, 1)
	assert.Equal(t, f.id("c2").String(), c3.Parents[0].String())

	// An annotated tag peels to its commit.
	peeled, err := f.be.ReadCommit(ctx, f.id("tag"))
	require.NoError(t, err)
	assert.Equal(t, f.id("c2").String(), peeled.ID.String())

	// A blob is not a commit, and a missing id is not found; the stream stays in step.
	_, err = f.be.ReadCommit(ctx, f.id("big"))
	assert.True(t, git.IsErrNotExist(err), "%v", err)
	_, err = f.be.ReadCommit(ctx, git.MustIDFromString(strings.Repeat("1", 40)))
	assert.True(t, git.IsErrNotExist(err), "%v", err)
	again, err := f.be.ReadCommit(ctx, f.id("c1"))
	require.NoError(t, err)
	assert.Equal(t, c.Message, again.Message)
}

func TestReadTree(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	// A commit id lists its root tree.
	entries, err := f.be.ReadTree(ctx, f.id("c3"))
	require.NoError(t, err)
	got := map[string]git.EntryMode{}
	for _, e := range entries {
		got[e.Name] = e.Mode
		assert.EqualValues(t, -1, e.Size, e.Name)
	}
	assert.Equal(t, map[string]git.EntryMode{
		".gitattributes": git.EntryModeBlob,
		"a2.txt":         git.EntryModeBlob,
		"big.bin":        git.EntryModeBlob,
		"dir":            git.EntryModeTree,
	}, got)

	// The same listing by tree id.
	commit, err := f.be.ReadCommit(ctx, f.id("c3"))
	require.NoError(t, err)
	byTree, err := f.be.ReadTree(ctx, commit.Tree)
	require.NoError(t, err)
	assert.Equal(t, entries, byTree)

	// A blob is no tree.
	_, err = f.be.ReadTree(ctx, f.id("big"))
	assert.Error(t, err)

	// The stream is still in step.
	rc, size, err := f.be.OpenBlob(ctx, f.id("big"), -1)
	require.NoError(t, err)
	assert.EqualValues(t, f.bigLen, size)
	require.NoError(t, rc.Close())
}

func TestReadTreeRecursive(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	collect := func(opt git.TreeWalkOpts) map[string]git.TreeEntryData {
		res := map[string]git.TreeEntryData{}
		for e, err := range f.be.ReadTreeRecursive(ctx, f.id("c3"), opt) {
			require.NoError(t, err)
			res[e.Name] = e
		}
		return res
	}

	leaves := collect(git.TreeWalkOpts{})
	assert.Contains(t, leaves, "dir/b.txt")
	assert.NotContains(t, leaves, "dir")
	assert.EqualValues(t, -1, leaves["dir/b.txt"].Size)

	withTrees := collect(git.TreeWalkOpts{Trees: true})
	assert.Contains(t, withTrees, "dir")
	assert.Equal(t, git.EntryModeTree, withTrees["dir"].Mode)
	assert.Contains(t, withTrees, "dir/b.txt")

	sized := collect(git.TreeWalkOpts{Trees: true, Sizes: true})
	assert.EqualValues(t, f.bigLen, sized["big.bin"].Size)
	assert.EqualValues(t, len("bravo\n"), sized["dir/b.txt"].Size)

	// A missing tree is an error from the iterator.
	for _, err := range f.be.ReadTreeRecursive(ctx, git.MustIDFromString(strings.Repeat("2", 40)), git.TreeWalkOpts{}) {
		require.Error(t, err)
		assert.True(t, git.IsErrNotExist(err), "%v", err)
	}
}

func TestOpenBlob(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	// Small blob, no limit.
	rc, size, err := f.be.OpenBlob(ctx, f.id("blobA1"), -1)
	require.NoError(t, err)
	body, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.Equal(t, "alpha one\nsecond line\n", string(body))
	assert.EqualValues(t, len(body), size)

	// Small blob truncated by the limit: the full size is still reported.
	rc, size, err = f.be.OpenBlob(ctx, f.id("blobA1"), 5)
	require.NoError(t, err)
	body, err = io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.Equal(t, "alpha", string(body))
	assert.EqualValues(t, len("alpha one\nsecond line\n"), size)

	// Large blob read whole.
	rc, size, err = f.be.OpenBlob(ctx, f.id("big"), -1)
	require.NoError(t, err)
	body, err = io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.EqualValues(t, f.bigLen, size)
	assert.Len(t, body, f.bigLen)
	assert.Equal(t, "abcdefghij", string(body[:10]))

	// Large blob truncated, then closed without reading everything: the next object must still read correctly.
	rc, size, err = f.be.OpenBlob(ctx, f.id("big"), 100)
	require.NoError(t, err)
	assert.EqualValues(t, f.bigLen, size)
	part := make([]byte, 10)
	_, err = io.ReadFull(rc, part)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	rc, _, err = f.be.OpenBlob(ctx, f.id("blobA1"), -1)
	require.NoError(t, err)
	body, err = io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.Equal(t, "alpha one\nsecond line\n", string(body))

	// A zero limit yields an empty stream.
	rc, _, err = f.be.OpenBlob(ctx, f.id("big"), 0)
	require.NoError(t, err)
	body, err = io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	assert.Empty(t, body)

	// A missing blob is not found.
	_, _, err = f.be.OpenBlob(ctx, git.MustIDFromString(strings.Repeat("3", 40)), -1)
	assert.True(t, git.IsErrNotExist(err), "%v", err)
}

func TestStatObjects(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	missing := git.MustIDFromString(strings.Repeat("4", 40))
	infos, err := f.be.StatObjects(ctx, []git.ObjectID{f.id("big"), missing, f.id("c1"), f.id("tag")})
	require.NoError(t, err)
	require.Len(t, infos, 4)
	assert.Equal(t, git.ObjectInfo{ID: f.id("big"), Type: git.ObjectBlob, Size: int64(f.bigLen)}, infos[0])
	assert.True(t, infos[1].Missing)
	assert.Equal(t, missing.String(), infos[1].ID.String())
	assert.Equal(t, git.ObjectCommit, infos[2].Type)
	assert.Equal(t, git.ObjectTag, infos[3].Type)

	// More ids than one batch round carries, in order.
	many := make([]git.ObjectID, 0, 600)
	for i := range 600 {
		if i%7 == 0 {
			many = append(many, missing)
		} else {
			many = append(many, f.id("big"))
		}
	}
	infos, err = f.be.StatObjects(ctx, many)
	require.NoError(t, err)
	require.Len(t, infos, len(many))
	for i, info := range infos {
		assert.Equal(t, i%7 == 0, info.Missing, "index %d", i)
	}

	infos, err = f.be.StatObjects(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, infos)
}

func TestReadTag(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	tag, err := f.be.ReadTag(ctx, f.id("tag"))
	require.NoError(t, err)
	assert.Equal(t, f.id("tag").String(), tag.ID.String())
	assert.Equal(t, f.id("c2").String(), tag.Object.String())
	assert.Equal(t, "commit", tag.Type)
	assert.Equal(t, "Tag Er", tag.Tagger.Name)
	assert.Equal(t, "release one\n", tag.Message)

	// A commit is not a tag.
	_, err = f.be.ReadTag(ctx, f.id("c1"))
	assert.True(t, git.IsErrNotExist(err), "%v", err)
}

func TestObjectFormatAndClose(t *testing.T) {
	f := newFixture(t)
	assert.Equal(t, "sha1", f.be.ObjectFormat().Name())
	require.NoError(t, f.be.Close())
	// Close does not stop the repository's own batch processes.
	_, _, err := f.be.Resolve(t.Context(), "master")
	require.NoError(t, err)
}
