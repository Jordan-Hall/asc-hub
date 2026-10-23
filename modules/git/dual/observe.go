// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package dual

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"forgejo.org/modules/git"
	"forgejo.org/modules/git/ascbackend"
	"forgejo.org/modules/log"

	"github.com/prometheus/client_golang/prometheus"
)

// The method labels of the metrics. The set is fixed, so label cardinality is bounded.
const (
	mReadCommit        = "ReadCommit"
	mReadTree          = "ReadTree"
	mReadTreeRecursive = "ReadTreeRecursive"
	mOpenBlob          = "OpenBlob"
	mStatObjects       = "StatObjects"
	mResolve           = "Resolve"
	mListRefs          = "ListRefs"
	mHead              = "Head"
	mLog               = "Log"
	mCount             = "Count"
	mMergeBase         = "MergeBase"
	mIsAncestor        = "IsAncestor"
	mLastCommits       = "LastCommits"
	mBlame             = "Blame"
	mChangedPaths      = "ChangedPaths"
	mUnifiedDiff       = "UnifiedDiff"
	mDiffStat          = "DiffStat"
	mGrep              = "Grep"
	mAttributes        = "Attributes"
	mLanguageStats     = "LanguageStats"
)

var allMethods = []string{
	mReadCommit, mReadTree, mReadTreeRecursive, mOpenBlob, mStatObjects, mResolve, mListRefs, mHead,
	mLog, mCount, mMergeBase, mIsAncestor, mLastCommits, mBlame, mChangedPaths, mUnifiedDiff, mDiffStat,
	mGrep, mAttributes, mLanguageStats,
}

// skipReason says why a sampled call was not compared. The set is fixed.
type skipReason string

const (
	skipUnsupported  skipReason = "unsupported"   // the shadow does not serve this call
	skipBusy         skipReason = "busy"          // over the in-flight cap
	skipTooLarge     skipReason = "too_large"     // over a comparison cap
	skipClosed       skipReason = "closed"        // the Backend was closed or the call cancelled
	skipPrimaryError skipReason = "primary_error" // git failed, so there is nothing to compare with
	skipShadowError  skipReason = "shadow_error"  // asc failed for a reason that says nothing about the data
	skipUncomparable skipReason = "uncomparable"  // a documented difference (package doc)
	skipPartial      skipReason = "partial"       // the caller did not read the whole stream
)

var allSkipReasons = []skipReason{
	skipUnsupported, skipBusy, skipTooLarge, skipClosed, skipPrimaryError, skipShadowError, skipUncomparable, skipPartial,
}

var (
	comparedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "forgejo_asc_shadow_compared_total",
		Help: "Sampled reads of dual repositories that were compared with asc, by method.",
	}, []string{"method"})
	mismatchTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "forgejo_asc_shadow_mismatch_total",
		Help: "Sampled reads of dual repositories where asc answered differently from git, by method.",
	}, []string{"method"})
	skippedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "forgejo_asc_shadow_skipped_total",
		Help: "Sampled reads of dual repositories that were not compared, by reason.",
	}, []string{"reason"})
)

func init() {
	prometheus.MustRegister(comparedTotal, mismatchTotal, skippedTotal)
	// Create every series, so that "no mismatch" reads as 0 rather than as a missing series.
	for _, m := range allMethods {
		comparedTotal.WithLabelValues(m)
		mismatchTotal.WithLabelValues(m)
	}
	for _, r := range allSkipReasons {
		skippedTotal.WithLabelValues(string(r))
	}
}

// Counts is a snapshot of what the shadow has compared in this process.
type Counts struct {
	Compared   map[string]uint64
	Mismatched map[string]uint64
	Skipped    map[string]uint64
}

var tally struct {
	sync.Mutex
	compared, mismatched, skipped map[string]uint64
}

