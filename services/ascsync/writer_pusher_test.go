// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascsync

import (
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
