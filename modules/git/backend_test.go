// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package git

import (
	"bytes"
	"context"
	"io"
	"iter"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	fakeID   = MustIDFromString(strings.Repeat("a", 40))
	fakeTree = MustIDFromString(strings.Repeat("b", 40))
	fakeBlob = MustIDFromString(strings.Repeat("c", 40))
)

// fakeBackend answers from memory. The embedded nil Backend makes every method it does not
// override panic, so a Repository method that bypasses the backend fails its test.
type fakeBackend struct {
	Backend
	calls  []string
	closed bool
}

func (f *fakeBackend) Resolve(_ context.Context, rev string) (ObjectID, ObjectType, error) {
	f.calls = append(f.calls, "Resolve "+rev)
	if rev == "missing" {
		return nil, "", ErrNotExist{ID: rev}
	}
	return fakeID, ObjectCommit, nil
}

func (f *fakeBackend) ReadCommit(_ context.Context, id ObjectID) (*CommitData, error) {
	f.calls = append(f.calls, "ReadCommit "+id.String())
	return &CommitData{
		ID:        id,
		Tree:      fakeTree,
		Parents:   []ObjectID{fakeBlob},
		Author:    &Signature{Name: "fake author"},
		Committer: nil,
		Message:   "from the fake\n",
	}, nil
}

func (f *fakeBackend) ReadTree(_ context.Context, id ObjectID) ([]TreeEntryData, error) {
	f.calls = append(f.calls, "ReadTree "+id.String())
	return []TreeEntryData{
		{ID: fakeBlob, Name: "plain", Mode: EntryModeBlob, Size: -1},
		{ID: fakeBlob, Name: "sized", Mode: EntryModeExec, Size: 7},
	}, nil
}

func (f *fakeBackend) ReadTreeRecursive(_ context.Context, id ObjectID, opt TreeWalkOpts) iter.Seq2[TreeEntryData, error] {
	call := "ReadTreeRecursive " + id.String()
	if opt.Trees {
		call += " trees"
	}
	if opt.Sizes {
		call += " sizes"
	}
	f.calls = append(f.calls, call)
	return func(yield func(TreeEntryData, error) bool) {
		yield(TreeEntryData{ID: fakeBlob, Name: "dir/file", Mode: EntryModeBlob, Size: 3}, nil)
	}
}

func (f *fakeBackend) StatObjects(_ context.Context, ids []ObjectID) ([]ObjectInfo, error) {
	f.calls = append(f.calls, "StatObjects")
	infos := make([]ObjectInfo, len(ids))
	for i, id := range ids {
		infos[i] = ObjectInfo{ID: id, Type: ObjectBlob, Size: 42}
	}
	return infos, nil
}

func (f *fakeBackend) OpenBlob(_ context.Context, id ObjectID, limit int64) (io.ReadCloser, int64, error) {
	f.calls = append(f.calls, "OpenBlob "+id.String())
	body := "0123456789"
	if limit >= 0 && limit < int64(len(body)) {
		return io.NopCloser(strings.NewReader(body[:limit])), int64(len(body)), nil
	}
	return io.NopCloser(strings.NewReader(body)), int64(len(body)), nil
}

func (f *fakeBackend) ListRefs(_ context.Context, q RefQuery) ([]Reference, error) {
	f.calls = append(f.calls, "ListRefs "+q.Prefix)
	return []Reference{{Name: "refs/heads/x", Object: fakeID, Type: "commit"}}, nil
}

func (f *fakeBackend) Head(context.Context) (RefName, error) {
	f.calls = append(f.calls, "Head")
	return "refs/heads/fake-main", nil
}

func (f *fakeBackend) Count(_ context.Context, q LogQuery) (int64, error) {
	f.calls = append(f.calls, "Count")
	if q.All {
		return 11, nil
	}
	return int64(len(q.From)), nil
}

func (f *fakeBackend) ChangedPaths(_ context.Context, base, head ObjectID, _ DiffOpts) ([]PathChange, error) {
	f.calls = append(f.calls, "ChangedPaths "+base.String()+" "+head.String())
	return []PathChange{{Path: "one"}, {Path: "two"}}, nil
}

func (f *fakeBackend) Archive(_ context.Context, at ObjectID, format ArchiveType, prefix string, w io.Writer) error {
	f.calls = append(f.calls, "Archive "+at.String()+" "+format.String()+" "+prefix)
	_, err := w.Write([]byte("archive"))
	return err
}

