// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ascclient

import (
	"context"
	"math/rand/v2"
	"strconv"
	"sync"
	"time"

	ascv1 "forgejo.org/modules/ascproto/ascv1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	// RetryAfterTrailer is the trailer that carries the server's retry hint in milliseconds.
	RetryAfterTrailer = "retry-after-ms"
	// CodeRateLimited is the ErrorDetail code of a limit that clears by waiting.
	CodeRateLimited = "RATE_LIMITED"

	budgetWindowSecs = 10
)

// ErrorDetail returns the asc.v1.ErrorDetail attached to err, or nil.
func ErrorDetail(err error) *ascv1.ErrorDetail {
	st, ok := status.FromError(err)
	if !ok {
		return nil
	}
	for _, d := range st.Details() {
		if ed, ok := d.(*ascv1.ErrorDetail); ok {
			return ed
		}
	}
	return nil
}

// ErrorCode returns the ErrorDetail code of err (for example REF_CONFLICT), or "".
func ErrorCode(err error) string {
	if ed := ErrorDetail(err); ed != nil {
		return ed.GetCode()
	}
	return ""
}

// isRetryable reports whether err is worth another attempt: UNAVAILABLE and DEADLINE_EXCEEDED
// always, RESOURCE_EXHAUSTED only when the server calls it RATE_LIMITED or says nothing
// (QUOTA_EXCEEDED and the other limits never clear by waiting).
func isRetryable(err error) bool {
	st, ok := status.FromError(err)
	if !ok {
		return false
	}
	switch st.Code() {
	case codes.Unavailable, codes.DeadlineExceeded:
		return true
	case codes.ResourceExhausted:
		code := ErrorCode(err)
		return code == "" || code == CodeRateLimited
	default:
		return false
	}
}

// retryHint reads the retry-after-ms trailer.
func retryHint(md metadata.MD) time.Duration {
	v := md.Get(RetryAfterTrailer)
	if len(v) == 0 {
		return 0
	}
	ms, err := strconv.ParseUint(v[0], 10, 32)
	if err != nil {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

// budget allows retries to be at most 10% of requests in a sliding 10 s window.
type budget struct {
	mu    sync.Mutex
	now   func() time.Time
	slots [budgetWindowSecs]budgetSlot
}

type budgetSlot struct {
	sec      int64
	requests uint32
	retries  uint32
}

func newBudget() *budget { return &budget{now: time.Now} }

func (b *budget) slot(sec int64) *budgetSlot {
	s := &b.slots[sec%budgetWindowSecs]
	if s.sec != sec {
		*s = budgetSlot{sec: sec}
	}
	return s
}

func (b *budget) record() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.slot(b.now().Unix()).requests++
}

func (b *budget) tryRetry() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	sec := b.now().Unix()
	cur := b.slot(sec)
	var requests, retries uint64
	for i := range b.slots {
		if s := &b.slots[i]; sec-s.sec < budgetWindowSecs {
			requests += uint64(s.requests)
			retries += uint64(s.retries)
		}
	}
	if (retries+1)*10 > requests {
		return false
	}
	cur.retries++
	return true
}

// retrier implements the retry loop shared by the unary interceptor and the streaming helpers.
type retrier struct {
	cfg    RetryConfig
	budget *budget
	// jitter returns a uniform sample in [0, 1); sleep waits or returns the context error.
	jitter func() float64
	sleep  func(ctx context.Context, d time.Duration) error
}

func newRetrier(cfg RetryConfig) *retrier {
	return &retrier{
		cfg:    cfg,
		budget: newBudget(),
		jitter: rand.Float64,
		sleep: func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-t.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
}

// delay is the wait after `failed` failed attempts: full jitter below an exponential ceiling,
// raised to the server's hint. The hint is capped, so a hostile server cannot park the client.
func (r *retrier) delay(failed int, hint time.Duration) time.Duration {
	exp := min(max(failed-1, 0), 20)
	ceiling := r.cfg.Cap
	if shifted := r.cfg.Base << exp; shifted > 0 && shifted>>exp == r.cfg.Base {
		ceiling = min(shifted, r.cfg.Cap)
	}
	d := time.Duration(float64(ceiling) * min(max(r.jitter(), 0), 1))
	return max(d, min(hint, r.cfg.Cap))
}

// do runs attempt until it succeeds, fails with an error that is not retryable, or runs out of
// attempts, budget or time. attempt returns the trailer's hint with its error. A mutating call
// without an op id is never replayed (replaySafe false).
func (r *retrier) do(ctx context.Context, replaySafe bool, attempt func(ctx context.Context) (time.Duration, error)) error {
	r.budget.record()
	for failed := 1; ; failed++ {
		hint, err := attempt(ctx)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil || !replaySafe || !isRetryable(err) || failed >= r.cfg.MaxAttempts || !r.budget.tryRetry() {
			return err
		}
		d := r.delay(failed, hint)
		if dl, ok := ctx.Deadline(); ok && time.Until(dl) <= d {
			return err
		}
		if r.sleep(ctx, d) != nil {
			return err
		}
	}
}
