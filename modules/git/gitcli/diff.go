// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitcli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"forgejo.org/modules/git"
	"forgejo.org/modules/util"
)

// maxChangedPaths bounds the output of one name-only diff.
const maxChangedPaths = 256 << 20

// ChangedPaths lists the paths that differ between base and head. A zero base lists the paths
// head itself changed (all of them for a root commit).
func (b *Backend) ChangedPaths(ctx context.Context, base, head git.ObjectID, opt git.DiffOpts) ([]git.PathChange, error) {
	cmd := git.NewCommand(ctx, "diff-tree", "--name-only", "--root", "--no-commit-id", "-r", "-z")
	if opt.MergeBase {
		cmd.AddArguments("--merge-base")
	}
	if base.IsZero() {
		cmd.AddDynamicArguments(head.String())
	} else {
		cmd.AddDynamicArguments(base.String(), head.String())
	}
	out := &limitedBuffer{max: maxChangedPaths}
	var stderr bytes.Buffer
	if err := cmd.Run(&git.RunOpts{Dir: b.repo.Path, Stdout: out, Stderr: &stderr}); err != nil {
		return nil, git.ConcatenateError(err, stderr.String())
	}
	// Git always emits a terminal NUL, so the last entry of the split is empty.
	split := strings.Split(out.String(), "\x00")
	if len(split) > 0 {
		split = split[:len(split)-1]
	}
	changes := make([]git.PathChange, len(split))
	for i, path := range split {
		changes[i] = git.PathChange{Path: path}
	}
	return changes, nil
}

// UnifiedDiff writes the text diff between base and head to w.
func (b *Backend) UnifiedDiff(ctx context.Context, base, head git.ObjectID, opt git.DiffOpts, w io.Writer) error {
	switch opt.Mode {
	case git.DiffModeText:
		return b.diffWithMergeBaseFallback(ctx, base, head, opt.MergeBase, w, []string{"diff", "-p"}, nil)
	case git.DiffModeBinary:
		// A direct comparison reads best with the histogram algorithm; a merge-base comparison is meant for "git apply".
		return b.diffWithMergeBaseFallback(ctx, base, head, opt.MergeBase, w, []string{"diff", "-p", "--binary"}, []string{"--histogram"})
	case git.DiffModePatch:
		return b.diffWithMergeBaseFallback(ctx, base, head, true, w, []string{"format-patch", "--binary", "--stdout"}, nil)
	}
	return util.NewInvalidArgumentErrorf("unknown diff mode %d", opt.Mode)
}

// diffWithMergeBaseFallback runs "git <args> base...head" when mergeBase is set, and the direct comparison
// "git <args> <directExtra> base head" otherwise or when the two commits share no history.
func (b *Backend) diffWithMergeBaseFallback(ctx context.Context, base, head git.ObjectID, mergeBase bool, w io.Writer, args, directExtra []string) error {
	run := func(extra []string, revs ...string) (stderr bytes.Buffer, err error) {
		cmd := git.NewCommand(ctx).AddArguments(git.ToTrustedCmdArgs(args)...).AddArguments(git.ToTrustedCmdArgs(extra)...)
		cmd.AddDynamicArguments(revs...)
		err = cmd.Run(&git.RunOpts{Dir: b.repo.Path, Stdout: w, Stderr: &stderr})
		return stderr, err
	}
	if mergeBase {
		stderr, err := run(nil, base.String()+"..."+head.String())
		if err == nil || !bytes.Contains(stderr.Bytes(), []byte("no merge base")) {
			return err
		}
	}
	_, err := run(directExtra, base.String(), head.String())
	return err
}

var shortStatFormat = regexp.MustCompile(
	`\s*(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)

// DiffStat counts the files, added lines and deleted lines between base and head, with rename detection.
func (b *Backend) DiffStat(ctx context.Context, base, head git.ObjectID) (git.DiffStat, error) {
	stdout, _, err := git.NewCommand(ctx, "diff-tree", "--shortstat", "--find-renames").
		AddDynamicArguments(base.String(), head.String()).
		RunStdString(&git.RunOpts{Dir: b.repo.Path})
	if err != nil {
		return git.DiffStat{}, err
	}
	return parseDiffStat(stdout)
}

func parseDiffStat(stdout string) (stat git.DiffStat, err error) {
	if len(stdout) == 0 || stdout == "\n" {
		return stat, nil
	}
	groups := shortStatFormat.FindStringSubmatch(stdout)
	if len(groups) != 4 {
		return stat, fmt.Errorf("unable to parse shortstat: %s groups: %s", stdout, groups)
	}
	if stat.NumFiles, err = strconv.Atoi(groups[1]); err != nil {
		return stat, fmt.Errorf("unable to parse shortstat: %s. Error parsing NumFiles %w", stdout, err)
	}
	if groups[2] != "" {
		if stat.Additions, err = strconv.Atoi(groups[2]); err != nil {
			return stat, fmt.Errorf("unable to parse shortstat: %s. Error parsing NumAdditions %w", stdout, err)
		}
	}
	if groups[3] != "" {
		if stat.Deletions, err = strconv.Atoi(groups[3]); err != nil {
			return stat, fmt.Errorf("unable to parse shortstat: %s. Error parsing NumDeletions %w", stdout, err)
		}
	}
	return stat, nil
}