func (f *fakeBackend) Grep(_ context.Context, at ObjectID, q GrepQuery) ([]*GrepResult, error) {
	f.calls = append(f.calls, "Grep "+at.String()+" "+q.Search+" "+q.Options.RefName)
	return []*GrepResult{{Filename: "hit"}}, nil
}

func (f *fakeBackend) ReadTag(_ context.Context, id ObjectID) (*TagData, error) {
	f.calls = append(f.calls, "ReadTag "+id.String())
	return &TagData{ID: id, Object: fakeID, Type: "commit", Tagger: &Signature{Name: "tagger"}, Message: "tag message\n"}, nil
}

func (f *fakeBackend) Close() error {
	f.closed = true
	return nil
}

func openFake(t *testing.T) (*Repository, *fakeBackend) {
	t.Helper()
	fake := &fakeBackend{}
	name := "fake-" + strings.ReplaceAll(t.Name(), "/", "-")
	RegisterBackend(name, func(*Repository) (Backend, error) { return fake, nil })
	repo, err := OpenRepositoryWithBackend(t.Context(), t.TempDir(), name)
	require.NoError(t, err)
	return repo, fake
}

func TestBackendRegistry(t *testing.T) {
	// The default backend registers itself when gitcli is linked (see backend_link_test.go).
	assert.Contains(t, Backends(), DefaultBackendName)

	_, err := OpenRepositoryWithBackend(t.Context(), t.TempDir(), "no-such-backend")
	require.ErrorIs(t, err, ErrNoBackend)

	assert.Panics(t, func() { RegisterBackend(DefaultBackendName, func(*Repository) (Backend, error) { return nil, nil }) })
	assert.Panics(t, func() { RegisterBackend("nil-factory", nil) })

	// A path that is not a directory is refused before any backend is made.
	_, err = OpenRepositoryWithBackend(t.Context(), t.TempDir()+"/missing", DefaultBackendName)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNoBackend)
}

func TestRepositoryBindsDefaultBackend(t *testing.T) {
	repo, err := OpenRepository(t.Context(), t.TempDir())
	require.NoError(t, err)
	be, err := repo.Backend()
	require.NoError(t, err)
	assert.NotNil(t, be)

	// A Repository that was not made by OpenRepository binds the default backend on first use.
	literal := &Repository{Path: t.TempDir()}
	be, err = literal.Backend()
	require.NoError(t, err)
	assert.NotNil(t, be)

	// Close releases the backend; the next use binds a new one.
	require.NoError(t, repo.Close())
	assert.Nil(t, repo.backend)
	_, err = repo.Backend()
	require.NoError(t, err)
}

func TestRepositoryClosesBackend(t *testing.T) {
	repo, fake := openFake(t)
	require.NoError(t, repo.Close())
	assert.True(t, fake.closed)
}

func TestCommitDispatch(t *testing.T) {
	repo, fake := openFake(t)

	commit, err := repo.GetCommit("some-rev")
	require.NoError(t, err)
	assert.Equal(t, []string{"Resolve some-rev", "ReadCommit " + fakeID.String()}, fake.calls)
	assert.Equal(t, fakeID.String(), commit.ID.String())
	assert.Equal(t, fakeTree.String(), commit.Tree.ID.String())
	assert.Equal(t, "from the fake\n", commit.Message())
	assert.Equal(t, "fake author", commit.Author.Name)
	assert.NotNil(t, commit.Committer, "a commit with no committer gets an empty one, as the reader gave")
	assert.Equal(t, 1, commit.ParentCount())

	fake.calls = nil
	_, err = repo.GetCommit("missing")
	assert.True(t, IsErrNotExist(err))
	assert.Equal(t, []string{"Resolve missing"}, fake.calls)

	fake.calls = nil
	count, err := commit.CommitsCount()
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)
	count, err = repo.GetAllCommitsCount()
	require.NoError(t, err)
	assert.EqualValues(t, 11, count)
}

