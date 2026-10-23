// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package gitcli is the repostore.Writer that runs the git binary on bare repositories.
//
// It is the Writer for legacy repositories and tests. The temporary-repository
// code in this package moved, mostly verbatim, out of services/repository/files,
// services/pull, services/wiki and services/repository.
//
// A change is built in a scratch repository under the local copy path. The
// scratch repository has no hooks and borrows the objects of the real
// repository through git alternates, so reading a large repository costs
// nothing. The change reaches the real repository only through git push, which
// runs the repository's hooks exactly as a user push does.
package gitcli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"forgejo.org/modules/git"
	_ "forgejo.org/modules/git/gitcli" // registers the git backend that git.OpenRepository binds
	"forgejo.org/modules/log"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
)

// Name is the name this Writer registers under.
const Name = repostore.DefaultName

// Timeouts and limits that bound the work one call can cost.
const (
	// forkTimeout bounds a fork's clone.
	forkTimeout = 10 * time.Minute
	// previewTimeout bounds the diff of a preview.
	previewTimeout = 30 * time.Second
	// maxPublishOutput caps the git output kept in an error.
	maxPublishOutput = 64 << 10
)

func init() {
	repostore.Register(Name, New())
}

// Writer changes repositories with the git binary. It has no state.
type Writer struct{}

var _ repostore.Writer = (*Writer)(nil)

// New returns a Writer.
func New() *Writer {
	return &Writer{}
}

// localCopyPath is the directory scratch repositories are created in. It matches modules/repository.LocalCopyPath.
func localCopyPath() string {
	if filepath.IsAbs(setting.Repository.Local.LocalCopyPath) {
		return setting.Repository.Local.LocalCopyPath
	}
	return path.Join(setting.AppDataPath, setting.Repository.Local.LocalCopyPath)
}

// scratch is a temporary git repository. Close removes it.
type scratch struct {
	ctx    context.Context
	dir    string
	format git.ObjectFormat
}

// newScratch creates an empty scratch repository whose objects can also be read from the object
// directories of repos. A bare scratch has no work tree.
func newScratch(ctx context.Context, prefix string, bare bool, formatName string, repos ...repostore.RepoRef) (*scratch, error) {
	format := git.ObjectFormatFromName(formatName)
	if format == nil {
		return nil, fmt.Errorf("%w: object format %q", repostore.ErrInvalidRequest, formatName)
	}
	base := localCopyPath()
	if err := os.MkdirAll(base, os.ModePerm); err != nil {
		log.Error("Unable to create the local copy path %s: %v", base, err)
		return nil, errors.New("unable to create the local copy path")
	}
	dir, err := os.MkdirTemp(base, prefix+".git")
	if err != nil {
		log.Error("Unable to create a temporary directory in %s: %v", base, err)
		return nil, errors.New("unable to create a temporary directory")
	}
	s := &scratch{ctx: ctx, dir: dir, format: format}
	if err := git.InitRepository(ctx, dir, bare, formatName); err != nil {
		s.Close()
		return nil, fmt.Errorf("init scratch repository: %w", err)
	}
	if err := s.addAlternates(bare, repos...); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// Close removes the scratch repository.
func (s *scratch) Close() {
	if err := util.RemoveAll(s.dir); err != nil {
		log.Error("Unable to remove the scratch repository %s: %v", s.dir, err)
	}
}

// addAlternates lets the scratch repository read the objects of repos. Every path was validated
// to hold no control character, so one path is always one alternates line.
func (s *scratch) addAlternates(bare bool, repos ...repostore.RepoRef) error {
	gitDir := s.dir
	if !bare {
		gitDir = filepath.Join(s.dir, ".git")
	}
	infoDir := filepath.Join(gitDir, "objects", "info")
	if err := os.MkdirAll(infoDir, 0o700); err != nil {
		return fmt.Errorf("create alternates directory: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(infoDir, "alternates"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open alternates: %w", err)
	}
	defer f.Close()
	seen := map[string]struct{}{}
	for _, repo := range repos {
		if err := repo.RequirePath(); err != nil {
			return err
		}
		objects := filepath.Join(repo.Path, "objects")
		if _, dup := seen[objects]; dup {
			continue
		}
		seen[objects] = struct{}{}
		if _, err := fmt.Fprintln(f, objects); err != nil {
			return fmt.Errorf("write alternates: %w", err)
		}
	}
	return nil
}

// run runs a git command in the scratch repository and returns its trimmed stdout.
func (s *scratch) run(cmd *git.Command, opts *git.RunOpts) (string, error) {
	if opts == nil {
		opts = &git.RunOpts{}
	}
	opts.Dir = s.dir
	stdout, _, err := cmd.RunStdString(opts)
	return strings.TrimSpace(stdout), err
}

// resolveID resolves a revision in the scratch repository to an object id.
func (s *scratch) resolveID(rev string) (git.ObjectID, error) {
	out, err := s.run(git.NewCommand(s.ctx, "rev-parse", "--verify").AddDynamicArguments(rev), nil)
	if err != nil {
		return nil, err
	}
	return git.NewIDFromString(out)
}

// errRecorder passes writes through and remembers the first failure, so the caller can
// tell a consumer that stopped reading from a git failure.
type errRecorder struct {
	w   io.Writer
	err error
}

func (e *errRecorder) Write(p []byte) (int, error) {
	n, err := e.w.Write(p)
	if err != nil && e.err == nil {
		e.err = err
	}
	return n, err
}

// limitString keeps the end of git output that goes into an error.
func limitString(s string) string {
	if len(s) > maxPublishOutput {
		return "..." + s[len(s)-maxPublishOutput:]
	}
	return s
}

// pushError maps the failure of a git push to the error types the services handle.
func pushError(stdout, stderr string, err error) error {
	stdout, stderr = limitString(stdout), limitString(stderr)
	switch {
	case strings.Contains(stderr, "non-fast-forward"):
		return &git.ErrPushOutOfDate{StdOut: stdout, StdErr: stderr, Err: err}
	case strings.Contains(stderr, "! [remote rejected]"):
		rejected := &git.ErrPushRejected{StdOut: stdout, StdErr: stderr, Err: err}
		rejected.GenerateMessage()
		return rejected
	case strings.Contains(stderr, "matches more than one"):
		return &git.ErrMoreThanOne{StdOut: stdout, StdErr: stderr, Err: err}
	}
	return fmt.Errorf("push failed: %w - %s\n%s", err, stderr, stdout)
}

// publish pushes a local ref or commit of the scratch repository to a ref of the real repository,
// so that the hooks of the real repository run.
func (s *scratch) publish(local string, to *repostore.RefUpdate, hookEnv []string) error {
	if err := to.Repo.RequirePath(); err != nil {
		return err
	}
	return git.Push(s.ctx, s.dir, git.PushOptions{
		Remote: to.Repo.Path,
		Branch: local + ":" + to.Ref,
		Force:  to.Force,
		Env:    hookEnv,
	})
}

func requireRepoPath(refs ...repostore.RepoRef) error {
	for _, r := range refs {
		if err := r.RequirePath(); err != nil {
			return err
		}
	}
	return nil
}
