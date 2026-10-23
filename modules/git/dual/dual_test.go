// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package dual_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"forgejo.org/modules/git"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/git/dual"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stub is a shadow backend: a real git backend with a few calls replaced.
type stub struct {
	git.Backend
	readCommit func(ctx context.Context, id git.ObjectID) (*git.CommitData, error)
	readTree   func(ctx context.Context, id git.ObjectID) ([]git.TreeEntryData, error)
	head       func(ctx context.Context) (git.RefName, error)
	count      func(ctx context.Context, q git.LogQuery) (int64, error)
	openBlob   func(ctx context.Context, id git.ObjectID, limit int64) (io.ReadCloser, int64, error)
}

func (s *stub) ReadCommit(ctx context.Context, id git.ObjectID) (*git.CommitData, error) {
	if s.readCommit != nil {
		return s.readCommit(ctx, id)
	}
	return s.Backend.ReadCommit(ctx, id)
}

func (s *stub) ReadTree(ctx context.Context, id git.ObjectID) ([]git.TreeEntryData, error) {
	if s.readTree != nil {
		return s.readTree(ctx, id)
	}
	return s.Backend.ReadTree(ctx, id)
}

func (s *stub) Head(ctx context.Context) (git.RefName, error) {
	if s.head != nil {
		return s.head(ctx)
	}
	return s.Backend.Head(ctx)
}

func (s *stub) Count(ctx context.Context, q git.LogQuery) (int64, error) {
	if s.count != nil {
		return s.count(ctx, q)
	}
	return s.Backend.Count(ctx, q)
}

func (s *stub) OpenBlob(ctx context.Context, id git.ObjectID, limit int64) (io.ReadCloser, int64, error) {
	if s.openBlob != nil {
		return s.openBlob(ctx, id, limit)
	}
	return s.Backend.OpenBlob(ctx, id, limit)
}

// mismatches collects the Mismatch values of a Backend.
type mismatches struct {
	mu  sync.Mutex
	got []dual.Mismatch
}

func (m *mismatches) add(x dual.Mismatch) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.got = append(m.got, x)
}

func (m *mismatches) methods() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, x := range m.got {
		out = append(out, x.Method)
	}
	return out
}

func always(int) bool { return true }

// setup returns the dual Backend over the fixture, its git backend, and a recorder of mismatches.
func setup(t *testing.T, fx *fixture, shadow git.Backend, opts dual.Options) (*dual.Backend, git.Backend, *mismatches) {
	t.Helper()
	primary := fx.open(t, fx.path)
	if shadow == nil {
		shadow = fx.open(t, fx.clone)
	}
	rec := &mismatches{}
	if opts.SamplePercent == 0 {
		opts.SamplePercent = 100
	}
	if opts.Sample == nil {
		opts.Sample = always
	}
	opts.OnMismatch = rec.add
	d := dual.New(primary, shadow, "owner/repo", opts)
	t.Cleanup(func() { _ = d.Close() })
	return d, primary, rec
}

func flush(t *testing.T, d *dual.Backend) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	require.NoError(t, d.Flush(ctx))
}

// since returns the change of a counter map since before.
func since(before, after map[string]uint64, key string) uint64 { return after[key] - before[key] }

