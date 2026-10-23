// Copyright 2019 The Gitea Authors.
// All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
	"forgejo.org/modules/util"
)

type errMergeConflict struct {
	filename string
}

func (e *errMergeConflict) Error() string {
	return fmt.Sprintf("conflict detected at: %s", e.filename)
}

func attemptMerge(ctx context.Context, file *unmergedFile, tmpBasePath string, filesToRemove *[]string, filesToAdd *[]git.IndexObjectInfo) error {
	log.Trace("Attempt to merge:\n%v", file)

	switch {
	case file.stage1 != nil && (file.stage2 == nil || file.stage3 == nil):
		// 1. Deleted in one or both:
		//
		// Conflict <==> the stage1 !SameAs to the undeleted one
		if (file.stage2 != nil && !file.stage1.SameAs(file.stage2)) || (file.stage3 != nil && !file.stage1.SameAs(file.stage3)) {
			// Conflict!
			return &errMergeConflict{file.stage1.path}
		}

		// Not a genuine conflict and we can simply remove the file from the index
		*filesToRemove = append(*filesToRemove, file.stage1.path)
		return nil
	case file.stage1 == nil && file.stage2 != nil && (file.stage3 == nil || file.stage2.SameAs(file.stage3)):
		// 2. Added in ours but not in theirs or identical in both
		//
		// Not a genuine conflict just add to the index
		*filesToAdd = append(*filesToAdd, git.IndexObjectInfo{Mode: file.stage2.mode, Object: git.MustIDFromString(file.stage2.sha), Filename: file.stage2.path})
		return nil
	case file.stage1 == nil && file.stage2 != nil && file.stage3 != nil && file.stage2.sha == file.stage3.sha && file.stage2.mode != file.stage3.mode:
		// 3. Added in both with the same sha but the modes are different
		//
		// Conflict! (Not sure that this can actually happen but we should handle)
		return &errMergeConflict{file.stage2.path}
	case file.stage1 == nil && file.stage2 == nil && file.stage3 != nil:
		// 4. Added in theirs but not ours:
		//
		// Not a genuine conflict just add to the index
		*filesToAdd = append(*filesToAdd, git.IndexObjectInfo{Mode: file.stage3.mode, Object: git.MustIDFromString(file.stage3.sha), Filename: file.stage3.path})
		return nil
	case file.stage1 == nil:
		// 5. Created by new in both
		//
		// Conflict!
		return &errMergeConflict{file.stage2.path}
	case file.stage2 != nil && file.stage3 != nil:
		// 5. Modified in both - we should try to merge in the changes but first:
		//
		if file.stage2.mode == "120000" || file.stage3.mode == "120000" {
			// 5a. Conflicting symbolic link change
			return &errMergeConflict{file.stage2.path}
		}
		if file.stage2.mode == "160000" || file.stage3.mode == "160000" {
			// 5b. Conflicting submodule change
			return &errMergeConflict{file.stage2.path}
		}
		if file.stage2.mode != file.stage3.mode {
			// 5c. Conflicting mode change
			return &errMergeConflict{file.stage2.path}
		}

		// Need to get the objects from the object db to attempt to merge
		root, _, err := git.NewCommand(ctx, "unpack-file").AddDynamicArguments(file.stage1.sha).RunStdString(&git.RunOpts{Dir: tmpBasePath})
		if err != nil {
			return fmt.Errorf("unable to get root object: %s at path: %s for merging. Error: %w", file.stage1.sha, file.stage1.path, err)
		}
		root = strings.TrimSpace(root)
		defer func() {
			_ = util.Remove(filepath.Join(tmpBasePath, root))
		}()

		base, _, err := git.NewCommand(ctx, "unpack-file").AddDynamicArguments(file.stage2.sha).RunStdString(&git.RunOpts{Dir: tmpBasePath})
		if err != nil {
			return fmt.Errorf("unable to get base object: %s at path: %s for merging. Error: %w", file.stage2.sha, file.stage2.path, err)
		}
		base = strings.TrimSpace(filepath.Join(tmpBasePath, base))
		defer func() {
			_ = util.Remove(base)
		}()
		head, _, err := git.NewCommand(ctx, "unpack-file").AddDynamicArguments(file.stage3.sha).RunStdString(&git.RunOpts{Dir: tmpBasePath})
		if err != nil {
			return fmt.Errorf("unable to get head object:%s at path: %s for merging. Error: %w", file.stage3.sha, file.stage3.path, err)
		}
		head = strings.TrimSpace(head)
		defer func() {
			_ = util.Remove(filepath.Join(tmpBasePath, head))
		}()

		// now git merge-file annoyingly takes a different order to the merge-tree ...
		_, _, conflictErr := git.NewCommand(ctx, "merge-file").AddDynamicArguments(base, root, head).RunStdString(&git.RunOpts{Dir: tmpBasePath})
		if conflictErr != nil {
			return &errMergeConflict{file.stage2.path}
		}

		// base now contains the merged data
		hash, _, err := git.NewCommand(ctx, "hash-object", "-w", "--path").AddDynamicArguments(file.stage2.path, base).RunStdString(&git.RunOpts{Dir: tmpBasePath})
		if err != nil {
			return err
		}
		hash = strings.TrimSpace(hash)
		*filesToAdd = append(*filesToAdd, git.IndexObjectInfo{Mode: file.stage2.mode, Object: git.MustIDFromString(hash), Filename: file.stage2.path})
		return nil
	default:
		if file.stage1 != nil {
			return &errMergeConflict{file.stage1.path}
		} else if file.stage2 != nil {
			return &errMergeConflict{file.stage2.path}
		} else if file.stage3 != nil {
			return &errMergeConflict{file.stage3.path}
		}
	}
	return nil
}

