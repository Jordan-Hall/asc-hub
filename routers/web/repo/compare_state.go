// Copyright 2026 The ASC Hub Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"strconv"
	"strings"

	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
	"forgejo.org/services/context"
)

// The bar of a new change shows how the head meets the base before anyone opens the pull request: how many changes
// the base has that the head has not (behind) and whether the two merge without a conflict. Both come from git of
// the base repository, only for a compare inside one repository (the objects of a fork are in another one), and a
// failure means "unknown", never "clean".

// Merge states of a compare.
const (
	compareMergeClean     = "clean"
	compareMergeConflicts = "conflicts"
	compareMergeUnknown   = "unknown"
)

// parseBehind reads the output of `git rev-list --count`.
func parseBehind(out string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// prepareCompareState puts CompareBehind and CompareMergeState into ctx.Data for a compare of head against base in
// the repository at path.
func prepareCompareState(ctx *context.Context, path, base, head string) {
	state := compareMergeUnknown
	if out, _, err := git.NewCommand(ctx, "rev-list", "--count").AddDynamicArguments(head + ".." + base).RunStdString(&git.RunOpts{Dir: path}); err == nil {
		if n, ok := parseBehind(out); ok {
			ctx.Data["CompareBehind"] = n
		}
	}
	// merge-tree --write-tree (git 2.38) exits 0 for a clean merge and 1 for conflicts; any other exit is "unknown"
	_, _, err := git.NewCommand(ctx, "merge-tree", "--write-tree", "--no-messages").AddDynamicArguments(base, head).RunStdString(&git.RunOpts{Dir: path})
	switch {
	case err == nil:
		state = compareMergeClean
	case git.IsErrorExitCode(err, 1):
		state = compareMergeConflicts
	default:
		log.Trace("compare merge state: %v", err)
	}
	ctx.Data["CompareMergeState"] = state
}
