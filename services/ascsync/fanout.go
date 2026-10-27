// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
	"context"
	"errors"
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
//     which ref to which commit for a short time (RememberPusher) and puts the user back (ClaimPusher).
//  2. The outbox can call before git has moved the ref (git updates it after pre-receive) or before the bridge has
//     promoted it, so reading the branch then gives the old commit and the branch table keeps it. WaitForRefs holds
//     the call until the repository holds the new commit, for a bounded time.

const (
	// pusherTTL is how long a remembered pusher is kept. The outbox calls within seconds; a retry after a hub
	// restart finds nothing and is attributed to nobody, as before.
	pusherTTL = 10 * time.Minute
	// MaxPushesPerRepo bounds the pushes remembered for one repository. A repository that pushes more drops its
	// own oldest first; it never costs another repository an entry.
	MaxPushesPerRepo = 64
	// MaxRefsPerPush bounds the refs of one remembered push. A push with more refs is not remembered (and so not
	// attributed): the memory of a push is all or nothing, see ClaimPusher.
	MaxRefsPerPush = 256
	// maxPusherRepos bounds the repositories that have pushes in memory. Past it the repository that was quiet
	// longest loses its pushes.
	maxPusherRepos = 2048
	// FanOutWait is the longest post-receive waits for the repository to hold the new refs. It is shorter than
	// BridgeWait because the push itself, not a retry, is usually what it waits for.
	FanOutWait = 15 * time.Second
)

// ErrTooManyRefs is returned by RememberPusher for a push with more than MaxRefsPerPush refs.
var ErrTooManyRefs = errors.New("ascsync: the push has too many refs to be remembered")

// pushKey names one ref change. The old commit is part of it, so a deletion (new commit zero) is as particular as a
// creation: two pushes that delete the same ref do not share a key unless they delete it from the same commit, and
// only one of those can succeed.
type pushKey struct {
	ref string
	old string
	new string
}

// push is one remembered push: who made it, until when it is remembered, and which ref changes of it have not
// been claimed yet.
type push struct {
	user    int64
	expires time.Time
	left    map[pushKey]struct{}
	// byWriter marks a write that the hub's own writer makes (RememberWriter). The writer expects any old commit,
	// so such a push is known by the ref and the new commit only: its keys have an empty old commit and a call is
	// matched with its old commit left out.
	byWriter bool
	// pr is the pull request this write merges, 0 when it merges none (a writer push only).
	pr int64
}

// keyOf is the key a ref change of a call has in this push.
func (p *push) keyOf(k pushKey) pushKey {
	if p.byWriter {
		k.old = ""
		if isZeroID(k.new) {
			k.new = ""
		}
	}
	return k
}

type repoPushes struct {
	list   []*push // oldest first
	active time.Time
}

var (
	pusherMu    sync.Mutex
	pushers     = map[int64]*repoPushes{}
	pusherClock = time.Now
	// fanOutWait is FanOutWait; tests shorten it.
	fanOutWait = FanOutWait
)

// PushToken names one remembered push, for forgetting it. The zero value names nothing.
type PushToken struct {
	repoID int64
	p      *push
}

func normalised(refs []PushRef) (map[pushKey]struct{}, bool) {
	keys := make(map[pushKey]struct{}, len(refs))
	for _, r := range refs {
		if r.Name == "" {
			return nil, false
		}
		keys[pushKey{r.Name, r.Old, r.New}] = struct{}{}
	}
	return keys, true
}

// RememberPusher records that userID is making the push of refs to repo, before the push is handed to asc. It
// returns the token that forgets it; the caller forgets the push on every path that does not end with the push
// accepted (a refusal or an error of asc), so a push that did not happen can never be claimed.
//
// A zero or negative user, an empty push and a push of more than MaxRefsPerPush refs are not remembered (the token
// is the zero value). Memory is bounded per repository (MaxPushesPerRepo, oldest first) and by the number of
// repositories; one repository cannot push another out.
func RememberPusher(repo *repo_model.Repository, userID int64, refs []PushRef) (PushToken, error) {
	if repo == nil || userID <= 0 || len(refs) == 0 {
		return PushToken{}, nil
	}
	if len(refs) > MaxRefsPerPush {
		return PushToken{}, ErrTooManyRefs
	}
	keys, ok := normalised(refs)
	if !ok {
		return PushToken{}, nil
	}
	now := pusherClock()
	pusherMu.Lock()
	defer pusherMu.Unlock()
	rp := pushers[repo.ID]
	if rp == nil {
		if len(pushers) >= maxPusherRepos {
			evictQuietRepo(now)
		}
		rp = &repoPushes{}
		pushers[repo.ID] = rp
	}
	rp.dropExpired(now)
	if len(rp.list) >= MaxPushesPerRepo {
		rp.list = rp.list[len(rp.list)-MaxPushesPerRepo+1:]
	}
	p := &push{user: userID, expires: now.Add(pusherTTL), left: keys}
	rp.list = append(rp.list, p)
	rp.active = now
	return PushToken{repo.ID, p}, nil
}

func (rp *repoPushes) dropExpired(now time.Time) {
	kept := rp.list[:0]
	for _, p := range rp.list {
		if p.expires.After(now) {
			kept = append(kept, p)
		}
	}
	clear(rp.list[len(kept):])
	rp.list = kept
}

// evictQuietRepo makes room for a repository: expired pushes go first, and if every slot is still taken the
// repository that pushed least recently loses its memory. The caller holds pusherMu.
func evictQuietRepo(now time.Time) {
	var quiet int64
	var quietAt time.Time
	for id, rp := range pushers {
		rp.dropExpired(now)
		if len(rp.list) == 0 {
			delete(pushers, id)
			continue
		}
		if quietAt.IsZero() || rp.active.Before(quietAt) {
			quiet, quietAt = id, rp.active
		}
	}
	if len(pushers) >= maxPusherRepos {
		delete(pushers, quiet)
	}
}