// exerciseAll makes every sampled read once. It waits for each comparison before the next call, because
// the stand-in shadow, a git backend, serves one call at a time (ascbackend is safe for concurrent use).
func exerciseAll(t *testing.T, d *dual.Backend, fx *fixture) {
	t.Helper()
	ctx := t.Context()
	c1, c2, c3 := fx.id("c1"), fx.id("c2"), fx.id("c3")
	steps := []func(){
		func() { _, err := d.ReadCommit(ctx, c3); require.NoError(t, err) },
		func() { _, err := d.ReadTree(ctx, c3); require.NoError(t, err) },
		func() {
			for _, err := range d.ReadTreeRecursive(ctx, c3, git.TreeWalkOpts{Trees: true}) {
				require.NoError(t, err)
			}
		},
		func() {
			rc, _, err := d.OpenBlob(ctx, fx.id("big"), -1)
			require.NoError(t, err)
			_, err = io.Copy(io.Discard, rc)
			require.NoError(t, err)
			require.NoError(t, rc.Close())
		},
		func() {
			_, err := d.StatObjects(ctx, []git.ObjectID{c1, fx.id("blobA1"), fx.id("tag"), fx.id("treeC3")})
			require.NoError(t, err)
		},
		func() { _, _, err := d.Resolve(ctx, "master"); require.NoError(t, err) },
		func() { _, err := d.ListRefs(ctx, git.RefQuery{Prefix: "refs/"}); require.NoError(t, err) },
		func() { _, err := d.Head(ctx); require.NoError(t, err) },
		func() {
			for _, err := range d.Log(ctx, git.LogQuery{From: []git.ObjectID{c3}}) {
				require.NoError(t, err)
			}
		},
		func() { _, err := d.Count(ctx, git.LogQuery{From: []git.ObjectID{c3}}); require.NoError(t, err) },
		func() { _, err := d.MergeBase(ctx, c1, c3); require.NoError(t, err) },
		func() { _, err := d.IsAncestor(ctx, c1, c3); require.NoError(t, err) },
		func() {
			_, err := d.LastCommits(ctx, c3, "", []string{"a2.txt", "big.bin", "dir"})
			require.NoError(t, err)
		},
		func() {
			for _, err := range d.Blame(ctx, c3, "a2.txt", git.BlameOpts{}) {
				require.NoError(t, err)
			}
		},
		func() { _, err := d.ChangedPaths(ctx, c1, c3, git.DiffOpts{}); require.NoError(t, err) },
		func() {
			var buf bytes.Buffer
			require.NoError(t, d.UnifiedDiff(ctx, c1, c2, git.DiffOpts{}, &buf))
		},
		func() { _, err := d.DiffStat(ctx, c1, c3); require.NoError(t, err) },
		func() { _, err := d.Grep(ctx, c3, git.GrepQuery{Search: "line"}); require.NoError(t, err) },
		func() {
			_, err := d.Attributes(ctx, c3, []string{"a2.txt"}, []string{"linguist-language"})
			require.NoError(t, err)
		},
		func() { _, err := d.LanguageStats(ctx, c3); require.NoError(t, err) },
	}
	for _, step := range steps {
		step()
		flush(t, d)
	}
}

var sampledMethods = []string{
	"ReadCommit", "ReadTree", "ReadTreeRecursive", "OpenBlob", "StatObjects", "Resolve", "ListRefs", "Head",
	"Log", "Count", "MergeBase", "IsAncestor", "LastCommits", "Blame", "ChangedPaths", "UnifiedDiff", "DiffStat",
	"Grep", "Attributes", "LanguageStats",
}

// With an identical shadow, every sampled method compares equal and the answers are the git answers.
func TestIdenticalShadowNeverMismatches(t *testing.T) {
	fx := newFixture(t)
	d, _, rec := setup(t, fx, nil, dual.Options{})
	before := dual.Snapshot()
	exerciseAll(t, d, fx)
	flush(t, d)
	after := dual.Snapshot()

	assert.Empty(t, rec.methods())
	for _, m := range sampledMethods {
		assert.Positive(t, since(before.Compared, after.Compared, m), "%s was not compared", m)
		assert.Zero(t, since(before.Mismatched, after.Mismatched, m), "%s", m)
	}
}

// The answer a caller gets is the git answer, byte for byte, whatever the shadow does.
func TestAnswerIsTheGitAnswer(t *testing.T) {
	fx := newFixture(t)
	shadow := &stub{Backend: fx.open(t, fx.clone)}
	shadow.head = func(context.Context) (git.RefName, error) { return "refs/heads/elsewhere", nil }
	shadow.readCommit = func(context.Context, git.ObjectID) (*git.CommitData, error) {
		return &git.CommitData{ID: fx.id("c1")}, nil
	}
	d, primary, rec := setup(t, fx, shadow, dual.Options{})

	want, err := primary.ReadCommit(t.Context(), fx.id("c3"))
	require.NoError(t, err)
	got, err := d.ReadCommit(t.Context(), fx.id("c3"))
	require.NoError(t, err)
	assert.Equal(t, want, got)

	wantHead, err := primary.Head(t.Context())
	require.NoError(t, err)
	gotHead, err := d.Head(t.Context())
	require.NoError(t, err)
	assert.Equal(t, wantHead, gotHead)

	var direct, viaDual bytes.Buffer
	require.NoError(t, primary.UnifiedDiff(t.Context(), fx.id("c1"), fx.id("c3"), git.DiffOpts{Mode: git.DiffModeBinary}, &direct))
	require.NoError(t, d.UnifiedDiff(t.Context(), fx.id("c1"), fx.id("c3"), git.DiffOpts{Mode: git.DiffModeBinary}, &viaDual))
	assert.Equal(t, direct.Bytes(), viaDual.Bytes())

	flush(t, d)
	assert.ElementsMatch(t, []string{"ReadCommit", "Head"}, rec.methods(), "the differences are reported, not applied")
}

