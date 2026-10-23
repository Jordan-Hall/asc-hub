// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"maps"
	"slices"
	"testing"
	"time"

	"forgejo.org/modules/git"
	"forgejo.org/modules/git/ascbackend"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var commitNames = []string{"c1", "c2", "c3", "f1", "m1", "c4", "sg"}

func sameSig(t *testing.T, want, got *git.Signature) {
	t.Helper()
	require.NotNil(t, got)
	assert.Equal(t, want.Name, got.Name)
	assert.Equal(t, want.Email, got.Email)
	assert.True(t, want.When.Equal(got.When), "%v != %v", want.When, got.When)
	_, wantOff := want.When.Zone()
	_, gotOff := got.When.Zone()
	assert.Equal(t, wantOff, gotOff)
}

func sameCommit(t *testing.T, want, got *git.CommitData, withSignature bool) {
	t.Helper()
	assert.Equal(t, want.ID.String(), got.ID.String())
	assert.Equal(t, want.Tree.String(), got.Tree.String())
	require.Len(t, got.Parents, len(want.Parents))
	for i := range want.Parents {
		assert.Equal(t, want.Parents[i].String(), got.Parents[i].String())
	}
	sameSig(t, want.Author, got.Author)
	sameSig(t, want.Committer, got.Committer)
	assert.Equal(t, want.Message, got.Message)
	if withSignature {
		assert.Equal(t, want.Signature, got.Signature)
	}
}

func TestReadCommitMatchesGit(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "raw", true: "native"}[native], func(t *testing.T) {
			fx := newFixture(t, ascbackend.Options{})
			fx.fake.omitRaw = native
			for _, name := range commitNames {
				want, err := fx.git.ReadCommit(t.Context(), fx.id(name))
				require.NoError(t, err, name)
				got, err := fx.asc.ReadCommit(t.Context(), fx.id(name))
				require.NoError(t, err, name)
				// A commit made by asc has no stored signature header to return.
				sameCommit(t, want, got, !native)
			}
			signed, err := fx.asc.ReadCommit(t.Context(), fx.id("sg"))
			require.NoError(t, err)
			assert.Equal(t, native, signed.Signature == nil)
		})
	}
}

func TestReadCommitIsCachedAndCopied(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	first, err := fx.asc.ReadCommit(t.Context(), fx.id("c2"))
	require.NoError(t, err)
	first.Message = "changed by the caller"
	first.Parents[0] = fx.id("c4")
	second, err := fx.asc.ReadCommit(t.Context(), fx.id("c2"))
	require.NoError(t, err)
	assert.NotEqual(t, "changed by the caller", second.Message)
	assert.Equal(t, fx.id("c1").String(), second.Parents[0].String())
	assert.Equal(t, 1, fx.fake.callCount("ReadChangeset"))
}

func TestReadCommitRefusesWhatIsNotACommit(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	for _, name := range []string{"treeC1", "blobA1", "tag"} {
		_, err := fx.asc.ReadCommit(t.Context(), fx.id(name))
		assert.True(t, git.IsErrNotExist(err), "%s: %v", name, err)
	}
	missing := git.MustIDFromString("0123456789012345678901234567890123456789")
	_, err := fx.asc.ReadCommit(t.Context(), missing)
	assert.True(t, git.IsErrNotExist(err))
	_, err = fx.asc.ReadCommit(t.Context(), git.Sha256ObjectFormat.EmptyTree())
	require.Error(t, err)
}

func entriesKey(entries []git.TreeEntryData) []string {
	keys := make([]string, len(entries))
	for i, e := range entries {
		keys[i] = e.ID.String() + " " + e.Mode.String() + " " + e.Name
	}
	return keys
}