// Snapshot returns the counters, for tests and diagnostics. The Prometheus counters are the
// operational interface.
func Snapshot() Counts {
	tally.Lock()
	defer tally.Unlock()
	cp := func(m map[string]uint64) map[string]uint64 {
		out := make(map[string]uint64, len(m))
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	return Counts{Compared: cp(tally.compared), Mismatched: cp(tally.mismatched), Skipped: cp(tally.skipped)}
}

func count(m *map[string]uint64, key string) {
	tally.Lock()
	defer tally.Unlock()
	if *m == nil {
		*m = map[string]uint64{}
	}
	(*m)[key]++
}

func countCompared(method string) {
	comparedTotal.WithLabelValues(method).Inc()
	count(&tally.compared, method)
}

func countMismatch(method string) {
	mismatchTotal.WithLabelValues(method).Inc()
	count(&tally.mismatched, method)
}

func countSkip(reason skipReason) {
	skippedTotal.WithLabelValues(string(reason)).Inc()
	count(&tally.skipped, string(reason))
}

// outcome is one side of a comparison.
type outcome struct {
	fp   fingerprint
	err  error
	skip skipReason // when set, the call is not compared
}

func skipped(reason skipReason) outcome { return outcome{skip: reason} }

// primaryOutcome turns the git answer into an outcome. A git error other than "no such object" says
// nothing a comparison could use.
func primaryOutcome(err error, build func() fingerprint) (out outcome) {
	if err != nil {
		if isNotExist(err) {
			return outcome{err: err}
		}
		return skipped(skipPrimaryError)
	}
	defer func() {
		if r := recover(); r != nil {
			out = skipped(skipShadowError)
		}
	}()
	return outcome{fp: build()}
}

// observe compares the git answer with the shadow's answer in the background. call repeats the read
// against the shadow and summarises the answer the same way the git answer was summarised.
// lenientMissing says that a shadow answer of "no such object" is a known limit, not a difference.
func (d *Backend) observe(method string, want outcome, lenientMissing bool, call func(ctx context.Context, sh git.Backend) (fingerprint, error)) {
	if want.skip != "" {
		countSkip(want.skip)
		return
	}
	if reason, ok := d.begin(); !ok {
		countSkip(reason)
		return
	}
	go func() {
		defer d.end()
		defer func() {
			if r := recover(); r != nil {
				countSkip(skipShadowError)
				logf(log.Warn, "dual: the shadow %s of %s panicked", method, d.name)
			}
		}()
		ctx, cancel := context.WithTimeout(d.root, d.opts.Timeout)
		defer cancel()
		fp, err := call(ctx, d.shadow)
		d.judge(method, want, outcome{fp: fp, err: err}, lenientMissing)
	}()
}

type errClass int

const (
	clsNone errClass = iota
	clsNotExist
	clsUnsupported
	clsBad
	clsTooLarge
	clsCancelled
	clsOther
)

func isNotExist(err error) bool {
	var ne git.ErrNotExist
	return errors.As(err, &ne)
}

func classify(err error) errClass {
	switch {
	case err == nil:
		return clsNone
	case isNotExist(err):
		return clsNotExist
	case errors.Is(err, ascbackend.ErrUnsupported):
		return clsUnsupported
	case errors.Is(err, ascbackend.ErrBadAnswer):
		return clsBad
	case errors.Is(err, ascbackend.ErrTooLarge), errors.Is(err, errOverLimit):
		return clsTooLarge
	case errors.Is(err, context.Canceled), errors.Is(err, ascbackend.ErrClosed):
		return clsCancelled
	}
	return clsOther
}

// judge decides what the two answers mean.
func (d *Backend) judge(method string, want, got outcome, lenientMissing bool) {
	cls := classify(got.err)
	switch cls {
	case clsUnsupported:
		countSkip(skipUnsupported)
		return
	case clsTooLarge:
		countSkip(skipTooLarge)
		return
	case clsCancelled:
		countSkip(skipClosed)
		return
	case clsOther:
		countSkip(skipShadowError)
		logf(log.Debug, "dual: the shadow %s of %s failed: %s", method, d.name, describeErr(got.err))
		return
	}
	switch {
	case cls == clsBad:
		d.mismatch(method, "the asc answer failed its checks", want.fp.head, "bad answer")
	case want.err != nil && cls == clsNotExist:
		countCompared(method) // both say the object does not exist
	case want.err != nil:
		d.mismatch(method, "git reports a missing object, asc returns one", "missing", got.fp.head)
	case cls == clsNotExist && lenientMissing:
		countSkip(skipUncomparable)
	case cls == clsNotExist:
		d.mismatch(method, "asc reports a missing object, git returns one", want.fp.head, "missing")
	case want.fp.equal(got.fp):
		countCompared(method)
	default:
		detail := "answers differ"
		if want.fp.items != got.fp.items {
			detail = fmt.Sprintf("item counts differ (git %d, asc %d)", want.fp.items, got.fp.items)
		}
		d.mismatch(method, detail, want.fp.head, got.fp.head)
	}
}

func (d *Backend) mismatch(method, detail, gitHead, ascHead string) {
	countCompared(method)
	countMismatch(method)
	m := Mismatch{Method: method, Repo: d.name, Detail: detail, Git: gitHead, Asc: ascHead}
	logf(log.Warn, "dual: shadow mismatch in %s for %s: %s (git: %q, asc: %q)", method, d.name, detail, gitHead, ascHead)
	if d.opts.OnMismatch != nil {
		d.opts.OnMismatch(m)
	}
}

// describeErr names an error without repeating its text, which may come from the network.
func describeErr(err error) string {
	var ce *ascbackend.Error
	if errors.As(err, &ce) {
		return fmt.Sprintf("asc %s answered %s", ce.Op, ce.Code)
	}
	switch {
	case errors.Is(err, ascbackend.ErrNotConfigured):
		return "the asc client is not configured"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline exceeded"
	}
	return fmt.Sprintf("an error of type %T", err)
}

// echo reduces s to a short printable string, for a log line.
func echo(s string) string {
	var b strings.Builder
	for _, r := range s {
		if b.Len() >= maxNameEcho {
			b.WriteString("...")
			break
		}
		if r >= 0x20 && r < 0x7f {
			b.WriteRune(r)
		} else {
			b.WriteByte('?')
		}
	}
	return b.String()
}

var logBudget struct {
	sync.Mutex
	minute time.Time
	used   int
}

// logf writes a log line unless this minute's budget is spent. The counters are not limited.
func logf(write func(format string, v ...any), format string, args ...any) {
	logBudget.Lock()
	now := time.Now().Truncate(time.Minute)
	if !now.Equal(logBudget.minute) {
		logBudget.minute, logBudget.used = now, 0
	}
	logBudget.used++
	ok := logBudget.used <= maxLogsPerMinute
	logBudget.Unlock()
	if ok {
		write(format, args...)
	}
}