// A shadow that has a different history is caught, and the callers still see git's history.
func TestDifferentShadowIsCounted(t *testing.T) {
	fx := newFixture(t)
	env := []string{
		"GIT_AUTHOR_NAME=A", "GIT_AUTHOR_EMAIL=a@example.com", "GIT_COMMITTER_NAME=A", "GIT_COMMITTER_EMAIL=a@example.com",
	}
	extra := gitRun(t, fx.clone, env, "commit-tree", "-p", fx.id("c3").String(), "-m", "only in asc", fx.id("treeC3").String())
	gitRun(t, fx.clone, nil, "update-ref", "refs/heads/master", extra)

	d, _, rec := setup(t, fx, nil, dual.Options{})
	before := dual.Snapshot()
	ctx := t.Context()

	n, err := d.Count(ctx, git.LogQuery{All: true})
	require.NoError(t, err)
	assert.EqualValues(t, 3, n)
	flush(t, d) // the stand-in shadow, a git backend, serves one call at a time
	count := 0
	for _, err := range d.Log(ctx, git.LogQuery{All: true}) {
		require.NoError(t, err)
		count++
	}
	assert.Equal(t, 3, count)
	flush(t, d)
	refs, err := d.ListRefs(ctx, git.RefQuery{Prefix: "refs/heads/"})
	require.NoError(t, err)
	flush(t, d)
	for _, r := range refs {
		if r.Name == "refs/heads/master" {
			assert.Equal(t, fx.id("c3").String(), r.Object.String())
		}
	}
	_, err = d.ReadCommit(ctx, fx.id("c3")) // the same on both sides
	require.NoError(t, err)
	flush(t, d)

	after := dual.Snapshot()
	assert.ElementsMatch(t, []string{"Count", "Log", "ListRefs"}, rec.methods())
	for _, m := range []string{"Count", "Log", "ListRefs"} {
		assert.EqualValues(t, 1, since(before.Mismatched, after.Mismatched, m), m)
	}
	assert.Zero(t, since(before.Mismatched, after.Mismatched, "ReadCommit"))
	for _, m := range rec.got {
		assert.Equal(t, "owner/repo", m.Repo)
		assert.NotEmpty(t, m.Detail)
		assert.NotEmpty(t, m.Git, m.Method)
		assert.NotEmpty(t, m.Asc, m.Method)
		if m.Method == "Count" {
			assert.Equal(t, "3", m.Git)
			assert.Equal(t, "4", m.Asc)
		}
	}
}

// A shadow that panics, hangs or fails never reaches the caller.
func TestShadowFailuresStayInTheShadow(t *testing.T) {
	fx := newFixture(t)
	shadow := &stub{Backend: fx.open(t, fx.clone)}
	shadow.readCommit = func(context.Context, git.ObjectID) (*git.CommitData, error) { panic("boom") }
	shadow.head = func(context.Context) (git.RefName, error) { return "", ascbackend.ErrUnsupported }
	shadow.count = func(context.Context, git.LogQuery) (int64, error) {
		return 0, errors.New("dial tcp 10.0.0.1:443: token=secret")
	}
	d, _, rec := setup(t, fx, shadow, dual.Options{})
	before := dual.Snapshot()

	c, err := d.ReadCommit(t.Context(), fx.id("c3"))
	require.NoError(t, err)
	assert.Equal(t, fx.id("c3").String(), c.ID.String())
	head, err := d.Head(t.Context())
	require.NoError(t, err)
	assert.Equal(t, git.RefName("refs/heads/master"), head)
	n, err := d.Count(t.Context(), git.LogQuery{From: []git.ObjectID{fx.id("c3")}})
	require.NoError(t, err)
	assert.EqualValues(t, 3, n)
	flush(t, d)

	after := dual.Snapshot()
	assert.Empty(t, rec.methods(), "a failing shadow is not a data mismatch")
	assert.EqualValues(t, 2, since(before.Skipped, after.Skipped, "shadow_error"))
	assert.EqualValues(t, 1, since(before.Skipped, after.Skipped, "unsupported"))
}

