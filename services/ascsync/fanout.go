// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
	"context"
	"fmt"
	"sync"
	"time"

	repo_model "forgejo.org/models/repo"
)

// The git-origin push of a storage_backend=asc repository is handed to asc in pre-receive, and asc's outbox then
// calls post-receive with PushTrigger "asc" for the same ref changes. Two things differ between that call and a
// plain post-receive, and this file closes both:
//
//  1. The call names the pusher by asc's principal, which for an import made by the hub is the hub itself, so the
//     hook options arrive without a user (uid 0) and the push cannot be attributed. The hub remembers who pushed
//     which ref to which commit for a short time (RememberPusher) and puts the user back (PusherOf).
//  2. The outbox can call before git has moved the ref (git updates it after pre-receive) or before the bridge has
//     promoted it, so reading the branch then gives the old commit and the branch table keeps it. WaitForRefs holds
//     the call until the repository holds the new commit, for a bounded time.

const (
	// pusherTTL is how long a remembered pusher is kept. The outbox calls within seconds; a retry after a hub
	// restart finds nothing and is attributed to nobody, as before.
	pusherTTL = 10 * time.Minute
	// pusherMax bounds the memory.
	pusherMax = 4096
	// FanOutWait is the longest post-receive waits for the repository to hold the new refs. It is shorter than
	// BridgeWait because the push itself, not a retry, is usually what it waits for.
	FanOutWait = 15 * time.Second
)

type pusherKey struct {
	repoID int64
	ref    string
	commit string
}

type pusherEntry struct {
	userID  int64
	expires time.Time
}

var (
	pusherMu    sync.Mutex
	pushers     = map[pusherKey]pusherEntry{}
	pusherClock = time.Now
	// fanOutWait is FanOutWait; tests shorten it.
	fanOutWait = FanOutWait
)

// RememberPusher records that userID pushed each of refs (to its new commit) to repo. A deletion is kept under the
// zero id. A zero or negative user is not recorded: there is nobody to attribute.
func RememberPusher(repo *repo_model.Repository, userID int64, refs []PushRef) {
	if repo == nil || userID <= 0 || len(refs) == 0 {
		return
	}
	now := pusherClock()
	pusherMu.Lock()
	defer pusherMu.Unlock()
	if len(pushers)+len(refs) > pusherMax {
		for k, e := range pushers {
			if !e.expires.After(now) {
				delete(pushers, k)
			}
		}
		// Still full: drop everything. Losing attribution is better than growing without bound.
		if len(pushers)+len(refs) > pusherMax {
			clear(pushers)
		}
	}
	for _, r := range refs {
		pushers[pusherKey{repo.ID, r.Name, r.New}] = pusherEntry{userID: userID, expires: now.Add(pusherTTL)}
	}
}

// PusherOf returns the user remembered for a push of ref to commit in repo.
func PusherOf(repo *repo_model.Repository, ref, commit string) (int64, bool) {
	if repo == nil {
		return 0, false
	}
	pusherMu.Lock()
	defer pusherMu.Unlock()
	e, ok := pushers[pusherKey{repo.ID, ref, commit}]
	if !ok {
		return 0, false
	}
	if !e.expires.After(pusherClock()) {
		delete(pushers, pusherKey{repo.ID, ref, commit})
		return 0, false
	}
	return e.userID, true
}

// WaitForRefs blocks until every ref of refs in the repository's git directory equals its new commit (a deletion
// is waited for until the ref is gone), or FanOutWait has passed, or ctx ends. It returns nil when the repository
// caught up and ErrBridgeBehind when it did not; the caller may carry on either way.
func WaitForRefs(ctx context.Context, repo *repo_model.Repository, refs []PushRef) error {
	if repo == nil {
		return fmt.Errorf("%w: no repository", ErrBadRequest)
	}
	dir := repo.RepoPath()
	deadline := time.NewTimer(fanOutWait)
	defer deadline.Stop()
	delay := bridgePollMin
	for {
		behind := false
		for _, r := range refs {
			want := r.New
			if isZeroID(want) {
				want = ""
			}
			got, err := bridgeRef(ctx, dir, r.Name)
			if err != nil || got != want {
				behind = true
				break
			}
		}
		if !behind {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("ascsync: waiting for the repository: %w", ctx.Err())
		case <-deadline.C:
			return ErrBridgeBehind
		case <-time.After(delay):
		}
		delay = min(delay*2, bridgePollMax)
	}
}

func isZeroID(id string) bool {
	if id == "" {
		return true
	}
	for _, c := range id {
		if c != '0' {
			return false
		}
	}
	return true
}

// SetFanOutWaitForTest shortens the wait of WaitForRefs for a test and returns the function that restores it.
func SetFanOutWaitForTest(d time.Duration) func() {
	old := fanOutWait
	fanOutWait = d
	return func() { fanOutWait = old }
}
