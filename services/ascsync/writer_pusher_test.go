// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
	"context"
	"testing"
	"time"

	repo_model "forgejo.org/models/repo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRememberedWriterIsPeekedByPreReceiveAndClaimedOncePostReceive(t *testing.T) {
	resetPushers(t)
	repo := &repo_model.Repository{ID: 51}
	// The writer knows the ref and the new commit; the hook knows the old commit as well.
	RememberWriter(repo, 7, 3, []PushRef{{Name: "refs/heads/a", New: "aaaa"}, {Name: "refs/heads/gone", New: ""}})

	both := []PushRef{ref("refs/heads/a", "1111", "aaaa"), ref("refs/heads/gone", "2222", zeroID)}
	for range 3 {
		user, pr, ok := PeekWriter(repo, both)
		assert.True(t, ok, "pre-receive does not use the record up")
		assert.Equal(t, int64(7), user)
		assert.Equal(t, int64(3), pr)
	}
	user, ok := ClaimPusher(repo, both)
	assert.True(t, ok)
	assert.Equal(t, int64(7), user)
	_, ok = ClaimPusher(repo, both)
	assert.False(t, ok, "attributed once")
	_, _, ok = PeekWriter(repo, both)
	assert.False(t, ok)
	assert.Zero(t, pusherCount(repo))
}

func TestWriterRecordFitsOnlyTheWholeWrite(t *testing.T) {
	resetPushers(t)
	repo := &repo_model.Repository{ID: 52}
	RememberWriter(repo, 7, 0, []PushRef{{Name: "refs/heads/a", New: "aaaa"}})

	for name, refs := range map[string][]PushRef{
		"another ref":                   {ref("refs/heads/b", "1111", "aaaa")},
		"another commit":                {ref("refs/heads/a", "1111", "bbbb")},
		"the write and a ref of nobody": {ref("refs/heads/a", "1111", "aaaa"), ref("refs/heads/c", "1111", "cccc")},
		"no refs":                       nil,
	} {
		_, _, ok := PeekWriter(repo, refs)
		assert.False(t, ok, name)
	}
	_, _, ok := PeekWriter(&repo_model.Repository{ID: 53}, []PushRef{ref("refs/heads/a", "1111", "aaaa")})
	assert.False(t, ok, "another repository")
	_, _, ok = PeekWriter(repo, []PushRef{ref("refs/heads/a", "1111", "aaaa")})
	assert.True(t, ok)
}

func TestTwoPeopleWritingTheSameRefChangeAreNobody(t *testing.T) {
	resetPushers(t)
	repo := &repo_model.Repository{ID: 54}
	refs := []PushRef{ref("refs/heads/a", "1111", "aaaa")}
	RememberWriter(repo, 7, 0, []PushRef{{Name: "refs/heads/a", New: "aaaa"}})
	b := RememberWriter(repo, 8, 0, []PushRef{{Name: "refs/heads/a", New: "aaaa"}})
	_, _, ok := PeekWriter(repo, refs)
	assert.False(t, ok, "an ambiguous call is attributed to nobody")
	_, ok = ClaimPusher(repo, refs)
	assert.False(t, ok)
	b.Forget()
	user, _, ok := PeekWriter(repo, refs)
	assert.True(t, ok)
	assert.Equal(t, int64(7), user)
}

func TestAGitOriginRecordIsNotAWriterRecord(t *testing.T) {
	resetPushers(t)
	repo := &repo_model.Repository{ID: 55}
	_, err := RememberPusher(repo, 7, []PushRef{ref("refs/heads/a", zeroID, "aaaa")})
	require.NoError(t, err)
	_, _, ok := PeekWriter(repo, []PushRef{ref("refs/heads/a", zeroID, "aaaa")})
	assert.False(t, ok, "pre-receive takes a person from the writer's record only")
	// And a writer's record does not answer for another old commit through the git-origin rule.
	RememberWriter(repo, 8, 0, []PushRef{{Name: "refs/heads/z", New: "zzzz"}})
	user, ok := ClaimPusher(repo, []PushRef{ref("refs/heads/a", zeroID, "aaaa")})
	assert.True(t, ok)
	assert.Equal(t, int64(7), user)
}

