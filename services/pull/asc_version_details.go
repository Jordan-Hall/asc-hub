// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"bytes"
	"context"
	"strconv"
	"strings"

	"forgejo.org/modules/git"
)

// What a row of the versions list says about its version besides the number and the author: the first line of the
// commit message, the short hash, how many lines it adds and removes, and whether it is a restack, a version whose
// patch is the one of the version before it (only the base moved). All of it is read from the commit the version had
// in the git view, so it needs nothing of asc but the list of versions.

// ascVersionDetailMax bounds how many versions get details (the newest ones).
const ascVersionDetailMax = 30

// AscVersionDetail is what a row of the versions list shows about one version.
type AscVersionDetail struct {
	Subject string
	Short   string // the first 10 digits of the git commit
	Add     int
	Del     int
	// Restack says the patch of this version is the patch of the version before it: asc moved it onto a new base and
	// nothing in the diff changed.
	Restack bool
}

// AscVersionDetails reads the details of the newest versions. A version whose commit cannot be read has no entry: its
// row stays as it was.
func AscVersionDetails(ctx context.Context, repo *git.Repository, versions *AscVersionList) map[uint32]AscVersionDetail {
	out := map[uint32]AscVersionDetail{}
	if repo == nil || versions == nil {
		return out
	}
	first := max(0, len(versions.Versions)-ascVersionDetailMax)
	prevPatch := ""
	for i := first; i < len(versions.Versions); i++ {
		v := versions.Versions[i]
		commit, err := repo.GetCommit(v.GitCommit)
		if err != nil {
			prevPatch = ""
			continue
		}
		d := AscVersionDetail{Subject: commit.Summary(), Short: v.GitCommit[:min(10, len(v.GitCommit))]}
		patch := ""
		if parent, err := commit.ParentID(0); err == nil {
			d.Add, d.Del = ascNumstat(ctx, repo, parent.String(), v.GitCommit)
			patch = ascPatchID(ctx, repo, parent.String(), v.GitCommit)
		}
		d.Restack = patch != "" && patch == prevPatch
		prevPatch = patch
		out[v.N] = d
	}
	return out
}

// ascNumstat returns the lines added and removed between two commits.
func ascNumstat(ctx context.Context, repo *git.Repository, from, to string) (add, del int) {
	out, _, err := git.NewCommand(ctx, "diff", "--numstat").AddDynamicArguments(from, to).RunStdString(&git.RunOpts{Dir: repo.Path})
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		a, errA := strconv.Atoi(f[0])
		d, errD := strconv.Atoi(f[1])
		if errA != nil || errD != nil {
			continue // a binary file has no counts
		}
		add += a
		del += d
	}
	return add, del
}

// ascPatchID returns the stable patch id of the diff between two commits ("" when there is none: an empty diff or a
// failure), so that two versions with the same patch on different bases have the same id.
func ascPatchID(ctx context.Context, repo *git.Repository, from, to string) string {
	diff, _, err := git.NewCommand(ctx, "diff").AddDynamicArguments(from, to).RunStdBytes(&git.RunOpts{Dir: repo.Path})
	if err != nil || len(diff) == 0 {
		return ""
	}
	out, _, err := git.NewCommand(ctx, "patch-id", "--stable").RunStdString(&git.RunOpts{Dir: repo.Path, Stdin: bytes.NewReader(diff)})
	if err != nil {
		return ""
	}
	id, _, _ := strings.Cut(strings.TrimSpace(out), " ")
	return id
}
