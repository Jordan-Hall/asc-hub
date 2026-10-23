// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package dual

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"forgejo.org/modules/git"
	"forgejo.org/modules/git/ascbackend"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
)

func TestOptionsAreBounded(t *testing.T) {
	o := Options{SamplePercent: 1000, MaxInFlight: 1 << 30, Timeout: 24 * time.Hour}.normalised()
	assert.Equal(t, 100, o.SamplePercent)
	assert.Equal(t, maxMaxInFlight, o.MaxInFlight)
	assert.Equal(t, maxTimeout, o.Timeout)
	require.NotNil(t, o.Sample)

	o = Options{SamplePercent: -5}.normalised()
	assert.Zero(t, o.SamplePercent)
	assert.Equal(t, defaultMaxInFlight, o.MaxInFlight)
	assert.Equal(t, defaultTimeout, o.Timeout)
}

func TestRandomSample(t *testing.T) {
	assert.False(t, randomSample(0))
	assert.True(t, randomSample(100))
	hits := 0
	for range 20000 {
		if randomSample(10) {
			hits++
		}
	}
	assert.InDelta(t, 2000, hits, 400)
}

func TestClassify(t *testing.T) {
	assert.Equal(t, clsNone, classify(nil))
	assert.Equal(t, clsNotExist, classify(git.ErrNotExist{ID: "x"}))
	assert.Equal(t, clsNotExist, classify(fmt.Errorf("wrapped: %w", git.ErrNotExist{ID: "x"})))
	assert.Equal(t, clsUnsupported, classify(fmt.Errorf("%w: ReadTag", ascbackend.ErrUnsupported)))
	assert.Equal(t, clsBad, classify(fmt.Errorf("%w: x", ascbackend.ErrBadAnswer)))
	assert.Equal(t, clsTooLarge, classify(ascbackend.ErrTooLarge))
	assert.Equal(t, clsTooLarge, classify(errOverLimit))
	assert.Equal(t, clsCancelled, classify(context.Canceled))
	assert.Equal(t, clsCancelled, classify(ascbackend.ErrClosed))
	assert.Equal(t, clsOther, classify(context.DeadlineExceeded))
	assert.Equal(t, clsOther, classify(errors.New("boom")))
}

// An error is described by its kind, never by its text, which may come from the network.
func TestDescribeErrDoesNotRepeatText(t *testing.T) {
	secret := "Bearer s3cr3t-token"
	assert.NotContains(t, describeErr(errors.New("dial: "+secret)), "s3cr3t")
	assert.NotContains(t, describeErr(&ascbackend.Error{Op: "ReadFile", Code: codes.Unavailable, Msg: secret}), "s3cr3t")
	assert.Contains(t, describeErr(&ascbackend.Error{Op: "ReadFile", Code: codes.Unavailable}), "ReadFile")
	assert.Contains(t, describeErr(ascbackend.ErrNotConfigured), "not configured")
}

func TestEcho(t *testing.T) {
	assert.Equal(t, "owner/repo", echo("owner/repo"))
	assert.Equal(t, "a?b?", echo("a\nb\x00"))
	assert.LessOrEqual(t, len(echo(strings.Repeat("x", 10_000))), maxNameEcho+3)
}

func TestLogBudget(t *testing.T) {
	logBudget.Lock()
	logBudget.minute, logBudget.used = time.Now().Truncate(time.Minute), 0
	logBudget.Unlock()
	written := 0
	for range maxLogsPerMinute * 3 {
		logf(func(string, ...any) { written++ }, "x")
	}
	// The window may roll over once while the loop runs.
	assert.LessOrEqual(t, written, 2*maxLogsPerMinute)
	assert.GreaterOrEqual(t, written, maxLogsPerMinute)
}