func TestReadTreeMatchesGit(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	ctx := t.Context()
	// A commit id lists its root tree, and teaches the backend where its trees are.
	for _, name := range []string{"c1", "c4", "m1"} {
		want, err := fx.git.ReadTree(ctx, fx.id(name))
		require.NoError(t, err)
		got, err := fx.asc.ReadTree(ctx, fx.id(name))
		require.NoError(t, err)
		assert.Equal(t, entriesKey(want), entriesKey(got), name)
	}
	// Git orders "d-e.txt", "d.txt", "d" (a tree sorts as "d/"); plain byte order would put "d" first.
	root, err := fx.asc.ReadTree(ctx, fx.id("c4"))
	require.NoError(t, err)
	var order []string
	for _, e := range root[:5] {
		order = append(order, e.Name)
	}
	assert.Equal(t, []string{".gitattributes", "a2.txt", "big.bin", "d-e.txt", "d.txt"}, order)

	for _, name := range []string{"treeC4", "treeDir", "treeDirSb"} {
		want, err := fx.git.ReadTree(ctx, fx.id(name))
		require.NoError(t, err)
		got, err := fx.asc.ReadTree(ctx, fx.id(name))
		require.NoError(t, err, name)
		assert.Equal(t, entriesKey(want), entriesKey(got), name)
	}
	// Blob sizes arrive with the listing, so a size needs no further call.
	for _, e := range root {
		if e.Mode == git.EntryModeBlob && e.Name == "big.bin" {
			assert.Equal(t, int64(10000), e.Size)
		}
	}
}

