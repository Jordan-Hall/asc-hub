// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package dual is the git.Backend for storage_backend = dual (SPEC section 13.2, PLAN unit K11).
//
// A dual repository is served from its git directory, exactly as storage_backend = git is. On a
// sample of read calls (10% by default, [asc] SHADOW_SAMPLE_PERCENT) the same call is repeated against
// the asc backend (modules/git/ascbackend) in the background and the two answers are compared. A
// difference is counted in forgejo_asc_shadow_mismatch_total{method} and logged. This is the evidence
// that a repository can move from dual to asc (Stage 3).
//
// # The shadow never changes a response
//
//   - The answer of the git backend is returned as it is, always. It is summarised (a digest of the
//     fields that both backends can answer) before it is returned, so a caller that edits the result
//     afterwards cannot race with the comparison.
//   - The shadow call runs in its own goroutine, on its own context, with a time limit. The caller never
//     waits for it, and a failing, slow, unsupported or panicking shadow only changes a counter.
//   - Shadow work is bounded: a process-wide cap on calls in flight (a sampled call over the cap is
//     dropped and counted), a time limit per call, a cap on the bytes and items one comparison reads,
//     and a cap on log lines per minute. Close cancels the work in flight.
//   - The shadow only reads. Writes (SetHead) go to the git backend alone; asc learns of them through
//     the Stage 2 hooks, not through this package.
//
// # What counts as a mismatch
//
// A mismatch is a call where git answered and asc gave a different answer, or where git reports a
// missing object that asc returns (or the other way round), or where asc sent an answer that failed its
// own verification (ascbackend.ErrBadAnswer). A call is skipped, and counted in
// forgejo_asc_shadow_skipped_total{reason}, when it cannot be compared: the shadow does not serve it
// (ascbackend.ErrUnsupported), it failed for a reason that says nothing about the data (outage, deadline,
// refusal), the result is over a comparison cap, the caller did not read a stream to its end, or the
// two backends are documented to answer differently (see below).
//
// Documented differences, kept out of the comparison rather than hidden in it:
//
//   - Annotated tag objects: ReadTag is not sampled, ListRefs compares an annotated tag by name only,
//     StatObjects leaves tag ids out and Resolve of a tag is skipped (asc peels tags).
//   - A tree id that the shadow has not seen is unknown to it (ascbackend lists trees through their
//     commit), so ReadTree and ReadTreeRecursive of a tree id are skipped when asc reports it missing.
//   - Resolve of an abbreviated id, a message search or a peel suffix is skipped.
//   - Entry sizes of a tree, the order of references and of a recursive walk, the highlight ranges of
//     Grep, and PreviousSha and PreviousPath of Blame are not compared.
//   - Archive and a format-patch UnifiedDiff are not compared: git and asc write different bytes for
//     the same content (archive headers, patch trailers).
package dual

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"forgejo.org/modules/git"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/log"
	"forgejo.org/modules/setting"
)

// Name is the name this backend registers under (see modules/gitrepo/backend_select.go).
const Name = "dual"

// Named limits. Each bounds the memory, time or log volume that the shadow can cost.
const (
	// DefaultSamplePercent is the share of read calls that are repeated against asc (SPEC section 13.2).
	DefaultSamplePercent = 10
	// defaultMaxInFlight is the default for Options.MaxInFlight.
	defaultMaxInFlight = 8
	// maxMaxInFlight caps Options.MaxInFlight.
	maxMaxInFlight = 256
	// defaultTimeout is the default for Options.Timeout.
	defaultTimeout = 30 * time.Second
	// maxTimeout caps Options.Timeout.
	maxTimeout = 10 * time.Minute
	// closeWait bounds how long Close waits for the shadow calls it cancelled.
	closeWait = 5 * time.Second
	// maxCompareBytes caps the bytes one blob or diff comparison reads from either side.
	maxCompareBytes = 8 << 20
	// maxCompareItems caps the items (tree entries, commits, blame runs, paths) one comparison collects.
	maxCompareItems = 200_000
	// maxLogsPerMinute caps the log lines this package writes in one minute, across all repositories.
	maxLogsPerMinute = 30
	// maxHeadBytes caps the readable excerpt of a digest that a mismatch log line carries.
	maxHeadBytes = 160
	// maxNameEcho caps the repository name a log line repeats.
	maxNameEcho = 200
)

// Options tunes the shadow comparison.
type Options struct {
	// SamplePercent is the share of read calls (0 to 100) that are repeated against asc. Zero turns the
	// shadow off. Values outside the range are clamped.
	SamplePercent int
	// MaxInFlight caps the shadow calls running at once in the whole process (default 8). A sampled
	// call over the cap is dropped.
	MaxInFlight int
	// Timeout bounds one shadow call (default 30 seconds).
	Timeout time.Duration
	// Sample decides whether one call is sampled. The default draws at random.
	Sample func(percent int) bool
	// OnMismatch is called for every mismatch, after it is counted and logged. It must not block.
	OnMismatch func(Mismatch)
}

// Mismatch describes one difference between the git answer and the asc answer.
type Mismatch struct {
	Method string
	Repo   string
	// Detail says what differs, in a few words.
	Detail string
	// Git and Asc are short excerpts of the two answers: ids, counts and names, never file content.
	Git string
	Asc string
}

// DefaultOptions returns the options of a dual repository when nothing else is configured:
// [asc] SHADOW_SAMPLE_PERCENT of the calls, at most defaultMaxInFlight in flight.
func DefaultOptions() Options {
	return Options{SamplePercent: setting.Asc.ShadowSamplePercent}.normalised()
}

