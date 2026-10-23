// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package code

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/git"
	"forgejo.org/modules/indexer/code/internal"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testSignature = &git.Signature{Name: "Alice", Email: "alice@example.com", When: time.Unix(1_700_000_000, 0).UTC()}

type testRepo struct {
	ref  repostore.RepoRef
	repo *git.Repository
}

func newTestRepo(t *testing.T) *testRepo {
	t.Helper()
	ref := repostore.RepoRef{ID: 1, Owner: "o", Name: "r", ObjectFormat: "sha1", Path: filepath.Join(t.TempDir(), "r.git")}
	w, err := repostore.Default()
	require.NoError(t, err)
	require.NoError(t, w.Init(t.Context(), repostore.InitRequest{Repo: ref, DefaultBranch: "main"}))
	// The generated hooks run the Forgejo binary, which here is the test binary; the commits below need none.
	_, _, err = git.NewCommand(t.Context(), "config", "core.hooksPath").AddDynamicArguments(filepath.Join(ref.Path, "hooks")).RunStdString(&git.RunOpts{Dir: ref.Path})
	require.NoError(t, err)

	repo, err := git.OpenRepository(t.Context(), ref.Path)
	require.NoError(t, err)
	t.Cleanup(func() { repo.Close() })
	return &testRepo{ref: ref, repo: repo}
}

// commit writes a commit on main on top of parent (nil for the first) and returns its id.
func (r *testRepo) commit(t *testing.T, parent git.ObjectID, ops ...repostore.FileOp) string {
	t.Helper()
	w, err := repostore.Default()
	require.NoError(t, err)
	res, err := w.CreateCommit(t.Context(), repostore.CommitRequest{
		Repo: r.ref, Parent: parent, Ops: ops,
		Meta:   repostore.CommitMeta{Author: testSignature, Committer: testSignature, Message: "commit"},
		Update: &repostore.RefUpdate{Repo: r.ref, Ref: "refs/heads/main"},
	})
	require.NoError(t, err)
	return res.Commit.String()
}

func put(path, content string) repostore.FileOp {
	return repostore.FileOp{Kind: repostore.OpPut, Path: path, Content: strings.NewReader(content)}
}

func putMode(path, content string, mode git.EntryMode) repostore.FileOp {
	return repostore.FileOp{Kind: repostore.OpPut, Path: path, Mode: mode, Content: strings.NewReader(content)}
}

func del(path string) repostore.FileOp {
	return repostore.FileOp{Kind: repostore.OpDelete, Path: path}
}

func mustID(t *testing.T, s string) git.ObjectID {
	t.Helper()
	id, err := git.NewIDFromString(s)
	require.NoError(t, err)
	return id
}

func updatedFiles(changes *internal.RepoChanges) map[string]int64 {
	files := map[string]int64{}
	for _, u := range changes.Updates {
		files[u.Filename] = u.Size
	}
	return files
}

func TestGenesisChanges(t *testing.T) {
	r := newTestRepo(t)
	head := r.commit(t, nil,
		put("README.md", "hello\n"),
		put("dir/sub/deep.txt", "deep"),
		putMode("run.sh", "#!/bin/sh\n", git.EntryModeExec),
		putMode("link", "README.md", git.EntryModeSymlink),
	)

	changes, err := genesisChanges(r.repo, head)
	require.NoError(t, err)
	// Symlinks and directories are not indexed; the sizes come with the entries.
	assert.Equal(t, map[string]int64{"README.md": 6, "dir/sub/deep.txt": 4, "run.sh": 10}, updatedFiles(changes))
	assert.Empty(t, changes.RemovedFilenames)
	for _, u := range changes.Updates {
		assert.True(t, u.Sized)
		assert.Len(t, u.BlobSha, 40)
	}

	_, err = genesisChanges(r.repo, "0000000000000000000000000000000000000001")
	require.Error(t, err)
}

func TestNonGenesisChanges(t *testing.T) {
	r := newTestRepo(t)
	first := r.commit(t, nil,
		put("README.md", "one\n"),
		put("dir/b.txt", "b"),
		put("dir/sub/c.txt", "c"),
		put("keep.txt", "keep"),
		putMode("run.sh", "#!/bin/sh\n", git.EntryModeExec),
		putMode("link", "keep.txt", git.EntryModeSymlink),
	)
	second := r.commit(t, mustID(t, first),
		put("README.md", "changed\n"),                     // modified
		del("dir/b.txt"),                                  // deleted
		del("dir/sub/c.txt"),                              // a directory replaced by a file
		put("dir/sub", "now a file"),                      // ...
		put("new/deep/x.txt", "x"),                        // added below new directories
		putMode("run.sh", "run.sh", git.EntryModeSymlink), // a file replaced by a symlink
		put("link", "no longer a link"),                   // a symlink replaced by a file
	)

	repo := &repo_model.Repository{ID: 1, CodeIndexerStatus: &repo_model.RepoIndexerStatus{CommitSha: first}}
	changes, err := nonGenesisChanges(t.Context(), r.repo, repo, second)
	require.NoError(t, err)

	assert.Equal(t, map[string]int64{"README.md": 8, "dir/sub": 10, "new/deep/x.txt": 1, "link": 16}, updatedFiles(changes))
	removed := slices.Clone(changes.RemovedFilenames)
	slices.Sort(removed)
	assert.Equal(t, []string{"dir/b.txt", "dir/sub/c.txt", "run.sh"}, removed)
	assert.NotContains(t, updatedFiles(changes), "keep.txt", "an unchanged file is not reported")

	// Patterns match the path from the repository root, not the file name.
	defer test.MockVariableValue(&setting.Indexer.ExcludePatterns, setting.IndexerGlobFromString("vendor/**"))()
	third := r.commit(t, mustID(t, second), put("vendor/lib/x.js", "x"), put("lib/y.js", "y"))
	patterned, err := nonGenesisChanges(t.Context(), r.repo, &repo_model.Repository{ID: 1, CodeIndexerStatus: &repo_model.RepoIndexerStatus{CommitSha: second}}, third)
	require.NoError(t, err)
	assert.Equal(t, map[string]int64{"lib/y.js": 1}, updatedFiles(patterned))

	// No change at all.
	none, err := nonGenesisChanges(t.Context(), r.repo, repo, first)
	require.NoError(t, err)
	assert.Empty(t, none.Updates)
	assert.Empty(t, none.RemovedFilenames)
}

func TestHeadTreeLookup(t *testing.T) {
	r := newTestRepo(t)
	head := r.commit(t, nil, put("a/b/c.txt", "c"), put("top.txt", "t"), put("a/file", "f"))
	commit, err := r.repo.GetCommit(head)
	require.NoError(t, err)
	tree := &headTree{root: &commit.Tree, dirs: map[string]map[string]*git.TreeEntry{}}

	for path, want := range map[string]bool{
		"top.txt":       true,
		"a/b/c.txt":     true,
		"a/file":        true,
		"a/b":           true, // a directory is an entry too
		"a/missing":     false,
		"missing/x.txt": false,
		"top.txt/x":     false, // below a file
		"a/file/x/y":    false,
	} {
		entry, err := tree.lookup(path)
		require.NoError(t, err, path)
		assert.Equal(t, want, entry != nil, path)
	}
}