func TestFingerprints(t *testing.T) {
	id := func(s string) git.ObjectID { return git.MustIDFromString(strings.Repeat(s, 40)) }

	t.Run("rows are a set", func(t *testing.T) {
		assert.True(t, fpRows([]string{"b", "a"}).equal(fpRows([]string{"a", "b"})))
		assert.False(t, fpRows([]string{"a", "b"}).equal(fpRows([]string{"a", "c"})))
		assert.False(t, fpRows([]string{"a"}).equal(fpRows([]string{"a", "a"})))
	})
	t.Run("a boundary cannot be moved", func(t *testing.T) {
		assert.False(t, newFP().text("ab").text("c").done().equal(newFP().text("a").text("bc").done()))
	})
	t.Run("tree levels are ordered and ignore sizes", func(t *testing.T) {
		a := git.TreeEntryData{ID: id("a"), Name: "x", Mode: git.EntryModeBlob, Size: -1}
		b := git.TreeEntryData{ID: id("b"), Name: "y", Mode: git.EntryModeTree, Size: 0}
		assert.True(t, fpTree([]git.TreeEntryData{a, b}).equal(fpTree([]git.TreeEntryData{{ID: id("a"), Name: "x", Mode: git.EntryModeBlob, Size: 7}, b})))
		assert.False(t, fpTree([]git.TreeEntryData{a, b}).equal(fpTree([]git.TreeEntryData{b, a})))
		assert.False(t, fpTree([]git.TreeEntryData{a}).equal(fpTree([]git.TreeEntryData{{ID: id("a"), Name: "x", Mode: git.EntryModeExec}})))
	})
	t.Run("stat leaves out tags and trees and commits have no size", func(t *testing.T) {
		git1 := []git.ObjectInfo{
			{ID: id("a"), Type: git.ObjectCommit, Size: 250},
			{ID: id("b"), Type: git.ObjectTag, Size: 90},
			{ID: id("c"), Type: git.ObjectBlob, Size: 5},
		}
		asc := []git.ObjectInfo{
			{ID: id("a"), Type: git.ObjectCommit},
			{ID: id("b"), Missing: true},
			{ID: id("c"), Type: git.ObjectBlob, Size: 5},
		}
		skip := []bool{false, true, false}
		assert.True(t, fpStat(git1, skip).equal(fpStat(asc, skip)))
		asc[2].Size = 6
		assert.False(t, fpStat(git1, skip).equal(fpStat(asc, skip)))
	})
	t.Run("an annotated tag is compared by name", func(t *testing.T) {
		gitRefs := []git.Reference{{Name: "refs/tags/v1", Object: id("a"), Type: "tag"}, {Name: "refs/heads/m", Object: id("b"), Type: "commit"}}
		ascRefs := []git.Reference{{Name: "refs/heads/m", Object: id("b"), Type: "commit"}, {Name: "refs/tags/v1", Object: id("c"), Type: "commit"}}
		ann := annotatedTags(gitRefs)
		assert.True(t, fpRefs(gitRefs, ann).equal(fpRefs(ascRefs, ann)), "order and the peeled id are not compared")
		ascRefs[0].Object = id("d")
		assert.False(t, fpRefs(gitRefs, ann).equal(fpRefs(ascRefs, ann)))
	})
	t.Run("a nil commit or signature does not panic", func(t *testing.T) {
		assert.NotPanics(t, func() { fpCommit(nil) })
		assert.NotPanics(t, func() { fpCommit(&git.CommitData{}) })
		assert.False(t, fpCommit(nil).equal(fpCommit(&git.CommitData{})))
	})
	t.Run("the log excerpt is printable and short", func(t *testing.T) {
		b := newFP()
		for range 100 {
			b.ident("a\x00\xffb")
		}
		assert.LessOrEqual(t, len(b.done().head), maxHeadBytes)
		assert.NotContains(t, b.done().head, "\x00")
	})
	t.Run("content stays out of the excerpt", func(t *testing.T) {
		c := &git.CommitData{ID: id("a"), Tree: id("b"), Message: "password=hunter2"}
		assert.NotContains(t, fpCommit(c).head, "hunter2")
	})
}

func TestHashWriterCap(t *testing.T) {
	var out bytes.Buffer
	hw := &hashWriter{w: &out, h: sha256.New()}
	chunk := bytes.Repeat([]byte("z"), 1<<20)
	for range maxCompareBytes/len(chunk) + 3 {
		n, err := hw.Write(chunk)
		require.NoError(t, err)
		require.Equal(t, len(chunk), n)
	}
	assert.True(t, hw.over)
	assert.EqualValues(t, maxCompareBytes+3<<20, hw.total)
	assert.Equal(t, int(hw.total), out.Len(), "everything is passed through")

	small := &hashWriter{h: sha256.New()}
	_, _ = small.Write([]byte("abc"))
	other := &hashWriter{h: sha256.New()}
	_, _ = other.Write([]byte("abc"))
	assert.True(t, small.fingerprint().equal(other.fingerprint()))
	_, _ = other.Write([]byte("d"))
	assert.False(t, small.fingerprint().equal(other.fingerprint()))
}

func TestResolveUncomparable(t *testing.T) {
	for _, rev := range []string{"abcd", "1234567", ":/fix the bug", "v1^{commit}", "HEAD^{tree}"} {
		assert.True(t, resolveUncomparable(rev, git.ObjectCommit), rev)
	}
	assert.True(t, resolveUncomparable("v1", git.ObjectTag))
	for _, rev := range []string{"master", "refs/heads/main", "HEAD", "HEAD~2", strings.Repeat("a", 40), "abc", "deadbeefx"} {
		assert.False(t, resolveUncomparable(rev, git.ObjectCommit), rev)
	}
}
