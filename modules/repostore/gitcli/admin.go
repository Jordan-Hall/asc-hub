// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/util"
)

// UpdateRefs pushes refs from one repository to another (or the same), so that hooks run once.
func (w *Writer) UpdateRefs(ctx context.Context, req repostore.UpdateRefsRequest) ([]repostore.RefResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if err := requireRepoPath(req.Repo); err != nil {
		return nil, err
	}
	from := req.Repo
	if req.From != nil {
		from = *req.From
		if err := requireRepoPath(from); err != nil {
			return nil, err
		}
	}

	results := make([]repostore.RefResult, len(req.Updates))
	specs := make([]string, 0, len(req.Updates))
	for i, u := range req.Updates {
		old, err := resolveRef(ctx, req.Repo.Path, u.Name)
		if err != nil {
			return nil, err
		}
		results[i] = repostore.RefResult{Name: u.Name, Old: old}
		switch {
		case u.Delete:
			specs = append(specs, ":"+u.Name)
		case u.Force:
			specs = append(specs, "+"+u.NewRev+":"+u.Name)
		default:
			specs = append(specs, u.NewRev+":"+u.Name)
		}
	}

	cmd := git.NewCommand(ctx, "push", "--atomic").AddDashesAndList(append([]string{req.Repo.Path}, specs...)...)
	stdout, stderr, err := cmd.RunStdString(&git.RunOpts{Env: req.HookEnv, Dir: from.Path})
	if err != nil {
		return nil, pushError(stdout, stderr, err)
	}

	for i, u := range req.Updates {
		if u.Delete {
			continue
		}
		id, err := resolveRef(ctx, req.Repo.Path, u.Name)
		if err != nil {
			return nil, err
		}
		results[i].New = id
	}
	return results, nil
}

// resolveRef returns the commit a ref points at, or nil when the ref does not exist.
func resolveRef(ctx context.Context, repoPath, name string) (git.ObjectID, error) {
	out, _, err := git.NewCommand(ctx, "rev-parse", "--verify", "--quiet").AddDynamicArguments(name).RunStdString(&git.RunOpts{Dir: repoPath})
	if err != nil {
		if git.IsErrorExitCode(err, 1) {
			return nil, nil
		}
		return nil, fmt.Errorf("resolve %s: %w", name, err)
	}
	return git.NewIDFromString(strings.TrimSpace(out))
}

// Init creates an empty bare repository.
func (w *Writer) Init(ctx context.Context, req repostore.InitRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if err := requireRepoPath(req.Repo); err != nil {
		return err
	}
	if err := refuseRoot(req.Repo.Path); err != nil {
		return err
	}
	exists, err := util.IsExist(req.Repo.Path)
	if err != nil {
		log.Error("Unable to check if %s exists. Error: %v", req.Repo.Path, err)
		return err
	}
	if exists {
		return repostore.ErrExists
	}
	if err := git.InitRepository(ctx, req.Repo.Path, true, req.Repo.ObjectFormat); err != nil {
		return fmt.Errorf("git.InitRepository: %w", err)
	}
	if req.DefaultBranch != "" {
		if _, _, err := git.NewCommand(ctx, "symbolic-ref", "HEAD").AddDynamicArguments(git.BranchPrefix + req.DefaultBranch).
			RunStdString(&git.RunOpts{Dir: req.Repo.Path}); err != nil {
			return fmt.Errorf("unable to set the default branch to %s: %w", req.DefaultBranch, err)
		}
	}
	return nil
}

// Fork copies a repository into a new bare repository.
func (w *Writer) Fork(ctx context.Context, req repostore.ForkRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if err := requireRepoPath(req.Repo, req.From); err != nil {
		return err
	}
	if err := refuseRoot(req.Repo.Path); err != nil {
		return err
	}
	exists, err := util.IsExist(req.Repo.Path)
	if err != nil {
		return err
	}
	if exists {
		return repostore.ErrExists
	}

	cmd := git.NewCommand(ctx, "clone", "--bare")
	if req.SingleBranch != "" {
		cmd.AddArguments("--single-branch", "--branch").AddDynamicArguments(req.SingleBranch)
	}
	if stdout, _, err := cmd.AddDashesAndList(req.From.Path, req.Repo.Path).
		SetDescription(fmt.Sprintf("Fork(git clone): %s to %s", req.From.Name, req.Repo.Name)).
		RunStdBytes(&git.RunOpts{Timeout: forkTimeout}); err != nil {
		log.Error("Fork (git clone) failed for %s (from %s):\nStdout: %s\nError: %v", req.Repo.Name, req.From.Name, limitString(string(stdout)), err)
		return fmt.Errorf("git clone: %w", err)
	}
	return nil
}

// Delete removes a repository from disk.
func (w *Writer) Delete(ctx context.Context, repo repostore.RepoRef) error {
	if err := repo.Validate(); err != nil {
		return err
	}
	if err := requireRepoPath(repo); err != nil {
		return err
	}
	if err := refuseRoot(repo.Path); err != nil {
		return err
	}
	return util.RemoveAll(repo.Path)
}

// refuseRoot refuses a path that cannot be the directory of a repository, so a bad reference cannot make Init,
// Fork or Delete write to or remove a directory that is not one. Repository directories end in ".git".
func refuseRoot(p string) error {
	clean := filepath.Clean(p)
	if clean == filepath.VolumeName(clean)+string(filepath.Separator) || clean == "." {
		return errors.New("repository path is a filesystem root")
	}
	if !strings.HasSuffix(filepath.Base(clean), ".git") {
		return errors.New("repository path does not end in .git")
	}
	return nil
}

const notRegularFileMode = os.ModeSymlink | os.ModeNamedPipe | os.ModeSocket | os.ModeDevice | os.ModeCharDevice | os.ModeIrregular

// Size returns the disk consumption of a repository.
func (w *Writer) Size(ctx context.Context, repo repostore.RepoRef) (repostore.Size, error) {
	if err := repo.Validate(); err != nil {
		return repostore.Size{}, err
	}
	if err := requireRepoPath(repo); err != nil {
		return repostore.Size{}, err
	}
	var size int64
	err := filepath.WalkDir(repo.Path, func(_ string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) { // ignore the error because some files (like temp/lock file) may be deleted during traversing.
			return nil
		} else if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if os.IsNotExist(err) { // ignore the error as above
			return nil
		} else if err != nil {
			return err
		}
		if (info.Mode() & notRegularFileMode) == 0 {
			size += info.Size()
		}
		return nil
	})
	if err != nil {
		return repostore.Size{}, err
	}
	return repostore.Size{Logical: size, Unique: size}, nil
}