// attemptThreeWayMerge will attempt to three way merge using git read-tree and then follow the git merge-one-file algorithm to attempt to resolve basic conflicts
func attemptThreeWayMerge(ctx context.Context, dir string, base, ours, theirs string) (bool, []string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// First we use read-tree to do a simple three-way merge
	if _, _, err := git.NewCommand(ctx, "read-tree", "-m").AddDynamicArguments(base, ours, theirs).RunStdString(&git.RunOpts{Dir: dir}); err != nil {
		log.Error("Unable to run read-tree -m! Error: %v", err)
		return false, nil, fmt.Errorf("unable to run read-tree -m! Error: %w", err)
	}

	var filesToRemove []string
	var filesToAdd []git.IndexObjectInfo

	// Then we use git ls-files -u to list the unmerged files and collate the triples in unmergedfiles
	unmerged := make(chan *unmergedFile)
	go unmergedFiles(ctx, dir, unmerged)

	defer func() {
		cancel()
		for range unmerged {
			// empty the unmerged channel
		}
	}()

	numberOfConflicts := 0
	conflict := false
	conflictedFiles := make([]string, 0, 5)

	for file := range unmerged {
		if file == nil {
			break
		}
		if file.err != nil {
			cancel()
			return false, nil, file.err
		}

		// OK now we have the unmerged file triplet attempt to merge it
		if err := attemptMerge(ctx, file, dir, &filesToRemove, &filesToAdd); err != nil {
			if conflictErr, ok := err.(*errMergeConflict); ok {
				log.Trace("Conflict: %s", conflictErr.filename)
				conflict = true
				if numberOfConflicts < 10 {
					conflictedFiles = append(conflictedFiles, conflictErr.filename)
				}
				numberOfConflicts++
				continue
			}
			return false, nil, err
		}
	}

	gitRepo, err := git.OpenRepository(ctx, dir)
	if err != nil {
		return false, nil, err
	}
	defer gitRepo.Close()

	// Add and remove files in one command, as this is slow with many files otherwise
	if err := gitRepo.RemoveFilesFromIndex(filesToRemove...); err != nil {
		return false, nil, err
	}
	if err := gitRepo.AddObjectsToIndex(filesToAdd...); err != nil {
		return false, nil, err
	}

	return conflict, conflictedFiles, nil
}

// mergeTree runs a 3-way merge between `ours` and `theirs` with
// `base` as the merge base.
//
// It uses git-merge-tree(1) to do this merge without requiring a work-tree and
// can run in a bare repository. It returns the object ID of the merge tree, if
// there are any conflicts and conflicted files.
func mergeTree(ctx context.Context, dir string, base, ours, theirs string, env []string) (string, bool, []string, error) {
	cmd := git.NewCommand(ctx, "merge-tree", "--write-tree", "-z", "--name-only", "--no-messages")
	if git.CheckGitVersionAtLeast("2.40") == nil {
		cmd.AddOptionFormat("--merge-base=%s", base)
	}

	stdout := &bytes.Buffer{}
	gitErr := cmd.AddDynamicArguments(ours, theirs).Run(&git.RunOpts{Dir: dir, Stdout: stdout, Env: env})
	if gitErr != nil && !git.IsErrorExitCode(gitErr, 1) {
		log.Error("Unable to run merge-tree: %v", gitErr)
		return "", false, nil, fmt.Errorf("unable to run merge-tree: %w", gitErr)
	}

	// There are two situations that we consider for the output:
	// 1. Clean merge and the output is <OID of toplevel tree>NUL
	// 2. Merge conflict and the output is <OID of toplevel tree>NUL<Conflicted file info>NUL
	treeOID, conflictedFileInfo, _ := strings.Cut(stdout.String(), "\x00")
	if len(conflictedFileInfo) == 0 {
		return treeOID, git.IsErrorExitCode(gitErr, 1), nil, nil
	}

	// Remove last NULL-byte from conflicted file info, then split with NULL byte as separator.
	return treeOID, true, strings.Split(conflictedFileInfo[:len(conflictedFileInfo)-1], "\x00"), nil
}