func TestReadTreeOfAnUnknownTreeIsMissing(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	// This Backend has never seen a commit, so it cannot place the tree.
	_, err := fx.asc.ReadTree(t.Context(), fx.id("treeDir"))
	assert.True(t, git.IsErrNotExist(err), "%v", err)
	// A blob is not a tree.
	_, err = fx.asc.ReadCommit(t.Context(), fx.id("c4"))
	require.NoError(t, err)
	_, err = fx.asc.ReadTree(t.Context(), fx.id("main"))
	assert.True(t, git.IsErrNotExist(err), "%v", err)
	// The empty tree needs no server.
	entries, err := fx.asc.ReadTree(t.Context(), git.Sha1ObjectFormat.EmptyTree())
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestReadTreeRecursiveMatchesGit(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	ctx := t.Context()
	for _, name := range []string{"c4", "m1"} {
		_, err := fx.asc.ReadCommit(ctx, fx.id(name))
		require.NoError(t, err)
		for _, opt := range []git.TreeWalkOpts{{}, {Trees: true}, {Sizes: true}, {Trees: true, Sizes: true}} {
			var want, got []git.TreeEntryData
			for e, err := range fx.git.ReadTreeRecursive(ctx, fx.id(name), opt) {
				require.NoError(t, err)
				want = append(want, e)
			}
			for e, err := range fx.asc.ReadTreeRecursive(ctx, fx.id(name), opt) {
				require.NoError(t, err)
				got = append(got, e)
			}
			assert.Equal(t, entriesKey(want), entriesKey(got), "%s %+v", name, opt)
			if opt.Sizes {
				require.Len(t, got, len(want))
				for i := range want {
					assert.Equal(t, want[i].Size, got[i].Size, "%s size of %s", name, want[i].Name)
				}
			}
		}
	}
	// A subtree, by tree id, once the listing of its parent has been read.
	root, err := fx.asc.ReadTree(ctx, fx.id("c4"))
	require.NoError(t, err)
	require.NotEmpty(t, root)
	var want, got []string
	for e, err := range fx.git.ReadTreeRecursive(ctx, fx.id("treeDir"), git.TreeWalkOpts{Trees: true}) {
		require.NoError(t, err)
		want = append(want, e.Name)
	}
	for e, err := range fx.asc.ReadTreeRecursive(ctx, fx.id("treeDir"), git.TreeWalkOpts{Trees: true}) {
		require.NoError(t, err)
		got = append(got, e.Name)
	}
	assert.Equal(t, want, got)
	assert.Equal(t, []string{"b.txt", "sub", "sub/c.txt"}, got)
}

func readBlob(t *testing.T, be git.Backend, id git.ObjectID, limit int64) ([]byte, int64) {
	t.Helper()
	rc, size, err := be.OpenBlob(t.Context(), id, limit)
	require.NoError(t, err)
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	return data, size
}

func TestOpenBlobMatchesGit(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	for _, name := range []string{"blobA1", "big", "empty", "main"} {
		for _, limit := range []int64{-1, 0, 1, 5, 4096, 9999, 1 << 20} {
			want, wantSize := readBlob(t, fx.git, fx.id(name), limit)
			got, gotSize := readBlob(t, fx.asc, fx.id(name), limit)
			assert.Equal(t, want, got, "%s limit %d", name, limit)
			assert.Equal(t, wantSize, gotSize, "%s limit %d", name, limit)
		}
	}
	// Close before reading anything is allowed.
	rc, _, err := fx.asc.OpenBlob(t.Context(), fx.id("big"), -1)
	require.NoError(t, err)
	require.NoError(t, rc.Close())

	for _, name := range []string{"treeC1", "c1"} {
		_, _, err = fx.asc.OpenBlob(t.Context(), fx.id(name), -1)
		assert.True(t, git.IsErrNotExist(err), "%s: %v", name, err)
	}
}

func TestStatObjectsMatchesGit(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	ctx := t.Context()
	missing := git.MustIDFromString("0123456789012345678901234567890123456789")
	ids := []git.ObjectID{fx.id("blobA1"), fx.id("big"), fx.id("empty"), fx.id("treeC1"), fx.id("c1"), missing}
	want, err := fx.git.StatObjects(ctx, ids)
	require.NoError(t, err)
	got, err := fx.asc.StatObjects(ctx, ids)
	require.NoError(t, err)
	require.Len(t, got, len(want))
	for i := range want {
		assert.Equal(t, want[i].Missing, got[i].Missing, ids[i].String())
		assert.Equal(t, want[i].Type, got[i].Type, ids[i].String())
		if want[i].Type == git.ObjectBlob {
			assert.Equal(t, want[i].Size, got[i].Size, ids[i].String())
		}
	}
	// An annotated tag object cannot be read through asc.
	info, err := fx.asc.StatObjects(ctx, []git.ObjectID{fx.id("tag")})
	require.NoError(t, err)
	assert.True(t, info[0].Missing)
	_, err = fx.asc.ReadTag(ctx, fx.id("tag"))
	assert.ErrorIs(t, err, ascbackend.ErrUnsupported)
}

func TestStatObjectsUsesWhatListingsTaught(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	_, err := fx.asc.ReadTree(t.Context(), fx.id("c4"))
	require.NoError(t, err)
	before := fx.fake.callCount("Stat") + fx.fake.callCount("ReadFile")
	infos, err := fx.asc.StatObjects(t.Context(), []git.ObjectID{fx.id("big"), fx.id("main")})
	require.NoError(t, err)
	assert.Equal(t, int64(10000), infos[0].Size)
	assert.Equal(t, before, fx.fake.callCount("Stat")+fx.fake.callCount("ReadFile"))
}

func TestResolve(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	ctx := t.Context()
	for _, rev := range []string{"master", "HEAD", "refs/heads/master", "feature", "master~1", "master~1^2", fx.id("c2").String(), "refs/heads/signed"} {
		want, wantType, err := fx.git.Resolve(ctx, rev)
		require.NoError(t, err, rev)
		got, gotType, err := fx.asc.Resolve(ctx, rev)
		require.NoError(t, err, rev)
		assert.Equal(t, want.String(), got.String(), rev)
		assert.Equal(t, wantType, gotType, rev)
	}
	// A tag is peeled to its commit, whatever the git backend says.
	for _, rev := range []string{"v1", "refs/tags/v1", "lw"} {
		got, typ, err := fx.asc.Resolve(ctx, rev)
		require.NoError(t, err, rev)
		assert.Equal(t, fx.id("c2").String(), got.String())
		assert.Equal(t, git.ObjectCommit, typ)
	}
	// A tree or a blob by full id.
	for name, typ := range map[string]git.ObjectType{"treeC1": git.ObjectTree, "blobA1": git.ObjectBlob} {
		got, gotType, err := fx.asc.Resolve(ctx, fx.id(name).String())
		require.NoError(t, err, name)
		assert.Equal(t, fx.id(name).String(), got.String())
		assert.Equal(t, typ, gotType)
	}
	// Nothing, junk, and an abbreviated id (which asc serve refuses) do not exist.
	for _, rev := range []string{"nope", "", "bad\nrev", fx.id("c2").String()[:10], "0123456789012345678901234567890123456789"} {
		_, _, err := fx.asc.Resolve(ctx, rev)
		assert.True(t, git.IsErrNotExist(err), "%q: %v", rev, err)
	}
}

func TestListRefs(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	ctx := t.Context()
	for _, q := range []git.RefQuery{{Prefix: "refs/heads/"}, {Prefix: "refs/heads/m"}, {Prefix: "refs/heads/", Limit: 2}, {Prefix: "refs/tags/lw"}} {
		want, err := fx.git.ListRefs(ctx, q)
		require.NoError(t, err)
		got, err := fx.asc.ListRefs(ctx, q)
		require.NoError(t, err)
		require.Len(t, got, len(want), "%+v", q)
		for i := range want {
			assert.Equal(t, want[i].Name, got[i].Name)
			assert.Equal(t, want[i].Object.String(), got[i].Object.String())
			assert.Equal(t, want[i].Type, got[i].Type)
		}
	}
	// An annotated tag is listed as a reference to its commit.
	tags, err := fx.asc.ListRefs(ctx, git.RefQuery{Prefix: "refs/tags/"})
	require.NoError(t, err)
	require.Len(t, tags, 2)
	assert.Equal(t, "refs/tags/lw", tags[0].Name)
	assert.Equal(t, "refs/tags/v1", tags[1].Name)
	for _, r := range tags {
		assert.Equal(t, fx.id("c2").String(), r.Object.String())
		assert.Equal(t, string(git.ObjectCommit), r.Type)
	}

	all, err := fx.asc.ListRefs(ctx, git.RefQuery{})
	require.NoError(t, err)
	wantAll, err := fx.git.ListRefs(ctx, git.RefQuery{})
	require.NoError(t, err)
	assert.Equal(t, len(wantAll), len(all))

	reversed, err := fx.asc.ListRefs(ctx, git.RefQuery{Prefix: "refs/heads/", Sort: "-refname"})
	require.NoError(t, err)
	heads, err := fx.asc.ListRefs(ctx, git.RefQuery{Prefix: "refs/heads/"})
	require.NoError(t, err)
	slices.Reverse(heads)
	assert.Equal(t, heads, reversed)

	_, err = fx.asc.ListRefs(ctx, git.RefQuery{Sort: "-creatordate"})
	assert.ErrorIs(t, err, ascbackend.ErrUnsupported)
	_, err = fx.asc.ListRefs(ctx, git.RefQuery{Sort: "bad key;"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ascbackend.ErrUnsupported)

	// Contains is answered with IsAncestor.
	for _, contains := range []string{"c1", "c2", "f1", "c4"} {
		q := git.RefQuery{Prefix: "refs/heads/", Contains: fx.id(contains)}
		want, err := fx.git.ListRefs(ctx, q)
		require.NoError(t, err)
		got, err := fx.asc.ListRefs(ctx, q)
		require.NoError(t, err)
		var wantNames, gotNames []string
		for _, r := range want {
			wantNames = append(wantNames, r.Name)
		}
		for _, r := range got {
			gotNames = append(gotNames, r.Name)
		}
		assert.Equal(t, wantNames, gotNames, contains)
	}
}

func TestHead(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	_, err := fx.asc.Head(t.Context())
	assert.ErrorIs(t, err, ascbackend.ErrUnsupported, "the server reported no HEAD")
	fx.fake.head = "refs/heads/master"
	head, err := fx.asc.Head(t.Context())
	require.NoError(t, err)
	assert.Equal(t, git.RefName("refs/heads/master"), head)
	fx.fake.head = "not-a-branch"
	_, err = fx.asc.Head(t.Context())
	require.Error(t, err)
	assert.ErrorIs(t, fx.asc.SetHead(t.Context(), "refs/heads/master"), ascbackend.ErrUnsupported)
}

func ids(list []*git.CommitData) []string {
	out := make([]string, len(list))
	for i, c := range list {
		out[i] = c.ID.String()
	}
	return out
}

func collectLog(t *testing.T, be git.Backend, q git.LogQuery) []*git.CommitData {
	t.Helper()
	var out []*git.CommitData
	for c, err := range be.Log(t.Context(), q) {
		require.NoError(t, err)
		out = append(out, c)
	}
	return out
}

func TestLogAndCountMatchGit(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	id := fx.id
	since := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	queries := map[string]git.LogQuery{
		"from master":     {From: []git.ObjectID{id("c4")}},
		"from two":        {From: []git.ObjectID{id("c3"), id("f1")}},
		"not":             {From: []git.ObjectID{id("c4")}, Not: []git.ObjectID{id("c2")}},
		"limit":           {From: []git.ObjectID{id("c4")}, Limit: 3},
		"skip and limit":  {From: []git.ObjectID{id("c4")}, Skip: 2, Limit: 2},
		"skip":            {From: []git.ObjectID{id("c4")}, Skip: 4},
		"no merges":       {From: []git.ObjectID{id("c4")}, NoMerges: true},
		"no merges+limit": {From: []git.ObjectID{id("c4")}, NoMerges: true, Skip: 1, Limit: 3},
		"path":            {From: []git.ObjectID{id("c4")}, Paths: []string{"main.go"}},
		"two paths":       {From: []git.ObjectID{id("c4")}, Paths: []string{"main.go", "feature.txt"}},
		"follow":          {From: []git.ObjectID{id("c4")}, Paths: []string{"a2.txt"}, Follow: true},
		"all":             {All: true},
		"author":          {From: []git.ObjectID{id("c4")}, Author: "Ann"},
		"author none":     {From: []git.ObjectID{id("c4")}, Author: "Nobody"},
		"grep":            {From: []git.ObjectID{id("c4")}, Grep: "commit"},
		"since":           {From: []git.ObjectID{id("c4")}, Since: since},
		"until":           {From: []git.ObjectID{id("c4")}, Until: since},
		"signed":          {From: []git.ObjectID{id("sg")}},
	}
	for name, q := range queries {
		want := collectLog(t, fx.git, q)
		got := collectLog(t, fx.asc, q)
		assert.Equal(t, ids(want), ids(got), name)
		for i := range want {
			sameCommit(t, want[i], got[i], true)
		}
		if !q.NoMerges && !q.Follow {
			wantN, err := fx.git.Count(t.Context(), q)
			require.NoError(t, err)
			gotN, err := fx.asc.Count(t.Context(), q)
			require.NoError(t, err, name)
			assert.Equal(t, wantN, gotN, name)
		}
	}
	count, err := fx.asc.Count(t.Context(), git.LogQuery{From: []git.ObjectID{id("c4")}, NoMerges: true})
	require.NoError(t, err)
	assert.Equal(t, int64(len(collectLog(t, fx.git, queries["no merges"]))), count)

	for _, bad := range []git.LogQuery{
		{}, {From: []git.ObjectID{id("c4")}, Skip: -1}, {From: []git.ObjectID{id("c4")}, Follow: true},
		{From: []git.ObjectID{nil}},
	} {
		for _, err := range fx.asc.Log(t.Context(), bad) {
			require.Error(t, err)
			break
		}
	}
	// Stopping early must not leak the stream or fail.
	n := 0
	for _, err := range fx.asc.Log(t.Context(), git.LogQuery{From: []git.ObjectID{id("c4")}}) {
		require.NoError(t, err)
		if n++; n == 2 {
			break
		}
	}
	assert.Equal(t, 2, n)
}

func TestMergeBaseIsAncestorLastCommits(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	ctx := t.Context()
	for _, pair := range [][2]string{{"c4", "f1"}, {"c3", "f1"}, {"c2", "c3"}, {"c4", "f1"}} {
		want, wantErr := fx.git.MergeBase(ctx, fx.id(pair[0]), fx.id(pair[1]))
		got, err := fx.asc.MergeBase(ctx, fx.id(pair[0]), fx.id(pair[1]))
		require.NoError(t, wantErr)
		require.NoError(t, err)
		assert.Equal(t, want.String(), got.String())
		wantAnc, _ := fx.git.IsAncestor(ctx, fx.id(pair[0]), fx.id(pair[1]))
		gotAnc, err := fx.asc.IsAncestor(ctx, fx.id(pair[0]), fx.id(pair[1]))
		require.NoError(t, err)
		assert.Equal(t, wantAnc, gotAnc)
	}
	_, err := fx.asc.MergeBase(ctx, fx.id("c4"), fx.id("sg"))
	assert.ErrorIs(t, err, git.ErrNoMergebaseFound, "the signed commit is a separate root")
	ok, err := fx.asc.IsAncestor(ctx, fx.id("c1"), fx.id("c1"))
	require.NoError(t, err)
	assert.True(t, ok, "a commit is its own ancestor")
	_, err = fx.asc.IsAncestor(ctx, git.MustIDFromString("0123456789012345678901234567890123456789"), fx.id("c1"))
	assert.True(t, git.IsErrNotExist(err), "%v", err)

	// An orphan history shares no base. The signed commit shares c1, so build the case with a fresh root.
	orphan := gitRun(t, fx.dir, nil, "", "commit-tree", "-m", "orphan", fx.id("treeC1").String())
	_, err = fx.asc.MergeBase(ctx, fx.id("c4"), git.MustIDFromString(orphan))
	assert.ErrorIs(t, err, git.ErrNoMergebaseFound)

	for _, dir := range []string{"", "dir", "dir/", "/dir"} {
		names := []string{"b.txt", "sub", "main.go", "a2.txt", "missing"}
		want, err := fx.git.LastCommits(ctx, fx.id("c4"), dir, names)
		require.NoError(t, err)
		got, err := fx.asc.LastCommits(ctx, fx.id("c4"), dir, names)
		require.NoError(t, err)
		assert.Equal(t, mapStrings(want), mapStrings(got), "dir %q", dir)
	}
	_, err = fx.asc.LastCommits(ctx, fx.id("c4"), "", []string{"a/b"})
	require.Error(t, err)
}

func mapStrings(m map[string]git.ObjectID) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		out[k] = v.String()
	}
	return out
}

