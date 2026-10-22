// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli

import (
	"bytes"
	"context"
	"iter"
	"strconv"
	"strings"
	"time"

	"forgejo.org/modules/git"
	"forgejo.org/modules/util"
)

const (
	// maxQueryItems bounds the start points, exclusions and paths of one query.
	maxQueryItems = 4096
	// maxLogCommits bounds the commit ids one Log call collects before reading them.
	maxLogCommits = 1 << 20
	// maxLastCommitNames bounds the names one LastCommits call walks.
	maxLastCommitNames = 10000
)

// addLogQuery adds the revision, filter and path arguments of q to cmd.
// follow and the paging options are only valid for "git log", so the caller says which command this is.
func addLogQuery(cmd *git.Command, q git.LogQuery, isLog bool) error {
	if len(q.From) > maxQueryItems || len(q.Not) > maxQueryItems || len(q.Paths) > maxQueryItems {
		return util.NewInvalidArgumentErrorf("log query has more than %d start points, exclusions or paths", maxQueryItems)
	}
	if q.Skip < 0 || q.Limit < 0 {
		return util.NewInvalidArgumentErrorf("log query has a negative skip or limit")
	}
	if q.Follow && (!isLog || len(q.Paths) != 1 || strings.HasPrefix(q.Paths[0], ":")) {
		return util.NewInvalidArgumentErrorf("follow needs exactly one plain path")
	}

	if q.Skip > 0 {
		cmd.AddOptionFormat("--skip=%d", q.Skip)
	}
	if q.Limit > 0 {
		cmd.AddOptionFormat("--max-count=%d", q.Limit)
	}
	if q.Author != "" || q.Grep != "" {
		cmd.AddArguments("--fixed-strings")
	}
	if q.Author != "" {
		cmd.AddOptionFormat("--author=%s", q.Author)
	}
	if q.Grep != "" {
		cmd.AddOptionFormat("--grep=%s", q.Grep)
	}
	if !q.Since.IsZero() {
		cmd.AddOptionFormat("--since=%s", q.Since.UTC().Format(time.RFC3339))
	}
	if !q.Until.IsZero() {
		cmd.AddOptionFormat("--until=%s", q.Until.UTC().Format(time.RFC3339))
	}
	if q.NoMerges {
		cmd.AddArguments("--no-merges")
	}
	if q.Follow {
		cmd.AddArguments("--follow")
	}

	switch {
	case q.All:
		cmd.AddArguments("--all")
	case len(q.From) == 0:
		return util.NewInvalidArgumentErrorf("log query has no start point")
	default:
		for _, id := range q.From {
			if id == nil {
				return util.NewInvalidArgumentErrorf("log query has a nil start point")
			}
			cmd.AddDynamicArguments(id.String())
		}
	}
	if len(q.Not) > 0 {
		cmd.AddArguments("--not")
		for _, id := range q.Not {
			if id == nil {
				return util.NewInvalidArgumentErrorf("log query has a nil exclusion")
			}
			cmd.AddDynamicArguments(id.String())
		}
	}
	if len(q.Paths) > 0 {
		paths := make([]string, len(q.Paths))
		for i, p := range q.Paths {
			paths[i] = p
			if !q.Follow {
				paths[i] = ":(literal)" + p
			}
		}
		cmd.AddDashesAndList(paths...)
	}
	return nil
}

// Count counts the commits q selects.
func (b *Backend) Count(ctx context.Context, q git.LogQuery) (int64, error) {
	cmd := git.NewCommand(ctx, "rev-list", "--count")
	if err := addLogQuery(cmd, q, false); err != nil {
		return 0, err
	}
	stdout, _, err := cmd.RunStdString(&git.RunOpts{Dir: b.repo.Path})
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(stdout), 10, 64)
}

