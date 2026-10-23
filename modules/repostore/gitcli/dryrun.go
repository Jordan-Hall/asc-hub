// Copyright 2019 The Gitea Authors.
// All rights reserved.
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

	"github.com/gobwas/glob"
)

// maxProtectedFiles bounds the protected files a dry run reports.
const maxProtectedFiles = 10

// dryRunTarget is where a dry run reads from: a git directory and the revisions to compare in it.
type dryRunTarget struct {
	dir      string
	env      []string
	baseRev  string
	headRev  string
	headIsID bool
	close    func()
}

// dryRun reports whether the head would merge into the base, and what the merge would conflict on.
func (w *Writer) dryRun(ctx context.Context, req *repostore.MergeRequest) (*repostore.MergeResult, error) {
	// With git-merge-tree the check runs on the bare base repository. Without it the check needs a work tree,
	// so it runs in a scratch repository. Counting commits never needs a work tree.
	onBare := git.SupportGitMergeTree || req.DivergenceOnly
	target, err := w.dryRunTarget(ctx, req, onBare)
	if err != nil {
		return nil, err
	}
	defer target.close()

	// 1. the merge base
	mergeBase, _, err := git.NewCommand(ctx, "merge-base").AddDashesAndList(target.baseRev, target.headRev).
		RunStdString(&git.RunOpts{Dir: target.dir, Env: target.env})
	if err != nil {
		var err2 error
		mergeBase, err2 = revParse(ctx, target, target.baseRev)
		if err2 != nil {
			return nil, fmt.Errorf("GetMergeBase: %v and can't find commit ID for base: %w", err, err2)
		}
	}
	mergeBase = strings.TrimSpace(mergeBase)

	// 2. the head commit
	headCommit := target.headRev
	if !target.headIsID {
		if headCommit, err = revParse(ctx, target, target.headRev); err != nil {
			return nil, fmt.Errorf("GetRefCommitID: can't find commit ID for head: %w", err)
		}
	}

	result := &repostore.MergeResult{}
	if result.MergeBase, err = git.NewIDFromString(mergeBase); err != nil {
		return nil, err
	}
	if result.Head, err = git.NewIDFromString(headCommit); err != nil {
		return nil, err
	}

	if req.WantDivergence || req.DivergenceOnly {
		divergence, err := git.GetDivergingCommits(ctx, target.dir, target.baseRev, target.headRev, target.env)
		if err != nil {
			return nil, fmt.Errorf("GetDivergingCommits: %w", err)
		}
		result.Ahead, result.Behind = divergence.Ahead, divergence.Behind
	}
	if req.DivergenceOnly {
		return result, nil
	}

	// If the head commit is equal to the merge base it roughly means that the
	// head commit is a parent of the base commit.
	if headCommit == mergeBase {
		result.Status = repostore.DryRunAncestor
		return result, nil
	}

	// 3. conflicts and emptiness
	conflict, empty, files, err := checkConflicts(ctx, target, mergeBase)
	if err != nil {
		return nil, fmt.Errorf("checkConflicts: %w", err)
	}
	if len(files) > repostore.MaxConflictsReported {
		files = files[:repostore.MaxConflictsReported]
	}
	result.Conflicts = files
	switch {
	case conflict:
		result.Status = repostore.DryRunConflict
		return result, nil
	case empty:
		result.Status = repostore.DryRunEmpty
		return result, nil
	}

	// 4. protected files
	if result.ProtectedFiles, err = protectedFiles(ctx, target, mergeBase, req.ProtectedGlobs); err != nil {
		return nil, fmt.Errorf("checkPullFilesProtection: %w", err)
	}
	if len(result.ProtectedFiles) > 0 {
		log.Trace("Found %d protected files changed", len(result.ProtectedFiles))
	}
	result.Status = repostore.DryRunMergeable
	return result, nil
}

func revParse(ctx context.Context, t *dryRunTarget, rev string) (string, error) {
	out, _, err := git.NewCommand(ctx, "rev-parse", "--verify").AddDynamicArguments(rev).RunStdString(&git.RunOpts{Dir: t.dir, Env: t.env})
	return strings.TrimSpace(out), err
}

// dryRunTarget finds where a dry run reads the base and the head from.
func (w *Writer) dryRunTarget(ctx context.Context, req *repostore.MergeRequest, onBare bool) (*dryRunTarget, error) {
	if !onBare {
		m, err := newMergeScratch(ctx, req)
		if err != nil {
			return nil, err
		}
		return &dryRunTarget{dir: m.dir, baseRev: baseRef, headRev: trackingRef, close: m.Close}, nil
	}

	t := &dryRunTarget{dir: req.Base.Path, baseRev: req.BaseRef, headRev: req.HeadRev, close: func() {}}
	_, idErr := git.NewIDFromString(req.HeadRev)
	t.headIsID = idErr == nil

	if filepath.Clean(req.Base.Path) == filepath.Clean(req.Head.Path) {
		if !t.headIsID && !refExists(ctx, t.dir, t.headRev) {
			return nil, &repostore.RefNotFoundError{Ref: req.HeadRev}
		}
		return t, nil
	}

	// The head is in another repository. Resolve it there and let the base repository read its objects.
	if strings.ContainsRune(req.Head.Path, os.PathListSeparator) {
		return nil, errors.New("the head repository path cannot be used as an alternate object directory")
	}
	if !refExists(ctx, req.Head.Path, req.HeadRev) {
		return nil, &repostore.RefNotFoundError{Ref: req.HeadRev}
	}
	headCommit, _, err := git.NewCommand(ctx, "rev-parse", "--verify").AddDynamicArguments(req.HeadRev + "^{commit}").
		RunStdString(&git.RunOpts{Dir: req.Head.Path})
	if err != nil {
		return nil, fmt.Errorf("resolve the head: %w", err)
	}
	t.headRev = strings.TrimSpace(headCommit)
	t.headIsID = true
	t.env = append(os.Environ(), `GIT_ALTERNATE_OBJECT_DIRECTORIES=`+filepath.Join(req.Head.Path, "objects"))
	return t, nil
}

