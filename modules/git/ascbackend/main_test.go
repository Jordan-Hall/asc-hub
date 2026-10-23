// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/git/ascbackend"
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
func gitRun(t *testing.T, dir string, env []string, stdin string, args ...string) string {
	t.Helper()
	cmd := git.NewCommand(t.Context()).AddArguments(git.ToTrustedCmdArgs(args)...)
	opts := &git.RunOpts{Dir: dir}
	if env != nil {
		opts.Env = append(os.Environ(), env...)
	}
	if stdin != "" {
		opts.Stdin = strings.NewReader(stdin)
	}
	stdout, stderr, err := cmd.RunStdString(opts)
	require.NoError(t, err, stderr)
	return strings.TrimSpace(stdout)
}

func writeFile(t *testing.T, dir, name string, content []byte, mode os.FileMode) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(name))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, content, mode))
}

const goSource = `package main

import (
	"fmt"
	"os"
	"strings"
)

// Greeter says hello to everybody it knows about.
type Greeter struct {
	names []string
}

func (g *Greeter) Add(name string) {
	g.names = append(g.names, name)
}

func (g *Greeter) Greet() string {
	return "hello " + strings.Join(g.names, ", ")
}

func main() {
	g := &Greeter{}
	for _, a := range os.Args[1:] {
		g.Add(a)
	}
	fmt.Println(g.Greet())
}
`

// fixture is a bare repository with a history that exercises every read:
//
//	c1  a.txt, dir/, d.txt, d-e.txt, d/x.txt (git orders d-e.txt, d.txt, d), run.sh (exec), link (symlink), empty.txt, docs/guide.md
//	c2  a.txt changed, big.bin (binary), main.go; tags lw (lightweight) and v1 (annotated)
//	c3  a.txt renamed to a2.txt; a submodule entry "sub"
//	f1  (branch feature, from c1) feature.txt
//	m1  merge of feature into master
//	c4  main.go changed
//	sg  (branch signed, from c1) a commit with a gpgsig header
type fixture struct {
	t      *testing.T
	root   string // the repository root: dir is root/owner/repo.git
	dir    string
	ids    map[string]git.ObjectID
	git    git.Backend // the git binary
	asc    *ascbackend.Backend
	fake   *fakeAsc
	client ascv1.RepoClient
	repo   *git.Repository
}

// newAsc makes another Backend over the same server, with nothing learned yet.
func (fx *fixture) newAsc(t *testing.T, opts ascbackend.Options) *ascbackend.Backend {
	t.Helper()
	if opts.Auth == nil {
		opts.Auth = func(ctx context.Context) (context.Context, error) { return ctx, nil }
	}
	asc, err := ascbackend.New(fx.client, fakeRepoName, fx.repo, opts)
	require.NoError(t, err)
	return asc
}

func (fx *fixture) id(name string) git.ObjectID {
	id, ok := fx.ids[name]
	if !ok {
		panic("no fixture id " + name)
	}
	return id
}