// Log yields the commits q selects, newest first.
func (b *Backend) Log(ctx context.Context, q git.LogQuery) iter.Seq2[*git.CommitData, error] {
	return func(yield func(*git.CommitData, error) bool) {
		cmd := git.NewCommand(ctx, "log", "--format=%H")
		if err := addLogQuery(cmd, q, true); err != nil {
			yield(nil, err)
			return
		}
		out := &limitedBuffer{max: maxLogCommits * (b.ObjectFormat().FullLength() + 1)}
		var stderr bytes.Buffer
		if err := cmd.Run(&git.RunOpts{Dir: b.repo.Path, Stdout: out, Stderr: &stderr}); err != nil {
			yield(nil, git.ConcatenateError(err, stderr.String()))
			return
		}
		for line := range strings.SplitSeq(out.String(), "\n") {
			if line == "" {
				continue
			}
			id, err := git.NewIDFromString(line)
			if err != nil {
				yield(nil, err)
				return
			}
			commit, err := b.ReadCommit(ctx, id)
			if !yield(commit, err) || err != nil {
				return
			}
		}
	}
}

// MergeBase returns the best common ancestor of a and b.
func (b *Backend) MergeBase(ctx context.Context, a, c git.ObjectID) (git.ObjectID, error) {
	stdout, _, err := git.NewCommand(ctx, "merge-base").AddDynamicArguments(a.String(), c.String()).RunStdString(&git.RunOpts{Dir: b.repo.Path})
	if err != nil {
		if git.IsErrorExitCode(err, 1) && strings.TrimSpace(stdout) == "" {
			return nil, git.ErrNoMergebaseFound
		}
		return nil, err
	}
	return git.NewIDFromString(strings.TrimSpace(stdout))
}

// IsAncestor reports whether ancestor is reachable from desc. A commit is its own ancestor.
func (b *Backend) IsAncestor(ctx context.Context, ancestor, desc git.ObjectID) (bool, error) {
	_, _, err := git.NewCommand(ctx, "merge-base", "--is-ancestor").AddDynamicArguments(ancestor.String(), desc.String()).RunStdString(&git.RunOpts{Dir: b.repo.Path})
	if err == nil {
		return true, nil
	}
	if git.IsErrorExitCode(err, 1) {
		return false, nil
	}
	return false, err
}

// LastCommits finds the newest commit that touched each name in dir, at or below the commit at.
func (b *Backend) LastCommits(ctx context.Context, at git.ObjectID, dir string, names []string) (map[string]git.ObjectID, error) {
	if len(names) > maxLastCommitNames {
		return nil, util.NewInvalidArgumentErrorf("last commits asked for %d names, the limit is %d", len(names), maxLastCommitNames)
	}
	c, err := b.repo.GetCommit(at.String())
	if err != nil {
		return nil, err
	}
	found, err := git.GetLastCommitForPaths(ctx, c, strings.Trim(dir, "/"), names)
	if err != nil {
		return nil, err
	}
	res := make(map[string]git.ObjectID, len(names))
	for _, name := range names {
		if last, ok := found[name]; ok && last != nil {
			res[name] = last.ID
		}
	}
	return res, nil
}

// Blame yields the blame of path at commit at, one run of lines per part.
func (b *Backend) Blame(ctx context.Context, at git.ObjectID, path string, opt git.BlameOpts) iter.Seq2[git.BlamePart, error] {
	return func(yield func(git.BlamePart, error) bool) {
		commit, err := b.repo.GetCommit(at.String())
		if err != nil {
			yield(git.BlamePart{}, err)
			return
		}
		reader, err := git.CreateBlameReader(ctx, b.ObjectFormat(), b.repo, commit, path, opt.BypassIgnoreRevs)
		if err != nil {
			yield(git.BlamePart{}, err)
			return
		}
		defer reader.Close()
		for {
			part, err := reader.NextPart()
			if err != nil {
				yield(git.BlamePart{}, err)
				return
			}
			if part == nil || !yield(*part, nil) {
				return
			}
		}
	}
}