func TestMissingObjects(t *testing.T) {
	fx := newFixture(t)
	missing := git.MustIDFromString("1234567890123456789012345678901234567890")
	t.Run("both missing is a match", func(t *testing.T) {
		d, _, rec := setup(t, fx, nil, dual.Options{})
		before := dual.Snapshot()
		_, err := d.ReadCommit(t.Context(), missing)
		require.Error(t, err)
		flush(t, d)
		after := dual.Snapshot()
		assert.Empty(t, rec.methods())
		assert.EqualValues(t, 1, since(before.Compared, after.Compared, "ReadCommit"))
	})
	t.Run("git missing but asc has it", func(t *testing.T) {
		shadow := &stub{Backend: fx.open(t, fx.clone)}
		shadow.readCommit = func(ctx context.Context, _ git.ObjectID) (*git.CommitData, error) {
			return shadow.Backend.ReadCommit(ctx, fx.id("c1"))
		}
		d, _, rec := setup(t, fx, shadow, dual.Options{})
		_, err := d.ReadCommit(t.Context(), missing)
		require.Error(t, err)
		flush(t, d)
		assert.Equal(t, []string{"ReadCommit"}, rec.methods())
	})
	t.Run("git has it but asc reports it missing", func(t *testing.T) {
		shadow := &stub{Backend: fx.open(t, fx.clone)}
		shadow.readCommit = func(context.Context, git.ObjectID) (*git.CommitData, error) {
			return nil, git.ErrNotExist{ID: "x"}
		}
		d, _, rec := setup(t, fx, shadow, dual.Options{})
		_, err := d.ReadCommit(t.Context(), fx.id("c3"))
		require.NoError(t, err)
		flush(t, d)
		assert.Equal(t, []string{"ReadCommit"}, rec.methods())
	})
	t.Run("a tree id the shadow cannot locate is a known limit, a commit id is not", func(t *testing.T) {
		shadow := &stub{Backend: fx.open(t, fx.clone)}
		shadow.readTree = func(context.Context, git.ObjectID) ([]git.TreeEntryData, error) {
			return nil, git.ErrNotExist{ID: "x"}
		}
		d, _, rec := setup(t, fx, shadow, dual.Options{})
		before := dual.Snapshot()
		_, err := d.ReadTree(t.Context(), fx.id("treeC3"))
		require.NoError(t, err)
		flush(t, d)
		assert.Empty(t, rec.methods())
		assert.EqualValues(t, 1, since(before.Skipped, dual.Snapshot().Skipped, "uncomparable"))

		_, err = d.ReadTree(t.Context(), fx.id("c3"))
		require.NoError(t, err)
		flush(t, d)
		assert.Equal(t, []string{"ReadTree"}, rec.methods())
	})
	t.Run("an asc answer that failed its checks is a mismatch", func(t *testing.T) {
		shadow := &stub{Backend: fx.open(t, fx.clone)}
		shadow.head = func(context.Context) (git.RefName, error) { return "", ascbackend.ErrBadAnswer }
		d, _, rec := setup(t, fx, shadow, dual.Options{})
		_, err := d.Head(t.Context())
		require.NoError(t, err)
		flush(t, d)
		assert.Equal(t, []string{"Head"}, rec.methods())
	})
}