func collectBlame(t *testing.T, be git.Backend, at git.ObjectID, path string) []git.BlamePart {
	t.Helper()
	var out []git.BlamePart
	for p, err := range be.Blame(t.Context(), at, path, git.BlameOpts{}) {
		require.NoError(t, err)
		out = append(out, p)
	}
	return out
}

func TestBlameMatchesGit(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	for _, c := range []struct{ at, path string }{{"c4", "main.go"}, {"c4", "a2.txt"}, {"c1", "a.txt"}, {"c4", "dir/sub/c.txt"}, {"c4", "empty.txt"}} {
		want := collectBlame(t, fx.git, fx.id(c.at), c.path)
		got := collectBlame(t, fx.asc, fx.id(c.at), c.path)
		require.Len(t, got, len(want), "%s %s", c.at, c.path)
		for i := range want {
			assert.Equal(t, want[i].Sha, got[i].Sha)
			assert.Equal(t, want[i].Lines, got[i].Lines)
			assert.Equal(t, want[i].PreviousSha != "", got[i].PreviousSha != "", "%s %s part %d: git %q asc %q", c.at, c.path, i, want[i].PreviousSha, got[i].PreviousSha)
		}
	}
	for p, err := range fx.asc.Blame(t.Context(), fx.id("c4"), "missing.txt", git.BlameOpts{}) {
		assert.True(t, git.IsErrNotExist(err), "%v", err)
		assert.Empty(t, p.Sha)
	}
}