func TestWriterRecordIsForgottenAndExpires(t *testing.T) {
	resetPushers(t)
	now := time.Now()
	pusherClock = func() time.Time { return now }
	repo := &repo_model.Repository{ID: 56}
	refs := []PushRef{ref("refs/heads/a", "1111", "aaaa")}

	RememberWriter(repo, 7, 0, []PushRef{{Name: "refs/heads/a", New: "aaaa"}}).Forget()
	_, _, ok := PeekWriter(repo, refs)
	assert.False(t, ok, "forgotten")

	RememberWriter(repo, 7, 0, []PushRef{{Name: "refs/heads/a", New: "aaaa"}})
	now = now.Add(pusherTTL + time.Second)
	_, _, ok = PeekWriter(repo, refs)
	assert.False(t, ok, "expired")

	// Nothing is remembered for nobody, for no refs, for a nameless ref, or for too many refs.
	resetPushers(t)
	assert.Equal(t, PushToken{}, RememberWriter(repo, 0, 0, []PushRef{{Name: "refs/heads/a", New: "aaaa"}}))
	assert.Equal(t, PushToken{}, RememberWriter(repo, 7, 0, nil))
	assert.Equal(t, PushToken{}, RememberWriter(repo, 7, 0, []PushRef{{New: "aaaa"}}))
	many := make([]PushRef, MaxRefsPerPush+1)
	for i := range many {
		many[i] = PushRef{Name: "refs/heads/x" + string(rune('a'+i%26)) + string(rune('a'+i/26)), New: "aaaa"}
	}
	assert.Equal(t, PushToken{}, RememberWriter(repo, 7, 0, many))
	assert.Zero(t, pusherCount(repo))
}

func TestWriterWaitsUntilPostReceiveIsSettled(t *testing.T) {
	resetPushers(t)
	repo := &repo_model.Repository{ID: 57}
	refs := []PushRef{ref("refs/heads/a", "1111", "aaaa")}
	tok := RememberWriter(repo, 7, 0, []PushRef{{Name: "refs/heads/a", New: "aaaa"}})

	// Nothing has been claimed: the wait ends at its limit.
	start := time.Now()
	tok.Wait(t.Context(), 60*time.Millisecond)
	assert.GreaterOrEqual(t, time.Since(start), 50*time.Millisecond)

	// The claim alone is not enough: the hook is still working. The wait ends when the hook says it is settled.
	user, settled, ok := ClaimPusherUntilSettled(repo, refs)
	require.True(t, ok)
	assert.Equal(t, int64(7), user)
	released := make(chan time.Duration)
	go func() {
		s := time.Now()
		tok.Wait(t.Context(), 5*time.Second)
		released <- time.Since(s)
	}()
	time.Sleep(80 * time.Millisecond)
	select {
	case <-released:
		t.Fatal("the writer was released before the hook had settled")
	default:
	}
	settled()
	select {
	case d := <-released:
		assert.Less(t, d, 2*time.Second)
	case <-time.After(2 * time.Second):
		t.Fatal("the writer was not released when the hook settled")
	}
	settled() // twice is harmless
}

func TestWriterWaitEndsForAWriteThatHasNothingToWaitFor(t *testing.T) {
	resetPushers(t)
	repo := &repo_model.Repository{ID: 58}
	start := time.Now()
	PushToken{}.Wait(t.Context(), 5*time.Second)
	assert.Less(t, time.Since(start), time.Second, "the zero token")
	tok, err := RememberPusher(repo, 7, []PushRef{ref("refs/heads/a", zeroID, "aaaa")})
	require.NoError(t, err)
	tok.Wait(t.Context(), 5*time.Second)
	assert.Less(t, time.Since(start), time.Second, "a record of a git push")

	// A claim of a call that holds only some of the ref changes does not release the writer.
	w := RememberWriter(repo, 7, 0, []PushRef{{Name: "refs/heads/a", New: "aaaa"}, {Name: "refs/heads/b", New: "bbbb"}})
	_, settled, ok := ClaimPusherUntilSettled(repo, []PushRef{ref("refs/heads/a", "1111", "aaaa")})
	require.True(t, ok)
	settled()
	s := time.Now()
	w.Wait(t.Context(), 60*time.Millisecond)
	assert.GreaterOrEqual(t, time.Since(s), 50*time.Millisecond, "one ref change is still to be reported")
	_, settled, ok = ClaimPusherUntilSettled(repo, []PushRef{ref("refs/heads/b", "2222", "bbbb")})
	require.True(t, ok)
	settled()
	s = time.Now()
	w.Wait(t.Context(), 5*time.Second)
	assert.Less(t, time.Since(s), time.Second)

	// A cancelled context ends the wait.
	w2 := RememberWriter(repo, 7, 0, []PushRef{{Name: "refs/heads/c", New: "cccc"}})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s = time.Now()
	w2.Wait(ctx, 5*time.Second)
	assert.Less(t, time.Since(s), time.Second)
}
