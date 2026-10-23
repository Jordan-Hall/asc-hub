// Copyright 2019 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package files

import (
	"context"
	"fmt"
	"io"
	"strings"

	repo_model "forgejo.org/models/repo"
	"forgejo.org/modules/git"
	"forgejo.org/modules/repostore"
	"forgejo.org/modules/setting"
	"forgejo.org/services/gitdiff"
)

// GetDiffPreview produces and returns diff result of a file which is not yet committed.
func GetDiffPreview(ctx context.Context, repo *repo_model.Repository, branch, treePath, content string) (*gitdiff.Diff, error) {
	if branch == "" {
		branch = repo.DefaultBranch
	}
	writer, err := repostore.Default()
	if err != nil {
		return nil, err
	}
	_, closeRepo, commit, err := openBranch(ctx, repo, branch)
	if closeRepo != nil {
		defer closeRepo()
	}
	if err != nil {
		return nil, err
	}

	// The writer streams the diff of the change into the pipe while the parser reads it. The parser may
	// stop early when the diff is over its limits, and then the writer stops quietly.
	reader, writerEnd := io.Pipe()
	type parsed struct {
		diff *gitdiff.Diff
		err  error
	}
	done := make(chan parsed, 1)
	go func() {
		diff, err := gitdiff.ParsePatch(ctx, setting.Git.MaxGitDiffLines, setting.Git.MaxGitDiffLineCharacters, setting.Git.MaxGitDiffFiles, reader, "")
		_ = reader.CloseWithError(io.ErrClosedPipe)
		done <- parsed{diff, err}
	}()

	result, err := writer.CreateCommit(ctx, repostore.CommitRequest{
		Repo:   repo.StoreRef(),
		Parent: commit.ID,
		Ops: []repostore.FileOp{{
			Kind:    repostore.OpPut,
			Path:    treePath,
			Mode:    git.EntryModeBlob,
			Content: strings.NewReader(content),
		}},
		Preview: writerEnd,
	})
	_ = writerEnd.Close()
	parsedDiff := <-done
	if err != nil {
		return nil, convertStoreError(err)
	}
	if parsedDiff.err != nil {
		return nil, fmt.Errorf("ParsePatch: %w", parsedDiff.err)
	}

	diff := parsedDiff.diff
	diff.NumFiles, diff.TotalAddition, diff.TotalDeletion = result.Stat.NumFiles, result.Stat.Additions, result.Stat.Deletions
	return diff, nil
}