func paths(changes []git.PathChange) []string {
	out := make([]string, len(changes))
	for i, c := range changes {
		out[i] = c.Path
	}
	return out
}

func TestDiffMatchesGit(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	ctx := t.Context()
	zero := git.Sha1ObjectFormat.EmptyObjectID()
	type pair struct {
		base, head string
		opt        git.DiffOpts
	}
	for name, p := range map[string]pair{
		"c1..c2":        {"c1", "c2", git.DiffOpts{}},
		"c1..c4":        {"c1", "c4", git.DiffOpts{}},
		"c3..c4":        {"c3", "c4", git.DiffOpts{}},
		"rename":        {"c2", "c3", git.DiffOpts{}},
		"merge base":    {"c4", "f1", git.DiffOpts{MergeBase: true}},
		"merge base c3": {"f1", "c3", git.DiffOpts{MergeBase: true}},
		"same":          {"c4", "c4", git.DiffOpts{}},
	} {
		wantPaths, err := fx.git.ChangedPaths(ctx, fx.id(p.base), fx.id(p.head), p.opt)
		require.NoError(t, err, name)
		gotPaths, err := fx.asc.ChangedPaths(ctx, fx.id(p.base), fx.id(p.head), p.opt)
		require.NoError(t, err, name)
		assert.ElementsMatch(t, paths(wantPaths), paths(gotPaths), name)

		var want, got bytes.Buffer
		require.NoError(t, fx.git.UnifiedDiff(ctx, fx.id(p.base), fx.id(p.head), p.opt, &want), name)
		require.NoError(t, fx.asc.UnifiedDiff(ctx, fx.id(p.base), fx.id(p.head), p.opt, &got), name)
		assert.Equal(t, want.String(), got.String(), name)

		if !p.opt.MergeBase {
			wantStat, err := fx.git.DiffStat(ctx, fx.id(p.base), fx.id(p.head))
			require.NoError(t, err)
			gotStat, err := fx.asc.DiffStat(ctx, fx.id(p.base), fx.id(p.head))
			require.NoError(t, err)
			assert.Equal(t, wantStat, gotStat, name)
		}
	}
	// A zero base lists what a commit itself changed.
	for _, head := range []string{"c1", "c2", "c3", "m1", "c4"} {
		want, err := fx.git.ChangedPaths(ctx, zero, fx.id(head), git.DiffOpts{})
		require.NoError(t, err)
		got, err := fx.asc.ChangedPaths(ctx, zero, fx.id(head), git.DiffOpts{})
		require.NoError(t, err)
		assert.ElementsMatch(t, paths(want), paths(got), head)
	}
	merge, err := fx.asc.ChangedPaths(ctx, zero, fx.id("m1"), git.DiffOpts{})
	require.NoError(t, err)
	assert.Empty(t, merge, "diff-tree shows nothing for a merge")

	var binary bytes.Buffer
	require.NoError(t, fx.asc.UnifiedDiff(ctx, fx.id("c1"), fx.id("c2"), git.DiffOpts{Mode: git.DiffModeBinary}, &binary))
	assert.Contains(t, binary.String(), "GIT binary patch")
	var text bytes.Buffer
	require.NoError(t, fx.asc.UnifiedDiff(ctx, fx.id("c1"), fx.id("c2"), git.DiffOpts{}, &text))
	assert.Contains(t, text.String(), "Binary files")

	err = fx.asc.UnifiedDiff(ctx, fx.id("c1"), fx.id("c2"), git.DiffOpts{Mode: git.DiffModePatch}, io.Discard)
	assert.ErrorIs(t, err, ascbackend.ErrUnsupported)
	err = fx.asc.UnifiedDiff(ctx, fx.id("c1"), fx.id("c2"), git.DiffOpts{Mode: 99}, io.Discard)
	require.Error(t, err)
}