// Forget removes the remembered push. It is safe to call twice and on the zero token.
func (t PushToken) Forget() {
	if t.p == nil {
		return
	}
	pusherMu.Lock()
	defer pusherMu.Unlock()
	rp := pushers[t.repoID]
	if rp == nil {
		return
	}
	rp.remove(t.p)
	if len(rp.list) == 0 {
		delete(pushers, t.repoID)
	}
}

func (rp *repoPushes) remove(p *push) {
	for i, q := range rp.list {
		if q == p {
			rp.list = append(rp.list[:i], rp.list[i+1:]...)
			return
		}
	}
}

// ClaimPusher returns the user who made the one remembered push that all of refs belong to, and uses those ref
// changes up: a ref change is attributed once. It answers false, and uses nothing, unless
//
//   - refs is not empty, has no more than MaxRefsPerPush entries and every ref change of it is still unclaimed in the
//     same remembered push (the same user, the same import); a call that mixes ref changes of different pushes, or
//     that holds one nobody remembers, is attributed to nobody; and
//   - no other remembered push of the repository fits as well with another user (an ambiguous call is attributed
//     to nobody).
func ClaimPusher(repo *repo_model.Repository, refs []PushRef) (int64, bool) {
	user, _, ok := matchPusher(repo, refs, true, false)
	return user, ok
}

// PeekWriter returns the user who is making the write of refs through the hub's writer (RememberWriter) without
// using the ref changes up, for the pre-receive hook, which is called before the post-receive hook for the same
// ref changes. It answers false for a push that was not made by the writer, for a call that holds a ref change
// nobody remembers, and for one that more than one user remembers (it is attributed to nobody, and the hook then
// refuses it).
func PeekWriter(repo *repo_model.Repository, refs []PushRef) (userID, pullRequestID int64, ok bool) {
	return matchPusher(repo, refs, false, true)
}

// matchPusher is ClaimPusher (consume) and PeekWriter (onlyWriter, no consume).
func matchPusher(repo *repo_model.Repository, refs []PushRef, consume, onlyWriter bool) (user, pr int64, ok bool) {
	if repo == nil || len(refs) == 0 || len(refs) > MaxRefsPerPush {
		return 0, 0, false
	}
	keys, normOK := normalised(refs)
	if !normOK {
		return 0, 0, false
	}
	now := pusherClock()
	pusherMu.Lock()
	defer pusherMu.Unlock()
	rp := pushers[repo.ID]
	if rp == nil {
		return 0, 0, false
	}
	rp.dropExpired(now)
	if len(rp.list) == 0 {
		delete(pushers, repo.ID)
		return 0, 0, false
	}
	var match *push
	for _, p := range rp.list {
		if onlyWriter && !p.byWriter {
			continue
		}
		fits := true
		for k := range keys {
			if _, in := p.left[p.keyOf(k)]; !in {
				fits = false
				break
			}
		}
		if !fits {
			continue
		}
		if match != nil && match.user != p.user {
			return 0, 0, false
		}
		if match == nil {
			match = p
		}
	}
	if match == nil {
		return 0, 0, false
	}
	if !consume {
		return match.user, match.pr, true
	}
	for k := range keys {
		delete(match.left, match.keyOf(k))
	}
	if len(match.left) == 0 {
		rp.remove(match)
	}
	if len(rp.list) == 0 {
		delete(pushers, repo.ID)
	}
	return match.user, match.pr, true
}

// RememberWriter records that userID is making a write of refs to repo through the hub's own writer (a web edit, a
// merge, a branch operation), before it is sent to asc. asc runs the hub's pre-receive and post-receive hooks for it
// as the hub's service credential and names nobody, so the hooks take the person back from here (PeekWriter,
// ClaimPusher) and run their checks as that person, as they do for a git push. Only the ref and the new commit are
// known to the writer; the old commit of a PushRef is ignored. It returns the function that forgets the record, which
// the writer calls when the write fails. The record is not used up by the pre-receive hook, and ends when the
// post-receive hook claims it, or after pusherTTL.
//
// A zero or negative user, an empty write and one of more than MaxRefsPerPush refs are not remembered.
func RememberWriter(repo *repo_model.Repository, userID, pullRequestID int64, refs []PushRef) PushToken {
	if repo == nil || userID <= 0 || len(refs) == 0 || len(refs) > MaxRefsPerPush {
		return PushToken{}
	}
	keys := make(map[pushKey]struct{}, len(refs))
	for _, r := range refs {
		if r.Name == "" {
			return PushToken{}
		}
		n := r.New
		if isZeroID(n) {
			n = ""
		}
		keys[pushKey{ref: r.Name, new: n}] = struct{}{}
	}
	now := pusherClock()
	pusherMu.Lock()
	defer pusherMu.Unlock()
	rp := pushers[repo.ID]
	if rp == nil {
		if len(pushers) >= maxPusherRepos {
			evictQuietRepo(now)
		}
		rp = &repoPushes{}
		pushers[repo.ID] = rp
	}
	rp.dropExpired(now)
	if len(rp.list) >= MaxPushesPerRepo {
		rp.list = rp.list[len(rp.list)-MaxPushesPerRepo+1:]
	}
	p := &push{user: userID, expires: now.Add(pusherTTL), left: keys, byWriter: true, pr: max(pullRequestID, 0)}
	rp.list = append(rp.list, p)
	rp.active = now
	return PushToken{repo.ID, p}
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
