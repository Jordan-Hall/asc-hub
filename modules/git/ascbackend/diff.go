// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	ascv1 "forgejo.org/modules/ascproto/ascv1"
	"forgejo.org/modules/git"
	"forgejo.org/modules/util"
)

// diffStream opens a Diff stream for base and head, which are revision strings.
func (b *Backend) diffStream(ctx context.Context, req *ascv1.DiffRequest) (ascv1.Repo_DiffClient, error) {
	req.Repo = b.gitRef()
	st, err := b.api.Diff(ctx, req)
	if err != nil {
		return nil, callErr("Diff", req.GetHead(), err)
	}
	return st, nil
}

// ChangedPaths lists the paths that differ between base and head. A zero base lists the paths head
// itself changed against its first parent (all of them for a root commit, none for a merge).
func (b *Backend) ChangedPaths(ctx context.Context, base, head git.ObjectID, opt git.DiffOpts) ([]git.PathChange, error) {
	if err := errors.Join(requireSha1(base), requireSha1(head)); err != nil {
		return nil, err
	}
	if base.IsZero() {
		return b.ownChanges(ctx, head)
	}
	ctx, cancel, err := b.stream(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	st, err := b.diffStream(ctx, &ascv1.DiffRequest{
		Base: base.String(), Head: head.String(), MergeBase: opt.MergeBase,
		Format: ascv1.DiffFormat_DIFF_FORMAT_NAMES, MaxFiles: maxChangedPaths,
	})
	if err != nil {
		return nil, err
	}
	var changes []git.PathChange
	for {
		msg, err := st.Recv()
		if errors.Is(err, io.EOF) {
			return changes, nil
		}
		if err != nil {
			return nil, callErr("Diff", head.String(), err)
		}
		if msg.GetTruncated() {
			return nil, fmt.Errorf("%w: more than %d changed paths", ErrTooLarge, maxChangedPaths)
		}
		for _, c := range msg.GetChanges() {
			if len(changes) >= maxChangedPaths {
				return nil, fmt.Errorf("%w: more than %d changed paths", ErrTooLarge, maxChangedPaths)
			}
			p := c.GetPath()
			if len(p) == 0 || len(p) > maxPathBytes || bytes.IndexByte(p, 0) >= 0 {
				return nil, badAnswer("changed path is invalid")
			}
			changes = append(changes, git.PathChange{Path: string(p)})
		}
	}
}

// ownChanges lists what a commit changed against its first parent, like "git diff-tree --root <commit>".
func (b *Backend) ownChanges(ctx context.Context, head git.ObjectID) ([]git.PathChange, error) {
	commit, err := b.ReadCommit(ctx, head)
	if err != nil {
		return nil, err
	}
	switch len(commit.Parents) {
	case 0:
		var changes []git.PathChange
		for e, err := range b.ReadTreeRecursive(ctx, head, git.TreeWalkOpts{}) {
			if err != nil {
				return nil, err
			}
			changes = append(changes, git.PathChange{Path: e.Name})
		}
		return changes, nil
	case 1:
		return b.ChangedPaths(ctx, commit.Parents[0], head, git.DiffOpts{})
	}
	return nil, nil // diff-tree shows nothing for a merge commit
}

// UnifiedDiff writes the text diff between base and head to w. It supports DiffModeText and
// DiffModeBinary; the text is the server's, in git's format, with renames detected.
func (b *Backend) UnifiedDiff(ctx context.Context, base, head git.ObjectID, opt git.DiffOpts, w io.Writer) error {
	if err := errors.Join(requireSha1(base), requireSha1(head)); err != nil {
		return err
	}
	var binary bool
	switch opt.Mode {
	case git.DiffModeText:
	case git.DiffModeBinary:
		binary = true
	case git.DiffModePatch:
		return fmt.Errorf("%w: format-patch output", ErrUnsupported)
	default:
		return util.NewInvalidArgumentErrorf("unknown diff mode %d", opt.Mode)
	}
	ctx, cancel, err := b.bulk(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	st, err := b.diffStream(ctx, &ascv1.DiffRequest{
		Base: base.String(), Head: head.String(), MergeBase: opt.MergeBase,
		Format: ascv1.DiffFormat_DIFF_FORMAT_PATCH, Renames: true, Binary: binary,
		MaxFiles: maxChangedPaths,
	})
	if err != nil {
		return err
	}
	var written int64
	for {
		msg, err := st.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return callErr("Diff", head.String(), err)
		}
		if written += int64(len(msg.GetText())); written > maxDiffBytes {
			return fmt.Errorf("%w: diff of more than %d bytes", ErrTooLarge, int64(maxDiffBytes))
		}
		if _, err := w.Write(msg.GetText()); err != nil {
			return err
		}
		if msg.GetTruncated() {
			return fmt.Errorf("%w: diff has more than %d files", ErrTooLarge, maxChangedPaths)
		}
	}
}

// DiffStat counts the files, added lines and deleted lines between base and head, with rename detection.
func (b *Backend) DiffStat(ctx context.Context, base, head git.ObjectID) (git.DiffStat, error) {
	if err := errors.Join(requireSha1(base), requireSha1(head)); err != nil {
		return git.DiffStat{}, err
	}
	ctx, cancel, err := b.bulk(ctx)
	if err != nil {
		return git.DiffStat{}, err
	}
	defer cancel()
	st, err := b.diffStream(ctx, &ascv1.DiffRequest{
		Base: base.String(), Head: head.String(),
		Format: ascv1.DiffFormat_DIFF_FORMAT_NUMSTAT, Renames: true, MaxFiles: maxChangedPaths,
	})
	if err != nil {
		return git.DiffStat{}, err
	}
	var (
		text      bytes.Buffer
		truncated bool
	)
	for {
		msg, err := st.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return git.DiffStat{}, callErr("Diff", head.String(), err)
		}
		if text.Len()+len(msg.GetText()) > maxNumstatBytes {
			return git.DiffStat{}, fmt.Errorf("%w: numstat output", ErrTooLarge)
		}
		text.Write(msg.GetText())
		truncated = truncated || msg.GetTruncated()
	}
	if truncated {
		return git.DiffStat{}, fmt.Errorf("%w: diff has more than %d files", ErrTooLarge, maxChangedPaths)
	}
	return parseNumstat(text.Bytes())
}

// parseNumstat adds up "git diff --numstat" lines: added TAB removed TAB path. A binary file has "-" counts.
func parseNumstat(text []byte) (git.DiffStat, error) {
	var stat git.DiffStat
	for line := range bytes.SplitSeq(text, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		fields := bytes.SplitN(line, []byte{'\t'}, 3)
		if len(fields) != 3 {
			return git.DiffStat{}, badAnswer("numstat line without three fields")
		}
		stat.NumFiles++
		for i, dst := range []*int{&stat.Additions, &stat.Deletions} {
			if string(fields[i]) == "-" {
				continue
			}
			n, err := strconv.Atoi(string(fields[i]))
			if err != nil || n < 0 {
				return git.DiffStat{}, badAnswer("numstat count %q", fields[i])
			}
			*dst += n
		}
	}
	return stat, nil
}