type archived struct {
	mode    int64
	content string
}

func untarGz(t *testing.T, data []byte) map[string]archived {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	tr := tar.NewReader(zr)
	out := map[string]archived{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return out
		}
		require.NoError(t, err)
		if h.Typeflag == tar.TypeXGlobalHeader || h.Typeflag == tar.TypeDir {
			continue
		}
		body, err := io.ReadAll(tr)
		require.NoError(t, err)
		out[h.Name] = archived{mode: h.Mode & 0o111, content: string(body)}
	}
}

func unzip(t *testing.T, data []byte) map[string]archived {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	out := map[string]archived{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		require.NoError(t, err)
		body, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		out[f.Name] = archived{mode: int64(f.Mode()) & 0o111, content: string(body)}
	}
	return out
}

func TestArchiveMatchesGit(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	for _, prefix := range []string{"", "proj-1/"} {
		var want, got bytes.Buffer
		require.NoError(t, fx.git.Archive(t.Context(), fx.id("c4"), git.TARGZ, prefix, &want))
		require.NoError(t, fx.asc.Archive(t.Context(), fx.id("c4"), git.TARGZ, prefix, &got))
		assert.Equal(t, untarGz(t, want.Bytes()), untarGz(t, got.Bytes()), "tar.gz %q", prefix)

		want.Reset()
		got.Reset()
		require.NoError(t, fx.git.Archive(t.Context(), fx.id("c4"), git.ZIP, prefix, &want))
		require.NoError(t, fx.asc.Archive(t.Context(), fx.id("c4"), git.ZIP, prefix, &got))
		assert.Equal(t, unzip(t, want.Bytes()), unzip(t, got.Bytes()), "zip %q", prefix)
	}
	err := fx.asc.Archive(t.Context(), fx.id("c4"), git.BUNDLE, "", io.Discard)
	assert.ErrorIs(t, err, ascbackend.ErrUnsupported)
	require.Error(t, fx.asc.Archive(t.Context(), fx.id("c4"), git.ArchiveType(0), "", io.Discard))
	require.Error(t, fx.asc.Archive(t.Context(), fx.id("c4"), git.ZIP, "bad\nprefix/", io.Discard))
}