func TestTreeAndBlobDispatch(t *testing.T) {
	repo, fake := openFake(t)

	tree := NewTree(repo, fakeTree)
	entries, err := tree.ListEntries()
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, "plain", entries[0].Name())
	assert.False(t, entries[0].sized)
	assert.True(t, entries[1].IsExecutable())
	assert.EqualValues(t, 7, entries[1].Size(), "a size the backend loaded is not asked for again")
	assert.Same(t, tree, entries[0].ptree)
	assert.Equal(t, []string{"ReadTree " + fakeTree.String()}, fake.calls)

	// A second listing is the cached one.
	_, err = tree.ListEntries()
	require.NoError(t, err)
	assert.Len(t, fake.calls, 1)

	fake.calls = nil
	fast, err := tree.ListEntriesRecursiveFast()
	require.NoError(t, err)
	require.Len(t, fast, 1)
	assert.Equal(t, "dir/file", fast[0].Name())
	_, err = NewTree(repo, fakeTree).ListEntriesRecursiveWithSize()
	require.NoError(t, err)
	assert.Equal(t, []string{
		"ReadTreeRecursive " + fakeTree.String() + " trees",
		"ReadTreeRecursive " + fakeTree.String() + " trees sizes",
	}, fake.calls)

	fake.calls = nil
	assert.EqualValues(t, 42, entries[0].Size())
	blob, err := repo.GetBlob(fakeBlob.String())
	require.NoError(t, err)
	assert.EqualValues(t, 42, blob.Size())
	assert.EqualValues(t, 42, blob.Size(), "the size is cached")
	assert.Equal(t, []string{"StatObjects", "StatObjects"}, fake.calls)

	fake.calls = nil
	rc, err := blob.DataAsync()
	require.NoError(t, err)
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, "0123456789", string(data))
	assert.EqualValues(t, 10, blob.Size(), "opening the blob records its size")

	rc, size, err := blob.NewTruncatedReader(4)
	require.NoError(t, err)
	data, err = io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, "0123", string(data))
	assert.EqualValues(t, 10, size)

	_, _, err = blob.NewReader(5)
	var tooLarge BlobTooLargeError
	require.ErrorAs(t, err, &tooLarge)
	assert.EqualValues(t, 10, tooLarge.Size)
	rc, size, err = blob.NewReader(10)
	require.NoError(t, err)
	assert.EqualValues(t, 10, size)
	require.NoError(t, rc.Close())
}

func TestRefDispatch(t *testing.T) {
	repo, fake := openFake(t)

	refs, err := repo.GetRefsFiltered(BranchPrefix)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, "refs/heads/x", refs[0].Name)
	assert.Equal(t, "x", refs[0].ShortName())
	commit, err := refs[0].Commit()
	require.NoError(t, err)
	assert.Equal(t, fakeID.String(), commit.ID.String())

	branch, err := repo.GetHEADBranch()
	require.NoError(t, err)
	assert.Equal(t, "fake-main", branch.Name)

	assert.Equal(t, []string{"ListRefs refs/heads/", "ReadCommit " + fakeID.String(), "Head"}, fake.calls)
}

func TestDiffAndBulkDispatch(t *testing.T) {
	repo, fake := openFake(t)

	zero := Sha1ObjectFormat.EmptyObjectID().String()
	files, err := repo.GetFilesChangedBetween(zero, "HEAD")
	require.NoError(t, err)
	assert.Equal(t, []string{"one", "two"}, files)
	assert.Equal(t, []string{"Resolve HEAD", "ChangedPaths " + zero + " " + fakeID.String()}, fake.calls, "a full id is not resolved")

	fake.calls = nil
	var buf bytes.Buffer
	require.NoError(t, repo.CreateArchive(t.Context(), ZIP, &buf, true, fakeBlob.String()))
	assert.Equal(t, "archive", buf.String())
	require.NoError(t, repo.CreateArchive(t.Context(), TARGZ, io.Discard, false, fakeBlob.String()))
	assert.Equal(t, []string{
		"Archive " + fakeBlob.String() + " zip " + strings.TrimSuffix(repo.Path[strings.LastIndexAny(repo.Path, `/\`)+1:], ".git") + "/",
		"Archive " + fakeBlob.String() + " tar.gz ",
	}, fake.calls)
	assert.Error(t, repo.CreateArchive(t.Context(), ArchiveType(0), io.Discard, false, "x"))

	fake.calls = nil
	res, err := GrepSearch(t.Context(), repo, "needle", GrepOptions{RefName: "main"})
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, []string{"Resolve main", "Grep " + fakeID.String() + " needle main"}, fake.calls)

	fake.calls = nil
	typ, err := repo.GetTagType(fakeBlob)
	require.NoError(t, err)
	assert.Equal(t, "blob", typ)
	assert.Equal(t, []string{"StatObjects"}, fake.calls)
}
