// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"
)

// CherryPick applies the change a commit made onto the parent.
func (w *Writer) CherryPick(ctx context.Context, req repostore.PickRequest) (*repostore.CommitResult, error) {
	return w.pick(ctx, req, false)
}

// Revert applies the inverse of the change a commit made onto the parent.
func (w *Writer) Revert(ctx context.Context, req repostore.PickRequest) (*repostore.CommitResult, error) {
	return w.pick(ctx, req, true)
}

func (w *Writer) pick(ctx context.Context, req repostore.PickRequest, revert bool) (*repostore.CommitResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if err := requireRepoPath(req.Repo); err != nil {
		return nil, err
	}
	if req.Update != nil {
		if err := requireRepoPath(req.Update.Repo); err != nil {
			return nil, err
		}
	}
	if err := req.Repo.RequireObjectFormat(); err != nil {
		return nil, err
	}

	// git-merge-tree works on a bare repository. The fallback, read-tree -m, needs a work tree.
	s, err := newScratch(ctx, "pick", git.SupportGitMergeTree, req.Repo.ObjectFormat, req.Repo)
	if err != nil {
		return nil, err
	}
	defer s.Close()

	// The side of the change that the parent does not have is "right"; the state it came from is "base".
	base, err := s.parentOrEmptyTree(req.Commit)
	if err != nil {
		return nil, err
	}
	right := req.Commit.String()
	if revert {
		right, base = base, right
	}

	var tree git.ObjectID
	if git.SupportGitMergeTree {
		treeHash, conflict, files, err := mergeTree(ctx, s.dir, base, req.Parent.String(), right, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to three-way merge %s onto %s: %w", right, req.Parent, err)
		}
		if conflict {
			return nil, &repostore.ConflictError{Files: files}
		}
		if tree, err = git.NewIDFromString(treeHash); err != nil {
			return nil, err
		}
	} else {
		conflict, files, err := attemptThreeWayMerge(ctx, s.dir, base, req.Parent.String(), right)
		if err != nil {
			return nil, fmt.Errorf("failed to three-way merge %s onto %s: %w", right, req.Parent, err)
		}
		if conflict {
			return nil, &repostore.ConflictError{Files: files}
		}
		if tree, err = s.writeTree(); err != nil {
			return nil, err
		}
	}

	commit, err := s.commitTree(req.Parent, tree, &req.Meta)
	if err != nil {
		return nil, err
	}
	if req.Update != nil {
		in := repostore.PublishInput{GitDir: s.dir, Commit: commit, Base: req.Parent}
		if err := s.beforePublish(req.BeforePublish, in); err != nil {
			return nil, err
		}
		if err := s.publish(commit.String(), req.Update, req.HookEnv); err != nil {
			return nil, err
		}
	}
	return &repostore.CommitResult{Commit: commit, Tree: tree}, nil
}

// parentOrEmptyTree returns the first parent of a commit, or the empty tree for a root commit.
func (s *scratch) parentOrEmptyTree(commit git.ObjectID) (string, error) {
	out, err := s.run(git.NewCommand(s.ctx, "rev-list", "--parents", "-n", "1").AddDynamicArguments(commit.String()), nil)
	if err != nil {
		return "", fmt.Errorf("read commit %s: %w", commit, err)
	}
	fields := strings.Fields(out)
	if len(fields) == 0 || fields[0] != commit.String() {
		return "", fmt.Errorf("read commit %s: unexpected output", commit)
	}
	if len(fields) == 1 {
		return s.format.EmptyTree().String(), nil
	}
	return fields[1], nil
}

// ApplyPatch applies a unified diff onto the parent.
func (w *Writer) ApplyPatch(ctx context.Context, req repostore.PatchRequest) (*repostore.CommitResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if err := requireRepoPath(req.Repo); err != nil {
		return nil, err
	}
	if req.Update != nil {
		if err := requireRepoPath(req.Update.Repo); err != nil {
			return nil, err
		}
	}
	if err := req.Repo.RequireObjectFormat(); err != nil {
		return nil, err
	}

	s, err := newScratch(ctx, "patch", true, req.Repo.ObjectFormat, req.Repo)
	if err != nil {
		return nil, err
	}
	defer s.Close()

	if err := s.readTree(req.Parent); err != nil {
		return nil, err
	}

	stdout := &strings.Builder{}
	stderr := &strings.Builder{}

	// `git apply --index` has strange behaviour when running on a bare repo. As there is no working tree, it shouldn't
	// attempt to change files in a working tree, and typically doesn't. But when a conflict occurs between the patch
	// and the index, `git apply --index` will drop conflicting files into the current working directory as if it were
	// the working tree. This creates security risks where the files are actually being dropped into the bare repo. To
	// avoid this, we create a temporary working directory for the command where those junk files can be dropped and
	// then deleted:
	applyWorkingDir, err := os.MkdirTemp("", "forgejo-applydiffpatch-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(applyWorkingDir)

	cmdApply := git.NewCommand(ctx)
	cmdApply.AddArguments("--git-dir") // sets the path to the repo, like env GIT_DIR; does not change working dir, like `-C` does
	cmdApply.AddDynamicArguments(s.dir)
	cmdApply.AddArguments("apply", "--index", "--recount", "--cached", "--ignore-whitespace", "--whitespace=fix", "--binary", "-3")
	if err := cmdApply.Run(&git.RunOpts{
		Dir:    applyWorkingDir, // see note about applyWorkingDir above
		Stdout: stdout,
		Stderr: stderr,
		Stdin:  strings.NewReader(req.Patch),
	}); err != nil {
		return nil, fmt.Errorf("Error: Stdout: %s\nStderr: %s\nErr: %w", limitString(stdout.String()), limitString(stderr.String()), err)
	}

	tree, err := s.writeTree()
	if err != nil {
		return nil, err
	}
	commit, err := s.commitTree(req.Parent, tree, &req.Meta)
	if err != nil {
		return nil, err
	}
	if req.Update != nil {
		in := repostore.PublishInput{GitDir: s.dir, Commit: commit, Base: req.Parent}
		if err := s.beforePublish(req.BeforePublish, in); err != nil {
			return nil, err
		}
		if err := s.publish(commit.String(), req.Update, req.HookEnv); err != nil {
			return nil, err
		}
	}
	return &repostore.CommitResult{Commit: commit, Tree: tree}, nil
}