func TestGrepMatchesGit(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	for name, q := range map[string]git.GrepQuery{
		"fixed":        {Search: "alpha"},
		"fixed case":   {Search: "BRAVO"},
		"any":          {Search: "alpha bravo", Options: git.GrepOptions{Mode: git.FixedAnyGrepMode}},
		"regexp":       {Search: "^b[a-z]+o$", Options: git.GrepOptions{Mode: git.RegExpGrepMode}},
		"context":      {Search: "bravo", Options: git.GrepOptions{ContextLineNumber: 1}},
		"context 2":    {Search: "line", Options: git.GrepOptions{ContextLineNumber: 2}},
		"per file":     {Search: "r", Options: git.GrepOptions{MatchesPerFile: 1}},
		"result limit": {Search: "r", Options: git.GrepOptions{MaxResultLimit: 2}},
		"filename":     {Search: "bravo", Options: git.GrepOptions{Filename: "dir/sub/c.txt"}},
		"none":         {Search: "no such text anywhere"},
	} {
		want, err := fx.git.Grep(t.Context(), fx.id("c4"), q)
		require.NoError(t, err, name)
		got, err := fx.asc.Grep(t.Context(), fx.id("c4"), q)
		require.NoError(t, err, name)
		assert.Equal(t, want, got, name)
	}
	_, err := fx.asc.Grep(t.Context(), fx.id("c4"), git.GrepQuery{})
	require.Error(t, err)
	_, err = fx.asc.Grep(t.Context(), fx.id("c4"), git.GrepQuery{Search: "(", Options: git.GrepOptions{Mode: git.RegExpGrepMode}})
	require.Error(t, err)
}

