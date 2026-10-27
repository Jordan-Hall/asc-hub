// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
	"context"
	"strings"

	repo_model "forgejo.org/models/repo"
	ascwriter "forgejo.org/modules/repostore/asc"
)

// The hub's writer sends a web edit, a merge or a branch operation to asc as the hub's service credential, and asc
// runs the hub's pre-receive and post-receive hooks for it with no person. The writer tells the hub whose change it
// is before it sends the write (ascwriter.NotePusher), and the hooks take the person back (PeekWriter, ClaimPusher).
func init() {
	ascwriter.NotePusher = notePusher
}

// notePusher is ascwriter.NotePusher: it records the write under the repository the asc name "owner/name" stands
// for. A name that is no repository of the hub records nothing, and the hooks then refuse the call as before.
func notePusher(ctx context.Context, name string, pusherID, pullRequestID int64, refs []ascwriter.PushedRef) func() {
	owner, repoName, ok := strings.Cut(name, "/")
	if !ok {
		return nil
	}
	repo, err := repo_model.GetRepositoryByOwnerAndName(ctx, owner, repoName)
	if err != nil {
		return nil
	}
	pushed := make([]PushRef, len(refs))
	for i, r := range refs {
		pushed[i] = PushRef{Name: r.Name, New: r.New}
	}
	return RememberWriter(repo, pusherID, pullRequestID, pushed).Forget
}
