// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.org/modules/git"
	_ "forgejo.org/modules/git/gitcli"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"

	"github.com/stretchr/testify/require"
)

func testRun(m *testing.M) error {
	gitHomePath, err := os.MkdirTemp(os.TempDir(), "git-home")
	if err != nil {
		return fmt.Errorf("unable to create temp dir: %w", err)
	}
	defer util.RemoveAll(gitHomePath)
	setting.Git.HomePath = gitHomePath

	if err = git.InitFull(context.Background()); err != nil {
		return fmt.Errorf("failed to call Init: %w", err)
	}

	if exitCode := m.Run(); exitCode != 0 {
		return fmt.Errorf("run test failed, ExitCode=%d", exitCode)
	}
	return nil
}

func TestMain(m *testing.M) {
	if err := testRun(m); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Test failed: %v", err)
		os.Exit(1)
	}
}

// gitRun runs git in dir for test set-up and returns its trimmed stdout.
func gitRun(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := git.NewCommand(t.Context()).AddArguments(git.ToTrustedCmdArgs(args)...)
	opts := &git.RunOpts{Dir: dir}
	if env != nil {
		opts.Env = append(os.Environ(), env...)
	}
	stdout, stderr, err := cmd.RunStdString(opts)
	require.NoError(t, err, stderr)
	return strings.TrimSpace(stdout)
}

func writeFile(t *testing.T, dir, name string, content []byte) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, content, 0o644))
}

// fixture is a bare repository built from a small, fixed history:
//
//	c1 (master~2, feature)  a.txt, dir/b.txt, .gitattributes
//	c2 (master~1, tag v1)   a.txt changed, big.bin (10000 bytes) added
//	c3 (master)             a.txt renamed to a2.txt
//	orphan                  an unrelated root commit
type fixture struct {
	path   string
	repo   *git.Repository
	be     git.Backend
	ids    map[string]git.ObjectID // c1, c2, c3, orphan, tag (the annotated tag object), blobA1 (a.txt at c1), big (big.bin)
	bigLen int
}

func (f *fixture) id(name string) git.ObjectID { return f.ids[name] }

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "work")
	require.NoError(t, os.MkdirAll(work, 0o755))

	commit := func(msg, date string) string {
		env := []string{
			"GIT_AUTHOR_NAME=Ann Author", "GIT_AUTHOR_EMAIL=ann@example.com", "GIT_AUTHOR_DATE=" + date,
			"GIT_COMMITTER_NAME=Cy Committer", "GIT_COMMITTER_EMAIL=cy@example.com", "GIT_COMMITTER_DATE=" + date,
		}
		gitRun(t, work, nil, "add", "-A")
		gitRun(t, work, env, "commit", "-q", "-m", msg)
		return gitRun(t, work, nil, "rev-parse", "HEAD")
	}

	gitRun(t, work, nil, "init", "-q", "-b", "master")
	writeFile(t, work, "a.txt", []byte("alpha one\nsecond line\n"))
	writeFile(t, work, "dir/b.txt", []byte("bravo\n"))
	writeFile(t, work, ".gitattributes", []byte("*.txt linguist-language=Text\n"))
	c1 := commit("first commit\n\nbody of the first", "2026-01-01T10:00:00+00:00")
	gitRun(t, work, nil, "branch", "feature")

	writeFile(t, work, "a.txt", []byte("alpha two\nsecond line\nthird line\n"))
	big := make([]byte, 10000)
	for i := range big {
		big[i] = byte('a' + i%26)
	}
	big[100] = 0 // a NUL byte makes git treat the file as binary
	writeFile(t, work, "big.bin", big)
	c2 := commit("second commit", "2026-01-02T10:00:00+00:00")
	gitRun(t, work, []string{"GIT_COMMITTER_NAME=Tag Er", "GIT_COMMITTER_EMAIL=tag@example.com", "GIT_COMMITTER_DATE=2026-01-02T11:00:00+00:00"},
		"tag", "-a", "-m", "release one", "v1")

	require.NoError(t, os.Rename(filepath.Join(work, "a.txt"), filepath.Join(work, "a2.txt")))
	c3 := commit("rename a", "2026-01-03T10:00:00+00:00")

	gitRun(t, work, nil, "checkout", "-q", "--orphan", "unrelated")
	gitRun(t, work, nil, "rm", "-rq", "--cached", ".")
	require.NoError(t, os.RemoveAll(filepath.Join(work, "dir")))
	writeFile(t, work, "z.txt", []byte("zulu\n"))
	orphan := commit("unrelated root", "2026-01-04T10:00:00+00:00")
	gitRun(t, work, nil, "checkout", "-q", "master")

	bare := filepath.Join(root, "repo.git")
	gitRun(t, root, nil, "clone", "-q", "--bare", work, bare)
	gitRun(t, bare, nil, "symbolic-ref", "HEAD", "refs/heads/master")

	repo, err := git.OpenRepository(t.Context(), bare)
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	be, err := repo.Backend()
	require.NoError(t, err)

	parse := func(s string) git.ObjectID {
		id, err := git.NewIDFromString(s)
		require.NoError(t, err)
		return id
	}
	return &fixture{
		path:   bare,
		repo:   repo,
		be:     be,
		bigLen: len(big),
		ids: map[string]git.ObjectID{
			"c1": parse(c1), "c2": parse(c2), "c3": parse(c3), "orphan": parse(orphan),
			"tag":    parse(gitRun(t, bare, nil, "rev-parse", "refs/tags/v1")),
			"blobA1": parse(gitRun(t, bare, nil, "rev-parse", c1+":a.txt")),
			"big":    parse(gitRun(t, bare, nil, "rev-parse", c2+":big.bin")),
		},
	}
}