func TestAttributesAndLanguageStatsMatchGit(t *testing.T) {
	fx := newFixture(t, ascbackend.Options{})
	paths := []string{"d.txt", "docs/guide.md", "run.sh", "main.go", "nope"}
	names := []string{"linguist-language", "linguist-documentation", "text", "eol"}
	want, err := fx.git.Attributes(t.Context(), fx.id("c4"), paths, names)
	require.NoError(t, err)
	got, err := fx.asc.Attributes(t.Context(), fx.id("c4"), paths, names)
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, "Text", got["d.txt"]["linguist-language"])

	for _, at := range []string{"c1", "c2", "c4", "m1"} {
		wantStats, err := fx.git.LanguageStats(t.Context(), fx.id(at))
		require.NoError(t, err)
		gotStats, err := fx.asc.LanguageStats(t.Context(), fx.id(at))
		require.NoError(t, err)
		assert.Equal(t, wantStats, gotStats, at)
	}
	stats, err := fx.asc.LanguageStats(t.Context(), fx.id("c4"))
	require.NoError(t, err)
	assert.Contains(t, slices.Collect(maps.Keys(stats)), "Go")

	_, err = fx.asc.Attributes(t.Context(), fx.id("c4"), make([]string, 5000), names)
	require.Error(t, err)
}