// checkConflicts checks whether merging the head into the base would conflict, and if it does not, whether the
// merge would change nothing.
func checkConflicts(ctx context.Context, t *dryRunTarget, mergeBase string) (conflict, empty bool, files []string, err error) {
	if git.SupportGitMergeTree {
		// Check for conflicts via a merge-tree.
		treeHash, conflict, conflictFiles, err := mergeTree(ctx, t.dir, mergeBase, t.baseRev, t.headRev, t.env)
		if err != nil {
			return false, false, nil, fmt.Errorf("MergeTree: %w", err)
		}
		if conflict {
			log.Trace("Found %d files conflicted: %v", len(conflictFiles), conflictFiles)
			return true, false, conflictFiles, nil
		}

		// No conflicts were detected, now check if the pull request actually
		// contains anything useful via a diff. git-diff-tree(1) with --quiet
		// will return exit code 0 if there's no diff and exit code 1 if there's
		// a diff.
		err = git.NewCommand(ctx, "diff-tree", "--quiet").AddDynamicArguments(treeHash, mergeBase).Run(&git.RunOpts{Dir: t.dir, Env: t.env})
		isEmpty := true
		if err != nil {
			if git.IsErrorExitCode(err, 1) {
				isEmpty = false
			} else {
				return false, false, nil, fmt.Errorf("DiffTree: %w", err)
			}
		}
		return false, isEmpty, nil, nil
	}

	// AttemptThreeWayMerge first - this is much quicker than plain patch to base
	conflict, conflictFiles, err := attemptThreeWayMerge(ctx, t.dir, mergeBase, t.baseRev, t.headRev)
	if err != nil {
		return false, false, nil, err
	}
	if conflict {
		log.Trace("Found %d files conflicted: %v", len(conflictFiles), conflictFiles)
		return true, false, conflictFiles, nil
	}

	// No conflicts detected so we need to check if the patch is empty...
	// a. Write the newly merged tree and check the new tree-hash
	treeHash, _, err := git.NewCommand(ctx, "write-tree").RunStdString(&git.RunOpts{Dir: t.dir})
	if err != nil {
		lsfiles, _, _ := git.NewCommand(ctx, "ls-files", "-u").RunStdString(&git.RunOpts{Dir: t.dir})
		return false, false, nil, fmt.Errorf("unable to write unconflicted tree: %w\n`git ls-files -u`:\n%s", err, limitString(lsfiles))
	}
	treeHash = strings.TrimSpace(treeHash)
	baseTree, err := revParse(ctx, t, t.baseRev+"^{tree}")
	if err != nil {
		return false, false, nil, err
	}

	// b. compare the new tree-hash with the base tree hash
	return false, treeHash == baseTree, nil, nil
}

// protectedFiles lists the files changed between the merge base and the head that match a glob.
func protectedFiles(ctx context.Context, t *dryRunTarget, mergeBase string, globs []string) ([]string, error) {
	patterns := make([]glob.Glob, 0, len(globs))
	for _, expr := range globs {
		expr = strings.TrimSpace(strings.ToLower(expr))
		if expr == "" {
			continue
		}
		g, err := glob.Compile(expr, '.', '/')
		if err != nil {
			log.Info("Invalid glob expression '%s' (skipped): %v", expr, err)
			continue
		}
		patterns = append(patterns, g)
	}
	if len(patterns) == 0 {
		return nil, nil
	}

	gitRepo, err := git.OpenRepository(ctx, t.dir)
	if err != nil {
		return nil, err
	}
	defer gitRepo.Close()
	affectedFiles, err := git.GetAffectedFiles(gitRepo, mergeBase, t.headRev, t.env)
	if err != nil {
		return nil, err
	}
	changedProtectedFiles := make([]string, 0, maxProtectedFiles)
	for _, affectedFile := range affectedFiles {
		lpath := strings.ToLower(affectedFile)
		for _, pat := range patterns {
			if pat.Match(lpath) {
				changedProtectedFiles = append(changedProtectedFiles, lpath)
				break
			}
		}
		if len(changedProtectedFiles) >= maxProtectedFiles {
			break
		}
	}
	return changedProtectedFiles, nil
}