func newFixture(t *testing.T, opts ascbackend.Options) *fixture {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "work")
	require.NoError(t, os.MkdirAll(work, 0o755))

	commit := func(msg, date, tz string) string {
		stamp := date + " " + tz
		env := []string{
			"GIT_AUTHOR_NAME=Ann Author", "GIT_AUTHOR_EMAIL=ann@example.com", "GIT_AUTHOR_DATE=" + stamp,
			"GIT_COMMITTER_NAME=Cy Committer", "GIT_COMMITTER_EMAIL=cy@example.com", "GIT_COMMITTER_DATE=" + stamp,
		}
		gitRun(t, work, nil, "", "add", "-A")
		gitRun(t, work, env, "", "commit", "-q", "--allow-empty", "-m", msg)
		return gitRun(t, work, nil, "", "rev-parse", "HEAD")
	}

	gitRun(t, work, nil, "", "init", "-q", "-b", "master")
	writeFile(t, work, "a.txt", []byte("alpha one\nsecond line\n"), 0o644)
	writeFile(t, work, "dir/b.txt", []byte("bravo\n"), 0o644)
	writeFile(t, work, "dir/sub/c.txt", []byte("charlie\nbravo again\n"), 0o644)
	writeFile(t, work, "d.txt", []byte("dot\n"), 0o644)
	writeFile(t, work, "d-e.txt", []byte("dash\n"), 0o644)
	writeFile(t, work, "d/x.txt", []byte("x in d\n"), 0o644)
	writeFile(t, work, "run.sh", []byte("#!/bin/sh\necho hi\n"), 0o755)
	writeFile(t, work, "empty.txt", nil, 0o644)
	writeFile(t, work, "docs/guide.md", []byte("# Guide\n\nAlpha is first.\n"), 0o644)
	writeFile(t, work, ".gitattributes", []byte("d.txt linguist-language=Text\ndocs/*.md linguist-documentation=false\n*.sh text eol=lf\n"), 0o644)
	require.NoError(t, os.Symlink("a.txt", filepath.Join(work, "link")))
	c1 := commit("first commit\n\nbody of the first", "2026-01-01T10:00:00", "+0000")
	gitRun(t, work, nil, "", "branch", "feature")
	gitRun(t, work, nil, "", "branch", "signed")

	writeFile(t, work, "a.txt", []byte("alpha two\nsecond line\nthird line\n"), 0o644)
	big := make([]byte, 10000)
	for i := range big {
		big[i] = byte('a' + i%26)
	}
	big[100] = 0
	writeFile(t, work, "big.bin", big, 0o644)
	writeFile(t, work, "main.go", []byte(goSource), 0o644)
	c2 := commit("second commit", "2026-01-02T10:00:00", "+0200")
	gitRun(t, work, []string{"GIT_COMMITTER_NAME=Tag Er", "GIT_COMMITTER_EMAIL=tag@example.com", "GIT_COMMITTER_DATE=2026-01-02T11:00:00+0000"},
		"", "tag", "-a", "-m", "release one", "v1")
	gitRun(t, work, nil, "", "tag", "lw")

	require.NoError(t, os.Rename(filepath.Join(work, "a.txt"), filepath.Join(work, "a2.txt")))
	gitRun(t, work, nil, "", "update-index", "--add", "--cacheinfo", "160000,"+c1+",sub")
	c3 := commit("rename a", "2026-01-03T10:00:00", "-0530")

	gitRun(t, work, nil, "", "checkout", "-q", "feature")
	writeFile(t, work, "feature.txt", []byte("a feature\n"), 0o644)
	f1 := commit("feature work", "2026-01-04T10:00:00", "+0000")
	gitRun(t, work, nil, "", "checkout", "-q", "master")
	env := []string{
		"GIT_AUTHOR_NAME=Ann Author", "GIT_AUTHOR_EMAIL=ann@example.com", "GIT_AUTHOR_DATE=2026-01-05T10:00:00 +0000",
		"GIT_COMMITTER_NAME=Cy Committer", "GIT_COMMITTER_EMAIL=cy@example.com", "GIT_COMMITTER_DATE=2026-01-05T10:00:00 +0000",
	}
	gitRun(t, work, env, "", "merge", "-q", "--no-ff", "feature", "-m", "merge feature")
	m1 := gitRun(t, work, nil, "", "rev-parse", "HEAD")
	writeFile(t, work, "main.go", []byte(goSource+"\n// trailer\n"), 0o644)
	c4 := commit("tweak main", "2026-01-06T10:00:00", "+0000")

	repos := filepath.Join(root, "repos")
	bare := filepath.Join(repos, "owner", "repo.git")
	require.NoError(t, os.MkdirAll(filepath.Dir(bare), 0o755))
	gitRun(t, root, nil, "", "clone", "-q", "--bare", work, bare)
	gitRun(t, bare, nil, "", "symbolic-ref", "HEAD", "refs/heads/master")

	// A signed commit: the signature header is not checked by git when it is only stored.
	tree := gitRun(t, bare, nil, "", "rev-parse", c1+"^{tree}")
	raw := "tree " + tree + "\nauthor Sig Ner <sig@example.com> 1767225600 +0100\ncommitter Sig Ner <sig@example.com> 1767225600 +0100\n" +
		"gpgsig -----BEGIN PGP SIGNATURE-----\n \n abcdef\n -----END PGP SIGNATURE-----\n\nsigned commit\n"
	sg := gitRun(t, bare, nil, raw, "hash-object", "-t", "commit", "-w", "--stdin")
	gitRun(t, bare, nil, "", "update-ref", "refs/heads/signed", sg)

	parse := func(s string) git.ObjectID {
		id, err := git.NewIDFromString(s)
		require.NoError(t, err)
		return id
	}
	rev := func(spec string) git.ObjectID { return parse(gitRun(t, bare, nil, "", "rev-parse", spec)) }
	ids := map[string]git.ObjectID{
		"c1": parse(c1), "c2": parse(c2), "c3": parse(c3), "f1": parse(f1), "m1": parse(m1), "c4": parse(c4), "sg": parse(sg),
		"tag":       rev("refs/tags/v1"),
		"treeC1":    rev(c1 + "^{tree}"),
		"treeC4":    rev(c4 + "^{tree}"),
		"treeDir":   rev(c4 + ":dir"),
		"treeDirSb": rev(c4 + ":dir/sub"),
		"blobA1":    rev(c1 + ":a.txt"),
		"big":       rev(c2 + ":big.bin"),
		"empty":     rev(c1 + ":empty.txt"),
		"main":      rev(c4 + ":main.go"),
	}

	gitRepo, err := git.OpenRepositoryWithBackend(t.Context(), bare, "gitcli")
	require.NoError(t, err)
	t.Cleanup(func() { _ = gitRepo.Close() })
	gitBe, err := gitRepo.Backend()
	require.NoError(t, err)

	fake := newFake(t, bare)
	client, _ := fake.serve()
	ascRepo, err := git.OpenRepositoryWithBackend(t.Context(), bare, "gitcli")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ascRepo.Close() })
	if opts.Auth == nil {
		opts.Auth = func(ctx context.Context) (context.Context, error) { return ctx, nil }
	}
	asc, err := ascbackend.New(client, fakeRepoName, ascRepo, opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = asc.Close() })
	return &fixture{t: t, root: repos, dir: bare, ids: ids, git: gitBe, asc: asc, fake: fake, client: client, repo: ascRepo}
}