func TestSampling(t *testing.T) {
	fx := newFixture(t)
	t.Run("a call that is not sampled does not touch the shadow", func(t *testing.T) {
		shadow := &stub{Backend: fx.open(t, fx.clone)}
		calls := 0
		shadow.head = func(context.Context) (git.RefName, error) { calls++; return "refs/heads/master", nil }
		d, _, _ := setup(t, fx, shadow, dual.Options{Sample: func(int) bool { return false }})
		for range 20 {
			_, err := d.Head(t.Context())
			require.NoError(t, err)
		}
		flush(t, d)
		assert.Zero(t, calls)
	})
	t.Run("percent zero turns the shadow off", func(t *testing.T) {
		primary := fx.open(t, fx.path)
		d := dual.New(primary, fx.open(t, fx.clone), "o/r", dual.Options{SamplePercent: 0})
		assert.False(t, d.ShadowEnabled())
		_, err := d.Head(t.Context())
		require.NoError(t, err)
		require.NoError(t, d.Close())
	})
	t.Run("a nil shadow is a pass-through", func(t *testing.T) {
		d := dual.New(fx.open(t, fx.path), nil, "o/r", dual.Options{SamplePercent: 100})
		assert.False(t, d.ShadowEnabled())
		_, err := d.Head(t.Context())
		require.NoError(t, err)
		require.NoError(t, d.Close())
	})
	t.Run("the percent is clamped", func(t *testing.T) {
		var seen []int
		for _, p := range []int{500, -3} {
			opts := dual.Options{SamplePercent: p, Sample: func(percent int) bool { seen = append(seen, percent); return false }}
			d := dual.New(fx.open(t, fx.path), fx.open(t, fx.clone), "o/r", opts)
			_, err := d.Head(t.Context())
			require.NoError(t, err)
			require.NoError(t, d.Close())
		}
		assert.Equal(t, []int{100}, seen, "-3 becomes 0, which turns the shadow off")
	})
	t.Run("writes and tag reads go to git only", func(t *testing.T) {
		shadow := &stub{Backend: fx.open(t, fx.clone)}
		calls := 0
		shadow.head = func(context.Context) (git.RefName, error) { calls++; return "", nil }
		d, primary, _ := setup(t, fx, shadow, dual.Options{})
		require.NoError(t, d.SetHead(t.Context(), "refs/heads/feature"))
		head, err := primary.Head(t.Context())
		require.NoError(t, err)
		assert.Equal(t, git.RefName("refs/heads/feature"), head)
		assert.Zero(t, calls)
		_, err = d.ReadTag(t.Context(), fx.id("tag"))
		require.NoError(t, err)
		flush(t, d)
	})
}

// Shadow calls are bounded, and Close cancels them.
func TestBoundsAndClose(t *testing.T) {
	fx := newFixture(t)
	t.Run("a sampled call over the in-flight cap is dropped", func(t *testing.T) {
		shadow := &stub{Backend: fx.open(t, fx.clone)}
		release := make(chan struct{})
		started := make(chan struct{}, 4)
		shadow.head = func(ctx context.Context) (git.RefName, error) {
			started <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
			}
			return "refs/heads/master", nil
		}
		d, _, rec := setup(t, fx, shadow, dual.Options{MaxInFlight: 1})
		before := dual.Snapshot()
		_, err := d.Head(t.Context())
		require.NoError(t, err)
		<-started
		_, err = d.Head(t.Context()) // the first is still running
		require.NoError(t, err)
		close(release)
		flush(t, d)
		assert.Empty(t, rec.methods())
		after := dual.Snapshot()
		assert.EqualValues(t, 1, since(before.Skipped, after.Skipped, "busy"))
		assert.EqualValues(t, 1, since(before.Compared, after.Compared, "Head"))

		_, err = d.Head(t.Context()) // the place is free again
		require.NoError(t, err)
		flush(t, d)
		assert.EqualValues(t, 2, since(before.Compared, dual.Snapshot().Compared, "Head"))
	})
	t.Run("the caller does not wait for the shadow and Close cancels it", func(t *testing.T) {
		shadow := &stub{Backend: fx.open(t, fx.clone)}
		started := make(chan struct{})
		shadow.head = func(ctx context.Context) (git.RefName, error) {
			close(started)
			<-ctx.Done()
			return "", ctx.Err()
		}
		d, _, _ := setup(t, fx, shadow, dual.Options{Timeout: time.Hour})
		before := dual.Snapshot()
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, err := d.Head(t.Context())
			assert.NoError(t, err)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("the caller waited for the shadow")
		}
		<-started
		start := time.Now()
		require.NoError(t, d.Close())
		assert.Less(t, time.Since(start), 4*time.Second)
		assert.EqualValues(t, 1, since(before.Skipped, dual.Snapshot().Skipped, "closed"))
		require.NoError(t, d.Close(), "a second Close is harmless")
	})
	t.Run("a shadow call that times out is skipped", func(t *testing.T) {
		shadow := &stub{Backend: fx.open(t, fx.clone)}
		shadow.head = func(ctx context.Context) (git.RefName, error) {
			<-ctx.Done()
			return "", ctx.Err()
		}
		d, _, rec := setup(t, fx, shadow, dual.Options{Timeout: 20 * time.Millisecond})
		before := dual.Snapshot()
		_, err := d.Head(t.Context())
		require.NoError(t, err)
		flush(t, d)
		assert.Empty(t, rec.methods())
		assert.EqualValues(t, 1, since(before.Skipped, dual.Snapshot().Skipped, "shadow_error"))
	})
}

