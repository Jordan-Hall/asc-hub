// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"
	ascwriter "forgejo.org/modules/repostore/asc"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/util"
)

// Init connects the bridge directory handler to the asc Writer, so that the directory of a repository is made,
// renamed and removed together with the repository (SPEC 13.4). It does nothing while [asc] is off.
func Init(context.Context) error {
	if !setting.Asc.Enabled {
		return nil
	}
	ascwriter.SetBridge(Lifecycle{})
	return nil
}

// Lifecycle is the asc Writer's view of the bridge directory.
type Lifecycle struct{}

var _ ascwriter.Bridge = Lifecycle{}

// checkPath refuses a path that cannot be the directory of a repository, so that a bad reference cannot make
// the bridge make, move or remove a directory that is not one: it has to be absolute, below the repository
// root, and named <owner>/<name>.git.
func checkPath(path string) error {
	if path == "" || !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\n") {
		return fmt.Errorf("ascbridge: %q is not a bridge path", path)
	}
	if _, err := ascName(path); err != nil {
		return err
	}
	return nil
}

// ensureDir makes the directory a bare git repository when it is not one yet, with HEAD on defaultBranch
// (when given). A directory that already is one is left alone.
func ensureDir(ctx context.Context, path, defaultBranch string) error {
	if err := checkPath(path); err != nil {
		return err
	}
	if st, err := os.Stat(filepath.Join(path, "HEAD")); err == nil && !st.IsDir() {
		return nil
	}
	if err := git.InitRepository(ctx, path, true, git.Sha1ObjectFormat.Name()); err != nil {
		return fmt.Errorf("ascbridge: init bridge: %w", err)
	}
	if defaultBranch != "" {
		if _, _, err := git.NewCommand(ctx, "symbolic-ref", "HEAD").AddDynamicArguments(git.BranchPrefix + defaultBranch).
			RunStdString(&git.RunOpts{Dir: path}); err != nil {
			return fmt.Errorf("ascbridge: set HEAD of the bridge: %w", err)
		}
	}
	return nil
}

// Prepare implements ascwriter.Bridge.
func (Lifecycle) Prepare(ctx context.Context, repo repostore.RepoRef, defaultBranch string) error {
	return ensureDir(ctx, repo.Path, defaultBranch)
}

// Remove implements ascwriter.Bridge.
func (Lifecycle) Remove(_ context.Context, repo repostore.RepoRef) error {
	if err := checkPath(repo.Path); err != nil {
		return err
	}
	return util.RemoveAll(repo.Path)
}

// Rename implements ascwriter.Bridge. A bridge that was never made is not an error.
func (Lifecycle) Rename(_ context.Context, from, to repostore.RepoRef) error {
	if err := errors.Join(checkPath(from.Path), checkPath(to.Path)); err != nil {
		return err
	}
	if _, err := os.Stat(from.Path); os.IsNotExist(err) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(to.Path), 0o750); err != nil {
		return err
	}
	return util.Rename(from.Path, to.Path)
}
