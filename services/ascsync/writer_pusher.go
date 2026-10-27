// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
	"context"
	"strings"
	"time"

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
func notePusher(ctx context.Context, name string, pusherID, pullRequestID int64, refs []ascwriter.PushedRef) *ascwriter.PusherNote {
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
	tok := RememberWriter(repo, pusherID, pullRequestID, pushed)
	return &ascwriter.PusherNote{
		Forget: tok.Forget,
		Wait:   func(ctx context.Context) { tok.Wait(ctx, SettleWait) },
	}
}

// SettleWait is the longest a writer waits for the hub to process the post-receive calls of its write.
const SettleWait = 5 * time.Second