// The digest is taken before the call returns, so editing a result afterwards neither races with the
// comparison nor changes it. Run with -race.
func TestResultEditedAfterReturn(t *testing.T) {
	fx := newFixture(t)
	shadow := &stub{Backend: fx.open(t, fx.clone)}
	proceed := make(chan struct{})
	shadow.readTree = func(ctx context.Context, id git.ObjectID) ([]git.TreeEntryData, error) {
		<-proceed
		return shadow.Backend.ReadTree(ctx, id)
	}
	d, _, rec := setup(t, fx, shadow, dual.Options{})
	entries, err := d.ReadTree(t.Context(), fx.id("c3"))
	require.NoError(t, err)
	for i := range entries {
		entries[i].Name = "edited"
		entries[i].ID = nil
	}
	close(proceed)
	flush(t, d)
	assert.Empty(t, rec.methods())
}

func TestBlobStream(t *testing.T) {
	fx := newFixture(t)
	read := func(t *testing.T, d *dual.Backend, limit int64, n int) {
		t.Helper()
		rc, size, err := d.OpenBlob(t.Context(), fx.id("big"), limit)
		require.NoError(t, err)
		assert.EqualValues(t, 10000, size, "the full size is returned whatever the limit")
		if n < 0 {
			_, err = io.Copy(io.Discard, rc)
		} else {
			_, err = io.ReadFull(rc, make([]byte, n))
		}
		require.NoError(t, err)
		require.NoError(t, rc.Close())
	}
	t.Run("a whole read is compared", func(t *testing.T) {
		d, _, rec := setup(t, fx, nil, dual.Options{})
		before := dual.Snapshot()
		read(t, d, -1, -1)
		flush(t, d) // the stand-in shadow, a git backend, serves one call at a time
		read(t, d, 100, -1)
		flush(t, d)
		assert.Empty(t, rec.methods())
		assert.EqualValues(t, 2, since(before.Compared, dual.Snapshot().Compared, "OpenBlob"))
	})
	t.Run("a partial read is not compared", func(t *testing.T) {
		d, _, rec := setup(t, fx, nil, dual.Options{})
		before := dual.Snapshot()
		read(t, d, -1, 10)
		flush(t, d)
		assert.Empty(t, rec.methods())
		assert.EqualValues(t, 1, since(before.Skipped, dual.Snapshot().Skipped, "partial"))
		assert.Zero(t, since(before.Compared, dual.Snapshot().Compared, "OpenBlob"))
	})
	t.Run("a blob that differs is caught", func(t *testing.T) {
		shadow := &stub{Backend: fx.open(t, fx.clone)}
		shadow.openBlob = func(ctx context.Context, id git.ObjectID, limit int64) (io.ReadCloser, int64, error) {
			rc, size, err := shadow.Backend.OpenBlob(ctx, id, limit)
			if err != nil {
				return nil, 0, err
			}
			_ = rc.Close()
			return io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), int(size)))), size, nil
		}
		d, _, rec := setup(t, fx, shadow, dual.Options{})
		read(t, d, -1, -1)
		flush(t, d)
		assert.Equal(t, []string{"OpenBlob"}, rec.methods())
	})
}

func TestIteratorsStopEarly(t *testing.T) {
	fx := newFixture(t)
	d, _, rec := setup(t, fx, nil, dual.Options{})
	before := dual.Snapshot()
	for _, err := range d.Log(t.Context(), git.LogQuery{From: []git.ObjectID{fx.id("c3")}}) {
		require.NoError(t, err)
		break
	}
	flush(t, d)
	assert.Empty(t, rec.methods())
	assert.EqualValues(t, 1, since(before.Skipped, dual.Snapshot().Skipped, "partial"))
	assert.Zero(t, since(before.Compared, dual.Snapshot().Compared, "Log"))
}

func TestFormatPatchDiffIsNotCompared(t *testing.T) {
	fx := newFixture(t)
	d, _, rec := setup(t, fx, nil, dual.Options{})
	before := dual.Snapshot()
	var buf bytes.Buffer
	require.NoError(t, d.UnifiedDiff(t.Context(), fx.id("c1"), fx.id("c3"), git.DiffOpts{Mode: git.DiffModePatch}, &buf))
	assert.NotZero(t, buf.Len())
	flush(t, d)
	assert.Empty(t, rec.methods())
	assert.EqualValues(t, 1, since(before.Skipped, dual.Snapshot().Skipped, "uncomparable"))
}