func (o Options) normalised() Options {
	o.SamplePercent = min(max(o.SamplePercent, 0), 100)
	switch {
	case o.MaxInFlight <= 0:
		o.MaxInFlight = defaultMaxInFlight
	case o.MaxInFlight > maxMaxInFlight:
		o.MaxInFlight = maxMaxInFlight
	}
	switch {
	case o.Timeout <= 0:
		o.Timeout = defaultTimeout
	case o.Timeout > maxTimeout:
		o.Timeout = maxTimeout
	}
	if o.Sample == nil {
		o.Sample = randomSample
	}
	return o
}

func randomSample(percent int) bool {
	return percent >= 100 || (percent > 0 && rand.IntN(100) < percent)
}

// configured holds the options set by Configure; nil means DefaultOptions.
var configured atomic.Pointer[Options]

// Configure sets the options every dual Backend opened from now on uses, replacing the [asc] settings.
// Reset undoes it.
func Configure(opts Options) {
	opts = opts.normalised()
	configured.Store(&opts)
}

// Reset removes the configuration set by Configure.
func Reset() { configured.Store(nil) }

func currentOptions() Options {
	if o := configured.Load(); o != nil {
		return *o
	}
	return DefaultOptions()
}

func init() {
	git.RegisterBackend(Name, newFromRegistry)
}

// newFromRegistry binds the git backend of repo and, when the shadow is on, the asc backend. The git
// backend failing is an error. The shadow failing (the asc client is not set up) only turns the shadow
// off for this repository: a dual repository must keep serving from git.
func newFromRegistry(repo *git.Repository) (git.Backend, error) {
	primary, err := git.NewBackend(git.DefaultBackendName, repo)
	if err != nil {
		return nil, err
	}
	opts := currentOptions()
	name, nameErr := ascbackend.RepoName(setting.RepoRootPath, repo.Path)
	if nameErr != nil {
		name = ""
	}
	var shadow git.Backend
	if opts.SamplePercent > 0 && nameErr == nil {
		shadow, err = git.NewBackend(ascbackend.Name, repo)
		if err != nil {
			shadow = nil
			logf(log.Warn, "dual: the shadow is off for %s: %s", echo(name), describeErr(err))
		}
	}
	return New(primary, shadow, name, opts), nil
}

// inflight counts the shadow calls running in the process.
var inflight atomic.Int64

// Backend serves one repository from its git backend and compares a sample of its reads with a
// shadow backend. It is safe for concurrent use exactly when the git backend is.
type Backend struct {
	primary git.Backend
	shadow  git.Backend // nil when the shadow is off
	name    string      // "owner/repo", for log lines
	opts    Options

	root context.Context // the parent of every shadow call; cancelled by Close
	stop context.CancelFunc

	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

var _ git.Backend = (*Backend)(nil)

// New combines primary (which answers every call) with shadow (which is compared on a sample). A nil
// shadow, or SamplePercent zero, makes the Backend a plain pass-through. name is only used in log lines.
// The shadow must be safe for concurrent use, because several shadow calls can run at once (ascbackend is).
func New(primary, shadow git.Backend, name string, opts Options) *Backend {
	opts = opts.normalised()
	if opts.SamplePercent == 0 {
		shadow = nil
	}
	root, stop := context.WithCancel(context.Background())
	return &Backend{primary: primary, shadow: shadow, name: echo(name), opts: opts, root: root, stop: stop}
}

// ShadowEnabled reports whether this Backend repeats a sample of its reads against the shadow.
func (d *Backend) ShadowEnabled() bool { return d.shadow != nil }

// ObjectFormat is the object format of the git backend.
func (d *Backend) ObjectFormat() git.ObjectFormat { return d.primary.ObjectFormat() }

// Close cancels the shadow calls in flight, waits a short time for them, then closes both backends.
// The error is the git backend's: the shadow closing badly is not the caller's concern.
func (d *Backend) Close() error {
	d.mu.Lock()
	already := d.closed
	d.closed = true
	d.mu.Unlock()
	if already {
		return nil
	}
	d.stop()
	if !d.wait(closeWait) {
		logf(log.Warn, "dual: shadow calls for %s did not stop within %s", d.name, closeWait)
	}
	if d.shadow != nil {
		if err := d.shadow.Close(); err != nil {
			logf(log.Debug, "dual: closing the shadow of %s: %s", d.name, describeErr(err))
		}
	}
	return d.primary.Close()
}

// Flush waits until every shadow call started so far has finished and been counted. Tests use it.
func (d *Backend) Flush(ctx context.Context) error {
	done := make(chan struct{})
	go func() { d.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (d *Backend) wait(timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return d.Flush(ctx) == nil
}

// shouldSample decides, before a call, whether it is repeated against the shadow.
func (d *Backend) shouldSample() bool {
	return d.shadow != nil && d.opts.Sample(d.opts.SamplePercent)
}

// begin reserves a place for one shadow call. It reports why it could not.
func (d *Backend) begin() (skipReason, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return skipClosed, false
	}
	if inflight.Add(1) > int64(d.opts.MaxInFlight) {
		inflight.Add(-1)
		return skipBusy, false
	}
	d.wg.Add(1)
	return "", true
}

func (d *Backend) end() {
	inflight.Add(-1)
	d.wg.Done()
}

// errOverLimit is returned by a shadow collector that read more than a comparison cap allows.
var errOverLimit = errors.New("dual: comparison is over a limit")
